## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (≥10 counts, one session): article Rule,
      Callback, Call rows with `-benchmem`; goldset both modes. Record the
      `time.runtimeNow` and `AdoptEvalStateWithMeter` profile shares on the
      Rule row as the before-picture (8.2% CPU / 20.3% bytes at dcbdf62).
      The article Rule/Callback/Call rows live in the external comparison
      repo (`BenchmarkCall_Lispico`/`BenchmarkCallback_Lispico`), so the
      local stand-ins are the in-repo rows with the same shapes:
      `Engine_CallBytecode` is the GoFunc-dispatching row this change
      targets; `Engine_CallBytecodePlain`, `Engine_CallBytecodeCanonical`,
      `Engine_FuncCall`, `Engine_PinnedFnCall` are GoFunc-free controls that
      must stay flat. Baseline at eaf3da7 (`-benchtime=400ms -count=10`,
      benchstat): `Engine_CallBytecode` 288.0n ± 5%, 128 B/op, 2 allocs/op;
      all four controls 32 B/op, 1 alloc/op (`Plain` 133.9n ± 5%,
      `Canonical` 156.9n ± 9%, `FuncCall` 152.8n ± 4%,
      `PinnedFnCall` 151.8n ± 5%).
      Before-picture profile shares on `Engine_CallBytecode`
      (`-benchtime=3s`): CPU — `time.runtimeNow` 15.66% flat,
      `armDeadline` 17.45% cum, `AdoptEvalStateWithMeter` 12.36% cum;
      alloc_space — `AdoptEvalStateWithMeter` 74.58% of allocated bytes
      (the 96 B wrapper of the row's 128 B/op).

## 2. Wrapper reuse across runs

- [x] 2.1 Give the VM a run-generation counter bumped in `Reset` and
      `ResetIncremental`; stop zeroing `reentryCtx` in the reset paths.
      `vm.runGen atomic.Uint64`; the three `reentryCtx = nil` lines are gone,
      so `reentrantCtx` is the sole reuse-vs-rebuild decider.
      DEVIATION from the task text, required for correctness: the bump is NOT
      in the reset paths. Bumping only there leaves the VM's live generation
      equal to the wrapper's stamp once a call returns, so a retained ctx
      still reads the live counter and the guard is inert. The bump is at the
      top-level exits (`ApplyPooled`, `Run`) via `bumpRunGenIfWrapped`, gated
      on `reentryCtx != nil` so a GoFunc-free body pays no atomic. Invariant:
      a wrapper is live iff stamped during the currently-executing run.
      The reset-path bumps were additionally measured to cost 6-9% on the
      GoFunc-free control rows and were removed.
- [x] 2.2 On dispatch, reuse the retained wrapper iff its embedded outer
      context is interface-identical to the current run's outer context;
      re-arm per-evaluation fields (budget counters, deadline slot,
      generation) with plain field writes. Any mismatch → build fresh,
      exactly as today. Comparability hazard handled: Go's `==` panics when
      both interface values hold the same non-comparable dynamic type, and
      embedders supply arbitrary contexts, so comparability is tested once at
      build time via reflect and cached (`parentComparable`); the hot path
      never calls reflect and never compares an unproven type.
      Re-arm re-seeds resource limits explicitly rather than relying on
      `AdoptEvalStateWithMeter`'s short-circuit, which was written for
      intra-run reuse and does not re-seed.
- [x] 2.3 Wrapper state accesses (`Value(evalStateKey)`, deadline
      observation, budget charges) verify the generation and fall back to
      no-eval-state behavior when stale: outer-ctx delegation for
      `Deadline`/`Done`/`Err`/unrelated values, fresh-budget adoption on
      re-entry. No path may read another run's counters. All three access
      paths are guarded, including the two that bypass `Value()` entirely:
      `EvalCallCounter`'s wrapper type-assert and `EvalMeterIfMaterialized`.
      "Plain field writes" per 2.2 proved unsafe and was superseded: the
      re-armed fields are `atomic.Int64`, and every read path takes a
      seqlock-style generation re-check (see 4.5).

## 3. Observation-lazy deadline

- [x] 3.1 Remove the `time.Now` from wrapper construction; store the timeout
      and compute the absolute deadline on first observation (a `Deadline()`
      call, a poll checkpoint comparison, or re-entrant adoption), cached in
      the wrapper under its existing atomics. The timeout is snapshotted into
      the wrapper's own `atomic.Int64` at adopt/rearm time and resolved on
      first observation inside `Value()`. The arm-only-if-looser comparison
      lives once, in the shared pure helper `core.ResolveDeadlineBound`, used
      by both the wrapper path and `vm.armDeadline` — not forked.
      Deadline resolution deliberately does NOT touch VM state: an early
      implementation passed `vm.armDeadlineFor` (a closure over the `*VM`),
      which let a foreign goroutine write `vm.deadline`/`vm.deadlineArmed`.
      Proof of the guarantee, deterministic rather than profile-based:
      `TestVM_ReentrantCtx_NoClockReadWhenGoFuncNeverObservesState` swaps
      `core/vm`'s `nowFunc` (not `runtime`'s — they are distinct vars) and
      asserts 0 clock reads across 5 GoFunc dispatches under a live timeout.
      Fails on master.
