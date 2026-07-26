# bytecode-vm — delta

## ADDED Requirements

### Requirement: All-constant collection literals compile to shared constants

A collection literal — `Vector`, `HashMap`, or list literal — whose elements
(recursively, including map keys and values) are all compile-time constants
SHALL compile to a single reference into the chunk constant pool, built once
at compile time, rather than to per-execution element pushes and construction
opcodes. Nested all-constant literals SHALL fold into their parent. A literal
containing any non-constant element SHALL compile exactly as before. Repeated
execution SHALL return the shared constant value; this is unobservable
in-language because the value is immutable and comparison is by `Equals`.

Resource enforcement SHALL be preserved by precomputation, not skipped: the
folded constant's deep bytes and structural depth SHALL be computed once at
compile time, and each execution SHALL charge the per-evaluation allocation
ledger by the precomputed bytes and check the precomputed depth against the
running engine's `MaxStructuralDepth` in O(1), raising the same terminal
`ResourceLimitError` as the construction path. The allocation ledger SHALL
therefore observe the same charges under the bytecode VM as under the
tree-walking evaluator for the same program. No engine-specific limit SHALL
be baked into the compiled chunk. The folded constant SHALL be covered by the
chunk's compile-time retained-bytes accounting, and chunk validation SHALL
verify the new instruction's operands before the chunk runs.

#### Scenario: Rule-shaped literal stops allocating per call

- **WHEN** a compiled function returning `{:model :large :tools [:read :grep]}` is called repeatedly on a bytecode engine
- **THEN** per-call allocations SHALL NOT include construction of the map or its nested vector, and the returned value SHALL equal the tree-walker's result

#### Scenario: Mixed literals still construct

- **WHEN** a function returning `{:model m}` (a non-constant element) is called
- **THEN** the literal SHALL be constructed per execution exactly as before this change

#### Scenario: Allocation ledger is evaluator-independent

- **WHEN** a program whose folded literals exceed a small configured `MaxAllocationBytes` runs under the bytecode VM and under the tree-walker
- **THEN** both SHALL fail with the same terminal `ResourceLimitError`

#### Scenario: Depth limit still enforced on folded constants

- **WHEN** a folded literal's structural depth exceeds the engine's configured `MaxStructuralDepth`
- **THEN** execution SHALL fail with the same terminal `ResourceLimitError` the construction path raises, under both evaluators
