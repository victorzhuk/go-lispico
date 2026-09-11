## 0. Baseline and prerequisites

- [x] 0.1 Confirm both reviewed rollback triggers at commit `3bcf9c1` against the implementation branch and record baseline failures; confirm `registration-journal` has landed; inspect Makefile limits and existing plugin/lazy/meter tests; record bounded focused commands and the `Init` argument pointer-identity compatibility change without changing the plugin interface.

## 1. Pin failed-operation contracts

- [x] 1.1 Add regressions for failed initial load overwriting value/function/canonical bindings and adding names, plus failed cold/partly materialized stdlib reload; verify old handles, macro behavior, tombstones, registry, counts, and sibling-engine state expose current defects.
- [x] 1.2 Add channel-barrier regressions for host add/rebind/delete, same-name host writes between two plugin writes, host replacement/rebuild, and unrelated lazy materialization during failing initialization; verify the expected host state survives and the baseline fails rather than relying on race scheduling.
- [x] 1.3 Add failure cases for vocabulary rejection, successful retry, direct host registry conflict, and successful unload ownership; verify active-plugin counts are observable and successful last-writer semantics remain pinned.
- [x] 1.4 Add registration-alias cases where `Init` reaches the supplied environment through `Find`, children, evaluator reentry, merge target, retained view, and closures; verify successful use sees live root changes and failure reverts those writes.

## 2. Integrate plugin and lazy lifecycle

- [x] 2.1 Route initialization, reload deletion, and vocabulary mutation through the registration view while keeping registry/ownership/count changes pending; verify failure paths do not publish a successful plugin state.
- [x] 2.2 Add conditional registry publication against concurrent direct host edits; verify publication conflicts preserve the host entry, return an error, and undo only operation-owned bindings.
- [x] 2.3 Attribute lazy registration/materialization and per-engine active/tombstone/installed changes to their initiating operation; verify abort restores old deferred behavior while preserving foreign materialization and sibling templates.
- [x] 2.4 Fence transaction-originated materialization before settlement and journal retirement without blocking unrelated host lookup; verify deterministic in-flight lookup/abort interleavings complete without deadlock.
- [x] 2.5 Replace whole-root snapshot replay on failed `Use`/`ReloadPlugin` with journal abort and publish bookkeeping only after success; verify all section 1 cases, successful retry, and existing successful unload semantics pass.

## 3. Validate and document

- [x] 3.1 Run `go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm -run 'Test(Use|ReloadPlugin|UnloadPlugin|Meter_|Stdlib|Lazy|Env|Merge|Func|Pinned|Call)'` with every new regression name included, the channel-barrier and alias tests under `-race` with the same limits, `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`, `make lint`, and `openspec validate plugin-binding-rollback --strict --json`; classify unrelated baseline failures without weakening coverage; verify no dependency, no plugin-interface method, no ordinary-Eval transaction, and no changed successful-unload ownership policy.
- [x] 3.2 Update existing architecture/plugin lifecycle documentation and CHANGELOG `[Unreleased]`; verify Init argument identity, concurrent-write precedence, transient read visibility, post-return restoration, external-effect exclusions, and the retained-charge gap closed by `plugin-retained-rollback` are explicit.
