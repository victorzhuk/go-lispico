## Why

yagel ADR 0105 / ADR 0104 compose per-evaluation, Routine, Workflow, and Session ledgers through ranked leases — embedders need a public `Meter` API to draw credit at one rank, return unused credit before blocking waits, and have a child meter fail-closed when its parent's budget is exhausted. Change 1 provides the leaf primitive (per-eval reduction/allocation counters); this change adds the public composition API on top.

## What Changes

- New `runtime.Meter` interface:
  ```go
  type Meter interface {
      ChargeReductions(n int64) error
      ChargeAllocation(b int64) error
      ChargeRetained(env *core.Env, bytes, slots int64) error
      Lease(rank int, reductions, allocation, retainedBytes, retainedSlots int64) (*Lease, error)
  }
  ```
- `Lease` pre-allocates credit under a rank (lower rank = outer scope); `(*Lease).Return()` releases unused credit back to the parent meter.
- Engine entry points (`Eval`, `EvalWithBindings`, `Call`, `(*Fn).Call`) read a `Meter` from the caller's context via `runtime.WithMeter(ctx, m)`; when present, every reduction/allocation/retained charge hits the Meter in addition to the per-eval `evalState` ledger (the evalState ledger stays as the per-eval ceiling; the Meter is the cross-scope composition).
- `runtime.NewChildMeter(parent Meter, rank int, limits ResourceLimits) Meter` returns a child meter whose `Lease` draws from parent at the given rank; an exhausted parent makes the child's `Charge*` fail with `CodeResourceLimit`.
- Meters expose cumulative Session-total non-resettable counters alongside the draw-down budget: `Lease.Return` credits unused budget back, but the consumed totals only ever increase.
- Introduces ADR 0013.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `Meter composition through ranked leases`.

## Impact

- Code: new `runtime/meter.go`, `runtime/engine.go` (read meter from ctx at every entry point), `core/eval.go` + `vm/vm.go` (route charges through the meter when present).
- Depends on Changes 1 and 3 (uses their charge primitives).
