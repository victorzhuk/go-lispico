# dialect — delta

## ADDED Requirements

### Requirement: Truthiness is uniform: nil and false are falsy

Truthiness SHALL be uniform across all dialects: `nil` and the concrete boolean
`false` SHALL be falsy, and every other value SHALL be truthy. A concrete
`Bool{false}` — whether written as the `false` literal or returned by a
comparison or predicate builtin — SHALL be falsy under every dialect. All
conditional special forms — `if`, `when`, `unless`, `cond`, `and`, `or`, `not`
— SHALL determine truthiness this way on both the tree-walker and the bytecode
VM. Dialects SHALL NOT vary truthiness; there is no truthiness axis.

#### Scenario: Predicate-driven conditional takes the correct branch

- **WHEN** any dialect evaluates `(if (= 1 2) :then :else)`
- **THEN** it SHALL evaluate to `:else`

#### Scenario: Literal false is falsy

- **WHEN** any dialect evaluates `(if false :then :else)`
- **THEN** it SHALL evaluate to `:else`

#### Scenario: Axis applies across all conditional forms

- **WHEN** any dialect evaluates `when`, `unless`, `cond`, `and`, `or`, and `not` against a `false` value
- **THEN** each SHALL treat `false` as falsy, consistently with `if`, on both evaluators

## MODIFIED Requirements

### Requirement: Common Lisp dialect
The system SHALL provide a Common Lisp dialect composed of: a CL vocabulary over the shared builtin core (`defun`, `setq`, `progn`, `car`, `cdr`, `funcall`, and related), the Lisp-2 namespace axis, and CL reader flags (`#'` and `#(...)` enabled, `[..]`/`{..}` literals disabled). Its truthiness is the uniform truthiness (`nil` and `false` falsy), like every other dialect.

#### Scenario: CL surface forms evaluate

- **WHEN** an Engine runs the Common Lisp dialect
- **THEN** `defun` SHALL define a function, `(if false :y :n)` SHALL evaluate to `:n`, and `(funcall #'f args...)` SHALL apply `f`

#### Scenario: CL reader affordances parse

- **WHEN** an Engine runs the Common Lisp dialect
- **THEN** `#'f` and `#(...)` SHALL parse, and `[1 2]` SHALL NOT read as a vector literal

## REMOVED Requirements

### Requirement: Truthiness is a Dialect axis

**Reason**: Truthiness is no longer a per-dialect axis. The nil-only setting had
exactly one observable effect on the value model — making the concrete
`Bool{false}` truthy — which silently inverted every predicate-driven
conditional under CL. It is replaced by the uniform truthiness requirement
above.

### Requirement: Identity Dialect truthiness is unchanged

**Reason**: Subsumed by uniform truthiness. The identity dialect's `nil`+`false`
falsiness is now the single truthiness rule for all dialects, so a separate
"unchanged" guarantee is redundant.
