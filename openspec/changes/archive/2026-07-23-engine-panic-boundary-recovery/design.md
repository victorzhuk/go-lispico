## Context

`PinnedFn.Call` already carries the correct pattern (`runtime/func.go:176-214`):
a `defer` that runs `recover()`, wraps the value in `core.NewPanicError`, and —
because the steady-state `ResetIncremental` cannot be trusted after a panic —
fully `Reset()`s the handle's VM before it is reused. Every other public
boundary lacks it.

## Boundaries needing the guard

- `Engine.Call` (`runtime/eval.go:641`) — owns a pooled VM: `v := vmPool.Get();
  v.Reset(); defer vmPool.Put(v)`. On a panic the deferred `Put` runs during
  unwind and returns a corrupted VM to the pool.
- `Fn.Call` (`runtime/func.go:130`) — identical pooled pattern, same corruption
  risk.
- `Engine.Eval` (`runtime/eval.go:505`) — drives `EvalCached`; the panic can
  originate inside a `GoFunc` reached during compilation-time macro expansion
  or run.

`bytecodeEvaluator.Apply` (`runtime/eval.go:307-314`) acquires and returns its
pooled VM without `defer`, so a panic there skips `Put` and the VM is simply
dropped (GC'd) — no corruption, no change needed at that site.

## Decision

Place the recover where the pooled VM lifecycle is owned, so reset-before-Put
stays correct:

- In `Engine.Call` and `Fn.Call`, replace the bare `defer vmPool.Put(v)` with a
  deferred closure that: on a recovered panic, sets a typed
  `core.NewPanicError(name, r)` return, calls `v.Reset()`, then `vmPool.Put(v)`;
  on the normal path, `Put`s as before. Mirrors `PinnedFn.Call` exactly.
- In `Engine.Eval`, add a top-level deferred recover that converts a panic to
  `core.NewPanicError(source, r)` and records the failed eval in stats/callbacks
  the same way the error return path does, so a panic and a returned error look
  identical to an embedder.

## Why not a single wrapper

A shared `func recoverBoundary(...)` helper would centralize the recover, but
the pooled-VM reset differs per site (owned pooled VM in `Call`/`Fn.Call`,
none directly held in `Eval`) and the stats/callback bookkeeping in `Eval`
is already inline. Two small, local `defer`s mirroring the proven
`PinnedFn.Call` shape are simpler than a wrapper threading VM-reset and
stats closures. Revisit if a fourth boundary appears.

## Semantics

A recovered panic is not a terminal eval error — it is a host-facing failure of
a Go callback. It surfaces as a `*core.LispicoError` (Code `PanicError`) to the
caller. It is not subject to `try`/`catch` interception by design: a panic is a
program bug, not a throwable Lisp value, and the never-panics contract is a
host boundary guarantee, not a script-observable control-flow feature.

## Risks

- A panic mid-mutation of engine state (root env during `Bind`, cache during
  admission) could leave shared state inconsistent even after the VM resets.
  Scope of this change is the GoFunc dispatch boundary; state-mutation paths
  hold their own locks and do not run user GoFuncs mid-critical-section, so the
  recovered-panic surface is the evaluation path only. Note this boundary in
  the requirement rather than widening scope.
