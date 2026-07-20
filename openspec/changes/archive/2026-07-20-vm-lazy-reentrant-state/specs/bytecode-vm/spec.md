# bytecode-vm — delta

## ADDED Requirements

### Requirement: Lazy re-entrant evaluation state

Dispatching a host `GoFunc` from the VM SHALL NOT materialize an
evaluation-state value unless the host function actually requests it (by
re-entering the evaluator or reading the state from its context). When
requested, the state SHALL be materialized at most once per VM run and SHALL
carry the enclosing evaluation's structural-depth and deadline budget, so
re-entrant calls enforce the same resource limits as today. The context a
`GoFunc` receives SHALL delegate cancellation, deadline, and unrelated values
to the caller's context unchanged. State handed to a host function SHALL be
snapshot-consistent: retaining the context past the call SHALL NOT expose a
recycled VM's internals.

#### Scenario: Non-re-entrant host pays no state allocation

- **WHEN** a compiled body repeatedly dispatches a `GoFunc` that never re-enters the evaluator
- **THEN** no evaluation-state value SHALL be allocated for those dispatches

#### Scenario: Re-entry shares the enclosing budget

- **WHEN** a dispatched `GoFunc` re-enters the evaluator with the context it received
- **THEN** the re-entrant evaluation SHALL count structural depth against the enclosing run's budget and honor the enclosing deadline, identical to eager state adoption

#### Scenario: Caller context semantics pass through

- **WHEN** the caller's context is cancelled while a dispatched `GoFunc` is waiting on it
- **THEN** the `GoFunc` SHALL observe cancellation through the context it received exactly as it would through the caller's context
