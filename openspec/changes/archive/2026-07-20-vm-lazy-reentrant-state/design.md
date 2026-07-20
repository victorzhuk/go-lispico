# Design — vm-lazy-reentrant-state

## Evidence

Alloc profile, bytecode Callback bench (one `GoFunc` dispatch per call):

```
25.6%  core.AdoptEvalState        (state object)
24.1%  context.WithValue          (derived ctx)
```

`vm.reentryCtx` caches the adopted context per run, but a boundary call is one
run with one dispatch — the cache never amortizes anything on this shape.

## Sharing contract (the invariant the first draft got wrong)

The structural-depth budget is shared by POINTER today: `AdoptEvalState`
returns `&st.structDepth` and `reentrantCtx` assigns it to `vm.structDepth`
(vm.go), so the outer VM and any re-entrant evaluator mutate one atomic.
Any lazy design MUST preserve this: a value snapshot (`structDepth int64`)
materialized into a fresh counter splits the budget in two and silently
escapes `MaxStructuralDepth` (ADR 0007), and the single-dispatch snapshot in
every existing re-entrancy test happens to equal live depth, so no test would
catch it. A multi-dispatch run (VM descends deeper between two GoFunc
dispatches in one body) makes the split observable.

Retention safety constrains the fix the other way: the wrapper MUST NOT point
at `&vm.ownStructDepth` — a `GoFunc` retaining its ctx past the run would then
hold a pointer into pooled-VM memory recycled by an unrelated evaluation.

## Wrapper

The wrapper lives in `core` (it must recognize `evalStateKey` and build an
`evalState`; both are unexported) and owns the shared counter inline:

```go
type lazyEvalStateCtx struct {
    context.Context            // caller ctx: Done/Err/Deadline/other keys
    deadline    time.Time      // snapshot at wrapper creation (immutable once armed)
    counter     atomic.Int64   // THE shared counter; seeded at creation
    state       atomic.Pointer[evalState] // nil until first Value(evalStateKey)
}
```

`Value(key)`: key asserted via `key.(evalStateKey)` (type assertion, never
`==` — a non-comparable key must not panic) → return the already-attached
state if the embedded ctx carries one (mirrors `AdoptEvalState`'s early-out);
otherwise materialize once: `&evalState{deadline: c.deadline, structDepth:
&c.counter}` published with `CompareAndSwap` (CAS is unconditional — it costs
one atomic and closes the concurrent-`Value` write race outright). Any other
key → delegate to the embedded context.

`evalState.structDepth` changes from `atomic.Int64` to `*atomic.Int64` so a
materialized state can reference the wrapper's counter. Every construction
site (`evalStateFrom` fallback, `ensureEvalState`, `DetachEvalState`,
`AdoptEvalState`) allocates the counter; all `st.structDepth.Add/Load` call
sites are syntactically unchanged (pointer receiver).

`AdoptEvalState` (single caller: `reentrantCtx`) becomes lazy:

- ctx already carries a state → return it and its counter pointer unchanged
  (zero allocs, today's early-out).
- otherwise → allocate the wrapper, seed `counter` from the VM's current
  depth, snapshot the (already armed) deadline, return the wrapper and
  `&wrapper.counter`. The VM assigns that pointer to `vm.structDepth` exactly
  as today — from wrapper creation on, outer VM and any future materialized
  state count against the SAME live atomic, so multi-dispatch depth growth is
  always reflected (no stale snapshot).

## Cost accounting

Today per dispatching run: `evalState` alloc + `WithValue` alloc (2 objects,
~100 B) always. After: one wrapper alloc (~64 B) always; `evalState` alloc
only on actual re-entry, and never a `WithValue` (the wrapper IS the ctx;
materialization returns the state pointer directly). Net for non-re-entrant
hosts: 2 → 1 objects, −~40 B, no `WithValue` chain on `Done`/`Err`
delegation. Re-entrant hosts: 2 objects, same as today. The wrapper itself
could be pooled on the VM later; not now — pooling a ctx a host may retain
trades a spec-visible safety property for ~16 ns.

## Snapshot semantics (retention safety)

The wrapper carries a deadline VALUE and its OWN heap counter — never a
pointer into VM memory. The VM borrows `&wrapper.counter` for the run's
duration; `Reset`/`reset` restore `vm.structDepth = &vm.ownStructDepth` before
the VM returns to the pool. A wrapper retained past the run materializes a
state from its own counter and deadline snapshot — the same staleness a
retained adopted ctx exposes today. Pooled-VM recycling is never observable
through it.

## Re-entry parity obligations

- Structural depth: nested `Call`/`Apply` through the wrapper counts against
  the enclosing budget via the shared live counter — identical to today's
  pointer adoption, including across multiple dispatches in one run.
- Deadline: `armDeadline` runs before the snapshot (existing `reentrantCtx`
  order is preserved); the enclosing engine deadline governs re-entrant
  evaluation. `ctx.Deadline()` stays delegated to the caller — the engine
  deadline flows only through `evalState.deadline` via `pollCancel`; do not
  "unify" them.
- Regression coverage: the existing runtime-api scenarios (`Re-entrant body
  shares one evaluation-state`, `Nested Call shares the enclosing resource
  budget`) pass unmodified, PLUS a new multi-dispatch scenario — two GoFunc
  dispatches in one body, the outer VM entering deeper structure between
  them, the later dispatch re-entering — asserting the combined depth trips
  `MaxStructuralDepth`. The single-dispatch scenarios alone cannot detect a
  split counter.
