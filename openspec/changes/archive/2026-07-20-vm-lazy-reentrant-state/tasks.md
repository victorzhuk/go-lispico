## 1. Lazy wrapper

- [x] 1.1 `evalState.structDepth` becomes `*atomic.Int64` in `core`: every construction site (`evalStateFrom` fallback, `ensureEvalState`, `DetachEvalState`, `AdoptEvalState`) allocates the counter; `EvalStructCounter` returns the pointer directly; `st.structDepth.Add/Load` call sites unchanged syntactically.
- [x] 1.2 `lazyEvalStateCtx` in `core`: embeds caller ctx (delegates `Done`/`Err`/`Deadline`/unknown keys); owns the shared counter inline, seeded at creation; snapshots the armed deadline; `Value` matches the state key via type assertion (never `==`), returns an already-attached state if the embedded ctx carries one, else CAS-materializes `&evalState{deadline, structDepth: &c.counter}` at most once per run.
- [x] 1.3 `AdoptEvalState` becomes lazy: existing state → today's early-out; otherwise allocate the wrapper and return it with `&wrapper.counter`. `reentrantCtx` keeps its shape (arm deadline first, adopt, assign `vm.structDepth`, cache per run) — the VM borrows the wrapper's counter for the run and `Reset` restores `ownStructDepth` as today.

## 2. Tests

- [x] 2.1 Non-re-entrant `GoFunc`: no evaluation-state and no `WithValue` allocation per call (`AllocsPerRun` assertion on the Callback shape; the wrapper struct itself remains ~1 alloc — assert the drop, not zero).
- [x] 2.2 Re-entrant `GoFunc` via `Engine.Call` with the received ctx: structural-depth and deadline budgets shared with the enclosing call (existing runtime-api scenarios pass unmodified), PLUS a multi-dispatch regression: two `GoFunc` dispatches in one body with the outer VM deeper at the second, re-entry there trips `MaxStructuralDepth` against the combined depth.
- [x] 2.3 Two-goroutine `Value(evalStateKey)` hammer on ONE shared wrapper ctx under `-race`: CAS materialization, no race, exactly one state object observable.
- [x] 2.4 Retained-ctx safety: a `GoFunc` stashing its ctx and calling `Value` after the run returns (VM reset + pooled reuse) gets snapshot-consistent state, never recycled-VM internals.

## 3. Verify

- [x] 3.1 `go test ./...`, `-race`, crossval green.
- [x] 3.2 `GOLDSET_MODE=vm` gate non-increasing; GoFunc-dispatching cells show the alloc drop.
- [x] 3.3 Bench evidence (benchstat ≥6): Callback boundary ns/op and allocs recorded.
