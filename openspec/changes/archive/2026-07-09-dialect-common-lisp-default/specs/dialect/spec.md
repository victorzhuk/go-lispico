# dialect Specification

## ADDED Requirements

### Requirement: Common Lisp dialect
The system SHALL provide a Common Lisp dialect composed of: a CL vocabulary over the shared builtin core (`defun`, `setq`, `progn`, `car`, `cdr`, `funcall`, and related), `nil`-only truthiness, the Lisp-2 namespace axis, and CL reader flags (`#'` and `#(...)` enabled, `[..]`/`{..}` literals disabled).

#### Scenario: CL surface forms evaluate
- **WHEN** an Engine runs the Common Lisp dialect
- **THEN** `defun` SHALL define a function, `(if false :y :n)` SHALL evaluate to `:y`, and `(funcall #'f args...)` SHALL apply `f`

#### Scenario: CL reader affordances parse
- **WHEN** an Engine runs the Common Lisp dialect
- **THEN** `#'f` and `#(...)` SHALL parse, and `[1 2]` SHALL NOT read as a vector literal

### Requirement: Clojure dialect preserves the prior surface
The system SHALL provide a Clojure dialect reproducing the interpreter's behavior prior to the default flip: Lisp-1, `nil`+`false` truthiness, current vocabulary, and bracket literals enabled.

#### Scenario: Clojure dialect matches the old default
- **WHEN** an Engine runs the Clojure dialect
- **THEN** conditionals SHALL treat `false` as falsy, symbols SHALL resolve from a single namespace, and `[..]`/`{..}` literals SHALL parse as before this change

#### Scenario: Clojure dialect is identity-compatible with the bytecode VM
- **WHEN** an Engine is constructed with `New(nil, WithBytecode(), WithDialect(clojure.Dialect()))`
- **THEN** the construction SHALL succeed and the Engine SHALL run the bytecode evaluator; the `clojure.Dialect()` value SHALL report `IsIdentity() == true`
