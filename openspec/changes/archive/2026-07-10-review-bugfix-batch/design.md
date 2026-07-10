# Design — review-bugfix-batch

## Context

Nine verified defects from a full review; each fix is smallest-correct. The only
decision with real alternatives is how `UnloadPlugin` learns which bindings a
plugin owns, since `Plugin.Init(env)` registers names opaquely.

## Goals / Non-Goals

- Goals: fix the nine findings, add the regression tests that were structurally missing.
- Non-Goals: VM/compiler feature work (vm-first change), REPL binary, Lisp-side modules, runtime error positions.

## Decisions

- **Binding ownership via env-key snapshot.** `Use` snapshots root-env keys before and
  after `Init`; the difference is recorded as the plugin's bindings and deleted on
  `UnloadPlugin` / before `ReloadPlugin` re-`Init`. Alternative — adding a
  `Names()` method to `Plugin` — rejected: breaks every existing plugin
  implementation for a bookkeeping concern the runtime can observe itself.
  `Env` needs a `Delete` (and key-listing) capability scoped to the root frame;
  keep it unexported-friendly and minimal.
- **Bootstrap uses `env.Evaluator()`, falling back to `core.NewEvaluator()` only
  when nil.** The env already carries the engine's dialect-aware evaluator; the
  fallback preserves standalone `core.NewEnv` usage in stdlib's own tests.
- **Map construction uses the documented `Set` escape hatch** while the map is
  fresh and unshared — the same pattern `vm.go` `OpMakeMap` already uses. No new
  API; immutability contract unchanged because the map has not been shared yet.
- **Regression tests for the bootstrap bug go through `runtime.New`.** stdlib's
  own tests construct `core.NewEnv` + `core.NewEvaluator` directly and can never
  catch dialect-axis integration bugs; the new tests live in `runtime` tests.

## Risks / Trade-offs

- [Snapshot diff misses bindings overwritten by a later plugin] → acceptable: last
  writer wins today too; unload restores nothing, it only deletes names the plugin
  introduced. Documented in the method comment.
- [Deleting `compiler.MacroExpander`/`CompileExpanded` is API-breaking for external
  importers] → zero internal callers, pre-1.0 alpha; noted in proposal Impact.

## Migration Plan

Single PR, no data or config migration. Rollback = revert.

## Open Questions

None.
