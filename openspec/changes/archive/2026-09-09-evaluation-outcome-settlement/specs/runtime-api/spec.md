## ADDED Requirements

### Requirement: Evaluation observations follow final settlement

`Eval`, `EvalWithBindings`, and `LoadScope` SHALL determine their final result after evaluation settlement before publishing stats or an `OnEval` event. Each invocation SHALL contribute exactly one evaluation count and one event to each registered callback, with failure status and error cause consistent with the returned outcome. Duration SHALL cover settlement and exclude callback execution. Evaluation lease return SHALL remain exactly once on normal completion and failure.

This ordering SHALL preserve existing terminal-error precedence, recovered GoFunc errors, and public error wrapping. Retained-charge rejection SHALL continue to fail after ordinary evaluation writes have occurred; this requirement SHALL NOT roll those writes back. Callback-panic behavior is outside this requirement.

#### Scenario: Retained rejection is observed as failure

- **WHEN** otherwise successful evaluation writes a binding and its meter rejects the final retained charge
- **THEN** the caller SHALL receive `ResourceLimitError`, the event SHALL report the same failure class and cause, and the error count SHALL increase once
- **AND** the ordinary evaluation's binding SHALL remain under the existing charge-after-write contract

#### Scenario: Observers run after settlement

- **WHEN** a callback observes a metered evaluation completing normally
- **THEN** its retained settlement and unused lease return SHALL already have completed, and the evaluation SHALL be counted once as successful

#### Scenario: Terminal settlement error wins consistently

- **WHEN** evaluation returns a nonterminal error and settlement returns a terminal resource error
- **THEN** the returned outcome, event, and error statistics SHALL all reflect the selected terminal failure

#### Scenario: Setup and parse failures produce one outcome

- **WHEN** an evaluation fails before executing a form because setup or parsing fails
- **THEN** it SHALL produce exactly one failed evaluation count and callback event, without returning an unacquired lease

#### Scenario: Recovered GoFunc panic produces one outcome

- **WHEN** evaluation recovers a GoFunc panic
- **THEN** settlement SHALL complete before the single failure event, and the engine SHALL retain its documented panic-error and subsequent-call behavior

#### Scenario: Binding-scope entry points agree

- **WHEN** equivalent metered source is invoked through `EvalWithBindings` and `LoadScope`
- **THEN** both SHALL publish only their settled outcome, preserving `LoadScope`'s existing scope-return contract
