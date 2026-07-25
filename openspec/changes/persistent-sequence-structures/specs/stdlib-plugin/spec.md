# stdlib-plugin — delta

## ADDED Requirements

### Requirement: Sequence extension builds its result in linear cost

`cons`, `conj`, and the sequence-extending path of `concat` SHALL extend their
argument without copying it per call: allocated bytes and allocation count SHALL
grow roughly linearly with the number of elements added across a loop, not
quadratically with the accumulated length. Charging SHALL follow the incremental
rule — the storage the operation newly allocated, not a deep measure of a result
whose bulk is shared with the argument.

Observable semantics SHALL be unchanged: inputs stay immutable, element order and
equality are unaffected, `count` stays exact, collection-length and construction
depth limits still apply, and non-collection arguments keep their existing type
errors.

#### Scenario: Accumulation completes under the default ledger

- **WHEN** a loop conses 100,000 elements onto an accumulator with default resource limits
- **THEN** it SHALL return the accumulated sequence rather than a `ResourceLimitError`

#### Scenario: Semantics preserved

- **WHEN** `cons`/`conj` run on empty, small, and large sequences, on `nil`, and on a non-collection argument
- **THEN** results, immutability of the inputs, and the existing type errors SHALL be unchanged

#### Scenario: Fresh builders keep deep charging

- **WHEN** `list`, `vector`, or `range` constructs a result from unrelated values
- **THEN** the evaluation SHALL still be charged the result's deep allocation size

## MODIFIED Requirements

### Requirement: assoc charges constructed allocation deeply

`assoc` SHALL charge the evaluation allocation ledger for the storage its result
newly allocates, together with the length check applied to `conj`'s map branch and
`merge`, rather than relying on the shallow post-return `GoFunc` result charge.
Where the result shares substructure with the input map, the shared part SHALL NOT
be charged again — it was charged when it was created; where `assoc` inserts a
value that is new to the ledger, that value's deep size SHALL be charged. A
chained or large-valued `assoc` whose newly allocated storage exceeds the
remaining `MaxAllocationBytes` SHALL fail with a `ResourceLimitError`. Ordinary
`assoc` within budget SHALL return the updated map unchanged in value.

#### Scenario: assoc with large nested values is charged for its real size

- **WHEN** `assoc` inserts a large nested value into a map under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the inserted value plus the map storage the operation allocated, not the map's shallow entry count

#### Scenario: Chained assoc does not re-charge the shared map

- **WHEN** `assoc` is applied repeatedly to the result of the previous `assoc` in a loop
- **THEN** the total charge SHALL grow with the values and nodes added, not with the accumulated map size per iteration

#### Scenario: Over-budget assoc fails closed

- **WHEN** an `assoc` result's newly allocated storage would exceed the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` rather than an uncharged large map

#### Scenario: Ordinary assoc is unchanged

- **WHEN** `assoc` runs within the allocation budget
- **THEN** it SHALL return the same updated map it returns today
