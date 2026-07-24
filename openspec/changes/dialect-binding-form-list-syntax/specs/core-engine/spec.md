# core-engine — delta

## ADDED Requirements

### Requirement: let/let*/loop accept List or Vector bindings

The binding forms `let`, `let*`, and `loop` SHALL accept their binding list as
either a `Vector` of flat alternating name/value elements (`[n0 v0 n1 v1 …]`) or
a `List` of two-element `(name value)` binding pairs (`((n0 v0) (n1 v1) …)`).
Both surface shapes SHALL produce identical bindings and identical evaluation
behavior, and the tree-walker and the bytecode compiler SHALL parse them
identically. This makes the forms usable under a bracket-less dialect (for
example the default Common Lisp dialect, which has no `Vector` reader syntax).
An empty binding list SHALL be valid in either shape. A malformed binding list —
a Vector of odd length, or a List element that is not a two-element pair headed
by a symbol — SHALL be a compile/eval error naming both accepted shapes.
Binding semantics (parallel `let`, sequential `let*`, `loop`/`recur` targets)
SHALL be unchanged.

#### Scenario: Common Lisp list-pair bindings under the default dialect

- **WHEN** `(let ((a 1) (b 2)) (+ a b))` is evaluated under the `cl` dialect
- **THEN** it SHALL bind `a` and `b` and return `3`, with no read or compile error

#### Scenario: Clojure vector bindings still work

- **WHEN** `(let [a 1 b 2] (+ a b))` is evaluated under the `clojure` dialect
- **THEN** it SHALL behave exactly as before

#### Scenario: Both evaluators agree on the list form

- **WHEN** the same `let`/`let*`/`loop` List-form source is run through the tree-walker and the bytecode compiler
- **THEN** both SHALL produce the same result (crossval parity)

#### Scenario: Malformed bindings are rejected clearly

- **WHEN** a `let` binding list is neither a valid flat Vector nor a list of two-element pairs
- **THEN** evaluation SHALL fail with an error naming both accepted binding shapes
