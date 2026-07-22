## Why

The "no panics — all errors returned gracefully" invariant (CLAUDE.md,
`ARCHITECTURE.md`) holds on only one of the public call paths. The sole
`recover()` in non-test code is `runtime/func.go:181` (`PinnedFn.Call`).
`Engine.Eval`, `Engine.Call`, and `Fn.Call` route GoFunc dispatch through
`callBoundary` / `bytecodeEvaluator` with no recover, so a panic inside any
registered function — a bug in the shipped `stdlib`/`data` builtins or in an
embedder's own Go callback — propagates through `core/vm.(*VM).call` to the
caller's goroutine and aborts the process.

Confirmed by reproduction on both evaluators: a `GoFunc` that panics, invoked
via `Engine.Eval("(boom)")` or `Engine.Call("boom")`, escapes uncaught; only
the host's own `defer recover()` stops it. For the shared, process-wide engine
the primary consumer runs, one tenant's buggy function crashes every tenant.

## What Changes

- Recover a GoFunc/VM panic at every public evaluation boundary — `Engine.Eval`,
  `Engine.Call`, `Fn.Call` — and convert it to a typed `*core.LispicoError`
  via the existing `core.NewPanicError`, exactly as `PinnedFn.Call` already
  does.
- Pool hygiene: a pooled VM that panicked SHALL NOT be returned to `vmPool` in
  a corrupted state. The deferred `vmPool.Put` sites in `Engine.Call`
  (`runtime/eval.go:664`) and `Fn.Call` (`runtime/func.go:152`) fully reset or
  discard the VM on a recovered panic before reuse.
- A recovered panic is an ordinary returnable error surfaced to the host, not a
  terminal eval error and not silently swallowed; existing terminal-error and
  `try`/`catch` semantics are unchanged.
- No new option, no signature change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `GoFunc panics are recovered at evaluation
  boundaries`.

## Impact

- Code: `runtime/eval.go` (`Eval`, `Call`), `runtime/func.go` (`Fn.Call`),
  reusing `core.NewPanicError` and the `PinnedFn.Call` reset pattern.
- Downstream: the shared-engine consumer keeps a buggy rule function from
  taking down the process; no consumer code change.
- Invariant: brings `Engine.Eval`/`Engine.Call`/`Fn.Call` in line with the
  documented never-panics contract.
