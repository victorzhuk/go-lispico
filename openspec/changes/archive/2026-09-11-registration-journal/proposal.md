## Why

A failed plugin operation cannot be rolled back precisely because root environment mutations carry no provenance. `snapshotBindings` diffs names only, and `restoreRootEnv` replays a whole-root snapshot over any concurrent host write (review at commit `3bcf9c1`). `plugin-binding-rollback` needs a core seam that attributes root writes to one operation and reverts only the writes that operation still owns.

## What Changes

- Add a forwarding registration view over a root `Env`: reads and writes delegate to the fixed canonical root; writes reached through the view carry an opaque operation identity. The view is not a lexical child, a copied environment, or a copied mutex.
- Add an optional per-root mutation journal: per namespace/name before-images (cell pointer, live/tombstoned state, value, canonical marker, prior map membership), rebased when a foreign write intervenes. Abort restores an entry only while its current state is still the operation's latest write.
- Route every `Env` surface that can reach the root through the view, including `Find`, `Child`, `ChildVariadic`, evaluator reentry, `MergeInto`, `MergeIntoCanonical`, `Rebuild`, and root configuration accessors; audit core and VM direct field access for escapes.
- Keep live cell identity across abort; versions, `NameGen`, and `MacroEpoch` stay monotone.
- Environments without an active operation pay one absent-journal branch on affected mutations.
- Out of this change: runtime wiring of `Use`/`ReloadPlugin` (`plugin-binding-rollback`) and retained-capacity ownership in the journal (`plugin-retained-rollback`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: add **Registration view attributes and reverts owned root writes**.

## Impact

Core environment mutation paths (`core/env.go` and a new file for the view and journal), new `core/registration_*_test.go` regression files plus one test in `core/vm/vm_test.go`, and an audit of direct cell/field access in `core/vm`. Adds exported core API whose names are new and fixed in design; no dependency, no `Plugin` interface change, no runtime behavior change until `plugin-binding-rollback` wires it.

First of three changes split from the original `plugin-binding-rollback`; `plugin-binding-rollback` and `plugin-retained-rollback` depend on it.
