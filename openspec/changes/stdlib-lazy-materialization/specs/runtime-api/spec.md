# runtime-api — delta

## ADDED Requirements

### Requirement: Deferred plugin binding materialization

Plugin load MAY defer binding creation behind a process-level template: names
materialize into the engine's root environment on first resolution — value
cell, function cell, or macro lookup — with results, errors, canonical-operator
status, and macro expansion byte-identical to an eagerly loaded engine.
Materialization SHALL be at-most-once per name per engine and safe under
concurrent first resolution, including definitions whose execution resolves
other unmaterialized names. Surfaces that enumerate bindings SHALL observe the
full plugin surface (forcing materialization as needed). `UnloadPlugin` SHALL
remove both the deferred template and every binding it materialized. A user
definition shadowing a deferred name SHALL win exactly as it wins over an
eager binding, and deleting bindings SHALL behave as it would on an eagerly
loaded engine — deferral SHALL never resurrect a deleted name.

#### Scenario: First use is identical to eager load

- **WHEN** a fresh engine with deferred stdlib evaluates a program using arithmetic, collection functions, and a stdlib macro
- **THEN** results, errors, and native-operator execution SHALL be identical to the same engine loaded eagerly

#### Scenario: Concurrent first touch materializes once

- **WHEN** many goroutines concurrently first-resolve the same deferred name and names whose definitions depend on other deferred names
- **THEN** each name SHALL materialize exactly once, no evaluation SHALL deadlock, and `go test -race` SHALL report no data race

#### Scenario: Enumeration sees the full surface

- **WHEN** a surface that lists plugin bindings runs on an engine where nothing has been resolved yet
- **THEN** it SHALL report the same bindings an eagerly loaded engine reports

#### Scenario: Unload removes deferred and materialized bindings

- **WHEN** `UnloadPlugin` runs after some deferred names were materialized and others were not
- **THEN** neither materialized nor unmaterialized plugin names SHALL resolve afterwards

#### Scenario: Deletion is not undone by deferral

- **WHEN** a deferred stdlib name is shadowed by a user definition which is then deleted
- **THEN** subsequent resolution SHALL behave exactly as on an eagerly loaded engine after the same operations
