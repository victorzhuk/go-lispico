# vm-call-frame-fast-path

## Why

With the governance tax removed (spikes, 2026-07-27), the fib profile's
second-largest block after raw dispatch is frame machinery: `reloadFrame`
12.61% flat, `vm.call` 9.24% flat. `reloadFrame` re-derives seven pieces of
dispatch state (chunk, code, ip, base, env, caps, truthy) on every call AND
every return — for fib(20), ~44k full syncs per evaluation, most of them
re-loading values that did not change (a fib recursion returns into the same
chunk it left).

On the boundary axis, `ApplyPooled` performs two atomic RMWs per call
(`counter.Add(1)` / deferred `Add(-1)` on the call-depth counter,
core/vm/vm.go:571-572) even when the counter is the VM's own private
`ownCallDepth` that no other goroutine can observe, plus an unconditional
deferred `bumpRunGenIfWrapped`. `VM.Reset` (5.5% of Call CPU) rewrites ~20
fields per call including two atomic stores, most of which the previous run
already left in the required state. The raw-VM floor measured this session —
`ApplyPooled` on a reused VM with zero engine boundary — is ~98ns, against
GopherLua's ~97ns *full protected call* on the same box: the VM-internal
apply path itself must get cheaper for the boundary program to win.

## What Changes

- **Same-chunk return fast path**: a return whose target frame runs the same
  chunk restores only ip/base (and env if changed) instead of the full
  seven-field sync; the full `reloadFrame` remains for cross-chunk
  transitions. Same treatment for the call path when the callee chunk is the
  currently-loaded chunk (self-recursion).
- **Non-shared depth counter elision**: when the call-depth counter is the
  VM's own (`counter == &vm.ownCallDepth` — no eval-state sharing), depth
  tracking uses a plain field; the atomic path remains whenever the counter
  is shared with an evaluation state. Same audit for `structDepth`.
- **Reset slimming**: split `Reset` into the per-call arm (only fields a
  clean previous run leaves dirty) and the full reset (error/terminal
  paths). The per-call arm must be provably equivalent for a VM whose
  previous run exited cleanly.
- **Conditional run-gen bump**: `bumpRunGenIfWrapped`'s defer is skipped
  when no reentry wrapper exists (the common Call shape dispatches no
  GoFunc).

## Impact

- Affected specs: `bytecode-vm` (Structural-depth state hygiene — the
  shared-vs-private depth counter rule; no observable-behavior change). The
  frame-sync fast paths were dropped on measurement, so no dispatch-state
  wording lands.
- Affected code: `core/vm/vm.go`, `core/vm/frame.go`.
- Expected: fib −6-10%; Call/Callback −10-20ns each; goldset VM cells
  non-increasing.
- Risk: frame-state divergence bugs (state in locals vs frames drifting on
  throw/handler paths) — the throw/catch and terminal-error paths must be
  re-audited against the frame-local dispatch invariant from
  2026-07-18-vm-dispatch-loop-tightening; `-race` guards the counter
  elision (a shared counter mistakenly elided is a real data race, so the
  elision condition must be a pointer identity check set at arm time).
