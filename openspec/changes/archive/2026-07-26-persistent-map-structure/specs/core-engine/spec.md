# core-engine — delta

## MODIFIED Requirements

### Requirement: Map representation efficiency

`HashMap` SHALL keep its public semantics — immutable operations, key domain,
`Int`/`Float` key distinctness, deterministic iteration — while meeting
efficiency bounds: a map operation SHALL NOT format a key into a string;
iterating a map SHALL NOT allocate or re-sort per call for maps at or below the
small-map threshold; and constructing, reading, or copying a small map SHALL
allocate O(1) objects. Promotion between the small and large representations
SHALL be semantically invisible: equality, iteration order rules, printing, and
immutability are identical at both representations.

Above the small-map threshold an immutable update SHALL NOT copy the whole map:
`Assoc` and `Dissoc` SHALL share the untouched majority of the structure with the
receiver, so that the storage a single update allocates is bounded by the depth of
the structure rather than by its entry count, and extending a map n times costs
O(n log n) in total rather than O(n²). The receiver SHALL be unaffected by an
update derived from it.

The hash backing that structure SHALL be derived from fixed constants rather than
a per-process random seed. A randomized seed would make the structure's shape, and
anything derived from it, differ across restarts for identical input, which
contradicts the determinism this requirement states.

#### Scenario: Small-map operations are allocation-bounded

- **WHEN** a map literal with at most the threshold number of keys is built, read with `Get`, extended with `Assoc`, and iterated
- **THEN** `Get` and iteration SHALL allocate nothing and `Assoc` SHALL allocate only the new map's storage

#### Scenario: Numeric keys never format

- **WHEN** `Get`, `Set`, `Assoc`, or `Dissoc` runs with an `Int` or `Float` key
- **THEN** the operation SHALL NOT allocate a formatted string representation of the key

#### Scenario: Promotion is invisible

- **WHEN** a map grows past the small-map threshold via `Assoc` and later shrinks via `Dissoc`
- **THEN** equality with a same-pairs map, iteration determinism, and immutability SHALL hold identically before and after promotion

#### Scenario: Iteration order is deterministic

- **WHEN** the same map value is iterated or printed repeatedly, at either representation
- **THEN** the order SHALL be identical on every iteration and identical across both evaluators

#### Scenario: Extending a large map does not copy it

- **WHEN** a map above the small-map threshold is extended by `Assoc` at sizes spanning two orders of magnitude
- **THEN** the bytes and allocations a single call charges SHALL stay bounded as the map grows rather than rising in proportion to its entry count, and the receiver SHALL remain unchanged and independently readable

#### Scenario: Colliding keys stay retrievable

- **WHEN** a large map holds distinct keys whose hashes agree in every bit position the structure discriminates on
- **THEN** each key SHALL resolve to its own value, `Dissoc` of one SHALL leave the others intact, and `Len` SHALL count them separately

#### Scenario: Structure shape does not vary across processes

- **WHEN** the same sequence of map operations runs in separate processes
- **THEN** the resulting map SHALL print identically and iterate identically in every run
