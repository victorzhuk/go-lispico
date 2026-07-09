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

### Requirement: Reader syntax varies by Dialect flags
A Dialect SHALL carry reader feature flags controlling: `[..]` and `{..}` literal syntax, `#'` function-reference syntax, and `#(...)` vector syntax. The reader SHALL honor the running Dialect's flags when tokenizing and parsing source.

#### Scenario: Function-reference syntax gated by flag
- **WHEN** an Engine runs a Dialect with `#'` enabled
- **THEN** `#'foo` SHALL read as a function-reference form
- **AND** under a Dialect with `#'` disabled, `#'foo` SHALL NOT read as a function-reference form

#### Scenario: Reader-vector syntax gated by flag
- **WHEN** an Engine runs a Dialect with `#(...)` enabled
- **THEN** `#(...)` SHALL read as a vector form

#### Scenario: Bracket literals gated by flag
- **WHEN** an Engine runs a Dialect with `[..]` literals disabled
- **THEN** `[1 2]` SHALL NOT read as a vector literal
- **AND** under a Dialect with `[..]` literals enabled, `[1 2]` SHALL read as a vector literal

### Requirement: Identity Dialect reader flags are unchanged
The identity Dialect SHALL enable `[..]`/`{..}` literals and disable `#'` and `#(...)`, so an Engine created without changing reader flags parses source exactly as before this change.

#### Scenario: Default Engine parsing is preserved
- **WHEN** an Engine is created without changing reader flags
- **THEN** `[..]` and `{..}` literals SHALL parse as before, and `#'`/`#(...)` SHALL NOT be special

### Requirement: Vocabulary is a name map over shared implementations
A Dialect SHALL present builtins under dialect-specific names via a vocabulary map from a visible name to a shared builtin implementation. A Dialect SHALL NOT carry its own copy of an implementation that the shared core already provides.

#### Scenario: Renamed builtin resolves to the shared implementation
- **WHEN** an Engine runs a Dialect mapping `car` to the shared first-implementation
- **THEN** `(car '(1 2 3))` SHALL evaluate to `1` using that shared implementation

#### Scenario: Semantics-differing name uses an adapter over the shared core
- **WHEN** a Dialect maps a name whose semantics differ from the shared core by argument order or arity
- **THEN** the name SHALL resolve through a thin adapter over the shared implementation, not a duplicated implementation

### Requirement: Empty-base vocabulary is fail-closed
An empty-base Dialect's vocabulary SHALL be an allowlist. A builtin whose name is absent from the Dialect's vocabulary map SHALL be uncallable, and a builtin added to the shared core by a later change SHALL NOT become callable unless the map adds it.

#### Scenario: Unlisted builtin is rejected
- **WHEN** an empty-base Dialect omits a builtin from its vocabulary map
- **THEN** invoking that builtin under the Dialect SHALL fail as undefined

### Requirement: Identity Dialect vocabulary is unchanged
The identity Dialect SHALL map today's builtin names onto today's implementations, so an Engine created without changing vocabulary resolves builtins exactly as before this change.

#### Scenario: Default Engine vocabulary is preserved
- **WHEN** an Engine is created without changing vocabulary
- **THEN** the builtins registered by loaded plugins SHALL be callable under their current names

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

