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

Deadline enforcement SHALL NOT require a wall-clock read at every checkpoint.
The VM MAY observe deadline expiry through an externally-armed expiry signal
(set once when the deadline passes) or through clock reads at a reduced,
fixed multiple of the checkpoint interval. In either mechanism the interval
between a deadline passing and the evaluation terminating SHALL be bounded
and documented: no more than a small fixed number of checkpoint intervals of
instruction execution (plus any single host `GoFunc`'s own execution time,
as today). Context cancellation SHALL still be checked at every checkpoint.
Arming the deadline signal SHALL remain lazy: an evaluation that completes
before its first checkpoint SHALL perform no clock read and no timer work.

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

#### Scenario: Deadline crossing terminates within the documented bound

- **WHEN** the engine deadline passes while a compiled body is executing instructions
- **THEN** evaluation SHALL fail with the same deadline error shape as today, within the documented bound of checkpoint intervals

#### Scenario: Short evaluations stay clock-free

- **WHEN** an evaluation completes before its first checkpoint on an engine with a configured timeout
- **THEN** the evaluation SHALL perform no wall-clock read and SHALL create no timer
