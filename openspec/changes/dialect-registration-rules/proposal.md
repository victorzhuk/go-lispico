## Why

Vocabulary and Lisp-2 registration rules are implemented only in `runtime`, twice (eager `applyVocabulary` in `runtime/engine.go:470`, lazy `RegisterValue` in `runtime/lazy_template.go:715`), and `applyVocabulary` re-runs over the whole root environment on every `Use`/`ReloadPlugin`. Reproduced on the current tree:

- **Lisp-2 without a vocabulary.** The function-cell bridge sits after `if vocab == nil { return nil }` (`runtime/engine.go:472-474`). With `FullDialect().Lisp2()` plus stdlib and json, `(+ 1 2)` works (the lazy path bridges it) but `(json/encode 1)` fails with `UndefinedError` on both evaluators; only `(funcall json/encode 1)` works.
- **Host bindings stripped.** On an empty-base dialect, `Bind("host/f", GoFunc)` is callable until the next `Use(json)`, which deletes it: the allowlist pass removes every root GoFunc absent from the vocabulary, including host bindings, and the deletion is journaled under json's registration.
- **Cost per `Use`.** Under CL with eager stdlib, `Use(json)` costs 61.8 KB / 166 allocs against 21.4 KB / 38 under Clojure; the difference is the dialect pass rebuilding a map of every root GoFunc, re-setting every vocabulary entry, and re-bridging every name. Lazy stdlib: 4.4 KB / 49 vs 2.0 KB / 25. Cost grows with root size for each plugin loaded. All startup benchmarks run Clojure, so this path is unmeasured in the repo.
- **Eager/lazy drift.** The two paths already differ: identity vocabulary entries leave a canonical binding on the lazy path and a non-canonical one on the eager path.
- **Core-only embedders.** `core.NewEvaluatorWithDialect(cl.Dialect())` plus `stdlib.New().Init(env)` never applies the vocabulary or the bridge, so `(car ...)` and `(+ 1 2)` are undefined (reported by the architecture review; not re-run here).

## What Changes

- Move the registration rules into one core entry point that decides, for a name a plugin registers, which bindings to create: the vocabulary rename or adapter, the empty-base allowlist, and the Lisp-2 function cell with the canonical flag preserved. Eager and lazy runtime paths both call it.
- The Lisp-2 bridge depends only on the namespace axis, never on whether a vocabulary exists.
- The allowlist applies to names plugins register. Host bindings made through `Bind` are never removed by a later plugin operation.
- The pass is scoped to the names the loading plugin added or changed (`loadPlugin` already snapshots bindings before `Init`), so a `Use` costs O(its own names), not O(root).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **Lisp-2 dialects bridge every plugin builtin**, **Host bindings survive plugin vocabulary passes** and **Vocabulary registration is one rule set on every path**.

## Impact

`core/dialect.go` (registration decision), `runtime/engine.go` (`applyVocabulary`), `runtime/lazy_template.go`, `runtime/plugin.go`, tests in `runtime/` and `core/`, a CL-dialect startup benchmark row, CHANGELOG. Plugin interface unchanged. Related earlier fix: the defun-revert guard (`!existing.Equals(v)` in the bridge) must keep holding; its regression test stays.
