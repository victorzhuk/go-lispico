# value-layout-locality

## Why

A full field-layout inventory of `core/types.go` and `core/vm/chunk.go` finds
several structural inefficiencies, but none currently move a gold-set cell:
every fixture's data is small enough that no `Vector` reaches its trie
representation (`vectorFlatThreshold = 32`), and nothing in the repo
benchmarks a collection past that threshold. This change is measurement-first
by design — Go's allocator size classes make most struct shrinks free of
benefit unless they cross a class boundary (`Cell`, at 64 bytes, would round
right back up to the 64-byte class if trimmed to 56; there is no case for
touching it), so a proposal to change layout without measuring first would be
exactly the kind of speculative optimization this repo's own history has
closed out before (Stage D's per-node depth annotation was reverted because
growing `listNode` 32→48 bytes tripped the accounting-byte gate).

Concrete, size-class-crossing targets identified by direct inspection:

- `Vector` (`core/types.go:406`) is 72 bytes — `flat(24) + root(8) + shift
  uint(8) + count int(8) + tail(24)`, landing in Go's 80-byte size class.
  Narrowing `shift` to `uint8` and `count` to `int32` packs it to 64 bytes,
  crossing into the 64-byte class — a real 16-byte-per-boxed-vector saving,
  at the cost of capping vector length at 2³¹ elements.
- `chunk.ConstCharges` (`core/vm/chunk.go:87`) is a `map[int]int64` read
  inside the VM dispatch loop at `vm.go:899-902` (`OpConstCharged`) — the only
  map hash lookup anywhere in the hot dispatch path. A dense `[]int64`
  parallel to `Constants` removes it entirely.
- `OpTrue`/`OpFalse` (`vm.go:890,893`) construct a fresh `core.Bool{V: …}`
  per execution instead of pushing the already-shared `core.True`/`core.False`
  singletons (`types.go:61-62`).
- `vecNode` (`core/types.go:395`) is 48 bytes with exactly one of its two
  slice fields ever populated — half of every trie node is structurally dead
  weight, and `Conj`'s path-copy allocates a fresh node and slice per trie
  level.

## What Changes

Task 0 is a gate, matching this repo's convention for any change whose
motivating cost is not yet demonstrated on a real workload
(`vm-register-dispatch`'s task-0 gate is the precedent): add benchmarks that
actually exercise vectors past the flat threshold, measure, then decide.

- Add large-collection benchmarks (nothing in the repo today exercises a
  `Vector` past 32 elements) and measure before changing any layout.
- If warranted by measurement, in order of confidence: shrink `Vector` to 64
  bytes; replace `ConstCharges`'s map with a dense `[]int64`; push the shared
  `core.True`/`core.False` singletons from `OpTrue`/`OpFalse` instead of
  constructing fresh `Bool` values; consider splitting `vecNode` into
  distinct internal/leaf representations to eliminate the always-nil half.
- Hard constraint, to be pinned by a test before any struct changes size: the
  allocation ledger's accounted sizes come from ADR 0011's fixed,
  architecture-independent size table, never `unsafe.Sizeof` — accounted
  bytes for any value type MUST NOT move when its Go struct layout changes.
  A change here that moves a gold-set `B/op` ledger figure is wrong, not a
  side effect to accept.
- State plainly, if this change proceeds: the benefit is for embedders holding
  large in-memory vectors, not for this repo's own release gate. One gated cell
  does exercise the trie — `queue-promote` conjs to 40 elements and its own
  comment records that it crosses `vectorFlatThreshold` — so "no gold-set cell
  moves" is a check with a named target, not a formality. Any movement there
  should be a reduction; an increase is a defect.

## Impact

- Affected specs: `core-engine` (new requirement: boxed value memory layout
  MAY be tuned for size-class efficiency without changing accounted ledger
  size or observable semantics).
- Affected code, if gated in: `core/types.go` (`Vector`, `vecNode`,
  `OpTrue`/`OpFalse` call sites in `core/vm/vm.go`), `core/vm/chunk.go`
  (`ConstCharges` representation), `core/vm/vm.go:899-902`.
- Explicitly not touched: `Cell` (`core/env.go:17`) — 64 bytes today, a
  56-byte trim would round back up to the same 64-byte size class and buy
  nothing; not proposed. Adjacent to pending `vm-register-dispatch`, which
  would rewrite chunk representation wholesale if its own gate fires — the
  `ConstCharges` change here is forward-compatible with that outcome and low
  risk either way.
- Risk: low if the gate does not fire (no code changes at all beyond
  benchmarks). If it fires and layout changes proceed: the accounted-bytes
  invariant above is the primary correctness risk, verified by a dedicated
  test before any struct size is touched.
