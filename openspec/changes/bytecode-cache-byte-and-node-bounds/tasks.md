## 1. Behavior contracts

- [ ] 1.1 Red tests: fill cache with 4,096 25-KiB chunks → bytes ceiling trips; fill with 4,096 250-node chunks → nodes ceiling trips; combined-over load evicts in one insert.

## 2. Implementation

- [ ] 2.1 Add `MaxCacheBytes`, `MaxCacheNodes` to `runtime.ResourceLimits`; defaults 64 MiB / 1,000,000.
- [ ] 2.2 Capture per-chunk byte + node counts at compile; publish as fields on `Chunk`.
- [ ] 2.3 Cache admission checks all three ceilings on insert; multi-eviction via LRU until all three are under.

## 3. Integration

- [ ] 3.1 Existing cache tests stay green (defaults chosen so current capacity is unaffected).
- [ ] 3.2 `go test ./... -race`.

## 4. Verification

- [ ] 4.1 `openspec validate --strict bytecode-cache-byte-and-node-bounds`.
