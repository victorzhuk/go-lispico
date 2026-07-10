# stdlib-plugin — delta

## ADDED Requirements

### Requirement: Bootstrap macros bind through the engine's evaluator

The stdlib bootstrap SHALL define its Lisp-source macros and functions through the
environment's own evaluator, so definitions land where the engine's dialect axes
(Lisp-2 function cell, truthiness) place them — never through a separately
constructed evaluator with different axes.

#### Scenario: Threading macros work under the default CL dialect

- **WHEN** `runtime.New(nil)` loads the stdlib plugin and evaluates `(-> 1 (+ 2))`
- **THEN** the result SHALL be `3`, not an `UndefinedError`

#### Scenario: All bootstrap macros resolve in head position under Lisp-2

- **WHEN** a Lisp-2 engine evaluates each of `->`, `->>`, `as->`, `if-let`, `when-let`, `get-in` in head position
- **THEN** every form SHALL resolve and evaluate without `UndefinedError`
