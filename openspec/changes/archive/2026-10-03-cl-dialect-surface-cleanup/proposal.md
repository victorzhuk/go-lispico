## Why

A review of `cl/` found small drift and dead weight around the CL dialect:

- The CL vocabulary carries five identity entries (`cons`, `list`, `reverse`, `apply`, `type`). They are no-ops at runtime: `core.Dialect.AppendVocabBindings` (`core/dialect.go:399`) keeps the incoming canonical flag for an identity entry, `applyVocabulary` (`runtime/engine.go:541`) skips the `setVocabValue`/`setVocabFunc` calls when `canon == b.Canonical && v.Equals(b.Value)`, and the lazy template path (`runtime/lazy_template.go:735`) emits exactly one entry per binding — `NewDialect` skips identity entries in the alias loop (`core/dialect.go:304`). They show up only in `Dialect.Vocab()`/`VocabEntry` and in the fingerprint hash, so removing them changes lookup results and the cache key, not any runtime binding. They are harmless today because none is a VM native operator, but the pattern is misleading and copying it for `+` or `<` would still turn off the native-op fast path through `Set`/`SetFunc` — the canonical flag is preserved only because `applyVocabulary` already short-circuits on the identical write, not because of any property the entry itself adds.
- The package doc says CL runs on the VM "when WithBytecode() is enabled"; `runtime.New()` has defaulted to the VM since ADR 0013.
- Three places repeat the stock definition by hand and could drift from `cl.Dialect()`. They now derive the stock definition from `cl.CLSpec()`: `TestCL_Dialect_StockFingerprint` (`cl/cl_test.go:244`), `TestCL_StockMatchesSpecFingerprint` (`cl/dialect_spec_fingerprint_test.go:13`), and the `vocab` subtest of `TestCL_StockFormTable` (`cl/dialect_parity_test.go:62`).
- `clNth` checks for a list with an empty-case `switch` where a single type assertion does the job, and the `sort` arity message is duplicated.
- `clSort` allocates a fresh two-element args slice on every comparison: about 26.3k allocs and 1.1 MB for a 1000-element sort, half of them that slice.

## What Changes

- Drop the five identity entries (`cons`, `list`, `reverse`, `apply`, `type`) from the CL vocabulary; `car`, `cdr`, `null`, `append`, `length` and the three adapters remain. Each dropped name keeps callable through the stdlib plugin's own registration, which is what made the entry a no-op in the first place.
- Hold the CL definition in one unexported `clSpec core.DialectSpec` value; `cl.Dialect()` and the three duplicated-spec tests build from it, so the tests cannot drift from the stock dialect.
- Correct the package doc; simplify the `clNth` list check; share the `sort` arity message.
- Keep the fresh comparison args slice in `clSort`. The buffer reuse was dropped: `ChildVariadic` binds rest arguments through `NewList(args[len(params):])` and caller-supplied GoFuncs can retain `args`, so a reused slice would mutate captured arguments. `cl/sort_bench_test.go` records the baseline of 26,118 allocs/op and 1,108,249 B/op at n=1000.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **Common Lisp dialect keeps shared-core bindings for unrenamed builtins**. The CL surface is unchanged: every dropped name stays callable under its registered builtin name.

## Impact

`cl/cl.go` plus the three former duplicated-spec test sites (`cl/cl_test.go:244`, `cl/dialect_spec_fingerprint_test.go:13`, `cl/dialect_parity_test.go:62`), which read the spec through `cl.CLSpec()`; a new `cl/export_test.go` is needed to expose the unexported `clSpec` to those tests. Runtime binding does not change: the five dropped names still resolve to the stdlib-registered GoFunc on both evaluators, with the same canonical flag — only the `Dialect.Vocab()`/`VocabEntry` lookup and `Dialect.Fingerprint()` change. The fingerprint is an in-memory cache key only (`runtime/eval.go:143`, `runtime/engine.go:129`, `runtime/lazy_template.go:337`) and is never persisted, so changing it busts nothing on disk. No public API change.
