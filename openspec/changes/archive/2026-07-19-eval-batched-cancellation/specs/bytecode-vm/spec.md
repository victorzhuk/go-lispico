# bytecode-vm — delta

## ADDED Requirements

### Requirement: Batched cancellation observation

The VM SHALL observe context cancellation and the engine evaluation deadline at
bounded intervals rather than before every instruction: at most a fixed
instruction budget apart on straight-line code, and unconditionally at every
loop back-jump and every function-call boundary. Cancellation and deadline
errors SHALL keep their current error shape.

#### Scenario: Loop observes cancellation within one iteration

- **WHEN** the caller's context is cancelled while a `loop`/`recur` body is iterating under the VM
- **THEN** evaluation SHALL stop with a context error no later than the next back-jump

#### Scenario: Recursion observes cancellation within one call

- **WHEN** the caller's context is cancelled while a recursive function is descending under the VM
- **THEN** evaluation SHALL stop with a context error no later than the next call boundary

#### Scenario: Straight-line code observes cancellation within the budget

- **WHEN** the caller's context is cancelled during a long straight-line instruction sequence
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget
