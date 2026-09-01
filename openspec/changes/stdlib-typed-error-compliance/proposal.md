## Why

The core contract requires every evaluation failure to be recoverable as a
`*core.LispicoError`, but stdlib Builtins still return many plain `fmt.Errorf`
values. Evaluator and VM apply sites pass those values through unchanged, so
hosts cannot reliably classify ordinary arity, type, or domain failures.

Blocked by: `cl-collection-adapters` and
`stdlib-nil-lookup-semantics`.

## What Changes

- Classify every stdlib-originated failure as `ArityError`, `TypeError`,
  `EvalError`, or the existing terminal/resource code as appropriate.
- Preserve operation-specific messages while making `errors.As` and `Code`
  reliable through direct Apply, Evaluator, VM, eager, and lazy paths.
- Preserve typed callback and Terminal errors returned through higher-order
  Builtins instead of flattening or reclassifying them.
- Add small stdlib construction helpers over the existing
  `core.LispicoError`/constructors for exact, ranged, and variadic arity plus
  operation-specific type/domain messages.
- Freeze a package-wide inventory of validation sites and reachable helpers, then
  enforce it across the active stdlib package rather than only direct GoFunc
  closures.
- **BREAKING** Plain concrete error values from direct stdlib calls become
  `*core.LispicoError`; callers comparing concrete error types must switch to
  `errors.As` and `Code`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: require typed, operation-classified failures from every
  registered Builtin.
- `dialect`: require typed local-validation failures from the final CL adapter
  surface while preserving callback and Terminal errors.

## Impact

- Affects error construction throughout `plugins/stdlib`, CL adapter validation,
  direct stdlib tests, runtime behavior goldens, and host-facing documentation.
- Does not change which valid inputs succeed or which invalid inputs fail.
- Does not add source locations to `core.Value`; positional evaluation errors
  require a separate positional-form design.
- Runs after lookup and CL adapter call shapes settle, then unblocks the nil
  sequence change that must preserve exact error classes.
