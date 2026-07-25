## 1. Fix the run parameters

- [ ] 1.1 Set `GOMAXPROCS: "2"` and `BENCHTIME: "200ms"` in
      `.github/workflows/release.yml`, replacing the empty values and the
      "not yet chosen" comment. Leave `BENCH_COUNT` at 10.
- [ ] 1.2 Record in the workflow, next to the values, that changing either
      invalidates every stored `bench-vm.txt` non-regression baseline.
- [ ] 1.3 Decide whether the automatic trigger can now be uncommented, or
      whether the gate's release-asset lookup still blocks it. Report which,
      with the line that decides it — do not enable it silently.

## 2. Harness

- [ ] 2.1 `make profile`: run `./internal/goldset/` once per `GOLDSET_MODE`
      (eval, vm) with the fixed parameters and `-benchmem`, writing
      `profiles/{eval,vm}.{cpu,mem}.prof`. Create `profiles/` the way `build`
      creates `bin/`.
- [ ] 2.2 `make profile-report`: `go tool pprof -top -nodecount=20` over each
      captured profile.
- [ ] 2.3 Add `profiles/` to `.gitignore`; confirm it stays untracked after a
      real run.

## 3. Baseline measurement

- [ ] 3.1 Capture on a quiet machine — nothing else CPU-heavy running. Verify
      each profile file is non-empty and that the bench output ends in `ok`
      before reading anything from it.
- [ ] 3.2 Write `docs/profiling-baseline.md`: date, commit hash, fixed
      parameters, and per mode the top CPU entries by flat and cumulative time
      plus the top allocation sites, each with a one-line reading.
- [ ] 3.3 Re-check and state explicitly whether global lookups still dominate,
      given the per-chunk site cache landed after the 2026-07-18 profile that
      measured 33%.
- [ ] 3.4 Re-check and state explicitly whether `nativeAdd` and its siblings
      still exceed Go's inline budget (previously cost 156-513 against ~80).
      Use `go build -gcflags='-m'` for the inlining question rather than
      inferring it from the profile.
- [ ] 3.5 Record the measurement discipline in the doc: quiet machine, timing
      variance above ~15% means re-measure, allocation counts are the
      deterministic signal when timings disagree.

## 4. Verify

- [ ] 4.1 Both targets run clean; `profiles/` untracked; committed baseline
      names its commit and date.
- [ ] 4.2 A second capture on a quiet machine reproduces the top entries in the
      same order. If it does not, the baseline is noise — say so rather than
      committing it.
- [ ] 4.3 Existing floor unchanged, not merely green: `go build ./...`,
      `go vet ./...`, `gofmt -l .`, `make lint`, `go test ./...`,
      `go test ./... -race`, crossval `TestVMVsTreeWalker`,
      `go test ./internal/goldset/`. No production code changed, so any
      movement here is a bug in this change.
- [ ] 4.4 Report which of Stage F (tree-walker modernization) and Stage G
      (native fast paths, constant folding) the baseline actually supports, and
      which it does not. Stage F is gated on this answer; a profile that fails
      to justify it is a valid and useful outcome.
