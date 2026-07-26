# runtime-api — delta

## MODIFIED Requirements

### Requirement: Deferred plugin binding materialization

Plugin load MAY defer binding creation behind a process-level template: names
materialize into the engine's root environment on first resolution — value
cell, function cell, or macro lookup — with results, errors, canonical-operator
status, and macro expansion byte-identical to an eagerly loaded engine.
Materialization SHALL be at-most-once per name per engine and safe under
concurrent first resolution, including definitions whose execution resolves
other unmaterialized names. Surfaces that enumerate bindings SHALL observe the
full plugin surface (forcing materialization as needed). `UnloadPlugin` SHALL
remove both the engine's deferred template attachment and every binding it
materialized, while leaving the process-level layer available to other
engines. A user definition shadowing a deferred name SHALL win exactly as it
wins over an eager binding, and deleting bindings SHALL behave as it would on
an eagerly loaded engine — deferral SHALL never resurrect a deleted name.

Template construction SHALL additionally be at-most-once per process for a
given dialect fingerprint and plugin identity (name and version): when a
completed template layer already exists for that key, loading the plugin into
a further engine SHALL NOT re-run the plugin's `Init` and SHALL NOT rebuild
its builtin function values — the engine attaches the existing immutable
layer. Concurrent first loads of one key SHALL construct the layer exactly
once. A failed `Init` SHALL NOT mark the layer complete; a later load SHALL
retry construction. This sharing SHALL apply only to plugins whose
registration routes through the process-level template; a plugin that writes
directly into the engine environment SHALL keep per-engine `Init`. Plugins
with the same name but different identity/version SHALL NOT share a layer.

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
- **THEN** neither materialized nor unmaterialized plugin names SHALL resolve afterwards, and a different engine sharing the same template layer SHALL be unaffected

#### Scenario: Deletion is not undone by deferral

- **WHEN** a deferred stdlib name is shadowed by a user definition which is then deleted
- **THEN** subsequent resolution SHALL behave exactly as on an eagerly loaded engine after the same operations

#### Scenario: Second engine attaches without re-running Init

- **WHEN** a second engine with an identical dialect fingerprint loads the same plugin identity in one process
- **THEN** the plugin's `Init` SHALL NOT run again, no builtin function values SHALL be rebuilt, and the second engine's evaluation behavior SHALL be identical to the first's

#### Scenario: Concurrent first loads build one layer

- **WHEN** many engines with one dialect fingerprint concurrently load the same plugin in a fresh process
- **THEN** the template layer SHALL be constructed exactly once, every load SHALL succeed, and `go test -race` SHALL report no data race
