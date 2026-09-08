## ADDED Requirements

### Requirement: Ordinary opcode errors reach active catch handlers

An ordinary evaluation error raised while executing valid bytecode SHALL reach
the nearest active catch handler regardless of whether it originates in a call,
native operation, name lookup, assignment or runtime collection construction.
The handler SHALL receive the same message-string value as under the tree-walker.
Without a handler, the original error class SHALL reach the host. Compile-time
syntax and bytecode-validation failures remain outside this runtime contract.

Cancellation, deadline expiry and resource-limit errors SHALL retain their
existing terminal classification and SHALL bypass all handlers. Unwinding SHALL
restore the handler's frame, expression state and structural depth; an unhandled
failure SHALL not affect a later evaluation through a reused VM.

#### Scenario: Undefined lookup is caught

- **WHEN** `(try missing (catch e 42))` is evaluated without a binding for `missing`
- **THEN** both evaluators SHALL return `42`

#### Scenario: Undefined mutation is caught

- **WHEN** `(try (set! missing 1) (catch e 42))` is evaluated without a binding for `missing`
- **THEN** both evaluators SHALL return `42` and SHALL NOT create the missing binding

#### Scenario: Closure error reaches its enclosing handler

- **WHEN** a compiled closure performs an undefined lookup while called within an outer `try`
- **THEN** the outer handler SHALL run with the same caught value and result as the tree-walker

#### Scenario: Native argument failure preserves later evaluation

- **WHEN** an ordinary error occurs while evaluating arguments of a native operator under a handler, followed by another operator call
- **THEN** the handler and later call SHALL use their own operands and callee bindings, with no stale native-call state

#### Scenario: Terminal error cannot run a handler

- **WHEN** a wrapped cancellation or deadline error, or a resource-limit error, arises under nested handlers
- **THEN** no handler SHALL run and the same terminal error class SHALL reach the host under both evaluators

#### Scenario: Failed evaluation does not poison reused execution

- **WHEN** an unhandled opcode error is followed by a valid evaluation or function application using a reused VM
- **THEN** the later operation SHALL see fresh execution state and the full configured depth allowance

### Requirement: Uncaught throws preserve their value and error identity

An explicit throw caught in Lisp SHALL deliver the original thrown value,
including its runtime type. An uncaught throw SHALL expose a typed error with
code `ThrowError` and the same thrown-value rendering as the tree-walker.
Throwing a value SHALL NOT become a missing-handler type error. A value whose
text resembles a terminal error SHALL remain an ordinary catchable throw.

#### Scenario: Uncaught string throw retains its message

- **WHEN** `(throw "boom")` is evaluated without an enclosing handler
- **THEN** the host SHALL recover a typed error with code `ThrowError` and message `boom` under both evaluator modes

#### Scenario: Caught structured throw retains its type

- **WHEN** `(try (throw {:code :denied}) (catch e e))` is evaluated
- **THEN** both evaluator modes SHALL return the same map value rather than a rendered string

#### Scenario: Uncaught non-String throw retains its rendering

- **WHEN** a non-String value is thrown without a handler
- **THEN** both evaluator modes SHALL expose `ThrowError` with equal thrown-value rendering

#### Scenario: Terminal-looking text remains catchable

- **WHEN** `(try (throw "context deadline exceeded") (catch e e))` is evaluated with an active, unexpired context
- **THEN** both evaluators SHALL return the thrown string successfully
