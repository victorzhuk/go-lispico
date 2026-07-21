## Why

yagel ADR 0104 / 0105 compose per-evaluation, Routine, Workflow, and Session
ledgers, with the engine drawing credit through small amortizing leases: "Lisp
meters amortize this with leases of at most 1,024 reductions and 64 KiB
allocation credit; unused credit returns on blocking yield or unwind."
`engine-reduction-and-allocation-metering` provides the leaf primitive
(per-eval counters); this change adds the embedder-facing composition seam.

Two consumer facts shape the design. First, yagel already owns a ranked
ledger coordinator (`internal/core/ledger.go` `LedgerRank`,
`reservation.go` `Coordinator`) with Session/Workflow/Invocation ranks
scaffolded and waiting — shipping a second ranked-ledger tree inside lispico
would create two competing session-ledger concepts. Lispico therefore defines
only the consumer interface and draws through it; the embedder implements
composition. Second, yagel's hot dispatch path calls
`core.Evaluator.Apply` directly, bypassing every `Engine` method — the meter
must attach at the `evalState` layer, not in runtime entry wrappers.

## What Changes

- New `runtime.Meter` interface — the engine is the CONSUMER; the embedder
  implements it:

  ```go
  type Meter interface {
      // LeaseEval draws up to the requested compute credit for the current
      // evaluation. The engine never requests more than 1,024 reductions and
      // 64 KiB per call. Granted amounts may be smaller near a ceiling; a
      // zero grant with err != nil means exhausted (fail closed).
      LeaseEval(reductions, allocBytes int64) (grantedRed, grantedAlloc int64, err error)
      // ReturnEval credits back the unconsumed remainder of prior grants.
      ReturnEval(reductions, allocBytes int64)
      // ChargeRetained settles persistent-scope growth (bytes, slots).
      ChargeRetained(bytes, slots int64) error
      // ReleaseRetained credits retained capacity on scope release/compaction.
      ReleaseRetained(bytes, slots int64)
  }
  ```

- No rank lattice, no `NewChildMeter`, no lispico-side ledger tree: the
  embedder's meter implementation knows its own position (yagel: a meter per
  invocation over its Coordinator). Lispico ships the interface, a no-op
  default, and one plain threshold meter (`runtime.NewLimitMeter`) for tests
  and simple embedders.
- Attachment, two levels: `runtime.WithMeter(ctx, m)` (per evaluation,
  carried in `evalState` — covers `Eval`, `EvalWithBindings`, `LoadScope`,
  `Call`, `(*Fn).Call`, and direct `core.Evaluator.Apply`) and
  `runtime.WithEngineMeter(m)` (EngineOption: the default when ctx carries
  none, and the meter for engine setup — `New` + `Use` bootstrap
  evaluations — and for the bytecode cache). Ctx meter overrides engine
  meter. Engine-setup coverage satisfies yagel's command-scoped
  `rules check` ledger, which "spans engine setup and every selected file".
- Lease amortization inside the engine: the evaluation draws a lease into
  `evalState`, consumes it via the existing local countdown/charge sites
  (from change 1 — no new per-step work), re-leases on exhaustion, and on
  evaluation end or unwind returns the unconsumed remainder. Consumed credit
  is never re-credited by the engine; monotonic totals are the meter
  implementation's own affair. A denied or exhausted lease raises
  `CodeResourceLimit` — terminal per `eval-noncatchable-terminal-errors`.
- Retained settlement at evaluation granularity: at evaluation end the
  engine charges `ChargeRetained` with the delta of the entry's persistent
  scopes (root env, and the `LoadScope` env for loads) since evaluation
  start, reading `RetainedUsage` from
  `retained-state-owned-capacity-accounting`. Scope retirement and `Rebuild`
  shrinkage credit back via `ReleaseRetained` (embedder calls it with the
  scope's final usage; `Rebuild` reports the freed delta). Per-write
  engine-side ceilings still enforce before work locally — the meter
  settlement is cross-scope bookkeeping, deliberately not per-write (see
  Deviations).
- Meter state is invisible to Lisp: no binding, form, or plugin exposes
  budgets, grants, or counters to evaluated code (yagel: "Rules cannot
  inspect, reset or delegate any ledger").
- Absent any meter: behavior and performance identical to change 1 alone
  (no-op path, characterization-tested).
- Introduces ADR 0013 (meter seam: interface consumption, lease
  amortization, settlement points).

## Deviations from yagel ADR 0104/0105

- Retained charges settle at evaluation end, not before each binding write.
  Before-work enforcement exists locally (per-env ceilings); the cross-scope
  meter sees the net delta once per evaluation. Rationale: a meter call per
  `Set` on the dispatch path is hot-loop poison and the per-env caps bound
  single-evaluation growth. Trigger to revisit: yagel needing pre-write
  session-level retained denial tighter than (per-env cap × one eval).
- "≤8 active leases" from yagel core-runtime is trivially satisfied: the
  engine holds at most one active compute lease per evaluation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `Meter interface with engine-side lease
  amortization`.

## Impact

- Depends on: `engine-reduction-and-allocation-metering` (charge sites,
  counters), `retained-state-owned-capacity-accounting` (`RetainedUsage`,
  `LoadScope`), `eval-noncatchable-terminal-errors` (terminal class).
- Code: new `runtime/meter.go`; `runtime/engine.go` (option, setup
  metering); `runtime/eval.go` (entry wiring); `core/eval.go` +
  `core/vm/vm.go` (lease countdown in `evalState`, return-on-unwind).
- yagel: implements `Meter` over its Coordinator; wires per-invocation
  meters via `WithMeter(ctx)` at its supervisor, engine meter for
  `rules check`. Unblocks the lease half of `meter-lisp-evaluation`.
