## Why

`HashMap.Assoc`/`Dissoc` (`core/types.go`) allocate a new `HashMap` and copy every existing entry on each call — correct copy-on-write, but O(n) per insertion. `json/decode` (`plugins/data/plugin.go`) builds a decoded object by calling `Assoc` once per key, so decoding an n-key JSON object is O(n²). Measured on an active plugin (`data` is not frozen): decoding an 8000-key object takes **14.7 seconds**. Any bulk map build from an external source hits the same cliff.

## What Changes

- Give `json/decode` (and any other bulk map builder in the `data`/stdlib path) a builder fast path: construct the map in place with the existing internal `HashMap.Set` escape hatch, copying-on-write only once, when the finished map is handed to the caller. This makes bulk decode O(n).
- `HashMap.Assoc`/`Dissoc` themselves stay flat-copy — a single immutable insertion remains O(n). Lisp-side incremental builds such as `(reduce assoc ...)` therefore stay O(n²); this is an accepted trade-off (a persistent HAMT is deferred until a consumer needs incremental big-map building), documented so nobody assumes otherwise.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `data-plugin`: `json/decode` gains a requirement that decoding scales linearly in key count (no quadratic blowup), while preserving the existing round-trip and integer-detection guarantees.

## Impact

- Code: `plugins/data/plugin.go` (`fromJSONValue` object case uses an in-place builder), possibly a small internal builder helper in `core` around `HashMap.Set`. `core/types.go` `Assoc`/`Dissoc` unchanged.
- Invariants preserved: immutability holds at the `Equals` level — the in-place `Set` is used only build-before-share, never after the map is exposed (the existing documented contract for `HashMap.Set`). `core/` stays zero-import.
- Out of scope: a persistent HAMT / structural-sharing map; optimizing Lisp-side `assoc` loops; everything in the other two changes.
