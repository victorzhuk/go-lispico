## 0. Baseline

- [x] 0.1 Record the current state of the record: every non-test call site of `HashMapShallowBytes` and `MeterCollectionHeaderBytes`, and for each, whether it prices a value being constructed or returned, or scratch storage discarded at return. Name which of them the published table in `docs/adr/0011-reduction-and-allocation-metering.md` accounts for and which it does not. Record the conversion's current charge at n = 9, 100 and 1000, so the size of any move is measurable — the figures the archived `trie-conversion-charge-determinism` recorded under its task 2.2 are the reference. Re-measure rather than quote: since `hashmap-builder-trie-conversion` landed its per-value memo, a repeated `Assoc` against one retained receiver no longer reconverts, so the conversion charge is only observable on a receiver that has not yet been converted — `BenchmarkHashMapFanOutAssoc`'s `builder/first` sub-arm is the shape that measures it.

## 1. Decision

- [x] 1.1 Choose the unit that prices an evaluator-side construction buffer, on the evidence of 0.1: either converge on the 24-byte slice-header unit that `core/reader.go` (at `admitAlloc(MeterCollectionHeaderBytes)`) and `core/eval.go` (at `chargeAllocBytes(MeterCollectionHeaderBytes)`) already charge for construction storage, or keep `HashMapShallowBytes`' 32-byte header and record why a `[]entry` buffer sized from a hash map is priced as a hash map. Record why the other was rejected. The decision governs whether this change touches Go at all, so make it before the failing contract in 2.1 is written, and state which of the two the rest of these tasks apply to.

  Surveyed at `3cbcaa3`, whose Go tree is identical to `cbbb0f3`: 16 non-test sites —
  11 `HashMapShallowBytes` calls, 5 `MeterCollectionHeaderBytes` uses. Fourteen price a
  value the call returns or binds and are table-owned. Two price storage discarded before
  the call returns, both inside `trieFromBuildMap`'s loop: the entry buffer at
  `core/types.go:1099`, which no table row owns, and the superseded path copies through
  `hamtSizeBytes` at `core/types.go:775`, already owned by the *Evaluator persistent-map
  node* row and already headed with 24. The reader's `nodePlan` / `formPlan` / `entryPlan`
  are discarded at return too but use neither symbol — `growthPlan.admit` charges
  `capacity * unit` with no header term — so they are neither precedent nor
  counter-example. Exactly one header-bearing charge for storage discarded at return has
  no row in the published table. Conversion charge re-measured at the pre-change unit:
  first-`Assoc` 4400 / 101328 / 1145936 at n = 9 / 100 / 1000, `trieFromBuildMap`'s own
  return 100704 at n = 100 and 1144336 at n = 1000, buffer term `32 + 64n`.

  **Ruling: `MeterCollectionHeaderBytes` (24)**, applied at `core/types.go:1099`; tasks 2.1
  onward apply to that unit. Derivability — the published rule stays "construction headers
  are 24", which the promoted-map and persistent-map node rows already state. Shape — a Go
  slice header is 24, and no `*HashMap` is constructed here. It does not under-count —
  `entry` is exactly 64 on ADR 0011's 64-bit basis and `sortedEntries` allocates with
  `cap == n`, so `24 + 64n` is the nominal footprint, and the ADR's list/vector bullet
  already reads 24 as one slice header. And it leaves ADR 0011 with one evaluator
  construction header instead of two.

  **Rejected: `MeterHashMapHeaderBytes` (32).** Its only argument is that the buffer is
  sized from a hash map's contents, which prices provenance rather than storage. Keeping it
  would make the table publish a distinction running opposite to the Go shapes: a real Go
  map charged 24 on the reader path, a plain `[]entry` charged 32 on the evaluator path.

## 2. Failing contract and fix

- [x] 2.1 If 1.1 moves the unit, use existing-service-strict testing: add a test in `core` that pins the conversion's buffer term to the chosen unit — build a builder-form map above `hashMapSmallLimit`, take one `Assoc`, and require the charge to equal the chosen header plus the per-entry term plus the path-copy sum. Verify it fails at base. If 1.1 keeps the unit, add the same isolation test in its passing direction instead of waiving it: `TestHashMap_ConversionChargeIsReproducible` does NOT pin this value — every assertion it makes (later-update equality, cross-receiver equality, and the honesty floor, which itself moves with the unit) passes identically at 24, so nothing in the suite would catch the header flipping. Either outcome leaves behind a test that fails if the chosen unit changes.
- [x] 2.2 Apply the decision at the one site that prices scratch storage — the line `bytes := HashMapShallowBytes(len(entries))` inside `(*HashMap).trieFromBuildMap` in `core/types.go`; locate it by that string, not by a line number. Verify the resulting trie is unchanged in shape and contents, that `TestHashMap_ConversionChargeIsReproducible` and `TestHashMap_ConversionChargeIgnoresBuildOrder` stay green, and that the charge still exceeds `retainedTrieBytes(root)` plus the buffer term — attributing a charge must not become a way of dropping one.

## 3. Documentation and verification

- [x] 3.1 Give the evaluator-side construction buffer a row in ADR 0011's fixed size table, and reconcile the paragraph below the table. This task is unconditional — it runs whichever unit 1.1 picks — because that paragraph today both scopes the 24-byte construction header to the reader's map builder alone and closes "The conversion publishes no unit of its own ... and the table gains no row for it", which is true of the trie nodes it describes and false of the entry buffer charged on the same call. If 1.1 converged the units, say the two builders charge one unit; if it kept them apart, state the distinction that makes them different storage. Verify no unit is published that no table row owns, and that no row is added that nothing charges.
- [x] 3.2 If the charged value moved, record it in `CHANGELOG.md` under `[Unreleased]`, giving the shape of the move rather than a byte count that depends on the key set. If it did not move, add no entry.
- [x] 3.3 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then `go test -race -timeout 2m -p 2 -parallel 2 ./core`, then `golangci-lint run ./core/...`, then `make build`, `make lint` and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; record pass evidence. Name every exact-byte expectation in `core`, `plugins/stdlib` and `runtime` that crossed a conversion and moved.
- [x] 3.4 Compare `TestGoldsetVMAllocations` and `BenchmarkGoldsetParse` against the pre-change baseline in both evaluator modes and confirm no ADR 0008 threshold moves — no gold-set fixture builds a map above `hashMapSmallLimit`, so the expected delta is zero on both the bytes and the allocation-count axes, and a non-zero one names the fixture that reached the conversion. Run `openspec validate conversion-buffer-charge-unit --strict --json`. Report allocation evidence separately from timing; latency deltas under this machine's measurement floor are not a verdict.
