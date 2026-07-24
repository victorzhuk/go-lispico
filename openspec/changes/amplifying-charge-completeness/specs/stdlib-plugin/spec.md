# stdlib-plugin — delta

## ADDED Requirements

### Requirement: assoc charges constructed allocation deeply

`assoc` SHALL charge the evaluation allocation ledger for its constructed result
using the same deep-bytes charge and length check applied to `conj`'s map branch
and `merge`, rather than relying on the shallow post-return `GoFunc` result
charge. A chained or large-valued `assoc` whose result exceeds the remaining
`MaxAllocationBytes` SHALL fail with a `ResourceLimitError`. Ordinary `assoc`
within budget SHALL return the updated map unchanged in value.

#### Scenario: assoc with large nested values is charged for its real size

- **WHEN** `assoc` inserts a large nested value into a map under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the result, not the map's shallow entry count

#### Scenario: Over-budget assoc fails closed

- **WHEN** an `assoc` result would exceed the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` rather than an uncharged large map

#### Scenario: Ordinary assoc is unchanged

- **WHEN** `assoc` runs within the allocation budget
- **THEN** it SHALL return the same updated map it returns today
