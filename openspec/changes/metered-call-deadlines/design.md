## Context

See proposal.md for the reproduced failure. `callBoundary` creates eval state for metered calls; `bytecodeEvaluator.applyOnVM` selects `SetDeadline` whenever state exists, even when its deadline is zero. `SetDeadline` marks the VM deadline armed, so the timeout is never derived. `Fn.Call` and `PinnedFn.Call` share this path.

## Goals / Non-Goals

**Goals:** compose deadline selection with existing state and meter ownership at the common call boundary; preserve already-resolved deadlines and handle parity.

**Non-Goals:** changing lease semantics, clock polling cadence, caller-context wrapping, direct core evaluator configuration, or ordinary evaluation side effects.

## Decisions

### Arm absent bounds without replacing inherited state

Keep the lean path unchanged. On the general boundary, distinguish existing state from an existing deadline: state creation for metering is not evidence that deadline ownership has been resolved. Resolve the configured engine bound when a top-level call lacks one, respecting the caller deadline, then install it into the existing eval state before VM dispatch. Reuse `evalDeadline`, `core.WithEvalDeadline`, and `core.EvalDeadlineFrom`; introduce no second eval state or lease lifecycle.

An inherited nonzero absolute eval deadline remains unchanged on reentry. A zero timeout introduces no bound and cannot erase an inherited one. The tree-walker branch must obey the same preservation rule when it currently calls `core.WithEvalDeadline` unconditionally. The callback timing start is independent of the inherited deadline; callback registration must not reset it.

Changing `SetDeadline` to reinterpret zero globally is rejected: zero is an intentional VM contract, including explicit engine timeout disablement. Allocating a timer context is also rejected by the existing deadline contract.

### Test state and enforcement separately

Existing-service-strict, regression-first. Runtime integration tests capture `core.EvalDeadlineFrom` inside a cooperative GoFunc for the meter/entry-point matrix. They assert absent versus present deadlines and exact equality on reentry. An already-expired inherited eval deadline exercises error propagation without sleeping. Keep the existing bounded long-work timeout regression as an end-to-end check.

Use `runtime/lazy_deadline_test.go`, `runtime/vm_reentry_deadline_test.go`, and `runtime/call_boundary_flag_test.go`. Their package-private `nowFunc` does not control the separate clocks in core and VM; do not assume one injected clock covers all layers. Existing tests for unobserved boundary clock reads guard the lean path.

## Risks / Trade-offs

- Eager deadline resolution on the general metered path adds a clock read when no bound exists → keep the no-meter lean path untouched and preserve an inherited bound without reading the clock again.
- Reentry can arrive with state owned by another boundary → retain its deadline and counters; never call through a fresh independent resource context to reset them.
- Deadline error precedence can change accidentally during unwind → retain existing terminal-error handling and lease-return tests.

## Migration Plan

No API or configuration migration. Update existing deadline docs and CHANGELOG. Reverting this change restores the metered-timeout defect; no stored data changes. Merge before the other two runtime lifecycle changes, with no semantic dependency.
