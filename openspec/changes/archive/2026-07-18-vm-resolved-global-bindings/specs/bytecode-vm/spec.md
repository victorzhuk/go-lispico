# bytecode-vm — delta

## ADDED Requirements

### Requirement: Resolved global bindings

Repeated execution of a compiled chunk SHALL NOT re-resolve a global name through
a locked map walk on every read. A call site's resolution MAY be cached on the
chunk, guarded so that a chunk running against a different environment, or after
a new name is bound into the resolution environment, resolves afresh. Rebinding
an already-bound global SHALL be visible to subsequent reads through any cached
resolution, and concurrent execution with concurrent binds SHALL stay race-free
per the concurrency-safety requirement.

#### Scenario: Rebind visible through a cached resolution

- **WHEN** a chunk reads global `f`, then the program rebinds `f`, then the same cached chunk executes again
- **THEN** the second execution SHALL observe the new binding, matching the tree-walker

#### Scenario: Shared chunk across environments

- **WHEN** one cached chunk executes against two engines with different root environments
- **THEN** each execution SHALL resolve globals in its own environment, with no cross-engine value leakage

#### Scenario: Concurrent bind and execute

- **WHEN** one goroutine rebinds a global while others execute chunks reading it on the same engine
- **THEN** each execution SHALL observe either the old or the new binding and `go test -race` SHALL report no data race

## MODIFIED Requirements

### Requirement: Native arithmetic and comparison opcodes

The VM SHALL execute `+`, `-`, `*`, `/`, `<`, `>`, `<=`, `>=`, `=` through
dedicated opcodes operating on stack slots, with semantics identical to the stdlib
builtins including int/float promotion and division-by-zero errors. When the
operator symbol is locally shadowed or its global binding is no longer the
canonical stdlib builtin, execution SHALL fall back to the ordinary call path.
Canonical status SHALL be determined through the operator's resolved binding, not
re-derived by a per-execution environment walk, and a canonical operator SHALL
take the native path on every execution — not intermittently.

#### Scenario: Hot loop avoids builtin dispatch

- **WHEN** a `loop` body evaluates `(+ acc 1)` under the VM
- **THEN** the addition SHALL execute as an opcode without a `GoFunc` invocation, and the loop result SHALL equal the tree-walker's

#### Scenario: Promotion parity

- **WHEN** `(+ 1 2.5)` and `(< 1 1.5)` run under the VM
- **THEN** results SHALL equal the stdlib builtins' results (`3.5`, `true`)

#### Scenario: Rebound operator falls back

- **WHEN** a program rebinds `+` to a custom function and then calls `(+ 1 2)` under the VM
- **THEN** the custom function SHALL be called, matching tree-walker behavior

#### Scenario: Recursive calls keep the native path

- **WHEN** a recursive function's body applies canonical `+`, `-`, and `<` across nested self-calls under the VM
- **THEN** every application SHALL execute as a native opcode, with no fallback to `GoFunc` dispatch for canonical bindings
