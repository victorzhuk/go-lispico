# Design — bytecode-cache-byte-and-node-bounds

## Decisions

- D1: Per-chunk byte + node counts are captured once at compile time and stored on the `Chunk` struct, not recomputed at eviction.
- D2: All three ceilings enforced atomically on insert; if any one is over, LRU-evict until all three are under. A single insertion may evict multiple entries.

## Risks / Trade-offs

An O(n) eviction sweep on overflow; mitigated because eviction is rare (only on overflow) and the cache size is bounded by `MaxCacheEntries`.

## Migration Plan

1. Capture byte + node counts at compile; add fields to `Chunk`.
2. Add the two `ResourceLimits` fields with defaults.
3. Replace the entry-only admission check with a three-dimensional check + multi-eviction.
4. Add adversarial tests.

## Open Questions

None blocking.
