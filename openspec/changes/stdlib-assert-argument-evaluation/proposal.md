## Why

`assert` is registered as a `GoFunc`, not as a special form, so the apply site
has already evaluated its arguments by the time the builtin runs. The builtin
then calls `eval.Eval` on those arguments a second time
(`plugins/stdlib/control.go:17` for the condition, `:24` for the message).

Evaluating a value that is already a value is usually the identity, which is why
the defect is quiet. It is not the identity for the two value types that are
also forms. A `Symbol` value is looked up, and a `List` value is applied:

- `(assert false 'x)` reports `undefined: x` instead of `assertion failed: x`.
  Measured on both the tree-walker and the VM, and under both dialects.
- an assertion message that evaluates to a list is re-applied as a call, so the
  failure the user sees is whatever that application produces.

The result is that `assert`'s own failure message is replaced by an unrelated
error exactly when a script is trying to report why an assertion failed — the
moment the message matters most.

This was found during `stdlib-builtin-resource-migration` and deliberately left
alone there: that change's migration plan forbids altering language semantics,
and this is a semantic fix.

## What Changes

- Stop re-evaluating `assert`'s arguments: use the values the apply site already
  produced.
- Pin the corrected reporting for the argument shapes that expose the defect —
  symbol, list, string, and non-string scalar messages — under both execution
  modes and both dialects.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: `assert` reports against its received arguments.

## Impact

- `plugins/stdlib/control.go` and the `assert` goldens.
- Scripts that today rely on the accidental second evaluation of an assert
  argument will observe the assertion message instead of a lookup or call error.
  No script can depend on the current behavior deliberately: it produces an
  unrelated error, never a usable value.
- The `%.200v` render behind a non-string assertion message stays as it is. Its
  cost under structural sharing is a separate, already-tracked defect owned by
  `core-value-walk-sharing-bound`; this change neither widens nor narrows it.
