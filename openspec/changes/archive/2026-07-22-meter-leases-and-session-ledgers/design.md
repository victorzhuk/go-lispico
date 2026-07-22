# Design — meter-leases-and-session-ledgers

## Decisions

- D1: Interface inversion (user decision): lispico defines and consumes
  `Meter`; the embedder implements composition. The earlier ranked
  `NewChildMeter` tree is dropped — yagel's Coordinator already owns rank
  semantics, and two ledger trees would drift. Lispico ships only the no-op
  and `NewLimitMeter` (flat thresholds, atomic counters) so the contract is
  testable without an embedder.
- D2: Meter rides `evalState`, not runtime wrappers. yagel's dispatch is
  `RootEnv().Evaluator().Apply(...)` — any design reading the meter only in
  `Engine` methods leaves the consumer's hottest path unmetered. `WithMeter`
  stores the meter where `ensureEvalState`/`AdoptEvalState` already
  propagate; every entry converges there.
- D3: Lease mechanics: draw ≤1,024 reductions / 64 KiB per `LeaseEval`
  (yagel core-runtime lease granularity), consume through change 1's local
  counters, re-lease at the poll boundary when the local grant is exhausted,
  return the remainder in one `ReturnEval` on evaluation end — including the
  error/unwind path (single deferred settlement per evaluation). At most one
  active compute lease per evaluation.
- D4: Grant semantics: partial grants are legal near ceilings; the engine
  consumes what it got and re-leases. Zero grant + error = exhausted →
  `CodeResourceLimit`, terminal. The engine never retries a denied lease.
- D5: Retained settlement once per evaluation from persistent-scope
  `RetainedUsage` deltas (root env; plus the load scope for `LoadScope`).
  `ChargeRetained` failure fails the evaluation with `CodeResourceLimit`
  after the writes happened — accepted at evaluation granularity and
  recorded as a deviation in the proposal; per-env ceilings did the
  before-work bounding.
- D6: Release is embedder-driven for scope retirement
  (`m.ReleaseRetained(env.RetainedUsage())` before dropping the scope) and
  engine-driven for `Rebuild` (frees delta) and cache eviction
  (`bytecode-cache-byte-and-node-bounds`).
- D7: Engine meter (`WithEngineMeter`) is the fallback and the setup meter:
  `New`'s dialect bootstrap and `Use`'s plugin bootstrap evaluate under it,
  so a command-scoped ledger can span setup. Ctx meter wins when both
  present; they are not stacked (one meter per evaluation — composition is
  inside the embedder's implementation).
- D8: Invisibility: no Lisp-facing API. Enforced negatively — no binding is
  added; the requirement pins it so a future debugging aid doesn't leak
  budgets into rule space.
- D9: `Meter` implementations must be goroutine-safe (concurrent evaluations
  share the engine meter); each evaluation's lease bookkeeping is
  single-goroutine by the per-evaluation `evalState` contract.

## Risks / Trade-offs

- Meter overhead when present: one `LeaseEval` per ≤1,024 reductions plus one
  settlement pair per evaluation — ~10⁴ meter calls for a worst-case 10M
  reduction eval, amortized exactly as ADR 0104 prescribes. Absent meter:
  a nil check.
- Charge-after-write retained settlement can overshoot a session ceiling by
  at most one evaluation's persistent delta (bounded by per-env caps);
  documented deviation.
- Interface stability: `Meter` is public API from day one; kept to four
  methods to minimize regret. Extensions (e.g. node dimensions) would be a
  new optional interface, not a breaking change.

## Migration Plan

1. `runtime/meter.go`: interface, no-op, `NewLimitMeter`; `WithMeter` ctx
   helper; `WithEngineMeter` option.
2. `evalState` carries the meter + lease-local remainders; wire lease draw /
   re-lease into the poll boundary; wire return-on-end/unwind.
3. Route change 1's allocation charges against the lease remainder.
4. Retained settlement at entry points; `ReleaseRetained` wiring for
   `Rebuild`.
5. Setup metering in `New`/`Use`.
6. Concurrency + exhaustion + characterization tests; goldset gate (no-meter
   path must not regress).

## Open Questions

None blocking.
