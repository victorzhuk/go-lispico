# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Slot-resident locals

The compiler SHALL determine which locals are captured by inner closures; locals
that are not captured SHALL live only in stack slots, with no per-call `Env`
allocation or write-mirroring for them, and their access cost SHALL be
unaffected by this requirement's capture provisions. A captured local SHALL be
cell-resident: one shared storage cell allocated at its binding site, written
through on every mutation, with **no** per-mutation environment mirroring. A
closure SHALL capture direct references to exactly the variables it uses —
not its defining environment chain — and captured-variable semantics SHALL be
unchanged from the tree-walker: every closure over a variable and its defining
scope observe one shared binding, before and after the defining frame returns.

#### Scenario: Uncaptured locals allocate no environment

- **WHEN** a function whose locals are never captured is called in a hot loop under the VM
- **THEN** the call SHALL not allocate an `Env` map for those locals

#### Scenario: Captured variable still works

- **WHEN** a closure captures a local and is called after the defining frame returns
- **THEN** the captured value SHALL be correct, matching the tree-walker

#### Scenario: Mutating a captured local mirrors nothing

- **WHEN** a loop body repeatedly mutates a local that an inner closure captures
- **THEN** each mutation SHALL write the shared cell only, with no environment write and no allocation per mutation

#### Scenario: Sibling closures alias one binding

- **WHEN** two closures capture the same local and one applies `set!` to it
- **THEN** the other closure and the defining scope SHALL observe the new value, matching the tree-walker

#### Scenario: Defining-scope mutation is visible to the closure

- **WHEN** the defining scope mutates a captured local after the closure was created
- **THEN** a subsequent call of the closure SHALL observe the new value, matching the tree-walker

#### Scenario: Loop-iteration capture matches the tree-walker

- **WHEN** a `loop`/`recur` body creates a closure over a loop binding on each iteration
- **THEN** each closure SHALL observe exactly the binding instance the tree-walker gives it (fresh or shared per iteration), verified by cross-validation

#### Scenario: Transitive capture through nested closures

- **WHEN** a closure nested two levels deep references a local of the outermost function
- **THEN** the reference SHALL read and write the same shared binding at every level, matching the tree-walker
