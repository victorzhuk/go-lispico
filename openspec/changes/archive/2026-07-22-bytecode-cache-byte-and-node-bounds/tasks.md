## 1. Behavior contracts

- [x] 1.1 Red tests: cache filled with small-code / huge-constant chunks
  trips the bytes ceiling below the entry ceiling; 250-node chunks trip the
  nodes ceiling; a combined-over insert evicts multiple entries in one
  insertion.
- [x] 1.2 Determinism test: identical operation sequences produce identical
  eviction order and identical surviving key sets (run twice, compare).
- [x] 1.3 Unfit chunk: a single chunk over any ceiling alone evaluates
  correctly and is absent from the cache afterward.
- [x] 1.4 Meter hooks: with `WithEngineMeter`, insert charges retained
  (deepBytes, 1); evict, macro-epoch flush, and `Close` release the same;
  `ChargeRetained` denial → chunk runs uncached, evaluation succeeds.

## 2. Implementation

- [x] 2.1 Add `MaxCacheBytes`, `MaxCacheNodes` to `runtime.ResourceLimits`;
  defaults 64 MiB / 1,000,000.
- [x] 2.2 Capture deep bytes (fixed size table; boxed constants structural,
  SubChunks recursive, LocalNames included) + expanded-form node count at
  compile; publish as `Chunk` fields.
- [x] 2.3 Replace the map cache with deterministic LRU (list + map, recency
  on hit and insert); preserve the macro-epoch flush.
- [x] 2.4 Three-ceiling atomic admission with LRU multi-eviction; unfit
  chunks bypass admission and run uncached.
- [x] 2.5 Engine-meter hooks: `ChargeRetained` on insert;
  `ReleaseRetained` on evict / flush / `Close`.
- [x] 2.6 `EngineStats.Cache`: entries, bytes, nodes, epoch (dialect
  fingerprint + macro epoch).
- [x] 2.7 Document the stdlib bootstrap artifact cache exemption where the
  cache limits are documented.

## 3. Integration

- [x] 3.1 Existing cache tests stay green (defaults leave current capacity
  unaffected).
- [x] 3.2 `go test ./... -race`; `GOLDSET_MODE=vm` goldset gate
  non-increasing (LRU bookkeeping on the hit path must not regress).

## 4. Verification

- [x] 4.1 `openspec validate --strict bytecode-cache-byte-and-node-bounds`.
