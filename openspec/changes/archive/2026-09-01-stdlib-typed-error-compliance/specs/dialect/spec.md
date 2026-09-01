## ADDED Requirements

### Requirement: Dialect adapter failures are typed by violated contract

Every validation failure originated locally by an active Dialect adapter SHALL
be recoverable through `errors.As` as a `*core.LispicoError`. Wrong argument
shape SHALL carry `Code: "ArityError"`, a value of the wrong runtime type SHALL
carry `Code: "TypeError"`, and a correctly typed value or keyword option outside
the adapter's accepted domain SHALL carry `Code: "EvalError"` unless a more
specific existing code governs it.

Errors received from shared kernels, evaluator callbacks, Builtin work-budget
flushes, and resource helpers SHALL retain their original type and code. Terminal
errors SHALL remain Terminal. The completeness check SHALL cover CL `nth`,
`mapcar`, and `sort` adapter bodies plus their factories and transitive helpers.

#### Scenario: CL adapter validation is classifiable

- **WHEN** CL `nth`, `mapcar`, or `sort` rejects argument shape, runtime type, or option domain locally
- **THEN** its error SHALL carry `ArityError`, `TypeError`, or `EvalError` according to the violated contract

#### Scenario: CL adapter callback errors pass through

- **WHEN** a CL adapter receives a typed or Terminal error from its shared kernel, key function, predicate, or mapped callback
- **THEN** it SHALL return that error without changing its code or Terminal status
