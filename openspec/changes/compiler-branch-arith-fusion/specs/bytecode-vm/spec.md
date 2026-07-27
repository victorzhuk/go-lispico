# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Native arithmetic and comparison opcodes

The compiler SHALL continue to emit native opcodes for canonical arithmetic
and comparison operators, and the VM SHALL continue to freeze each operator
site's canonicality before its argument values are produced, falling back to
generic operator application whenever the site is non-canonical — a program
that rebinds an operator SHALL observe results identical to the tree-walker.

In addition, the compiler MAY fuse adjacent shapes into single instructions
when the operator site qualifies for native emission:

- a native comparison whose boolean result feeds a conditional branch SHALL
  be emittable as one fused compare-and-branch instruction;
- a native arithmetic op whose operands are a local slot and a constant (or
  two local slots) SHALL be emittable as one fused instruction that reads
  its operands directly and pushes the result.

A fused instruction SHALL preserve, bit-for-bit, the semantics of the
sequence it replaces: the same canonicality freeze point, the same
non-canonical fallback, the same numeric edge behavior (division by zero,
float promotion), and the same allocation-ledger charge as the unfused fused
native op. Shapes not covered by a fusion SHALL compile exactly as before.
Chunk validation SHALL verify every fused instruction's operand indices and
branch targets before the chunk runs, preserving the validated-chunk
invariant that the dispatch loop performs no per-instruction bounds checks.

#### Scenario: Fused compare-branch matches unfused semantics

- **WHEN** `(if (< n 2) a b)` executes with `<` canonical, and separately after `<` has been rebound by user code
- **THEN** the result SHALL be identical to the tree-walker in both cases — the fused instruction takes the native path only under the same conditions the unfused sequence would

#### Scenario: Fused arithmetic matches unfused semantics

- **WHEN** `(- n 1)` compiles to a fused local/const instruction and executes, including division-by-zero and float-operand edge cases for the other arithmetic ops
- **THEN** results and errors SHALL be identical to the unfused native-op sequence, and the allocation ledger SHALL be charged identically

#### Scenario: Validation covers fused operands

- **WHEN** a chunk containing fused instructions with out-of-range slot, constant, site, or branch-target operands is loaded
- **THEN** `Validate` SHALL reject it before execution

#### Scenario: Uncovered shapes compile as before

- **WHEN** a comparison result is consumed by a non-branch consumer, or an operand is neither a local slot nor a constant
- **THEN** the compiler SHALL emit the existing unfused sequence unchanged
