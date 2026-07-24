## Why

Two public evaluation entry points and the file-watcher reload path evaluate
user code without the `recover()` that `Engine.Eval` and `Engine.Call` already
install, so a panicking `GoFunc` (a plugin/stdlib bug: nil deref, bad type
assertion, index out of range) escapes as a raw Go panic — a direct violation of
the "No panics — all errors returned gracefully" invariant.

- `EvalWithBindings` and `LoadScope` both run through `evalWithBindingScope`
  (`runtime/eval.go:781`), which has no recover. A panic propagates out of the
  documented public API to the caller's goroutine (reproduced: panic escapes
  both, while the same panic through `Eval` returns a `PanicError`).
- `Watch`'s `reloadFile` (`runtime/watch.go:118`) calls the bare evaluator on
  the background `watchLoop` goroutine started by `Start`. A panic there has no
  caller to observe it and **terminates the entire host process** (reproduced
  with the crash stack). `Watch()` is a documented production feature, so any
  watched `.lisp` file that triggers a `GoFunc` panic is a live-process DoS.

## What Changes

- `evalWithBindingScope` (backing `EvalWithBindings` and `LoadScope`) SHALL
  recover a panicking `GoFunc` and return it as a `PanicError`, matching
  `Engine.Eval`.
- `fileWatcher.reloadFile` SHALL recover a panic during a background reload,
  convert it to an error, and surface it through the watcher's existing error
  path instead of letting it crash the process. The whole-process-crash
  behavior on a background-goroutine panic is a **BREAKING** robustness fix
  (previously fatal, now a reported reload error).
- No public API signature change; recovered panics reach callers as the same
  `PanicError` the other boundaries already return.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement that every public evaluation entry point —
  including `EvalWithBindings`, `LoadScope`, and background hot-reload — recovers
  `GoFunc` panics and never lets one escape to the host or crash the process.

## Impact

- Code: `runtime/eval.go` (`evalWithBindingScope`), `runtime/watch.go`
  (`reloadFile`, and the reload-error surface).
- Behavior: `Watch` on a file whose evaluation panics reports a reload error
  instead of killing the host process; `EvalWithBindings`/`LoadScope` return a
  `PanicError` instead of propagating a raw panic.
- Closes the last three uncovered evaluation boundaries against the project's
  no-escaping-panics invariant.
