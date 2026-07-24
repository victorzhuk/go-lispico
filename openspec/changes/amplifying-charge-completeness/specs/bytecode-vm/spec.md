# bytecode-vm — delta

## ADDED Requirements

### Requirement: Fused native-op results charge the allocation ledger

The VM fused native arithmetic and comparison ops SHALL charge the evaluation
allocation ledger for their result, consistent with the charge the GoFunc
dispatch path already applies, so a heap-boxed `Float` or out-of-preboxed-range
`Int` produced by a fused `+`/`-`/`*`/`/`/comparison op is not invisible to
`MaxAllocationBytes`. The charge SHALL be a fixed scalar size computed at the
fused dispatch site (no Go allocation added). Preboxed small-int and boolean
results MAY be charged their same fixed scalar size; the intent is consistency
with the non-fused path, not a new exemption.

#### Scenario: Fused arithmetic result is charged

- **WHEN** a fused native op produces a heap-boxed numeric result under the ledger
- **THEN** the allocation ledger SHALL be charged for that result, matching the GoFunc-dispatch charge for the same operation

#### Scenario: Goldset allocation posture is preserved

- **WHEN** the goldset benchmark cells run in VM mode after this change
- **THEN** their allocations per operation SHALL be non-increasing (the charge is a size computation, not an allocation)
