## 1. Lazy wrapper

- [ ] 1.1 `lazyStateCtx` in `core/vm`: embed caller ctx; snapshot deadline + structural depth at creation; materialize the evaluation state on first `Value(evalStateKey)`, at most once per run; delegate everything else.
- [ ] 1.2 Replace `reentrantCtx`'s eager `AdoptEvalState`+`WithValue` with the wrapper; keep the per-run cache field.
- [ ] 1.3 Expose the state-construction helper from `core` so the wrapper builds exactly what `AdoptEvalState` builds (no duplicated construction logic).

## 2. Tests

- [ ] 2.1 Non-re-entrant `GoFunc`: no evaluation-state allocation per call (`AllocsPerRun` assertion on the Callback shape).
- [ ] 2.2 Re-entrant `GoFunc` via `Engine.Call` with the received ctx: structural-depth and deadline budgets shared with the enclosing call (existing runtime-api scenarios pass unmodified).
- [ ] 2.3 Two-goroutine re-entry hammer under `-race`; if materialization races, switch to CAS materialization and re-run.
- [ ] 2.4 Retained-ctx safety: a `GoFunc` stashing its ctx and calling `Value` after the run returns gets snapshot-consistent state, never recycled-VM internals.

## 3. Verify

- [ ] 3.1 `go test ./...`, `-race`, crossval green.
- [ ] 3.2 `GOLDSET_MODE=vm` gate non-increasing; GoFunc-dispatching cells show the alloc drop.
- [ ] 3.3 Bench evidence (benchstat ≥6): Callback boundary ns/op and allocs recorded.
