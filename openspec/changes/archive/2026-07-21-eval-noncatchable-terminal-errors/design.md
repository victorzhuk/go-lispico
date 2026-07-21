# Design — eval-noncatchable-terminal-errors

## Decisions

- D1: One classifier, `core.IsTerminalEvalError(err)`, owns the class list.
  Both evaluators call it; later metering changes extend behavior only by
  raising errors that already match (`CodeResourceLimit`), never by touching
  the filter sites again.
- D2: Filter on Go error classes, not on thrown values. `throw` of any Lisp
  value — including a string spelling "context deadline exceeded" — remains
  catchable. Only errors originating as Go `error` values from the evaluator,
  a GoFunc, or a nested evaluation are classified.
- D3: Match by `errors.Is` for the context classes so the VM's `"vm: %w"`
  wrapping and any future wrapping survive; match `CodeResourceLimit` by
  `errors.As` on `*LispicoError`, same as the existing tree-walker filter.
- D4: In the VM, a terminal error returns from `run` through the plain error
  path (as `OpStructEnter` already does), which unwinds frames, handlers, and
  `freezeStack` via the established return machinery. No new unwind code.
- D5: `catch` binding shape is unchanged for non-terminal errors — the handler
  still receives `String{V: err.Error()}`. Out of scope: structured catch
  values.

## Risks / Trade-offs

Behavior change: scripts that (accidentally) caught cancellation today will now
terminate. That is the point; the old behavior is a defense bypass, and no
dialect or spec ever promised catchable cancellation. Cross-evaluator parity is
the main regression risk — covered by crossval tests running the same
adversarial programs on both evaluators.

## Migration Plan

1. Red tests: deadline-evasion loop on tree-walker; nested resource-limit
   catch and cancellation catch on the VM; characterization test that ordinary
   `throw` values stay catchable.
2. Add `IsTerminalEvalError`; wire `evalTry`.
3. Wire the VM's four error-routing sites.
4. Crossval + `-race` + goldset gate.

## Open Questions

None blocking.
