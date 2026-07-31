# Tasks — value-layout-locality

## Gate record (2026-07-31)

Measured on `core/bench_test.go`'s five new large-vector benchmarks at
`-benchtime=1000x -count=2`. `B/op` and `allocs/op` are the decidable axes on
this box; `ns/op` is release-runner-only and is not quoted as evidence. A
`-cpu=1` control run made `B/op` bit-identical across every count block,
confirming the few-byte spread under default `GOMAXPROCS=24` is background
noise in the process-wide counter, not benchmark contamination — `allocs/op`
never varied.

Raw layout figures: `unsafe.Sizeof(Vector{})` = 72, `unsafe.Sizeof(vecNode{})`
= 48 (measured out-of-tree; deliberately not committed as an assertion, per
task 1.1's separation of the ledger from the real layout).

**Size-class premise — CONFIRMED.** `BenchmarkVectorRetentionBoxed` boxes 1000
distinct trie-form `Vector`s per op and allocates exactly 1000 times per op at
every size, for 80000 `B/op` — 80 bytes per boxed `Vector`, at n=100, n=1000
and n=10000 alike. The 72-byte struct does land in the 80-byte size class, so
a 64-byte struct would cross into the 64-byte class: a predicted 20% cut on
this benchmark, independent of element count.

**`vecNode` split (2.4) — does not fire.** A 10,000-element trie holds 323
`vecNode`s. Eliminating the always-nil half of each (48 → 24 bytes) recovers
323 × 24 = 7,752 bytes against `BenchmarkVectorConstructBatched`'s 449,056
`B/op` at the same size: **1.7%**. The one-at-a-time shape is worse still —
6,025,594 `B/op`, where `Conj`'s per-call tail copy dominates and the node
headers are 0.13% of the total. `vm-register-dispatch` was closed as
not-needed at a measured 4.5%; 1.7% for a discriminated-union rewrite of the
trie plus `Conj`'s path-copy is not a case this repo makes.

**`OpTrue`/`OpFalse` singleton reuse (2.3) — does not fire, and the premise is
wrong.** `core.Bool{V: false}` at `vm.go:894` is a constant composite literal,
so the compiler never calls `runtime.convT` for it: the emitted code is `MOVBLZX
runtime.zeroVal(SB), DI` / `LEAQ runtime.staticuint64s(SB), R11` / `LEAQ
(R11)(DI*8), DI`. It allocates nothing today. Pushing the pre-boxed
`core.True`/`core.False` would save no allocation and no accounted byte
(`ValueShallowBytes` returns `MeterScalarBytes` for any `Bool`).

**`ConstCharges` map removal (2.2) — ledger-clean, but its benefit axis is not
decidable here.** `chunkDeepBytes` (`core/compiler/compiler.go:871-887`) never
references `ConstCharges`; it was never on the ledger, so a representation
change moves no accounted byte. A map read allocates nothing either, so the
change has no `B/op` signature at all — its whole benefit is dispatch-loop
`ns/op`, the axis this box cannot decide.

## 0. Gate (blocking — nothing below starts until this fires)

- [x] 0.1 Add benchmarks exercising `Vector` construction, `Conj`, and
      indexed reads past `vectorFlatThreshold` (32) — e.g. 100, 1,000,
      10,000 elements — since nothing in the repo today measures the trie
      representation at all.
- [x] 0.2 Measure current `B/op`/`allocs/op` for those benchmarks as the
      baseline. Measure the accounted ledger bytes (via
      `core.ValueDeepBytes`/`ValueSlotsBytes` on a `Vector`) alongside the
      real Go-runtime bytes, to establish the accounted/real split this
      change must not disturb.
- [x] 0.3 Gate fires iff a realistic large-vector workload shows a
      measurable win from the `Vector`/`vecNode` layout changes (not merely
      a theoretical size-class crossing) — record the actual numbers. If it
      does not fire for the collection-layout items, still evaluate the
      `ConstCharges` map removal and `OpTrue`/`OpFalse` singleton reuse
      independently: these are dispatch-loop-local and may be worth doing
      even if large-vector layout is not (record separately).

      **Verdict: fires for 2.1 only.** The size-class crossing is not
      theoretical here — `BenchmarkVectorRetentionBoxed` isolates the boxed
      `Vector` header from its element payload and reads exactly 80 bytes per
      retained vector at every size, so 64 bytes is a measured 20% on the
      shape the proposal named as its beneficiary. 2.2, 2.3 and 2.4 are closed
      on the evidence in the gate record above.

## 1. Accounted-bytes invariant (before any struct size changes)

- [x] 1.1 Write a test pinning that `core.ValueDeepBytes`/`ValueSlotsBytes`
      for `Vector`, and the fixed size table entries feeding them
      (ADR 0011), are independent of `unsafe.Sizeof(Vector{})` — i.e. a
      struct layout change cannot silently move a ledger figure. This test
      must exist and pass before task 2 touches any struct.

## 2. Implementation (only for items the gate at 0.3 authorizes)

- [x] 2.1 `Vector`: narrow `shift` to `uint8`, `count` to `int32`; confirm
      `unsafe.Sizeof(Vector{})` == 64 in a test separate from 1.1's, and that
      the accounted ledger size is unchanged (per 1.1's test). The resulting
      2³¹ length cap must fail closed, not wrap.

      The narrowing alone is sufficient; the declaration order is unchanged.
      An intermediate version of this task claimed a field reorder was
      required, on the reasoning that `tail` would need 8-byte realignment
      after a 1-byte `shift` — measured wrong: in the original order `shift`
      sits at offset 32, `count` at 36, and `tail` lands at 40, already
      8-aligned, for 64 bytes. Verified against three struct variants:
      original order at original widths 72, original order at narrowed widths
      64, reordered at narrowed widths 64. Width crosses the size class;
      order contributes nothing.

      **Known limit on the fail-closed guarantee.** The guard rejects a
      configured `MaxCollectionLen` above `math.MaxInt32` at engine
      construction, which covers every path an embedder reaches through
      `runtime.New`. It does not cover an embedder importing `core` directly
      and driving `Vector.Conj` past 2³¹ elements: `Conj` returns
      `(Vector, int64)` with no error slot, so the count would wrap rather
      than fail. Adding an error return is a public API change, outside this
      change's scope. Reaching it needs tens of gigabytes in a single vector,
      four orders of magnitude past the 10,000,000 default ceiling — recorded
      as a limit rather than closed, so the gap is on the record instead of
      being implied away by the guard's existence.
- [ ] 2.2 `chunk.ConstCharges`: replace `map[int]int64` with a dense
      `[]int64` parallel to `Constants`, indexed identically; update
      `AddChargedConstant` (`core/vm/chunk.go:268-275`, the charge-recording
      wrapper around `AddConstant`, `chunk.go:257-265`) and the dispatch-loop
      read (`vm.go:899-902`).

      **Not authorized — closed on reachability.** `OpConstCharged` is emitted
      only by `compileConstantCollection` for a folded constant collection
      (`compiler.go:1195-1211`). Across every gold-set fixture the folded
      literals — `[]` in `queue-promote`, `{:mode …}` in `merge-config`,
      the tool vector in `registry-fold`, `[1 2 … 10]` in `pipeline` — sit in
      top-level or one-shot bindings, never inside a loop body. The opcode
      executes once per literal per program, so "the only map hash lookup in
      the hot dispatch path" is true by position and false by frequency.
      Two further findings, recorded so a future revisit starts from them: a
      dense `[]int64` would also have to keep `Validate`'s presence check
      (`chunk.go:352`) working — `validate_test.go:33-38` pins that a chunk
      with no recorded charge is rejected, and a bare slice collapses "absent"
      into "charge 0" (viable, since `AddChargedConstant` is unreachable with
      charge 0, but it must be deliberate) — and the dispatch read would need
      an `idx < len(…)` guard, because `core/vm` is public and a hand-built
      chunk reaching `Run` without `Validate` turns today's safe nil-map read
      into an index panic.
- [ ] 2.3 `OpTrue`/`OpFalse` (`vm.go:890,893`): push `core.True`/`core.False`
      instead of constructing `core.Bool{V: …}`.

      **Not authorized — the premise is false.** `core.Bool{V: …}` here is a
      constant composite literal, so no `runtime.convT` call is emitted and
      nothing is allocated; see the gate record above for the emitted
      instructions. There is no allocation to remove.
- [ ] 2.4 `vecNode` split (only if 0.3's numbers justify the larger surface):
      distinct internal/leaf node types or a discriminated union eliminating
      the always-nil field; `Conj`'s path-copy updated accordingly.

      **Not authorized — 1.7%, measured.** See the gate record above. The
      always-nil half is real, but recovering it is worth 7,752 bytes against
      449,056 `B/op`, and `Conj`'s per-call tail copy dominates the shape
      where vectors are actually built.

## 3. Verify

- [x] 3.1 Full floor: build/vet/gofmt/lint, full suite, `-race`, crossval,
      goldset both modes non-increasing on every existing cell (none are
      expected to move, since none exercise large vectors — confirm this
      expectation holds).

      Floor green: `make build`, `make lint` (0 issues), `gofmt -l .` empty,
      `go vet ./...`, `make test`, `go test -race ./...`. Crossval rides in
      `make test`. The stated expectation was wrong and the check caught it:
      `queue-promote` does exercise the trie, and it moved — down 2.9% (eval)
      and 3.9% (vm). Every other cell moved down or stayed flat in both modes
      and in `BenchmarkGoldsetParse`; nothing moved up anywhere.
- [x] 3.2 Interleaved benchstat vs. 0.2's baseline on the new large-vector
      benchmarks: measurable win, matching or exceeding what justified the
      gate firing.

      `BenchmarkVectorRetentionBoxed` 80000 → 64000 `B/op` at n=100, 1,000 and
      10,000 alike — exactly the predicted 20%, against a firing rule of 15% —
      with `allocs/op` unchanged at 1000. The other four benchmarks each moved
      exactly −16 `B/op`, one Vector header, confirming the saving is per
      boxed value and not per element. Interleaving does not apply to a
      before/after across a code change; instead both arms ran at `-cpu=1`,
      which makes `B/op` bit-identical across count blocks, so the comparison
      rests on exact figures rather than on a distribution.
- [x] 3.3 Ledger-accounting test from 1.1 still passes after every struct
      change in task 2.

      Passes unedited — `core/metering_test.go` was not touched by task 2.
- [x] 3.4 `openspec validate --strict` on this change.
