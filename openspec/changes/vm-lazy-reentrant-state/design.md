# Design — vm-lazy-reentrant-state

## Evidence

Alloc profile, bytecode Callback bench (one `GoFunc` dispatch per call):

```
25.6%  core.AdoptEvalState        (state object)
24.1%  context.WithValue          (derived ctx)
```

`vm.reentryCtx` caches the adopted context per run, but a boundary call is one
run with one dispatch — the cache never amortizes anything on this shape.

## Wrapper

```go
type lazyStateCtx struct {
    context.Context            // caller ctx: Done/Err/Deadline/other keys
    deadline    time.Time      // snapshot at wrapper creation
    structDepth int64          // snapshot at wrapper creation
    state       *evalState     // nil until first Value(evalStateKey)
    counter     *atomic.Int64  // set with state
}
```

`Value(key)`: evaluation-state key → materialize once (build the state from
the snapshots — the same construction `AdoptEvalState` performs today — store
in `state`, return it); any other key → delegate to the embedded context.

Materialization is not synchronized: the wrapper is handed to one `GoFunc`
invocation on one goroutine. A host function that shares the ctx across its
own goroutines and races `Value` calls would be racing today's code too (the
current adopted ctx is built before dispatch, but nothing about `GoFunc`
contracts promises multi-goroutine re-entry); if the race detector disagrees
during implementation, a `sync.Once`-free CAS on the state pointer is the
fallback — one atomic, still zero steady-state allocs.

Actually decide by test: hammer a `GoFunc` that re-enters from two goroutines
under `-race`; if it flags, use `atomic.Pointer[evalState]` CAS
materialization. Either way the observable contract below holds.

## Cost accounting

Today per dispatching run: state alloc + `WithValue` alloc (≈2 objects,
~100 B) always. After: one `lazyStateCtx` alloc (~64 B) always, state alloc
only on actual re-entry. Net for non-re-entrant hosts: −1 object, −~40 B, no
`WithValue` chain on `Done`/`Err` delegation (embedding keeps those direct).
The wrapper itself could be pooled on the VM later; not now — one small object
is already inside the boundary-efficiency contract, and pooling a ctx that a
host may retain trades a spec-visible safety property for ~16 ns.

## Snapshot semantics (retention safety)

The wrapper carries values, not the `*VM`. A retained wrapper after the run
returns can at worst materialize a state from stale snapshots — the same
staleness a retained adopted ctx exposes today. Pooled-VM recycling is never
observable through it.

## Re-entry parity obligations

- Structural depth: nested `Call`/`Apply` through the wrapper counts against
  the enclosing budget (snapshot seeds the shared counter exactly as
  `AdoptEvalState` does now).
- Deadline: the enclosing engine deadline governs re-entrant evaluation.
- Existing runtime-api scenarios (`Re-entrant body shares one
  evaluation-state`, `Nested Call shares the enclosing resource budget`) are
  the regression tests; they must pass unmodified.
