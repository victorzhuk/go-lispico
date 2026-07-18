## Why

The rule-dispatch benchmark — the consumer-shaped workload ADR 0008's gold set models — runs at 4244 ns / 35 allocs vs GopherLua's 734 ns / 13 and goja's 993 ns / 15. The alloc profile shows `HashMap` machinery on the hot path: `NewHashMap` allocates **two** Go maps per literal (`m` for values, `keys` for original key display), every `Each`/`Pairs`/`String` call allocates a `[]hashKey` slice and **re-sorts it from scratch**, and `toHashKey` formats numeric keys through `strconv.FormatInt`/`FormatFloat` (and `fmt.Sprintf` for bools) on **every** `Get`/`Set`/`Assoc` — a string allocation per numeric-key operation. `Assoc`/`Dissoc` copy both Go maps even for a 2-entry map.

Rule workloads build and read small maps constantly: `{:kind :review :files [...]}` tasks, `{:model :large :tools [...]}` results — 2–4 keys. Go maps are the wrong shape at that size: Clojure itself backs map literals with `PersistentArrayMap` up to 8 entries before promoting to a hash map, precisely because a scanned array beats hashing for tiny maps and copies in O(n) words.

## What Changes

- **Format-free hash keys**: `hashKey` becomes `{typ uint8, num uint64, str string}` — numeric/bool keys store their bits in `num`, only string-kinded keys use `str`. No `strconv`/`fmt` on any map operation, at any size. Hashing/equality semantics unchanged (`1` and `1.0` remain distinct keys, as today).
- **Single storage**: entries carry the original key `Value` alongside the value, removing the parallel `keys` map at every size.
- **Compact representation for small maps**: at or below a promotion threshold (8 entries), a `HashMap` is backed by a sorted entry array — `Get` is a short scan/binary search, `Assoc`/`Dissoc` copy one small slice, iteration walks in order with **zero** per-iteration allocation and no re-sort. Above the threshold it promotes to the Go-map backing.
- **Deterministic iteration order preserved exactly**: the current sorted-by-key order of `Each`/`Pairs`/`String` is kept at both representations, so printing, literal evaluation, and golden outputs do not change.
- Public API (`Get`/`Set`/`Assoc`/`Dissoc`/`Len`/`Each`/`Pairs`/`Equals`/`String`) and immutability semantics are unchanged; this is a representation change behind the existing surface.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: new requirement — map representation efficiency (small-map operations allocate O(1) objects, no key formatting on lookups, deterministic iteration preserved, promotion at the threshold is semantically invisible).

## Impact

- Code: `core/types.go` (`hashKey`, `HashMap`, iteration), `plugins/data` JSON decode builder keeps working through `Set` (in-place build then promote check), `plugins/stdlib` map builtins untouched at the API level.
- Expected effect: rule-shaped cells lose the double-map, sort-per-iteration, and strconv allocs — the lispico-side share of the 35 allocs/op shrinks toward GopherLua's 13; both evaluators and the VM (`OpMakeMap`) benefit.
- Gate: ADR 0008 data/output-dominated and hot cells — bytes and alloc count must be non-increasing; this change moves them down.
- Invariants: value equality/hash unchanged; deterministic iteration order byte-identical; immutability (`Assoc` never mutates the receiver); `-race` clean (immutable after publication, as today).
- Sequencing: independent of the VM changes; combines with `vm-dispatch-loop-tightening`'s preboxing on map-heavy workloads.
