# core-engine — delta

## ADDED Requirements

### Requirement: Terminal errors are not catchable

`try`/`catch` SHALL NOT intercept terminal errors in either evaluator. The
terminal classes are: `context.Canceled` and `context.DeadlineExceeded`
(matched by `errors.Is`, including wrapped forms) and `*core.LispicoError`
with `Code: "ResourceLimitError"` (matched by `errors.As`). A terminal error
SHALL unwind every active handler, frame, and freeze record and surface to the
host boundary unchanged in class. Values raised by the `throw` special form
SHALL remain catchable regardless of their content — the filter applies to Go
error classes, never to thrown Lisp values.

#### Scenario: Deadline evasion loop terminates

- **WHEN** `(loop [] (try body (catch e nil)))` runs under an expired engine deadline or cancelled context on either evaluator
- **THEN** evaluation SHALL stop with an error satisfying `errors.Is(err, context.DeadlineExceeded)` or `errors.Is(err, context.Canceled)`, and the `catch` handler SHALL NOT observe it

#### Scenario: Nested resource-limit error is not caught by the VM

- **WHEN** a resource ceiling trips inside a closure or GoFunc invoked under an enclosing `try` on the bytecode VM
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` and the enclosing handler SHALL NOT run

#### Scenario: Explicit throws stay catchable

- **WHEN** a program evaluates `(try (throw "context deadline exceeded") (catch e e))` on either evaluator
- **THEN** the handler SHALL receive the thrown value and evaluation SHALL succeed

#### Scenario: Evaluators agree on terminal class

- **WHEN** the same terminal-error-producing program runs on the tree-walker and on the VM under identical limits
- **THEN** both SHALL surface the same terminal error class to the host
