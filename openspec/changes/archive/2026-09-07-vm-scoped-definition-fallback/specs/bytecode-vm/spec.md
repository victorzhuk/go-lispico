## ADDED Requirements

### Requirement: Scoped definitions fall back before bytecode execution

A definition inside a lexical scope SHALL bind in that scope, preserving the
tree-walker's result and binding effects. The compiler SHALL reject scoped
`def` and scoped `defn` with its existing typed unsupported-form error, including
function-namespace definitions under Lisp-2. The runtime SHALL evaluate the
entire enclosing top-level form with the tree-walker before executing any
bytecode from that form. It SHALL NOT execute a compiled prefix and then repeat
its side effects during fallback.

Lexical scopes SHALL be recognized even when their binding list is empty.
Definitions in the actual top-level scope SHALL remain compilable, including
definitions within a top-level `do` that introduces no lexical scope. Unaffected
compiled functions SHALL retain existing slot/capture behavior without a new
per-call lexical environment allocation.

#### Scenario: Function-local definition leaves the outer value intact

- **WHEN** `(def x 10) ((fn [] (def x 1))) x` is evaluated under a VM engine
- **THEN** the result SHALL be `10`, matching the tree-walker, and the form containing the scoped definition SHALL use whole-form fallback

#### Scenario: Definition updates the current local binding

- **WHEN** `(let [x 1] (def x 2) x)` is evaluated
- **THEN** both evaluator modes SHALL return `2`

#### Scenario: New local definition does not escape

- **WHEN** `(let [x 1] (def y x)) y` is evaluated without an outer `y`
- **THEN** both evaluator modes SHALL report an undefined-symbol error for the final `y`

#### Scenario: Empty scope still isolates definitions

- **WHEN** `(def x 10) (let [] (def x 1)) x` is evaluated
- **THEN** both evaluator modes SHALL return `10`, and the empty binding list SHALL not allow a global write

#### Scenario: Lisp-2 local function definition does not replace its outer function

- **WHEN** a function is defined in an enclosing Lisp-2 function namespace and redefined inside a lexical scope
- **THEN** calls inside the scope SHALL use the local definition and calls after scope exit SHALL use the enclosing definition, matching the tree-walker

#### Scenario: Fallback executes evaluation side effects once

- **WHEN** a top-level form performs an observable mutation before reaching a scoped definition and is evaluated cold and repeatedly under a VM engine
- **THEN** each evaluation SHALL perform the mutation exactly once and return the same value and binding effects as the tree-walker

#### Scenario: Top-level do remains compiled

- **WHEN** `(do (def x 1) x)` is evaluated in the top-level scope
- **THEN** it SHALL remain in the compiled subset, bind top-level `x`, and return `1`
