## 1. Pin the cost first

- [x] 1.1 `BenchmarkListAt` at n=32/100/1000 — confirms `At` is O(i) past the
      flat threshold: 2.5ns flat, ~24ns at 100, ~451ns at 1000.
- [x] 1.2 `BenchmarkQuasiquoteWideList` at n=16/64/256, straddling the
      threshold. n=16 is the control: below the threshold the backing is flat
      and `At` is already O(1), so that cell must not move.
- [x] 1.3 Baseline captured at `GOMAXPROCS=2`, `-benchtime=200ms`, `-count=6`,
      `TMPDIR` outside `/tmp`: 634ns / 3.96µs / 38.97µs, with allocs/op
      8 / 74 / 268.
- [x] 1.4 Identify the shape before fixing: allocations grow linearly while
      time grows quadratically, so the excess is traversal, not construction.

## 2. Find every site

- [x] 2.1 Audit positional indexing across `core/`, `core/vm/`,
      `core/compiler/`, and `plugins/`. Eight `At(i)`-in-loop sites found.
- [x] 2.2 Five are on the `Vector` branch (`core/depth.go:71,133,194,237,292`)
      where `At` is O(1) — not defects, left alone. `plugins/stdlib/collections.go:181`
      and `core/eval.go:410,760` are likewise Vector. Recording this so the
      audit does not have to be redone to prove they were considered.
- [x] 2.3 Three are on `List`: `core/eval.go:705`, `core/eval.go:721`,
      `core/dialect.go:511`.

## 3. The fix

- [x] 3.1 Each of the three iterates with the existing `listCursor` instead of
      `At(i)`. No new helper — `cursor()`/`next()` already exist and
      `boundedEquals` already uses them.
- [x] 3.2 `dialect.go` takes the clause test from the same cursor before the
      body loop rather than re-reading position 0.
- [x] 3.3 Comment the WHY at each site: positional indexing restarts the walk
      on a shared chain.

## 4. Measure

- [x] 4.1 Paired capture, same parameters. n=256 **−74.61%** (38.97µs →
      9.89µs, p=0.002), n=64 −30.49% (p=0.002).
- [x] 4.2 The control held: n=16 is `~` (p=0.180). A change here would have
      meant the fix was doing something other than advertised.
- [x] 4.3 `B/op` and `allocs/op` identical at every size (p=1.000, "all samples
      are equal") — the fix changed the traversal and nothing else.

## 5. The asymmetry, investigated

- [x] 5.1 Tested `NewList` always-flat, mirroring `NewVector`. It fails
      `TestSequenceOperationsAgainstReference/boundary_length_1000` with
      `Cons: byte cost = 40040, want 40`: building flat defers a whole-list
      promotion onto the first `Cons`, which the sequence-extension bound
      forbids.
- [x] 5.2 Conclusion recorded in the proposal and in a comment on `NewList`:
      the asymmetry is required, not accidental. `Cons` on a list must stay
      O(1); indexed reads on a vector must stay O(1); the two constructors
      differ because those obligations differ. Closes the performance
      program's "promotion asymmetry" item as answered rather than fixed.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [x] 6.2 `go test ./... -count=1` 2441 passed; `-race` 2443 passed.
- [x] 6.3 Crossval `TestVMVsTreeWalker` 218 passed.
- [x] 6.4 `go test ./internal/goldset/ -count=1` 27 passed in both modes.
- [x] 6.5 `cmd/perfgate`: 22 PASS, 4 latency FAIL, none a regression. As
      predicted, no gold-set cell moved by work: allocs/op is identical on
      every cell, geomean +0.00%. Two of the four FAILs were timed cells at
      ~+9.7%, large enough not to wave away, so they were re-measured paired at
      `-count=12` and both came back `~` (`pipeline` p=0.514, `registry-fold`
      p=0.755) — the gate's figure came from comparing two separately captured
      files. The failing set again differs from the previous change's run,
      which is the signature of the filed `GoldsetParse/*` noise issue rather
      than of anything in this diff.
