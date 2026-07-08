# dialect Specification

## ADDED Requirements

### Requirement: Symbol namespaces are a Dialect axis
A Dialect SHALL set the namespace axis to Lisp-1 (a single binding namespace) or Lisp-2 (a separate function cell). Under Lisp-2 a symbol MAY name a function and a value simultaneously; under Lisp-1 a symbol names one binding.

#### Scenario: Lisp-2 resolves head and argument positions separately
- **WHEN** an Engine runs a Lisp-2 Dialect and a symbol is bound as both a value and a function
- **THEN** the symbol in head position SHALL resolve to its function binding
- **AND** the same symbol in argument position SHALL resolve to its value binding

#### Scenario: Lisp-1 resolves both positions from one namespace
- **WHEN** an Engine runs a Lisp-1 Dialect
- **THEN** a symbol SHALL resolve to the same binding in head and argument position

### Requirement: funcall and function-reference are Lisp-2 forms
Under a Lisp-2 Dialect, `funcall` SHALL apply a function value taken from value position, and `#'name` SHALL yield the function-cell binding of `name`. These forms SHALL be absent under a Lisp-1 Dialect.

#### Scenario: funcall and #' apply the function cell under Lisp-2
- **WHEN** an Engine runs a Lisp-2 Dialect with `f` bound in the function cell
- **THEN** `(funcall #'f args...)` SHALL apply that function to the arguments

#### Scenario: funcall and #' are undefined under Lisp-1
- **WHEN** an Engine runs the default Lisp-1 Dialect
- **THEN** referencing `funcall` or `#'` SHALL fail as undefined

### Requirement: Identity Dialect namespace is unchanged
The identity Dialect SHALL be Lisp-1, so an Engine created without selecting the axis resolves symbols exactly as before this change.

#### Scenario: Default Engine keeps single-namespace resolution
- **WHEN** an Engine is created without changing the namespace axis
- **THEN** head and argument symbols SHALL resolve from the single binding namespace as before
