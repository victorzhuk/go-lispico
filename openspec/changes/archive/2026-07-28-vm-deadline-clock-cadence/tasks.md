# Tasks — vm-deadline-clock-cadence

## 0. Decide the mechanism

- [x] 0.1 **Revised during zapply planning (user-approved deviation,
      see design.md "Mechanism decision"):** mechanism 1 (timer-flag)
      rejected without an empirical build — a real correctness defect
      (pooled-VM cross-call false-positive deadline errors from a
      `time.AfterFunc` callback racing an unrelated call's `Reset()`) made
      building it to benchmark throwaway work, not a fair trial. Shipped
      mechanism 2 (reduced clock cadence) outright. K swept at {8, 16, 32};
      numbers and the chosen K (8) recorded in design.md "K sizing".

## 1. Pin the baseline

- [x] 1.1 **Partially superseded, see design.md "K sizing" sweep result.**
      `core/vm.BenchmarkFibonacci_VM` (the fib row this task names) never
      arms a deadline, so it cannot measure this change at all — confirmed
      identical before/after as expected (the "short evaluations stay
      clock-free" invariant, not evidence about the cadence gate). A
      throwaway `runtime`-level benchmark that does exercise the armed-
      deadline path found no K value with a reliable win on this
      workstation (noise-level differences, one apparent win at n=8 that
      did not replicate at n=15) — recorded in design.md. Call/Callback/
      Rule/goldset baselines not separately captured: this change's diff is
      confined to `pollCancel`'s deadline-armed branch, which none of those
      cells exercise either (no benchmark or gold-set fixture in this repo
      arms a deadline). The real verdict is deferred to the hosted release
      runner per `release-gate-activation`.

## 2. Implement

- [x] 2.1 Implemented as a countdown field (`deadlineClockPolls`) inside
      `pollCancel`, not a timer (mechanism 1 rejected, see 0.1) — no timer
      lifecycle to audit. Countdown resets to zero (meaning "due now") in
      `SetDeadline`/`SetTimeout` directly (added during review — the three
      `reset()`/`Reset()`/`ResetIncremental()` paths also zero it, but
      correctness no longer depends on caller discipline to Reset before
      re-arming) and in all three reset paths; `Apply`'s fresh-VM copy
      deliberately does not copy it (commented).
- [x] 2.2 `ctx.Err()` unchanged, checked unconditionally every poll,
      never gated behind the cadence counter (verified in review by both
      the quality and perf lenses independently). Clock source unified:
      `pollCancel`'s deadline compare now reads `nowFunc()`, not raw
      `time.Now()`.
- [x] 2.3 Deadline error shape unchanged (`vm: context deadline exceeded`,
      `errors.Is` compatibility) — existing tests pin this, still green.
- [x] 2.4 `resolveReentrantDeadline` untouched; confirmed via
      `TestVM_ReentrantCtx_NoClockReadWhenGoFuncNeverObservesState` (still
      green) that a GoFunc/Lambda dispatch through `apply` never touches
      `pollCancel` or the new counter.

## 3. Verify

- [x] 3.1 Existing timeout/cancellation suite green (11 test files spot-
      checked by name, all pass). Four new tests added:
      `TestVM_PollCancel_ClockReadCadence` (table-driven over n ∈
      {1,8,9,16,17,37} — the boundary values discriminate a reset-to-K vs.
      the correct reset-to-K-1 off-by-one, added during review),
      `TestVM_DeadlineCrossing_BoundedPollsAfterExpiry`,
      `TestVM_ArmedDeadline_CtxCancellationEveryCheckpoint` (rewritten
      during review from a wall-clock-timing assertion to a deterministic
      one that actually fails on the regression it targets),
      `TestVM_PooledReuse_CadenceCounterResetsPerRun`.
- [x] 3.2 `TestVM_ReentrantCtx_NoClockReadWhenGoFuncNeverObservesState`
      (core) and the `runtime/lazy_deadline_test.go` pair (runtime-api
      "Boundary call efficiency: unobserved calls read no clock") both
      green.
- [x] 3.3 Full floor green, re-run independently by the orchestrator (not
      just the implementing agent): `go build ./...`, `go vet ./...`,
      `gofmt -l .` (empty), `golangci-lint run` (0 issues),
      `go test ./... -count=1`, `go test ./... -race -count=1`,
      `TestVMVsTreeWalker` crossval, `GOLDSET_MODE=eval`/`vm` both green.
      `cmd/perfgate` intentionally not run (known local false-FAIL).
- [x] 3.4 **Bar not measurable as literally stated, see 1.1/design.md.**
      `BenchmarkFibonacci_VM` never arms a deadline; no reliable K-dependent
      win found on this workstation via the throwaway runtime-level
      benchmark. No row regresses (full floor green); the −5% claim is
      unverified pending the hosted release runner.
