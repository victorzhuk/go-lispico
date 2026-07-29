# Tasks — engine-lean-call-boundary

## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (one session, `-count=10`): Call, Callback,
      Rule, Fn/Pinned probes, fib as control, `-benchmem`; per-call profile
      shares (RLock pair 10.3%, Reset 5.5% at HEAD 2026-07-27).
      GopherLua/goja rows same session for the bar.

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
- [ ] 5.4 Interleaved benchstat vs 1.1: Call ≤110ns standalone (≤95ns once
      vm-call-frame-fast-path is in), Callback/Rule improved, fib control
      flat. Update the harness-facing docs only after the release-runner
      gate confirms.
