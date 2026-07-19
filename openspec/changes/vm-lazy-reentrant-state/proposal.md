## Why

Every `GoFunc` dispatched from the VM receives a context built by `vm.reentrantCtx` → `core.AdoptEvalState` + `context.WithValue`: an evaluation-state object and a derived context, allocated **eagerly per VM run that dispatches a host function** — whether or not the `GoFunc` ever re-enters the evaluator. On the Callback boundary bench these two account for ~50% of allocated objects (5 allocs/op, 160 B). The overwhelming majority of host functions — the entire stdlib builtin set, typical embedder callbacks — never call back in; they pay for a capability they don't use.

## What Changes

- `reentrantCtx` returns a lightweight context wrapper instead of a materialized state context. The wrapper delegates `Done`/`Err`/`Deadline`/unknown keys to the caller's context; a `Value` request for the evaluation-state key materializes the state on first use — at most once per VM run — from inputs snapshotted at wrapper creation (deadline, structural-depth budget), exactly the values `AdoptEvalState` captures today.
- A `GoFunc` that re-enters (`Engine.Call`/`Apply` with the received context) observes identical behavior: same shared structural-depth and deadline budget, same ADR 0007 limit enforcement. A `GoFunc` that never asks pays one small wrapper allocation per run instead of the state + derived-context pair per dispatch.
- Retention safety unchanged: the wrapper snapshots inputs rather than referencing live pooled-VM state, so a host function that (incorrectly but harmlessly) retains the context past its call cannot observe a recycled VM's internals — matching today's snapshot semantics.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: new requirement — host-function dispatch materializes evaluation state lazily; re-entrant resource-budget sharing preserved.

## Impact

- Code: `core/vm/vm.go` (`reentrantCtx`, wrapper type), `core/eval_state` adoption helper (lazy constructor path).
- Expected: Callback boundary −2 allocs and −60–80 ns/op; every stdlib-builtin-calling workload sheds the same per-dispatch cost on the VM path.
- The existing `runtime-api` boundary requirement ("MAY allocate at most one evaluation-state value" per call) is already satisfied — laziness tightens actual behavior without changing that contract.
- Sequencing: independent; combines with `engine-func-handle` for the Callback parity target.
