## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (≥10 counts): article Call and Rule rows plus
      in-repo `BenchmarkEngine_FuncCall`/name-Call pair with `-benchmem`.
      Record the dcbdf62 spread: name 187ns / Fn 155ns / Pinned 152ns.
      Local host at dcbdf62 (`-benchtime=400ms -count=10`, benchstat):
      `Engine_CallBytecodeCanonical` 152.5n ± 3%, `Engine_FuncCall` 133.6n ± 4%,
      `Engine_PinnedFnCall` 134.8n ± 5%, all 32 B/op — a ~19ns resolution gap.

## 2. Cache

- [x] 2.1 Per-engine name→handle cache on `engineImpl`; `Call` hit path
      reuses the `Fn` call flow (cell read + cached stats counter), miss
      path resolves and populates. Lisp-2 resolves the function cell first
      and falls back to the value cell, matching `resolveHead`
      (core/eval.go:625-644); `Func` keeps its function-cell-only contract.
      Covers the newly-reachable `defun` case and the both-bound precedence
      case.
- [x] 2.2 Bound the cache (entry cap with flush-on-overflow or coarse LRU,
      matching the chunk-cache ceiling discipline); a flush is a
      performance event, never a correctness event.

## 3. Invalidation

- [x] 3.1 Guard entries by `{env identity, Env.NameGen()}`, re-resolving on
      mismatch. `Env.Rebuild` (core/env.go:844) is the only operation that
      changes the name→cell mapping, and it bumps the name generation
      (core/env.go:893); delete, redefine, `UnloadPlugin` and hot-reload are
      cell-mediated and need no hook. Drop unloaded names from the cache in
      `removePluginBindings` as hygiene, not for correctness.
- [x] 3.2 Scenario tests: redefine-then-call sees the new definition
      (cell-mediated); delete-then-call reports undefined;
      delete-then-`Rebuild`-then-redefine-then-call sees the new definition
      (the new-cell case — the one a stale cache would break; without the
      explicit `Rebuild` this scenario passes vacuously); unload-then-call
      reports undefined; hot-reload-then-call sees reloaded definitions.
      Each asserted equal to a fresh-resolution engine.
- [x] 3.3 Concurrent `Call`s during redefinition and reload under `-race`.

## 4. Measure

- [x] 4.1 Re-run 1.1 interleaved. Success criteria: name-based Call within
      ~5ns of `Fn.Call`; Rule row improves by the resolution share; no
      goldset cell regresses; `Stats()` counts remain exact.
      Interleaved A/B, 3 rounds × `-count=5` per side, `-benchtime=400ms`:
      `Engine_CallBytecodeCanonical` 158.1n → 144.9n (−8.35%, p=0.000, n=15);
      controls flat — `Engine_FuncCall` p=0.152, `Engine_PinnedFnCall` p=0.263.
      Resolution gap 20.2ns → 4.8ns, within the ~5ns criterion. 32 B/op and
      1 alloc/op unchanged. `Stats()` exactness covered by
      `TestCallCache_StatsExactnessAcrossForcedMiss`.
      Goldset non-regression is NOT locally evaluable — see 5.2.

## 5. Verify

- [x] 5.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean. All four verified in the worktree; lint reports 0 issues.
- [x] 5.2 Full suite + `-race`; crossval; goldset both modes; `cmd/perfgate`
      one-sided non-regression — local floor green, perfgate DEFERRED to the
      release gate (not passed locally; see below).
      Green locally on the rebased tree: full suite (2467 tests, 18
      packages), full `-race` (2469), crossval (`TestVMVsTreeWalker`, 218
      tests), goldset unit tests, `golangci-lint` 0 issues.
      `cmd/perfgate` NOT evaluable on this workstation — its noise floor
      exceeds the gate's 5% tolerance. Across four interleaved runs the
      failing cells never reproduced, and most sat in `GoldsetParse/*`
      (`core.Read` only, no engine), which a `runtime/`-only diff cannot
      affect; one fixture swapped which family failed between consecutive
      runs (`safe-parse` +6.28% parse / clean eval, then clean parse /
      +12.59% eval). Allocation counts were bit-identical throughout, so no
      semantic change is implied. The gate is authoritative on the release
      workflow's quiet 2-vCPU runner (`.github/workflows/release.yml`,
      GOMAXPROCS=2, BENCHTIME=200ms, stored per-release baseline); it must
      be evaluated there.
