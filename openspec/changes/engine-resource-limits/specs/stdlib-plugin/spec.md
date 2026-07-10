# stdlib-plugin — delta

## ADDED Requirements

### Requirement: range is bounded and cancellable

The `range` builtin SHALL NOT build an unbounded result. It SHALL cap its result
length at the Engine's configured collection-length ceiling, returning a
`*core.LispicoError` when the requested range would exceed it, and it SHALL check
`ctx` cooperatively while building so a `WithTimeout` or cancelled context stops it
promptly instead of running to completion or exhausting memory.

#### Scenario: Oversized range fails closed

- **WHEN** `(range 0 n)` is evaluated with `n` greater than the collection-length ceiling
- **THEN** evaluation SHALL return a `*core.LispicoError` reporting the length limit, and no oversized slice SHALL be allocated

#### Scenario: range honors cancellation

- **WHEN** a `range` over a large span is evaluated under a context that is cancelled or times out mid-build
- **THEN** the evaluation SHALL stop with the context error rather than continuing to allocate
