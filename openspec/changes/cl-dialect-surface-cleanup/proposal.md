## Why

A review of `cl/` found small drift and dead weight around the CL dialect:

- The CL vocabulary carries five identity entries (`cons`, `list`, `reverse`, `apply`, `type`). On a full-base dialect vocabulary is additive (`runtime/engine.go:460`), so each one only rebinds a name to the value it already has. On the eager path that rebind goes through plain `env.Set`, which stores the cell as non-canonical. It is harmless today because none of the five is a VM native operator, but copying the pattern for `+` or `<` would silently turn off the native-op fast path.
- The package doc says CL runs on the VM "when WithBytecode() is enabled"; `runtime.New()` has defaulted to the VM since ADR 0013.
- `TestCL_Dialect_Memoized` repeats the whole stock definition by hand, so it tests a copy that can drift from `cl.Dialect()`.
- `clNth` checks for a list with an empty-case `switch` where a single type assertion does the job, and the `sort` arity message is duplicated.
- `clSort` allocates a fresh two-element args slice on every comparison: about 26.3k allocs and 1.1 MB for a 1000-element sort, half of them that slice.

## What Changes

- Drop the identity entries from the CL vocabulary; `car`, `cdr`, `null`, `append`, `length` and the three adapters remain.
- Keep the CL definition in one unexported `DialectSpec` value; `cl.Dialect()` and the tests build from it, so the test cannot drift from the stock dialect.
- Correct the package doc; simplify the `clNth` list check; share the `sort` arity message.
- Reuse one comparison args buffer in `clSort`, gated on proving no callee keeps the slice.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **Common Lisp dialect keeps shared-core bindings for unrenamed builtins**. The CL surface is unchanged: every dropped name stays callable under its registered builtin name.

## Impact

`cl/cl.go`, `cl/cl_test.go`, possibly `cl/export_test.go`. `cl.Dialect().Fingerprint()` changes; the fingerprint is an in-memory cache key only (`runtime/eval.go:143`, `runtime/lazy_template.go:329`) and is never persisted. No public API change.
