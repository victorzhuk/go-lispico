## REMOVED Requirements

### Requirement: Dialect-axis execution

## ADDED Requirements

### Requirement: Dialect-driven execution

The VM SHALL honor the Engine's dialect: form names normalized to canonical kernel
forms before compilation, truthiness decided by the fixed uniform rule, `nil` and `false` falsy,
head-position symbol resolution through the function cell under Lisp-2, and special
forms with a dialect-owned Form-shape rule (`cond` clause shape first) compiled from
the same canonical structure the Evaluator dispatches on. Any resolvable dialect
SHALL be VM-eligible.

#### Scenario: CL dialect runs on the VM

- **WHEN** an Engine is created with the default CL dialect and `WithBytecode()`, and evaluates `(progn (setq x 1) (if nil 2 x))`
- **THEN** construction SHALL succeed and the result SHALL be `1`, matching the tree-walker

#### Scenario: Restricted dialect runs on the VM

- **WHEN** a fail-closed dialect built from the empty base with a form subset runs a program using only its forms under the VM
- **THEN** the program SHALL evaluate correctly, and forms outside the subset SHALL remain undefined

#### Scenario: Both cond clause shapes compile

- **WHEN** a Clojure-dialect Engine compiles a flat-pair `cond` and a CL-dialect Engine compiles a nested-clause `cond` under `WithBytecode()`
- **THEN** both SHALL compile from the dialect's canonical clauses and return results equal to the tree-walker's

## MODIFIED Requirements

### Requirement: Per-Engine compiled-chunk cache

The runtime SHALL cache compiled chunks per Engine, keyed by source, form index, and
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

The cache's internal locking granularity is an implementation choice: it MAY be
a single mutex guarding one map and LRU, or MAY be sharded (striped by a hash of
the cache key) to reduce contention under concurrent evaluation on one shared
Engine. Whichever granularity is used, every correctness and bound invariant
above SHALL hold in aggregate across the whole cache, not merely within one
shard: a chunk-cache-size ceiling configured on the Engine SHALL bound the
cache's total entries, bytes, and nodes across all internal partitions, and
concurrent evaluation on a shared Engine SHALL observe the same hit/miss and
invalidation behavior as single-goroutine use, verified under `-race`.

Compiled stdlib bootstrap artifacts SHALL NOT be cached or shared across Engines
at process scope. Trusted Lisp-source definitions SHALL be evaluated for their
own target environment, and the runtime SHALL keep no process-global bootstrap
artifact map, statistics, or cache-control hooks.

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

#### Scenario: Bootstrap source is environment-owned

- **WHEN** two Engines with the same Dialect load identical stdlib bootstrap source
- **THEN** each SHALL define that source for its own environment without reading or writing a process-level compiled bootstrap artifact

#### Scenario: A sharded cache enforces its budget in aggregate

- **WHEN** the chunk cache is implemented as multiple internally-locked partitions and concurrent evaluations insert entries across several partitions at once
- **THEN** the Engine's configured `MaxCacheBytes` and `MaxCacheNodes` ceilings SHALL bound the total across all partitions, not merely each partition independently

#### Scenario: Concurrent access is race-free regardless of locking granularity

- **WHEN** multiple goroutines call `EvalCached` concurrently on one shared Engine, whether the cache is single-locked or sharded
- **THEN** `go test -race` SHALL report no data race, and hit/miss/invalidation behavior SHALL match single-goroutine use for the same sequence of operations
