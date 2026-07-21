## Why

A VM `Closure` captures its entire defining `*Env` chain, and every captured local is kept visible to it by **write-mirroring**: `OpSetLocal` on a captured slot re-writes the value into the env map on every mutation (`FullEnv` chunks mirror every local). This is the residual cost the slot-residency change left behind — loop bodies that mutate captured locals pay an env write per iteration, closure-heavy goldset cells carry the alloc traffic, and every closure holds its whole lexical chain live for the GC even when it references one variable.

Lua solves this with flat closures: a closure stores direct references to exactly the variables it uses (upvalues), computed by a purely local compiler pass — no static analysis, no whole-environment capture (Ierusalimschy, "Closures in Lua"; adversarially verified round-4 research, the top-ranked unapplied lever). CPython's cell-variable model is the same idea. Lispico's compiler already computes the capture set (`markCaptures`/`Captured`); the runtime just doesn't use it to flatten.

## What Changes

- Captured locals move from slot-value + env-mirror to **cell-resident**: a slot the compiler marked captured holds a `*core.Cell` created at binding time; reads/writes on that slot deref the cell. Uncaptured slots are untouched (plain values, today's fast path).
- `OpClosure` builds a flat capture array — the cells for exactly the free variables the sub-chunk references — instead of referencing the frame env. A VM closure becomes `{chunk, captures []*Cell, globals *Env}`; no lexical env chain.
- Write-mirroring (`env.Set` from `OpSetLocal`) is deleted. `set!` on a captured variable writes through the shared cell — visible to every closure over it and to the defining scope, exactly as the env gave; aliasing semantics are unchanged because the cell is the single storage location.
- Global resolution from closure bodies goes straight to the globals env (compiled bodies resolve locals to slots/cells at compile time; any remaining free name is global by construction; forms the compiler cannot compile already fall back whole-form to the tree-walker before any VM closure exists, so no compiled closure ever needs name-addressable locals).
- Tree-walker `Lambda` is untouched — parity is by results (crossval), not by mechanism.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: `Slot-resident locals` extended — captured locals are cell-resident with no per-mutation environment mirroring; capture aliasing, escape, and `set!` visibility semantics spelled out as scenarios.

## Impact

- Code: `core/vm/chunk.go` (capture descriptors), `core/compiler/compiler.go` (free-variable resolution to capture indices), `core/vm/vm.go` (`OpClosure`, `OpGetLocal`/`OpSetLocal` on captured slots, `OpBindCell` or equivalent at binding sites), `core/vm/frame.go`.
- Expected: closure-mutating loops drop the per-iteration env write; goldset closure/loop cells drop allocs (cell allocated once per binding instead of map traffic per write); GC no longer retains whole scope chains behind small closures. fib itself is nearly unaffected (no captures) — this targets the closure-heavy tail the program's other changes don't reach.
- Risks: `loop`/`recur` fresh-vs-shared binding per iteration must match the tree-walker exactly (crossval cells decide); Lisp-2 function-cell captures; hot-reload redefinition must keep today's behavior (captured cells are locals — global redef never touched them). Highest-complexity change of the set — lands last, behind the measurement of everything else.
- Sequencing: after the six-change program and `vm-fused-native-ops`; independent of the runtime-api changes.
