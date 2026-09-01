## Why

`builtin-resource-accounting` adds the correct batched work and borrowed-result
primitives, but applying them to every existing Builtin would turn a core
foundation into a repository-wide prerequisite. The final stdlib surface still
needs a separate completeness pass after lookup, CL adapter, and nil semantics
settle.

Blocked by: `stdlib-nil-sequence-semantics`.

## What Changes

- Freeze the final active stdlib and CL-adapter vocabulary as an executable
  work/result inventory.
- Assign exactly one owner to every scalable uninterrupted phase and callback
  phase, including transitive helpers and opaque library calls.
- Migrate scalable core-owned work to `core.BuiltinWorkBudget`; rewrite or bound
  opaque phases and mark trusted host `Value` methods explicitly.
- Classify every successful result branch as scalar/singleton, wholly borrowed,
  fresh, incremental persistent, mixed, or callback-produced and charge it once.
- Make collection/depth helpers consume the active evaluator rather than
  `env.Evaluator()` and add nested-scope regressions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: require complete Builtin work and result ownership across the
  final active stdlib surface.

## Impact

- Affects all active stdlib GoFuncs, shared collection kernels, CL adapters,
  transitive helpers, static inventory checks, resource tests, and benchmarks.
- Does not change valid language semantics established by the predecessor
  changes; resource ceilings may be observed earlier and false borrowed-result
  allocation charges may disappear.
