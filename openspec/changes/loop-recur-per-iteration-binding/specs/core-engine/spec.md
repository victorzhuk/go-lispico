# core-engine — delta

## ADDED Requirements

### Requirement: loop/recur gives per-iteration binding identity for captured variables

When a closure is created inside a `loop` body and captures a loop variable, the
closure SHALL observe the value that variable held at the closure's own
iteration; a subsequent `recur` SHALL NOT change what an earlier iteration's
closure observes. A `recur` that rebinds a captured loop slot SHALL install a
fresh binding cell for that slot, so each iteration's closure holds a distinct
cell. Loop variables that are not captured by any closure MAY continue to use
in-place rebinding and SHALL NOT incur additional allocation. An explicit `set!`
of a captured loop variable within an iteration SHALL still be visible to
closures created in that same iteration (ordinary write-through), independent of
the fresh-cell-per-iteration behavior of `recur`. The tree-walker and the
bytecode compiler SHALL implement this identically.

#### Scenario: Closures in a loop capture per-iteration values

- **WHEN** `(loop [i 0 acc []] (if (< i 3) (recur (+ i 1) (conj acc (fn [] i))) acc))` is evaluated and each returned closure is called
- **THEN** the closures SHALL return `0`, `1`, `2` respectively — not `3`, `3`, `3`

#### Scenario: Both evaluators agree

- **WHEN** the closures-in-loop program is run through the tree-walker and the bytecode compiler
- **THEN** both SHALL produce `(0 1 2)` (crossval parity)

#### Scenario: Non-capturing loops allocate nothing extra

- **WHEN** a `loop` that never captures its loop variables (for example an accumulate-and-sum loop) runs under the allocation gate
- **THEN** its per-iteration allocation SHALL be unchanged from before this change

#### Scenario: set! within an iteration stays visible

- **WHEN** a loop body captures a variable in a closure and also `set!`s it later in the same iteration before creating the closure
- **THEN** the closure SHALL observe the `set!`-updated value for that iteration
