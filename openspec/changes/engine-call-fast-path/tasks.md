## 1. VM apply without wrapper chunk

- [ ] 1.1 Rework `vm.apply` closure branch to seed stack + enter the call protocol directly; delete the `<apply>` wrapper-chunk synthesis; keep `Apply` (fresh) and `ApplyPooled` (pooled) contracts.
- [ ] 1.2 Tests: apply parity for fixed/variadic closures, keywords, GoFuncs; arity errors before frame push; `AllocsPerRun` shows no chunk/slice allocation per apply.

## 2. Eval-state threading

- [ ] 2.1 Move per-invocation state onto the pooled VM (reset with `Reset`); tree-walker `Apply` accepts explicit state; lazy ctx wrap only on non-canonical GoFunc dispatch for re-entrancy.
- [ ] 2.2 Tests: re-entrant `GoFunc → Evaluator.Eval` observes shared depth counters; max-depth limits still enforced across the re-entry boundary; `-race` clean.

## 3. Lazy observability in Engine.Call

- [ ] 3.1 Atomic call counters when no callbacks registered; full timing + events when registered; `Stats()` accurate in both modes.
- [ ] 3.2 Tests: `Stats()` counts match call counts with and without callbacks; `OnPluginCall` fires with duration when registered; concurrent registration + `Call` race-free.

## 4. Verify

- [ ] 4.1 `go test ./...`, `-race ./runtime/... ./core/...` green.
- [ ] 4.2 Goldset gate: boundary-shaped cells improved, others non-regressing (ADR 0008 tiers).
- [ ] 4.3 Bench evidence recorded: `BenchmarkCall_Lispico` / `BenchmarkCallback_Lispico` on `WithBytecode()` engine — target ≤ ~500 ns and ≤ 4 allocs/op; residual gap vs GopherLua itemized (boxing, name lookup).
