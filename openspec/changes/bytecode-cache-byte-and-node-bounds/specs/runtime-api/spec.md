# runtime-api — delta

## ADDED Requirements

### Requirement: Bytecode cache byte and node bounds

`ResourceLimits` SHALL carry `MaxCacheBytes` and `MaxCacheNodes` fields,
defaulting to 64 MiB and 1,000,000 when left at zero. Each compiled `Chunk`
SHALL carry a captured byte size and node count published at compile time. The
chunk cache SHALL enforce all three ceilings — `MaxCacheEntries`,
`MaxCacheBytes`, `MaxCacheNodes` — atomically on insert; crossing any one SHALL
trigger LRU eviction until the cache is under all three. A single insertion
MAY evict multiple entries.

#### Scenario: Byte ceiling evicts

- **WHEN** the cache is filled with `MaxCacheEntries` chunks whose combined bytes exceed `MaxCacheBytes`
- **THEN** the cache SHALL evict LRU entries until combined bytes are under `MaxCacheBytes`, even though entry count is at or below `MaxCacheEntries`

#### Scenario: Node ceiling evicts

- **WHEN** the cache holds `MaxCacheEntries` chunks whose combined node count exceeds `MaxCacheNodes`
- **THEN** the cache SHALL evict LRU entries until combined nodes are under `MaxCacheNodes`

#### Scenario: All three ceilings enforced on one insert

- **WHEN** inserting a chunk whose addition crosses more than one ceiling
- **THEN** the cache SHALL evict enough LRU entries to bring all three back under their ceilings in one insertion
