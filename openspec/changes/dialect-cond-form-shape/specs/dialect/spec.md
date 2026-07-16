# dialect — delta

## ADDED Requirements

### Requirement: Form-shape rules are Dialect-owned

A Dialect MAY define a Form-shape rule for a special form: a normalizer that
produces the form's canonical argument structure at special-form dispatch. One
normalizer SHALL serve both the Evaluator and the Compiler, so the two execution
paths cannot parse the same form differently. Normalization SHALL NOT rewrite
Reader output or stored data: quoted and quasiquoted forms pass through unchanged.
The first Form-shape rule is `cond` clause shape: the Clojure dialect accepts flat
test/expression pairs, the Common Lisp dialect retains nested clauses, and a
canonical clause is one test plus one body expression — a multi-expression
implicit-progn body SHALL be wrapped in kernel `do`. A form that does not match
its dialect's shape SHALL produce a typed error, never a panic.

#### Scenario: Clojure flat cond

- **WHEN** a Clojure-dialect Engine evaluates `(cond (< x 0) :neg (> x 0) :pos :else :zero)`
- **THEN** the flat pairs SHALL evaluate as clauses with the same result under the Evaluator and the VM

#### Scenario: CL nested cond with implicit progn

- **WHEN** a CL-dialect Engine evaluates a `cond` clause whose body holds multiple expressions
- **THEN** the body SHALL evaluate in order as if wrapped in kernel `do`, returning the last expression's value, identically under both execution paths

#### Scenario: Quoted cond data is untouched

- **WHEN** a program evaluates `(quote (cond ...))` or embeds a `cond` form in quasiquoted data
- **THEN** the resulting data SHALL be structurally identical to the source, with no normalization applied

#### Scenario: Malformed clause shape is a typed error

- **WHEN** a `cond` form violates its dialect's clause shape (odd flat pair, non-list nested clause)
- **THEN** evaluation SHALL return a typed error under both execution paths, never a panic
