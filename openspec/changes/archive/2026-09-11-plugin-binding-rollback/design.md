## Context

See proposal.md for both verified failures. `snapshotBindings` tracks only name additions. `snapshotRootEnv` stores values without canonical flags, cell identity, or lazy state; `restoreRootEnv` rewrites the entire root and rebuilds it. That cannot preserve writes from `Eval`, `RootEnv().Set`, or lazy lookup that overlap initialization.

`Plugin.Init` accepts a concrete `*core.Env`, without a context or registration token. Holding `engine.mu` does not serialize direct environment access. Holding the environment lock across `Init` would deadlock its own binding writes. Shared-root identity and live `Cell` identity are observable through handles, closures, and VM caches.

`registration-journal` supplies the forwarding view and mutation journal that attribute and revert operation-owned root writes. This change wires the plugin lifecycle through them. Retained-charge ownership on failure belongs to `plugin-retained-rollback`.

## Goals / Non-Goals

**Goals:** restore failed-operation writes with explicit ownership, preserving concurrent host writes and existing alias behavior; keep registry and plugin bookkeeping consistent with the returned result.

**Non-Goals:** read isolation during initialization, rollback of arbitrary Go/external effects, successful-unload ownership redesign, freezing host evaluation during initialization, transactional ordinary `Eval`, the view and journal mechanics (`registration-journal`), and retained-charge settlement on failure (`plugin-retained-rollback`).

## Decisions

### Init receives a registration view

Runtime obtains a registration view for its root and passes it to `Init`. Root writes remain visible immediately, matching current registration behavior. The canonical root returned by `RootEnv()` never changes. After successful completion the retained view forwards without an active operation; closures and plugin-held view references continue to see root updates. An aborted view also becomes an ordinary forwarding view.

**Compatibility:** `Init`'s argument is no longer pointer-identical to `RootEnv()`. No `Plugin` method or signature changes. This identity change must be documented.

### Abort replaces snapshot replay

Failed `Use` and `ReloadPlugin` abort the operation's journal instead of `snapshotBindings` diffs and `restoreRootEnv` replay. Reload deletion of the old plugin's names and vocabulary writes run through the view, so they revert with the rest of the operation. `callCache` entries for reverted names are dropped; existing `Fn`/`PinnedFn` handles keep observing the restored cell unless a host replaced or deleted it.

### Lazy state and metadata follow the same ownership rule

Keep old registry entries, `bindings`, and active-plugin counts pending until initialization, vocabulary work, and settlement succeed. If public registry mutation races the final publication, compare its entry generation/identity with the operation's starting observation; preserve the host entry and abort with a conflict error rather than overwriting it. This needs a new conditional publication seam in `core.Registry`; no public plugin-interface change is required.

Lazy bookkeeping requires operation-tagged changes for active version, tombstones, installed names, and materialization count. Pass registration identity through `RegisterValue`, `RegisterSource`, and lookup/materialization reached from the view; do not infer identity solely from mutable `loadingPlugin`. Restore only entries still owned by the failed operation, with before-images rebased after host materialization/deletion. Never restore all of `state.active`, `state.installed`, or `state.tombstoned` from a snapshot. Published process-level templates remain immutable and sibling engines remain untouched.

Operation completion must fence in-flight materialization started through its view: close registration, wait for those operations without holding env/lazy locks, then settle and publish or undo. Host materialization remains independent. A late use of the completed view is an ordinary host operation, not a mutation of a retired journal.

### Verification focuses on interleavings and aliases

Existing-service-strict, regression-first. Extend `runtime/plugin_test.go`, `runtime/lazy_materialize_test.go`, and `runtime/meter_test.go`. Channel barriers inside an initializing plugin make host writes deterministic; no scheduling sleeps. Cover disjoint and same-name host writes, host delete/recreate, host writes between two plugin writes, concurrent lazy first touch, and host registry change at publication. Repeat these under the race detector.

Cross both namespaces, canonical operators, macros, existing call handles, partial lazy state, shared templates, successful retry, closures capturing the supplied view, `Find`-derived owners, child scopes, evaluator reentry, and merge targets. Tests of successful unload pin its existing last-writer semantics from the archived `2026-07-10-review-bugfix-batch/design.md`.

## Risks / Trade-offs

- Lazy attribution through mutable `loadingPlugin` misattributes host materialization → identity travels explicitly through the view.
- Readers may observe transient plugin definitions → retain current read visibility and document that rollback guarantees post-return state, not read isolation.
- Registry publication can conflict with direct host registry edits → return a conflict failure and preserve the host mutation.
- Fencing in-flight materialization can deadlock → wait without env/lazy locks; deterministic interleaving tests.
- Retained charges for removed cells still leak until `plugin-retained-rollback` lands → documented, and that change is ordered next.

## Migration Plan

Land after `registration-journal`. Keep successful ownership semantics unchanged. Document the `Init` argument identity change, post-return rollback guarantee, and excluded external effects in existing architecture/plugin docs; add a CHANGELOG entry. No stored data or dependency migration. Reversion restores the incomplete rollback behavior.

## Implementation plan

Base `477d999`, tier heavy, mode existing-service-strict, lenses `spec` + `quality` + `perf` (the view lookup family, lazy miss path and `Env.Evaluator` sit on hot paths). One integration worktree; c0 → c5 run serially in package `runtime` (each red stage writes one new test file; a red stage may run while the previous coder runs); c7 runs in its own worktree in parallel and merges back before the floor. A contract test, once written and sealed, is read-only for every later stage; a change to it goes back through the test writer.

| Chunk | Tasks | Shape | Red file | Coder |
|---|---|---|---|---|
| c0 | 0.1 | first | — (waiver) | zpatcher |
| c1 | 1.4, 2.1 | after c0, red after c0 | `runtime/plugin_view_rollback_test.go` | go-coder |
| c2 | 1.3, 2.2 | after c1, red after c0 | `runtime/plugin_registry_publish_test.go` | go-coder |
| c3 | 1.1, 2.3 | after c2, red after c0 | `runtime/lazy_rollback_test.go` | go-coder |
| c4 | 1.2, 2.4 | after c3, red after c3 | `runtime/lazy_fence_test.go` | go-coder |
| c5 | 2.5 | after c4, red after c0 | `runtime/plugin_reload_rollback_test.go` | go-coder |
| c6 | 3.1 | after c5 | — (waiver: the floor) | — |
| c7 | 3.2 | parallel, shard `docs` | — (waiver: docs) | coder |

**c0 — FIELD-FIRST inert API.** `core/error.go`: `CodeRegistryConflict = "RegistryConflictError"` and `NewRegistryConflictError(name string) *LispicoError` (message `plugin %q registry entry changed during registration`), next to `CodeRegistrationActive`. `core/plugin.go`: `(*Registry).Generation(name string) uint64` returning 0 and `(*Registry).PublishIf(p Plugin, gen uint64) error` returning nil. No `Plugin` interface change. `NO-RED-WAIVER` / `NO-TESTER-WAIVER`: no observable contract.

**c1 — registration view lifecycle.** `Use` refuses a duplicate through `Registry.Get` (`register plugin %s: %w`), opens `BeginRegistration` on the root, and passes `reg.Env()` to `Init`, the lazy build callback, reload deletion and `applyVocabulary`. Registry entry, `bindings`, activation and `ActivePlugins` are published only after `FinishEval`; failure runs `reg.Abort()`. `ReloadPlugin` keeps its snapshot replay as an interim step until c5. `Env.Evaluator` reads `eval` under the owner read lock. Red: `TestUseFailedInitRestoresOverwrittenBindings`, `TestUseFailedInitRestoresMacro`, `TestUseRegistryPendingDuringInit`, `TestReloadPluginRegistryKeepsOldDuringInit`, `TestReloadPluginSettlementFailureKeepsActiveCount`, `TestUseFailedVocabularyRestoresOwnedBindings`, `TestUseFailedInitKeepsConcurrentHostWrites`, `TestUseFailedInitKeepsHostLazyMaterialization`, `TestUseFailedInitRevertsAliasWrites`, `TestUseEvaluatorReadDuringFailedInitIsRaceFree`, and the guard `TestUseSuppliedEnvStaysLiveAfterSuccess`. redRun runs under `-race`, because the Evaluator test fails only there.

**c2 — conditional registry publication.** `Registry` entries carry a generation from a registry-wide sequence (absent = 0). `Use` reads `Generation` before `BeginRegistration`, `ReloadPlugin` before `Get`; both publish with `PublishIf` after `FinishEval` and before `Complete`. A changed generation returns `*core.LispicoError` with `CodeRegistryConflict`, wrapped `publish plugin %s: %w`, keeps the host entry, and aborts. Red: `TestUsePublishIfRejectsStaleGeneration`, `TestUseRegistryConflictKeepsHostEntry`, `TestReloadPluginRegistryConflictKeepsHostRemoval`.

**c3 — lazy attribution.** The view carries its root's lazy layer; view lookups, enumeration, `Find` and `Delete` hand the view (not the root) to the layer, and the view `Get` family consults the layer only when the root has no live binding. `lazyOp` (declared here) journals installed membership, tombstones and the materialized count per name, with a foreign flag; `endOp(false)` undoes owned entries before `reg.Abort()`. `loadingPlugin`, `loadingVersion` and `m.eager` go away. A host `RegisterValue` outside an operation binds immediately. Red: `TestReloadPluginFailedColdStdlibKeepsDeferredNames`, `TestLazyViewMaterializationRevertsWithFailedUse`, `TestLazyViewDeleteTombstoneRevertsWithFailedUse`, `TestLazyRegisterValueWithoutOperationBindsImmediately`, and the guard `TestLazyHostTombstoneDuringFailedUseSurvives`.

**c4 — materialization fence.** An attributed `materializeOne` joins `op.inflight` under `state.mu`; `m.fence(op)` marks the op closed, drops `state.mu`, and waits with no env or lazy lock held, before `FinishEval` on both paths. After the fence, lookups through the view take the host path. Red (`-race`): `TestLazyFenceWaitsForInFlightViewMaterialization`, `TestLazyFenceSettlesInFlightMaterializationOnSuccess`. Seeding: a one-shot evaluator probe on the `(defmacro -> ` source, channel barriers, 2 s guards on the spin and every post-release wait, no sleeps.

**c5 — journal abort on reload.** `snapshotRootEnv`, `restoreRootEnv`, `rootEnvSnapshot` and `rollbackPluginUse` are deleted. `ReloadPlugin` fails the same way `Use` does: fence, `FinishEval`, `endOp(false)`, `reg.Abort()`, then `callCache.drop` for the names the operation wrote. Red: `TestReloadPluginFailedInitRestoresCanonicalBinding`, `TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion`, `TestReloadPluginFailedInitKeepsConcurrentHostWrites`, and the guards `TestUseRetryAfterFailedInitPublishesOnce` and `TestUnloadPluginKeepsLastWriterOwnership`.

**c6 — floor.** `NO-RED-WAIVER` / `NO-TESTER-WAIVER`: runs the floor below.

**c7 — docs.** `ARCHITECTURE.md` (Plugin Loading Flow), `CONTEXT.md` (Registration view), `CHANGELOG.md` `[Unreleased]`: the identity of the `Init` argument, concurrent-write precedence, transient read visibility, post-return restoration, excluded external effects, the retained-charge gap closed by `plugin-retained-rollback`, the new `Registry` API and conflict code, `RegisterValue` binding immediately, and the residual unlocked `eval` copy in `NewEnv`. `NO-RED-WAIVER` / `NO-TESTER-WAIVER`: documentation only.

Each chunk's `verify` runs `go build ./core/... ./runtime/...`, `go vet ./runtime ./core`, the whole `./runtime ./core ./core/vm` test set with `-skip` of every later chunk's red tests, the race leg over the concurrency tests landed so far, and `golangci-lint run ./runtime/... ./core/...`. The literal commands are in the appendix.

**Floor:** `make lint && make test GOTESTFLAGS='-timeout 10m -p 2 -parallel 2'`, then the channel-barrier and fence tests under `-race`, then `go test -race` over `./core ./core/vm` for `Test(Env|Merge|Registration|Registry)`. `openspec validate plugin-binding-rollback --strict` runs in the primary checkout.

**Risks:** fence deadlock (the wait holds only `e.mu`, and materialization never takes it); host materialization misattributed (identity is the view pointer under `state.mu`); the Evaluator race is fixed, but the unlocked `eval` copy in `NewEnv` remains and is documented; registry conflicts fail closed; a transient miss between `endOp(false)` and `Abort` is covered by the post-return guarantee; retained charges leak until `plugin-retained-rollback`, whose plan must be re-reviewed against the final `Use`/`ReloadPlugin` order.

## Plan appendix

