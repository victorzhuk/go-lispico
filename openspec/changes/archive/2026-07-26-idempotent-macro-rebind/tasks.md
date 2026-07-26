## 1. Pin the current behaviour

- [x] 1.1 Baselined `BenchmarkGoldset/twice-macro` under `GOLDSET_MODE=vm`,
      `GOMAXPROCS=2`, `-benchtime=400ms`, `-count=10`, `TMPDIR` outside `/tmp`:
      103 allocs/op, 7.967 KiB/op, 9.262µs/op.
- [x] 1.2 `TestCache_IdenticalMacroRebindKeepsEpoch` written first and watched
      fail under **both** dialects, so the fix is demonstrably what makes it
      pass. The probe behind it — printing the epoch across four identical
      evaluations — showed it climbing 1→2→3→4.

## 2. The fix

- [x] 2.1 `evalDefmacro` bumps the epoch only when `macroRebindIsIdentical`
      returns false.
- [x] 2.2 The previous binding is resolved through the function cell first,
      then the value cell. This was not hypothetical: the first attempt checked
      only `GetFunc`, which made the whole change a no-op under the gold set's
      Clojure dialect and produced a flat benchmark that read as "worthless
      fix" rather than "fix never ran". Caught by asserting the epoch instead
      of trusting the timing.
- [x] 2.3 Compares name, defining `*Env` by pointer, variadic tail, parameters,
      and body element-wise via `Value.Equals`.
- [x] 2.4 WHY comments on both the skip and the lookup.
- [x] 2.5 **Not foreseen: the lookup must not materialize.** Going through
      `Env.Get`/`GetFunc` consults the lazy stdlib layer and materializes on
      miss, so a `defmacro` evaluated while the stdlib bootstrap was still
      loading re-entered it and deadlocked —
      `TestStdlibBootstrapCache_SecondEngineReusesArtifact` hung for the full
      test timeout. `lookupBoundMacro` walks the materialized cells directly.
      This is sound, not merely expedient: the lazy layer holds stdlib
      builtins, never a user macro, so a name it has not materialized cannot
      be an identical rebind.

## 3. Prove it does not serve a stale expansion

- [x] 3.1 `TestCache_MacroInvalidation` and
      `TestCache_MacroInvalidation_ViaCall` green, unmodified.
- [x] 3.2 `TestCache_ChangedMacroBodyInvalidates` — a changed body still bumps.
- [x] 3.3 Different defining scope still invalidates, covered as a
      `macroRebindIsIdentical` branch test in `core`. The runtime-level version
      of this test was **removed as unsound**: a `defmacro` inside a `let`
      binds the macro into that child env and bumps that same child's epoch,
      so it never reaches the root cache either way, and the test would have
      passed for a reason unrelated to what it claimed to check.
- [x] 3.4 `TestCache_IdenticalMacroRebindKeepsEpoch` runs under Lisp-1
      (Clojure) and Lisp-2 (default CL), asserting the epoch directly.
- [x] 3.5 `TestMacroRebindIsIdentical/body_too_deep_to_compare` — a body nested
      past `DefaultMaxStructuralDepth` compares unequal and therefore bumps.
      The fail-closed direction is confirmed by test, not assumed.

## 4. Measure

- [x] 4.1 `twice-macro` under the VM: allocs/op **103 → 68 (−33.98%)**,
      B/op 7.967 KiB → 5.864 KiB (−26.40%), sec/op 9.262µs → 7.194µs
      (−22.33%). allocs and bytes are deterministic at ±0%.
- [x] 4.2 Every other cell is unchanged — all 25 remaining gold-set cells
      report allocs/op identical at `p=1.000` ("all samples are equal"). The
      change moves the one cell that contains a `defmacro` and nothing else.

## 5. Verify

- [x] 5.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean — 0 issues.
- [x] 5.2 `go test ./... -count=1` 2441 passed; `-race` 2443 passed.
- [x] 5.3 Crossval `TestVMVsTreeWalker` 218 passed.
- [x] 5.4 `go test ./internal/goldset/ -count=1` 27 passed.
- [x] 5.5 **Perfgate: 20 PASS, 6 latency FAIL, none a regression.** allocs/op
      and B/op are identical on every cell except the intended `twice-macro`
      improvement. The FAILs are the two-sided ±5% latency rule firing on
      noise — `merge-config` reports −7.51% timed and +10.62% parse in the
      same run — plus, structurally, this change's own win:
      `Goldset/twice-macro-2: FAIL (latency delta -28.36%)`. A non-regression
      gate cannot express "this cell is supposed to get faster", so a change
      that improves a cell by design cannot pass it unaltered. Filed with the
      existing `GoldsetParse/*` noise follow-up; not adjusted here, because
      loosening a gate to admit one's own change is the wrong direction and
      the decision is the maintainer's.
