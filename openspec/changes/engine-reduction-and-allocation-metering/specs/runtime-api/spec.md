# runtime-api — delta

## ADDED Requirements

### Requirement: Evaluation reductions and cumulative allocation are metered

The runtime SHALL extend `ResourceLimits` with `MaxReductions` and
`MaxAllocationBytes` fields. Each SHALL default to a conservative value
(10,000,000 reductions and 64 MiB per evaluation) when left at zero. Each
evaluation SHALL carry a per-call reduction counter and a per-call cumulative
allocation counter, threaded through the same `evalState` used for structural
depth; both SHALL be charged before reader-output is consumed, before every
apply / VM-instruction / macro-expansion / compiler-emit / GoFunc-dispatch
step, and before every value constructor allocates backing. Exceeding either
ceiling SHALL raise a `*core.LispicoError` with `Code: "ResourceLimitError"`
that `try`/`catch` SHALL NOT intercept. The evaluator SHALL observe the
caller's context cancellation at least every 1,024 reductions. Counters SHALL
NOT be shared across concurrent evaluations on the same engine.

#### Scenario: Tight allocation loop fails closed

- **WHEN** an Engine runs a loop that allocates faster than it reduces, configured with `MaxAllocationBytes: 1<<20`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` before the host is exhausted, and `try`/`catch` SHALL NOT intercept the error

#### Scenario: Reduction-amplified macro recursion fails closed

- **WHEN** an Engine runs a macro-amplified recursion that exceeds `MaxReductions` before tripping `MaxDepth`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Context observed within the reduction budget

- **WHEN** the caller's context is cancelled mid-evaluation
- **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation

#### Scenario: Per-evaluation counters are isolated

- **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race`
- **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race

#### Scenario: Defaults match the embedder contract

- **WHEN** an Engine is constructed with no `MaxReductions` / `MaxAllocationBytes` and adversarial input runs
- **THEN** the defaults (10M reductions / 64 MiB allocation per evaluation) SHALL apply, never "unlimited"