- [x] 3.2 Verify a caller-supplied ctx with a tighter deadline still wins,
      per the existing arm-only-if-looser rule.
      `TestVM_ReentrantCtx_LazyDeadlineRespectsCallerTighterDeadline`
      exercises it through the relocated lazy-observation path; the
      pre-existing `TestVM_SetTimeoutCallerEarlierDeadlineSuppressesEngineDeadline`
      still covers the direct `armDeadline` call, unmodified.

## 4. Adversarial retention tests

- [x] 4.1 Retain-and-read: a GoFunc stores its ctx; after the call returns,
      `Deadline`/`Err`/`Value(evalStateKey)` on the stored ctx observe
      fail-safe behavior, never a later run's budget or deadline.
      `TestVM_ReentrantCtx_StaleGenerationFailSafeAfterReset`: the stashed
      counter is a distinct pointer from the live one, reads 0 while the live
      one reads 7, stays 0 after the live one advances, and the stashed
      deadline is zero.
- [x] 4.2 Retain-and-reenter: re-entering the engine with a stale-generation
      ctx runs with a fresh budget (documented misuse), and the enclosing
      current run's counters are unaffected. `-race` clean.
      `TestVM_ReentrantCtx_StaleGenerationFreshBudgetOnReenter`: recursion
      under `MaxDepth = 3` succeeds only because the stale ctx did NOT
      inherit the stashing run's call counter (already advanced to 10) — it
      would trip immediately if state leaked, so the test discriminates.
- [x] 4.3 Same-ctx reuse hit: two sequential `Call`s with one outer ctx
      allocate the wrapper once (assert with `testing.AllocsPerRun`);
      different outer ctxs allocate per ctx.
      `TestVM_ApplyPooled_ReentrantCtxReuseOneOuterCtx` (0 allocs/run) and
      `TestVM_ApplyPooled_ReentrantCtxFreshPerDistinctOuterCtx` (>= 1),
      the latter proving reuse is not unconditional. Both skip under `-race`
      per the existing `raceEnabled` convention.
- [x] 4.5 NOT IN THE ORIGINAL PLAN — two concurrency defects found in review,
      both introduced by wrapper reuse and both absent on master.
      (a) Data race: in-place re-arm mutated four plain `int64` fields that a
      retained ctx reads from another goroutine. Reproduced with the race
      detector (25 reports) via exported API only; master clean. Fixed by
      making those fields `atomic.Int64` and decoupling deadline resolution
      from VM state. Regression test:
      `TestVM_ReentrantCtx_ConcurrentStashedReadDuringRearmRaceFree`.
      (b) Cross-generation tearing, invisible to `-race` because every access
      is a proper atomic: `Value()` validated the generation once, then read
      five atomics and published via CAS. Because re-arm nils `state` before
      stamping `gen`, a reader could publish an evalState mixing two runs'
      limits, which the live run then enforced. Fixed with a seqlock-style
      re-check of the observed generation at every point that returns or
      publishes a state, plus the same guard in `EvalMeterIfMaterialized`.
      Regression test (deterministic, not timing-based; uses the
      caller-supplied `resolveDeadline` callback as a synchronization seam so
      no test-only hook enters production code):
      `TestLazyEvalStateCtx_ValueNeverPublishesCrossGenerationState`, which
      asserts value consistency — the live run enforces exactly the ceiling
      its own re-arm installed. Verified to fail without the fix.
      Residual gap, disclosed: `EvalMeterIfMaterialized`'s guard has no
      dedicated deterministic test. Its hazard window is two adjacent
      statements with no seam to synchronize on, and adding one would mean a
      test-only hook in production code. It is correct by inspection and by
      symmetry with `Value()`, and is exercised (not asserted on) by the
      concurrent-rearm stress test.
- [x] 4.6 NOT IN THE ORIGINAL PLAN — a new panic surface, closed at the
      pre-merge gate by explicit decision rather than deferred.
      The reuse check compares the retained wrapper's outer ctx with the
      current one using `==`, which panics when both operands hold the same
      non-comparable dynamic type. The first guard used
      `reflect.Type.Comparable()`, which is insufficient: it answers true for
      a struct carrying an interface-typed field, and `==` still panics when
      that field holds a slice/map/func on both sides. Embedders may pass any
      `context.Context`, including a by-value struct with an `any` field, so
      this was a live path violating the project's "No panics" invariant —
      and it was new, since no ctx comparison existed before this change.
      Fixed by replacing the `Comparable()` test with `comparableKind`, a
      depth-bounded structural walk: pointer/chan/scalar kinds are safe
      unconditionally, structs and arrays only when every field or element is
      safe, everything else rejected. A rejected ctx is simply "not
      reusable" and builds a fresh wrapper — the pre-existing, well-tested
      path, costing one allocation for that unusual embedder.
      A blanket struct exclusion would NOT have worked: `context.Background()`
      and `context.TODO()` are themselves structs (wrapping an empty struct),
      so excluding structs would have silently disabled the reuse fast path
      and erased the entire allocation win. The recursive walk keeps them on
      it. Verified both directions: with the guard reverted the new test
      panics with `comparing uncomparable type map[string]int`; with it, all
      three tests pass. Tests in `core/reentrant_ctx_comparable_test.go`:
      `TestCtxComparable_RejectsInterfaceCarryingStruct`,
      `TestCtxComparable_AcceptsStdlibContexts` (pins Background/TODO/
      WithValue/WithCancel/WithDeadline on the fast path so the hardening
      cannot silently cost the win), and
      `TestReentrantReuse_HostileCtxNeverPanics` (drives the reuse decision
      itself, not just the predicate).
