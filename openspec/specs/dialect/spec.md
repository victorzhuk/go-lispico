# dialect Specification

## Purpose
TBD - created by archiving change dialect-per-engine-dispatch. Update Purpose after archive.
## Requirements
### Requirement: Dialect selection is fixed at Engine construction
An Engine SHALL run exactly one Dialect, selected via a construction-time option, and that Dialect SHALL be immutable for the Engine's lifetime. Evaluated code SHALL NOT be able to change the running Dialect.

#### Scenario: Dialect chosen at construction
- **WHEN** an Engine is created with a Dialect option
- **THEN** every evaluation on that Engine SHALL dispatch special forms through that Dialect's effective table

#### Scenario: Evaluated code cannot change the Dialect
- **WHEN** evaluated source attempts to alter which special forms are available
- **THEN** the effective table SHALL remain the one resolved at construction

### Requirement: A Dialect is a Delta over a declared base
A Dialect SHALL be defined as a Delta — renames, additions, and removals of special forms — over a base that is either the full Kernel table or empty. Resolving the Dialect SHALL produce the Engine's effective special-form table.

#### Scenario: Rename resolves to the canonical form
- **WHEN** a Dialect renames a canonical Kernel form to another name
- **THEN** invoking the renamed name SHALL evaluate the canonical form
- **AND** the original canonical name SHALL NOT resolve unless the Delta also keeps it

#### Scenario: Removal makes a form uncallable
- **WHEN** a Dialect removes a form from its base
- **THEN** invoking that form SHALL fail as undefined

### Requirement: Empty-base Dialects are fail-closed
A Dialect built on the empty base SHALL expose only the special forms its Delta explicitly adds. A special form added to the Kernel table by a later change SHALL NOT become callable in an empty-base Dialect unless its Delta adds it.

#### Scenario: Unlisted kernel form is rejected
- **WHEN** an empty-base Dialect omits a kernel special form from its Delta
- **THEN** invoking that form under the Dialect SHALL fail as undefined

### Requirement: Per-Engine dispatch isolation
Two Engines running different Dialects in one process SHALL NOT share special-form dispatch state.

#### Scenario: Divergent Dialects on concurrent Engines
- **WHEN** two Engines are constructed with different Dialects
- **THEN** a form present in one Dialect and absent in the other SHALL resolve only on the Engine whose Dialect defines it

### Requirement: Default Engine behavior is preserved
An Engine created without a Dialect option SHALL evaluate the identity Dialect, reproducing the special-form behavior of the interpreter prior to this change.

#### Scenario: No option selects the identity Dialect
- **WHEN** an Engine is created with no Dialect option
- **THEN** the special forms `if`, `def`, `defn`, `let`, `quote`, `cond`, `loop`, and `recur` SHALL behave as they did before this change

### Requirement: Truthiness is a Dialect axis
A Dialect SHALL set the truthiness axis to one of two values: only `nil` is falsy, or both `nil` and `false` are falsy. All conditional special forms — `if`, `when`, `unless`, `cond`, `and`, `or`, `not` — SHALL determine truthiness from the running Dialect's setting.

#### Scenario: nil-only truthiness treats false as true
- **WHEN** an Engine runs a Dialect with `nil`-only truthiness
- **THEN** `(if false :yes :no)` SHALL evaluate to `:yes`

#### Scenario: nil-plus-false truthiness treats false as falsy
- **WHEN** an Engine runs a Dialect with `nil`+`false` truthiness
- **THEN** `(if false :yes :no)` SHALL evaluate to `:no`

#### Scenario: Axis applies across all conditional forms
- **WHEN** an Engine runs a Dialect with `nil`-only truthiness
- **THEN** `when`, `unless`, `cond`, `and`, `or`, and `not` SHALL each treat `false` as a true value consistently with `if`

### Requirement: Identity Dialect truthiness is unchanged
The identity Dialect SHALL keep `nil`+`false` truthiness, so an Engine created without selecting the axis behaves as before this change.

#### Scenario: Default Engine keeps prior truthiness
- **WHEN** an Engine is created without changing the truthiness axis
- **THEN** `(if false :yes :no)` SHALL evaluate to `:no`

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

