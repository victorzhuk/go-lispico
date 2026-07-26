## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (≥10 counts, one session): article Rule,
      Callback, Call rows with `-benchmem`; goldset both modes. Record the
      `time.runtimeNow` and `AdoptEvalStateWithMeter` profile shares on the
      Rule row as the before-picture (8.2% CPU / 20.3% bytes at dcbdf62).

## 2. Wrapper reuse across runs

- [ ] 2.1 Give the VM a run-generation counter bumped in `Reset` and
      `ResetIncremental`; stop zeroing `reentryCtx` in the reset paths.
- [ ] 2.2 On dispatch, reuse the retained wrapper iff its embedded outer
      context is interface-identical to the current run's outer context;
      re-arm per-evaluation fields (budget counters, deadline slot,
      generation) with plain field writes. Any mismatch → build fresh,
      exactly as today.
- [ ] 2.3 Wrapper state accesses (`Value(evalStateKey)`, deadline
      observation, budget charges) verify the generation and fall back to
      no-eval-state behavior when stale: outer-ctx delegation for
      `Deadline`/`Done`/`Err`/unrelated values, fresh-budget adoption on
      re-entry. No path may read another run's counters.

## 3. Observation-lazy deadline

- [ ] 3.1 Remove the `time.Now` from wrapper construction; store the timeout
      and compute the absolute deadline on first observation (a `Deadline()`
      call, a poll checkpoint comparison, or re-entrant adoption), cached in
      the wrapper under its existing atomics.
- [ ] 3.2 Verify a caller-supplied ctx with a tighter deadline still wins,
      per the existing arm-only-if-looser rule.

## 4. Adversarial retention tests

- [ ] 4.1 Retain-and-read: a GoFunc stores its ctx; after the call returns,
      `Deadline`/`Err`/`Value(evalStateKey)` on the stored ctx observe
      fail-safe behavior, never a later run's budget or deadline.
- [ ] 4.2 Retain-and-reenter: re-entering the engine with a stale-generation
      ctx runs with a fresh budget (documented misuse), and the enclosing
      current run's counters are unaffected. `-race` clean.
- [ ] 4.3 Same-ctx reuse hit: two sequential `Call`s with one outer ctx
      allocate the wrapper once (assert with `testing.AllocsPerRun`);
      different outer ctxs allocate per ctx.
- [ ] 4.4 Existing boundary-efficiency, deadline-ownership, and
      lazy-reentrant-state tests stay green unmodified — this change tightens
      cost, not semantics.

## 5. Measure

- [ ] 5.1 Re-run 1.1 interleaved with the baseline. Success criteria: Rule
      and Callback each drop one alloc and the wrapper bytes;
      `time.runtimeNow` disappears from the Rule profile for
      never-observing bodies; Call row flat; no goldset cell regresses.

## 6. Verify

- [ ] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [ ] 6.2 Full suite + `-race`; crossval `TestVMVsTreeWalker`; goldset both
      modes; `cmd/perfgate` one-sided non-regression green.
