# runtime-api — delta

## ADDED Requirements

### Requirement: Meter composition through ranked leases

The runtime SHALL provide a `Meter` interface with `ChargeReductions`,
`ChargeAllocation`, `ChargeRetained`, and `Lease` methods, and a `Lease` type
with a `Return` method. Engine entry points (`Eval`, `EvalWithBindings`,
`Call`, `(*Fn).Call`) SHALL read a `Meter` from the caller's context via
`runtime.WithMeter(ctx, m)` and SHALL route every reduction, allocation, and
retained-capacity charge through it when one is present. A `Meter` SHALL be
safe for concurrent use; a `Lease` SHALL be owned by one evaluation.
`runtime.NewChildMeter(parent Meter, rank int, limits ResourceLimits) Meter`
SHALL return a child meter whose `Lease` draws credit from the parent at the
given rank (lower rank = outer scope). An exhausted parent SHALL fail the
child's `Charge*` with `Code: "ResourceLimitError"`. `Lease.Return` SHALL
release unused credit back to the parent. Consumed totals SHALL be
Session-total non-resettable counters: only the draw-down budget is credited
back on `Return`. Absent a meter, the engine SHALL fall back to the existing
per-evaluation `evalState` ledger unchanged.

#### Scenario: Child meter fails closed when parent exhausted

- **WHEN** a parent meter's reductions are exhausted and a child meter at a higher rank attempts to charge
- **THEN** the child's `ChargeReductions` SHALL fail with `Code: "ResourceLimitError"` and the parent's counter SHALL not go negative

#### Scenario: Lease returns unused credit

- **WHEN** a `Lease` was granted N reductions and consumed M < N, then `Return` is called
- **THEN** the parent meter's reductions SHALL be credited (N − M) back

#### Scenario: Absent meter preserves existing behavior

- **WHEN** an engine entry point is called with a context carrying no `Meter`
- **THEN** charges SHALL flow to the per-evaluation `evalState` ledger exactly as before this change

#### Scenario: Concurrent charges are race-free

- **WHEN** multiple goroutines charge the same `Meter` concurrently
- **THEN** each charge SHALL be accounted and `go test -race` SHALL report no data race
