# Tasks — vm-deadline-clock-cadence

## 0. Decide the mechanism

- [ ] 0.1 Measure both candidates on fib + a long-loop cell: (a)
      `time.AfterFunc`-armed atomic expiry flag, (b) clock read every Kth
      checkpoint (K=8 → ≤1024-instruction extra latency). Record ns/op,
      allocs (timer path must not add per-call allocs on short calls), and
      worst-case deadline-overrun latency for each. Pick the winner; record
      the loser's numbers in design.md.

## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (one session, `-count=10`): fib, Call,
      Callback, Rule with `-benchmem`; goldset both modes; `time.runtimeNow`
      flat share on fib (4.84% at HEAD 2026-07-27). Untouched-row control.

## 2. Implement

- [ ] 2.1 Implement the chosen mechanism behind the existing
      `deadlineArmed` state. Timer path: arm at first checkpoint that finds
      a timeout (today's lazy arming point), release on every run exit
      including terminal Reset and panic unwind — audit all exits; a leaked
      timer per call is a regression worse than the win.
- [ ] 2.2 Keep `ctx.Err()` at every checkpoint unchanged. Keep the
      already-cancelled-at-boundary rejection unchanged. While here: unify
      the clock source — `pollCancel` calls raw `time.Now()` (vm.go:703)
      where every other deadline site goes through `nowFunc` (vm.go:14,
      :433, :446); route the remaining reads through `nowFunc` so tests can
      fake the clock for the bound assertion in 3.1.
- [ ] 2.3 Deadline error shape unchanged (`vm: context deadline exceeded`
      wrapping — errors.Is compatibility pinned by existing tests).
- [ ] 2.4 Reentrant path: `resolveReentrantDeadline` (vm.go:436-446) still
      resolves the deadline lazily at first host observation — this change
      must not make short GoFunc dispatches read the clock.

## 3. Verify

- [ ] 3.1 Deadline tests: existing timeout/cancellation suite green; add a
      test asserting a deadline crossing mid-run terminates within the
      documented bound (timer path: fires without any further instruction
      budget; cadence path: within K checkpoints).
- [ ] 3.2 "Unobserved calls read no clock" scenario (runtime-api Boundary
      call efficiency) still green — short calls arm nothing.
- [ ] 3.3 Full floor: build, vet, lint, full suite, `-race`, crossval,
      goldset both modes non-increasing.
- [ ] 3.4 Interleaved benchstat vs 1.1: fib −5% or better, no row regresses.
