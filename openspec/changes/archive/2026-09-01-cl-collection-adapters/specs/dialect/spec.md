## ADDED Requirements

### Requirement: Dialect adapters have semantic fingerprint identity

Every Dialect adapter SHALL have a non-empty stable semantic ID/version supplied
through `WithAdapter` and stored in its `VocabEntry`. The Dialect fingerprint
SHALL include that ID with the visible/canonical names and SHALL change when the
ID/configuration changes. It SHALL NOT use a function pointer or only the
adapter's Go concrete type as semantic identity. Dialect resolution/Engine
construction SHALL reject an adapter entry with an empty ID.

#### Scenario: Adapter semantics participate in the fingerprint

- **WHEN** two otherwise identical Dialects bind an adapter under the same visible name with different semantic IDs or versions
- **THEN** their fingerprints SHALL differ, while repeated construction with the same stable ID SHALL produce the same fingerprint

#### Scenario: Empty adapter identity fails closed

- **WHEN** a Dialect adapter has an empty semantic ID
- **THEN** Dialect resolution and Engine construction SHALL reject the Dialect rather than cache an ambiguous fingerprint

## MODIFIED Requirements

### Requirement: Common Lisp dialect

The system SHALL provide a Common Lisp dialect composed of: a CL vocabulary over
the shared builtin core (`defun`, `setq`, `progn`, `car`, `cdr`, `funcall`, and
related), the Lisp-2 namespace axis, and CL reader flags (`#'` and `#(...)`
enabled, `[..]`/`{..}` literals disabled). Its truthiness SHALL be the uniform
truthiness (`nil` and `false` falsy), like every other Dialect.

A CL collection name whose argument order or arity differs from its shared
canonical Builtin SHALL resolve through a thin adapter over shared collection
kernels. `nth` SHALL accept a non-negative index followed by a list and SHALL
return `nil` beyond the list; `nil` SHALL be accepted as an empty list. `mapcar`
SHALL accept a function and one or more lists (including `nil` as an empty list),
apply the function to aligned elements, stop at the shortest list, and return a
List.

CL `sort` SHALL accept exactly a list, vector, or `nil` followed by a predicate
and either no options or one `:key` value. `:key nil` SHALL select identity.
Missing required arguments, dangling options, and extra positional arguments
SHALL be `ArityError`; unknown or duplicate keywords SHALL be `EvalError`; an
unsupported sequence SHALL be `TypeError`. The adapter SHALL project each
element exactly once in original order before invoking any predicate, apply the
predicate to projected keys using the active Dialect's generalized truthiness,
and stop all later callbacks after the first callback error. Sorting SHALL be
stable and SHALL NOT mutate the input. A List SHALL produce a List, a Vector a
Vector, and `nil` an empty List. The first callback error SHALL be preserved
unless the mandatory work-budget flush discovers a Terminal error, in which case
the shared Terminal precedence rule applies.

Adapter/kernel phases that do not re-enter evaluation, including input copying,
tuple alignment, key/result storage, and comparator scheduling, SHALL use the
shared Builtin work budget. Callback re-entry SHALL remain the sole owner of
callback execution charges. Canonical Clojure-style names SHALL retain their own
argument shapes and natural-sort behavior.

#### Scenario: CL surface forms evaluate

- **WHEN** an Engine runs the Common Lisp Dialect
- **THEN** `defun` SHALL define a function, `(if false :y :n)` SHALL evaluate to `:n`, and `(funcall #'f args...)` SHALL apply `f`

#### Scenario: CL reader affordances parse

- **WHEN** an Engine runs the Common Lisp Dialect
- **THEN** `#'f` and `#(...)` SHALL parse, and `[1 2]` SHALL NOT read as a vector literal

#### Scenario: CL nth adapts order and absence

- **WHEN** `(nth 1 '(a b c))` and `(nth 9 '(a))` are evaluated under the Common Lisp Dialect
- **THEN** the results SHALL be `b` and `nil`, respectively

#### Scenario: CL mapcar maps aligned tuples

- **WHEN** `(mapcar #'+ '(1 2) '(10 20 30))` is evaluated under the Common Lisp Dialect
- **THEN** the result SHALL be `(11 22)` and the function SHALL be invoked exactly twice

#### Scenario: CL sort uses predicate and key adapters

- **WHEN** CL `sort` receives a supported sequence, predicate, and optional `:key` function
- **THEN** it SHALL project each element once, order stably by generalized predicate truthiness, return the specified result type, and leave the input unchanged

#### Scenario: CL sort rejects malformed options deterministically

- **WHEN** CL `sort` receives a missing/dangling/extra argument, unknown keyword, or duplicate `:key`
- **THEN** it SHALL return `ArityError` for argument-shape failures and `EvalError` for unknown or duplicate keywords before invoking callbacks

#### Scenario: CL sort stops at the first callback error

- **WHEN** a key or predicate callback returns a typed or Terminal error
- **THEN** `sort` SHALL return that error unchanged and SHALL NOT invoke any later Lisp callback

#### Scenario: Canonical collection names are unchanged

- **WHEN** canonical `nth`, `map`, and natural `sort` are evaluated under a Dialect that exposes them directly
- **THEN** their argument shapes, results, and typed errors SHALL remain unchanged

#### Scenario: Adapters share implementation kernels

- **WHEN** CL adapter names and canonical names perform corresponding indexing, mapping, or sorting work
- **THEN** they SHALL use one shared kernel per operation family rather than duplicate algorithms
