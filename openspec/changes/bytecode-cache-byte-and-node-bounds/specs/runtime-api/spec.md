# runtime-api — delta

## ADDED Requirements

### Requirement: Bytecode cache byte and node bounds with deterministic LRU

`ResourceLimits` SHALL carry `MaxCacheBytes` and `MaxCacheNodes` fields,
defaulting to 64 MiB and 1,000,000 when left at zero. Each compiled `Chunk`
SHALL publish a deep byte size (fixed size table; boxed constant payloads
measured structurally; `SubChunks` recursive) and the node count of its
macro-expanded source form, captured at compile time. The per-engine chunk
cache SHALL be a deterministic LRU: recency SHALL update on hit and insert,
and eviction SHALL be strictly least-recently-used, reproducible for
identical operation sequences. Admission SHALL enforce `MaxCacheEntries`,
`MaxCacheBytes`, and `MaxCacheNodes` atomically on insert, evicting LRU
entries until all three hold; one insertion MAY evict multiple entries. A
chunk that alone exceeds any ceiling SHALL NOT be admitted and its
evaluation SHALL proceed uncached without error. When an engine meter is
configured, the cache SHALL charge `ChargeRetained(deepBytes, 1)` on insert
and `ReleaseRetained` on evict, macro-epoch flush, and `Close`; a denied
retained charge SHALL cause the chunk to run uncached, never fail the
evaluation. `EngineStats` SHALL expose cache entries, bytes, nodes, and the
cache epoch. The process-level stdlib bootstrap artifact cache is exempt
from these ceilings.

#### Scenario: Byte ceiling evicts

- **WHEN** the cache is filled with chunks whose combined deep bytes exceed `MaxCacheBytes` while entry count is at or below `MaxCacheEntries`
- **THEN** the cache SHALL evict least-recently-used entries until combined bytes are under `MaxCacheBytes`

#### Scenario: Node ceiling evicts

- **WHEN** the cache holds chunks whose combined node count exceeds `MaxCacheNodes`
- **THEN** the cache SHALL evict least-recently-used entries until combined nodes are under `MaxCacheNodes`

#### Scenario: All three ceilings enforced on one insert

- **WHEN** inserting a chunk whose addition crosses more than one ceiling
- **THEN** the cache SHALL evict enough least-recently-used entries to bring all three back under their ceilings in one insertion

#### Scenario: Eviction is deterministic

- **WHEN** two identical sequences of compile/hit/insert operations run against two engines with identical limits
- **THEN** both caches SHALL evict the same keys in the same order and retain identical entry sets

#### Scenario: Unfit chunk runs uncached

- **WHEN** a single chunk's deep bytes or node count alone exceeds a ceiling
- **THEN** its evaluation SHALL succeed, the chunk SHALL NOT enter the cache, and no entries SHALL be evicted for it

#### Scenario: Cache charges the engine meter

- **WHEN** an engine constructed with `WithEngineMeter` inserts and later evicts a chunk
- **THEN** the meter SHALL receive `ChargeRetained` with the chunk's deep bytes on insert and a matching `ReleaseRetained` on evict

#### Scenario: Constant payloads count

- **WHEN** a chunk with tiny code embeds a quoted structure of large structural size
- **THEN** its published deep bytes SHALL reflect the structure's measured size, not merely constant-slot headers