```json
{
  "v": 2,
  "change": "plugin-binding-rollback",
  "baseSha": "477d999d16472c1177947cff6bbaa552de1ccc92",
  "generatedAt": "2026-09-11T18:27:21.977Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "estimateHours": 3.6,
  "chunks": [
    {
      "id": "c0",
      "taskIds": [
        "0.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "baseline-inert-api",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "0.1",
          "file": "Makefile",
          "symbol": "GOTESTFLAGS / test / lint",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "none; record limits: `make test` = `go test $(GOTESTFLAGS) ./...`, `make lint` = `golangci-lint run`, default GOTESTFLAGS `-timeout 2m` (no -p/-parallel cap; task 3.1 passes them inline)"
        },
        {
          "task": "0.1",
          "file": "core/plugin.go",
          "symbol": "Plugin.Init",
          "anchor": "Init(env *Env) error",
          "change": "no signature change; record compatibility: Init arg becomes the registration view, not pointer-identical to RootEnv(); doc comment may need the identity note"
        },
        {
          "task": "0.1",
          "file": "core/registration.go",
          "symbol": "Env.BeginRegistration",
          "anchor": "func (e *Env) BeginRegistration() (*Registration, error) {",
          "change": "none; confirms registration-journal landed (archived 2026-09-11-registration-journal, HEAD 477d999); returns CodeRegistrationActive on nested begin"
        },
        {
          "task": "0.1",
          "file": "core/error.go",
          "symbol": "CodeRegistryConflict / NewRegistryConflictError",
          "anchor": "const CodeRegistrationActive = \"RegistrationActiveError\"",
          "change": "add CodeRegistryConflict and NewRegistryConflictError next to CodeRegistrationActive"
        },
        {
          "task": "0.1",
          "file": "core/plugin.go",
          "symbol": "Registry.Generation / Registry.PublishIf",
          "anchor": "func (r *Registry) RegisterNoCheck(p Plugin) {",
          "change": "add inert Generation (returns 0) and PublishIf (returns nil)"
        }
      ],
      "contract": {
        "states": [
          "baseline-recorded",
          "inert-declared"
        ],
        "transitions": [
          {
            "input": "first source change of the change",
            "state": "baseline-recorded",
            "effect": "no-op",
            "evidence": "tasks.md 0.1"
          },
          {
            "input": "red stages compile against the inert members",
            "state": "inert-declared",
            "effect": "set",
            "evidence": "FIELD-FIRST rule; red tests name core.CodeRegistryConflict, core.NewRegistryConflictError, Registry.Generation, Registry.PublishIf"
          }
        ],
        "forbidden": [
          "any behavior in an inert member: Generation always returns 0, PublishIf returns nil and mutates nothing",
          "any change to the Plugin interface (core/plugin.go:12-23) or go.mod/go.sum"
        ],
        "seeding": [
          "none: no test seeds this seam"
        ],
        "budgets": [
          "none apply: inert members execute no production path"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "0.1 core/error.go next to CodeRegistrationActive (error.go:134-142): const CodeRegistryConflict = `RegistryConflictError`; func NewRegistryConflictError(name string) *LispicoError returning &LispicoError{Code: CodeRegistryConflict, Message: fmt.Sprintf(`plugin %q registry entry changed during registration`, name)} (constructor is final, not inert)",
        "0.1 core/plugin.go: func (r *Registry) Generation(name string) uint64 { return 0 } (inert); func (r *Registry) PublishIf(p Plugin, gen uint64) error { return nil } (inert)",
        "0.1 record baseline: run the c1..c5 redRun commands once red files land and record every red test failing on an assertion (not on compile)"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go build ./core/... ./runtime/... && go vet ./runtime ./core && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime && golangci-lint run ./core/... ./runtime/...",
      "coder": "zpatcher"
    },
    {
      "id": "c1",
      "taskIds": [
        "1.4",
        "2.1"
      ],
      "prev": "c0",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "view-lifecycle",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.4",
          "file": "runtime/meter_test.go",
          "symbol": "evaluatorSetupPlugin",
          "anchor": "type evaluatorSetupPlugin struct {",
          "change": "pattern for evaluator reentry via env.Evaluator().Eval(ctx, form, env); add alias plugins reaching the view via Find, child scope (NewChild), MergeInto target, retained view, closure; success sees live root updates, failure reverts"
        },
        {
          "task": "1.4",
          "file": "core/env.go",
          "symbol": "Env.Evaluator",
          "anchor": "return e.owner().eval",
          "change": "carried defect CONFIRMED: reads root.eval with no lock while Registration.Abort writes root.eval under root.mu (core/registration.go `root.eval = r.eval.prior`) and setEvaluator writes under e.mu → take owner RLock (or make eval atomic) before Abort is wired into Use; evaluator-reentry alias test under -race exposes it"
        },
        {
          "task": "2.1",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.initPlugin",
          "anchor": "if name != \"\" || e.lazyMaterializer == nil {",
          "change": "pass reg.Env() (view) instead of e.rootEnv to p.Init on both branches (also inside `return stdlibLazyTemplateRegistry.ensureLayer(key, eager, func() error {`); stop relying on loadingPlugin for attribution"
        },
        {
          "task": "2.1",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.Use",
          "anchor": "func (e *engineImpl) Use(p core.Plugin) (err error) {",
          "change": "BeginRegistration on rootEnv; defer registry.Register (currently first, line `if err := e.registry.Register(p); err != nil {`), bindings, incPlugins until Init+vocab+FinishEval succeed; Complete on success, Abort on any error"
        },
        {
          "task": "2.1",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.ReloadPlugin",
          "anchor": "func (e *engineImpl) ReloadPlugin(p core.Plugin) (err error) {",
          "change": "open registration before old-name deletion so removePluginBindings runs through the view; keep old registry entry/bindings/count pending (no Unregister/RegisterNoCheck dance); count bump at `if !hadOld {` currently precedes FinishEval and is never undone on settlement error"
        },
        {
          "task": "2.1",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.removePluginBindings",
          "anchor": "func (e *engineImpl) removePluginBindings(name string) {",
          "change": "take the target env (view during reload, root during unload); deletes via view become journaled; keep callCache drop + BumpMacroEpoch"
        },
        {
          "task": "2.1",
          "file": "runtime/engine.go",
          "symbol": "engineImpl.applyVocabulary",
          "anchor": "func (e *engineImpl) applyVocabulary() error {",
          "change": "take env param (view) instead of e.rootEnv for Get/Delete/Set/SetFunc/SetFuncCanonical so vocab writes (e.g. `e.rootEnv.Delete(name)`) revert with the op"
        },
        {
          "task": "2.1",
          "file": "runtime/engine.go",
          "symbol": "engineImpl.bindings",
          "anchor": "bindings          map[string]map[string]struct{}",
          "change": "published only after success; ownership set derived from journal/op writes instead of snapshotBindings name diff"
        },
        {
          "task": "2.1",
          "file": "runtime/stats.go",
          "symbol": "Stats.activePlugins / incPlugins / decPlugins",
          "anchor": "activePlugins  atomic.Int64",
          "change": "incPlugins only after success publication; no decrement path needed if never incremented early"
        },
        {
          "task": "2.1",
          "file": "runtime/engine.go",
          "symbol": "engineImpl.RootEnv",
          "anchor": "func (e *engineImpl) RootEnv() *core.Env {",
          "change": "unchanged; root identity stays stable (rootEnvPtr set once in New)"
        },
        {
          "task": "1.4",
          "file": "runtime/plugin_view_rollback_test.go",
          "symbol": "(new test file)",
          "anchor": "",
          "change": "c1 red file; helpers prefixed vr, file-local",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "idle",
          "op-active",
          "op-settling",
          "published",
          "aborted",
          "retired-view"
        ],
        "transitions": [
          {
            "input": "Use(p) with Registry().Get(name) absent",
            "state": "idle",
            "effect": "set",
            "evidence": "enters op-active via RootEnv().BeginRegistration(); no registry write (today plugin.go:160 registers first); design.md 'Keep old registry entries, bindings, and active-plugin counts pending'"
          },
          {
            "input": "Use(p) with the name already registered",
            "state": "idle",
            "effect": "no-op",
            "evidence": "returns error wrapping 'register plugin %s' and containing 'already registered'; no registration begun; plugin_test.go:89-110 TestUse_AlreadyRegistered"
          },
          {
            "input": "BeginRegistration refused (a host holds a registration on RootEnv())",
            "state": "idle",
            "effect": "no-op",
            "evidence": "Use/ReloadPlugin return fmt.Errorf(`register plugin %s: %w`) wrapping *LispicoError Code == core.CodeRegistrationActive; registration.go:50-58"
          },
          {
            "input": "Init write through its env: Set/SetFunc/SetCanonical/SetBoth/Delete/ReplaceCell, Find owner, child-scope set!, evaluator reentry, closure over the env, MergeInto(env)",
            "state": "op-active",
            "effect": "set",
            "evidence": "journaled via viewReg routing (env.go Set*/Delete/mergeInto); spec scenario 'Registration aliases retain ownership'; runtime/eval.go:402-430 runs reentry on the passed env"
          },
          {
            "input": "host raw write through RootEnv(): add, rebind, delete, ReplaceCell, Rebuild",
            "state": "op-active",
            "effect": "forced",
            "evidence": "unattributed; survives abort (registration.go:81-113); spec scenario 'Concurrent host writes survive abort'"
          },
          {
            "input": "host first touch of an unrelated deferred name via RootEnv().Get",
            "state": "op-active",
            "effect": "forced",
            "evidence": "raw root materialization, unattributed; survives Use failure because Use no longer deletes a name diff (plugin.go:212-228 deleted it); spec scenario 'Concurrent host materialization survives abort'"
          },
          {
            "input": "Registry().Get(name) / Generation(name) read while Init runs",
            "state": "op-active",
            "effect": "no-op",
            "evidence": "Use: absent / 0; ReloadPlugin: the old plugin and its generation; design.md pending registry"
          },
          {
            "input": "Init returns an error",
            "state": "op-active",
            "effect": "clear",
            "evidence": "enters aborted: FinishEval, then reg.Abort(); returns error wrapping 'init plugin %s'; registry, e.bindings, ActivePlugins unchanged; spec requirement paragraph 1"
          },
          {
            "input": "applyVocabulary(view) returns an error",
            "state": "op-active",
            "effect": "clear",
            "evidence": "enters aborted; returns error wrapping 'apply vocabulary for plugin %s' with Code CodeResourceLimit; engine.go:458-529; spec scenario 'Vocabulary rejection rolls back once'"
          },
          {
            "input": "FinishEval returns a settlement error (retained charge denied)",
            "state": "op-settling",
            "effect": "clear",
            "evidence": "enters aborted; ReloadPlugin of a fresh name leaves ActivePlugins unchanged (defect today: plugin.go:334-336 increments before FinishEval at 338-341)"
          },
          {
            "input": "Init, vocabulary and FinishEval all succeed",
            "state": "op-settling",
            "effect": "set",
            "evidence": "enters published: registry entry via RegisterNoCheck in this chunk (PublishIf from 2.2), reg.Complete(), e.bindings[name] = diff(after, before) or delete when empty, populateTemplateBindings, stats.incPlugins (Use always; ReloadPlugin only when !hadOld); plugin.go:191-209"
          },
          {
            "input": "ReloadPlugin(p) with an old plugin",
            "state": "idle",
            "effect": "set",
            "evidence": "enters op-active; old names deleted through the view (removePluginBindings(view, name)); e.bindings[name] and the registry entry stay until publish; plugin.go:252-268"
          },
          {
            "input": "ReloadPlugin failure (interim, until 2.5)",
            "state": "op-active",
            "effect": "clear",
            "evidence": "enters aborted: reg.Abort(), then restoreRootEnv(oldRoot); registry/e.bindings untouched; no lazy deactivate (plugin.go:225-227 removed from every failure path)"
          },
          {
            "input": "call through a retained Init env or closure after Use returned",
            "state": "retired-view",
            "effect": "no-op",
            "evidence": "unattributed forward to the live root; registration.go:152-159; spec scenario 'Successful registration remains live'"
          },
          {
            "input": "host RootEnv().Evaluator() concurrent with view.SetEvaluator and the Abort restore",
            "state": "op-active",
            "effect": "forced",
            "evidence": "read under owner RLock; no data race; env.go:897-899 carried defect"
          }
        ],
        "forbidden": [
          "Registry().Get(name) returning the operation's plugin while its Init runs",
          "ActivePlugins, e.bindings[name] or lazy state.active changed by a failed Use/ReloadPlugin",
          "Init receiving e.rootEnv instead of reg.Env()",
          "a registration still active after Use/ReloadPlugin returns: RootEnv().BeginRegistration() must succeed right after return",
          "both Complete and Abort on one operation; Complete on any error path",
          "an unsynchronized read of Env.eval in Evaluator()"
        ],
        "seeding": [
          "idle: New(nil, WithDialect(cl.Dialect())) when a function cell is needed, else New(nil, WithDialect(clojure.Dialect())); pre-seed through eng.RootEnv().Set / SetFunc / SetCanonical before Use; macros through eng.Eval before Use",
          "op-active: file-local vr barrier plugin whose Init performs its writes, closes entered, blocks on <-release; Use runs in a goroutine; the test acts only after <-entered and only through RootEnv() or Registry() (eng.Eval, Bind and LoadScope block on e.mu for the whole Use: runtime/eval.go:741, 1057, 1124)",
          "op-settling with settlement failure: New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(&recordingMeter{chargeErr: errors.New(...)})), m.reset() after New, plugin adding a fresh name (setupPlugin pattern meter_test.go:606-610)",
          "vocabulary failure: dialect core.FullDialect().Vocabulary(map[string]string{`vr-visible`: `vr-canon`}) with WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 16}); Init binds GoFunc vr-canon, overwrites a pre-seeded name, then fills fresh names until env.RetainedUsage() slots == 16, so Init succeeds and the vocab Set of vr-visible is refused with CodeResourceLimit",
          "retired-view: plugin stores its Init env in a field; the test uses it after Use returns",
          "evaluator race: Init calls env.SetEvaluator(wrapper around env.Evaluator()), closes evalSet, returns an error; a host goroutine loops RootEnv().Evaluator() from <-evalSet until Use returns (at most 10000 iterations)"
        ],
        "budgets": [
          "each Use/ReloadPlugin: exactly 1 BeginRegistration and exactly 1 of Complete/Abort",
          "Evaluator(): +1 RLock/RUnlock of the owner mutex, 0 allocs; not on the VM or tree-walker per-call path (callers: core/depth.go:42-47, core/value_walk_context.go:399, runtime/lazy_template.go:463/503, plugins/stdlib/bootstrap.go:42-45, plugins/json 1 site)",
          "Get/Cell/NameGen lookup paths: +0 instructions in this chunk",
          "Use success: +2 allocs (Registration, view) per call, off the eval path",
          "every barrier test: 2s watchdog (select with time.After used only as deadlock detection, never for ordering)"
        ]
      },
      "redTasks": [
        "[1.1] TestUseFailedInitRestoresOverwrittenBindings, cl.Dialect(). Pre-seed vr-val=Int 1 (Set), vr-fn GoFunc returning 1 (SetFunc), vr-canon GoFunc (SetCanonical); fn, _ := eng.Func(`vr-fn`); gen0 := RootEnv().NameGen(); root0 := RootEnv(). Plugin overwrites all three (non-canonical Set for vr-canon), adds vr-new-val (Set) and vr-new-fn (SetFunc), then returns an error. Assert: values equal the originals, RootEnv().GetCanonical(`vr-canon`) canonical true, the vr-new-* Get/GetFunc report not found, Registry().Get false, Generation == 0, Stats().ActivePlugins == 0, fn.Call(ctx) returns Int 1, NameGen() >= gen0, RootEnv() == root0. Red at baseline (overwrites survive). Spec: scenario 'Failed initial load restores overwritten bindings' plus requirement paragraph 1 (handles, canonical, registry, count, root identity, generations)",
        "[1.1] TestUseFailedInitRestoresMacro, clojure. eng.Eval `(defmacro vr-m [x] x)`; the plugin runs env.Evaluator().Eval(ctx, (defmacro vr-m [x] 99), env) and fails; after that, eng.Eval `(vr-m 1)` returns Int 1. Red at baseline. Spec: scenario 'Failed initial load restores overwritten bindings' (macro), requirement 'Cache invalidation'",
        "[1.1] TestUseRegistryPendingDuringInit (-race). Barrier plugin; at <-entered, Registry().Get(name) false and Generation(name) == 0; release, Init fails; still absent, ActivePlugins 0. Red at baseline (registered first)",
        "[1.1] TestReloadPluginRegistryKeepsOldDuringInit (-race). bindingPlugin v1.0.0 Use; ReloadPlugin(barrier v2.0.0); at <-entered, Registry().Get(name) returns version 1.0.0; release, Init fails; still 1.0.0, ActivePlugins 1. Red at baseline",
        "[1.3] TestReloadPluginSettlementFailureKeepsActiveCount. Meter chargeErr; ReloadPlugin(fresh plugin adding a name) returns Code CodeResourceLimit; Stats().ActivePlugins == 0, Registry().Get false, the added name absent. Red at baseline (count 1)",
        "[1.3] TestUseFailedVocabularyRestoresOwnedBindings. Vocabulary seeding above; error contains 'apply vocabulary for plugin' with Code CodeResourceLimit; the pre-seeded overwritten name restored, vr-canon and the fillers absent, ActivePlugins unchanged, Registry().Get false; a follow-up Use of a plugin writing nothing succeeds. Red at baseline (overwrite survives). Spec: scenario 'Vocabulary rejection rolls back once'. Setup asserts New() boot retained usage stays below MaxRetainedSlotsPerEnv so the vocabulary rejection is the only failure; Abort does not release retained slots until plugin-retained-rollback, so the follow-up Use writes nothing",
        "[1.2] TestUseFailedInitKeepsConcurrentHostWrites (-race). Pre-seed a=1, h-rebind=1, h-del=1, h-repl=1. Plugin writes a=2, closes entered, waits release, writes a=3, returns an error. At <-entered the host does RootEnv().Set(`h-add`, 1), Set(`h-rebind`, 9), Delete(`h-del`), ReplaceCell(`h-repl`, 5), Set(`a`, 7), Rebuild(), then release. Assert h-add 1, h-rebind 9, h-del absent, h-repl 5, a 7 (op-host-op rebases to the host state), plugin-only names absent. Red at baseline (diff deletes h-add). Spec: scenario 'Concurrent host writes survive abort'",
        "[1.2] TestUseFailedInitKeepsHostLazyMaterialization (-race). clojure engine plus stdlib.New() (cold); barrier plugin; at <-entered the host calls RootEnv().Get(`str`) (true); release, Init fails; `(str 1)` evaluates, installedNames(impl) contains str, MaterializeCount() == before + 1. Red at baseline (the diff deletes the str cell and the installed path then misses). Spec: scenario 'Concurrent host materialization survives abort'",
        "[1.4] TestUseFailedInitRevertsAliasWrites (-race), clojure, pre-seed x=1, subtests find/child/evaluator/merge/closure. find: owner, _ := env.Find(`x`); owner.Set(`x`, 2). child: env.Evaluator().Eval(ctx, (set! x 2), env.Child()). evaluator: Eval (def x 2) on env. merge: src := core.NewEnv(nil); src.Set(`x`, 2); src.MergeInto(env). closure: Eval (defn vr-bump [] (set! x 2)) on env, then (vr-bump). Each plugin fails; after that x == 1 and vr-bump is absent. Red at baseline. Spec: scenario 'Registration aliases retain ownership'",
        "[1.4] TestUseEvaluatorReadDuringFailedInitIsRaceFree (-race only). Evaluator-race seeding; after Use returns, RootEnv().Evaluator() is the original evaluator (Abort restored it). Red only under -race at baseline (unlocked read vs locked write)",
        "[1.4] TestUseSuppliedEnvStaysLiveAfterSuccess. Guard, green at baseline. Plugin retains its env and evaluates (defn vr-get [] vr-x) on it; after Use succeeds: RootEnv().Set(`vr-x`, 5); retained.Get(`vr-x`) is 5; `(vr-get)` is 5; retained.Set(`vr-late`, 1) lands in root and survives a later failing Use of another plugin; UnloadPlugin removes the plugin-added vr-get. Spec: scenario 'Successful registration remains live'"
      ],
      "codeTasks": [
        "2.1 runtime/plugin.go Use: duplicate check: `if _, ok := e.registry.Get(name); ok` returns fmt.Errorf(`register plugin %s: %w`) around the core `plugin %q already registered` text (TestUse_AlreadyRegistered stays green); no Generation read in this chunk; reg, err := e.rootEnv.BeginRegistration() (err wrapped the same); view := reg.Env(); before := snapshotBindings(); StartEval; initPlugin(p, view, name, version); applyVocabulary(view); after-diff kept local; FinishEval; success: e.registry.RegisterNoCheck(p), reg.Complete(), e.bindings[name] = added, populateTemplateBindings, incPlugins; any error: FinishEval if pending, then reg.Abort(); no Unregister, no deactivate, no diff deletion",
        "2.1 runtime/plugin.go ReloadPlugin: Begin (no Generation read in this chunk); removePluginBindings(view, name) without deleting e.bindings[name]; same pipeline; success replaces e.bindings[name], RegisterNoCheck(p), Complete, populate, incPlugins only when !hadOld (moved after FinishEval); failure: reg.Abort() then restoreRootEnv(oldRoot) (interim)",
        "2.1 runtime/plugin.go initPlugin(p, env, name, version): pass env on both branches, including the ensureLayer build callback (plugin.go:90-96); loadingPlugin/loadingVersion/eager bookkeeping stays until 2.3",
        "2.1 runtime/plugin.go removePluginBindings(env *core.Env, name string): delete through env; e.bindings deletion moves to callers (UnloadPlugin passes e.rootEnv and still deletes e.bindings[name]: last-writer semantics unchanged)",
        "2.1 runtime/engine.go applyVocabulary(env *core.Env): every e.rootEnv use (engine.go:465-526) goes through env",
        "2.1 core/env.go Evaluator(): o := e.owner(); o.mu.RLock(); defer o.mu.RUnlock(); return o.eval. Caller audit: no caller holds any env mutex (grep before landing); the known in-core caller core/depth.go runs under no env lock — confirm it and every other caller before landing (RWMutex is not reentrant); the residual unlocked eval copy in NewEnv stays documented-only",
        "2.1 runtime/plugin.go rollbackPluginUse: no longer called from Use; ReloadPlugin keeps only the restoreRootEnv replay until 2.5"
      ],
      "redTests": [
        "TestUseFailedInitRestoresOverwrittenBindings",
        "TestUseFailedInitRestoresMacro",
        "TestUseRegistryPendingDuringInit",
        "TestReloadPluginRegistryKeepsOldDuringInit",
        "TestReloadPluginSettlementFailureKeepsActiveCount",
        "TestUseFailedVocabularyRestoresOwnedBindings",
        "TestUseFailedInitKeepsConcurrentHostWrites",
        "TestUseFailedInitKeepsHostLazyMaterialization",
        "TestUseFailedInitRevertsAliasWrites",
        "TestUseEvaluatorReadDuringFailedInitIsRaceFree",
        "TestUseSuppliedEnvStaysLiveAfterSuccess"
      ],
      "redRun": "go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUseFailedInitRestoresOverwrittenBindings|TestUseFailedInitRestoresMacro|TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestReloadPluginSettlementFailureKeepsActiveCount|TestUseFailedVocabularyRestoresOwnedBindings|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseSuppliedEnvStaysLiveAfterSuccess)$'",
      "verify": "go build ./core/... ./runtime/... && go vet ./runtime ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm -skip '^(TestUsePublishIfRejectsStaleGeneration|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval|TestReloadPluginFailedColdStdlibKeepsDeferredNames|TestLazyViewMaterializationRevertsWithFailedUse|TestLazyViewDeleteTombstoneRevertsWithFailedUse|TestLazyRegisterValueWithoutOperationBindsImmediately|TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess|TestReloadPluginFailedInitRestoresCanonicalBinding|TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree)$' && golangci-lint run ./runtime/... ./core/...",
      "coder": "go-coder",
      "redAfter": "c0",
      "race": "channel-barrier host writes, registry edits and Evaluator reads racing an initializing plugin and its Abort"
    },
    {
      "id": "c2",
      "taskIds": [
        "1.3",
        "2.2"
      ],
      "prev": "c1",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "registry-publication",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.3",
          "file": "runtime/plugin_test.go",
          "symbol": "TestUnloadPlugin_Success",
          "anchor": "func TestUnloadPlugin_Success(t *testing.T) {",
          "change": "add: vocabulary rejection (dialect vocab Set error) rolls back once + counts correct; successful retry after failure; direct host Registry().Unregister/Register during Init → conflict error, host entry preserved; successful unload last-writer pin"
        },
        {
          "task": "1.3",
          "file": "runtime/meter_test.go",
          "symbol": "TestMeter_UseRollsBackPluginOnRetainedChargeError",
          "anchor": "func TestMeter_UseRollsBackPluginOnRetainedChargeError(t *testing.T) {",
          "change": "extend settlement-error (FinishEval) failure: overwritten pre-existing binding restored, registry/ActivePlugins unchanged; ReloadPlugin variant"
        },
        {
          "task": "2.2",
          "file": "core/plugin.go",
          "symbol": "Registry",
          "anchor": "plugins map[string]Plugin",
          "change": "no generation/identity today (only mu + plugins map); add per-entry generation or identity observation + conditional publish (compare-and-register / compare-and-replace) returning a conflict error"
        },
        {
          "task": "2.2",
          "file": "core/plugin.go",
          "symbol": "Registry.Register / RegisterNoCheck / Unregister / Get",
          "anchor": "func (r *Registry) RegisterNoCheck(p Plugin) {",
          "change": "RegisterNoCheck (only used by ReloadPlugin restore) likely replaced by the conditional seam; Register/Unregister bump generation so host edits are detectable"
        },
        {
          "task": "2.2",
          "file": "core/plugin_test.go",
          "symbol": "TestRegistry_Unregister",
          "anchor": "func TestRegistry_Unregister(t *testing.T) {",
          "change": "add unit tests for the conditional publication seam (match publishes; host edit since observation → conflict, host entry kept) using newStub"
        },
        {
          "task": "2.2",
          "file": "core/error.go",
          "symbol": "NewRegistrationActiveError",
          "anchor": "const CodeRegistrationActive = \"RegistrationActiveError\"",
          "change": "pattern site if the conflict gets a typed *LispicoError code (Code const + NewXError constructor); else fmt error in core/plugin.go style `plugin %q already registered`"
        },
        {
          "task": "1.3",
          "file": "runtime/plugin_registry_publish_test.go",
          "symbol": "(new test file)",
          "anchor": "",
          "change": "c2 red file; helpers prefixed rp, file-local",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "absent",
          "registered-gen-g",
          "op-observed-g",
          "host-edited",
          "published-gen-g2",
          "conflict"
        ],
        "transitions": [
          {
            "input": "Register(p) / RegisterNoCheck(p) / PublishIf that installs",
            "state": "absent|registered-gen-g",
            "effect": "set",
            "evidence": "entry gen = ++seq (strictly increasing, never reused)"
          },
          {
            "input": "Unregister(name)",
            "state": "registered-gen-g",
            "effect": "clear",
            "evidence": "entry removed; Generation(name) == 0; core/plugin.go:86-91"
          },
          {
            "input": "Register(p) on a present name",
            "state": "registered-gen-g",
            "effect": "no-op",
            "evidence": "existing error 'plugin %q already registered'; generation unchanged; core/plugin.go:45-56"
          },
          {
            "input": "PublishIf(p, gen) where Generation(p.Name()) == gen",
            "state": "op-observed-g",
            "effect": "set",
            "evidence": "installs p, new generation > gen; returns nil; design.md 'conditional publication seam'"
          },
          {
            "input": "PublishIf(p, gen) where Generation(p.Name()) != gen",
            "state": "host-edited",
            "effect": "no-op",
            "evidence": "returns NewRegistryConflictError(p.Name()); entry and generation untouched; design.md 'preserve the host entry and abort with a conflict error'"
          },
          {
            "input": "Use/ReloadPlugin publish returns the conflict",
            "state": "conflict",
            "effect": "clear",
            "evidence": "runtime returns fmt.Errorf(`publish plugin %s: %w`) and runs the failure path (reg.Abort()); ActivePlugins and e.bindings unchanged; tasks.md 2.2"
          },
          {
            "input": "host RegisterNoCheck(hostP) during Use's Init",
            "state": "op-observed-g",
            "effect": "forced",
            "evidence": "host wins: Registry().Get(name) == hostP after Use returns"
          },
          {
            "input": "host Unregister(name) during ReloadPlugin's Init",
            "state": "op-observed-g",
            "effect": "forced",
            "evidence": "host removal wins: registry absent after return; the old plugin's bindings are restored by abort"
          },
          {
            "input": "Use retried after the host frees the name",
            "state": "absent",
            "effect": "set",
            "evidence": "spec requirement: failed operations leave no residue; tasks.md 1.3 successful retry"
          }
        ],
        "forbidden": [
          "PublishIf overwriting an entry whose generation differs from the observed one",
          "a generation value reused after Unregister",
          "Registry.mu held across any env, lazy or engine lock",
          "comparing Plugin interface values for identity (non-comparable dynamic types panic); generations only",
          "any runtime publish path other than PublishIf after this chunk (RegisterNoCheck stays public API; runtime stops calling it)"
        ],
        "seeding": [
          "absent/registered: core.NewRegistry() plus Register/Unregister directly (runtime test file, package runtime)",
          "op-observed with a host edit: file-local rp barrier plugin (writes pre-seeded x=2 and new rp-y, closes entered, blocks on release, returns nil); after <-entered the host calls eng.Registry().RegisterNoCheck(rpHost{name}) or Unregister(name), then release",
          "retry: after the conflict, eng.Registry().Unregister(name), then Use(a fresh non-blocking instance)"
        ],
        "budgets": [
          "Generation and PublishIf: O(1) under Registry.mu, 0 allocs besides the map entry",
          "exactly 1 PublishIf per successful-path Use/ReloadPlugin; 0 on failure paths",
          "registry entry grows by one uint64; seq is uint64, so no wrap is reachable"
        ]
      },
      "redTasks": [
        "[1.3] TestUsePublishIfRejectsStaleGeneration. r := core.NewRegistry(); Generation(`p`) == 0; PublishIf(p1, 0) nil and g1 := Generation > 0; PublishIf(p2, 0) returns *core.LispicoError Code core.CodeRegistryConflict whose message contains `p`, and Get still returns p1 with Generation == g1; PublishIf(p2, g1) nil, Get returns p2, Generation > g1; Unregister gives 0; Register(p3) gives a Generation greater than every earlier one; RegisterNoCheck bumps it. Red at baseline (inert Generation 0, inert PublishIf nil)",
        "[1.3] TestUseRegistryConflictKeepsHostEntry (-race), clojure, pre-seed x=1. The host RegisterNoCheck(rpHost) runs during Init; Use returns an error that errors.As to *core.LispicoError with Code == core.CodeRegistryConflict; Registry().Get(name) returns the same rpHost pointer, Generation equals the value read right after the host write, x == 1, rp-y absent, ActivePlugins unchanged. Then Unregister and Use(fresh) succeed with ActivePlugins +1 and rp-y live. Red at baseline and after c1 (RegisterNoCheck overwrites the host). Spec: requirement 'plugin registry/ownership ... remain as before'; tasks 1.3 host registry conflict and successful retry",
        "[1.3] TestReloadPluginRegistryConflictKeepsHostRemoval (-race). Use v1 (bindingPlugin adding rp-old); ReloadPlugin(rp barrier v2, which adds rp-new); the host Unregister(name) runs during Init; the error has Code CodeRegistryConflict; Registry().Get false; rp-old resolves, rp-new absent; ActivePlugins still 1. Red at baseline (reload succeeds)"
      ],
      "codeTasks": [
        "2.2 core/plugin.go: Registry{mu; plugins map[string]registryEntry; seq uint64} with type registryEntry struct{ p Plugin; gen uint64 }; Register/RegisterNoCheck assign gen = ++seq; Get/Namespaces/HasPrefix read entry.p; Generation returns the entry gen or 0; PublishIf(p, gen) compares and installs under mu, else returns NewRegistryConflictError(p.Name())",
        "2.2 runtime/plugin.go: Use reads gen0 := e.registry.Generation(name) first, replacing c1's Get check: gen0 != 0 refuses with the same `register plugin %s: %w` wrapping around the `already registered` text; then BeginRegistration and ReloadPlugin reads gen := Generation(name) before Get; Use publishes with e.registry.PublishIf(p, gen0) and ReloadPlugin with PublishIf(p, gen), after FinishEval and before reg.Complete(); an error is wrapped `publish plugin %s: %w` and takes the failure path",
        "2.2 core/plugin_test.go existing TestRegistry_* stay green unchanged"
      ],
      "redTests": [
        "TestUsePublishIfRejectsStaleGeneration",
        "TestUseRegistryConflictKeepsHostEntry",
        "TestReloadPluginRegistryConflictKeepsHostRemoval"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUsePublishIfRejectsStaleGeneration|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval)$'",
      "verify": "go build ./core/... ./runtime/... && go vet ./runtime ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm -skip '^(TestReloadPluginFailedColdStdlibKeepsDeferredNames|TestLazyViewMaterializationRevertsWithFailedUse|TestLazyViewDeleteTombstoneRevertsWithFailedUse|TestLazyRegisterValueWithoutOperationBindsImmediately|TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess|TestReloadPluginFailedInitRestoresCanonicalBinding|TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval)$' && golangci-lint run ./runtime/... ./core/...",
      "coder": "go-coder",
      "redAfter": "c0",
      "race": "channel-barrier host writes, registry edits and Evaluator reads racing an initializing plugin and its Abort"
    },
    {
      "id": "c3",
      "taskIds": [
        "1.1",
        "2.3"
      ],
      "prev": "c2",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "lazy-attribution",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "runtime/plugin_test.go",
          "symbol": "failingBindingPlugin",
          "anchor": "type failingBindingPlugin struct {",
          "change": "extend/reuse: failing Init that overwrites pre-seeded value/function/canonical bindings and adds names; assert old values, canonical status, Fn/PinnedFn handles, registry, Stats().ActivePlugins"
        },
        {
          "task": "1.1",
          "file": "runtime/plugin_test.go",
          "symbol": "TestReloadPlugin_InitFailure_RestoresOldBindings",
          "anchor": "func TestReloadPlugin_InitFailure_RestoresOldBindings(t *testing.T) {",
          "change": "sibling regressions next to it: failing Use overwriting an existing value (review repro #2)"
        },
        {
          "task": "1.1",
          "file": "runtime/lazy_materialize_test.go",
          "symbol": "sharedTemplatePlugin",
          "anchor": "type sharedTemplatePlugin struct {",
          "change": "reuse (Name()==\"\", fail flag) for failed cold stdlib reload with a different version: original stays registered, (+ 1 2)/function/macro names still resolve deferred (review repro #1); partly materialized + shadowed + deleted variant; sibling engine untouched"
        },
        {
          "task": "1.1",
          "file": "runtime/lazy_materialize_test.go",
          "symbol": "installedNames",
          "anchor": "func installedNames(impl *engineImpl) []string {",
          "change": "reuse to assert installed set; add sibling helpers for tombstoned/active snapshot under state.mu"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.RegisterValue",
          "anchor": "func (m *stdlibLazyMaterializer) RegisterValue(",
          "change": "derive op identity from env (view → registration) instead of `m.engine.loadingPlugin` (read at `if m.eager || m.engine.loadingPlugin != \"\" {` twice + key build); eager path writes go through env (view) and are journaled"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.RegisterSource",
          "anchor": "func (m *stdlibLazyMaterializer) RegisterSource(",
          "change": "same identity threading as RegisterValue"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.LookupAndMaterialize / materializeOne",
          "anchor": "func (m *stdlibLazyMaterializer) LookupAndMaterialize(env *core.Env, name string, funcNS bool) (core.Value, bool, bool) {",
          "change": "receives view-originated lookups to attribute installs; today env is always the root (see core/env.go sites)"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "Env.CellLocal / Get lazy fallback",
          "anchor": "layer.LookupAndMaterialize(o, name, false)",
          "change": "carried defect CONFIRMED: lookups pass owner root `o`; view.lazy() is nil (core/registration.go `lazy reads e's own lazy layer; a view has none`) so Get falls to parent root (`if val, ok, _ := layer.LookupAndMaterialize(e, name, false); ok {`); pass the originating view/registration to the layer"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "Env.deleteName → LazyLayer.TombstoneForDelete",
          "anchor": "layer.TombstoneForDelete(e, name)",
          "change": "carried defect CONFIRMED: called with root e after view forwarding (`r.root.deleteName(r, name)`), outside root.mu; Abort has no lazy-state hook → op-tagged tombstone must be revertible"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.TombstoneForDelete",
          "anchor": "func (m *stdlibLazyMaterializer) TombstoneForDelete(env *core.Env, name string) {",
          "change": "record op-owned tombstone + removed installed entry with before-image; restore only still-owned entries on abort"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyEngineState.active",
          "anchor": "active       map[string]string // pluginName -> pluginVersion",
          "change": "op-tagged active-version change (activate/deactivate), never whole-map snapshot restore; activeList rebuilt after undo"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyEngineState.installed",
          "anchor": "installed    map[string]struct{}",
          "change": "op-tagged install entries (recordInstall) + materialized count; rebase before-images on host materialization"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyEngineState.tombstoned",
          "anchor": "tombstoned   map[string]struct{}",
          "change": "op-tagged tombstone add/remove (TombstoneForDelete, activate clears)"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.activate / deactivate",
          "anchor": "func (m *stdlibLazyMaterializer) activate(pluginName, pluginVersion string, names map[string]struct{}) {",
          "change": "activate deletes tombstones for all names; reload deactivation/activation must be attributable and revertible per entry"
        },
        {
          "task": "2.3",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.recordInstall",
          "anchor": "func (m *stdlibLazyMaterializer) recordInstall(pluginName, name string) {",
          "change": "carry op identity; count materialized per op for undo"
        },
        {
          "task": "2.3",
          "file": "runtime/engine.go",
          "symbol": "engineImpl.loadingPlugin",
          "anchor": "loadingPlugin    string // plugin whose Init",
          "change": "demote/remove as attribution source; loadingVersion (`loadingVersion string` in lazy_template.go) likewise"
        },
        {
          "task": "1.1",
          "file": "runtime/lazy_rollback_test.go",
          "symbol": "(new test file)",
          "anchor": "",
          "change": "c3 red file; helpers prefixed lr, file-local",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "no-op-open",
          "op-open",
          "entry-owned",
          "entry-foreign",
          "op-committed",
          "op-undone"
        ],
        "transitions": [
          {
            "input": "Begin of a plugin operation: m.beginOp(view, name, version, eager)",
            "state": "no-op-open",
            "effect": "set",
            "evidence": "state.op = &lazyOp{view, name, version, eager} under state.mu; replaces initPlugin's loadingPlugin/loadingVersion/eager writes (plugin.go:74-88)"
          },
          {
            "input": "RegisterValue(env == op.view) with op.name == `` and !op.eager",
            "state": "op-open",
            "effect": "set",
            "evidence": "deferred into key{dialectFP, op.name, op.version} (lazy_template.go:548-592)"
          },
          {
            "input": "RegisterValue/RegisterSource with any other env, or no open op",
            "state": "no-op-open|op-open",
            "effect": "forced",
            "evidence": "binds immediately on env (SetCanonical/Set) / returns false; fixes a host RegisterValue outside Use being deferred into an inactive key{fp, ``, ``} today (lazy_template.go:552-561)"
          },
          {
            "input": "lookup miss through the view: Get/GetCanonical/GetFunc/GetFuncCanonical/CellLocal/FuncCellLocal/Find/VarNames/FuncNames",
            "state": "op-open",
            "effect": "set",
            "evidence": "the layer receives the view and materializes through it (journaled in core); entry records the before-image (installed=false) and the count delta; core-engine spec.md:1242"
          },
          {
            "input": "lookup miss through the root or any non-current view (host, retired view)",
            "state": "no-op-open|op-open",
            "effect": "forced",
            "evidence": "materializes through m.engine.rootEnv (raw, unattributed); an existing owned entry for that name becomes foreign; spec scenario 'Concurrent host materialization survives abort'"
          },
          {
            "input": "Delete through the view",
            "state": "op-open",
            "effect": "set",
            "evidence": "Delete calls layer.TombstoneForDelete(view, name) after forwarding; entry records the before-image (tombstoned, installed); env.go:924-956"
          },
          {
            "input": "Delete through the root (host)",
            "state": "op-open",
            "effect": "forced",
            "evidence": "tombstone applied; an existing entry for that name becomes foreign"
          },
          {
            "input": "op write to a foreign entry",
            "state": "entry-foreign",
            "effect": "set",
            "evidence": "rebases the before-image to the current membership, clears foreign (core rule registration.go:173-190)"
          },
          {
            "input": "operation fails",
            "state": "op-open",
            "effect": "clear",
            "evidence": "m.endOp(false) under state.mu restores each owned entry's tombstoned/installed membership, subtracts owned installs from materialized, sets state.op = nil; then reg.Abort()"
          },
          {
            "input": "operation succeeds",
            "state": "op-open",
            "effect": "clear",
            "evidence": "m.endOp(true): the journal is dropped and op installs stay; then e.bindings and activate as in 2.1"
          },
          {
            "input": "sibling engine sharing the template",
            "state": "no-op-open|op-open",
            "effect": "no-op",
            "evidence": "per-engine state only; published layers never written (lazy_template.go:180-194)"
          }
        ],
        "forbidden": [
          "restoring state.active, state.installed or state.tombstoned from a whole-map snapshot",
          "a view lookup materializing through the view after its op closed or ended (journal and lazy attribution must agree)",
          "state.mu held while calling any env method or while root.mu is held (no nesting in either order)",
          "reading engine.loadingPlugin, m.loadingVersion or m.eager (fields removed)",
          "installed[name] true for a name whose root cell reverted to tombstoned after the op finished",
          "writes to a published stdlibTemplateLayer"
        ],
        "seeding": [
          "cold stdlib: New(nil, WithBytecode(), WithDialect(clojure.Dialect())) plus Use(stdlib.New()); MaterializeCount() == 0",
          "failing reload of stdlib: file-local lrFailingStdlib{} with Name() ``, Metadata().Version `lr-2.0.0`, and an Init that returns lrErr before any registration",
          "op-open: file-local lr barrier plugin (non-empty name) whose Init touches env (Get/Evaluator().Eval/Delete) and returns an error or blocks on channels",
          "host tombstone during op: at <-entered, RootEnv().Delete(`sort`), then release; the plugin deletes `sort` after release (host-op order); the host also deletes `concat` after the plugin's pre-barrier env.Delete(`concat`) (op-host order)",
          "lazy state reads: installedNames(impl) (existing lazy_materialize_test.go:498); a file-local lrTombstoned(impl, name) reading state.tombstoned under state.mu"
        ],
        "budgets": [
          "LookupAndMaterialize miss path: 0 extra mutex acquisitions (the op check runs in the existing state.mu section), +1 pointer compare",
          "non-view env lookups: core +0 instructions (the env handed to the layer is e, which equals owner() off a view)",
          "Env stays 208 bytes (TestEnv_Size); view.lazyLayer is set once at BeginRegistration",
          "lazy journal: at most 1 entry per distinct name the op touched; endOp O(entries)",
          "a true miss through a view pays at most 2 template consults (view layer, then root walk)"
        ]
      },
      "redTasks": [
        "[1.1] TestReloadPluginFailedColdStdlibKeepsDeferredNames. Cold stdlib; ReloadPlugin(lrFailingStdlib) returns an error containing 'init plugin'; Registry().Get(``) Metadata().Version == 1.0.0; MaterializeCount() == 0 right after; then `(+ 1 2)` is 3, `(str 1 2)` evaluates, `(-> 1 (+ 2))` is 3; ActivePlugins unchanged. Red at baseline and through c2 (root tombstones never reverted). Spec: scenario 'Failed cold stdlib reload keeps deferred names'; requirement 'without forcing eager materialization'",
        "[1.1] TestLazyViewMaterializationRevertsWithFailedUse. Cold stdlib, before := MaterializeCount(); the lr plugin runs env.Get(`str`) and env.Evaluator().Eval(ctx, (reverse [1 2]), env), then fails; installedNames lacks str and reverse, MaterializeCount() == before; then `(str 1)` evaluates and the count is before + 1. Red at baseline and through c2. Spec: 'Effects reached through that supplied environment SHALL remain attributable'",
        "[1.1] TestLazyViewDeleteTombstoneRevertsWithFailedUse. Cold stdlib; eng.Eval `(+ 1 2)` first (materializes +); the plugin runs env.Delete(`nth`) (deferred) and env.Delete(`+`), then fails; lrTombstoned is false for both; `(nth [10 20] 1)` is 20 and `(+ 1 2)` is 3. Red through c2. Spec: requirement 'deferred attachments, and deletion state SHALL remain as before'",
        "[1.1] TestLazyRegisterValueWithoutOperationBindsImmediately. Cold stdlib; with no operation, eng.RootEnv().RegisterValue(`lr-host`, GoFunc, false) returns nil and RootEnv().Get(`lr-host`) is found immediately. Red at baseline (deferred into an inactive key)",
        "[1.2] TestLazyHostTombstoneDuringFailedUseSurvives (-race). Guard, green at baseline, pins the foreign rule. Host-op order on sort and op-host order on concat as seeded; after the failure both stay undefined and lrTombstoned is true for both"
      ],
      "codeTasks": [
        "2.3 core/registration.go BeginRegistration: view.lazyLayer.Store(root.lazyLayer.Load()); lazy() doc updated (a view carries its root's layer)",
        "2.3 core/env.go setLazyLayer: while a registration is active also store the new pointer into cur.view.lazyLayer; Abort's lazy restore likewise stores into r.view before endLocked",
        "2.3 core/env.go CellLocal/FuncCellLocal: layer.LookupAndMaterialize(e, ...) instead of o, with the re-read through o; Find view branch: when !r.root.HasLive(name) consult e.lazy() with e before delegating to r.root.Find; VarNames/FuncNames: layer.ForceAll(e)",
        "2.3 core/env.go Delete: deleteName stops calling the layer; Delete calls LazyLayer().TombstoneForDelete(e, name) after forwarding (e is the view or the root)",
        "2.3 runtime/lazy_template.go: declare type lazyOp struct { view *core.Env; closed bool; name, version string; eager bool; entries map[string]*lazyOpEntry; installs int64 } and field op *lazyOp on stdlibLazyEngineState (guarded by state.mu, nil outside a plugin operation; c4 red tests read state.op and op.closed); entries map[string]*lazyOpEntry (tomb, installed, foreign bool) and installs int64; add m.beginOp, m.opFor(env) (under state.mu: the op when state.op != nil && !state.op.closed && env == state.op.view), m.endOp(commit bool); LookupAndMaterialize/materializeOne/installValue/publishBootstrap/recordInstall/TombstoneForDelete/RegisterValue/RegisterSource/ForceAll take attribution from opFor; the unattributed materialization target is m.engine.rootEnv; the eager check reads state.op.eager under state.mu",
        "2.3 runtime/engine.go and plugin.go: remove loadingPlugin, loadingVersion, m.eager; initPlugin calls beginOp; failure paths call m.endOp(false) before reg.Abort(); success calls m.endOp(true) after reg.Complete()",
        "2.3 core/env.go view Get/GetFunc/GetFuncCanonical: consult the lazy layer only when the owner has no live binding (HasLive/HasLiveFunc on the root), the same guard as Find — a root-live or host-shadowed name must not reach LookupAndMaterialize (no MaterializeCount bump, no installed mark)"
      ],
      "redTests": [
        "TestReloadPluginFailedColdStdlibKeepsDeferredNames",
        "TestLazyViewMaterializationRevertsWithFailedUse",
        "TestLazyViewDeleteTombstoneRevertsWithFailedUse",
        "TestLazyRegisterValueWithoutOperationBindsImmediately",
        "TestLazyHostTombstoneDuringFailedUseSurvives"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestReloadPluginFailedColdStdlibKeepsDeferredNames|TestLazyViewMaterializationRevertsWithFailedUse|TestLazyViewDeleteTombstoneRevertsWithFailedUse|TestLazyRegisterValueWithoutOperationBindsImmediately|TestLazyHostTombstoneDuringFailedUseSurvives)$'",
      "verify": "go build ./core/... ./runtime/... && go vet ./runtime ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm -skip '^(TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess|TestReloadPluginFailedInitRestoresCanonicalBinding|TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval|TestLazyHostTombstoneDuringFailedUseSurvives)$' && golangci-lint run ./runtime/... ./core/...",
      "coder": "go-coder",
      "race": "channel-barrier host writes, registry edits and Evaluator reads racing an initializing plugin and its Abort; host tombstone racing view deletion",
      "redAfter": "c0"
    },
    {
      "id": "c4",
      "taskIds": [
        "1.2",
        "2.4"
      ],
      "prev": "c3",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "materialization-fence",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "runtime/plugin_test.go",
          "symbol": "bindingPlugin",
          "anchor": "type bindingPlugin struct {",
          "change": "new channel-barrier plugin beside it: Init writes A, signals, waits for host add/rebind/delete/replace+Rebuild via RootEnv(), writes A again, then fails; assert host state survives"
        },
        {
          "task": "1.2",
          "file": "runtime/lazy_materialize_test.go",
          "symbol": "TestLazyMaterialize_ConcurrentFirstTouch",
          "anchor": "func TestLazyMaterialize_ConcurrentFirstTouch(t *testing.T) {",
          "change": "add barrier case: host first-touch of an unrelated deferred stdlib name while another plugin's Init is parked, then fails; materialized binding + installed entry survive"
        },
        {
          "task": "2.4",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.materializeOne",
          "anchor": "func (m *stdlibLazyMaterializer) materializeOne(",
          "change": "track in-flight view-originated materializations (per-name nameMu, state.mu); completion closes registration and waits for them without env/lazy locks"
        },
        {
          "task": "2.4",
          "file": "runtime/lazy_template.go",
          "symbol": "stdlibLazyMaterializer.materializeBootstrap",
          "anchor": "func (m *stdlibLazyMaterializer) materializeBootstrap(",
          "change": "bootstrap path reenters evaluator (env.Evaluator()/DefineBootstrap with env) — must stay inside fence and not deadlock"
        },
        {
          "task": "2.4",
          "file": "core/registration.go",
          "symbol": "Registration.Complete / Abort / finish",
          "anchor": "func (r *Registration) Abort() {",
          "change": "fence hook point: settlement must wait on in-flight view materialization before Abort/Complete retire the journal; late view writes stay unattributed (Env.active check)"
        },
        {
          "task": "1.2",
          "file": "runtime/lazy_fence_test.go",
          "symbol": "(new test file)",
          "anchor": "",
          "change": "c4 red file; helpers prefixed lf, file-local",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "op-open",
          "op-closed-waiting",
          "op-quiescent"
        ],
        "transitions": [
          {
            "input": "attributed lookup through the view starts materializing",
            "state": "op-open",
            "effect": "set",
            "evidence": "inflight +1 under state.mu (WaitGroup Add never concurrent with Wait: Add only while !closed)"
          },
          {
            "input": "Init returned (success or error)",
            "state": "op-open",
            "effect": "set",
            "evidence": "enters op-closed-waiting: closed = true, Wait; design.md 'close registration, wait for those operations without holding env/lazy locks, then settle'"
          },
          {
            "input": "view lookup arriving while op-closed-waiting",
            "state": "op-closed-waiting",
            "effect": "forced",
            "evidence": "host path through m.engine.rootEnv; survives abort; design.md 'A late use of the completed view is an ordinary host operation'"
          },
          {
            "input": "host lookup of an unrelated deferred name while op-closed-waiting",
            "state": "op-closed-waiting",
            "effect": "no-op",
            "evidence": "completes without waiting on the fence; tasks.md 2.4 'without blocking unrelated host lookup'"
          },
          {
            "input": "last in-flight materialization finishes",
            "state": "op-closed-waiting",
            "effect": "clear",
            "evidence": "enters op-quiescent; FinishEval, then commit or undo per the lifecycle seam"
          },
          {
            "input": "in-flight materialization finishing before a failure",
            "state": "op-closed-waiting",
            "effect": "clear",
            "evidence": "its writes are still journaled (registration active) and undone by endOp(false) plus reg.Abort()"
          },
          {
            "input": "in-flight materialization finishing before a success",
            "state": "op-closed-waiting",
            "effect": "set",
            "evidence": "its install is kept by endOp(true)"
          }
        ],
        "forbidden": [
          "fence Wait while holding state.mu, any per-name mutex, or root.mu",
          "reg.Abort(), reg.Complete() or FinishEval before the fence returns",
          "inflight.Add after closed is set",
          "an in-flight attributed materialization landing after the registration ended"
        ],
        "seeding": [
          "in-flight view materialization: New(nil, WithBytecode(), WithDialect(clojure.Dialect())); root := eng.RootEnv(); root.SetEvaluator(lfProbe{inner: root.Evaluator()}), where lfProbe forwards Eval/Apply and its DefineBootstrap matches the `->` definition exactly (source prefix `(defmacro -> `) and fires once under a sync.Once: close(entered), then <-release; every later call delegates unchanged; a separate releaseOnce, used by both the test body and t.Cleanup, closes release (distinct from the fire-once that closes entered) so a red run does not leak the parked goroutine (ownerProbe pattern lazy_owner_test.go:20-61); then Use(stdlib.New()) with the lazy layer, then arm the probe",
          "the lf plugin's Init spawns go func(){ env.Get(`->`); close(gDone) }(), waits <-entered, then returns lfErr (or nil for the success test)",
          "fence window: Use runs in goroutine U; after <-entered the test spins with runtime.Gosched() (2s wall-clock guard) until either impl.lazyMaterializer.state.op != nil && op.closed (read under state.mu) or U's done channel closes; U closing first fails the test with 'Use returned before in-flight view materialization settled'; only then close(release)",
          "host lookup inside the window (failure test only): RootEnv().Get(`str`) called after the window opens and before release; the success test performs no host lookup"
        ],
        "budgets": [
          "fence waits only for the attributed materializations in flight when it closes (each one definition); no production timeout",
          "each fence test: 2s wall-clock guard on the spin and a 2s select guard on each wait after close(release) (U done, gDone), spin with runtime.Gosched, no sleeps",
          "host lookups: 0 waits on the fence"
        ]
      },
      "redTasks": [
        "[1.2] TestLazyFenceWaitsForInFlightViewMaterialization (-race). The window opens before Use returns; RootEnv().Get(`str`) inside the window returns found; after release, Use returns an error containing 'init plugin', <-gDone, installedNames lacks `->`, MaterializeCount() == before + 1 (only str, host), `(-> 1 (+ 2))` is 3. Red through c3 (deterministic: with no fence, U closes before the window opens). Spec: scenario 'Concurrent host materialization survives abort'; tasks 2.4",
        "[1.2] TestLazyFenceSettlesInFlightMaterializationOnSuccess (-race). Same seeding with Init returning nil and no host lookup in the window; the window opens; after release, Use succeeds, installedNames contains `->`, MaterializeCount() == before + 1, ActivePlugins +1. Red through c3 (U returns before the window opens)"
      ],
      "codeTasks": [
        "2.4 runtime/lazy_template.go: lazyOp gains inflight sync.WaitGroup; materializeOne increments under state.mu when opFor(env) is non-nil and defers Done; m.fence(op) sets closed under state.mu, unlocks, then Waits",
        "2.4 runtime/plugin.go: Use and ReloadPlugin call m.fence(op) right after Init/vocabulary return on both paths, before FinishEval; the fence is idempotent"
      ],
      "redTests": [
        "TestLazyFenceWaitsForInFlightViewMaterialization",
        "TestLazyFenceSettlesInFlightMaterializationOnSuccess"
      ],
      "redRun": "go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess)$'",
      "verify": "go build ./core/... ./runtime/... && go vet ./runtime ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm -skip '^(TestReloadPluginFailedInitRestoresCanonicalBinding|TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval|TestLazyHostTombstoneDuringFailedUseSurvives|TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess)$' && golangci-lint run ./runtime/... ./core/...",
      "coder": "go-coder",
      "redAfter": "c3",
      "race": "completion fence waits on in-flight view materialization while the host looks up names"
    },
    {
      "id": "c5",
      "taskIds": [
        "2.5"
      ],
      "prev": "c4",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "reload-abort",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.snapshotBindings",
          "anchor": "func (e *engineImpl) snapshotBindings() []string {",
          "change": "delete (callers: Use before/after, rollbackPluginUse, ReloadPlugin)"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.snapshotRootEnv / rootEnvSnapshot",
          "anchor": "func (e *engineImpl) snapshotRootEnv() rootEnvSnapshot {",
          "change": "delete with `type rootEnvSnapshot struct {`; only caller `oldRoot := e.snapshotRootEnv()`"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.restoreRootEnv",
          "anchor": "func (e *engineImpl) restoreRootEnv(s rootEnvSnapshot) {",
          "change": "delete; replaced by reg.Abort() (3 call sites in ReloadPlugin)"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.rollbackPluginUse",
          "anchor": "func (e *engineImpl) rollbackPluginUse(name string, before []string) {",
          "change": "replace with Abort + op-owned lazy undo + callCache drop for reverted names; no registry Unregister needed if publish deferred"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "diff / unionOf",
          "anchor": "func diff(after, before []string) map[string]struct{} {",
          "change": "dead once snapshotBindings goes (also `func unionOf(a, b []string) []string {`); only runtime/plugin.go callers"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.removePluginBindings callCache drop",
          "anchor": "e.callCache.drop(n)",
          "change": "also drop callCache entries for names Abort reverted (drop is hygiene; generation guard protects correctness; Abort bumps NameGen)"
        },
        {
          "task": "2.5",
          "file": "runtime/call_cache.go",
          "symbol": "callCache.drop",
          "anchor": "func (c *callCache) drop(name string) {",
          "change": "doc comment names removePluginBindings as sole caller; update if rollback also calls it"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.populateTemplateBindings",
          "anchor": "func (e *engineImpl) populateTemplateBindings(pluginName, pluginVersion string) {",
          "change": "runs before FinishEval today and mutates e.bindings + activate; move into pending/publish-on-success step"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin.go",
          "symbol": "engineImpl.UnloadPlugin",
          "anchor": "func (e *engineImpl) UnloadPlugin(name string) error {",
          "change": "keep last-writer semantics (registry Unregister, removePluginBindings on root, deactivate, `e.stats.decPlugins()`); only adapt to new removePluginBindings signature"
        },
        {
          "task": "2.5",
          "file": "runtime/plugin_reload_rollback_test.go",
          "symbol": "(new test file)",
          "anchor": "",
          "change": "c5 red file; helpers prefixed rr, file-local",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "reload-open",
          "reload-aborted",
          "reload-published",
          "unloaded"
        ],
        "transitions": [
          {
            "input": "ReloadPlugin failure: init, vocabulary, settlement or conflict",
            "state": "reload-open",
            "effect": "clear",
            "evidence": "enters reload-aborted: journal abort only; canonical flags and cell identity restored in place (registration.go:81-133); no replay Set clobbering canonical or host writes (plugin.go:133-152 deleted)"
          },
          {
            "input": "host write during a failing reload",
            "state": "reload-open",
            "effect": "forced",
            "evidence": "survives (the replay no longer rewrites the root); spec scenario 'Concurrent host writes survive abort'"
          },
          {
            "input": "ReloadPlugin success",
            "state": "reload-open",
            "effect": "set",
            "evidence": "enters reload-published: e.bindings[name] = the new diff, activate the new version, count unchanged when hadOld"
          },
          {
            "input": "UnloadPlugin(name) after a success",
            "state": "reload-published",
            "effect": "clear",
            "evidence": "enters unloaded: deletes every name in e.bindings[name] through the root, deactivates, decPlugins; last writer wins (plugin.go:230-250 unchanged apart from the removePluginBindings signature)"
          },
          {
            "input": "Use retried after a failed Use",
            "state": "reload-aborted",
            "effect": "set",
            "evidence": "publishes exactly once: ActivePlugins +1, no residue from the failed attempt"
          }
        ],
        "forbidden": [
          "any whole-root snapshot or replay on a failure path",
          "canonical flags lost on reverted cells",
          "UnloadPlugin deleting a name the plugin did not introduce",
          "ActivePlugins moved by a failed operation"
        ],
        "seeding": [
          "reload overwriting a canonical binding: cl.Dialect() engine; RootEnv().SetCanonical(`rr-canon`, GoFunc returning 1); Use(bindingPlugin v1 adding rr-old); ReloadPlugin(rr plugin v2 that Sets rr-canon to GoFunc returning 2 and adds rr-new, then fails)",
          "partly materialized reload: eng1 and eng2, both New(nil, WithBytecode(), WithDialect(clojure.Dialect())) plus Use(stdlib.New()); on eng1 evaluate (+ 1 2) and (str 1); fnStr, _ := eng1.Func(`str`); RootEnv().Set(`count`, GoFunc returning Int 99) (shadow); RootEnv().Delete(`reverse`); m2 := eng2's MaterializeCount(); eng1.ReloadPlugin(rrFailingStdlib{version: `rr-2.0.0`}), whose Init returns an error before registering",
          "concurrent host writes during a reload: rr barrier plugin as in c1's concurrent test, but through ReloadPlugin over an old bindingPlugin",
          "unload pin: plugin A (rr-a) adds shared and a-only; plugin B (rr-b) overwrites shared and adds b-only; UnloadPlugin(rr-a), then UnloadPlugin(rr-b)"
        ],
        "budgets": [
          "failure path: 0 whole-root scans (LocalNames/LocalFuncNames not called on failure); O(journal entries + lazy entries)",
          "success path: 2 LocalNames+LocalFuncNames snapshots, as today"
        ]
      },
      "redTasks": [
        "[1.1] TestReloadPluginFailedInitRestoresCanonicalBinding. After the failed reload, RootEnv().GetCanonical(`rr-canon`) returns the original GoFunc (call returns 1) with canonical true; rr-old resolves; rr-new absent; Registry version 1.0.0; ActivePlugins 1. Red at baseline and through c4 (the replay Sets non-canonical)",
        "[1.1] TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion. After the failed reload on eng1: `(+ 1 2)` is 3 and GetCanonical(`+`) canonical true; fnStr.Call(ctx, Int 1) succeeds; `(count [1])` is 99 (shadow intact); `(reverse [1 2])` errors 'undefined' (deleted stays deleted); `(nth [10 20] 1)` is 20 (untouched deferred); on eng2 `(reverse [1 2])` evaluates and its MaterializeCount() == m2 + 1 (only that touch). Red at baseline and through c4. Spec: scenario 'Partly materialized reload restores deletion state'",
        "[1.2] TestReloadPluginFailedInitKeepsConcurrentHostWrites (-race). The host add/rebind/delete/ReplaceCell/Rebuild and the op-host-op same-name write survive a failed ReloadPlugin; the old plugin's bindings resolve. Red through c4 (the replay deletes host adds). Spec: scenario 'Concurrent host writes survive abort'",
        "[1.3] TestUseRetryAfterFailedInitPublishesOnce. Guard, green at baseline. Use(overwriting plugin that fails), then Use(same name, succeeding): ActivePlugins == 1, Registry().Get(name) returns the second (succeeding) instance, the second Init's values live, UnloadPlugin removes only the names that Init introduced. Spec: requirement 'Successful ... remain unchanged'; tasks 1.3 successful retry",
        "[1.3] TestUnloadPluginKeepsLastWriterOwnership. Guard, green at baseline. After UnloadPlugin(rr-a): shared and a-only are undefined, b-only live; after UnloadPlugin(rr-b): b-only undefined. Spec: requirement 'Successful unload's existing last-writer semantics ... SHALL remain unchanged'"
      ],
      "codeTasks": [
        "2.5 runtime/plugin.go: delete rootEnvSnapshot, snapshotRootEnv, restoreRootEnv, rollbackPluginUse; ReloadPlugin's failure path matches Use's: fence, FinishEval, m.endOp(false), reg.Abort(), callCache.drop for the names the op wrote",
        "2.5 runtime/call_cache.go: drop doc names both callers (removePluginBindings and the failure path)",
        "2.5 keep snapshotBindings, diff, unionOf (success-path ownership)"
      ],
      "redTests": [
        "TestReloadPluginFailedInitRestoresCanonicalBinding",
        "TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion",
        "TestReloadPluginFailedInitKeepsConcurrentHostWrites",
        "TestUseRetryAfterFailedInitPublishesOnce",
        "TestUnloadPluginKeepsLastWriterOwnership"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestReloadPluginFailedInitRestoresCanonicalBinding|TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion|TestReloadPluginFailedInitKeepsConcurrentHostWrites|TestUseRetryAfterFailedInitPublishesOnce|TestUnloadPluginKeepsLastWriterOwnership)$'",
      "verify": "go build ./core/... ./runtime/... && go vet ./runtime ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime ./core ./core/vm && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval|TestLazyHostTombstoneDuringFailedUseSurvives|TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && golangci-lint run ./runtime/... ./core/...",
      "coder": "go-coder",
      "redAfter": "c0",
      "race": "channel-barrier host writes, registry edits and Evaluator reads racing an initializing plugin and its Abort"
    },
    {
      "id": "c6",
      "taskIds": [
        "3.1"
      ],
      "prev": "c5",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "validation-floor",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [
        "./runtime",
        "./core",
        "./core/vm"
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "Makefile",
          "symbol": "test / lint",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "none; run `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`, `make lint`, focused go test + -race, `openspec validate plugin-binding-rollback --strict --json`"
        }
      ],
      "contract": {
        "states": [
          "floor-run"
        ],
        "transitions": [
          {
            "input": "all coder chunks closed",
            "state": "floor-run",
            "effect": "no-op",
            "evidence": "tasks.md 3.1"
          }
        ],
        "forbidden": [
          "skipping or weakening any section-1 test to pass the floor"
        ],
        "seeding": [
          "none"
        ],
        "budgets": [
          "focused and race legs: -timeout 2m -p 2 -parallel 2 per tasks.md 3.1; full floor: -timeout 10m"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.1 run the fullFloor commands and record names, commands and results"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make lint && make test GOTESTFLAGS='-timeout 10m -p 2 -parallel 2' && go test -race -timeout 10m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval|TestLazyHostTombstoneDuringFailedUseSurvives|TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && go test -race -timeout 10m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration|Registry)'",
      "coder": "zpatcher"
    },
    {
      "id": "c7",
      "taskIds": [
        "3.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "docs-changelog",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.2",
          "file": "ARCHITECTURE.md",
          "symbol": "Plugin Loading Flow",
          "anchor": "### Plugin Loading Flow",
          "change": "describe Init receiving a registration view (not pointer-identical to RootEnv()), abort on failure, host-write precedence, transient read visibility, post-return restoration, external-effect exclusion, retained-charge gap"
        },
        {
          "task": "3.2",
          "file": "CONTEXT.md",
          "symbol": "Registration view glossary",
          "anchor": "**Registration view**:",
          "change": "note plugin Init receives the view; optional **Plugin**: entry cross-ref"
        },
        {
          "task": "3.2",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Changed/Fixed entry: failed Use/ReloadPlugin restore owned bindings; Init env identity change (breaking detail)"
        },
        {
          "task": "3.2",
          "file": "docs/adr/0003-concurrency-model.md",
          "symbol": "registration view concurrency",
          "anchor": "A registration view holds no bindings of its own",
          "change": "optional: concurrent-write precedence during plugin init and the materialization fence"
        }
      ],
      "contract": {
        "states": [
          "docs-updated"
        ],
        "transitions": [
          {
            "input": "3.1 floor passed",
            "state": "docs-updated",
            "effect": "no-op",
            "evidence": "tasks.md 3.2"
          }
        ],
        "forbidden": [
          "new docs files (update existing ones only)",
          "tool or process references in docs"
        ],
        "seeding": [
          "none"
        ],
        "budgets": [
          "none apply"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.2 edit ARCHITECTURE.md, CONTEXT.md, CHANGELOG.md [Unreleased] (Changed: Init env identity, failed Use/ReloadPlugin restore owned state, registry conflict error; Fixed: ActivePlugins after a failed fresh ReloadPlugin settlement, host RegisterValue deferral, Evaluator data race); CHANGELOG Added: Registry.Generation, Registry.PublishIf, CodeRegistryConflict, NewRegistryConflictError; Changed: host RegisterValue outside a plugin operation binds immediately; document the residual NewEnv unlocked eval copy"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "git diff --check && git diff --cached --check",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "baseline-inert-api",
      "tasks": [
        "0.1"
      ],
      "summary": "NO-RED-WAIVER / NO-TESTER-WAIVER: baseline record plus FIELD-FIRST inert declarations; no observable contract. Records at 477d999: registration-journal landed (archive 2026-09-11-registration-journal, core/registration.go present, zero runtime callers of BeginRegistration); review repro #1 (failed stdlib replacement leaves (+ 1 2) undefined) and #2 (failed Use leaves an overwritten value) have no test today; Makefile GOTESTFLAGS ?= -timeout 2m with no -p/-parallel cap, make lint = golangci-lint run; Init argument pointer-identity change (Init receives reg.Env(), not RootEnv()) with no Plugin interface change. FIELD-FIRST: declares every production member a red test names, with no behavior, so all five red files compile and fail on assertions. Sequencing: red stages of c1, c2 and c5 run after this chunk (they name Registry.Generation/PublishIf and CodeRegistryConflict); lazyOp and stdlibLazyEngineState.op are declared by c3, and c4's red stage runs after c3.",
      "contract": {
        "states": [
          "baseline-recorded",
          "inert-declared"
        ],
        "transitions": [
          {
            "input": "first source change of the change",
            "state": "baseline-recorded",
            "effect": "no-op",
            "evidence": "tasks.md 0.1"
          },
          {
            "input": "red stages compile against the inert members",
            "state": "inert-declared",
            "effect": "set",
            "evidence": "FIELD-FIRST rule; red tests name core.CodeRegistryConflict, core.NewRegistryConflictError, Registry.Generation, Registry.PublishIf"
          }
        ],
        "forbidden": [
          "any behavior in an inert member: Generation always returns 0, PublishIf returns nil and mutates nothing",
          "any change to the Plugin interface (core/plugin.go:12-23) or go.mod/go.sum"
        ],
        "seeding": [
          "none: no test seeds this seam"
        ],
        "budgets": [
          "none apply: inert members execute no production path"
        ]
      },
      "codeTasks": [
        "0.1 core/error.go next to CodeRegistrationActive (error.go:134-142): const CodeRegistryConflict = `RegistryConflictError`; func NewRegistryConflictError(name string) *LispicoError returning &LispicoError{Code: CodeRegistryConflict, Message: fmt.Sprintf(`plugin %q registry entry changed during registration`, name)} (constructor is final, not inert)",
        "0.1 core/plugin.go: func (r *Registry) Generation(name string) uint64 { return 0 } (inert); func (r *Registry) PublishIf(p Plugin, gen uint64) error { return nil } (inert)",
        "0.1 record baseline: run the c1..c5 redRun commands once red files land and record every red test failing on an assertion (not on compile)"
      ]
    },
    {
      "id": "view-lifecycle",
      "tasks": [
        "1.4",
        "2.1"
      ],
      "summary": "Use/ReloadPlugin open one core registration on the root before any root write; Init, reload deletion of old names, and applyVocabulary write through reg.Env(); registry entry, e.bindings, lazy activation (populateTemplateBindings) and the ActivePlugins count are published only after Init, vocabulary and FinishEval succeed. Use failure ends with reg.Abort() alone: the name-diff deletion and lazy deactivate leave Use's failure path. ReloadPlugin failure calls reg.Abort() and still runs the old restoreRootEnv replay; the replay goes in 2.5. Env.Evaluator() reads under the owner read lock: this is the carried race, now confirmed. core/env.go:897-899 reads root.eval unlocked, while registration.go:114-116 (Abort) and env.go:910-917 (setEvaluator) write it under root.mu. Owns red tasks 1.2 and 1.4; tests of those tasks that first go green later sit in c3/c4/c5 and are listed there. Red file runtime/plugin_view_rollback_test.go (helpers prefixed vr).",
      "contract": {
        "states": [
          "idle",
          "op-active",
          "op-settling",
          "published",
          "aborted",
          "retired-view"
        ],
        "transitions": [
          {
            "input": "Use(p) with Registry().Get(name) absent",
            "state": "idle",
            "effect": "set",
            "evidence": "enters op-active via RootEnv().BeginRegistration(); no registry write (today plugin.go:160 registers first); design.md 'Keep old registry entries, bindings, and active-plugin counts pending'"
          },
          {
            "input": "Use(p) with the name already registered",
            "state": "idle",
            "effect": "no-op",
            "evidence": "returns error wrapping 'register plugin %s' and containing 'already registered'; no registration begun; plugin_test.go:89-110 TestUse_AlreadyRegistered"
          },
          {
            "input": "BeginRegistration refused (a host holds a registration on RootEnv())",
            "state": "idle",
            "effect": "no-op",
            "evidence": "Use/ReloadPlugin return fmt.Errorf(`register plugin %s: %w`) wrapping *LispicoError Code == core.CodeRegistrationActive; registration.go:50-58"
          },
          {
            "input": "Init write through its env: Set/SetFunc/SetCanonical/SetBoth/Delete/ReplaceCell, Find owner, child-scope set!, evaluator reentry, closure over the env, MergeInto(env)",
            "state": "op-active",
            "effect": "set",
            "evidence": "journaled via viewReg routing (env.go Set*/Delete/mergeInto); spec scenario 'Registration aliases retain ownership'; runtime/eval.go:402-430 runs reentry on the passed env"
          },
          {
            "input": "host raw write through RootEnv(): add, rebind, delete, ReplaceCell, Rebuild",
            "state": "op-active",
            "effect": "forced",
            "evidence": "unattributed; survives abort (registration.go:81-113); spec scenario 'Concurrent host writes survive abort'"
          },
          {
            "input": "host first touch of an unrelated deferred name via RootEnv().Get",
            "state": "op-active",
            "effect": "forced",
            "evidence": "raw root materialization, unattributed; survives Use failure because Use no longer deletes a name diff (plugin.go:212-228 deleted it); spec scenario 'Concurrent host materialization survives abort'"
          },
          {
            "input": "Registry().Get(name) / Generation(name) read while Init runs",
            "state": "op-active",
            "effect": "no-op",
            "evidence": "Use: absent / 0; ReloadPlugin: the old plugin and its generation; design.md pending registry"
          },
          {
            "input": "Init returns an error",
            "state": "op-active",
            "effect": "clear",
            "evidence": "enters aborted: FinishEval, then reg.Abort(); returns error wrapping 'init plugin %s'; registry, e.bindings, ActivePlugins unchanged; spec requirement paragraph 1"
          },
          {
            "input": "applyVocabulary(view) returns an error",
            "state": "op-active",
            "effect": "clear",
            "evidence": "enters aborted; returns error wrapping 'apply vocabulary for plugin %s' with Code CodeResourceLimit; engine.go:458-529; spec scenario 'Vocabulary rejection rolls back once'"
          },
          {
            "input": "FinishEval returns a settlement error (retained charge denied)",
            "state": "op-settling",
            "effect": "clear",
            "evidence": "enters aborted; ReloadPlugin of a fresh name leaves ActivePlugins unchanged (defect today: plugin.go:334-336 increments before FinishEval at 338-341)"
          },
          {
            "input": "Init, vocabulary and FinishEval all succeed",
            "state": "op-settling",
            "effect": "set",
            "evidence": "enters published: registry entry via RegisterNoCheck in this chunk (PublishIf from 2.2), reg.Complete(), e.bindings[name] = diff(after, before) or delete when empty, populateTemplateBindings, stats.incPlugins (Use always; ReloadPlugin only when !hadOld); plugin.go:191-209"
          },
          {
            "input": "ReloadPlugin(p) with an old plugin",
            "state": "idle",
            "effect": "set",
            "evidence": "enters op-active; old names deleted through the view (removePluginBindings(view, name)); e.bindings[name] and the registry entry stay until publish; plugin.go:252-268"
          },
          {
            "input": "ReloadPlugin failure (interim, until 2.5)",
            "state": "op-active",
            "effect": "clear",
            "evidence": "enters aborted: reg.Abort(), then restoreRootEnv(oldRoot); registry/e.bindings untouched; no lazy deactivate (plugin.go:225-227 removed from every failure path)"
          },
          {
            "input": "call through a retained Init env or closure after Use returned",
            "state": "retired-view",
            "effect": "no-op",
            "evidence": "unattributed forward to the live root; registration.go:152-159; spec scenario 'Successful registration remains live'"
          },
          {
            "input": "host RootEnv().Evaluator() concurrent with view.SetEvaluator and the Abort restore",
            "state": "op-active",
            "effect": "forced",
            "evidence": "read under owner RLock; no data race; env.go:897-899 carried defect"
          }
        ],
        "forbidden": [
          "Registry().Get(name) returning the operation's plugin while its Init runs",
          "ActivePlugins, e.bindings[name] or lazy state.active changed by a failed Use/ReloadPlugin",
          "Init receiving e.rootEnv instead of reg.Env()",
          "a registration still active after Use/ReloadPlugin returns: RootEnv().BeginRegistration() must succeed right after return",
          "both Complete and Abort on one operation; Complete on any error path",
          "an unsynchronized read of Env.eval in Evaluator()"
        ],
        "seeding": [
          "idle: New(nil, WithDialect(cl.Dialect())) when a function cell is needed, else New(nil, WithDialect(clojure.Dialect())); pre-seed through eng.RootEnv().Set / SetFunc / SetCanonical before Use; macros through eng.Eval before Use",
          "op-active: file-local vr barrier plugin whose Init performs its writes, closes entered, blocks on <-release; Use runs in a goroutine; the test acts only after <-entered and only through RootEnv() or Registry() (eng.Eval, Bind and LoadScope block on e.mu for the whole Use: runtime/eval.go:741, 1057, 1124)",
          "op-settling with settlement failure: New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(&recordingMeter{chargeErr: errors.New(...)})), m.reset() after New, plugin adding a fresh name (setupPlugin pattern meter_test.go:606-610)",
          "vocabulary failure: dialect core.FullDialect().Vocabulary(map[string]string{`vr-visible`: `vr-canon`}) with WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 16}); Init binds GoFunc vr-canon, overwrites a pre-seeded name, then fills fresh names until env.RetainedUsage() slots == 16, so Init succeeds and the vocab Set of vr-visible is refused with CodeResourceLimit",
          "retired-view: plugin stores its Init env in a field; the test uses it after Use returns",
          "evaluator race: Init calls env.SetEvaluator(wrapper around env.Evaluator()), closes evalSet, returns an error; a host goroutine loops RootEnv().Evaluator() from <-evalSet until Use returns (at most 10000 iterations)"
        ],
        "budgets": [
          "each Use/ReloadPlugin: exactly 1 BeginRegistration and exactly 1 of Complete/Abort",
          "Evaluator(): +1 RLock/RUnlock of the owner mutex, 0 allocs; not on the VM or tree-walker per-call path (callers: core/depth.go:42-47, core/value_walk_context.go:399, runtime/lazy_template.go:463/503, plugins/stdlib/bootstrap.go:42-45, plugins/json 1 site)",
          "Get/Cell/NameGen lookup paths: +0 instructions in this chunk",
          "Use success: +2 allocs (Registration, view) per call, off the eval path",
          "every barrier test: 2s watchdog (select with time.After used only as deadlock detection, never for ordering)"
        ]
      },
      "redTasks": [
        "[1.1] TestUseFailedInitRestoresOverwrittenBindings, cl.Dialect(). Pre-seed vr-val=Int 1 (Set), vr-fn GoFunc returning 1 (SetFunc), vr-canon GoFunc (SetCanonical); fn, _ := eng.Func(`vr-fn`); gen0 := RootEnv().NameGen(); root0 := RootEnv(). Plugin overwrites all three (non-canonical Set for vr-canon), adds vr-new-val (Set) and vr-new-fn (SetFunc), then returns an error. Assert: values equal the originals, RootEnv().GetCanonical(`vr-canon`) canonical true, the vr-new-* Get/GetFunc report not found, Registry().Get false, Generation == 0, Stats().ActivePlugins == 0, fn.Call(ctx) returns Int 1, NameGen() >= gen0, RootEnv() == root0. Red at baseline (overwrites survive). Spec: scenario 'Failed initial load restores overwritten bindings' plus requirement paragraph 1 (handles, canonical, registry, count, root identity, generations)",
        "[1.1] TestUseFailedInitRestoresMacro, clojure. eng.Eval `(defmacro vr-m [x] x)`; the plugin runs env.Evaluator().Eval(ctx, (defmacro vr-m [x] 99), env) and fails; after that, eng.Eval `(vr-m 1)` returns Int 1. Red at baseline. Spec: scenario 'Failed initial load restores overwritten bindings' (macro), requirement 'Cache invalidation'",
        "[1.1] TestUseRegistryPendingDuringInit (-race). Barrier plugin; at <-entered, Registry().Get(name) false and Generation(name) == 0; release, Init fails; still absent, ActivePlugins 0. Red at baseline (registered first)",
        "[1.1] TestReloadPluginRegistryKeepsOldDuringInit (-race). bindingPlugin v1.0.0 Use; ReloadPlugin(barrier v2.0.0); at <-entered, Registry().Get(name) returns version 1.0.0; release, Init fails; still 1.0.0, ActivePlugins 1. Red at baseline",
        "[1.3] TestReloadPluginSettlementFailureKeepsActiveCount. Meter chargeErr; ReloadPlugin(fresh plugin adding a name) returns Code CodeResourceLimit; Stats().ActivePlugins == 0, Registry().Get false, the added name absent. Red at baseline (count 1)",
        "[1.3] TestUseFailedVocabularyRestoresOwnedBindings. Vocabulary seeding above; error contains 'apply vocabulary for plugin' with Code CodeResourceLimit; the pre-seeded overwritten name restored, vr-canon and the fillers absent, ActivePlugins unchanged, Registry().Get false; a follow-up Use of a plugin writing nothing succeeds. Red at baseline (overwrite survives). Spec: scenario 'Vocabulary rejection rolls back once'. Setup asserts New() boot retained usage stays below MaxRetainedSlotsPerEnv so the vocabulary rejection is the only failure; Abort does not release retained slots until plugin-retained-rollback, so the follow-up Use writes nothing",
        "[1.2] TestUseFailedInitKeepsConcurrentHostWrites (-race). Pre-seed a=1, h-rebind=1, h-del=1, h-repl=1. Plugin writes a=2, closes entered, waits release, writes a=3, returns an error. At <-entered the host does RootEnv().Set(`h-add`, 1), Set(`h-rebind`, 9), Delete(`h-del`), ReplaceCell(`h-repl`, 5), Set(`a`, 7), Rebuild(), then release. Assert h-add 1, h-rebind 9, h-del absent, h-repl 5, a 7 (op-host-op rebases to the host state), plugin-only names absent. Red at baseline (diff deletes h-add). Spec: scenario 'Concurrent host writes survive abort'",
        "[1.2] TestUseFailedInitKeepsHostLazyMaterialization (-race). clojure engine plus stdlib.New() (cold); barrier plugin; at <-entered the host calls RootEnv().Get(`str`) (true); release, Init fails; `(str 1)` evaluates, installedNames(impl) contains str, MaterializeCount() == before + 1. Red at baseline (the diff deletes the str cell and the installed path then misses). Spec: scenario 'Concurrent host materialization survives abort'",
        "[1.4] TestUseFailedInitRevertsAliasWrites (-race), clojure, pre-seed x=1, subtests find/child/evaluator/merge/closure. find: owner, _ := env.Find(`x`); owner.Set(`x`, 2). child: env.Evaluator().Eval(ctx, (set! x 2), env.Child()). evaluator: Eval (def x 2) on env. merge: src := core.NewEnv(nil); src.Set(`x`, 2); src.MergeInto(env). closure: Eval (defn vr-bump [] (set! x 2)) on env, then (vr-bump). Each plugin fails; after that x == 1 and vr-bump is absent. Red at baseline. Spec: scenario 'Registration aliases retain ownership'",
        "[1.4] TestUseEvaluatorReadDuringFailedInitIsRaceFree (-race only). Evaluator-race seeding; after Use returns, RootEnv().Evaluator() is the original evaluator (Abort restored it). Red only under -race at baseline (unlocked read vs locked write)",
        "[1.4] TestUseSuppliedEnvStaysLiveAfterSuccess. Guard, green at baseline. Plugin retains its env and evaluates (defn vr-get [] vr-x) on it; after Use succeeds: RootEnv().Set(`vr-x`, 5); retained.Get(`vr-x`) is 5; `(vr-get)` is 5; retained.Set(`vr-late`, 1) lands in root and survives a later failing Use of another plugin; UnloadPlugin removes the plugin-added vr-get. Spec: scenario 'Successful registration remains live'"
      ],
      "codeTasks": [
        "2.1 runtime/plugin.go Use: duplicate check: `if _, ok := e.registry.Get(name); ok` returns fmt.Errorf(`register plugin %s: %w`) around the core `plugin %q already registered` text (TestUse_AlreadyRegistered stays green); no Generation read in this chunk; reg, err := e.rootEnv.BeginRegistration() (err wrapped the same); view := reg.Env(); before := snapshotBindings(); StartEval; initPlugin(p, view, name, version); applyVocabulary(view); after-diff kept local; FinishEval; success: e.registry.RegisterNoCheck(p), reg.Complete(), e.bindings[name] = added, populateTemplateBindings, incPlugins; any error: FinishEval if pending, then reg.Abort(); no Unregister, no deactivate, no diff deletion",
        "2.1 runtime/plugin.go ReloadPlugin: Begin (no Generation read in this chunk); removePluginBindings(view, name) without deleting e.bindings[name]; same pipeline; success replaces e.bindings[name], RegisterNoCheck(p), Complete, populate, incPlugins only when !hadOld (moved after FinishEval); failure: reg.Abort() then restoreRootEnv(oldRoot) (interim)",
        "2.1 runtime/plugin.go initPlugin(p, env, name, version): pass env on both branches, including the ensureLayer build callback (plugin.go:90-96); loadingPlugin/loadingVersion/eager bookkeeping stays until 2.3",
        "2.1 runtime/plugin.go removePluginBindings(env *core.Env, name string): delete through env; e.bindings deletion moves to callers (UnloadPlugin passes e.rootEnv and still deletes e.bindings[name]: last-writer semantics unchanged)",
        "2.1 runtime/engine.go applyVocabulary(env *core.Env): every e.rootEnv use (engine.go:465-526) goes through env",
        "2.1 core/env.go Evaluator(): o := e.owner(); o.mu.RLock(); defer o.mu.RUnlock(); return o.eval. Caller audit: no caller holds any env mutex (grep before landing); the known in-core caller core/depth.go runs under no env lock — confirm it and every other caller before landing (RWMutex is not reentrant); the residual unlocked eval copy in NewEnv stays documented-only",
        "2.1 runtime/plugin.go rollbackPluginUse: no longer called from Use; ReloadPlugin keeps only the restoreRootEnv replay until 2.5"
      ]
    },
    {
      "id": "registry-publication",
      "tasks": [
        "1.3",
        "2.2"
      ],
      "summary": "Conditional registry publication in core.Registry. Every entry carries a generation taken from a registry-wide monotonic sequence; an absent name has generation 0. Use and ReloadPlugin observe the generation at the start and publish with PublishIf. If a host edited the entry through eng.Registry() in between, core returns *LispicoError Code CodeRegistryConflict at publish time; runtime wraps it as 'publish plugin %s' and aborts, so the host entry or removal stands. Publish runs after FinishEval and before reg.Complete(), and nothing after it can fail. Owns red task 1.3; its vocabulary/count tests sit in c1 and its retry/unload guards in c5. Red file runtime/plugin_registry_publish_test.go (helpers prefixed rp).",
      "contract": {
        "states": [
          "absent",
          "registered-gen-g",
          "op-observed-g",
          "host-edited",
          "published-gen-g2",
          "conflict"
        ],
        "transitions": [
          {
            "input": "Register(p) / RegisterNoCheck(p) / PublishIf that installs",
            "state": "absent|registered-gen-g",
            "effect": "set",
            "evidence": "entry gen = ++seq (strictly increasing, never reused)"
          },
          {
            "input": "Unregister(name)",
            "state": "registered-gen-g",
            "effect": "clear",
            "evidence": "entry removed; Generation(name) == 0; core/plugin.go:86-91"
          },
          {
            "input": "Register(p) on a present name",
            "state": "registered-gen-g",
            "effect": "no-op",
            "evidence": "existing error 'plugin %q already registered'; generation unchanged; core/plugin.go:45-56"
          },
          {
            "input": "PublishIf(p, gen) where Generation(p.Name()) == gen",
            "state": "op-observed-g",
            "effect": "set",
            "evidence": "installs p, new generation > gen; returns nil; design.md 'conditional publication seam'"
          },
          {
            "input": "PublishIf(p, gen) where Generation(p.Name()) != gen",
            "state": "host-edited",
            "effect": "no-op",
            "evidence": "returns NewRegistryConflictError(p.Name()); entry and generation untouched; design.md 'preserve the host entry and abort with a conflict error'"
          },
          {
            "input": "Use/ReloadPlugin publish returns the conflict",
            "state": "conflict",
            "effect": "clear",
            "evidence": "runtime returns fmt.Errorf(`publish plugin %s: %w`) and runs the failure path (reg.Abort()); ActivePlugins and e.bindings unchanged; tasks.md 2.2"
          },
          {
            "input": "host RegisterNoCheck(hostP) during Use's Init",
            "state": "op-observed-g",
            "effect": "forced",
            "evidence": "host wins: Registry().Get(name) == hostP after Use returns"
          },
          {
            "input": "host Unregister(name) during ReloadPlugin's Init",
            "state": "op-observed-g",
            "effect": "forced",
            "evidence": "host removal wins: registry absent after return; the old plugin's bindings are restored by abort"
          },
          {
            "input": "Use retried after the host frees the name",
            "state": "absent",
            "effect": "set",
            "evidence": "spec requirement: failed operations leave no residue; tasks.md 1.3 successful retry"
          }
        ],
        "forbidden": [
          "PublishIf overwriting an entry whose generation differs from the observed one",
          "a generation value reused after Unregister",
          "Registry.mu held across any env, lazy or engine lock",
          "comparing Plugin interface values for identity (non-comparable dynamic types panic); generations only",
          "any runtime publish path other than PublishIf after this chunk (RegisterNoCheck stays public API; runtime stops calling it)"
        ],
        "seeding": [
          "absent/registered: core.NewRegistry() plus Register/Unregister directly (runtime test file, package runtime)",
          "op-observed with a host edit: file-local rp barrier plugin (writes pre-seeded x=2 and new rp-y, closes entered, blocks on release, returns nil); after <-entered the host calls eng.Registry().RegisterNoCheck(rpHost{name}) or Unregister(name), then release",
          "retry: after the conflict, eng.Registry().Unregister(name), then Use(a fresh non-blocking instance)"
        ],
        "budgets": [
          "Generation and PublishIf: O(1) under Registry.mu, 0 allocs besides the map entry",
          "exactly 1 PublishIf per successful-path Use/ReloadPlugin; 0 on failure paths",
          "registry entry grows by one uint64; seq is uint64, so no wrap is reachable"
        ]
      },
      "redTasks": [
        "[1.3] TestUsePublishIfRejectsStaleGeneration. r := core.NewRegistry(); Generation(`p`) == 0; PublishIf(p1, 0) nil and g1 := Generation > 0; PublishIf(p2, 0) returns *core.LispicoError Code core.CodeRegistryConflict whose message contains `p`, and Get still returns p1 with Generation == g1; PublishIf(p2, g1) nil, Get returns p2, Generation > g1; Unregister gives 0; Register(p3) gives a Generation greater than every earlier one; RegisterNoCheck bumps it. Red at baseline (inert Generation 0, inert PublishIf nil)",
        "[1.3] TestUseRegistryConflictKeepsHostEntry (-race), clojure, pre-seed x=1. The host RegisterNoCheck(rpHost) runs during Init; Use returns an error that errors.As to *core.LispicoError with Code == core.CodeRegistryConflict; Registry().Get(name) returns the same rpHost pointer, Generation equals the value read right after the host write, x == 1, rp-y absent, ActivePlugins unchanged. Then Unregister and Use(fresh) succeed with ActivePlugins +1 and rp-y live. Red at baseline and after c1 (RegisterNoCheck overwrites the host). Spec: requirement 'plugin registry/ownership ... remain as before'; tasks 1.3 host registry conflict and successful retry",
        "[1.3] TestReloadPluginRegistryConflictKeepsHostRemoval (-race). Use v1 (bindingPlugin adding rp-old); ReloadPlugin(rp barrier v2, which adds rp-new); the host Unregister(name) runs during Init; the error has Code CodeRegistryConflict; Registry().Get false; rp-old resolves, rp-new absent; ActivePlugins still 1. Red at baseline (reload succeeds)"
      ],
      "codeTasks": [
        "2.2 core/plugin.go: Registry{mu; plugins map[string]registryEntry; seq uint64} with type registryEntry struct{ p Plugin; gen uint64 }; Register/RegisterNoCheck assign gen = ++seq; Get/Namespaces/HasPrefix read entry.p; Generation returns the entry gen or 0; PublishIf(p, gen) compares and installs under mu, else returns NewRegistryConflictError(p.Name())",
        "2.2 runtime/plugin.go: Use reads gen0 := e.registry.Generation(name) first, replacing c1's Get check: gen0 != 0 refuses with the same `register plugin %s: %w` wrapping around the `already registered` text; then BeginRegistration and ReloadPlugin reads gen := Generation(name) before Get; Use publishes with e.registry.PublishIf(p, gen0) and ReloadPlugin with PublishIf(p, gen), after FinishEval and before reg.Complete(); an error is wrapped `publish plugin %s: %w` and takes the failure path",
        "2.2 core/plugin_test.go existing TestRegistry_* stay green unchanged"
      ]
    },
    {
      "id": "lazy-attribution",
      "tasks": [
        "1.1",
        "2.3"
      ],
      "summary": "Registration identity travels explicitly: the lazy layer attributes a registration, lookup, materialization or tombstone to the running operation only when the env it receives is that operation's view, while the op is open. It never uses loadingPlugin, which is removed together with m.loadingVersion and m.eager. Per-engine lazy writes (installed membership, tombstones, the materialized count) are journaled per name with before-images and a foreign flag. Failure undoes only owned entries, and only before reg.Abort(). Carried defects closed here: (a) view lookups reach the layer with the root (env.go:532-536 on the view walk, 644-650, 664-670, 848-853, 1046-1048, 1076-1078); (b) deleteName hands the root to TombstoneForDelete (env.go:953-955), and Abort never reverts that tombstone. Activation stays pending (2.1), so state.active is never journaled. Owns red task 1.1; its Use tests sit in c1 and its reload-restore tests in c5. Red file runtime/lazy_rollback_test.go (helpers prefixed lr).",
      "contract": {
        "states": [
          "no-op-open",
          "op-open",
          "entry-owned",
          "entry-foreign",
          "op-committed",
          "op-undone"
        ],
        "transitions": [
          {
            "input": "Begin of a plugin operation: m.beginOp(view, name, version, eager)",
            "state": "no-op-open",
            "effect": "set",
            "evidence": "state.op = &lazyOp{view, name, version, eager} under state.mu; replaces initPlugin's loadingPlugin/loadingVersion/eager writes (plugin.go:74-88)"
          },
          {
            "input": "RegisterValue(env == op.view) with op.name == `` and !op.eager",
            "state": "op-open",
            "effect": "set",
            "evidence": "deferred into key{dialectFP, op.name, op.version} (lazy_template.go:548-592)"
          },
          {
            "input": "RegisterValue/RegisterSource with any other env, or no open op",
            "state": "no-op-open|op-open",
            "effect": "forced",
            "evidence": "binds immediately on env (SetCanonical/Set) / returns false; fixes a host RegisterValue outside Use being deferred into an inactive key{fp, ``, ``} today (lazy_template.go:552-561)"
          },
          {
            "input": "lookup miss through the view: Get/GetCanonical/GetFunc/GetFuncCanonical/CellLocal/FuncCellLocal/Find/VarNames/FuncNames",
            "state": "op-open",
            "effect": "set",
            "evidence": "the layer receives the view and materializes through it (journaled in core); entry records the before-image (installed=false) and the count delta; core-engine spec.md:1242"
          },
          {
            "input": "lookup miss through the root or any non-current view (host, retired view)",
            "state": "no-op-open|op-open",
            "effect": "forced",
            "evidence": "materializes through m.engine.rootEnv (raw, unattributed); an existing owned entry for that name becomes foreign; spec scenario 'Concurrent host materialization survives abort'"
          },
          {
            "input": "Delete through the view",
            "state": "op-open",
            "effect": "set",
            "evidence": "Delete calls layer.TombstoneForDelete(view, name) after forwarding; entry records the before-image (tombstoned, installed); env.go:924-956"
          },
          {
            "input": "Delete through the root (host)",
            "state": "op-open",
            "effect": "forced",
            "evidence": "tombstone applied; an existing entry for that name becomes foreign"
          },
          {
            "input": "op write to a foreign entry",
            "state": "entry-foreign",
            "effect": "set",
            "evidence": "rebases the before-image to the current membership, clears foreign (core rule registration.go:173-190)"
          },
          {
            "input": "operation fails",
            "state": "op-open",
            "effect": "clear",
            "evidence": "m.endOp(false) under state.mu restores each owned entry's tombstoned/installed membership, subtracts owned installs from materialized, sets state.op = nil; then reg.Abort()"
          },
          {
            "input": "operation succeeds",
            "state": "op-open",
            "effect": "clear",
            "evidence": "m.endOp(true): the journal is dropped and op installs stay; then e.bindings and activate as in 2.1"
          },
          {
            "input": "sibling engine sharing the template",
            "state": "no-op-open|op-open",
            "effect": "no-op",
            "evidence": "per-engine state only; published layers never written (lazy_template.go:180-194)"
          }
        ],
        "forbidden": [
          "restoring state.active, state.installed or state.tombstoned from a whole-map snapshot",
          "a view lookup materializing through the view after its op closed or ended (journal and lazy attribution must agree)",
          "state.mu held while calling any env method or while root.mu is held (no nesting in either order)",
          "reading engine.loadingPlugin, m.loadingVersion or m.eager (fields removed)",
          "installed[name] true for a name whose root cell reverted to tombstoned after the op finished",
          "writes to a published stdlibTemplateLayer"
        ],
        "seeding": [
          "cold stdlib: New(nil, WithBytecode(), WithDialect(clojure.Dialect())) plus Use(stdlib.New()); MaterializeCount() == 0",
          "failing reload of stdlib: file-local lrFailingStdlib{} with Name() ``, Metadata().Version `lr-2.0.0`, and an Init that returns lrErr before any registration",
          "op-open: file-local lr barrier plugin (non-empty name) whose Init touches env (Get/Evaluator().Eval/Delete) and returns an error or blocks on channels",
          "host tombstone during op: at <-entered, RootEnv().Delete(`sort`), then release; the plugin deletes `sort` after release (host-op order); the host also deletes `concat` after the plugin's pre-barrier env.Delete(`concat`) (op-host order)",
          "lazy state reads: installedNames(impl) (existing lazy_materialize_test.go:498); a file-local lrTombstoned(impl, name) reading state.tombstoned under state.mu"
        ],
        "budgets": [
          "LookupAndMaterialize miss path: 0 extra mutex acquisitions (the op check runs in the existing state.mu section), +1 pointer compare",
          "non-view env lookups: core +0 instructions (the env handed to the layer is e, which equals owner() off a view)",
          "Env stays 208 bytes (TestEnv_Size); view.lazyLayer is set once at BeginRegistration",
          "lazy journal: at most 1 entry per distinct name the op touched; endOp O(entries)",
          "a true miss through a view pays at most 2 template consults (view layer, then root walk)"
        ]
      },
      "redTasks": [
        "[1.1] TestReloadPluginFailedColdStdlibKeepsDeferredNames. Cold stdlib; ReloadPlugin(lrFailingStdlib) returns an error containing 'init plugin'; Registry().Get(``) Metadata().Version == 1.0.0; MaterializeCount() == 0 right after; then `(+ 1 2)` is 3, `(str 1 2)` evaluates, `(-> 1 (+ 2))` is 3; ActivePlugins unchanged. Red at baseline and through c2 (root tombstones never reverted). Spec: scenario 'Failed cold stdlib reload keeps deferred names'; requirement 'without forcing eager materialization'",
        "[1.1] TestLazyViewMaterializationRevertsWithFailedUse. Cold stdlib, before := MaterializeCount(); the lr plugin runs env.Get(`str`) and env.Evaluator().Eval(ctx, (reverse [1 2]), env), then fails; installedNames lacks str and reverse, MaterializeCount() == before; then `(str 1)` evaluates and the count is before + 1. Red at baseline and through c2. Spec: 'Effects reached through that supplied environment SHALL remain attributable'",
        "[1.1] TestLazyViewDeleteTombstoneRevertsWithFailedUse. Cold stdlib; eng.Eval `(+ 1 2)` first (materializes +); the plugin runs env.Delete(`nth`) (deferred) and env.Delete(`+`), then fails; lrTombstoned is false for both; `(nth [10 20] 1)` is 20 and `(+ 1 2)` is 3. Red through c2. Spec: requirement 'deferred attachments, and deletion state SHALL remain as before'",
        "[1.1] TestLazyRegisterValueWithoutOperationBindsImmediately. Cold stdlib; with no operation, eng.RootEnv().RegisterValue(`lr-host`, GoFunc, false) returns nil and RootEnv().Get(`lr-host`) is found immediately. Red at baseline (deferred into an inactive key)",
        "[1.2] TestLazyHostTombstoneDuringFailedUseSurvives (-race). Guard, green at baseline, pins the foreign rule. Host-op order on sort and op-host order on concat as seeded; after the failure both stay undefined and lrTombstoned is true for both"
      ],
      "codeTasks": [
        "2.3 core/registration.go BeginRegistration: view.lazyLayer.Store(root.lazyLayer.Load()); lazy() doc updated (a view carries its root's layer)",
        "2.3 core/env.go setLazyLayer: while a registration is active also store the new pointer into cur.view.lazyLayer; Abort's lazy restore likewise stores into r.view before endLocked",
        "2.3 core/env.go CellLocal/FuncCellLocal: layer.LookupAndMaterialize(e, ...) instead of o, with the re-read through o; Find view branch: when !r.root.HasLive(name) consult e.lazy() with e before delegating to r.root.Find; VarNames/FuncNames: layer.ForceAll(e)",
        "2.3 core/env.go Delete: deleteName stops calling the layer; Delete calls LazyLayer().TombstoneForDelete(e, name) after forwarding (e is the view or the root)",
        "2.3 runtime/lazy_template.go: declare type lazyOp struct { view *core.Env; closed bool; name, version string; eager bool; entries map[string]*lazyOpEntry; installs int64 } and field op *lazyOp on stdlibLazyEngineState (guarded by state.mu, nil outside a plugin operation; c4 red tests read state.op and op.closed); entries map[string]*lazyOpEntry (tomb, installed, foreign bool) and installs int64; add m.beginOp, m.opFor(env) (under state.mu: the op when state.op != nil && !state.op.closed && env == state.op.view), m.endOp(commit bool); LookupAndMaterialize/materializeOne/installValue/publishBootstrap/recordInstall/TombstoneForDelete/RegisterValue/RegisterSource/ForceAll take attribution from opFor; the unattributed materialization target is m.engine.rootEnv; the eager check reads state.op.eager under state.mu",
        "2.3 runtime/engine.go and plugin.go: remove loadingPlugin, loadingVersion, m.eager; initPlugin calls beginOp; failure paths call m.endOp(false) before reg.Abort(); success calls m.endOp(true) after reg.Complete()",
        "2.3 core/env.go view Get/GetFunc/GetFuncCanonical: consult the lazy layer only when the owner has no live binding (HasLive/HasLiveFunc on the root), the same guard as Find — a root-live or host-shadowed name must not reach LookupAndMaterialize (no MaterializeCount bump, no installed mark)"
      ]
    },
    {
      "id": "materialization-fence",
      "tasks": [
        "1.2",
        "2.4"
      ],
      "summary": "Completion fences attributed in-flight materialization. Under state.mu, an attributed materializeOne calls op.inflight.Add(1) before releasing the lock and Done on exit. m.fence(op) sets op.closed under state.mu, releases it, and op.inflight.Wait()s with no env or lazy lock held; e.mu stays held, which is legal because materialization never takes e.mu (lazy_template.go:260-265). The fence runs before FinishEval on both paths, so settlement and journal retirement see a quiescent op. After closed, lookups through the view take the host path and write through the root. Red file runtime/lazy_fence_test.go (helpers prefixed lf).",
      "contract": {
        "states": [
          "op-open",
          "op-closed-waiting",
          "op-quiescent"
        ],
        "transitions": [
          {
            "input": "attributed lookup through the view starts materializing",
            "state": "op-open",
            "effect": "set",
            "evidence": "inflight +1 under state.mu (WaitGroup Add never concurrent with Wait: Add only while !closed)"
          },
          {
            "input": "Init returned (success or error)",
            "state": "op-open",
            "effect": "set",
            "evidence": "enters op-closed-waiting: closed = true, Wait; design.md 'close registration, wait for those operations without holding env/lazy locks, then settle'"
          },
          {
            "input": "view lookup arriving while op-closed-waiting",
            "state": "op-closed-waiting",
            "effect": "forced",
            "evidence": "host path through m.engine.rootEnv; survives abort; design.md 'A late use of the completed view is an ordinary host operation'"
          },
          {
            "input": "host lookup of an unrelated deferred name while op-closed-waiting",
            "state": "op-closed-waiting",
            "effect": "no-op",
            "evidence": "completes without waiting on the fence; tasks.md 2.4 'without blocking unrelated host lookup'"
          },
          {
            "input": "last in-flight materialization finishes",
            "state": "op-closed-waiting",
            "effect": "clear",
            "evidence": "enters op-quiescent; FinishEval, then commit or undo per the lifecycle seam"
          },
          {
            "input": "in-flight materialization finishing before a failure",
            "state": "op-closed-waiting",
            "effect": "clear",
            "evidence": "its writes are still journaled (registration active) and undone by endOp(false) plus reg.Abort()"
          },
          {
            "input": "in-flight materialization finishing before a success",
            "state": "op-closed-waiting",
            "effect": "set",
            "evidence": "its install is kept by endOp(true)"
          }
        ],
        "forbidden": [
          "fence Wait while holding state.mu, any per-name mutex, or root.mu",
          "reg.Abort(), reg.Complete() or FinishEval before the fence returns",
          "inflight.Add after closed is set",
          "an in-flight attributed materialization landing after the registration ended"
        ],
        "seeding": [
          "in-flight view materialization: New(nil, WithBytecode(), WithDialect(clojure.Dialect())); root := eng.RootEnv(); root.SetEvaluator(lfProbe{inner: root.Evaluator()}), where lfProbe forwards Eval/Apply and its DefineBootstrap matches the `->` definition exactly (source prefix `(defmacro -> `) and fires once under a sync.Once: close(entered), then <-release; every later call delegates unchanged; a separate releaseOnce, used by both the test body and t.Cleanup, closes release (distinct from the fire-once that closes entered) so a red run does not leak the parked goroutine (ownerProbe pattern lazy_owner_test.go:20-61); then Use(stdlib.New()) with the lazy layer, then arm the probe",
          "the lf plugin's Init spawns go func(){ env.Get(`->`); close(gDone) }(), waits <-entered, then returns lfErr (or nil for the success test)",
          "fence window: Use runs in goroutine U; after <-entered the test spins with runtime.Gosched() (2s wall-clock guard) until either impl.lazyMaterializer.state.op != nil && op.closed (read under state.mu) or U's done channel closes; U closing first fails the test with 'Use returned before in-flight view materialization settled'; only then close(release)",
          "host lookup inside the window (failure test only): RootEnv().Get(`str`) called after the window opens and before release; the success test performs no host lookup"
        ],
        "budgets": [
          "fence waits only for the attributed materializations in flight when it closes (each one definition); no production timeout",
          "each fence test: 2s wall-clock guard on the spin and a 2s select guard on each wait after close(release) (U done, gDone), spin with runtime.Gosched, no sleeps",
          "host lookups: 0 waits on the fence"
        ]
      },
      "redTasks": [
        "[1.2] TestLazyFenceWaitsForInFlightViewMaterialization (-race). The window opens before Use returns; RootEnv().Get(`str`) inside the window returns found; after release, Use returns an error containing 'init plugin', <-gDone, installedNames lacks `->`, MaterializeCount() == before + 1 (only str, host), `(-> 1 (+ 2))` is 3. Red through c3 (deterministic: with no fence, U closes before the window opens). Spec: scenario 'Concurrent host materialization survives abort'; tasks 2.4",
        "[1.2] TestLazyFenceSettlesInFlightMaterializationOnSuccess (-race). Same seeding with Init returning nil and no host lookup in the window; the window opens; after release, Use succeeds, installedNames contains `->`, MaterializeCount() == before + 1, ActivePlugins +1. Red through c3 (U returns before the window opens)"
      ],
      "codeTasks": [
        "2.4 runtime/lazy_template.go: lazyOp gains inflight sync.WaitGroup; materializeOne increments under state.mu when opFor(env) is non-nil and defers Done; m.fence(op) sets closed under state.mu, unlocks, then Waits",
        "2.4 runtime/plugin.go: Use and ReloadPlugin call m.fence(op) right after Init/vocabulary return on both paths, before FinishEval; the fence is idempotent"
      ]
    },
    {
      "id": "reload-abort",
      "tasks": [
        "2.5"
      ],
      "summary": "Replaces ReloadPlugin's whole-root snapshot replay with journal abort. Deleted: snapshotRootEnv, restoreRootEnv, rootEnvSnapshot, rollbackPluginUse. Failure is now fence, then FinishEval, then m.endOp(false), then reg.Abort(), with registry, e.bindings, activation and count untouched. callCache.drop runs for the names the op wrote (hygiene; Abort's NameGen bump already invalidates; call_cache.go:85-104 doc updated). snapshotBindings/diff/unionOf stay for the success-path ownership diff, so successful-unload last-writer semantics are unchanged (archived 2026-07-10-review-bugfix-batch/design.md:16-37). Red file runtime/plugin_reload_rollback_test.go (helpers prefixed rr).",
      "contract": {
        "states": [
          "reload-open",
          "reload-aborted",
          "reload-published",
          "unloaded"
        ],
        "transitions": [
          {
            "input": "ReloadPlugin failure: init, vocabulary, settlement or conflict",
            "state": "reload-open",
            "effect": "clear",
            "evidence": "enters reload-aborted: journal abort only; canonical flags and cell identity restored in place (registration.go:81-133); no replay Set clobbering canonical or host writes (plugin.go:133-152 deleted)"
          },
          {
            "input": "host write during a failing reload",
            "state": "reload-open",
            "effect": "forced",
            "evidence": "survives (the replay no longer rewrites the root); spec scenario 'Concurrent host writes survive abort'"
          },
          {
            "input": "ReloadPlugin success",
            "state": "reload-open",
            "effect": "set",
            "evidence": "enters reload-published: e.bindings[name] = the new diff, activate the new version, count unchanged when hadOld"
          },
          {
            "input": "UnloadPlugin(name) after a success",
            "state": "reload-published",
            "effect": "clear",
            "evidence": "enters unloaded: deletes every name in e.bindings[name] through the root, deactivates, decPlugins; last writer wins (plugin.go:230-250 unchanged apart from the removePluginBindings signature)"
          },
          {
            "input": "Use retried after a failed Use",
            "state": "reload-aborted",
            "effect": "set",
            "evidence": "publishes exactly once: ActivePlugins +1, no residue from the failed attempt"
          }
        ],
        "forbidden": [
          "any whole-root snapshot or replay on a failure path",
          "canonical flags lost on reverted cells",
          "UnloadPlugin deleting a name the plugin did not introduce",
          "ActivePlugins moved by a failed operation"
        ],
        "seeding": [
          "reload overwriting a canonical binding: cl.Dialect() engine; RootEnv().SetCanonical(`rr-canon`, GoFunc returning 1); Use(bindingPlugin v1 adding rr-old); ReloadPlugin(rr plugin v2 that Sets rr-canon to GoFunc returning 2 and adds rr-new, then fails)",
          "partly materialized reload: eng1 and eng2, both New(nil, WithBytecode(), WithDialect(clojure.Dialect())) plus Use(stdlib.New()); on eng1 evaluate (+ 1 2) and (str 1); fnStr, _ := eng1.Func(`str`); RootEnv().Set(`count`, GoFunc returning Int 99) (shadow); RootEnv().Delete(`reverse`); m2 := eng2's MaterializeCount(); eng1.ReloadPlugin(rrFailingStdlib{version: `rr-2.0.0`}), whose Init returns an error before registering",
          "concurrent host writes during a reload: rr barrier plugin as in c1's concurrent test, but through ReloadPlugin over an old bindingPlugin",
          "unload pin: plugin A (rr-a) adds shared and a-only; plugin B (rr-b) overwrites shared and adds b-only; UnloadPlugin(rr-a), then UnloadPlugin(rr-b)"
        ],
        "budgets": [
          "failure path: 0 whole-root scans (LocalNames/LocalFuncNames not called on failure); O(journal entries + lazy entries)",
          "success path: 2 LocalNames+LocalFuncNames snapshots, as today"
        ]
      },
      "redTasks": [
        "[1.1] TestReloadPluginFailedInitRestoresCanonicalBinding. After the failed reload, RootEnv().GetCanonical(`rr-canon`) returns the original GoFunc (call returns 1) with canonical true; rr-old resolves; rr-new absent; Registry version 1.0.0; ActivePlugins 1. Red at baseline and through c4 (the replay Sets non-canonical)",
        "[1.1] TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion. After the failed reload on eng1: `(+ 1 2)` is 3 and GetCanonical(`+`) canonical true; fnStr.Call(ctx, Int 1) succeeds; `(count [1])` is 99 (shadow intact); `(reverse [1 2])` errors 'undefined' (deleted stays deleted); `(nth [10 20] 1)` is 20 (untouched deferred); on eng2 `(reverse [1 2])` evaluates and its MaterializeCount() == m2 + 1 (only that touch). Red at baseline and through c4. Spec: scenario 'Partly materialized reload restores deletion state'",
        "[1.2] TestReloadPluginFailedInitKeepsConcurrentHostWrites (-race). The host add/rebind/delete/ReplaceCell/Rebuild and the op-host-op same-name write survive a failed ReloadPlugin; the old plugin's bindings resolve. Red through c4 (the replay deletes host adds). Spec: scenario 'Concurrent host writes survive abort'",
        "[1.3] TestUseRetryAfterFailedInitPublishesOnce. Guard, green at baseline. Use(overwriting plugin that fails), then Use(same name, succeeding): ActivePlugins == 1, Registry().Get(name) returns the second (succeeding) instance, the second Init's values live, UnloadPlugin removes only the names that Init introduced. Spec: requirement 'Successful ... remain unchanged'; tasks 1.3 successful retry",
        "[1.3] TestUnloadPluginKeepsLastWriterOwnership. Guard, green at baseline. After UnloadPlugin(rr-a): shared and a-only are undefined, b-only live; after UnloadPlugin(rr-b): b-only undefined. Spec: requirement 'Successful unload's existing last-writer semantics ... SHALL remain unchanged'"
      ],
      "codeTasks": [
        "2.5 runtime/plugin.go: delete rootEnvSnapshot, snapshotRootEnv, restoreRootEnv, rollbackPluginUse; ReloadPlugin's failure path matches Use's: fence, FinishEval, m.endOp(false), reg.Abort(), callCache.drop for the names the op wrote",
        "2.5 runtime/call_cache.go: drop doc names both callers (removePluginBindings and the failure path)",
        "2.5 keep snapshotBindings, diff, unionOf (success-path ownership)"
      ]
    },
    {
      "id": "validation-floor",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-RED-WAIVER / NO-TESTER-WAIVER: validation only; it runs the suites authored under section 1 and has no observable contract of its own. Runs the focused regex from tasks.md 3.1 with every new name included (all names above start with TestUse, TestReloadPlugin, TestUnloadPlugin or TestLazy, so the regex matches them), the race leg, make test with limits, make lint, openspec validate. It classifies unrelated baseline failures (known flake: json TestDecodeHashMap_Scaling under load) without weakening coverage, and verifies no go.mod/go.sum change and no Plugin interface change.",
      "contract": {
        "states": [
          "floor-run"
        ],
        "transitions": [
          {
            "input": "all coder chunks closed",
            "state": "floor-run",
            "effect": "no-op",
            "evidence": "tasks.md 3.1"
          }
        ],
        "forbidden": [
          "skipping or weakening any section-1 test to pass the floor"
        ],
        "seeding": [
          "none"
        ],
        "budgets": [
          "focused and race legs: -timeout 2m -p 2 -parallel 2 per tasks.md 3.1; full floor: -timeout 10m"
        ]
      },
      "codeTasks": [
        "3.1 run the fullFloor commands and record names, commands and results"
      ]
    },
    {
      "id": "docs-changelog",
      "tasks": [
        "3.2"
      ],
      "summary": "NO-RED-WAIVER / NO-TESTER-WAIVER: documentation and CHANGELOG only; no executable behavior. Updates ARCHITECTURE.md '### Plugin Loading Flow', CONTEXT.md **Registration view**, CHANGELOG.md [Unreleased], and optionally docs/adr/0003-concurrency-model.md. Must make explicit: Init receives a forwarding registration view that is not pointer-identical to RootEnv() (BREAKING behavioral detail); concurrent host writes win over rollback; reads during Init are not isolated; restoration is guaranteed after return; plugin Go objects, external effects and independently retained env references are outside rollback; a new registry conflict error (CodeRegistryConflict) is returned when the host edits the registry mid-operation; host RegisterValue outside a plugin operation binds immediately; the retained-charge gap is closed by plugin-retained-rollback.",
      "contract": {
        "states": [
          "docs-updated"
        ],
        "transitions": [
          {
            "input": "3.1 floor passed",
            "state": "docs-updated",
            "effect": "no-op",
            "evidence": "tasks.md 3.2"
          }
        ],
        "forbidden": [
          "new docs files (update existing ones only)",
          "tool or process references in docs"
        ],
        "seeding": [
          "none"
        ],
        "budgets": [
          "none apply"
        ]
      },
      "codeTasks": [
        "3.2 edit ARCHITECTURE.md, CONTEXT.md, CHANGELOG.md [Unreleased] (Changed: Init env identity, failed Use/ReloadPlugin restore owned state, registry conflict error; Fixed: ActivePlugins after a failed fresh ReloadPlugin settlement, host RegisterValue deferral, Evaluator data race); CHANGELOG Added: Registry.Generation, Registry.PublishIf, CodeRegistryConflict, NewRegistryConflictError; Changed: host RegisterValue outside a plugin operation binds immediately; document the residual NewEnv unlocked eval copy"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "When `Use` or `ReloadPlugin` returns an initialization, vocabulary, or settlement error, it SHALL remove the failed operation's engine-owned changes while preserving concurrent host writes.",
      "tests": [
        "TestUseFailedInitRestoresOverwrittenBindings",
        "TestUseFailedVocabularyRestoresOwnedBindings",
        "TestReloadPluginSettlementFailureKeepsActiveCount",
        "TestUseFailedInitKeepsConcurrentHostWrites"
      ]
    },
    {
      "shall": "Without a competing host write, prior value and function bindings, canonical status, live handles, plugin registry/ownership, active-plugin count, deferred attachments, and deletion state SHALL remain as before the operation.",
      "tests": [
        "TestUseFailedInitRestoresOverwrittenBindings",
        "TestReloadPluginFailedInitRestoresCanonicalBinding",
        "TestUseRegistryPendingDuringInit",
        "TestReloadPluginRegistryKeepsOldDuringInit",
        "TestLazyViewDeleteTombstoneRevertsWithFailedUse",
        "TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion"
      ]
    },
    {
      "shall": "Previously unmaterialized names SHALL remain resolvable after failed reload without forcing eager materialization.",
      "tests": [
        "TestReloadPluginFailedColdStdlibKeepsDeferredNames"
      ]
    },
    {
      "shall": "A competing host write SHALL win over rollback, including a write to the same name between two writes by the failing plugin.",
      "tests": [
        "TestUseFailedInitKeepsConcurrentHostWrites",
        "TestReloadPluginFailedInitKeepsConcurrentHostWrites",
        "TestUseRegistryConflictKeepsHostEntry",
        "TestReloadPluginRegistryConflictKeepsHostRemoval"
      ]
    },
    {
      "shall": "Rollback SHALL preserve unrelated host materialization and deletion.",
      "tests": [
        "TestUseFailedInitKeepsHostLazyMaterialization",
        "TestLazyHostTombstoneDuringFailedUseSurvives",
        "TestLazyFenceWaitsForInFlightViewMaterialization"
      ]
    },
    {
      "shall": "Cache invalidation SHALL prevent stale failed definitions without reversing generations observed by concurrent users.",
      "tests": [
        "TestUseFailedInitRestoresMacro",
        "TestUseFailedInitRestoresOverwrittenBindings"
      ]
    },
    {
      "shall": "The root environment's identity SHALL remain stable.",
      "tests": [
        "TestUseFailedInitRestoresOverwrittenBindings"
      ]
    },
    {
      "shall": "The environment supplied to `Plugin.Init` SHALL retain normal binding, lookup, evaluator, and lexical-scope behavior and SHALL continue to address the same root when retained after successful initialization; its pointer identity need not equal `RootEnv()`.",
      "tests": [
        "TestUseSuppliedEnvStaysLiveAfterSuccess",
        "TestUseFailedInitRevertsAliasWrites",
        "TestUseEvaluatorReadDuringFailedInitIsRaceFree"
      ]
    },
    {
      "shall": "Effects reached through that supplied environment SHALL remain attributable to the plugin operation.",
      "tests": [
        "TestUseFailedInitRevertsAliasWrites",
        "TestLazyViewMaterializationRevertsWithFailedUse",
        "TestLazyFenceSettlesInFlightMaterializationOnSuccess"
      ]
    },
    {
      "shall": "Successful unload's existing last-writer semantics and ordinary `Eval` effects SHALL remain unchanged.",
      "tests": [
        "TestUnloadPluginKeepsLastWriterOwnership",
        "TestUseRetryAfterFailedInitPublishesOnce"
      ]
    },
    {
      "shall": "old values and canonical status SHALL be restored, new names SHALL be absent, the failed plugin SHALL be unregistered, and existing handles SHALL remain usable",
      "tests": [
        "TestUseFailedInitRestoresOverwrittenBindings"
      ]
    },
    {
      "shall": "the original plugin SHALL remain registered and its deferred arithmetic, function, and macro names SHALL remain available",
      "tests": [
        "TestReloadPluginFailedColdStdlibKeepsDeferredNames"
      ]
    },
    {
      "shall": "the original visible and deleted names SHALL retain their behavior, existing handles SHALL remain valid, and sibling engines sharing templates SHALL be unaffected",
      "tests": [
        "TestReloadPluginFailedPartlyMaterializedStdlibRestoresDeletion"
      ]
    },
    {
      "shall": "those host writes SHALL survive rollback, including when the plugin writes the same binding again after the host",
      "tests": [
        "TestUseFailedInitKeepsConcurrentHostWrites",
        "TestReloadPluginFailedInitKeepsConcurrentHostWrites"
      ]
    },
    {
      "shall": "that materialized binding and its per-engine lazy state SHALL remain available without being mistaken for failed plugin output",
      "tests": [
        "TestUseFailedInitKeepsHostLazyMaterialization",
        "TestLazyFenceWaitsForInFlightViewMaterialization"
      ]
    },
    {
      "shall": "only the failed operation's owned changes SHALL be reverted, the error SHALL be returned, and active-plugin counts SHALL remain correct",
      "tests": [
        "TestUseFailedVocabularyRestoresOwnedBindings"
      ]
    },
    {
      "shall": "resulting root writes SHALL remain part of the same plugin operation and SHALL obey its rollback conflict policy",
      "tests": [
        "TestUseFailedInitRevertsAliasWrites"
      ]
    },
    {
      "shall": "later calls SHALL observe root rebindings, and subsequent successful unload/reload SHALL preserve existing ownership semantics",
      "tests": [
        "TestUseSuppliedEnvStaysLiveAfterSuccess",
        "TestUnloadPluginKeepsLastWriterOwnership"
      ]
    }
  ],
  "testHarness": [
    "mockPlugin — runtime/plugin_test.go:type mockPlugin struct { — name/version/lifecycle/initErr, counts Init calls; no env writes",
    "failingBindingPlugin — runtime/plugin_test.go:type failingBindingPlugin struct { — embeds bindingPlugin, performs its writes then returns err",
    "bindingPlugin — runtime/plugin_test.go:type bindingPlugin struct { — Init env.Set GoFuncs for names (value cell) and env.SetFunc for funcs (function cell); version 1.0.0",
    "installedNames — runtime/lazy_materialize_test.go:func installedNames(impl *engineImpl) []string { — snapshot of lazyMaterializer.state.installed under state.mu",
    "sharedTemplatePlugin — runtime/lazy_materialize_test.go:type sharedTemplatePlugin struct { — Name()==\"\" stdlib-shaped plugin, RegisterValue shared-template-fn→42, shared *int64 init counter, fail flag errors before registering",
    "errSharedTemplateInit — runtime/lazy_materialize_test.go:var errSharedTemplateInit = errors.New(\"shared template init failed\") — sentinel for sharedTemplatePlugin failure",
    "recordingMeter — runtime/meter_test.go:type recordingMeter struct { — Meter recording lease/charge/release, denyAfter, chargeErr to force retained-charge (settlement) failure; reset/snapshot",
    "setupPlugin — runtime/meter_test.go:type setupPlugin struct{} — Init sets setup/value=1",
    "evaluatorSetupPlugin — runtime/meter_test.go:type evaluatorSetupPlugin struct { — Init evaluates (def setup/evaluator-value 42) via env.Evaluator().Eval (evaluator reentry)",
    "rebuildDuringChargeMeter — runtime/meter_test.go:type rebuildDuringChargeMeter struct { — meter whose ChargeRetained deletes x and Rebuilds the env",
    "newBytecodeStdlibEngine — runtime/bootstrap_isolation_test.go:func newBytecodeStdlibEngine(t *testing.T) Engine { — bytecode clojure engine with stdlib Used",
    "stubPlugin/newStub — core/plugin_test.go:type stubPlugin struct { — no-op plugin for Registry unit tests",
    "requireRegistrationRefused/beginViewRegistration — core/registration_view_test.go:func requireRegistrationRefused(t *testing.T, err error, what string) { — CodeRegistrationActive assertion; begin helper",
    "journalTry/journalWrite/journalEntry/journalNoEntry/journalMapCell/beginJournal — core/registration_journal_test.go:func beginJournal(t *testing.T, root *Env) *Registration { — panic-safe write wrappers, reg.entries inspection, raw map cell access",
    "abortTry/abortMapCell/abortBegin/abortSeed/abortWantValue/abortWantAbsent — core/registration_abort_test.go:func abortBegin(t *testing.T, root *Env) *Registration { — abort-ownership assertions on root Get",
    "counterTry/counterEntry/counterBegin/counterReleaseMeter/counterWantUnchanged/counterWantBumped — core/registration_counters_test.go:func counterWantBumped(t *testing.T, root *Env, gen uint64, epoch int, what string) { — NameGen/MacroEpoch bump assertions, release-recording meter",
    "aliasTry/aliasEntry/aliasBegin/aliasEval/aliasEvalRoot/aliasLazyLayer/aliasConfigFields/aliasConfigureThroughView — core/registration_alias_test.go:func aliasEvalRoot(t *testing.T) *Env { — root with NewEvaluator + x=1, evaluator-reentry eval through view, stub LazyLayer, config-field (evaluator/meter/lazy) round-trip through view"
  ],
  "floor": "make lint && make test GOTESTFLAGS='-timeout 10m -p 2 -parallel 2' && go test -race -timeout 10m -p 2 -parallel 2 ./runtime -run '^(TestUseRegistryPendingDuringInit|TestReloadPluginRegistryKeepsOldDuringInit|TestUseFailedInitKeepsConcurrentHostWrites|TestUseFailedInitKeepsHostLazyMaterialization|TestUseFailedInitRevertsAliasWrites|TestUseEvaluatorReadDuringFailedInitIsRaceFree|TestUseRegistryConflictKeepsHostEntry|TestReloadPluginRegistryConflictKeepsHostRemoval|TestLazyHostTombstoneDuringFailedUseSurvives|TestLazyFenceWaitsForInFlightViewMaterialization|TestLazyFenceSettlesInFlightMaterializationOnSuccess|TestReloadPluginFailedInitKeepsConcurrentHostWrites)$' && go test -race -timeout 10m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration|Registry)'",
  "risks": [
    "Fence deadlock: the wait holds e.mu but no env or lazy lock; materialization never takes e.mu (lazy_template.go:260-265) and Add happens only under state.mu while !closed. Mitigation: forbidden rows in materialization-fence; both fence tests under -race with a 2s guard; the coder greps materializeOne/materializeBootstrap/DefineBootstrap for any e.mu path before landing.",
    "Host lazy materialization misattributed: identity is the view pointer compared under state.mu against the open op, never loadingPlugin; unattributed materialization always writes through m.engine.rootEnv, so the core journal and the lazy journal agree. Covered by TestUseFailedInitKeepsHostLazyMaterialization, TestLazyViewMaterializationRevertsWithFailedUse and the fence window host lookup.",
    "Evaluator race: fixed in Evaluator() (owner RLock). Residual, not closed: NewEnv copies parent.eval unlocked (core/env.go:190), so a host RootEnv().Child() racing an op's view.SetEvaluator or its Abort restore is still a data race. It is reachable only through direct RootEnv use during Init (eng.Eval, LoadScope and Bind block on e.mu). Locking there would put the root RWMutex on every root-child creation. Mitigation: document it in 3.2 and track a follow-up (atomic eval with an Env size re-budget).",
    "Registry conflict semantics: generations, not Plugin interface equality (non-comparable dynamic types panic). Use reads Generation before BeginRegistration and ReloadPlugin reads it before Get, so a host edit racing the observation surfaces as a conflict (fail-closed). A host Register-then-Unregister during Use leaves generation 0 and the publish succeeds, since no host entry exists to preserve.",
    "Lookup-path perf: non-view Get/Cell/NameGen paths are unchanged (+0 instructions); the lazy miss path adds 1 pointer compare inside the existing state.mu section; Evaluator() adds 1 RLock pair off the eval hot path; Env stays 208 bytes. Latency cells cannot be judged locally (perfgate memory), bytes/allocs are locally exact: the goldset bytes axis must not move.",
    "Behavior change: a host RegisterValue outside a plugin operation now binds immediately instead of deferring into an inactive key{fp, , }. Recorded in the CHANGELOG (3.2).",
    "Carried, not owned: the success-path ownership diff (snapshotBindings) still counts host adds and materializations reached during a successful Init as plugin-owned, so a later UnloadPlugin deletes them. Pre-existing last-writer semantics, pinned unchanged by the spec; a follow-up can switch to journal-derived ownership.",
    "Transient reads during rollback: endOp(false) runs before reg.Abort() (state.mu and root.mu never nested), so a host lookup between them can briefly miss a name the op had deleted. Covered by the post-return guarantee in the spec ('Reads ... need not be isolated'); documented in 3.2.",
    "Archived registration-journal design ruling ('lazy layer lookups receive the root', archive design.md:49 and :384) is superseded by view attribution. No core test pins it (grep of core/registration_*_test.go), and core-engine spec.md:1242 requires view-reached writes to be attributed, so this is consistent. The coder must keep TestRegistration_AbortRestoresConfiguration green via the view.lazyLayer sync in setLazyLayer and Abort.",
    "Retained charges for cells removed by abort still leak until plugin-retained-rollback lands. That change's plan will rot once this lands (sibling-change plan rot); re-review it against the final Use/ReloadPlugin order: fence, FinishEval, PublishIf, Complete | endOp(false), Abort.",
    "Serial chain in package runtime: every later chunk's red tests are skipped in earlier verifies (verifyCommands). A narrow -run can report pass on a red package, so each verify runs the whole package set with -skip, never -run alone.",
    "golangci-lint with absolute paths from a foreign cwd returns exit 7 with 'No issues found': run it as env -C <worktree>.",
    "Known unrelated flake: json TestDecodeHashMap_Scaling under load or -race; classify it, never skip it, in 3.1."
  ],
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
