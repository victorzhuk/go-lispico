# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Compiled-chunk cache

The runtime SHALL cache compiled chunks per Engine, keyed by source, dialect, and
macro-definition epoch. A cache hit SHALL skip macro expansion and compilation.
Macro expansion SHALL therefore be performed at most once per cached chunk, not
once per evaluation: an expander body is ordinary evaluated code, so re-running
it to reach a chunk that already embeds its result would repeat any effect it
has. Defining or redefining a macro SHALL invalidate affected entries, so a
stale chunk never runs an outdated expansion. The cache SHALL be bounded: its
entry count SHALL NOT grow without limit over the Engine's lifetime. Entries
orphaned by a macro-epoch bump SHALL be reclaimed, and the cache SHALL enforce
the Engine's configured chunk-cache-size ceiling, so a long-lived Engine that
evaluates many distinct sources or repeatedly redefines macros stays within its
memory budget.

Binding a macro name to a definition indistinguishable from the one already bound
there SHALL NOT count as a redefinition for invalidation: no cached expansion can
differ, so no entry is affected. Indistinguishable means the same name, the same
defining environment, the same parameters and variadic tail, and an equal body.
The comparison SHALL fail closed — where equality cannot be decided, including a
body deeper than the structural-depth bound, the binding SHALL be treated as a
redefinition and invalidate as before. Correctness outranks reuse here: serving a
stale expansion is a defect, recompiling something that need not be recompiled is
only a cost.

Additionally, plugin-load compilation SHALL be reusable across Engines within a
process: loading identical plugin source under an identical dialect fingerprint
into a second Engine SHALL NOT repeat macro expansion and compilation, provided
the source's expansion is fully determined by the dialect and the source itself.
This process-level tier SHALL be bounded, SHALL share only immutable compiled
artifacts, and SHALL NOT share per-engine resolution state: each Engine resolves
globals against its own environments, and no binding, macro, or canonical flag
SHALL leak between Engines through the shared artifacts.

#### Scenario: Repeated evaluation reuses the chunk

- **WHEN** the same source is evaluated twice on one Engine under the VM
- **THEN** the second evaluation SHALL not recompile and SHALL return the same result

#### Scenario: A cache hit does not re-run the expander

- **WHEN** a source using a macro whose expander body has an observable effect is evaluated repeatedly on one Engine under the VM, with the macro unchanged
- **THEN** that effect SHALL be observed once for the compilation, not once per evaluation

#### Scenario: Re-evaluating a source that defines a macro reuses its chunks

- **WHEN** a source containing a `defmacro` is evaluated repeatedly on one Engine under the VM, with the macro's definition unchanged between evaluations
- **THEN** the cache epoch SHALL be the same after every evaluation as after the first, and no form in that source SHALL be recompiled

#### Scenario: Macro redefinition invalidates

- **WHEN** source using macro `m` is evaluated, `m` is redefined, and the same source is evaluated again
- **THEN** the second evaluation SHALL reflect the new definition of `m`

#### Scenario: An identical body in a different scope still invalidates

- **WHEN** a macro name is rebound to a body equal to its current one but closing over a different defining environment
- **THEN** the binding SHALL invalidate as a redefinition, since the expansion it produces may differ

#### Scenario: Cache does not grow without bound

- **WHEN** an Engine repeatedly evaluates distinct sources and redefines macros far beyond the chunk-cache-size ceiling
- **THEN** the cache entry count SHALL stay at or below the configured ceiling, and results SHALL remain correct for whatever is evaluated next

#### Scenario: Second engine skips plugin recompilation

- **WHEN** a second Engine with the same dialect loads the same stdlib plugin source in one process
- **THEN** the load SHALL reuse the process-level compiled artifacts without repeating expansion or compilation, and every stdlib definition SHALL behave identically to a freshly compiled load

#### Scenario: Shared artifacts leak no engine state

- **WHEN** two Engines built from shared plugin artifacts each define new bindings and one unloads the plugin
- **THEN** neither Engine SHALL observe the other's bindings or unload, and `go test -race` SHALL report no data race across concurrent engine construction
