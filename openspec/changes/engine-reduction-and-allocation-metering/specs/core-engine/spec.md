# core-engine — delta

## ADDED Requirements

### Requirement: Per-evaluation reduction and allocation counters

The core evaluator SHALL carry a per-call reduction counter and a per-call
cumulative allocation counter on the `evalState` of every evaluation, never
shared across concurrent evaluations on the same engine. The tree-walker
SHALL charge one reduction per apply-trampoline iteration and per form
dispatch; the VM SHALL charge one reduction per instruction decode. Both
evaluators SHALL charge approximate allocation bytes before every value
constructor allocates backing, SHALL raise `Code: "ResourceLimitError"` when
either counter exceeds the configured ceiling, and SHALL pass that error
through `try`/`catch` unintercepted in both the tree-walker and the VM. For
shared forms under the same limits, the tree-walker and the VM SHALL agree on
counter values.

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

#### Scenario: Evaluators agree on counters

- **WHEN** the same form is evaluated by the tree-walker and by the VM under identical limits
- **THEN** both SHALL report the same reduction and allocation counter values
