## Purpose
The bytecode VM is the Engine's default evaluator. It compiles forms to bytecode
chunks and executes them on a stack machine, with an in-memory chunk cache
retained across evaluations within one process. It executes a documented subset of
the language: for a form it does not compile, the runtime returns a typed error
and falls back to the tree-walking evaluator for that whole top-level form.
There is no on-disk bytecode cache — it was removed, and `WithBytecodeCache` is
gone. Ground truth: `runtime/engine.go:305-310` (`bytecode: true` in the engine
config, `WithBytecode()`/`WithTreeWalker()` selecting away from it) and
`docs/adr/0002-bytecode-vm-disposition.md:23`.

## ADDED Requirements

### Requirement: A nested scope restores the enclosing scoped-definition flag

A lexical-scope boundary opened on a compiler SHALL capture the enclosing scoped-definition state and restore that state when the scope closes. A definition following a closed nested scope SHALL be judged against the enclosing scope, not treated as top-level. A sub-compiler introduced for a function body SHALL NOT alter the enclosing compiler's state.

#### Scenario: Definition after a nested scope is refused

- **WHEN** `(defn f [] (let [x 1] x) (def leaked 1))` is compiled
- **THEN** compilation SHALL return the typed unsupported-form error for the scoped definition

#### Scenario: Definition after each nested form is refused

- **WHEN** a function body ends a `let`, `let*`, `loop` with `recur`, or `try` with `catch` scope and then performs a `def` at the function's own scope
- **THEN** compilation SHALL return the typed unsupported-form error for that definition

#### Scenario: Definition inside the nested scope stays refused

- **WHEN** `(defn g [] (def inner 2))` is compiled
- **THEN** compilation SHALL return the typed unsupported-form error, unchanged

#### Scenario: A top-level definition still compiles

- **WHEN** a top-level `(def y 1)` is compiled with no enclosing lexical scope
- **THEN** compilation SHALL succeed

#### Scenario: Neither evaluator defines the escaped name

- **WHEN** such a function is defined and called under the bytecode engine and again under the tree-walker engine
- **THEN** the root environment SHALL not contain the definition in either evaluator

### Requirement: Deep macro expansion reaches binding-form operands

The deep macro-expansion pre-pass SHALL expand macros inside the binding forms of `let`, `let*`, and `loop`, reading the bindings operand rather than the head of the form. An expanded binding form SHALL retain its head, its binding operand, and its remaining body forms. A macro expansion inside a binding form SHALL produce the same value under the bytecode engine and the tree-walker engine.

#### Scenario: A macro in a let binding expands

- **WHEN** a macro doubling its argument is defined and `(let [y (m 3)] y)` is evaluated
- **THEN** no macro form SHALL remain in the expansion

#### Scenario: A macro in a let-star binding expands

- **WHEN** the same macro is used inside a `let*` binding form
- **THEN** the expansion SHALL contain no unexpanded macro form

#### Scenario: A macro in a loop binding expands

- **WHEN** the same macro is used inside a `loop` binding form
- **THEN** the expansion SHALL contain no unexpanded macro form

#### Scenario: Both evaluators agree on the expanded value

- **WHEN** the macro definition and the `let` form are evaluated under the bytecode engine and again under the tree-walker engine
- **THEN** both SHALL return the same doubled value

#### Scenario: A bindings operand that is not a binding list still reports a syntax error

- **WHEN** deep expansion is invoked on a binding form whose bindings operand is neither a vector of pairs nor a list of pairs
- **THEN** expansion SHALL report the binding syntax error