# Tasks — value-layout-locality

## 0. Gate (blocking — nothing below starts until this fires)

- [ ] 0.1 Add benchmarks exercising `Vector` construction, `Conj`, and
      indexed reads past `vectorFlatThreshold` (32) — e.g. 100, 1,000,
      10,000 elements — since nothing in the repo today measures the trie
      representation at all.
- [ ] 0.2 Measure current `B/op`/`allocs/op` for those benchmarks as the
      baseline. Measure the accounted ledger bytes (via
      `core.ValueDeepBytes`/`ValueSlotsBytes` on a `Vector`) alongside the
      real Go-runtime bytes, to establish the accounted/real split this
      change must not disturb.
- [ ] 0.3 Gate fires iff a realistic large-vector workload shows a
      measurable win from the `Vector`/`vecNode` layout changes (not merely
      a theoretical size-class crossing) — record the actual numbers. If it
      does not fire for the collection-layout items, still evaluate the
      `ConstCharges` map removal and `OpTrue`/`OpFalse` singleton reuse
      independently: these are dispatch-loop-local and may be worth doing
      even if large-vector layout is not (record separately).

## 1. Accounted-bytes invariant (before any struct size changes)

- [ ] 1.1 Write a test pinning that `core.ValueDeepBytes`/`ValueSlotsBytes`
      for `Vector`, and the fixed size table entries feeding them
      (ADR 0011), are independent of `unsafe.Sizeof(Vector{})` — i.e. a
      struct layout change cannot silently move a ledger figure. This test
      must exist and pass before task 2 touches any struct.

## 2. Implementation (only for items the gate at 0.3 authorizes)

- [ ] 2.1 `Vector`: narrow `shift` to `uint8`, `count` to `int32`; confirm
      `unsafe.Sizeof(Vector{})` == 64 and the accounted ledger size is
      unchanged (per 1.1's test).
- [ ] 2.2 `chunk.ConstCharges`: replace `map[int]int64` with a dense
      `[]int64` parallel to `Constants`, indexed identically; update
      `AddChargedConstant` (`core/vm/chunk.go:257-263`, the charge-recording
      wrapper around `AddConstant`, `chunk.go:246-253`) and the dispatch-loop
      read (`vm.go:811`).
- [ ] 2.3 `OpTrue`/`OpFalse` (`vm.go:800,803`): push `core.True`/`core.False`
      instead of constructing `core.Bool{V: …}`.
- [ ] 2.4 `vecNode` split (only if 0.3's numbers justify the larger surface):
      distinct internal/leaf node types or a discriminated union eliminating
      the always-nil field; `Conj`'s path-copy updated accordingly.

## 3. Verify

- [ ] 3.1 Full floor: build/vet/gofmt/lint, full suite, `-race`, crossval,
      goldset both modes non-increasing on every existing cell (none are
      expected to move, since none exercise large vectors — confirm this
      expectation holds).
- [ ] 3.2 Interleaved benchstat vs. 0.2's baseline on the new large-vector
      benchmarks: measurable win, matching or exceeding what justified the
      gate firing.
- [ ] 3.3 Ledger-accounting test from 1.1 still passes after every struct
      change in task 2.
- [ ] 3.4 `openspec validate --strict` on this change.
