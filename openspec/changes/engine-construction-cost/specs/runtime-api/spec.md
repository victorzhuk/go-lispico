# runtime-api — delta

## ADDED Requirements

### Requirement: Engine construction efficiency

Constructing an engine SHALL NOT allocate state whose content is identical for
every engine, and SHALL NOT eagerly allocate per-engine bookkeeping that an
engine only needs once a deferred-template plugin is loaded.

When `New` is given no logger, the engine SHALL use a discard logger obtained
once per process rather than constructing one per engine; the resulting logging
behavior SHALL be indistinguishable from constructing one per engine. When `New`
is given a logger, that logger SHALL be used exactly as supplied, SHALL NOT be
replaced or wrapped, and SHALL remain independent of any other engine's logger.

Per-engine deferred-materialization bookkeeping MAY be created on first write
instead of at construction. Whichever way it is created, it SHALL remain
per-engine: materializing, shadowing, deleting, or unloading on one engine SHALL
NOT be observable on another, and concurrent first writes SHALL be safe under
`go test -race`.

#### Scenario: Repeated construction shares no per-engine identity

- **WHEN** many engines are constructed in one process with no logger supplied
- **THEN** each SHALL evaluate and log exactly as an engine constructed with its own discard logger would, and no engine's behavior SHALL depend on another's

#### Scenario: A supplied logger is used as given

- **WHEN** two engines are constructed with two different loggers
- **THEN** each SHALL emit through the logger it was given, and neither SHALL emit through the other's

#### Scenario: An engine that loads no plugin allocates no template bookkeeping

- **WHEN** an engine is constructed and closed without loading any plugin
- **THEN** it SHALL behave exactly as before this requirement, and the deferred-materialization maps it never used SHALL not have been allocated

#### Scenario: Lazily created bookkeeping is race-free

- **WHEN** many goroutines concurrently trigger the first materialization on one freshly constructed engine
- **THEN** every name SHALL materialize exactly once, no evaluation SHALL panic on an uninitialized map, and `go test -race` SHALL report no data race
