# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Batched cancellation observation

The VM SHALL observe context cancellation and the engine evaluation deadline at
bounded intervals rather than before every instruction: at most a fixed
instruction budget apart, counting every executed instruction — calls, tail
calls, and loop back-jumps included. A host `GoFunc` extends the wall-clock
observation window by its own execution time, since the VM never preempts host
code. An already-cancelled context SHALL be rejected at the evaluation boundary
before any instruction executes. Cancellation and deadline errors SHALL keep
their current error shape.

#### Scenario: Loop observes cancellation within the budget

- **WHEN** the caller's context is cancelled while a `loop`/`recur` body is iterating under the VM
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget

#### Scenario: Recursion observes cancellation within the budget

- **WHEN** the caller's context is cancelled while a recursive function is descending under the VM
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget

#### Scenario: Straight-line code observes cancellation within the budget

- **WHEN** the caller's context is cancelled during a long straight-line instruction sequence
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget

#### Scenario: Cancelled context rejected at the boundary

- **WHEN** `Eval` or `Call` is invoked with a context that is already cancelled
- **THEN** the evaluation SHALL return a context error without executing any instruction
