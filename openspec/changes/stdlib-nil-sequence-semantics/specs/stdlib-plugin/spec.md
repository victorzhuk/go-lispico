## ADDED Requirements

### Requirement: Nil is an empty input at sequence builtin boundaries

Nil acceptance SHALL be limited to this closed operation/position matrix:
`first`, `rest`, `last`, `count`, `empty?`, `reverse`, and canonical `sort` at
argument 1; every sequence argument of `concat`; canonical `nth` at argument 1;
`cons` at argument 2; `conj` at argument 1; canonical `map` and `filter` at
argument 2; the final sequence argument of both `reduce` arities; only the final
expanded argument of `apply`; and `string/join` at argument 2. At those
positions, `nil` SHALL observe zero elements and use the operation's specified
empty-list behavior.

Dialect adapters SHALL normalize their own call shapes before entering shared
kernels: CL `nth` argument 2, each CL `mapcar` list after its function, and CL
`sort` argument 1 follow the adapter's contract. No unlisted operation or
argument position SHALL gain nil acceptance from this requirement. Adding one
requires an explicit contract and matrix amendment.

This boundary rule SHALL NOT change the runtime type, equality, printing, or
truthiness of `nil` or an empty list. A non-collection, non-`nil` sequence input
SHALL retain the operation's existing observable behavior, whether that behavior
is a value or a typed error. Collection-length and construction-depth policy
SHALL be obtained from the active GoFunc evaluator, not from
`env.Evaluator()`, including inside a child lexical environment.

#### Scenario: Empty observers return their existing empty results

- **WHEN** `first`, `last`, and two-argument `reduce` receive `nil`
- **THEN** each result SHALL be `nil`, matching its current empty-list result

#### Scenario: Empty sequence producers return concrete empty lists

- **WHEN** `rest`, `reverse`, `sort`, `concat`, `map`, or `filter` receives `nil` in its sequence position
- **THEN** the result SHALL be an empty list, matching the operation's empty-list result

#### Scenario: Count and emptiness observe zero elements

- **WHEN** `(count nil)` and `(empty? nil)` are evaluated
- **THEN** the results SHALL be `0` and `true`, respectively

#### Scenario: Join observes no parts

- **WHEN** `(string/join "," nil)` is evaluated
- **THEN** the result SHALL be the empty string

#### Scenario: Nth uses empty-list bounds behavior

- **WHEN** `(nth nil 0)` and `(nth nil 0 :missing)` are evaluated
- **THEN** the first form SHALL return the existing index-out-of-bounds error and the second SHALL return `:missing`

#### Scenario: Cons extends an empty list

- **WHEN** `(cons 1 nil)` is evaluated
- **THEN** the result SHALL be the one-element list `(1)`

#### Scenario: Conj selects list semantics

- **WHEN** `conj` receives `nil` as its collection
- **THEN** its result and element order SHALL equal applying the same arguments to an empty list

#### Scenario: Empty higher-order inputs do not invoke their function

- **WHEN** `map`, `filter`, or `reduce` receives `nil` as its sequence and no invocation is required by the corresponding empty-list case
- **THEN** the supplied function SHALL NOT be invoked

#### Scenario: Reduce with an initializer returns it unchanged

- **WHEN** `(reduce f initial nil)` is evaluated
- **THEN** the result SHALL be `initial` without invoking `f`

#### Scenario: Apply expands a nil tail to zero arguments

- **WHEN** `(apply f prefix... nil)` is evaluated
- **THEN** `f` SHALL be called with exactly the explicit `prefix...` arguments and no tail arguments

#### Scenario: Non-nil scalar behavior is unchanged

- **WHEN** an affected operation receives a non-collection, non-`nil` value in its sequence position
- **THEN** evaluation SHALL produce that operation's existing value or typed error, including `false` for `empty?`

#### Scenario: Engines and dialect adapters share the boundary behavior

- **WHEN** an affected shared operation is reached through any Dialect name and evaluated by the Evaluator or VM
- **THEN** it SHALL apply the same nil boundary rule after the dialect adapter has normalized that name's call shape and SHALL produce the specified value or equivalent typed error

#### Scenario: Unlisted positions remain unchanged

- **WHEN** `nil` appears in an operation or argument position absent from the closed matrix
- **THEN** that call SHALL retain its prior behavior rather than inherit generic sequence nil acceptance

#### Scenario: Nested environments retain active resource limits

- **WHEN** a limited Engine invokes an affected sequence operation inside a Lambda whose child environment has no evaluator of its own
- **THEN** collection-length and construction-depth checks SHALL use the active evaluator's limits under both Evaluator and VM execution

## MODIFIED Requirements

### Requirement: Sequence extension builds its result in linear cost

`cons`, `conj`, and the sequence-extending path of `concat` SHALL extend their
argument without copying it per call: allocated bytes and allocation count SHALL
grow roughly linearly with the number of elements added across a loop, not
quadratically with the accumulated length. Charging SHALL follow the incremental
rule — the storage the operation newly allocated, not a deep measure of a result
whose bulk is shared with the argument.

Inputs SHALL stay immutable, element order and equality SHALL be unaffected,
`count` SHALL stay exact, and collection-length and construction-depth limits
SHALL still apply. `nil` in a sequence position SHALL select empty-list behavior;
non-collection, non-`nil` arguments SHALL keep their existing type errors.

#### Scenario: Accumulation completes under the default ledger

- **WHEN** a loop conses 100,000 elements onto an accumulator with default resource limits
- **THEN** it SHALL return the accumulated sequence rather than a `ResourceLimitError`

#### Scenario: Nil uses empty-list extension semantics

- **WHEN** `cons` or `conj` extends `nil`, or `concat` receives `nil` as an input
- **THEN** the result SHALL match extending or concatenating an empty list with the same arguments

#### Scenario: Semantics preserved

- **WHEN** `cons`/`conj` run on empty, small, and large sequences and on a non-collection, non-`nil` argument
- **THEN** results, immutability of the inputs, and the existing scalar type errors SHALL be unchanged

#### Scenario: Fresh builders keep deep charging

- **WHEN** `list`, `vector`, or `range` constructs a result from unrelated values
- **THEN** the evaluation SHALL still be charged the result's deep allocation size
