# vm-deadline-clock-cadence

## Why

`pollCancel` reads the wall clock every checkpoint: `time.Now().Before(vm.deadline)`
(core/vm/vm.go:703), i.e. once per 128 executed instructions whenever a
deadline is armed — and the engine's default 30s timeout arms one on every
evaluation that reaches a first checkpoint. Profile evidence (fib(20)
bytecode, HEAD, 2026-07-27): `time.runtimeNow` is 4.84% flat CPU. A spike
that skipped only the deadline clock read moved fib(20) from 2.53ms to
2.36ms (−7% on top of the batched-ledger spike).

Production interpreters do not read a clock in the hot loop. goja's
`Interrupt()` stores an atomic flag from outside (armed by the embedder via
`time.AfterFunc`), and the loop checks one atomic load on a countdown. V8
checks interrupts only via a sentinel compare at function entry and loop
back-edges. Wasmtime's epoch interruption has a background tick bump a
counter that deadline checks compare against. The common shape: the hot loop
pays a load and a compare; the clock lives outside.

## What Changes

- When a deadline is armed, checkpoint enforcement stops calling `time.Now()`
  per poll. Two candidate mechanisms, chosen by measurement in the design
  task (the requirement text stays mechanism-neutral):
  1. **Timer-flag**: at deadline arming, register one `time.AfterFunc` that
     sets a VM-visible expiry flag; each checkpoint checks a plain atomic
     load. The timer is released at run exit. One timer per armed run, never
     per call; runs that finish before the first checkpoint (the Call
     benchmark shape) arm nothing, exactly as today.
  2. **Reduced clock cadence**: read the clock only every Kth checkpoint
     (K sized so the added detection latency stays well under the documented
     observation bound), keeping ctx-cancellation checks at every checkpoint.
- Context cancellation observation is unchanged: `ctx.Err()` stays at every
  checkpoint (it is an atomic-load-cheap operation, not a clock read).
- The observation-latency bound in "Batched cancellation observation" gains
  an explicit wall-clock term: deadline overrun detection SHALL stay within
  a small documented bound instead of implicitly "one checkpoint".

## Impact

- Affected specs: `bytecode-vm` (Batched cancellation observation).
- Affected code: `core/vm/vm.go` (`pollCancel`, `armDeadline`, run
  entry/exit for timer lifecycle if mechanism 1 wins).
- Expected: fib(20) −5-7%; Rule minor; no allocation change on the
  no-deadline-observed paths (runtime-api "Boundary call efficiency"
  scenarios must stay green — short calls still read no clock at all).
- Risk: mechanism 1 adds a timer lifecycle per armed run (leak audit on
  every exit path); mechanism 2 widens worst-case deadline overshoot —
  bounded and documented, ADR 0010's bounded-interval posture allows it.
