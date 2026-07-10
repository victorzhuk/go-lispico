# dialect — delta

## ADDED Requirements

### Requirement: Dialect renames normalize to canonical kernel forms

A resolved Dialect SHALL expose the mapping from its visible form names to
canonical kernel forms, and compilation SHALL normalize source through that
mapping so the compiler and VM operate only on canonical names. Removed forms
SHALL stay absent — normalization never resurrects a form the Dialect excludes.

#### Scenario: Renamed form compiles to the canonical form

- **WHEN** a Dialect renames `do` to `progn` and `(progn 1 2)` is compiled
- **THEN** the emitted chunk SHALL be equivalent to compiling `(do 1 2)` under the identity Dialect

#### Scenario: Removed form stays removed

- **WHEN** a fail-closed Dialect excludes `set!` and source containing `set!` (under any name) is compiled
- **THEN** compilation SHALL fail with an undefined-form error, not silently normalize to the kernel form
