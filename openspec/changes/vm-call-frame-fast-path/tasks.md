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
  Built, verified, then DROPPED on measurement (2026-07-29): the premise
  was stale — `reloadFrame` is 3.8% flat on fib post-OpFusedNativeOp, not
  the 12.61% the proposal cites. Three-way interleaved A/B/C (master /
  with-fast-paths / without): fib ~flat in both variants; Accumulate1000
  −5.4% (p=0.002) without the branches vs ~0 with them — the extra
  dispatch-switch branches cost more than the elided field syncs, the
  same codegen sensitivity that rejected vm-global-call-inline-cache.
- [x] 2.2 Same-chunk call: built and dropped with 2.1 (same evidence).
- [x] 2.3 Re-audit throw/handler unwind and terminal Reset against the
      frame-local state: moot for frame sync after the 2.1/2.2 drop; the
      audit ran for the depth-counter pointer switch instead (throw's
      `structDepthStore`, handler push `structDepthLoad`).

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

- [x] 4.1 Full floor: build, vet, lint (0 issues), full suite, `-race`,
      crossval, goldset both modes non-increasing (vm geomean +1.5% n.s.,
      eval geomean +0.8% n.s., all rows p>0.05 except noise-band rows on
      untouched reader code) — 2026-07-29, composed with
      vm-callback-rearm-elision and engine-lean-call-boundary.
- [x] 4.2 Reentrant suite (reentrant_generation_test.go,
      reentrant_hostile_ctx_test.go and siblings) green under `-race`.
- [x] 4.3 Interleaved benchstat vs master (n=8, one session, composed):
      CallBytecode −24%, CallBytecodePlain −21%, Canonical −24%,
      FuncCall −24%, PinnedFnCall −26..−32%, Callback rows −9..−14%;
      fib ~flat (premise correction in 2.1 — the frame-sync share no
      longer exists to win); fib B/op +1..3 B is the one-time engine
      vmSlot amortized over b.N, not a per-op alloc. Nothing regresses
      after the 2.1/2.2 drop.
