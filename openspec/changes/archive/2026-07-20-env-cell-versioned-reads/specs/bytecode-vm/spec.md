# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Resolved global bindings

Repeated execution of a compiled chunk SHALL NOT re-resolve a global name through
a locked map walk on every read. A call site's resolution MAY be cached on the
chunk, guarded so that a chunk running against a different environment, or after
a new name is bound into the resolution environment, resolves afresh. Reading a
global through a cached site whose binding has not been written since resolution
SHALL take no lock and SHALL allocate nothing. Rebinding an already-bound global
SHALL be visible to subsequent reads through any cached resolution, deleting it
SHALL make subsequent reads report it undefined, and concurrent execution with
concurrent binds SHALL stay race-free per the concurrency-safety requirement.
Neither the read path nor the write path of a binding SHALL allocate per
operation on account of the cache.

#### Scenario: Rebind visible through a cached resolution

- **WHEN** a chunk reads global `f`, then the program rebinds `f`, then the same cached chunk executes again
- **THEN** the second execution SHALL observe the new binding, matching the tree-walker

#### Scenario: Delete visible through a cached resolution

- **WHEN** a chunk reads global `f` through a warmed cached site, then `f` is deleted, then the same chunk executes again
- **THEN** the second execution SHALL report `f` undefined, matching the tree-walker

#### Scenario: Shared chunk across environments

- **WHEN** one cached chunk executes against two engines with different root environments
- **THEN** each execution SHALL resolve globals in its own environment, with no cross-engine value leakage

#### Scenario: Concurrent bind and execute

- **WHEN** one goroutine rebinds a global while others execute chunks reading it on the same engine
- **THEN** each execution SHALL observe either the old or the new binding and `go test -race` SHALL report no data race

#### Scenario: Stable global reads are lock- and allocation-free

- **WHEN** a cached chunk repeatedly reads a global that is never rebound after the chunk warmed up
- **THEN** those reads SHALL acquire no lock and allocate nothing
