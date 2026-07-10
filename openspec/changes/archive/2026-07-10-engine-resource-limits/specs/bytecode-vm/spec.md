# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Compiled-chunk cache

The runtime SHALL cache compiled chunks per Engine, keyed by source, dialect, and
macro-definition epoch. A cache hit SHALL skip macro expansion and compilation.
Defining or redefining a macro SHALL invalidate affected entries, so a stale chunk
never runs an outdated expansion. The cache SHALL be bounded: its entry count SHALL
NOT grow without limit over the Engine's lifetime. Entries orphaned by a macro-epoch
bump SHALL be reclaimed, and the cache SHALL enforce the Engine's configured
chunk-cache-size ceiling, so a long-lived Engine that evaluates many distinct
sources or repeatedly redefines macros stays within its memory budget.

#### Scenario: Repeated evaluation reuses the chunk

- **WHEN** the same source is evaluated twice on one Engine under the VM
- **THEN** the second evaluation SHALL not recompile and SHALL return the same result

#### Scenario: Macro redefinition invalidates

- **WHEN** source using macro `m` is evaluated, `m` is redefined, and the same source is evaluated again
- **THEN** the second evaluation SHALL reflect the new definition of `m`

#### Scenario: Cache does not grow without bound

- **WHEN** an Engine repeatedly evaluates distinct sources and redefines macros far beyond the chunk-cache-size ceiling
- **THEN** the cache entry count SHALL stay at or below the configured ceiling, and results SHALL remain correct for whatever is evaluated next
