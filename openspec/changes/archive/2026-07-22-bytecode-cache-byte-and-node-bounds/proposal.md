## Why

yagel ADR 0105: "The bytecode cache is deterministic LRU, bounded to 4,096
entries / 64 MiB / 1,000,000 nodes, contains no owner/capability graph,
charges Session retention and executes unfit chunks uncached."
`MaxCacheEntries` covers entry count only, and today's eviction is not LRU at
all — `runtime/eval.go` deletes an arbitrary map-iteration victim, so
eviction order is nondeterministic. An adversary can also fit 4,096
small-code chunks whose constants hold enormous quoted structures and never
trip the entry ceiling.

yagel runs the tree-walker at v0.8.0, but master's default evaluator is
already the VM — this contract goes live for yagel at its next version bump.

## What Changes

- Extend `runtime.ResourceLimits` with `MaxCacheBytes int` and
  `MaxCacheNodes int` (zero → defaults `64 * 1024 * 1024`, `1_000_000`).
- Each compiled `Chunk` carries a deep byte size and a node count, captured
  once at compile time and published as fields:
  - deep bytes: fixed-size-table cost (shared with
    `engine-reduction-and-allocation-metering`, deterministic across
    platforms) of `Code`, `Constants` including boxed payload structural
    size (a quoted list charges its elements, not just interface headers),
    `LocalNames`, and `SubChunks` recursively; lazily built site tables are
    excluded (bounded by code length, documented);
  - node count: AST node count of the macro-expanded form the chunk was
    compiled from.
- The per-engine chunk cache becomes a deterministic LRU (recency updated on
  hit and insert; eviction strictly least-recently-used), replacing the
  random map-delete.
- Admission enforces all three ceilings atomically on insert; crossing any
  one evicts LRU entries until all three are under. A single insertion may
  evict multiple entries.
- A chunk that alone exceeds any ceiling is NOT admitted and its evaluation
  proceeds uncached ("executes unfit chunks uncached") — admission denial is
  never an evaluation error.
- When the engine has a meter (`runtime.WithEngineMeter`,
  `meter-leases-and-session-ledgers`), the cache charges
  `Meter.ChargeRetained(deepBytes, 1)` on insert and calls
  `ReleaseRetained` on evict, macro-epoch flush, and `Close` — yagel's
  "cache charges Session retention".
- `EngineStats` gains a `Cache` block: entries, bytes, nodes, and the
  cache epoch (dialect fingerprint + macro epoch — the existing key inputs,
  exposed so a host can observe invalidation pressure).
- The process-level stdlib bootstrap artifact cache
  (`runtime/bootstrap_cache.go`) is exempt and documented as such: it is
  bounded by the embedded stdlib source per dialect, is not per-engine, and
  carries no user-authored chunks.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: extend the `ResourceLimits Engine option` requirement with
  the two new fields; add requirement `Bytecode cache byte and node bounds
  with deterministic LRU`.

## Impact

- Depends on: `engine-reduction-and-allocation-metering` (size table),
  `meter-leases-and-session-ledgers` (engine meter, retained charge hooks).
- Code: `runtime/eval.go` (cache structure → LRU, admission, meter hooks),
  `runtime/engine.go` + `runtime/stats.go` (limits, stats),
  `core/compiler/compiler.go` + `core/vm/chunk.go` (publish deep bytes +
  node count).
- Defaults chosen so current capacity is unaffected; existing cache tests
  stay green.
