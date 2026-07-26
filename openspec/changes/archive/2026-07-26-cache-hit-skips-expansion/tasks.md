## 1. Establish the defect observably

- [x] 1.1 Count expander-body invocations across four evaluations of one
      unchanged source, using a `GoFunc` called from the macro body. Before:
      1, 2, 3, 4. This is the observable the existing reuse scenario could not
      see, because the *result* is identical either way.
- [x] 1.2 Baseline the gold set under `GOLDSET_MODE=vm`, `GOMAXPROCS=2`,
      `-benchtime=400ms`, `-count=10`, `TMPDIR` outside `/tmp`.

## 2. The fix

- [x] 2.1 Move `MacroExpand` inside the cache-miss branch in `EvalCached`.
- [x] 2.2 Keep `PollEvalState` ahead of everything, so a cancelled evaluation
      is still refused before any work.
- [x] 2.3 Confirm `expanded` is dead on the hit path before moving it — it is
      referenced only by the compile call and the unsupported-form fallback,
      both inside the miss branch.
- [x] 2.4 Correct the `EvalCached` doc comment, which described the old order
      ("macro-expands, checks the chunk cache"). Leaving it would have
      documented behaviour the function no longer has.

## 3. Prove it

- [x] 3.1 `TestCache_HitSkipsMacroExpansion` asserts the expander runs exactly
      once across four evaluations. It fails on the pre-fix tree with the
      counts recorded in 1.1.
- [x] 3.2 The existing cache suite stays green unmodified — 24 tests, including
      `TestCache_MacroInvalidation`, `TestCache_MacroInvalidation_ViaCall`, and
      the epoch tests from the previous change.
- [x] 3.3 A form whose expansion fails cannot reach the hit path: a failed
      expansion never produced a chunk, so no error is skipped. Argued from the
      control flow rather than tested, since the state is unconstructible.

## 4. Measure

- [x] 4.1 Every gold-set cell improves, deterministic at ±0%, p=0.000:
      `twice-macro` 68 → 57 (−16.2%), `counter-closure` 69 → 64 (−7.3%),
      `rule-load` 189 → 177 (−6.4%), `guard-nil` 36 → 34 (−5.6%);
      geomean 62.14 → 60.78 (−2.20%).
- [x] 4.2 All 13 cells moving the same way is the expected shape for removing
      per-eval work from the hit path. A change confined to one or two cells
      would have meant something narrower was happening.

## 5. Verify

- [x] 5.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean — 0 issues.
- [x] 5.2 `go test ./... -count=1` 2442 passed; `-race` 2444 passed. One
      full-suite run tripped `TestDecodeHashMap_Scaling` at ratio 4.22 against
      its 3.0 bound; confirmed not this change's rather than assumed — it
      exercises `fromJSONValue` directly and never reaches `EvalCached`, and it
      passes 5/5 in isolation (ratios 1.31–2.94) with absolute times in the
      usual range. It is the filed wall-clock flake: a ratio of best-of-5
      timings taken under parallel suite load, which is an unstable statistic
      by construction.
- [x] 5.3 Crossval `TestVMVsTreeWalker` 218 passed.
- [x] 5.4 `go test ./internal/goldset/ -count=1` 27 passed in both modes.
- [x] 5.5 `cmd/perfgate`: 20 PASS, 6 FAIL — **all six FAILs are negative
      deltas, i.e. improvements**: `twice-macro` −19.95%, `counter-closure`
      −12.49%, `registry-fold` −12.44%, `merge-config` −5.94%,
      `GoldsetParse/merge-config` −8.04%, `GoldsetParse/guard-nil` −6.74%.
      Allocations are down or unchanged on every cell and up on none. This is
      the clearest case yet of the filed structural problem: `-mode
      non-regression` tests `math.Abs(delta) > tolerance`, so a change that
      makes the engine broadly faster cannot pass it. The gate needs a
      one-sided rule for the non-regression direction, or an explicit
      improvement allowance; not adjusted here, since changing a release gate
      to admit one's own change is the maintainer's call.
