# core-engine — delta

## ADDED Requirements

### Requirement: Tree-walker batched cancellation observation

The tree-walking evaluator SHALL observe context cancellation and the engine
evaluation deadline on the same bounded budget as the bytecode VM: at most a
fixed node budget apart, and unconditionally at every `apply` trampoline
iteration, so loops and recursion observe cancellation within one iteration or
call. Error shape SHALL be unchanged.

#### Scenario: Tree-walker loop observes cancellation

- **WHEN** the caller's context is cancelled while a `loop`/`recur` body iterates on the tree-walker
- **THEN** evaluation SHALL stop with a context error no later than the next trampoline iteration

#### Scenario: Tree-walker straight-line budget

- **WHEN** the caller's context is cancelled during evaluation of a long form sequence on the tree-walker
- **THEN** evaluation SHALL stop with a context error within the fixed node budget
