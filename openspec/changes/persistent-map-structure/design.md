## Context

`HashMap` has two representations. At or below `hashMapSmallLimit` (8) it is a
sorted `[]entry` scanned linearly; above it, a `map[hashKey]entry`. Promotion is
one-way. The small form's per-update copy is O(8) — a constant, and not in scope.
The large form's is O(n), and is.

Three internal helpers are the whole coupling surface to the rest of the package:
`sortedEntries()`, `eachRaw(func(entry))` and `getByHashKey(hashKey)`, used by
`core/depth.go` for printing, equality and byte accounting. Keeping their
signatures keeps this change inside `core/types.go`.

## Goals / Non-Goals

**Goals:** large-form `Assoc`/`Dissoc` in O(log₃₂ n) with structural sharing;
`assoc`/`dissoc` charging the storage actually allocated; the measured ledger
ceiling removed.

**Non-Goals:** changing iteration order; changing the small form or the promotion
threshold; demoting a shrunken map back to small form; making `Get` faster —
it is already a single map lookup and the trie will be marginally slower.

## Decisions

### CHAMP node layout, two disjoint bitmaps

```go
type hamtNode struct {
	dataMap  uint32      // slots holding an entry
	nodeMap  uint32      // slots holding a child; disjoint from dataMap
	entries  []entry     // compacted, len == bits.OnesCount32(dataMap)
	children []*hamtNode // compacted, len == bits.OnesCount32(nodeMap)
}
```

Slot for hash fragment `f`: `bit := uint32(1) << f`; child index
`bits.OnesCount32(nodeMap & (bit-1))`, entry index
`bits.OnesCount32(dataMap & (bit-1))`.

Chosen over Clojure's single `[]Object` with alternating key/value slots, which
in Go means `[]any` and boxes every entry. Two typed slices avoid that and match
the `vecNode{kids []*vecNode; vals []Value}` layout this codebase already runs for
`Vector`'s trie — same branching factor (`vecBits = 5`, `vecBranch = 32`), same
path-copying idiom, so the reviewer is reading a shape they have already reviewed.

Reuse `vecBits`/`vecBranch` rather than introducing parallel constants. A second
pair of names for the same two numbers is how they drift apart.

### Collision nodes, and why they are mandatory

A `uint32` hash consumed 5 bits at a time yields fragments at shifts 0,5,…,30 —
the last contributing only 2 bits. At shift 35 Go evaluates `h >> 35` on a
`uint32` as 0, so every key would land in slot 0 and descent would not terminate.
Two distinct keys whose hashes are equal reach that point by construction.

A collision node is marked by `dataMap == 0 && nodeMap == 0 && len(entries) > 0`
and holds its entries as a flat, linearly scanned slice. That combination is
unreachable for a normal node — a node with no bits set holds nothing — so it
needs no extra field. Guard the descent on `shift >= 32`, not on a node count.

### Fixed-seed FNV-1a over `hashKey`, never `hash/maphash`

`hash/maphash` seeds randomly per process. Identical input would then produce a
different trie shape per run, and while sorted iteration would hide it from
`Each`/`String`, it would surface anywhere trie order leaks — and it would make
failures unreproducible. That directly contradicts `core-engine`'s determinism
invariant.

64-bit FNV-1a over `(typ, num, str)` with the standard offset basis and prime,
folded to 32 bits as `uint32(h ^ (h >> 32))`. ~10 lines, no imports beyond what
`core/` already has. The fold matters: the top levels of the trie discriminate on
the low bits, and folding mixes the high half into them.

### The hash is not cached in `entry`

`entry` is 64 bytes (`hashKey` 32 + two interface values). Adding a `uint32` pads
it to 72 and grows every node's `entries` slice by 12%, to save recomputation only
during node splits. Each operation computes the hash once regardless. Not worth
it; revisit only if a profile disagrees.

### `Len` moves to a counter on the struct

`len(h.m)` is O(1) today; counting trie entries is not. `HashMap` gains a `count int`,
maintained by `Assoc`/`Dissoc`/`Set`, mirroring how `Vector` keeps `count` on the
root struct rather than on `vecNode`. This grows `HashMap` from 32 to 40 bytes.
`MeterHashMapHeaderBytes` stays at 32: the metering constants are a deliberately
arch-independent approximation table, not `unsafe.Sizeof` assertions, and moving one
shifts every recorded ledger expectation in the suite for no gain in honesty.

