# runtime-api — delta

## ADDED Requirements

### Requirement: Evaluation deadline ownership

The Engine SHALL apply a safe default evaluation deadline of 30 seconds to `Eval`,
`EvalWithBindings`, and `Call`. When the caller's context already carries a
deadline at or earlier than the Engine's, the Engine SHALL NOT create its own
timer — the caller's deadline governs. When the caller's deadline is later, the
Engine's tighter bound SHALL still apply. `WithTimeout(0)` SHALL disable the
Engine deadline entirely, leaving the caller's context as the only bound; this is
intended for embedders that apply a deadline to every evaluation lifecycle
themselves (ADR 0010).

#### Scenario: Default deadline applies

- **WHEN** an Engine is constructed without `WithTimeout` and an evaluation runs longer than 30 seconds
- **THEN** the evaluation SHALL be cancelled with a deadline error

#### Scenario: Earlier caller deadline governs alone

- **WHEN** the caller's context deadline is earlier than the Engine's configured timeout
- **THEN** the evaluation SHALL be bounded by the caller's deadline and the Engine SHALL NOT layer a second timer

#### Scenario: Later caller deadline is tightened

- **WHEN** the caller's context deadline is later than the Engine's configured timeout
- **THEN** the evaluation SHALL be bounded by the Engine's timeout

#### Scenario: Explicit disablement

- **WHEN** an Engine is constructed with `WithTimeout(0)`
- **THEN** the Engine SHALL impose no deadline of its own and the caller's context SHALL be the only cancellation source
