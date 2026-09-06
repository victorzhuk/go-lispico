## 1. Pin the defect

- [ ] 1.1 Pin that a sequence of short Builtin calls under an armed deadline reads
  the wall clock a bounded fraction of the times it synchronizes, rather than once
  per call. Package `core` has no clock seam today; introduce one, as `core/vm`
  did for the same contract.
- [ ] 1.2 Pin that an expired deadline still terminates Builtin work within the
  documented number of synchronizations, with the error shape unchanged.
- [ ] 1.3 Pin that caller cancellation and the Reduction charge still occur at
  every synchronization, never gated behind the cadence.
- [ ] 1.4 Pin that installing a deadline makes the next synchronization read the
  clock, so an evaluation cannot inherit a cadence position from an earlier one.

## 2. Amortize the clock read

- [ ] 2.1 Carry the cadence on `evalState` rather than on `BuiltinWorkBudget`, and
  verify a budget constructed per GoFunc call no longer reads the clock on each
  flush. Choose the fixed multiple to match `deadlineClockCadence`
  (`core/vm/vm.go:836`) unless a measurement argues otherwise, and state which.
- [ ] 2.2 Gate only the clock read in `flushPending`. Verify by test that
  `chargeReductions` and `b.ctx.Err()` remain unconditional.
- [ ] 2.3 Reset the cadence at both sites that install a deadline
  (`core/eval.go:266`, `core/eval.go:403`) rather than relying on caller
  discipline, matching how `SetDeadline`/`SetTimeout` do it in the VM.

## 3. Verify the cost is gone

- [ ] 3.1 Profile `Goldset/queue-promote` under the VM against a `v0.12.0` build
  and verify `time.runtimeNow` falls from ~11% of the profile toward the ~0.5%
  `v0.12.0` carried. Anchor the benchmark pattern: an unanchored `Goldset/...`
  also matches `GoldsetParse`. Do not use wall-clock A/B or `git bisect` for this
  verdict — this workstation's noise band swallows the signal, and a bisect over
  this range has already returned a test-only commit.
- [ ] 3.2 Run the repository test suite, the race suite over `core`, `plugins` and
  `runtime`, `go vet`, and the linter; verify every command exits successfully.
- [ ] 3.3 Verify the gold set passes under both execution modes and that the
  committed VM allocation pins in `internal/goldset/alloc_test.go` are unchanged —
  this change moves no allocation.

## 4. Verify on the runner

- [ ] 4.1 Dispatch the release gate against the candidate tree and verify it
  reports non-regression, no longer fails `Goldset/queue-promote`, and reaches
  exit 0 so a baseline would be stored.
