# Tasks — engine-lean-call-boundary

## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (one session, `-count=10`): Call, Callback,
      Rule, Fn/Pinned probes, fib as control, `-benchmem`; per-call profile
      shares (RLock pair 10.3%, Reset 5.5% at HEAD 2026-07-27).
  Pinned at HEAD c8645fb, 2026-07-29, `GOMAXPROCS=2`, `-count=10`, n=10
  medians (ns/op): Call 218.4, CallBytecode 222.7, CallBytecodePlain 137.4,
  CallBytecodeCanonical 162.0, FuncCall 149.0, PinnedFnCall 137.0,
  FuncCallCallback 270.1, PinnedFnCallCallback 264.8, CallBytecodeCallback
  370.6; FibonacciCL 209.7µs. Every boundary row 32 B/1 alloc.
  `BenchmarkEngine_FibonacciCL` is fib(15) (`runtime/bench_test.go:388`) and
  controls only within this run — it is NOT the fib figure the round-6 goal
  table quotes against GopherLua/goja, which measures a larger N on an
  external harness. Rule-shaped cells are covered by `internal/goldset`, not
  by a `runtime` probe.
  These absolutes are session-local: the same HEAD read 119.7 ns for
  CallBytecodePlain earlier the same day (see 5.4). Use them as the
  within-session reference for this table, not as a figure to diff against
  another session's.
  GopherLua/goja rows are out of scope for this change: no such dependency
  exists in `go.mod` (testify, x/sync, x/term only), so the cross-engine bar
  is an external-harness measure and moves to `release-gate-activation`.

## 2. Fast-path condition

- [x] 2.1 Engine fast-flag: bytecode evaluator + no engine meter + no
      OnPluginCall/OnEval callbacks. Stored atomically; recomputed under
      the engine lock at every mutation site (callback registration,
      option application). Entry check: flag && !HasEvalState(ctx) &&
      !HasEvalMeter(ctx) — derived ONCE per call, threaded down instead of
      re-probed in callBoundary/applyOnVM.
- [x] 2.2 Root-env atomic snapshot: replace the per-call `e.mu.RLock` root
      read with an atomic pointer maintained wherever rootEnv is replaced
      (hot-reload/Rebuild sites — enumerate them; each already holds the
      engine lock).

## 3. Lock-free callee read

- [x] 3.1 Extend the call cache hit path to a versioned snapshot read
      (cell version + value, no env RLock), mirroring the VM site-cache
      read; version mismatch or tombstone → today's locked re-resolution.
      The never-cache-value-fallback rule is unchanged.
- [x] 3.2 Prove the read is coherent: value+version read under the same
      ordering the VM snapshot uses (the env write path bumps version
      under its lock; the read must not tear) — reuse `ReadCellSnapshot`'s
      contract or lift it to an atomic pair.

## 4. Lean call spine

- [x] 4.1 Single recover-defer for the fast path covering VM release,
      panic→`NewPanicError`, and stats bump; no StartEval/FinishEval, no
      `nowFunc` (callbacks are off by the flag's definition).
- [x] 4.2 Per-engine VM slot: atomic CAS claim/release with pool fallback;
      the claimed VM uses the vm-call-frame-fast-path per-call arm rather
      than full Reset when the prior exit was clean. Concurrent `Call`s
      race the slot safely — loser takes the pool path.
- [x] 4.3 Undefined-name path, arity errors, Lisp-2 fallback resolution:
      byte-identical behavior on both paths (table-driven parity test that
      runs the same call matrix through a fast-flag engine and a
      callback-registered engine and diffs results/errors/stats).

## 5. Verify

- [x] 5.1 Concurrency: concurrent `Call` storm under `-race`; VM slot
      never double-leased (add an invariant check in the claim path under
      a build tag or test hook).
- [x] 5.2 Flag transitions: register a callback between calls → next call
      fires it; attach ctx meter → metered path taken; detach → fast path
      resumes. Stats exact in all phases.
- [x] 5.3 Full floor: build, vet, lint, full suite, `-race`, crossval,
      goldset both modes non-increasing.
- [x] 5.4 Interleaved benchstat vs 1.1. Update the harness-facing docs only
      after the release-runner gate confirms — still pending, see below.
  RELATIVE result confirmed at merge (composed round-6 A/B, n=8, this box):
  CallBytecode −24..−34%, Plain/Canonical −21..−36%, FuncCall −24..−37%,
  PinnedFnCall −26..−32%, Callback rows −9..−14%, fib control flat, goldset
  both modes not significant. Callback and Fn/Pinned rows improved as
  required.
  ABSOLUTE `Call ≤110ns` bar NOT MET, and not adjudicable on this box. The
  2026-07-29 re-measure at HEAD c8645fb gives PinnedFnCall 137.0 and
  CallBytecodePlain 137.4 ns median (best single sample 131.5) against the
  119.7 / 120.8-122.8 recorded earlier the same day at the same HEAD — ~15%
  session-to-session drift on this laptop part, wider than the margin under
  test. `GOMAXPROCS=2` and `GOMAXPROCS=24` agree (137.4 vs 145.8 median), so
  concurrency is not the driver. ADR 0008 requires a hosted run at fixed
  concurrency and benchtime for an authoritative verdict, and
  `cmd/perfgate` false-FAILs locally, so neither the ≤110ns bar nor the
  ≤95ns composed target is settled here. Carried to
  `release-gate-activation` task 2.3; this change archives with the bar
  recorded as unmet rather than silently ticked.
