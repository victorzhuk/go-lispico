## Why

`try`/`catch` must not let a program outlive its own termination. Today only the
tree-walker filters resource-limit errors (`core/eval.go:973`), and only that one
class. Everything else routed through the VM's throw path — `OpCall`,
`OpTailCall`, and native-op dispatch coerce any Go error into a `core.String`
and offer it to the active handler (`vm.go` OpCall handler) — is catchable,
including `context.Canceled`, `context.DeadlineExceeded`, and a
`ResourceLimitError` propagating out of a nested GoFunc or tree-walker
fallback. The tree-walker has the mirror hole for cancellation: `evalTry`
catches deadline/cancel errors because its filter checks only
`CodeResourceLimit`.

Consequence: `(loop [] (try (heavy-work) (catch e nil)))` absorbs each batched
cancellation error and iterates forever. The embedder's wall-clock deadline —
yagel's only runaway-rule defense at v0.8.0 (its characterization test
`TestV08_CallerContextDeadline_GovernsEvaluation` pins exactly this) — is
evadeable by any hostile or buggy script. The upcoming metering changes raise
the stakes: a budget-exhausted evaluation must never continue as a caught
branch.

## What Changes

- Define the terminal error classes: `context.Canceled`,
  `context.DeadlineExceeded` (matched via `errors.Is`, so the VM's `"vm: %w"`
  wrapping still qualifies), and `*core.LispicoError{Code: CodeResourceLimit}`.
- New classifier `core.IsTerminalEvalError(err) bool` shared by both
  evaluators.
- Tree-walker `evalTry` passes all three classes through uncaught (today:
  resource-limit only).
- VM error routing filters terminal errors before `vm.throw` at every
  `err → throw` site (`OpCall`, `OpTailCall`, `OpThrow` re-raise, native-op
  dispatch): terminal errors return from `run` directly, unwinding handlers,
  frames, and the freeze stack exactly like the existing structural-depth
  return path.
- Explicit user throws stay fully catchable — the filter applies to Go error
  classes, never to values a program `throw`s (a `String` that merely looks
  like an error message is still caught).
- No API surface change; no new `ResourceLimits` field.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: new requirement `Terminal errors are not catchable`.

## Impact

- Code: `core/eval.go` (`evalTry` filter), `core/error.go`
  (`IsTerminalEvalError`), `core/vm/vm.go` (pre-throw filter at the four
  error-routing sites).
- Downstream: `engine-reduction-and-allocation-metering` and
  `meter-leases-and-session-ledgers` reuse the classifier for budget-exhaustion
  errors; this change lands first.
- yagel: closes the deadline-evasion hole at the next version bump with zero
  consumer code change.