### `Set`: measure before choosing — this is the one open decision

`Set` is the mutable escape hatch. Its contract already says it is "an in-place
escape hatch for building a fresh map before it is shared". Every in-repo caller
honours that: all six start from a fresh map and fill it. The problem is that
these six are the only large-map builders that exist, so whatever `Set` costs is
what map construction costs.

Path-copying `Set` — reassign `h.root` from a persistent insert — is trivially
correct and safe for any receiver, shared or not. Its cost is not obviously
acceptable: a path copy at depth 2 duplicates a node's `entries` slice, up to 32 ×
64 bytes, so a builder loop moves roughly two orders of magnitude more bytes per
insert than a Go map assignment. Over a 1000-key `json/decode` that is a real
regression on a real path.

In-place trie mutation is what the contract's wording permits and is O(1)
amortized, but it is unsafe the moment a receiver shares nodes with a map produced
by `Assoc` — which is exactly what this change introduces. Distinguishing the two
cases needs an ownership flag, and the flag has to be cleared on the *receiver*
when `Assoc` derives from it, which is a write to a value shared across
goroutines. That is a data race on a type whose concurrent use is a stated
requirement, and making it atomic taxes every `Assoc`. Rejected on that basis, not
on complexity.

So: implement path-copying `Set` first, measure `BenchmarkHashMap_SetBuild`
against the recorded baseline, and report the delta. If it is material, the
follow-up is a package-internal builder that owns its nodes from construction and
is handed to the six call sites — no exported surface, no flag on the shared type.
Do not choose between these by argument; the benchmark exists to decide it.

### Canonicalization on `Dissoc` is limited to dropping empty nodes

Removing an entry can leave a child empty; drop it from the parent so repeated
`Dissoc` cannot accumulate dead nodes. Inlining a single-entry child into its
parent is the further CHAMP canonicalization and is deliberately skipped:
correctness does not need it, `Equals` is membership-based rather than
shape-based, and lookup depth stays bounded by hash bits either way. Skipping it
keeps `Dissoc` readable; revisit if a profile shows depth is real.

### Charging follows the incremental rule already written for sequences

`assoc` currently charges `HashMapShallowBytes(result.Len()) + ValueDeepBytes(v)`
per call. The first term becomes the bytes the insert actually allocated — the
copied path plus any new node — leaving the second unchanged, since an inserted
value is genuinely new to the ledger. This is the rule `cons`/`conj` follow after
`persistent-sequence-structures`, and the reason `assoc`'s requirement already
describes it.

`ValueShallowBytes(*HashMap)` (`core/metering.go:565`) stays `HashMapShallowBytes(Len())`
and is deliberately not touched: it is the apply-site fallback, and the callee
marks itself charged through `ChargeGoFuncResultBytes`, so the fallback is not
reached for `assoc`. Changing it would alter charging for every other map-valued
builtin in the same breath as this one.

## Risks / Trade-offs

- **The gold set is blind to this.** No fixture builds a map past 3 keys, so
  perfgate cannot confirm or refute the change. Treat its verdict as a
  collateral-damage check only, and rely on the ledger ceiling plus the two new
  benchmarks for the actual result. Stating this here so a green gate is not
  later cited as proof the change worked.
- **`Get` gets slightly slower** on the large form: a few levels of trie descent
  against one Go map lookup. Acceptable — reads were never the defect — but it is
  a real regression and `BenchmarkHashMap_ScanVsMap` does not measure it, since it
  pins the small-form boundary. A large-form `Get` benchmark is added so the cost
  is recorded rather than discovered later.
- **Two guarding tests are in the diff.** `TestHashMap_PromotionBoundary` reads the
  unexported storage field and `TestAssocMonotonic_ChargesPerCallHonestly` asserts
  the exact charge this change alters. Both must be edited by the change they
  guard. Neither may be weakened to pass: promotion must still be asserted at the
  9th distinct key and still be one-way, and the charge test must assert a linear
  total rather than dropping the assertion.

## Migration

None. No exported signature, error, iteration order, or printed form changes.