- [x] 4.4 Existing boundary-efficiency, deadline-ownership, and
      lazy-reentrant-state tests stay green unmodified — this change tightens
      cost, not semantics. AMENDED: one exception, agreed at the plan gate.
      `TestCallReentrancy_StashedLazyCtxSurvivesPooledVMReuse`
      (runtime/call_reentrancy_test.go:278) cannot hold unmodified. It makes
      its second `Call` with the same `context.Background()` singleton — the
      reuse-hit case — and then asserts the stashed ctx still reports a
      non-zero deadline (`require.NotZero`, line 318). The spec delta's
      stale-generation rule ("behave as a context carrying no evaluation
      state") makes that deadline zero, and task 4.1 says the same ("never a
      later run's budget or deadline"). Resolution: keep the spec-literal
      fail-safe and update this one test to assert it — a stale ctx yields a
      private zero counter and a zero deadline. Every other test in the
      boundary-efficiency / deadline-ownership / lazy-reentrant-state set
      stays green unmodified.
      Load-bearing consequence for 2.1: staleness is only detectable if the
      run generation is bumped at the END of a run as well as on reset.
      Bumping only in `Reset`/`ResetIncremental` leaves the VM's live
      generation equal to the wrapper's stamp once a call returns, so a
      retained ctx would still read the live counter and even the counter
      assertions (lines 319-320) would fail. Invariant to implement: a
      wrapper is live iff it was stamped during the currently-executing run;
      once the top-level run returns, no wrapper is live.

## 5. Measure

- [x] 5.1 Re-run 1.1 interleaved with the baseline. Success criteria: Rule
      and Callback each drop one alloc and the wrapper bytes;
      `time.runtimeNow` disappears from the Rule profile for
      never-observing bodies; Call row flat; no goldset cell regresses.
      MET on allocations, which are the load-independent signal and were
      bit-identical (± 0%) across every run. Article rows, interleaved A/B,
      prebuilt binaries, benchstat n=12/side:
      `Callback_Lispico` 144 → 48 B/op, 3 → 2 allocs (−23.31% time,
      p=0.000); `Rule_Lispico` 480 → 384 B/op, 9 → 8 allocs (−11.88%,
      p=0.002); `Call_Lispico` control 48 B/op, 2 allocs unchanged.
      In-repo, n=16-20/side: `Engine_CallBytecode` 128 → 32 B/op, 2 → 1
      allocs; the four GoFunc-free controls bit-identical at 32 B/op, 1
      alloc with no significant time delta (p=0.067-0.974).
      Clock read: proven deterministically by the `nowFunc` test in 3.1
      rather than by reading a profile.
      TIMING IS NOT RELIABLY MEASURABLE ON THIS WORKSTATION and no precise
      figure is claimed. The quiet interleaved run put `Engine_CallBytecode`
      at −27.80% (p=0.000) with controls flat; a later run under load from
      other sessions (load average 11+ on 24 cores) reported −28.22%
      (p=0.006) but with ±227% variance, i.e. worthless. Direction is
      consistent and large; the authoritative timing verdict belongs to the
      release runner, per 6.2 and the precedent in
      2026-07-26-engine-named-call-handle-cache.
      Goldset non-regression is likewise not locally evaluable — see 6.2.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean. All four verified in the worktree; lint reports 0 issues.
- [x] 6.2 Full suite + `-race`; crossval `TestVMVsTreeWalker`; goldset both
      modes; `cmd/perfgate` one-sided non-regression green.
      Green locally: full suite (18 packages), full `-race` (18 packages, the
      two `AllocsPerRun` tests self-skipping under `-race` per the existing
      `raceEnabled` convention), crossval `TestVMVsTreeWalker` (218 tests),
      goldset (both modes, its own test loops `Modes`).
      Independent race probe written by the orchestrator — a GoFunc stashing
      its ctx while a background goroutine reads it through 2000 rearm cycles
      — reports no race on this tree across 3 runs, and reproduced 25 DATA
      RACE reports before the fix.
      `cmd/perfgate` DEFERRED to the release gate, not evaluated locally: its
      noise floor exceeds the gate's tolerance on this workstation, and the
      box carried unrelated load throughout this session. The gate is
      authoritative on the release workflow's quiet 2-vCPU runner
      (`.github/workflows/release.yml`, GOMAXPROCS=2, BENCHTIME=200ms,
      stored per-release baseline); it must be evaluated there. Same
      disposition as 2026-07-26-engine-named-call-handle-cache.
