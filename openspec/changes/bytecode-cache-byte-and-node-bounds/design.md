# Design — bytecode-cache-byte-and-node-bounds

## Decisions

- D1: Per-chunk deep bytes + node count captured once at compile, stored on
  `Chunk`, never recomputed at eviction. Deep means: boxed constant payloads
  measured structurally with the change-1 fixed size table (deterministic
  across platforms; `unsafe.Sizeof` rejected for the same reason as in
  change 1), `SubChunks` recursive, `LocalNames` included, lazily built site
  tables excluded (bounded by code length; documented).
- D2: Deterministic LRU replaces the random map-delete (correcting this
  change's earlier draft, which claimed LRU already existed). Structure:
  intrusive list + map, recency on hit and insert. Determinism is a yagel
  ADR 0105 requirement, not an optimization.
- D3: All three ceilings enforced atomically on insert; multi-evict until
  all under. Eviction order is strictly LRU — reproducible given the same
  operation sequence.
- D4: Unfit chunk (alone over any ceiling) → run uncached, no error, no
  eviction storm to make room for it.
- D5: Meter integration is engine-meter-only (cache is engine-scoped, not
  per-evaluation): insert charges `ChargeRetained(deepBytes, 1)`; evict /
  macro-epoch flush / `Close` release. `ChargeRetained` failure on insert →
  the chunk runs uncached (admission denial, not evaluation failure) — the
  session ledger stays authoritative without turning cache pressure into
  eval errors.
- D6: Epoch exposure reuses the existing cache-key inputs (dialect
  fingerprint + macro epoch) as one stats field; no new invalidation
  machinery. Host-triggered per-source purge (yagel "eager eviction on
  Routine retirement") is deferred — no caller exists until yagel enables
  bytecode; trigger recorded.
- D7: Stdlib bootstrap artifact cache exempt (process-level, bounded by
  embedded stdlib source per dialect, no user chunks).

## Risks / Trade-offs

- O(evictions) sweep on overflow — bounded by entry ceiling, rare, and each
  eviction is O(1) list surgery.
- Deep constant measurement adds one structural walk per compile — compile
  is already the slow path and the walk is charged to the compiling
  evaluation (change 1), so adversarial constant blobs pay for their own
  measurement.
- LRU bookkeeping on hit takes the cache mutex the lookup already takes; no
  new contention point.

## Migration Plan

1. Publish deep bytes + node count on `Chunk` at compile (with the size
   table from change 1).
2. Replace the map cache with the LRU structure behind the same lookup API;
   keep the macro-epoch flush behavior.
3. Three-ceiling admission + multi-evict + unfit-chunk bypass.
4. Meter hooks (insert/evict/flush/Close) behind the engine meter.
5. Stats block; adversarial + determinism tests.

## Open Questions

None blocking.
