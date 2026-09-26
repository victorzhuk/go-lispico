## MODIFIED Requirements

### Requirement: Dialect renames normalize to canonical kernel forms

A resolved Dialect SHALL expose the mapping from its visible form names to
canonical kernel forms, and compilation SHALL normalize source through that
mapping so the compiler and VM operate only on canonical names. Removed forms
SHALL stay absent — normalization never resurrects a form the Dialect excludes.
The resolved special-form table SHALL be the only source of special-form
dispatch for the evaluator, macro expansion and the compiler. A name absent from
that table SHALL be treated as an ordinary symbol on every execution path, with
identical results and error codes.

#### Scenario: Renamed form compiles to the canonical form

- **WHEN** a Dialect renames `do` to `progn` and `(progn 1 2)` is compiled
- **THEN** the emitted chunk SHALL be equivalent to compiling `(do 1 2)` under the identity Dialect

#### Scenario: Removed form stays removed

- **WHEN** a fail-closed Dialect excludes `set!` and source calls `set!` without binding it
- **THEN** evaluation SHALL fail with `UndefinedError` on both execution paths, and SHALL NOT silently normalize to the kernel form

#### Scenario: Renamed-away name bound by the user

- **WHEN** under the CL dialect a program evaluates `(defun do (x) (* x 2))` and then `(do 5)`
- **THEN** the result SHALL be `10` under both the evaluator and the VM

### Requirement: Form-shape rules are Dialect-owned

A Dialect MAY define a Form-shape rule for a special form: a normalizer that
produces the form's canonical argument structure at special-form dispatch. One
normalizer SHALL serve both the Evaluator and the Compiler, so the two execution
paths cannot parse the same form differently. Normalization SHALL NOT rewrite
Reader output or stored data: quoted and quasiquoted forms pass through unchanged.
The first Form-shape rule is `cond` clause shape: the Clojure dialect accepts flat
test/expression pairs, the Common Lisp dialect retains nested clauses, and a
canonical clause is one test plus a body sequence evaluated as kernel `do`. The
body SHALL evaluate as kernel `do` regardless of whether the Dialect exposes
`do` under any name. A form that does not match its dialect's shape SHALL
produce a typed error, never a panic.

#### Scenario: Clojure flat cond

- **WHEN** a Clojure-dialect Engine evaluates `(cond (< x 0) :neg (> x 0) :pos :else :zero)`
- **THEN** the flat pairs SHALL evaluate as clauses with the same result under the Evaluator and the VM

#### Scenario: CL nested cond with implicit progn

- **WHEN** a CL-dialect Engine evaluates a `cond` clause whose body holds multiple expressions
- **THEN** the body SHALL evaluate in order as if wrapped in kernel `do`, returning the last expression's value, identically under both execution paths

#### Scenario: Multi-expression body without an exposed do

- **WHEN** a Dialect that removes `do`, or an empty-base Dialect that adds `if` and `cond` only, evaluates `(cond (true 1 2))`
- **THEN** the result SHALL be `2` under both execution paths

#### Scenario: Quoted cond data is untouched

- **WHEN** a program evaluates `(quote (cond ...))` or embeds a `cond` form in quasiquoted data
- **THEN** the resulting data SHALL be structurally identical to the source, with no normalization applied

#### Scenario: Malformed clause shape is a typed error

- **WHEN** a `cond` form violates its dialect's clause shape (odd flat pair, non-list nested clause)
- **THEN** evaluation SHALL return a typed error under both execution paths, never a panic

## ADDED Requirements

### Requirement: Special-form shape errors are typed on both paths

A malformed `function` or `funcall` form SHALL produce the same typed error code under the compiler as under the evaluator.

#### Scenario: function with two arguments

- **WHEN** a Lisp-2 Dialect evaluates `(function a b)` on each execution path
- **THEN** both SHALL return a `*LispicoError` with the same code
