## ADDED Requirements

### Requirement: The published charge table names every unit the ledger charges

Every allocation charge SHALL be composed from units the published fixed size table names,
and a reader SHALL be able to determine from that table alone which unit prices a given
piece of storage.

This binds the record, not the ceiling. A charge assembled from named constants still
violates the requirement if the table does not say what that combination prices: the
constants are then owned and the term is not. It is stated separately from the determinism
requirement because a charge can be perfectly reproducible and still unattributable.

Where two paths obtain storage of the same shape for the same role, they SHALL charge the
same unit for it. Charging two different units for one shape is permitted only where the
table states the distinction that makes them different storage, so that the difference is a
recorded decision rather than an accident of which helper was in reach.

#### Scenario: A construction buffer's unit is derivable from the table

- **WHEN** a path obtains scratch storage to construct a value, charges the allocation ledger for it, and a reader consults the published fixed size table
- **THEN** the table SHALL name the unit that charge is composed from and the storage it prices

#### Scenario: Two construction buffers of one shape charge one unit

- **WHEN** the reader's promoted-map builder and the evaluator's builder-to-trie conversion each obtain construction storage for the same number of key/value pairs
- **THEN** both SHALL charge the same header term, unless the published table states the distinction that makes the two pieces of storage different

#### Scenario: The ceiling does not loosen

- **WHEN** the unit pricing a construction buffer is changed to satisfy this requirement
- **THEN** the storage the operation actually obtains SHALL still be accounted rather than only the storage it keeps, so that attributing a charge does not become a way of dropping one
