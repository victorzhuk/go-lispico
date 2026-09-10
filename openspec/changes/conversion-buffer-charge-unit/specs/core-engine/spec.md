## ADDED Requirements

### Requirement: The published charge table names every unit the ledger charges

Every allocation charge SHALL be composed from units the published fixed size table names,
and a reader SHALL be able to determine from that table alone which unit prices a given
piece of storage.

This binds the record, not the ceiling. A charge assembled from named constants still
violates the requirement if the table does not say what that combination prices: the
constants are then owned and the term is not. It is stated separately from the determinism
requirement because a charge can be perfectly reproducible and still unattributable.

Where two paths obtain storage in the same role and with the same lifetime, they SHALL
charge the same header unit for it, whatever the underlying Go type. Role and lifetime are
what the ledger prices — storage a call obtains to build something and discards before
returning is one class whether it is a slice or a map — so a difference in Go type is not by
itself a reason to charge differently. Charging two different units across one role is
permitted only where the published table states the distinction that makes them different
storage, so that the difference is a recorded decision rather than an accident of which
helper was in reach.

#### Scenario: A construction buffer's unit is derivable from the table

- **WHEN** a path obtains scratch storage to construct a value, charges the allocation ledger for it, and a reader consults the published fixed size table
- **THEN** the table SHALL name the unit that charge is composed from and the storage it prices

#### Scenario: Two construction buffers of one role charge one unit

- **WHEN** one call obtains storage to build a value and discards it before returning, and another call in the same evaluator obtains storage in that same role
- **THEN** both SHALL charge the same header term, unless the published table states the distinction that makes the two pieces of storage different

#### Scenario: The ceiling does not loosen

- **WHEN** the unit pricing a construction buffer is changed to satisfy this requirement
- **THEN** the storage the operation actually obtains SHALL still be accounted rather than only the storage it keeps, so that attributing a charge does not become a way of dropping one
