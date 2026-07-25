# stdlib-plugin — delta

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

`dissoc` SHALL follow the same incremental rule for the storage its result
allocates.

#### Scenario: assoc with large nested values is charged for its real size

- **WHEN** `assoc` inserts a large nested value into a map under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the inserted value plus the map storage the operation allocated, not the map's shallow entry count

#### Scenario: Chained assoc does not re-charge the shared map

- **WHEN** `assoc` is applied repeatedly to the result of the previous `assoc` in a loop
- **THEN** the total charge SHALL grow with the values and nodes added, not with the accumulated map size per iteration

#### Scenario: Map accumulation completes under the default ledger

- **WHEN** a loop `assoc`s 100,000 distinct keys into an accumulating map with default resource limits
- **THEN** it SHALL return the accumulated map rather than a `ResourceLimitError`

#### Scenario: Over-budget assoc fails closed

- **WHEN** an `assoc` result's newly allocated storage would exceed the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` rather than an uncharged large map

#### Scenario: Ordinary assoc is unchanged

- **WHEN** `assoc` runs within the allocation budget
- **THEN** it SHALL return the same updated map it returns today
