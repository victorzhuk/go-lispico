## Why

`List` and `Vector` are copy-on-write over a Go slice (`core/types.go:173,181`):
every `cons`/`conj` allocates a full copy of the result. Accumulating N elements
therefore costs O(N²) real allocation, and because each result is charged to the
evaluation ledger by `ValueDeepBytes(result)`
(`plugins/stdlib/collections.go:652-663`), the *charged* bytes are quadratic too.
Under the default 64 MiB `MaxAllocationBytes` a plain accumulation loop becomes a
hard failure at around two thousand elements:

```
(loop ((i 0) (acc (quote ()))) (if (< i N) (recur (+ i 1) (cons i acc)) (count acc)))
N=1000 -> 1000
N=2000 -> eval: ResourceLimitError: allocation limit 67108864 bytes exceeded
N=4000 -> eval: ResourceLimitError: allocation limit 67108864 bytes exceeded
```

Before metering the same loop was quadratic in time but completed, so this is a
practical regression against v0.8.0 for accumulation-shaped rule code, and
accumulation is the first thing a consumer reaches for. `HashMap` already solved
its own version of this (small/large representation, requirement "Map
representation efficiency"); sequences never did.

Two halves are both required — either alone leaves the repro failing. Structural
sharing removes the copy; incremental charging stops the ledger from billing
shared substructure once per operation.

## What Changes

- `List` gains a persistent backing with shared tails: `cons` allocates one node
  referencing the existing list, with no element copy. `first`/`rest` stay O(1).
- `Vector` gains a persistent indexed backing (bit-partitioned trie) so `conj`
  shares structure and index reads stay effectively constant-time. Small
  sequences keep a flat representation, exactly as `HashMap` keeps a small-map
  form, so short-lived literals pay no trie overhead.
- Representation is invisible: equality, iteration order, printing, `count`,
  `nth`, immutability, and both evaluators' results are unchanged. Neither the
  Lisp surface nor the Go `Value` interface changes shape.
- Allocation charging becomes incremental for structurally derived results: an
  operation charges the nodes it actually allocates, not a deep walk of a result
  whose bulk is shared with an argument already paid for. `ValueDeepBytes`
  survives for retained-state accounting, where a deep measure is the point.
- Depth-bounded construction (`CheckConstructionDepth`) and collection-length
  limits keep their current semantics — a shared-tail list is no deeper than the
  list it extends.

## Capabilities

### Modified Capabilities

- `core-engine`: new requirement `Sequence representation efficiency` (parallel
  to `Map representation efficiency`) — sharing, invisible promotion, O(1)
  `cons`; `Per-evaluation reduction and allocation counters` amended so
  structurally derived results charge incremental bytes.
- `stdlib-plugin`: new requirement for linear-cost sequence extension;
  `assoc charges constructed allocation deeply` amended for shared substructure.

Gold-set accumulation cells are added as part of this change's tasks; the
`consumer-release-gate` spec does not enumerate fixtures, so it needs no delta.

## Impact

- Code: `core/types.go` (List/Vector backing), `core/metering.go` charge helpers
  and their call sites, `plugins/stdlib/collections.go` (charge sites),
  `core/vm/vm.go` `OpMakeList`/`OpMakeVector`/`OpMakeMap` construction charges.
  The compiler is unaffected.
- Risk: `Vector` carries binding forms (`let`/`fn` params) and reader output —
  hot paths where a trie must not regress small-N access. Flat representation for
  small sequences plus benchstat cells on binding-heavy shapes is the control.
- Risk: charge accounting must stay monotone — an embedder metering a session
  must not be able to build an arbitrarily large shared structure while being
  billed once. Shared substructure is charged at creation; each operation charges
  its own new nodes.
- Sequencing: independent of the release-blocker fixes; the largest change of the
  current set, and it wants its own benchstat pass under both execution modes.
