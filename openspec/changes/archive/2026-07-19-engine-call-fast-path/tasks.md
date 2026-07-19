## 1. VM apply without wrapper chunk

- [x] 1.1 Rework `vm.apply` closure branch to seed stack + enter the call protocol directly; delete the `<apply>` wrapper-chunk synthesis; keep `Apply` (fresh) and `ApplyPooled` (pooled) contracts.
- [x] 1.2 Tests: apply parity for fixed/variadic closures, keywords, GoFuncs; arity errors before frame push; `AllocsPerRun` shows no chunk/slice allocation per apply.

## 2. Eval-state threading

- [x] 2.1 Move per-invocation state onto the pooled VM (reset with `Reset`); tree-walker `Apply` accepts explicit state; lazy ctx wrap only on non-canonical GoFunc dispatch for re-entrancy.
- [x] 2.2 Tests: re-entrant `GoFunc → Evaluator.Eval` observes shared depth counters; max-depth limits still enforced across the re-entry boundary; `-race` clean.

## 3. Lazy observability in Engine.Call

- [x] 3.1 Atomic call counters when no callbacks registered; full timing + events when registered; `Stats()` accurate in both modes.
- [x] 3.2 Tests: `Stats()` counts match call counts with and without callbacks; `OnPluginCall` fires with duration when registered; concurrent registration + `Call` race-free.

## 4. Verify

- [x] 4.1 `go test ./...`, `-race ./runtime/... ./core/...` green.
- [x] 4.2 Goldset gate: boundary-shaped cells improved, others non-regressing (ADR 0008 tiers). VM-mode: pipeline 148→103, registry-fold 147→138 allocs; 10 cells flat, 0 regressed.
- [x] 4.3 Bench evidence recorded (in-repo `BenchmarkEngine_CallBytecode*`; the article `BenchmarkCall_Lispico`/`BenchmarkCallback_Lispico` live in the external comparison repo). Clean boundary (`pick`): ~300–410 ns, 32 B, 1 alloc/op. `(+ a b)` shape: ~660 ns, 144 B, 3 allocs/op (≤ 4 alloc target met; ~500 ns not met). Residual itemized: `+` dispatches as a GoFunc because the compiler emits `OpCall`, not `OpAdd`, for arithmetic under the shipped dialects (`CanonicalName` covers only the 22 special forms) — a pre-existing codegen gap tracked as the follow-up change `compiler-native-op-emission`; the one re-entrancy eval-state alloc is the safe-sharing cost, plus the variadic args slice.
