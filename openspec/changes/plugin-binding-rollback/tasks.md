## 0. Baseline and prerequisites

- [ ] 0.1 Confirm both reviewed rollback triggers at commit `3bcf9c1` against the implementation branch and record baseline failures; verify no semantic prerequisite and coordinate after the two runtime boundary changes for file overlap only.
- [ ] 0.2 Confirm existing-service-strict, regression-first coverage and the approved concurrent-host-writes policy; enumerate every Env method and core/VM direct-field path that can bypass a forwarding registration view, delivering an ownership checklist before source changes.
- [ ] 0.3 Inspect Makefile limits and existing plugin/lazy/meter/core-env tests; record bounded focused commands and the Init argument pointer-identity compatibility change without changing the plugin interface.

## 1. Pin failed-operation contracts

- [ ] 1.1 Add regressions for failed initial load overwriting value/function/canonical bindings and adding names, plus failed cold/partly materialized stdlib reload; verify old handles, macro behavior, tombstones, registry, counts, and sibling-engine state expose current defects.
- [ ] 1.2 Add channel-barrier regressions for host add/rebind/delete, same-name host writes between two plugin writes, host replacement/rebuild, and unrelated lazy materialization during failing initialization; verify the expected host state survives and the baseline fails rather than relying on race scheduling.
- [ ] 1.3 Add failure cases for vocabulary and retained settlement, successful retry, direct host registry conflict, and successful unload ownership; verify counts and exact retained charges/releases are observable and successful last-writer semantics remain pinned.
- [ ] 1.4 Add registration-alias cases for `Find`, children, evaluator reentry, merge target, retained view, and closures capturing the supplied environment; verify successful use sees live root changes and failure preserves ownership attribution.

## 2. Introduce the core registration ownership seam

- [ ] 2.1 Add the new forwarding registration view and operation journal without copying Env locks or lexical maps; verify canonical root identity stays fixed, view reads match root reads, and retained views switch to ordinary forwarding after completion.
- [ ] 2.2 Route binding/canonical/context variants, `ReplaceCell`, deletion, and cell lookups through the canonical owner and tag only view-originated writes; verify same-value host writes are distinguished by provenance and every namespace case passes.
- [ ] 2.3 Route `Find`, child creation, evaluator/capture paths, enumeration, merge/rebuild, and root configuration access through owner-aware views; verify the ownership checklist from 0.2 has no silent raw-root escape.
- [ ] 2.4 Implement per-entry before-images, foreign-write rebasing, and conditional abort under the root lock; verify plugin→host→abort and plugin→host→plugin→abort retain host state, including deletion/recreation.
- [ ] 2.5 Preserve live cell identity and monotone cache generations across rollback and rebuild; verify preexisting `Fn`/`PinnedFn` calls and VM/macro caches cannot retain a failed definition.
- [ ] 2.6 Track operation-owned retained capacity and release only removed charges, keeping host-adopted cells charged and external meter calls outside env/lazy locks; verify capacity rejection, partial settlement failure, and no double release with existing accounting fixtures.

## 3. Integrate plugin and lazy lifecycle

- [ ] 3.1 Route initialization, reload deletion, and vocabulary mutation through the registration view while keeping registry/ownership/count changes pending; verify failure paths do not publish a successful plugin state.
- [ ] 3.2 Add conditional registry publication against concurrent direct host edits; verify publication conflicts preserve the host entry, return an error, and undo only operation-owned bindings.
- [ ] 3.3 Attribute lazy registration/materialization and per-engine active/tombstone/installed changes to their initiating operation; verify abort restores old deferred behavior while preserving foreign materialization and sibling templates.
- [ ] 3.4 Fence transaction-originated materialization before settlement and journal retirement without blocking unrelated host lookup; verify deterministic in-flight lookup/abort interleavings complete without deadlock.
- [ ] 3.5 Replace whole-root snapshot replay on failed `Use`/`ReloadPlugin` with journal abort and publish bookkeeping only after success; verify all section 1 cases, successful retry, and existing successful unload semantics pass.

## 4. Validate and document

- [ ] 4.1 Run bounded focused runtime/core coverage with `go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm -run 'Test(Use|ReloadPlugin|UnloadPlugin|Meter_|Stdlib|Lazy|Env|Merge|Func|Pinned|Call)'`, ensuring all new regression names are included; record commands, names, and results.
- [ ] 4.2 Run the channel-barrier concurrency and alias tests with `-race -timeout 2m -p 2 -parallel 2`, then `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; classify unrelated baseline failures without weakening coverage.
- [ ] 4.3 Update existing architecture/plugin lifecycle documentation and CHANGELOG `[Unreleased]`; verify Init argument identity, concurrent-write precedence, transient read visibility, post-return restoration, and external-effect exclusions are explicit.
- [ ] 4.4 Run `make lint` and `openspec validate plugin-binding-rollback --strict --json`; verify the implementation adds no dependency, no plugin-interface method, no ordinary-Eval transaction, and no changed successful-unload ownership policy.
