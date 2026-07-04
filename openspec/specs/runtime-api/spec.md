# runtime-api Specification

## Purpose

The runtime-api capability provides the public Go embedding API functionality for the system, registered and made ready for use when the system initializes.
## Requirements
### Requirement: runtime-api implementation
The system SHALL implement the runtime-api functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the runtime-api SHALL be ready for use

### Requirement: Configuration options behave as documented

Runtime options SHALL take effect as their documentation states, or SHALL be
removed rather than shipped as no-ops. `WithTimeout` SHALL bound `Eval` and
`EvalWithBindings`, not only `Call`. `Watch` SHALL stop when the context passed to
it is cancelled. Options that cannot be honored — an inert `WithBytecodeCache`, a
`WithHotReloadDir` that never watches — SHALL be removed.

#### Scenario: WithTimeout bounds Eval

- **WHEN** an engine is built with `WithTimeout(d)` and an `Eval` runs longer than `d`
- **THEN** the evaluation SHALL be cancelled with a deadline error

#### Scenario: Watch honors its context

- **WHEN** `Watch(ctx, dir)` is called and `ctx` is later cancelled
- **THEN** the watcher SHALL stop without a separate `Stop()` call

#### Scenario: No option is a silent no-op

- **WHEN** the public option set is enumerated
- **THEN** every option SHALL either change behavior as documented or be absent

