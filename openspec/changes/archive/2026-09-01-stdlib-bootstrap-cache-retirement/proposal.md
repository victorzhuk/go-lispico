## Why

After `stdlib-nil-lookup-semantics` moves `get-in` out of Lisp source, stdlib has
no reusable bootstrap definition. The process-level compiled-artifact cache then
has no producer, while retaining global state, synchronization, test hooks,
benchmarks, interfaces, and public cache exemptions.

Blocked by: `stdlib-nil-lookup-semantics`.

## What Changes

- Remove the process-level stdlib bootstrap artifact cache and its compilation,
  replay, statistics, disable, and reset machinery.
- Remove reusable-source routing from stdlib bootstrap entries and lazy source
  materialization; remaining macros are environment-owned definitions and are not
  shared across Engines.
- Retain immutable source/template metadata used by lazy name discovery, while
  prohibiting shared compiled artifacts and evaluated definition values.
- Retain the bounded per-Engine compiled-chunk cache unchanged for ordinary source.
- Remove the process-level plugin compilation guarantee and cache-limit exemption
  from canonical specs and public resource documentation.
- Replace cache-specific tests/benchmarks with startup behavior and no-global-state
  regressions for eager/lazy, Dialect, concurrent construction, and unload paths.
- **BREAKING** Remove reusable-source parameters/interfaces that existed only for
  this cache: `Env.RegisterSource` and `LazyLayer.RegisterSource` lose their
  `reusable bool` parameter. Custom Go implementations must adopt the simplified
  source-registration contract.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: limit compiled-chunk reuse to the bounded per-Engine cache and
  remove process-level plugin artifact reuse.
- `runtime-api`: remove the special process-level cache exemption from cache
  resource-limit behavior.

## Impact

- Affects runtime bootstrap compilation, lazy-layer source registration, stdlib
  bootstrap metadata, cache tests/benchmarks, cache documentation, and exported
  Go interfaces carrying the reusable flag.
- Removes process-global mutable state and one cross-Engine synchronization point.
- Does not change remaining macro values, eager/lazy language behavior, or the
  ordinary per-Engine bytecode cache. It retains the `core.BootstrapDefiner`
  capability introduced by the ownership prerequisite.
