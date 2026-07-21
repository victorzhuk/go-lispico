# Design — engine-reduction-and-allocation-metering

## Decisions

- D1: Reduction is charged at the eval-step boundary (apply trampoline / VM instruction / macro / compiler emit / GoFunc dispatch), not at AST-node granularity. Matches ADR 0105's "context check within 1,024 reductions" precisely and keeps charging O(1) per step.
- D2: Allocation accounting is approximate — `unsafe.Sizeof` of value struct + slice/string header bytes; document the approximation in the ADR and lean conservative (over-count small allocations rather than under-count).
- D3: `CodeResourceLimit` is already non-catchable in tree-walker (`core/eval.go:971-975`); the implementation MUST extend the same pass-through to the VM `try`/`catch` opcode handler if it does not already (verify the VM catch path first; if already pass-through, no change).
- D4: Default values are identical to yagel ADR 0105's per-evaluation defaults so consumer and embedder share one contract.

## Risks / Trade-offs

Approximate allocation under-counts; mitigated by conservative per-type sizes and adversarial tests. Reduction charging adds per-step overhead; mitigated by piggy-backing on the v0.8.0 batched-cancellation countdown — no new clock read, one counter increment per step.

## Migration Plan

1. Add the two `ResourceLimits` fields with defaults resolved at `New`.
2. Extend `evalState` with reduction + allocation counters.
3. Wire charges into each eval boundary, one package at a time: tree-walker, then VM, then macro, then compiler, then plugin dispatch.
4. Add the adversarial + race tests alongside each wire.
5. `go test ./... -race` + goldset gate non-increasing.

## Open Questions

None blocking. The exact per-type allocation table is decided during implementation against the actual struct layouts.
