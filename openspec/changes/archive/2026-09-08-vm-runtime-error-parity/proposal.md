## Why

The code review of commit `3bcf9c1` found `(try missing (catch e 42))` escaping with `UndefinedError` on the VM while the tree-walker returns `42`. An uncaught `(throw "boom")` also becomes `TypeError: expected handler, got core.Nil`, losing the thrown message and `ThrowError` classification.

## What Changes

- Route ordinary runtime opcode errors to the nearest active handler, matching existing call-error handling.
- Preserve the original thrown value for catches and its `ThrowError` classification and rendering at an uncaught host boundary.
- Keep `core.IsTerminalEvalError` authoritative: cancellation, deadlines and resource ceilings bypass every handler.
- Restore frame, operand, native-freeze and structural-depth state while unwinding; pin successful reuse after failure.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: add requirements for ordinary runtime error routing and uncaught throw preservation, under existing terminal-error and state-hygiene contracts.

## Impact

Primary code: `core/vm/vm.go`, and `core/error.go` or `core/eval.go` only if sharing throw representation is necessary. Tests cover VM dispatch, cross-evaluator behavior and public runtime boundaries. Update existing `ARCHITECTURE.md` error semantics and `CHANGELOG.md` when implemented.

No dependency or public signature changes. Previously skipped catch bodies will run; uncaught throw classification will match the tree-walker. Compile-time syntax rejection and structured catch objects for ordinary Go errors are outside scope.

Implement and archive `vm-local-stack-scope` first so handlers target the settled frame layout. This change adds requirements without replacing `Bytecode VM execution`.
