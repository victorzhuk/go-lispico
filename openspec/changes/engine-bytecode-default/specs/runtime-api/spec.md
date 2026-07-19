# runtime-api — delta

## ADDED Requirements

### Requirement: Default evaluator selection

An Engine constructed without evaluator options SHALL run the bytecode
evaluator with per-form tree-walker fallback. `WithTreeWalker()` SHALL select
the tree-walking evaluator as the sole execution path; `WithBytecode()` SHALL
select the bytecode evaluator explicitly. When both options are passed, the
last one in argument order SHALL win. Both evaluators SHALL remain available
and fully supported; selecting either SHALL NOT change any evaluation result,
per the VM parity contract.

#### Scenario: Default is bytecode

- **WHEN** an Engine is constructed via `runtime.New(nil)` with no evaluator option
- **THEN** compiled-subset forms SHALL execute on the bytecode VM

#### Scenario: Tree-walker opt-out

- **WHEN** an Engine is constructed with `WithTreeWalker()`
- **THEN** all evaluation SHALL run on the tree-walking evaluator and the bytecode VM SHALL NOT be entered

#### Scenario: Last option wins

- **WHEN** an Engine is constructed with `WithTreeWalker()` followed by `WithBytecode()`, and another with the reverse order
- **THEN** the first SHALL run the bytecode evaluator and the second the tree-walker

#### Scenario: Results identical across evaluators

- **WHEN** the same program runs on a default Engine and on a `WithTreeWalker()` Engine
- **THEN** results and error shapes SHALL be identical
