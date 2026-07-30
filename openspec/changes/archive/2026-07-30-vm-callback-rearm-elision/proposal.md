# vm-callback-rearm-elision

## Why

A host callback costs ~284ns against GopherLua's ~151ns on the same box
(2026-07-27) — and ~100ns of the gap over plain `Call` (~178ns) is re-entrant
state bookkeeping. The Callback profile shows
`core.RearmReentrantEvalState` at 18.2% cumulative (10.9% flat) and the
`reentrantCtx` chain at 21.6% cumulative.

The boundary-state-reuse change eliminated the per-call wrapper *allocation*;
what remains is the per-call *rearm*. `bumpRunGenIfWrapped` marks the
wrapper stale at every run exit (correctly — the generation guard is the
retained-ctx safety mechanism), so the next call's first GoFunc dispatch
finds `ReentrantEvalStateLive` false and re-arms: eight atomic stores
(`counter`, `callCounter`, `maxReductions`, `maxAllocBytes`, `reductions`,
`allocBytes`, `timeout`, `state`+`gen`, core/eval.go:465-482) plus two
`normalizeEvalLimit` calls (3.4% flat in the profile) — every call, even
though between two adjacent calls on the same engine with the same context
every rearmed value is identical except the seeds and the generation.

## What Changes

- Rearm becomes delta-based: the wrapper remembers the configuration it was
  last armed with (limits snapshot, timeout, seed baseline); when a rearm
  request carries the same configuration — the steady repeated-`Call` shape —
  only the generation stamp and the per-run seeds that actually differ are
  written. A configuration change (different limits, meter snapshot, or
  timeout) takes today's full rearm.
- Seed writes get cheaper in the common case: a top-level boundary call
  always seeds depth counters from the VM's own zeroed counters; the wrapper
  can treat "seeds are zero" as the baseline and skip those stores when the
  VM state says so.
- The generation guard, live-wrapper fast path, meter-snapshot semantics,
  and the retained-context staleness contract are unchanged — this changes
  how much work a rearm does, never when a rearm is required.

## Impact

- Affected specs: `bytecode-vm` (Lazy re-entrant evaluation state — rearm
  cost posture).
- Affected code: `core/eval.go` (`RearmReentrantEvalState`,
  `lazyEvalStateCtx` fields), `core/vm/vm.go` (`reentrantCtx` seed
  plumbing).
- Expected: Callback −40-60ns (284 → ~230 standalone; ~150-170 combined
  with engine-lean-call-boundary and vm-call-frame-fast-path — at or below
  GopherLua's 151ns local bar); Rule inherits a smaller cut (one GoFunc
  dispatch per call).
- Risk: a stale-config rearm serving old limits — the config comparison
  must cover every field the full rearm writes (pin with a test that
  changes each field between calls and asserts the wrapper serves the new
  value); atomics dropped only where the single-writer-per-run invariant
  provably holds (the wrapper is written under one VM's run — document and
  `-race`-test the hostile retained-ctx interleavings that
  reentrant_hostile_ctx_test.go already covers).
