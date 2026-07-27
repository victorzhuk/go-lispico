# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Fused native-op results charge the allocation ledger

The VM fused native arithmetic and comparison ops SHALL charge the evaluation
allocation ledger for their result, consistent with the charge the GoFunc
dispatch path already applies, so a heap-boxed `Float` or out-of-preboxed-range
`Int` produced by a fused `+`/`-`/`*`/`/`/comparison op is not invisible to
`MaxAllocationBytes`. The charge SHALL be a fixed scalar size computed at the
fused dispatch site (no Go allocation added). Preboxed small-int and boolean
results MAY be charged their same fixed scalar size; the intent is consistency
with the non-fused path, not a new exemption.

Charges issued by VM opcodes MAY accumulate in VM-local storage between
settlement points instead of writing to the shared evaluation-state ledger per
instruction. Settlement SHALL occur at every batched cancellation checkpoint,
at every run exit (normal return and error unwind), and before any `GoFunc`
dispatch or re-entrant evaluation adoption, so any externally observable read
of the ledger — a host function, a nested evaluation, a meter lease, or the
evaluation's own completion — sees totals identical to per-instruction
charging. Limit enforcement MAY lag by at most one unsettled batch (one
checkpoint interval of fixed-size scalar charges); the terminal
`ResourceLimitError` and its error shape SHALL be unchanged.

#### Scenario: Fused arithmetic result is charged

- **WHEN** a fused native op produces a heap-boxed numeric result under the ledger
- **THEN** the allocation ledger SHALL be charged for that result, matching the GoFunc-dispatch charge for the same operation

#### Scenario: Goldset allocation posture is preserved

- **WHEN** the goldset benchmark cells run in VM mode after this change
- **THEN** their allocations per operation SHALL be non-increasing (the charge is a size computation, not an allocation)

#### Scenario: Host observation sees exact totals

- **WHEN** a compiled body dispatches a `GoFunc` (or re-enters the evaluator) after executing charged opcodes
- **THEN** the ledger the host or nested evaluation observes SHALL include every charge issued before the dispatch, identical to per-instruction charging

#### Scenario: Limit crossing fails within one batch window

- **WHEN** a program's charged bytes cross `MaxAllocationBytes` (or exhaust a meter lease) between two checkpoints
- **THEN** evaluation SHALL fail with the same terminal `ResourceLimitError` no later than the next settlement point, and the meter's draw/return accounting SHALL balance exactly as with per-instruction charging
