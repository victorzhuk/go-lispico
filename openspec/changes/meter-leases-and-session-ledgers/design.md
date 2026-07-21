# Design — meter-leases-and-session-ledgers

## Decisions

- D1: Meter is opt-in via context. Absent meter → existing per-eval `evalState` ledger applies (full back-compat, all current tests unchanged).
- D2: Ranks are integers; lower = outer scope. The embedder picks the rank lattice. yagel's lattice is eval < Routine < Workflow < Session; that mapping lives in yagel, not here.
- D3: `Lease.Return` releases unused credit back to the parent; a Lease that exhausted its parent's credit returns nothing and the parent is not debited further. Consumed totals are Session-total non-resettable counters — only the draw-down budget is credited back.
- D4: A `Meter` is safe for concurrent use (atomic counters); a `Lease` is owned by one evaluation (not shared across goroutines).
- D5: Wall-clock deadlines are out of scope (ADR 0010 unchanged).

## Risks / Trade-offs

Two layers of charging (evalState + Meter) when a meter is present. Mitigated: the evalState counter is the per-eval ceiling, the meter is the cross-scope budget; both charges are a single atomic add each, no locks.

## Migration Plan

1. Define the `Meter` / `Lease` types with atomic counters and a no-op default.
2. Add `WithMeter` context helper and read it at every engine entry point.
3. Route the existing per-eval charge sites through the meter when present.
4. Add the ranked-lease composition + return.
5. Add adversarial + concurrency tests.

## Open Questions

None blocking.
