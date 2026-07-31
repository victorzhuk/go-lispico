# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Bytecode VM execution

The bytecode VM SHALL execute validated chunks and MAY support more than one
validated instruction form: the existing stack form, and a register form in
which function-body instructions address a per-frame register window with
three-address operands. A chunk SHALL declare its form; chunks of different
forms SHALL interoperate within one program through the ordinary call
protocol. The compiler SHALL emit the register form only for shapes its
allocator fully covers, keeping stack-form emission (and the existing
tree-walker fallback) for everything else, so partial coverage is never
observable as a behavior change. Register-form chunks SHALL be validated at
load — register indices, window bounds, and jump targets — preserving the
invariant that the dispatch loop performs no per-instruction bounds checks.
Every cross-cutting contract SHALL apply to both forms identically: batched
cancellation observation, allocation-ledger charging, operator canonicality
freezing with non-canonical fallback, re-entrant evaluation state, resource
limits, and tree-walker result parity.

#### Scenario: Register-form execution matches the tree-walker

- **WHEN** the cross-validation suite runs a program whose functions compile to the register form
- **THEN** results and error shapes SHALL be identical to the tree-walking evaluator

#### Scenario: Mixed-form programs interoperate

- **WHEN** a register-form function calls a stack-form function and vice versa within one evaluation
- **THEN** arguments, results, throws, and resource accounting SHALL flow across the boundary exactly as between two stack-form functions

#### Scenario: Uncovered shapes keep the stack form

- **WHEN** a function uses a shape the register allocator does not cover
- **THEN** it SHALL compile to the stack form (or fall back to the tree-walker exactly as today) with unchanged semantics

#### Scenario: Register-form chunks are validated

- **WHEN** a register-form chunk with an out-of-window register index or invalid jump target is loaded
- **THEN** `Validate` SHALL reject it before execution
