## Why

The review at commit `3bcf9c1` reproduced two incomplete rollbacks: a failed replacement of freshly loaded stdlib left its original registry entry present but `(+ 1 2)` undefined, and a failing `Use` left an existing value overwritten. Current name diffs omit overwritten bindings; reload restoration omits lazy attachment state and cannot safely replay the whole root environment over concurrent writes.

## What Changes

- Track plugin-operation writes separately from concurrent host writes and restore only the failed operation's engine-owned changes.
- Restore registry and binding ownership, value/function cells and canonical status, lazy attachment/tombstones, handles, and retained ownership after initialization, vocabulary, or settlement errors.
- Preserve concurrent host writes, including same-name conflicts and writes between two plugin writes.
- Keep successful unload's existing last-writer behavior and per-engine root identity.
- Introduce a registration view and mutation journal in core; their names and Go surface are new implementation work described in the design.
- **BREAKING behavioral detail:** the `*core.Env` supplied to `Plugin.Init` becomes a forwarding registration view rather than pointer-identical to `RootEnv()`. Its successful retained use continues to target the same root; direct env-pointer identity assumptions require adjustment.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: add **Failed plugin operations restore owned binding state**.

## Impact

Changes `runtime/plugin.go`, `runtime/lazy_template.go`, core environment mutation/retained-accounting seams, and their tests. No dependency change or new plugin-interface method. This is a registration lifecycle change requiring compatibility and concurrency coverage, not a whole-environment snapshot fix.

Rollback excludes plugin Go objects, external effects, and writes through unrelated retained environment references. Ordinary `Eval` effects and successful unload ownership remain unchanged. Update existing architecture/plugin documentation and CHANGELOG during implementation.

No semantic dependency on another proposed change. Merge after `metered-call-deadlines` and `evaluation-outcome-settlement` because runtime files overlap.
