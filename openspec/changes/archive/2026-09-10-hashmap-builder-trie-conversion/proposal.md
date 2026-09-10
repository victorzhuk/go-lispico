## Why

`Map representation efficiency` already requires that above the small-map threshold
"the storage a single update allocates is bounded by the depth of the structure
rather than by its entry count", and its scenario `Extending a large map does not
copy it` pins the bytes and allocations one `Assoc` charges as the map grows. A
map in builder form does not meet that today.

`trieFromBuildMap` (`core/types.go:1091`) converts builder-form staging into trie
form on the first `Assoc` or `Dissoc` (`core/types.go:1121`, `:1158`). It builds a
fresh trie with one `root.assoc` per entry, each path-copying, and deliberately
leaves the receiver's `large.m` in place so no map is mutated behind a caller's
back. The receiver therefore stays in builder form: a chained update pays the
conversion once, because the intermediate it returns carries `large.root`, but k
updates against one retained base pay it k times, at O(n·ceil(log32 n)) node
allocations each, all but the final path immediate garbage. The charge is metered,
so the cost is budget-visible rather than silent — it is simply charged again for
storage the caller never keeps.

The scenario passes today only because its fixture is built by repeated `Assoc`,
which yields trie form and never exercises a builder-form receiver.

`reader-map-promotion-parity` widened the class this reaches. Before it, a map
literal above `hashMapSmallLimit` arrived from the reader already in trie form and
paid no conversion at all; it now arrives in builder form, so the first update on a
literal pays a full n-entry trie build and every further update on that same
literal pays another. The shape is reachable from Lisp, not just from Go: the
`conj`, `assoc` and `dissoc` builtins call straight into `HashMap.Assoc`/`Dissoc`
(`plugins/stdlib/collections.go:441`, `:553`, `:736`), so `(assoc m k v)` repeated
against one map literal is the exposed case.

The gold set did not and would not catch this: every fixture builds only small maps
below the threshold, so `reader-map-promotion-parity` measured allocation-neutral
there in both evaluator modes.

## What Changes

- Pin the builder-form receiver: add a scenario and a benchmark that drive repeated
  `Assoc` against one retained base built through `Set` and through a map literal,
  at sizes spanning two orders of magnitude, so the conformance gap has a number
  and cannot regress unobserved again.
- Bring repeated updates on a builder-form base within the requirement's bound.
- Leave the mechanism to the design stage and gate it on measurement. Building the
  trie in one bottom-up pass instead of n path-copying `assoc` calls reduces the
  constant while touching no invariant, but it does not by itself reach the bound:
  the conversion is inherently O(n), so conformance for the fan-out shape means the
  conversion happens once per map value rather than once per update. Deciding
  whether that amortisation is worth what it costs in immutability and charge
  determinism is this change's real question, not a detail of it.
- No change to which form the reader produces, to representation parity, or to
  `getByHashKey`, `Len`, iteration order, equality or printing.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: an immutable update on a map in builder form meets the same
  per-update storage bound the trie form already meets.

## Impact

Affected code: `core/types.go` (`trieFromBuildMap`, `Assoc`, `Dissoc`, and the
`largeMap` shape if a cache is introduced), plus a benchmark in `core`.

Three constraints bound the design, and any proposal that ignores one is wrong:

- A converted root cannot simply be stored in `large.root`. `getByHashKey` tests
  `root` before `m` (`core/types.go:1018-1025`) and `Len()` reads `large.count` for
  the trie form, which is zero for builder form, so writing `root` would change the
  read path and break `Len`. A cache the read path does not consult is a different
  and larger design than it first looks.
- `trieFromBuildMap`'s contract is that the receiver is untouched so concurrent
  readers are unaffected. ADR 0003 owns the concurrency model and immutability is a
  kernel invariant; memoisation trades against both and needs them stated, not
  assumed.
- Charging a cached conversion nothing on the second call makes a map's admitted
  total depend on its call history. The metering tests pin exact charges and
  ADR 0011 describes the terms as deterministic, so this is an ADR 0011 change and
  a `CHANGELOG.md` entry, not an implementation detail.

Whether ADR 0011's T-terms and the unit table move depends on which mechanism the
design picks; a one-pass build changes the admitted bytes of a first update even
though it changes no semantics.

Not blocked. Testing mode: existing-service-strict.
