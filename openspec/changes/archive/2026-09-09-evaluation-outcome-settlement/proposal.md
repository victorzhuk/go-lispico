## Why

The review at commit `3bcf9c1` reproduced `OnEval.Error == nil` and `Stats().TotalErrors == 0` while the same evaluation returned a retained-meter `ResourceLimitError`, on both evaluators. `Eval` and `evalWithBindingScope` publish success before deferred `core.FinishEval` selects the final error.

## What Changes

- Settle each evaluation before publishing its final stats and `OnEval` event.
- Cover `Eval`, `EvalWithBindings`, and `LoadScope`, including setup, parse, evaluation, settlement, and recovered GoFunc failures.
- Preserve ordinary evaluation writes, terminal-error precedence, error wrapping, and exactly-once lease return.
- Leave callback-panic policy unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: add **Evaluation observations follow final settlement**.

## Impact

Changes `runtime/eval.go` and runtime meter/stats/panic tests. No public API or dependency change. Retained settlement remains charge-after-write; failed ordinary evaluations do not become transactions. Update existing metering documentation and CHANGELOG during implementation.

No semantic dependency on another proposed change. Merge after `metered-call-deadlines` and before `plugin-binding-rollback` to keep overlapping runtime edits sequential.
