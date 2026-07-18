# core-engine — delta

## ADDED Requirements

### Requirement: Global binding cells

`Env` SHALL expose a stable binding cell per bound name: the cell created when a
name is first bound in a scope SHALL remain the write-through target for every
later rebind of that name in that scope, so a holder of the cell observes rebinds
and deletions without re-walking the scope chain. Cell reads SHALL be safe under
concurrent binds, guarded by a short read lock (not the full chain walk),
preserving the concurrent evaluation safety requirement.

#### Scenario: Cell observes rebind

- **WHEN** a caller resolves the cell for a bound name and the name is subsequently rebound in the same scope
- **THEN** reading through the held cell SHALL return the new value

#### Scenario: Cell observes deletion

- **WHEN** a caller resolves the cell for a bound name and the name is subsequently deleted from that scope
- **THEN** reading through the held cell SHALL report the name unbound rather than returning the stale value

#### Scenario: Reads race-free with writes

- **WHEN** goroutines read through held cells while another goroutine rebinds the same names
- **THEN** each read SHALL return either the prior or the new value and `go test -race` SHALL report no data race
