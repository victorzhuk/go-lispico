# Tasks — vm-call-frame-fast-path

## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (one session, `-count=10`): fib, Call,
      Callback, Rule, goldset both modes, `-benchmem`; `reloadFrame` and
      `vm.call` flat shares on fib (12.61% / 9.24% post-tax at
      2026-07-27). Untouched-row control.

## 2. Frame sync fast paths

- [x] 2.1 Same-chunk return: on `OpReturn`/`OpTailCall` return where the
      resumed frame's chunk pointer equals the loaded chunk, restore only
      ip/base(/env when different); full `reloadFrame` otherwise. Audit
      `truthy` and `caps` — prove they are chunk-derived (same chunk ⇒
      same values) or reload them.
- [x] 2.2 Same-chunk call: when the callee closure's chunk equals the
      loaded chunk, skip re-deriving code/truthy/caps on frame entry.
- [x] 2.3 Re-audit throw/handler unwind and terminal Reset against the
      frame-local state: every path that bypasses the normal return must
      leave the loop's local state coherent (the dispatch-loop-tightening
      invariant; two prior escapes were found by adversarial review — run
      the same review here).

## 3. Boundary micro-costs

- [x] 3.1 Depth-counter elision: plain-field depth when the counter is the
      VM-private one (pointer identity, decided once at arm time), atomic
      when shared with an evaluation state. `-race` must stay green across
      the reentrant test suite; add a test that a shared counter still
      counts across the boundary.
- [x] 3.2 Reset split: per-call arm vs full reset. Enumerate every `Reset`
      field against "what state does a clean run exit leave" — each field
      either provably clean (skip) or armed. Terminal-error and panic paths
      keep the full reset.
  Evidence: `ResetIncremental` already verifies clean-run invariants, resets every dirtiable field, and falls back to full `Reset` on terminal or dirty state.
- [x] 3.3 Conditional `bumpRunGenIfWrapped`: skip the defer when
      `reentryCtx == nil` at entry AND no wrapper can be created below
      (i.e. bump at wrapper creation instead if needed) — the
      generation-guard semantics of the lazy re-entrant state requirement
      must hold exactly (its retained-context scenario is the pin).

## 4. Verify

- [ ] 4.1 Full floor: build, vet, lint, full suite, `-race`, crossval,
      goldset both modes non-increasing.
- [ ] 4.2 Reentrant suite (reentrant_generation_test.go,
      reentrant_hostile_ctx_test.go and siblings) green — these pin the
      run-gen and shared-counter semantics this change touches.
- [ ] 4.3 Interleaved benchstat vs 1.1: fib −6% or better, Call/Callback
      improved, nothing regresses.
