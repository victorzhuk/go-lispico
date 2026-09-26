## ADDED Requirements

### Requirement: Dialect accessors never expose mutable state

No exported accessor of a Dialect SHALL return a map, slice or pointer through which a caller can change the Dialect or any other value sharing its state. Modifying a value returned by an accessor SHALL NOT change the Dialect's behavior, its fingerprint, or any engine built from it.

#### Scenario: Mutating the returned vocabulary does not leak

- **WHEN** a caller writes into the map returned by `cl.Dialect().Vocab()`
- **THEN** a later `cl.Dialect().Vocab()` SHALL return the original entries and an engine built afterwards SHALL resolve `car` to `first`

#### Scenario: Policy allowlist cannot be widened after construction

- **WHEN** a caller adds a name to the map returned by an empty-base Dialect's `Vocab()`
- **THEN** an engine built from that Dialect SHALL NOT make the added name callable

### Requirement: Dialect fingerprint is a process-local, collision-free identity

`Fingerprint()` SHALL encode every input unambiguously, so Dialects that differ in any fingerprinted field SHALL produce different digests regardless of the characters their names contain. It SHALL identify a Dialect within one process running one go-lispico version and SHALL NOT be a persistence or interchange format: its digest MAY change between releases.

#### Scenario: Separator characters do not collide

- **WHEN** one Dialect maps `a:b` to `c` and another maps `a` to `b:c`
- **THEN** their fingerprints SHALL differ

#### Scenario: Structurally identical Dialects agree

- **WHEN** two structurally identical Dialects are built in one process
- **THEN** their fingerprints SHALL be equal
