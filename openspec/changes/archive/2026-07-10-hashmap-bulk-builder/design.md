## Context

`HashMap` is a flat `map[...]Value`; `Assoc` copies the whole map per call. `fromJSONValue`'s object case loops `Assoc` per key → O(n²). `HashMap.Set` already exists as an internal in-place construction escape hatch (documented "build before share"), used the same way by `core/reader.go` and `plugins/stdlib/collections.go`. The fix reuses that hatch for the decode path.

## Goals / Non-Goals

**Goals:**

- `json/decode` is O(n) in key count.
- Immutability contract intact: in-place `Set` only before the map is exposed.

**Non-Goals:**

- Persistent HAMT / structural sharing (deferred; no consumer needs incremental big-map builds yet).
- Speeding up Lisp-side `(reduce assoc ...)` — stays O(n²), documented as accepted.

## Decisions

- **Builder over the existing `Set`.** `fromJSONValue` allocates one `HashMap`, fills it with `Set` while decoding the object's members, and returns it once — a single map, no per-key copy. This matches how `reader`/`collections` already build literal maps.
- **`Assoc`/`Dissoc` untouched.** Single immutable insertion semantics and their O(n) cost stay as-is; only bulk builders change.

## Risks / Trade-offs

- **Escape-hatch misuse.** `Set` mutates in place; using it after the map is shared would break immutability. Confine it to the local build inside `fromJSONValue`, returning the map only after the last `Set` — the same discipline the existing call sites follow. A round-trip equality test guards correctness.
