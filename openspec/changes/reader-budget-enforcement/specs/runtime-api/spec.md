## MODIFIED Requirements

### Requirement: Evaluation reductions and cumulative allocation are metered

The runtime SHALL extend `ResourceLimits` with `MaxReductions` and
`MaxAllocationBytes` fields. Each SHALL default to a conservative value
(10,000,000 reductions and 64 MiB per evaluation) when left at zero — never
unlimited. Each evaluation SHALL carry a per-call reduction counter and a
per-call cumulative allocation counter, threaded through the same `evalState`
used for structural depth, so every evaluation path — including direct
`core.Evaluator.Apply` calls that bypass `Engine` methods — is metered.
Runtime-owned reads SHALL charge scan work, workspace, and reader output while
reading and before allocating the storage each charge covers. All source SHALL
be admitted before the first form evaluates; already-accounted reader output
SHALL NOT receive a second post-parse charge. Exceeding either ceiling SHALL raise
a `*core.LispicoError` with `Code: "ResourceLimitError"` that is terminal per
the core-engine non-catchability requirement. The evaluator SHALL observe the
caller's context cancellation at least every 1,024 reductions. Ceiling
enforcement MAY be batched with the cancellation countdown (bounded slack of
one batch, at most 128 reductions). Reader cancellation and deadline observation
SHALL obey the tighter reader checkpoint contract. Counters SHALL NOT be shared
across concurrent evaluations on the same engine. Allocation charges SHALL use
a fixed, architecture-independent, documented per-type size table.

#### Scenario: Tight allocation loop fails closed

- **WHEN** an Engine runs a loop that allocates faster than it reduces, configured with `MaxAllocationBytes: 1<<20`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` before the host is exhausted, and `try`/`catch` SHALL NOT intercept the error

#### Scenario: Reduction-amplified macro recursion fails closed

- **WHEN** an Engine runs a macro-amplified recursion that exceeds `MaxReductions` before tripping `MaxDepth`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: GoFunc-built values are charged

- **WHEN** a loop concatenates strings through a stdlib GoFunc until shallow result sizes exceed `MaxAllocationBytes`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` without per-plugin instrumentation

#### Scenario: Reader output is charged before evaluation

- **WHEN** source containing a flat literal whose parsed size exceeds `MaxAllocationBytes` is evaluated
- **THEN** the call SHALL fail with `Code: "ResourceLimitError"` during reading, before the disallowed reader allocation and before the first form's evaluation begins

#### Scenario: Context observed within the reduction budget

- **WHEN** the caller's context is cancelled mid-evaluation
- **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation

#### Scenario: Per-evaluation counters are isolated

- **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race`
- **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race

#### Scenario: Defaults match the embedder contract

- **WHEN** an Engine is constructed with no `MaxReductions` / `MaxAllocationBytes` and adversarial input runs
- **THEN** the defaults (10M reductions / 64 MiB allocation per evaluation) SHALL apply, never "unlimited"

## ADDED Requirements

### Requirement: Runtime source reading shares the evaluation lifecycle

`Eval`, `EvalWithBindings`, `LoadScope`, and background hot-reload source parsing
SHALL install the effective absolute evaluation deadline and active meter before
reading. The reader, compiler, and evaluator SHALL consume one continuous ledger;
neither a second reader pass nor a cached compilation SHALL reset it. Caller
cancellation SHALL be checked even for empty or comment-only source. A failed
read SHALL prevent form execution or hot-reload publication. Public evaluation
events and statistics SHALL describe the final settled failure where those
events are part of the entry point's contract.

This guarantee starts with the source string supplied for parsing. Legacy
context-free core reader APIs retain their depth-only contract. Filesystem source
acquisition and arbitrary host callbacks are not covered by reader interruption.

#### Scenario: Every source entry uses its existing budget

- **WHEN** an over-budget source is submitted through `Eval`, `EvalWithBindings`, `LoadScope`, or background hot reload
- **THEN** reading SHALL fail under that invocation's budget without executing source forms or publishing replacement bindings

#### Scenario: Reader and evaluator consume the same remaining allowance

- **WHEN** reading consumes part of a budget and execution needs more than the remaining allowance
- **THEN** execution SHALL fail with terminal `ResourceLimitError`, with no reset between phases and no duplicate reader charge

#### Scenario: Reader failures publish settled errors

- **WHEN** a public evaluation fails during reading or reader meter settlement
- **THEN** its returned error, supported evaluation event, and error statistics SHALL describe the same final failure exactly once
