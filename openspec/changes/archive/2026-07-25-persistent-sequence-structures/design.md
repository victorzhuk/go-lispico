# Design

## Constraints that shape the choice

- `core/` has zero external imports, so the backing is hand-written; no
  third-party persistent-collection library.
- `List` and `Vector` are value types (`struct{ Items []Value }`) copied by
  assignment throughout the codebase and stored in `Value` interface values.
  Changing the field set is a wide but mechanical edit; making them pointer types
  would change nil handling and equality at every use site and is rejected.
- Immutability is a published invariant. Sharing is sound only because no
  operation mutates a reachable node — every writer allocates the path it
  changes.
- Reader output, `let`/`fn` binding carriers, and macro expansion all build small
  sequences on hot paths. Small-N cost is an acceptance criterion, not an
  afterthought.

## Representation

Both types keep a flat `Items []Value` at or below a length threshold (the same
shape the map uses for its small form; start at 32 and let benchstat decide).
Above it:

- `List`: chain of shared tails, `{head Value, tail *listNode, count int}`.
  `cons` allocates one node; `first`/`rest` are field reads; `count` stays O(1)
  because each node caches its length. Random access stays O(n), which is what a
  list already costs.
- `Vector`: bit-partitioned trie (branching factor 32) with a tail buffer. `conj`
  writes into the tail, pushing a full tail into the trie once per 32 elements;
  an index read walks at most log32(n) levels.

Promotion (flat → shared) happens on the operation that crosses the threshold and
is invisible: equality, iteration order, printing, and immutability are defined on
the logical sequence, never on the representation. There is no demotion — `rest`
of a shared list is a shared list.

## Charging

Today `chargeCollectionResult` calls `ChargeEvalAllocBytes(ValueDeepBytes(res))`,
so every operation pays for the whole result. With sharing that alone would keep
the repro failing: the charged bytes stay quadratic even when the allocation is
not. The ledger rule becomes:

- An operation that structurally derives its result from an argument charges only
  the nodes it allocated — one list node, one trie path, one tail slot — computed
  from the operation, not from a walk of the result.
- An operation that builds a fresh sequence from unrelated values (`list`,
  `vector`, `range`, `json/decode`) keeps its deep charge: those bytes are new.
- Retained accounting (`Env` capacity, `RetainedUsage`) keeps `ValueDeepBytes`.
  Retention is about what a binding holds alive, and shared structure held by a
  binding is alive.

Monotonicity holds because shared bytes were charged when they were created, so
the per-evaluation sum stays a faithful upper bound on newly allocated bytes. The
case to test explicitly is a loop that repeatedly conses onto the same base and
drops the result: each iteration charges its own node, which is correct and
bounded.

## Rejected alternatives

- Delta-charge only, keeping copy-on-write: ~3x headroom moves the cliff to ~6K
  elements without removing the O(N²) allocation. Rejected as a half fix — though
  it is exactly the charging half of this change.
- Raising the default ceiling: hides the asymptotics behind a bigger number and
  makes the eventual failure land on a bigger workload.
- Mutable transients for builders: an explicit escape hatch changes the surface
  and the immutability invariant. If profiling later demands it, that is its own
  decision.
- Pointer-typed `List`/`Vector`: touches nil semantics and equality everywhere.

## Verification

- Parity: crossval suite and gold set unchanged under both execution modes;
  results are representation-independent by construction.
- Property tests: for random operation sequences, a shared sequence and a naive
  slice-backed reference agree on `count`, `nth`, iteration, equality, printing.
- The proposal's repro must complete at N = 100,000 with room under the default
  ledger.
- Benchstat: small-N construction and binding-form paths (regression guard),
  accumulation shapes (the win), index reads at 32/1k/100k, `reverse`/`concat`/
  `map` folds, plus the gold set's own cells.
