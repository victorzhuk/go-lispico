## ADDED Requirements

### Requirement: Builtin failures are typed by violated contract

Every evaluation failure originated by an active stdlib Builtin SHALL be
recoverable through `errors.As` as a `*core.LispicoError`. A wrong argument count
SHALL carry `Code: "ArityError"`; a runtime value of the wrong required type
SHALL carry `Code: "TypeError"`; and a correctly typed value outside the
operation's accepted domain SHALL carry `Code: "EvalError"` unless an existing
more specific code governs it. Operation-specific diagnostic messages SHALL be
preserved where practical.

Errors received from a callback, the shared evaluation-state checkpoint, or a
resource helper SHALL retain their original type and code. In particular, a
Terminal error SHALL NOT be flattened into `EvalError` or made catchable. This
requirement SHALL NOT promise source positions because `core.Value` does not
carry them; positional errors require a separate value-model contract.

The completeness rule SHALL cover validation errors from registered GoFunc
bodies, closure factories, and every transitive stdlib helper they call. A plain
external error MAY cross a helper boundary only when it is immediately converted
to a `*core.LispicoError` with the correct code; bootstrap-only wrapping is
governed separately and is not an active Builtin failure.

#### Scenario: Wrong arity is classifiable

- **WHEN** any stdlib Builtin is called outside its accepted exact, ranged, or variadic arity
- **THEN** evaluation SHALL fail with a `*core.LispicoError` carrying `Code: "ArityError"`

#### Scenario: Wrong value type is classifiable

- **WHEN** a stdlib Builtin receives a runtime value of the wrong required type
- **THEN** evaluation SHALL fail with a `*core.LispicoError` carrying `Code: "TypeError"`

#### Scenario: Domain failure is classifiable

- **WHEN** a stdlib Builtin receives correctly typed arguments that violate an operation domain such as bounds, zero divisor, format syntax, or comparability
- **THEN** evaluation SHALL fail with a `*core.LispicoError` carrying `Code: "EvalError"` unless a more specific existing code applies

#### Scenario: Callback error keeps its classification

- **WHEN** a function invoked by `map`, `filter`, `reduce`, `apply`, sorting, or another higher-order Builtin returns a typed error
- **THEN** the enclosing Builtin SHALL return that error without changing its code

#### Scenario: Terminal error stays terminal

- **WHEN** a stdlib Builtin observes caller cancellation, an Evaluation deadline, or a ResourceLimitError
- **THEN** the error SHALL remain Terminal and SHALL unwind through Lisp `try`/`catch`

#### Scenario: Execution paths expose equivalent classifications

- **WHEN** the same invalid stdlib call is evaluated by the Evaluator and VM
- **THEN** both SHALL expose the same `Code` and equivalent diagnostic meaning

#### Scenario: A helper cannot hide a plain validation error

- **WHEN** a registered Builtin reaches a validation failure through a factory or transitive helper rather than directly in its GoFunc body
- **THEN** that failure SHALL still be a `*core.LispicoError` with the class required by the violated contract
