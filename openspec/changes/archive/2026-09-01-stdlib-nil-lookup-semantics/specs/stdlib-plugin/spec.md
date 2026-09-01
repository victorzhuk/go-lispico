## ADDED Requirements

### Requirement: Map lookup preserves key presence

`get` SHALL accept two or three arguments. For a map subject, it SHALL return
the stored value when the key is present, including when that value is `nil`.
For a missing key or a `nil` subject, it SHALL return `nil` in the two-argument
form and the supplied default in the three-argument form. A non-map, non-`nil`
subject SHALL return a `*core.LispicoError` with `Code: "TypeError"`.

#### Scenario: Nil subject has no key

- **WHEN** `(get nil :k)` and `(get nil :k :missing)` are evaluated
- **THEN** the results SHALL be `nil` and `:missing`, respectively

#### Scenario: Present nil is not replaced by the default

- **WHEN** `(get (hash-map :k nil) :k :missing)` is evaluated
- **THEN** the result SHALL be `nil`

#### Scenario: Missing map key uses the default

- **WHEN** `(get (hash-map) :k :missing)` is evaluated
- **THEN** the result SHALL be `:missing`

#### Scenario: Scalar lookup remains an error

- **WHEN** `get` is called with a non-map, non-`nil` subject
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "TypeError"`

#### Scenario: Lookup arity errors are typed

- **WHEN** `get` is called with any arity other than two or three
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "ArityError"`

### Requirement: Nested lookup distinguishes an absent path from a nil value

`get-in` SHALL accept a subject, a key path, and an optional default. The key
path SHALL be a list, vector, or `nil`; a `nil` path SHALL be the empty path.
For each remaining key, the current subject SHALL be a map or `nil`. An absent
key or a `nil` subject with keys remaining SHALL make the path missing; the
two-argument form SHALL then return `nil`, and the three-argument form SHALL
return its default. A present `nil` at the terminal key SHALL be a successful
lookup and SHALL NOT be replaced by the default.

#### Scenario: Missing intermediate short-circuits traversal

- **WHEN** `(get-in (hash-map :a (hash-map)) (list :a :b :c))` is evaluated
- **THEN** the result SHALL be `nil` without attempting lookup on a non-map value

#### Scenario: Missing nested path uses the default

- **WHEN** `(get-in (hash-map :a nil) (list :a :b) :missing)` is evaluated
- **THEN** the result SHALL be `:missing`

#### Scenario: Terminal nil remains present

- **WHEN** `(get-in (hash-map :a (hash-map :b nil)) (list :a :b) :missing)` is evaluated
- **THEN** the result SHALL be `nil`

#### Scenario: Empty path returns the original subject

- **WHEN** `get-in` is called with an empty list, empty vector, or `nil` key path
- **THEN** it SHALL return the original subject and ignore any supplied default

#### Scenario: Non-map intermediate remains an error

- **WHEN** `(get-in (hash-map :a 1) (list :a :b))` is evaluated
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "TypeError"`

#### Scenario: Invalid key path remains an error

- **WHEN** `get-in` receives a key path that is not a list, vector, or `nil`
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "TypeError"`

#### Scenario: Engines share nested lookup behavior

- **WHEN** the same `get-in` behavior golden is evaluated by the Evaluator and VM
- **THEN** both engines SHALL return equal values or equivalent errors

#### Scenario: Long traversal is cancellable and metered

- **WHEN** `get-in` traverses a long key path under a cancelled caller context, an expired Engine-owned Evaluation deadline, or an exhausted Reduction budget
- **THEN** traversal SHALL stop with the corresponding context or resource-limit error rather than completing as an unmetered Builtin loop

#### Scenario: Borrowed lookup results do not consume allocation budget

- **WHEN** `get` returns a stored collection or default, or `get-in` returns a stored collection or the original subject for an empty path, under an otherwise sufficient tight allocation limit
- **THEN** lookup SHALL charge zero result-allocation bytes and SHALL return the borrowed value rather than a `ResourceLimitError`

#### Scenario: Empty-base Dialects do not inherit Builtin get-in

- **WHEN** an empty-base Dialect omits `get-in` from its vocabulary
- **THEN** `get-in` SHALL be undefined, while explicitly allowlisting it SHALL expose the shared Builtin

#### Scenario: Get-in has Builtin representation

- **WHEN** the `get-in` callable is printed or compared after this change
- **THEN** it SHALL have the same printed and equality behavior as other Builtins named `get-in`, not the previous Lambda behavior
