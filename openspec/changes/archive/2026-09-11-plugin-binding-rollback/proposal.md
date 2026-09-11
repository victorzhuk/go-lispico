## Why

The review at commit `3bcf9c1` reproduced two incomplete rollbacks: a failed replacement of freshly loaded stdlib left its original registry entry present but `(+ 1 2)` undefined, and a failing `Use` left an existing value overwritten. Current name diffs omit overwritten bindings; reload restoration omits lazy attachment state and cannot safely replay the whole root environment over concurrent writes.

## What Changes

- Pass `Plugin.Init` a registration view from `registration-journal` and abort its journal on failed `Use` or `ReloadPlugin`, restoring only the failed operation's engine-owned changes.
- Restore registry and binding ownership, value/function cells and canonical status, lazy attachment/tombstones, and handles after initialization, vocabulary, or settlement errors.
- Preserve concurrent host writes, including same-name conflicts and writes between two plugin writes.
- Keep successful unload's existing last-writer behavior and per-engine root identity.
- Retained-charge ownership on failure is out of this change; see `plugin-retained-rollback`.
- **BREAKING behavioral detail:** the `*core.Env` supplied to `Plugin.Init` becomes a forwarding registration view rather than pointer-identical to `RootEnv()`. Its successful retained use continues to target the same root; direct env-pointer identity assumptions require adjustment.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: add **Failed plugin operations restore owned binding state**.

## Impact

Changes `runtime/plugin.go`, `runtime/lazy_template.go`, the conditional publication seam in `core.Registry` (new `Registry.Generation`, `Registry.PublishIf`, `CodeRegistryConflict`), and their tests. `core/env.go` and `core/registration.go` change so lookups, deletions, and lazy-layer access reached through a view carry the view to the lazy layer, superseding the `registration-journal` ruling that lazy lookups receive the root; `Env.Evaluator` reads under the owner lock. A host `RegisterValue` outside any plugin operation binds immediately instead of deferring into an inactive template key. No dependency change or new plugin-interface method. This is a registration lifecycle change requiring compatibility and concurrency coverage, not a whole-environment snapshot fix.

Rollback excludes plugin Go objects, external effects, and writes through unrelated retained environment references. Ordinary `Eval` effects and successful unload ownership remain unchanged. Update existing architecture/plugin documentation and CHANGELOG during implementation.

Depends on `registration-journal`. `plugin-retained-rollback` depends on this change. Second of three changes split from the original scope.
