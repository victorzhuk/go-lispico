## Why

yagel ADR 0105 caps the bytecode cache at "4,096 entries / 64 MiB / 1,000,000 nodes". `MaxCacheEntries` covers entry count only; an adversary can fit 4,096 small-but-deep chunks holding 100M nodes and never trip the entry ceiling. Add byte and node bounds.

## What Changes

- Extend `runtime.ResourceLimits` with `MaxCacheBytes int` and `MaxCacheNodes int` (zero → default; defaults `64 * 1024 * 1024`, `1_000_000`).
- Each compiled `Chunk` carries its byte size (`unsafe.Sizeof(Chunk)` + `cap(Constants)*sizeof(Value)` + `cap(Code)*sizeof(Opcode)`) and node count (recursive AST count captured at compile time); published as fields on `Chunk`.
- The chunk cache enforces all three ceilings atomically on insert; LRU eviction continues, evicting until the cache is under all three.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: extend the existing `ResourceLimits Engine option` requirement with the two new fields; add a new requirement `Bytecode cache byte and node bounds`.

## Impact

- Code: chunk cache (locate it during implementation; likely under `runtime/eval.go` where `maxCacheEntries` is consumed), `runtime/engine.go`, `compiler/compiler.go` / `vm/chunk.go` (publish byte + node counts).
