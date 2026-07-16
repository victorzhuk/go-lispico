# stdlib-plugin — delta

## ADDED Requirements

### Requirement: merge builds its result in linear cost

`merge` SHALL construct its fresh result map without copying the accumulated map
per key: allocated bytes and allocation count SHALL grow roughly linearly with the
total number of entries merged, not quadratically. Its observable semantics SHALL
be unchanged: input maps stay immutable, iteration of the result stays
deterministic, the right-most map wins on duplicate keys, `(merge)` and nil
arguments keep their current behavior, and non-map arguments keep the existing
type error.

#### Scenario: Semantics preserved

- **WHEN** `merge` is called with zero maps, nil arguments, overlapping keys, and a non-map argument
- **THEN** results and errors SHALL be identical to the prior implementation, and the input maps SHALL be unchanged

#### Scenario: Growth is no longer quadratic

- **WHEN** `merge` is benchmarked over increasing map sizes
- **THEN** `B/op` and `allocs/op` SHALL grow roughly linearly with entry count
