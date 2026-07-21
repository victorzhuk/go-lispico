# runtime-api — delta

## ADDED Requirements

### Requirement: Meter interface with engine-side lease amortization

The runtime SHALL define a `Meter` interface — `LeaseEval(reductions,
allocBytes int64) (int64, int64, error)`, `ReturnEval(reductions, allocBytes
int64)`, `ChargeRetained(bytes, slots int64) error`, `ReleaseRetained(bytes,
slots int64)` — that the engine consumes and the embedder implements. The
engine SHALL NOT define ledger ranks or composition; it SHALL draw compute
credit in leases of at most 1,024 reductions and 64 KiB per call, hold at
most one active compute lease per evaluation, re-lease when a grant is
exhausted, and return the unconsumed remainder exactly once on evaluation end
including error unwind. A zero grant with error SHALL terminate the
evaluation with `Code: "ResourceLimitError"` (terminal). A meter SHALL be
attachable per evaluation via `runtime.WithMeter(ctx, m)` — honored on every
evaluation path including direct `core.Evaluator.Apply` — and per engine via
the `runtime.WithEngineMeter(m)` option, which also meters engine setup
(`New` dialect bootstrap and `Use` plugin bootstrap); a ctx meter SHALL
override the engine meter. At evaluation end the engine SHALL settle
persistent-scope retained deltas through `ChargeRetained` and SHALL credit
`Rebuild`-freed capacity through `ReleaseRetained`. Meter state SHALL NOT be
observable from evaluated Lisp code. Absent any meter, behavior SHALL be
unchanged. The runtime SHALL ship a no-op meter and a flat threshold meter
(`NewLimitMeter`); `Meter` implementations MUST be safe for concurrent use.

#### Scenario: Exhausted meter fails closed mid-evaluation

- **WHEN** a meter's compute credit runs out while an evaluation is drawing its next lease
- **THEN** the evaluation SHALL fail with `Code: "ResourceLimitError"`, `try`/`catch` SHALL NOT intercept it, and the engine SHALL NOT retry the lease

#### Scenario: Unconsumed credit returns on end and on unwind

- **WHEN** an evaluation granted N reductions consumes M < N and then completes — normally or by error
- **THEN** the meter SHALL receive exactly one `ReturnEval` crediting the unconsumed remainder

#### Scenario: Context meter overrides engine meter

- **WHEN** an engine has `WithEngineMeter(a)` and a call carries `WithMeter(ctx, b)`
- **THEN** the evaluation SHALL draw from `b` only

#### Scenario: Engine setup is metered

- **WHEN** an engine is constructed with `WithEngineMeter` and `Use` loads a plugin whose bootstrap exceeds the meter's credit
- **THEN** `Use` SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Retained delta settles at evaluation end

- **WHEN** a metered `LoadScope` call defines bindings that retain bytes and slots
- **THEN** the meter SHALL receive one `ChargeRetained` with the load scope's retained delta at evaluation end

#### Scenario: Direct Apply path draws from the ctx meter

- **WHEN** a host invokes a handler via `core.Evaluator.Apply` with a ctx carrying `WithMeter`
- **THEN** the evaluation SHALL draw leases from that meter

#### Scenario: Meter is invisible to rules

- **WHEN** evaluated code enumerates its environment and probes for meter state
- **THEN** no binding, form, or plugin SHALL expose budgets, grants, or counters

#### Scenario: Absent meter preserves existing behavior

- **WHEN** an engine entry point is called with no ctx meter and no engine meter
- **THEN** charges SHALL flow to the per-evaluation `evalState` ledger exactly as without this change

#### Scenario: Concurrent evaluations share a meter race-free

- **WHEN** multiple goroutines evaluate under one engine meter concurrently
- **THEN** every lease, return, and settlement SHALL be accounted and `go test -race` SHALL report no data race
