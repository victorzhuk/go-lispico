# Design — vm-flat-closures

## Model choice: CPython cells over Lua open/closed upvalues

Lua keeps upvalues "open" (pointing into the stack) while the frame lives and
"closes" them (copies out) at scope exit — an optimization that avoids cell
indirection for the still-on-stack window, at the cost of the close machinery
and its interaction with escapes. CPython's model allocates a cell at binding
time for any local the compiler knows is captured; the slot holds the cell for
its whole lifetime.

Decision: **cell-at-binding (CPython model)**. Rationale: lispico's compiler
already computes the capture set statically (`Captured`); the open/closed
distinction buys ~one deref on captured-local access while the defining frame
runs, and costs a scope-exit walk plus a subtle aliasing state machine. The
simple model is deterministic, easy to prove parity for, and removes the same
mirroring cost. If profiling later shows captured-local access hot inside
defining frames, open upvalues are a compatible refinement.

## Representation

- Chunk: `Captured []bool` (exists) plus, per sub-chunk, a capture descriptor
  list: for each free variable of the sub-chunk, whether it resolves to the
  enclosing frame's captured slot N or to the enclosing closure's capture
  index M (transitive capture through nested closures).
- Frame slot for a captured local: holds `Value` = `*cellBox` internally
  (VM-private; never leaks as a user-visible Value — `OpGetLocal` on the slot
  derefs, so user code only ever sees the content).
- Closure: `{Chunk, caps []*cellBox, globals *Env}`. `NewClosure` today takes
  the env; it takes the capture array instead. The globals pointer serves
  `OpGetGlobal`/`OpSetGlobal`/`OpSetLexical`-to-global and dialect func-cell
  resolution.

`cellBox` vs `core.Cell`: captured locals never participate in name-keyed env
lookup, site caching, or tombstoning, so they do not need `core.Cell`'s
canonical flag or version field. A minimal `struct{ v Value }` avoids paying
the versioned-read machinery where nothing reads it. Kept VM-internal.

## Opcode changes

- Binding a captured local (function entry for captured params, `let`/`loop`
  binding sites the compiler marked): allocate the box, store it in the slot
  (`OpBindCell slotIdx`, or folded into existing binding emission).
- `OpGetLocal`/`OpSetLocal` on a slot with `Captured[idx]`: deref/write the
  box. Compiler knows statically which — emit distinct opcodes
  (`OpGetCell`/`OpSetCell`) rather than branching at runtime on every local
  access; uncaptured locals keep the existing opcodes and their exact cost.
- `OpClosure`: materialize `caps` by walking the descriptor list — source is
  either the current frame's slot (the box pointer, shared) or the current
  closure's `caps[M]` (transitive). No env involved.
- Delete: `chunk.FullEnv` mirroring, `env.Set` from `OpSetLocal`,
  `markCaptures`' mirror-set plumbing (the capture ANALYSIS stays — it now
  drives cell allocation instead of mirroring).

## Semantics obligations (spec scenarios)

1. **Aliasing**: two closures capturing the same variable share one box —
   `set!` through one is visible through the other and to the defining scope.
   Today the shared env entry gives this; the shared box preserves it.
2. **Escape**: closure survives the defining frame; box keeps the value live.
3. **`set!` from the defining scope after closure creation** is visible to the
   closure (same box).
4. **`loop`/`recur` capture**: whether each iteration's closure captures a
   fresh binding or a shared one must match the tree-walker's env behavior
   exactly. This is decided by where the compiler emits box allocation
   (per-iteration binding site vs loop entry) — pin it with crossval cells for
   both `loop` and `let`-in-loop shapes BEFORE freezing the emission, since
   the tree-walker's answer is the spec.
5. **Fallback interaction**: forms the compiler cannot compile fall back
   whole-form before any VM closure exists (`isUnsupportedInBytecode` at
   compile), so no compiled closure body ever needs name-addressable locals.
   Guard: a compiled body that could observe locals by NAME at runtime would
   be a parity break — audit the compiled-subset surface (`quote` of symbols
   is data, not resolution; no runtime-eval form is in the subset) and assert
   the invariant in the design review.
6. **Deep transitive capture**: closure inside closure referencing the outer
   function's local — descriptor chain resolves through `caps`, crossval'd.

## Interaction with active changes

- `env-cell-versioned-reads` touches `core.Cell` (globals) — disjoint from
  `cellBox` (locals). No delta overlap: this change modifies `Slot-resident
  locals`, that one `Resolved global bindings`.
- `vm-lazy-reentrant-state` and the boundary changes are orthogonal.
- Goldset gate risk concentrates in loop cells that mutate captured locals:
  today they pay env-map writes per iteration; after, one box alloc per
  binding occurrence. Expected direction is down; the gate verifies.

## Rollback

The change is VM-internal with the tree-walker untouched; `WithTreeWalker()`
remains the behavioral reference and escape hatch throughout.

## Soundness audit

Task 3.3 audit of every runtime path that could observe a local by name
after the lexical env chain is dropped from VM closures. Result: no compiled-
subset path resolves caller locals by name; the invariant holds.

- `OpGetGlobal`/`OpSetLexical` surviving in compiled code resolve only names
  the compiler proved unbound in every enclosing scope (`ancestorBinds` at
  emission). The tree-walker's chain walk reaches the same answer for those
  names: the chain's lexical entries are exactly the ancestor locals the
  compiler excluded, so both fall through to the same global env. These
  opcodes resolve against the closure's `globals` — the env captured at
  `OpClosure` time, equal to the old `f.Env`.
- `set!` on a captured variable compiles to `OpSetCap`/`OpSetCell`
  (write-through), never to a name lookup.
- GoFunc re-entrancy (`vm.call` passes the frame env to GoFunc callbacks):
  stdlib GoFuncs use the env only to `eval.Apply` function *values*, which
  carry their own caps/globals; none resolve caller locals by name in it.
  `assert`'s `eval.Eval(arg, env)` re-evaluates an already VM-computed
  (self-evaluating) value, not a raw form — the VM evaluates arguments
  before any GoFunc runs, so no GoFunc ever receives a form that could
  name a caller local.
- Tree-walker `Lambda` values called from the VM resolve names against
  their own captured creation env (`core/eval.go` applies them via
  `f.Env.ChildVariadic`), independent of the VM frame's env.
- `quote`/quasiquote symbols are constants (data), never resolved.
- `defmacro` and `unquote-splicing` fall back whole-form at compile
  (`CodeUnsupported`), so no VM closure exists while tree-walked code runs.
- `def` inside a closure body (`OpSetGlobal`) now writes to the closure's
  `globals` — the same target today's uncaptured closures already use (the
  tree-walker writes to the per-call env instead; that divergence predates
  this change for uncaptured closures and this change only extends the
  established VM behavior to captured ones).
