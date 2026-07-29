# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Resolved global bindings

Repeated execution of a compiled chunk SHALL NOT re-resolve a global name
through a locked map walk on every read — in the value namespace and, on a
Lisp-2 dialect, in the function namespace alike. A call site's resolution MAY
be cached on the chunk, guarded so that a chunk running against a different
environment, or after a new name is bound into the resolution environment,
resolves afresh. The two namespaces SHALL resolve through distinct cached
entries even for the same symbol in the same chunk. Reading a global through
a cached site whose binding has not been written since resolution SHALL take
no lock and SHALL allocate nothing. Rebinding an already-bound global —
including a Lisp-2 function rebind that clears or restores a canonical
operator marking — SHALL be visible to subsequent reads through any cached
resolution, deleting it SHALL make subsequent reads report it undefined, and
concurrent execution with concurrent binds SHALL stay race-free per the
concurrency-safety requirement. Neither the read path nor the write path of
a binding SHALL allocate per operation on account of the cache.

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

#### Scenario: Function-namespace head resolution is cached

- **WHEN** a Lisp-2 chunk repeatedly calls a function whose binding is never rewritten after warm-up
- **THEN** head resolutions SHALL acquire no lock and allocate nothing, and results SHALL match the tree-walker

#### Scenario: Defun rebind of a canonical operator through a warmed site

- **WHEN** a warmed Lisp-2 chunk calls a canonical operator head, then the program rebinds it with `defun`, then the chunk executes again
- **THEN** the second execution SHALL call the user definition (not native operator semantics), and restoring the canonical binding SHALL restore native semantics, both matching the tree-walker

#### Scenario: Function cell dropped by compaction and rebound

- **WHEN** a warmed function head is deleted, the environment is compacted (`Rebuild`), and the name is then bound to a new function cell
- **THEN** the next execution through the previously warmed site SHALL resolve the new binding, matching the tree-walker
