## Why

The review at commit `3bcf9c1` reproduced a VM call running for 10 ms despite `WithTimeout(time.Millisecond)` when either `WithMeter` or `WithEngineMeter` was attached. Meter setup creates eval state without a deadline; `bytecodeEvaluator.applyOnVM` installs that zero deadline and skips the engine timeout.

## What Changes

- Preserve engine deadline enforcement through `Engine.Call`, `Fn.Call`, and `PinnedFn.Call` when a meter or eval state is present.
- Keep caller and inherited evaluation deadlines, explicit timeout disablement, and shared reentry budgets intact.
- Add regressions for context meters, engine meters, existing eval state, and every call handle.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: add **Meter attachment preserves call deadlines**.

## Impact

Changes `runtime/eval.go` and runtime deadline/boundary tests. No public API, dependency, meter-policy, or timer-allocation change. Update the existing deadline documentation and CHANGELOG during implementation.

No semantic dependency on another proposed change. Merge before `evaluation-outcome-settlement` and `plugin-binding-rollback` to keep overlapping runtime edits sequential.
