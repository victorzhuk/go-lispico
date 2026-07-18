# bytecode-vm — delta

## ADDED Requirements

### Requirement: Kernel let binding scope parity

The VM SHALL bind kernel `let` in parallel: every binding init expression SHALL
resolve names in the scope enclosing the `let`, never in bindings introduced by
the same vector — matching the tree-walking evaluator. Kernel `let*` SHALL
remain sequential: each init resolves bindings introduced earlier in the same
vector. A binding name that shadows an enclosing binding SHALL not be visible
to any sibling init in the same `let` vector.

#### Scenario: let init sees the enclosing binding, not its sibling

- **WHEN** the VM evaluates `(def a 10) (let [a 1 b a] b)`
- **THEN** the result SHALL be `10`, equal to the tree-walking evaluator's result

#### Scenario: let* init sees the earlier sibling

- **WHEN** the VM evaluates `(def a 10) (let* [a 1 b a] b)`
- **THEN** the result SHALL be `1`, equal to the tree-walking evaluator's result
