# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Dialect-axis execution

The VM SHALL honor the Engine's dialect: form names normalized to canonical kernel
forms before compilation, truthiness decided through the dialect's truthiness rule,
head-position symbol resolution through the function cell under Lisp-2, and special
forms with a dialect-owned Form-shape rule (`cond` clause shape first) compiled from
the same canonical structure the Evaluator dispatches on. Any resolvable dialect
SHALL be VM-eligible.

#### Scenario: CL dialect runs on the VM

- **WHEN** an Engine is created with the default CL dialect and `WithBytecode()`, and evaluates `(progn (setq x 1) (if nil 2 x))`
- **THEN** construction SHALL succeed and the result SHALL be `1`, matching the tree-walker

#### Scenario: Truthiness axis honored

- **WHEN** a nil-only-falsy dialect evaluates `(if false 1 2)` under the VM
- **THEN** the result SHALL be `1`, because `false` is truthy on that axis

#### Scenario: Both cond clause shapes compile

- **WHEN** a Clojure-dialect Engine compiles a flat-pair `cond` and a CL-dialect Engine compiles a nested-clause `cond` under `WithBytecode()`
- **THEN** both SHALL compile from the dialect's canonical clauses and return results equal to the tree-walker's
