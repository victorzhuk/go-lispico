# Design — stdlib-startup-cache

## Measured baseline

Startup (article bench, engine + stdlib + one eval + close): tree 74 µs / 713
allocs; bytecode 120 µs / 744 allocs. GopherLua 70 µs / 837 allocs. The +46 µs
bytecode delta is per-engine stdlib compilation; the shared ~70 µs floor is
reader + env population + Go-builtin registration, partially addressable by
the same reuse.

## Profile first

Task 1 profiles a bytecode-startup loop and attributes cost to: `Read`
(tokenize/parse), `MacroExpand`, `Compile`+`Validate`, chunk-cache bookkeeping,
env `Set` traffic, plugin registration. The reuse boundary lands where the
cost is:

- compile-dominated → share compiled chunk trees (primary hypothesis);
- expand-dominated → share expanded forms too (they are immutable Values);
- reader-dominated → share parsed forms (cheapest, likely insufficient alone).

The design below assumes the primary hypothesis; it degrades gracefully to the
others (same key, different artifact).

## Cache shape

Package-level, in `runtime`:

```go
var stdlibArtifacts sync.Map // key → *artifactSet (immutable)
```

Key: `(dialectFP string, sourceHash string)`. `dialectFP` is the existing
dialect fingerprint; `sourceHash` the existing sha256 of the source text.
Value: the ordered compiled chunk trees (or expanded forms) for that source's
top-level forms, fully `Validate`d, plus the macro-definition prerequisites
the forms were expanded under.

Bounded by an entry ceiling (reuse `MaxCacheEntries` semantics at the process
tier); eviction is arbitrary-victim as in the engine cache. In practice the
map holds one entry per (dialect, stdlib version) — the ceiling only guards a
pathological dialect-churn embedder.

## Macro-epoch correctness

The per-engine cache keys on `macroEpoch` because a redefined macro must
invalidate old expansions. The process tier avoids the epoch entirely by
scoping reuse to **plugin-load position**: stdlib loads into a fresh engine
whose macro table for the stdlib source is fully determined by (dialect,
stdlib source) — the expansion environment is reproducible by construction.
Reuse is therefore keyed on that pair and applied only on the plugin-load
path, never for arbitrary user `Eval` source. A user engine that later
redefines macros affects only its own per-engine cache, as today.

Constraint to verify in implementation: stdlib's pure-Lisp forms are expanded
strictly against stdlib-defined macros + kernel forms (no engine-local state).
If any form violates this, it is excluded from reuse and compiled per engine.

## Cross-engine site safety

`siteEntry` publication is per-site mutable state. A chunk shared verbatim
across engines would ping-pong publications (entry.env differs per engine →
every miss republishes → per-read alloc churn in steady state). Therefore the
reuse path hands each engine a **shallow copy of the chunk tree**: shared
`Code`/`Constants`/`LocalNames`/`Captured` slices (immutable), fresh zero
`sites` per copy, `SubChunks` copied recursively. Cost: one small struct per
chunk, ~tens of chunks — nanoseconds against the 46 µs saved.

Closures created at load time reference the copied chunks, so each engine's
stdlib closures publish sites against their own root env only.

## Isolation obligations (spec scenarios)

- `def`/`defn` in engine A after load never visible in engine B.
- `UnloadPlugin(stdlib)` in A leaves B intact.
- Goldset + crossval byte-identical with reuse on and off (a test toggle or
  cache-clear hook for the suite).

## Non-goals

- Lazy stdlib loading (goja-style deferred init) — out of scope here; the
  trigger-gated follow-up `stdlib-lazy-materialization` covers it if this
  change's measured startup still misses the target.
- Caching user `Eval` sources across engines — macro-epoch reproducibility
  does not hold there.
