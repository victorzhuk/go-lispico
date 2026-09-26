## Why

The dialect review left a set of small, verified leftovers. None is urgent alone; together they are dead surface, one panic path and one layering leak.

- **Dead truthiness hook.** ADR 0005's amendment removed the truthiness axis, but `Dialect.TruthyFunc()` (always `IsTruthy`), private `isTruthy`, the evaluator's `truthy` field and `vm.Chunk.Truthiness` survive (`core/dialect.go:322-336`, `core/compiler/compiler.go:82`, `core/vm/vm.go:626`). Every conditional goes through an indirect call that can only ever be `IsTruthy`.
- **Panic path.** `compiler.NewCompilerWithDialect(name, nil)` dereferences nil at `compiler.go:82`, against the "no panics" invariant. The pointer only acts as a nil sentinel, which costs eight `c.dialect != nil` branches and a `core.Dialect{}` fallback.
- **Stale doc.** `IsIdentity`'s comment says only the identity dialect is safe on the VM; ADR 0006 removed that gate. `IsIdentity` has no production caller.
- **Layering.** `cl` imports `core/vm` only to name `*vm.Closure` in `isCallable` (`cl/cl.go:236`). A future callable kind would fail `mapcar`/`sort` with a TypeError. Core has no callable predicate.
- **Per-engine key field.** `dialectFP` sits in every chunk-cache key although the cache is per engine and the value is constant there (`runtime/eval.go:41`, comment at `:158`).
- **Modernization.** `copyVocab` loop → `maps.Copy` (`core/dialect.go:250`); backward loop → `slices.Backward` (`:550`).

## What Changes

- **BREAKING (pre-1.0):** remove `Dialect.TruthyFunc`, `isTruthy`, `vm.Chunk.Truthiness` and the evaluator's `truthy` indirection; conditionals call `IsTruthy` directly.
- The compiler takes `core.Dialect` by value; the zero value is the identity dialect. No nil path remains.
- Fix the `IsIdentity` doc; keep the method (tests and embedders may use it).
- Add `core.IsCallable(Value) bool`, backed by a small interface the VM closure implements; `cl` drops its `core/vm` import.
- Drop `dialectFP` from the per-engine `cacheKey`.
- Apply the two modernizations.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **Dialect adapters accept every callable kind**.
- `bytecode-vm`: add **Compiler construction never panics**.

## Impact

`core/dialect.go`, `core/eval.go`, `core/compiler/compiler.go`, `core/vm/chunk.go`, `core/vm/vm.go`, `core/types.go` or a new small file for `IsCallable`, `cl/cl.go`, `runtime/eval.go`, `runtime/engine.go`, tests, CHANGELOG (breaking removals listed). No perf claim is made for removing the indirect call: local latency is not trustworthy on this machine, and the release gate decides.
