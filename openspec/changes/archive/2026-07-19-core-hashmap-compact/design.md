# Design — compact HashMap representation

## Context

Measured (rule bench, alloc_objects): `NewHashMap` + `HashMap.Set` + `sortedKeys` are the lispico-side map costs; every iteration path (`Each`, `Pairs`, `String`, `Equals`) calls `sortedKeys` — an allocation and an O(n log n) sort per call; `toHashKey` allocates a formatted string per numeric/bool key operation. Result maps (`{:model :small}`) are built per rule invocation; task maps are read per invocation.

Current invariants to preserve:

- Keys limited to Nil/Bool/Int/Float/String/Symbol/Keyword (unhashable error otherwise); `Int 1` ≠ `Float 1.0` as keys.
- Iteration order: deterministic, sorted by `(typ, formatted-val)` today. Sorting by `(typ, num, str)` changes the *relative order of numeric keys* from lexicographic-on-string to numeric — e.g. today `10` sorts before `2` ("10" < "2"); numerically 2 < 10. Decision: **keep order deterministic but adopt numeric ordering**, and treat the old lexicographic-numeric artifact as an implementation accident, not a contract — no spec scenario pins it, and numeric order is the defensible behavior. Cross-val parity holds because both evaluators share the one implementation. If a golden test depends on the old order, the golden is updated in the same change with the reasoning recorded.
- Immutability: `Assoc`/`Dissoc` return new maps; `Set` is documented as pre-publication build-only.

## Decisions

### 1. `hashKey{typ uint8, num uint64, str string}`

- Int → bits of int64; Float → `math.Float64bits` (NaN keys: today `FormatFloat` makes all NaNs equal-ish as "NaN" — bits would split them; normalize NaN to one canonical pattern to keep "NaN key equals NaN key"); Bool → 0/1; Nil → 0; String/Symbol/Keyword → `str`.
- Comparable struct, three words — works as a Go map key directly; ordering for iteration: `(typ, num, str)`.

### 2. Entry-based storage

```go
type entry struct { hk hashKey; k, v Value }
```

- Small form: `entries []entry`, kept sorted by `hk` — insertion via copy+insert (n ≤ 8: a handful of word moves), `Get` via linear scan (branch-predictable, no hashing), iteration = walk, zero allocation.
- Large form: `m map[hashKey]entry` (single map — `keys` map deleted) + iteration sorts as today (rare shape; unchanged cost profile).
- Promotion at 9th distinct key inside `Set`/`Assoc`; no demotion on `Dissoc` (hysteresis avoids flapping; a big map that shrank stays map-backed — harmless).

### 3. Threshold

8 entries — three independent lines converge on it:

- Go's own map (swiss-table since 1.24) stores entries in 8-slot groups (`internal/runtime/maps/group.go`, `maxAvgGroupLoad = 7`) and allocates a **full 8-slot group plus `Map` struct and directory even for one entry** — below 8 keys a Go map is already a linearly probed small array, just with hashing and two levels of indirection on top.
- Published Go scan-vs-map crossover benchmarks land at 5–10 elements (darkcoding.net slice-vs-map; golang-nuts data: 4-element scan 17 ns vs 123 ns map lookup); golang/go#19495 profiles show `runtime.mapassign` dominating small-map workloads — and literal evaluation is an all-assignment workload.
- Clojure's `PersistentArrayMap`→`PersistentHashMap` boundary is 8.

Frozen after a local microbenchmark (scan vs map at 4/8/16 string and keyword keys) confirms the range on this codebase's `hashKey`.

### 4. What stays put

- `data/json` bulk decode builds via `Set` in place then publishes — works identically; promotion happens transparently during the build.
- `Equals` gets cheaper (walk both sorted entry arrays for small×small) but keeps semantics: same pairs, any representation.

## Alternatives considered

- **HAMT/persistent hash map**: right answer for large maps under heavy `Assoc` churn; wrong first move — rule workloads are ≤8 keys, and the gold set has no large-map churn cell. Revisit on evidence.
- **Insertion-order iteration** (Lua/JS-object flavor): cheaper still (no sort concept at all), but silently changes printing and any order-sensitive consumer; sorted order is the conservative choice.
- **Keep double storage, only cache sortedKeys**: fixes iteration but not construction, copies, or strconv; strictly dominated by entries.

## Verification

- Full map test surface (core, stdlib map builtins, data plugin JSON round-trip) + crossval parity.
- New: promotion boundary behavior (8→9→dissoc back), NaN-key equality, numeric-key ordering goldens, `AllocsPerRun`: small-map `Get`=0, `Each`=0, `Assoc`≤2.
- Rule bench + goldset data-dominated cells before/after; alloc count must drop.
