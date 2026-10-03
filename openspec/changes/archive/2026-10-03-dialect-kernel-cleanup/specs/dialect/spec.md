## ADDED Requirements

### Requirement: Dialect adapters accept every callable kind

A Dialect adapter that takes a function argument SHALL accept every value the evaluator can call, including values produced by the bytecode VM, and SHALL decide callability through one core predicate rather than an adapter-local list of concrete types.

#### Scenario: VM closure passed to a CL adapter

- **WHEN** a CL Engine on the VM evaluates `(mapcar (fn (x) (* x 2)) '(1 2))`
- **THEN** the result SHALL be `(2 4)`

#### Scenario: Non-callable argument is a type error

- **WHEN** a CL Engine evaluates `(mapcar 5 '(1 2))`
- **THEN** evaluation SHALL fail with a type error naming a function as the expected type
