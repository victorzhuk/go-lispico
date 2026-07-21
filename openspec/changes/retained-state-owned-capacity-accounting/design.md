# Design — retained-state-owned-capacity-accounting

## Decisions

- D1: Ownership stays with the `Env`. Capturing a `Lambda` does not transfer or double-count. Releasing dead-bucket capacity requires explicit `Rebuild`. This matches yagel ADR 0105 "only a metered atomic rebuild can replace and release backing."
- D2: Defaults match yagel ADR 0105 (32 MiB / 100,000 slots per child env).
- D3: `Rebuild` is opt-in via the new public method; the engine never auto-invokes it. The embedder (yagel Scheduler) decides when.
- D4: Charge on binding write, not on read. A read through a tombstoned slot is free.

## Risks / Trade-offs

Charging on every binding write adds overhead on hot `let` paths; mitigated by charging only when adding a new slot, not on rebinding through an existing `Cell` (the `Cell` is reused, no new backing).

## Migration Plan

1. Add capacity counters to `Env`; thread limits via `engineConfig`.
2. Charge on new slot write; add the race test.
3. Add `Rebuild` with the atomic-swap test.
4. Wire the VM per-call frame env allocation to the same counter.

## Open Questions

None blocking. Whether `Rebuild` also recursively rebuilds children is decided during implementation (default: no — only the env's own bindings).
