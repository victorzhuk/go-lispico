## 1. Behavior contracts

- [ ] 1.1 Red tests per scenarios: mid-eval exhaustion fails closed
  (terminal); remainder returned on normal end AND on error unwind; ctx
  meter overrides engine meter; setup (New + Use) charged under engine
  meter; retained delta settled at eval end; ReleaseRetained on Rebuild
  shrinkage; direct `core.Evaluator.Apply` path metered via ctx.
- [ ] 1.2 Characterization: no meter anywhere → behavior and error shapes
  identical to `engine-reduction-and-allocation-metering` alone.
- [ ] 1.3 Concurrency: `-race` over concurrent evaluations sharing one
  engine meter; lease bookkeeping isolated per evaluation.
- [ ] 1.4 Invisibility: no Lisp-reachable binding or form exposes meter
  state; adversarial probe test (enumerate root env, attempt known names).

## 2. Implementation

- [ ] 2.1 `runtime/meter.go`: `Meter` interface (`LeaseEval`, `ReturnEval`,
  `ChargeRetained`, `ReleaseRetained`), no-op default, `NewLimitMeter`
  (flat atomic thresholds).
- [ ] 2.2 `runtime.WithMeter(ctx, m)`; meter carried in `evalState`;
  propagated by `ensureEvalState` / `AdoptEvalState` / VM re-entry so every
  evaluation path sees it.
- [ ] 2.3 `runtime.WithEngineMeter(m)` EngineOption: fallback meter + setup
  metering for `New` dialect bootstrap and `Use` plugin bootstrap.
- [ ] 2.4 Lease draw at evaluation start and re-lease at poll boundaries:
  request ≤1,024 reductions / 64 KiB; consume via change 1 counters; zero
  grant → `CodeResourceLimit` (terminal).
- [ ] 2.5 Single settlement on evaluation end and unwind: `ReturnEval` of
  the unconsumed remainder (deferred, exactly once).
- [ ] 2.6 Retained settlement: persistent-scope `RetainedUsage` delta →
  `ChargeRetained` at entry-point end; `Rebuild` freed delta →
  `ReleaseRetained`.

## 3. Integration

- [ ] 3.1 `go test ./... -race`; existing suite unchanged with no meter.
- [ ] 3.2 Crossval parity under a meter (same exhaustion class on both
  evaluators).
- [ ] 3.3 `GOLDSET_MODE=vm` goldset gate non-increasing on the no-meter
  path.

## 4. Verification

- [ ] 4.1 `openspec validate --strict meter-leases-and-session-ledgers`.
