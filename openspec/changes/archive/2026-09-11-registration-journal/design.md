## Context

`plugin-binding-rollback` must revert a failed plugin operation's root writes without overwriting concurrent host writes. Root `Env` mutations carry no provenance today, so runtime can only diff names (`snapshotBindings`) or replay a whole-root snapshot (`restoreRootEnv`). Shared-root identity and live `Cell` identity are observable through handles, closures, and VM caches, which rules out a copied environment or installing old maps.

Facts at `396afc06` that shape the design:

- Every binding write goes through an `Env` method under the owner's `mu`; `Cell` has no value setter. The only direct private-field access outside `core/env.go` is the read-only walk in `lookupBoundMacro` (`core/eval.go`) and retained-field settlement in `settleRetained` (`core/metering.go`), whose `pending.env` is always the owning root.
- `core/vm` uses only exported `Env` methods. A VM site cache hits when `entry.env == env && entry.gen == env.NameGen() && entry.ver == entry.cell.Version()`; otherwise it re-reads through `ReadCell`. The bytecode chunk cache keys on the root's `MacroEpoch`, not on an env pointer.
- `SetFunc*` never bumps `NameGen`; `setBoth` bumps it for the value cell only; `ReplaceCell` installs a new cell. `Delete` tombstones in place; only `Rebuild` removes map entries and releases retained capacity (ADR 0012).
- `Plugin.Init` takes a `*core.Env`, so the view must be one.

## Goals / Non-Goals

**Goals:** attribute root writes to one operation; on abort revert only entries that operation still owns; keep root and cell identity; cost one absent-registration branch on paths that can reach a root when no operation is active.

**Non-Goals:** read isolation during an operation, a general transaction framework, goroutine-identity inference, retained-capacity ownership in the journal (`plugin-retained-rollback`), runtime plugin/lazy wiring and lazy-layer attribution (`plugin-binding-rollback`).

## Decisions

### API (fixed names)

| Identifier | Role |
| --- | --- |
| `core/registration.go` | New file: `Registration`, journal types, begin/complete/abort, journal hooks |
| `type Registration struct` | Opaque handle for one operation on one root; unexported `root`, `view *Env`, `entries map[registrationKey]*registrationEntry`, config before-images |
| `func (e *Env) BeginRegistration() (*Registration, error)` | Starts an operation on `e` (a view resolves to its root). Refused while the root has an active registration |
| `func (r *Registration) Env() *Env` | The forwarding view; same pointer for the registration's lifetime, never the root |
| `func (r *Registration) Complete()` | Keeps every op write, ends attribution; no-op once finished |
| `func (r *Registration) Abort()` | Reverts owned entries in place, ends attribution; no-op once finished; no return value, no meter or lazy-layer calls |
| `const CodeRegistrationActive = "RegistrationActiveError"` | `core/error.go`; Code of the nested/concurrent refusal |
| `func NewRegistrationActiveError() *LispicoError` | Message `environment already has an active registration` |
| `Env.reg atomic.Pointer[Registration]` | On a view: its registration, set before the view escapes, never changed. On a root: the active registration, stored/cleared under `root.mu`. Nil elsewhere |
| `type registrationKey struct{ name string; fn bool }` | Journal key; `fn` selects the function-cell namespace |
| `type registrationEntry struct{ prior *Cell; v Value; canonical bool; last *Cell; lastVer uint64 }` | Before-image (`prior == nil`: name absent from the map) and the op's latest write |
| `owner()`, `viewReg()`, `active(r)`, `lazy()` | Unexported routing helpers: canonical owner; the registration of a view; `r` only while `root.reg == r`; field-only lazy-layer read |
| `beforeWrite(key, cur)`, `afterWrite(key, cell)` | Journal hooks, called under `root.mu` |

One registration per root at a time. A second `BeginRegistration` on the root or its view returns `*LispicoError` with `Code == CodeRegistrationActive`; a plugin `Init` that re-enters `Use` on the same engine sees that error and `plugin-binding-rollback` surfaces it.

### Forwarding view

A view is `&Env{parent: root, eval: root.eval, maxRetainedBytes: root.maxRetainedBytes, maxRetainedSlots: root.maxRetainedSlots}` with `reg` set, built under `root.mu`, with nil `vars`/`funcs`. It is a forwarding scope, not a lexical child: no bindings ever live in it.

- **Walk-inherited reads, no code change:** `Get`, `GetCanonical`, `GetFunc`, `GetFuncCanonical`, `GetMaterialized*Canonical`, `Cell`, `FuncCell`, and the `lookupBoundMacro` walk reach the root through `parent`, because the view's maps are nil. Non-view envs pay nothing on these paths.
- **Forwarded reads through `owner()`:** `HasLive*`, `ReadCell*`, `CellLocal`, `FuncCellLocal`, `NameGen`, `MacroEpoch`, `Evaluator`, `LazyLayer`, `RetainedMeter`, `RetainedUsage`, `VarNames`, `FuncNames`, `LocalNames`, `LocalFuncNames`. Each gains one atomic pointer load and one nil branch on every env; `NameGen` is on the VM site-hit path.
- **Forwarded writes carrying `r`:** every `Set*`, `SetCanonical*`, `SetFunc*`, `SetFuncCanonical*`, `SetBoth*`, `ReplaceCell*`, `Delete`, `RegisterValue*`, merge target, `SetEvaluator`, `SetRetainedMeter`, `SetLazyLayer`. `Rebuild` and `BumpMacroEpoch` forward unattributed.
- **Owner-aware:** `Find` on a view returns the view when the root owns the name, never the raw root, so `set!` (`evalSet`, `OpSetLexical`) stays attributed. An owner above the root is returned raw and is outside the guarantee; the runtime root has no parent.
- **Lexical:** `Child`/`ChildVariadic` of a view are ordinary `NewEnv(view)` children. Their local writes stay local and unjournaled; their `set!` of a root name reaches the view. Closures (`Lambda.Env`, `Macro.Env`, VM `NewClosure`) and VM frames capture the view pointer, so their writes stay attributed until the registration finishes and are unattributed after.
- **Lazy layer:** lookups, `ForceAll`, and `TombstoneForDelete` receive the root, so materialization reached from a view read is an unattributed host write. `RegisterValue`/`RegisterSource` pass the view, so an eager fallback write is attributed. Attributing materialization belongs to `plugin-binding-rollback`.
- **Merge:** `mergeInto` resolves source and target through `owner()`, takes `r` from `target.viewReg()`, and keeps today's lock order (resolved source `RLock`, then resolved target `Lock`). A merge whose source and target resolve to the same env returns `EvalError` `merge source and target are the same environment` before taking any lock; today `root.MergeInto(root)` self-deadlocks.

A lexical child followed by `MergeInto` is rejected because it changes deletion, capture, and owner-lookup semantics; replaying snapshots is rejected because it cannot identify concurrent writers.

`Env` stays 208 bytes (`TestEnv_Size`): the new `reg` field is paid for by dropping `cell0Used`, which `localCell` replaces with `e.cell0.version.Load() != 0`. Every `localCell` caller bumps the returned cell's version before the next `localCell` under the same lock; a WHY comment at `localCell` records that invariant.

### Journal and abort under the owner lock

Under `root.mu`, each mutator computes `j := root.active(r)`, which is `r` only while `root.reg == r`. After capacity reservation succeeds and immediately before mutating, it calls `j.beforeWrite(key, cur)`, then `j.afterWrite(key, cell)` after. Raw writes never touch the journal. A write refused by capacity, or a `Delete` of an absent or non-canonical tombstoned name, mutates nothing and records nothing. The journal holds at most one entry per distinct `(namespace, name)` written through the view.

An entry is **owned** iff the map's current cell for the key `== entry.last` and its `Version() == entry.lastVer`. Every cell write bumps the version under the owner lock, so any foreign write, including one with an equal value, a delete, or a delete-and-recreate, makes the entry foreign. A later op write to a foreign entry rebases its before-image to the current state (cell, value, canonical; nil when absent) before applying.

| Input class | Effect of `Abort` |
| --- | --- |
| op overwrite of a live name, either namespace, canonical or not | owned: the same cell regains its prior value and canonical flag, stays in the map, version +1 |
| op add of an absent name | owned: the op cell is tombstoned in place (`v = nil`, canonical false, version +1) and left in the map holding its capacity until the next `Rebuild` after the operation closes (ADR 0012: `Rebuild` is the only release path) |
| op revive of a tombstoned name | owned: the same cell is re-tombstoned with its prior canonical flag, version +1 |
| op delete of a live name | owned per namespace: the same cell regains its prior value and canonical flag; the layer's `TombstoneForDelete` record is not reverted |
| op `ReplaceCell` over an existing cell | owned: `entry.prior` is reinstalled with its before-image, version +1; the op cell is tombstoned, version +1, so holders re-resolve |
| op `SetBoth` | two independent entries, one per namespace |
| op merge into the view | one entry per committed cell, per the rows above; retained fields copied from the source cell are not restored (`plugin-retained-rollback`) |
| op write, `Rebuild`, abort | while a registration is active `Rebuild` pins every tombstoned cell that is some entry's `last`, still at that write's version (stays in the map, counted as `cell.retainedBytes` + 1 slot, not released, `rebuilt` untouched); abort then proceeds per the rows above |
| foreign rebind / add / delete / delete+recreate / `ReplaceCell` after the op write | not owned: left untouched |
| op → host → op → abort | the second op write rebased the before-image to the host state; abort restores the host state |
| config through the view (`SetEvaluator`, `SetRetainedMeter`, `SetLazyLayer`) | prior recorded on the first op write; a later raw setter marks the field foreign under `root.mu`; abort restores only unmarked fields |
| `BumpMacroEpoch` through the view | forwarded to the root, never journaled or reverted |
| `Complete` | root's `reg` cleared, entries dropped, every op write kept, no counter change |
| `Complete`/`Abort` on a finished registration | no-op; a later registration on the same root is unaffected |

`Abort` holds `root.mu` once, is O(entries) with 0 allocations, creates no cells, and calls no meter, lazy layer, or `BumpMacroEpoch`. When it restores anything — at least one entry or one configuration field — it bumps `NameGen` and `MacroEpoch` once each, directly under the held lock; restoring nothing leaves both unchanged. Name generations, cell versions, and macro epochs stay monotone: restored cells get a version bump and historical counters are never restored. That invalidates every cache that could hold a reverted definition: VM site entries (NameGen and cell version), runtime call handles (cell version), and the bytecode chunk cache (MacroEpoch).

### Verification

Existing-service-strict, regression-first. Tests live in new `core/registration_*_test.go` files, one per chunk so parallel red stages never share a file, each declaring its own file-local helpers, plus one VM test in `core/vm/vm_test.go`. All names match task 3.1's `Test(Env|Merge|Registration)`. Provenance interleavings are sequenced on one goroutine by alternating view and raw-root writes; `TestRegistration_ConcurrentHostWritesRace` repeats them under the race detector. Journal state is read white-box (`reg.entries[registrationKey{…}]`) through a helper that fails on a missing key before any field access. Until a chunk lands, a view mutation can panic on the view's nil map, so red tests route view mutations through a helper that recovers and reports with `t.Fatalf`.

### Ownership checklist

Task 0.1's audit at `396afc06`. Every row is covered by the chunk named in parentheses.

- **Walk-inherited through `view.parent = root`, no change:** `Get`, `GetCanonical`, `GetFunc`, `GetFuncCanonical`, `GetMaterializedCanonical`, `GetMaterializedFuncCanonical`, `Cell`, `FuncCell`; `lookupBoundMacro` in `core/eval.go`.
- **Forwarded reads via `owner()` (c2):** `HasLive`, `HasLiveFunc`, `ReadCell`, `ReadCellSnapshot`, `CellLocal`, `FuncCellLocal`, `NameGen`, `MacroEpoch`, `Evaluator`, `LazyLayer`, `RetainedMeter`, `RetainedUsage`, `VarNames`, `FuncNames`, `LocalNames`, `LocalFuncNames`.
- **Forwarded writes carrying `r` (c3):** `Set`/`SetWithContext`, `SetCanonical`/`SetCanonicalWithContext`, `SetFunc`/`SetFuncWithContext`, `SetFuncCanonical`/`SetFuncCanonicalWithContext`, `SetBoth*` via `setBoth`, `ReplaceCell`/`ReplaceCellWithContext`, `Delete`, `RegisterValue`/`RegisterValueWithContext`, `RegisterSource`. Owner-only internals: `localCell`, `localFuncCell`, `prepareFreshRetained`, `reserveRetainedBindings`, `activeRetainedMeter`, `recordFreshRetained` (stores the owner in `pendingCellAlloc.env`).
- **Owner-aware, alias, and config (c5):** `Find`; `Child`, `ChildVariadic`, `NewEnv` copying `parent.eval` and retained limits; `forkCells` (receiver is always a loop child); `MergeInto`, `MergeIntoCanonical`, `mergeInto`, `mergePlan.add`, `applyMergePlan`; `Rebuild` and `BumpMacroEpoch` forwarding; `SetEvaluator`, `SetRetainedMeter`, `SetLazyLayer`.
- **Counters and pinning (c6):** `Rebuild` pinning of entry-held tombstones; `NameGen`/`MacroEpoch` bumps in `Abort`.
- **Direct private access outside `core/env.go`:** `lookupBoundMacro` (read-only walk; safe through nil view maps); `settleRetained` (`pending.env.mu`, retained cell fields; `pending.env` is always the owning root); `pendingCellAlloc.env`. `core/vm` and `core/compiler` have none. Test-only access: `core/env_test.go` (`env.funcs[...]`), `sumLiveRetainedBytes` in `core/env_merge_test.go`, `core/meter_settle_test.go`.
- **Bounded command:** `go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)'`; `Makefile` sets `GOTESTFLAGS ?= -timeout 2m` and has no race target, so the race run uses the raw command with `-race -timeout 5m`.

## Risks / Trade-offs

- A missed forwarding path would write into the view → the view's maps are nil, so a miss panics in tests instead of silently losing attribution; `TestRegistration_ViewHoldsNoBindings` and `…AfterMerge` drive every mutator, and the checklist above is the review list.
- `NameGen` on the VM site-hit path gains one atomic load and one branch → latency can't be judged on the local box (the perf gate's latency cells false-fail there); `reg` sits next to `newNameGen`, the bytes axis is unchanged because `Env` stays 208 bytes, and the release-runner gate decides latency.
- Dropping `cell0Used` relies on the `localCell` version-bump invariant → WHY comment, `TestEnv_Size`, and the existing cell-version tests.
- Aborted additions keep their capacity until the next `Rebuild`, and `RetainedUsage` still counts them → consistent with ADR 0012; `plugin-retained-rollback` owns the release-path question.
- Merge copies retained fields from the source cell and abort does not restore them → retained drift during a failed op is `plugin-retained-rollback` scope.
- An op that deletes a deferred, never-materialized stdlib name keeps it deleted after abort, because the layer's tombstone is runtime state → `plugin-binding-rollback` owns lazy attribution.
- Children created from a view snapshot the view's `eval` and limits → same as any existing child scope; a later `root.SetEvaluator` is invisible to them.
- `SetLazyLayer` now takes `e.mu` → its only caller runs at engine construction with no env lock held.
- A self-merge now returns `EvalError` instead of deadlocking → raw-path change, recorded in the CHANGELOG.
- Sibling plans consume these names → re-review `plugin-binding-rollback` and `plugin-retained-rollback` once this lands. No rename is expected, but two additive changes are: `plugin-binding-rollback` wants registration identity on lookup and materialization reached from the view, while this change hands the root to `LookupAndMaterialize` and exposes no `Env`-to-`Registration` accessor, so that change alters the miss-path env (behavioral) and adds an accessor (additive). `plugin-retained-rollback`'s abort-time pending-allocation settlement and after-unlock meter calls fit behind `Abort()` with no return value only if the settlement lives in runtime or in unexported core hooks.
- The ADR 0012 `Rebuild` text gains the pin rule for an active registration (c7); `plugin-retained-rollback` amends the same ADR for its release path.

## Migration Plan

No runtime caller until `plugin-binding-rollback` lands; environments without an active registration keep their binding behavior. No stored data or dependency migration. Reverting removes an unused seam.

## Implementation plan

Base `396afc06`, tier **heavy**, mode **existing-service-strict**. Seven chunks, serial in package `core` (each names `prev` and `sharedPkg: core`). Every red chunk sets `redAfter: c1-inert-api`: its tests name only symbols c1 declares, so red stages for c2–c6 can run while earlier coders work, each in its own test file.

Rules for any executor: work in the change worktree, never the primary checkout; a sealed red test is read-only to the coder; commits follow Conventional Commits (`feat|fix|refactor|perf|docs|test|build|ci|chore|revert(scope): description`) with no tool or process attribution; terse output; native file tools first.

### c1-inert-api — tasks 0.1, 2.1

- **Order:** first. Seam `view-forwarding`. Coder `go-coder`.
- **Split:** red —; code 2.1.
- **No red stage:** FIELD-FIRST — declares every production symbol the red tests name, with no behavior; closes through the tester against `verify`.
- **Closes:** FIELD-FIRST: every red test of c2..c6 compiles and fails on an assertion against this chunk; task 0.1 has no site: the ownership checklist is recorded in design.md and the audit seam; the orchestrator ticks 0.1 in tasks.md when c1 closes, not the c1 coder.
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `core/registration.go` | new | new file | FIELD-FIRST inert members only: type Registration struct{ root, view *Env; entries map[registrationKey]*registrationEntry }, type registrationKey struct{ name string; fn bool }, type registrationEntry struct{ prior *Cell; v Value; canonical bool; last *Cell; lastVer uint64 }; func (e *Env) BeginRegistration() (*Registration, error) returning &Registration{root: e, view: e}, nil; func (r *Registration) Env() *Env returning r.view; Complete() and Abort() with empty bodies |
  | `core/error.go` | CodeResourceLimit | `const CodeResourceLimit = "ResourceLimitError"` | add const CodeRegistrationActive = "RegistrationActiveError" and func NewRegistrationActiveError() *LispicoError with Message "environment already has an active registration", next to CodeConcurrentUse |
  | `core/env.go` | Env | `type Env struct {` | add reg atomic.Pointer[Registration] next to newNameGen; remove cell0Used (TestEnv_Size stays 208) |
  | `core/env.go` | localCell | `func (e *Env) localCell(name string) *Cell {` | replace cell0Used with e.cell0.version.Load() != 0; WHY comment: every localCell caller bumps the returned cell version before the next localCell under the same lock |

- **verify:** `go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm && golangci-lint run ./core/...`

### c2 — tasks 1.1, 2.1

- **Order:** after `c1-inert-api`, sharedPkg `core`; red stage after `c1-inert-api`. Seam `view-forwarding`. Coder `go-coder`.
- **Split:** red 1.1; code 2.1.
- **Red tests:** `TestRegistration_ViewForwardsReadsAndKeepsRootIdentity`, `TestRegistration_NestedBeginRefused`, `TestRegistration_CompleteAndAbortAreIdempotent`, `TestRegistration_ViewGetZeroAllocs`.
- **Closes:** view-forwarding: root.BeginRegistration() [idle]; view-forwarding: root.BeginRegistration() [active]; view-forwarding: view.BeginRegistration() [active]; view-forwarding: view.BeginRegistration() [finished]; view-forwarding: r.Complete() [active]; view-forwarding: r.Abort() [active] — lifecycle leg only (clears root.reg); the restore leg closes in c4; view-forwarding: r.Complete() or r.Abort() [finished]; view-forwarding: r.Complete() or r.Abort() [active-other]; view-forwarding: view read: Get, GetCanonical, GetFunc, GetFuncCanonical, Cell, FuncCell, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, LocalNames, VarNames, NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, RetainedUsage, Evaluator, LazyLayer; view-forwarding: raw root rebind, then a view read [finished].
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `core/registration_view_test.go` | new | new file | red tests: TestRegistration_ViewForwardsReadsAndKeepsRootIdentity, TestRegistration_NestedBeginRefused, TestRegistration_CompleteAndAbortAreIdempotent, TestRegistration_ViewGetZeroAllocs |
  | `core/env.go` | HasLive | `func (e *Env) HasLive(name string) bool {` | forward through owner(): Reads e.vars directly: forward to owner. |
  | `core/env.go` | HasLiveFunc | `func (e *Env) HasLiveFunc(name string) bool {` | forward through owner(): Reads e.funcs directly: forward to owner. |
  | `core/env.go` | CellLocal | `func (e *Env) CellLocal(name string) (*Cell, bool) {` | forward through owner(); the forwarded call reaches the layer with env = root, never the view |
  | `core/env.go` | FuncCellLocal | `func (e *Env) FuncCellLocal(name string) (*Cell, bool) {` | forward through owner(); the forwarded call reaches the layer with env = root, never the view |
  | `core/env.go` | ReadCell | `func (e *Env) ReadCell(c *Cell) (Value, bool, bool) {` | forward through owner(): Must take the canonical owner's RLock, not the view's mu (VM calls entry.env.ReadCell where entry.env may be a view). |
  | `core/env.go` | ReadCellSnapshot | `func (e *Env) ReadCellSnapshot(` | forward through owner(): Same: owner lock. |
  | `core/env.go` | Evaluator | `func (e *Env) Evaluator() Evaluator {` | forward through owner(): Unlocked read of e.eval: forward to owner. |
  | `core/env.go` | LazyLayer | `func (e *Env) LazyLayer() LazyLayer {` | forward through owner(): Forward to owner's atomic lazyLayer. |
  | `core/env.go` | RetainedMeter | `func (e *Env) RetainedMeter() any {` | forward through owner(): Forward to owner. |
  | `core/env.go` | NameGen | `func (e *Env) NameGen() uint64 { return e.newNameGen.Load() }` | forward through owner(): Return owner counter (VM site + runtime callCache compare it). |
  | `core/env.go` | MacroEpoch | `func (e *Env) MacroEpoch() int {` | forward through owner(): Return owner counter. |
  | `core/env.go` | RetainedUsage | `func (e *Env) RetainedUsage() (bytes, slots int64) {` | forward through owner(): Return owner counters. |
  | `core/env.go` | VarNames | `func (e *Env) VarNames() []string {` | forward through owner(); the forwarded call reaches the layer with env = root, never the view |
  | `core/env.go` | LocalNames | `func (e *Env) LocalNames() []string {` | forward through owner(): Reads e.vars: forward. |
  | `core/env.go` | FuncNames | `func (e *Env) FuncNames() []string {` | forward through owner(); the forwarded call reaches the layer with env = root, never the view |
  | `core/env.go` | LocalFuncNames | `func (e *Env) LocalFuncNames() []string {` | forward through owner(): Reads e.funcs: forward. |
  | `core/registration.go` | BeginRegistration | new file | real body: root := e.owner(); lock root.mu; refuse with NewRegistrationActiveError() when root.reg.Load() != nil; view := &Env{parent: root, eval: root.eval, maxRetainedBytes: root.maxRetainedBytes, maxRetainedSlots: root.maxRetainedSlots}; view.reg.Store(r); root.reg.Store(r); unlock. Env() returns the view. Complete: under root.mu, if root.reg.Load() == r clear it and drop entries. Abort in this chunk is lifecycle only (clear root.reg, drop entries); restore lands in c4 |
  | `core/registration.go` | owner/viewReg/lazy | new file | unexported helpers: owner() is r.root when reg is non-nil, else e; viewReg() is reg when e is a view (r.view == e), else nil; lazy() reads the lazyLayer field only |
  | `core/env.go` | Get | `func (e *Env) Get(name string) (Value, bool) {` | switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root |
  | `core/env.go` | GetCanonical | `func (e *Env) GetCanonical(name string) (Value, bool, bool) {` | switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root |
  | `core/env.go` | GetFunc | `func (e *Env) GetFunc(name string) (Value, bool) {` | switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root |
  | `core/env.go` | GetFuncCanonical | `func (e *Env) GetFuncCanonical(name string) (Value, bool, bool) {` | switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root |
  | `core/env.go` | Find | `func (e *Env) Find(name string) (*Env, bool) {` | switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root |

- **redRun:** `go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_ViewForwardsReadsAndKeepsRootIdentity|TestRegistration_NestedBeginRefused|TestRegistration_CompleteAndAbortAreIdempotent|TestRegistration_ViewGetZeroAllocs)$'`
- **verify:** `go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_ViewHoldsNoBindings|TestRegistration_ViewWriteRecordsBeforeImage|TestRegistration_HostEqualValueRebindIsUnowned|TestRegistration_RegisterValueThroughViewIsAttributed|TestRegistration_RawRebindDuringOperationZeroAllocs|TestRegistration_JournalBoundedByDistinctNames|TestRegistration_AbortRestoresOverwrittenBindings|TestRegistration_AbortRemovesAddedNames|TestRegistration_AbortRestoresCanonicalStatus|TestRegistration_AbortRestoresDeletedBinding|TestRegistration_AbortRestoresReplacedCell|TestRegistration_HostEqualValueRebindSurvivesAbort|TestRegistration_HostRebindAfterOperationSurvivesAbort|TestRegistration_HostAddSurvivesAbort|TestRegistration_HostDeleteAfterOperationSurvivesAbort|TestRegistration_HostDeleteRecreateSurvivesAbort|TestRegistration_HostReplaceCellSurvivesAbort|TestRegistration_OperationHostOperationAbortKeepsHostState|TestRegistration_CompletedViewForwardsUnattributed|TestRegistration_AbortedViewForwardsUnattributed|TestRegistration_ConcurrentHostWritesRace|TestRegistration_VMSiteDropsRevertedDefinition|TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration|TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/...`

### c3 — tasks 1.1, 2.2

- **Order:** after `c2`, sharedPkg `core`; red stage after `c1-inert-api`. Seam `write-routing`. Coder `go-coder`.
- **Split:** red 1.1; code 2.2.
- **Red tests:** `TestRegistration_ViewHoldsNoBindings`, `TestRegistration_ViewWriteRecordsBeforeImage`, `TestRegistration_HostEqualValueRebindIsUnowned`, `TestRegistration_RegisterValueThroughViewIsAttributed`, `TestRegistration_RawRebindDuringOperationZeroAllocs`, `TestRegistration_JournalBoundedByDistinctNames`.
- **Closes:** write-routing: view Set/SetWithContext; write-routing: view SetCanonical/SetCanonicalWithContext; write-routing: view SetFunc*/SetFuncCanonical*; write-routing: view SetBoth*/SetBothCanonical*; write-routing: view ReplaceCell/ReplaceCellWithContext; write-routing: view Delete [live-prior\|live-op]; write-routing: view Delete [absent\|tomb]; write-routing: view RegisterValue with no lazy layer; write-routing: view write refused by capacity (ResourceLimitError); write-routing: view write repeated on the same key with no foreign write between; write-routing: raw root write (any mutator) on a journaled key — journal-state leg (lastVer mismatch); the abort effect closes in c4; write-routing: raw root write with a value equal to the op's — journal-state leg; the abort effect closes in c4; write-routing: view write after finish or under another registration.
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `core/registration_journal_test.go` | new | new file | red tests: TestRegistration_ViewHoldsNoBindings, TestRegistration_ViewWriteRecordsBeforeImage, TestRegistration_HostEqualValueRebindIsUnowned, TestRegistration_RegisterValueThroughViewIsAttributed, TestRegistration_RawRebindDuringOperationZeroAllocs, TestRegistration_JournalBoundedByDistinctNames |
  | `core/env.go` | setBoth | `func (e *Env) setBoth(ctx context.Context, name string, val Value, canonical bool) error {` | thread r; under root.mu compute j := root.active(r); after prepareFreshRetained succeeds call j.beforeWrite / j.afterWrite for the value key and the function key; entry creation only, no rebase (c4). Covers the SetBoth* wrappers |
  | `core/env.go` | SetWithContext | `func (e *Env) SetWithContext(ctx context.Context, name string, val Value) error {` | Route to owner under owner lock; journal value-cell before-image (new name => prior absent; tombstoned => revive) when view-originated. Set wraps it. |
  | `core/env.go` | SetCanonicalWithContext | `func (e *Env) SetCanonicalWithContext(` | Same routing/journaling; canonical marker in before-image. SetCanonical wraps it. |
  | `core/env.go` | SetFuncWithContext | `func (e *Env) SetFuncWithContext(` | Same for funcs namespace; note it never bumps newNameGen. SetFunc wraps it. |
  | `core/env.go` | SetFuncCanonicalWithContext | `func (e *Env) SetFuncCanonicalWithContext(` | Same for canonical func cell. SetFuncCanonical wraps it. |
  | `core/env.go` | ReplaceCellWithContext | `func (e *Env) ReplaceCellWithContext(` | Installs a fresh *Cell (identity change): journal must record prior cell pointer + map membership; route to owner. ReplaceCell wraps it. Only caller in core is loop recur on a forkCells child. |
  | `core/env.go` | Delete | `func (e *Env) Delete(name string) {` | Route to owner lock; journal tombstone of both cells; then layer.TombstoneForDelete receives the env passed (see lazy facts). |
  | `core/env.go` | localCell | `func (e *Env) localCell(name string) *Cell {` | no change: owner-only; a forwarded write never reaches the view |
  | `core/env.go` | localFuncCell | `func (e *Env) localFuncCell(name string) *Cell {` | no change: owner-only; a forwarded write never reaches the view |
  | `core/env.go` | prepareFreshRetained | `func (e *Env) prepareFreshRetained(` | Reads/writes retainedBytes/retainedSlots and activeRetainedMeter/reserveRetainedBindings on e: must execute on owner. |
  | `core/env.go` | recordFreshRetained | `func recordFreshRetained(` | Stores env into pendingCellAlloc.env: must be the canonical owner (metering later locks pending.env.mu). |
  | `core/registration.go` | beforeWrite/afterWrite | new file | create the entry on first view write; later writes advance last/lastVer only (no rebase until c4) |

- **redRun:** `go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_ViewHoldsNoBindings|TestRegistration_ViewWriteRecordsBeforeImage|TestRegistration_HostEqualValueRebindIsUnowned|TestRegistration_RegisterValueThroughViewIsAttributed|TestRegistration_RawRebindDuringOperationZeroAllocs|TestRegistration_JournalBoundedByDistinctNames)$'`
- **verify:** `go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_AbortRestoresOverwrittenBindings|TestRegistration_AbortRemovesAddedNames|TestRegistration_AbortRestoresCanonicalStatus|TestRegistration_AbortRestoresDeletedBinding|TestRegistration_AbortRestoresReplacedCell|TestRegistration_HostEqualValueRebindSurvivesAbort|TestRegistration_HostRebindAfterOperationSurvivesAbort|TestRegistration_HostAddSurvivesAbort|TestRegistration_HostDeleteAfterOperationSurvivesAbort|TestRegistration_HostDeleteRecreateSurvivesAbort|TestRegistration_HostReplaceCellSurvivesAbort|TestRegistration_OperationHostOperationAbortKeepsHostState|TestRegistration_CompletedViewForwardsUnattributed|TestRegistration_AbortedViewForwardsUnattributed|TestRegistration_ConcurrentHostWritesRace|TestRegistration_VMSiteDropsRevertedDefinition|TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration|TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/...`

### c4 — tasks 1.1, 2.4

- **Order:** after `c3`, sharedPkg `core`; red stage after `c1-inert-api`. Seam `abort-ownership`. Coder `go-coder`.
- **Split:** red 1.1; code 2.4.
- **Red tests:** `TestRegistration_AbortRestoresOverwrittenBindings`, `TestRegistration_AbortRemovesAddedNames`, `TestRegistration_AbortRestoresCanonicalStatus`, `TestRegistration_AbortRestoresDeletedBinding`, `TestRegistration_AbortRestoresReplacedCell`, `TestRegistration_HostEqualValueRebindSurvivesAbort`, `TestRegistration_HostRebindAfterOperationSurvivesAbort`, `TestRegistration_HostAddSurvivesAbort`, `TestRegistration_HostDeleteAfterOperationSurvivesAbort`, `TestRegistration_HostDeleteRecreateSurvivesAbort`, `TestRegistration_HostReplaceCellSurvivesAbort`, `TestRegistration_OperationHostOperationAbortKeepsHostState`, `TestRegistration_CompletedViewForwardsUnattributed`, `TestRegistration_AbortedViewForwardsUnattributed`, `TestRegistration_ConcurrentHostWritesRace`, `TestRegistration_VMSiteDropsRevertedDefinition`.
- **Closes:** abort-ownership: abort [live-op]; abort-ownership: abort [tomb-op]; abort-ownership: abort of an op-added name; abort-ownership: abort [live-host]; abort-ownership: abort [tomb-host]; abort-ownership: abort [replaced-host]; abort-ownership: view write [live-host\|tomb-host\|replaced-host] (rebase); abort-ownership: raw write interleaved concurrently with view writes, then abort; view-forwarding: r.Abort() [active] — restore leg; write-routing: raw root write (any mutator) on a journaled key — abort-effect leg; write-routing: raw root write with a value equal to the op's — abort-effect leg; identity-counters: VM site hit after abort.
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `core/registration_abort_test.go` | new | new file | red tests: TestRegistration_AbortRestoresOverwrittenBindings, TestRegistration_AbortRemovesAddedNames, TestRegistration_AbortRestoresCanonicalStatus, TestRegistration_AbortRestoresDeletedBinding, TestRegistration_AbortRestoresReplacedCell, TestRegistration_HostEqualValueRebindSurvivesAbort, TestRegistration_HostRebindAfterOperationSurvivesAbort, TestRegistration_HostAddSurvivesAbort, TestRegistration_HostDeleteAfterOperationSurvivesAbort, TestRegistration_HostDeleteRecreateSurvivesAbort, TestRegistration_HostReplaceCellSurvivesAbort, TestRegistration_OperationHostOperationAbortKeepsHostState, TestRegistration_CompletedViewForwardsUnattributed, TestRegistration_AbortedViewForwardsUnattributed, TestRegistration_ConcurrentHostWritesRace, TestRegistration_VMSiteDropsRevertedDefinition |
  | `core/registration.go` | new | new file | Journal entry record/rebase/abort under root mu: before each view write compare installed cell ptr + version with entry's latest-write; if differs, advance before-image; abort restores only when current == latest op write; host delete counts as foreign. |
  | `core/vm/vm_test.go` | TestVM_SiteReResolvesAfterGenerationBump | `func TestVM_SiteReResolvesAfterGenerationBump(t *testing.T) {` | add TestRegistration_VMSiteDropsRevertedDefinition next to it (package vm, white-box chunk.site) |

- **redRun:** `go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run '^(TestRegistration_AbortRestoresOverwrittenBindings|TestRegistration_AbortRemovesAddedNames|TestRegistration_AbortRestoresCanonicalStatus|TestRegistration_AbortRestoresDeletedBinding|TestRegistration_AbortRestoresReplacedCell|TestRegistration_HostEqualValueRebindSurvivesAbort|TestRegistration_HostRebindAfterOperationSurvivesAbort|TestRegistration_HostAddSurvivesAbort|TestRegistration_HostDeleteAfterOperationSurvivesAbort|TestRegistration_HostDeleteRecreateSurvivesAbort|TestRegistration_HostReplaceCellSurvivesAbort|TestRegistration_OperationHostOperationAbortKeepsHostState|TestRegistration_CompletedViewForwardsUnattributed|TestRegistration_AbortedViewForwardsUnattributed|TestRegistration_ConcurrentHostWritesRace|TestRegistration_VMSiteDropsRevertedDefinition)$'`
- **verify:** `go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration|TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/... && go test -race -timeout 5m -p 2 -parallel 2 ./core ./core/vm -run '^TestRegistration_(ConcurrentHostWritesRace|VMSiteDropsRevertedDefinition)$'` (race: abort and the journal hooks run under root.mu against concurrent raw writers and Rebuild)

### c5 — tasks 1.1, 2.3

- **Order:** after `c4`, sharedPkg `core`; red stage after `c1-inert-api`. Seam `alias-routing`. Coder `go-coder`.
- **Split:** red 1.1; code 2.3.
- **Red tests:** `TestRegistration_ViewHoldsNoBindingsAfterMerge`, `TestRegistration_FindOwnerWriteIsAttributed`, `TestRegistration_ChildScopeWriteIsAttributed`, `TestRegistration_EvaluatorReentryWriteIsAttributed`, `TestRegistration_CapturedClosureWriteIsAttributed`, `TestRegistration_MergeIntoViewIsAttributed`, `TestRegistration_MergeFromViewReadsRoot`, `TestRegistration_MergeIntoSelfRefused`, `TestRegistration_AbortRestoresConfiguration`, `TestRegistration_AbortKeepsHostConfiguration`.
- **Closes:** alias-routing: view.Find(name), name owned by the root; alias-routing: owner.Set* on the Find result; alias-routing: view.Find(name), name owned above the root; alias-routing: child := view.Child(); child.Set(local); alias-routing: set! of a root name evaluated in a Child or ChildVariadic scope of the view; alias-routing: view.Evaluator().Eval(ctx, (def x v) \| (set! x v) \| (defmacro m [a] a), view); alias-routing: Apply of a Lambda whose Env is the view, body (set! x v); alias-routing: the same closure applied after finish; alias-routing: src.MergeInto(view) or src.MergeIntoCanonical(view); alias-routing: view.MergeInto(other); alias-routing: view.MergeInto(root) \| root.MergeInto(view) \| view.MergeInto(view) \| root.MergeInto(root); alias-routing: view.Rebuild(); alias-routing: view.BumpMacroEpoch(); alias-routing: view.SetEvaluator \| view.SetRetainedMeter \| view.SetLazyLayer; alias-routing: raw root.SetEvaluator \| SetRetainedMeter \| SetLazyLayer; alias-routing: view read that materializes a lazy name.
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `core/registration_alias_test.go` | new | new file | red tests: TestRegistration_ViewHoldsNoBindingsAfterMerge, TestRegistration_FindOwnerWriteIsAttributed, TestRegistration_ChildScopeWriteIsAttributed, TestRegistration_EvaluatorReentryWriteIsAttributed, TestRegistration_CapturedClosureWriteIsAttributed, TestRegistration_MergeIntoViewIsAttributed, TestRegistration_MergeFromViewReadsRoot, TestRegistration_MergeIntoSelfRefused, TestRegistration_AbortRestoresConfiguration, TestRegistration_AbortKeepsHostConfiguration |
  | `core/env.go` | Find | `func (e *Env) Find(name string) (*Env, bool) {` | owner-aware: when e is a view and the root owns name, return the view, never the raw root; keep c2's lazy() miss path; an owner above the root is returned raw (outside the guarantee) |
  | `core/env.go` | Child | `func (e *Env) Child() *Env {` | NewEnv(e): child of a view keeps the view as parent so parent traversal retains attribution. |
  | `core/env.go` | ChildVariadic | `func (e *Env) ChildVariadic(` | Child + child.Set on the new local scope; no root write; verify parent is the view. |
  | `core/env.go` | NewEnv | `func NewEnv(parent *Env) *Env {` | no change: the view carries eval and retained limits snapshotted at BeginRegistration, so NewEnv(view) copies the root values |
  | `core/env.go` | NewEnv | `e.eval = parent.eval` | no change: the view carries eval and retained limits snapshotted at BeginRegistration, so NewEnv(view) copies the root values |
  | `core/env.go` | forkCells | `func (e *Env) forkCells(parent *Env, names []Symbol) *Env {` | no change: the receiver is always a loop child; NewEnv(view) copies the view's Begin-time snapshot of eval and limits |
  | `core/env.go` | SetEvaluator | `func (e *Env) SetEvaluator(eval Evaluator) {` | Forward to owner; journal config mutation reached through view (conflict policy). |
  | `core/env.go` | SetLazyLayer | `func (e *Env) SetLazyLayer(layer LazyLayer) {` | Forward + journal config mutation via view. |
  | `core/env.go` | SetRetainedMeter | `func (e *Env) SetRetainedMeter(m any) {` | Forward + journal via view; also called by metering settleRetained on pending.env. |
  | `core/env.go` | RegisterValueWithContext | `return layer.RegisterValue(e, name, val, canonical)` | no change: the layer comes from LazyLayer() (forwarded to owner in c2) and receives the view; the eager fallback writes through the view |
  | `core/env.go` | RegisterSource | `return layer.RegisterSource(e, name, source)` | no change: same ruling as RegisterValueWithContext |
  | `core/env.go` | BumpMacroEpoch | `func (e *Env) BumpMacroEpoch() {` | Bump owner counter (runtime chunk cache key reads be.globals.MacroEpoch()). |
  | `core/env.go` | Rebuild | `func (e *Env) Rebuild() (freedBytes, freedSlots int64) {` | forward to owner(); unattributed compaction (pinning of entry-held tombstones lands in c6) |
  | `core/env.go` | mergeInto | `func (e *Env) mergeInto(target *Env, canonical bool) error {` | src := e.owner(); dst := target.owner(); r := target.viewReg(); when src == dst return evalErrorf("merge source and target are the same environment") before taking any lock; otherwise src.mu.RLock then dst.mu.Lock as today; dst.applyMergePlan(&plan, dst.active(r)) calls beforeWrite/afterWrite per commit |
  | `core/env.go` | MergeInto | `func (e *Env) MergeInto(target *Env) error {` | Wrapper; covered by mergeInto. |
  | `core/env.go` | MergeIntoCanonical | `func (e *Env) MergeIntoCanonical(target *Env) error {` | Wrapper; covered by mergeInto. |
  | `core/env.go` | mergePlan.add | `func (p *mergePlan) add(target *Env, name string, src *Cell, canonical, funcCell bool) error {` | Reads target.funcs/target.vars + target.reserveRetainedBindings: target must be owner. |
  | `core/env.go` | applyMergePlan | `func (e *Env) applyMergePlan(p *mergePlan) {` | Writes via localCell/localFuncCell, cell fields, counters: owner only; journal each commit (new vs existing cell). |
  | `core/eval.go` | lookupBoundMacro | `for e := env; e != nil; e = e.parent {` | no change: view maps are nil, so the walk reaches the root |
  | `core/eval.go` | macroRebindIsIdentical | `if pm.Name != macro.Name \|\| pm.Env != macro.Env \|\| pm.Variadic != macro.Variadic \|\|` | Env pointer identity: macro captured under a view never equals one captured under root -> extra epoch bump (fail-closed). Audit only. |
  | `core/eval.go` | BindMacro | `env.BumpMacroEpoch()` | no change: env.BumpMacroEpoch() forwards to the root through the c5 BumpMacroEpoch site; never journaled |
  | `core/eval.go` | evalFn | `return nil, &LispicoError{Code: "EvalError", Message: fmt.Sprintf("fn: %s", err), Cause: err}` | Next lines build Lambda{Env: env}: closure captures the view; audit only. |
  | `core/eval.go` | evalDefn | `if err := e.bindOperator(ctx, env, name.V, lambda); err != nil {` | Lambda{Env: env} capture + bindOperator -> SetFuncWithContext/SetWithContext; audit. |
  | `core/eval.go` | evalDefmacro | `if err := BindMacro(ctx, env, name.V, macro, e.lisp2); err != nil {` | Macro{Env: env} capture; audit. |
  | `core/eval.go` | apply Lambda | `child, err := f.Env.ChildVariadic(f.Params, args, f.Variadic)` | Captured-closure reentry: child of f.Env (may be view); audit. |
  | `core/eval.go` | expandMacro | `macroEnv, err := m.Env.ChildVariadic(m.Params, args, m.Variadic)` | Macro expansion child of m.Env; audit. |
  | `core/eval.go` | evalSet | `defEnv, ok := env.Find(name.V)` | set! owner lookup then defEnv.SetWithContext: owner must be owner-aware view. |
  | `core/eval.go` | evalLoop | `next := loopEnv.forkCells(env, loopVars)` | forkCells(parent=env): NewEnv copy from possible view. |
  | `core/eval.go` | pendingCellAlloc | `type pendingCellAlloc struct {` | env field receives recordFreshRetained env: must be owner. |
  | `core/registration.go` | Registration config before-images | new file | per config field (evaluator, retained meter, lazy layer): recorded prior, recorded flag, foreign flag. View setters record the prior on the first op write and rebase a foreign field; raw setters mark the field foreign under root.mu while root.reg is non-nil; Abort restores recorded && !foreign fields |

- **redRun:** `go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration)$'`
- **verify:** `go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/...`

### c6 — tasks 1.1, 2.5

- **Order:** after `c5`, sharedPkg `core`; red stage after `c1-inert-api`. Seam `identity-counters`. Coder `go-coder`.
- **Split:** red 1.1; code 2.5.
- **Red tests:** `TestRegistration_AbortInvalidatesCellCaches`, `TestRegistration_AbortBumpsMacroEpoch`, `TestRegistration_AbortWithoutOwnedEntriesKeepsCounters`, `TestRegistration_RebuildDuringOperationKeepsOwnedTombstone`.
- **Closes:** identity-counters: root.Rebuild() while active, cell == entry.last; identity-counters: root.Rebuild() while active, tombstone of an unjournaled name; identity-counters: abort [pinned-tomb-op]; identity-counters: root.Rebuild() after finish; identity-counters: abort restoring at least 1 entry.
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `core/registration_counters_test.go` | new | new file | red tests: TestRegistration_AbortInvalidatesCellCaches, TestRegistration_AbortBumpsMacroEpoch, TestRegistration_AbortWithoutOwnedEntriesKeepsCounters, TestRegistration_RebuildDuringOperationKeepsOwnedTombstone |
  | `core/env.go` | Rebuild | `func (e *Env) Rebuild() (freedBytes, freedSlots int64) {` | while a registration is active keep every tombstoned cell that is some entry.last (pinned: stays in the map, counted as cell.retainedBytes + 1 slot, not released, rebuilt untouched) |
  | `core/registration.go` | new | new file | counter bumps in Abort: when the abort restores at least one entry, newNameGen.Add(1) and macroEpoch++ exactly once each, directly under the held root.mu (never BumpMacroEpoch, which re-locks); restoring nothing leaves both unchanged |

- **redRun:** `go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$'`
- **verify:** `go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm && golangci-lint run ./core/...`

### c7-validate-docs — tasks 3.1

- **Order:** after `c6`, sharedPkg `core`. Seam `validation-docs`. Coder `zpatcher`.
- **Split:** red —; code 3.1.
- **Waiver:** NO-RED-WAIVER: validation and docs only. NO-TESTER-WAIVER: runs the suites authored under 1.
- **Sites:**

  | File | Symbol | Anchor | Change |
  | --- | --- | --- | --- |
  | `CONTEXT.md` | Owned capacity | `**Owned capacity**:` | add a **Registration view** entry next to it: forwards to its root; writes attributed while the operation is active; abort reverts owned writes in place; added names are tombstoned and released by the next Rebuild |
  | `CHANGELOG.md` | [Unreleased] | `## [Unreleased]` | Added: Env.BeginRegistration, Registration.Env/Complete/Abort, CodeRegistrationActive; Changed: a merge whose source and target resolve to the same environment returns an EvalError instead of blocking |
  | `CLAUDE.md` | Architecture tree | `├── env.go      # Environment chain (lexical scope)` | add core/registration.go to the tree |
  | `docs/adr/0012-retained-state-owned-capacity-accounting.md` | Rebuild decision | `**`Rebuild()` for in-place compaction.** `(*Env).Rebuild()` compacts the` | amend the Rebuild decision: while a registration is active, Rebuild keeps every tombstoned cell that is some journal entry's last write (not released, still counted) until the operation closes; aborted additions stay tombstoned and are released by the next Rebuild |
  | `docs/adr/0003-concurrency-model.md` | Consequences | `- Environments remain individually synchronized;` | note: a registration view forwards to its root and takes the root lock; the journal is updated under the same lock as the mutation |

- **verify:** `go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm && go test -race -timeout 5m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)' && golangci-lint run ./core/... && openspec validate registration-journal --strict --json && git diff --quiet master -- go.mod go.sum core/plugin.go` (race: task 3.1 names the race run over the whole regex)

### Errors and naming the tests assert

- Nested or concurrent `BeginRegistration`: `*LispicoError` with `Code == CodeRegistrationActive` (`"RegistrationActiveError"`), message `environment already has an active registration`.
- Self-merge (source and target resolve to one env): `*LispicoError` with `Code == "EvalError"`, message `merge source and target are the same environment`, returned before any lock.
- Capacity refusal during a view write: existing `*LispicoError` with `Code == CodeResourceLimit`; no journal entry.
- Journal reads: `reg.entries[registrationKey{name: n, fn: f}]`, fields `prior`, `v`, `canonical`, `last`, `lastVer`.

### Seam contracts

**audit** (tasks 0.1) — states: `checklist-recorded`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| first source change of the change | checklist-recorded | no-op | tasks.md 0.1 |

Forbidden: source edits before the checklist is recorded.

Seeding: checklist-recorded: reached only by recording the audit seam's ownership checklist in the change's apply notes before c1; no test seeds it, and its budget is checked by review against env.go's exported method list.

Budgets: checklist covers 100% of exported Env methods and every direct Env/Cell field access in core and core/vm.

**view-forwarding** (tasks 2.1) — states: `idle`, `active`, `finished`, `active-other`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| root.BeginRegistration() | idle | set | spec: root identity SHALL NOT change; root.reg=r, view.reg=r, view.parent=root, view != root |
| root.BeginRegistration() | active | no-op | plan ruling: one active registration per root; returns *LispicoError with Code == CodeRegistrationActive, root.reg unchanged |
| view.BeginRegistration() | active | no-op | plan ruling: a view resolves to its root; refused with CodeRegistrationActive |
| view.BeginRegistration() | finished | set | plan ruling: resolves to its root and starts r2 with a new view |
| r.Complete() | active | clear | design.md: after completion the retained view forwards without an active operation |
| r.Abort() | active | clear | spec: after completion or abort the view SHALL forward as an ordinary unattributed environment |
| r.Complete() or r.Abort() | finished | no-op | plan ruling: idempotent |
| r.Complete() or r.Abort() | active-other | no-op | plan ruling: root.reg == r2 != r; r2 unaffected |
| view read: Get, GetCanonical, GetFunc, GetFuncCanonical, Cell, FuncCell, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, LocalNames, VarNames, NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, RetainedUsage, Evaluator, LazyLayer | idle\|active\|finished\|active-other | no-op | spec: SHALL forward every read to that root; design.md surface table |
| raw root rebind, then a view read | finished | no-op | spec scenario Completed view keeps forwarding: the view observes the rebinding |

Forbidden: view == root; view.vars or view.funcs non-nil at any time; view.reg changed after BeginRegistration returns; two registrations active on one root; root.reg holding a registration whose root is another env; holding root.mu while acquiring any view mutex; a Registration built by struct literal outside BeginRegistration (tests included).

Seeding: idle: NewEnv(nil) or NewEnvWithRetainedLimits(nil, b, s), no BeginRegistration; active: reg, err := root.BeginRegistration() with err == nil; finished: reg.Complete() or reg.Abort() after Begin, keeping view := reg.Env(); active-other: after reg1 finishes, reg2, _ := root.BeginRegistration().

Budgets: unsafe.Sizeof(Env{}) == 208 (TestEnv_Size unchanged); Get through a view: 0 allocs; extra cost = one RLock/RUnlock of the view mutex + one nil-map probe + one atomic lazyLayer load, then the root's normal path; Get/GetCanonical/GetFunc/GetFuncCanonical/GetMaterialized*/Cell/FuncCell on non-view envs: +0 instructions (code unchanged); forwarded non-walking reads (NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, Find per level, Evaluator, LazyLayer, RetainedUsage, RetainedMeter, Local*Names, *Names): +1 atomic pointer load + 1 nil branch on every env; NameGen sits on the VM site-hit path (vm.go:1607, 1641); BeginRegistration: 2 allocs (Registration, view Env); entries map allocated on first op write.

**write-routing** (tasks 2.2) — states: `absent`, `tomb`, `live-prior`, `live-op`, `tomb-op`, `live-host`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| view Set/SetWithContext | absent\|tomb\|live-prior | set | env.go:364-390; spec: writes reached through the view SHALL be attributed |
| view SetCanonical/SetCanonicalWithContext | absent\|tomb\|live-prior | set | env.go:449-475 |
| view SetFunc*/SetFuncCanonical* | absent\|tomb\|live-prior | set | env.go:680-738; key fn=true |
| view SetBoth*/SetBothCanonical* | absent\|tomb\|live-prior | set | env.go:280-324; one entry per namespace |
| view ReplaceCell/ReplaceCellWithContext | absent\|tomb\|live-prior | set | env.go:398-423; entry.last is the new cell, entry.prior the old one |
| view Delete | live-prior\|live-op | set | env.go:861-878; journals only the namespaces whose cell is live or canonical; layer.TombstoneForDelete still receives the root |
| view Delete | absent\|tomb | no-op | env.go:863, 868 guards: no mutation, no entry |
| view RegisterValue with no lazy layer | absent\|live-prior | set | env.go:120-128: the eager SetCanonical/Set runs on the view |
| view write refused by capacity (ResourceLimitError) | absent\|tomb\|live-prior\|live-op | no-op | env.go:190-200; beforeWrite runs only after prepareFreshRetained succeeds |
| view write repeated on the same key with no foreign write between | live-op\|tomb-op | set | design.md: before-image kept, only last/lastVer advance |
| raw root write (any mutator) on a journaled key | live-op\|tomb-op | forced | spec: raw-root writes SHALL be unattributed; the version bump makes the entry foreign (live-host) at abort; the journal is untouched at write time |
| raw root write with a value equal to the op's | live-op | forced | spec: unattributed even when equal; Set always bumps the version (env.go:385) |
| view write after finish or under another registration | absent\|tomb\|live-prior\|live-host | no-op | root.active(r) returns nil, so the forward is unattributed |

Forbidden: an entry created for a write that did not mutate (capacity refusal; Delete of an absent or non-canonical tombstone); the raw write path reading or writing r.entries; a Cell v/canonical assignment without version.Add(1) under the owner lock; entry.last == nil after afterWrite; a forwarded write reaching localCell/localFuncCell on the view.

Seeding: live-prior: root.Set/SetFunc/SetCanonical/SetFuncCanonical before BeginRegistration; tomb: root.Set then root.Delete before BeginRegistration; absent: name never written; live-op: reg.Env().Set* while active; tomb-op: reg.Env().Delete of a live-prior name while active; live-host: a view write, then raw root.Set on the same name.

Budgets: raw public mutator on an env with no registration: +1 atomic pointer load + 1 nil branch, 0 extra allocs; raw rebind of a live name on a root with an active registration: 0 allocs; journal: at most one entry per distinct (namespace, name) written through the view; 1000 view rebinds of one name keep len(reg.entries) == 1; view write: +1 map lookup under the root lock, +1 entry alloc on the first write per key.

**alias-routing** (tasks 2.3) — states: `active`, `finished`, `cfg-prior`, `cfg-op`, `cfg-host`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| view.Find(name), name owned by the root | active\|finished | no-op | design.md table: Find returns an owner-aware view; returns the view, never the raw root |
| owner.Set* on the Find result | active | set | spec: writes reached through an owner returned by lookup |
| view.Find(name), name owned above the root | active\|finished | no-op | plan ruling: returns the raw ancestor, outside the guarantee |
| child := view.Child(); child.Set(local) | active | no-op | design.md: preserve lexical child scopes; child-local, no entry |
| set! of a root name evaluated in a Child or ChildVariadic scope of the view | active | set | spec: a child scope |
| view.Evaluator().Eval(ctx, (def x v) \| (set! x v) \| (defmacro m [a] a), view) | active | set | spec: evaluator reentry; eval.go:1217, 1543, 1306-1323 |
| Apply of a Lambda whose Env is the view, body (set! x v) | active | set | spec: a closure capturing the view |
| the same closure applied after finish | finished | no-op | spec: forwards unattributed |
| src.MergeInto(view) or src.MergeIntoCanonical(view) | active | set | spec: a merge whose target is the view; env.go:1097-1129 |
| view.MergeInto(other) | active\|finished | no-op | plan ruling: the source resolves to the root and copies root locals; no journal, the target is not the root |
| view.MergeInto(root) \| root.MergeInto(view) \| view.MergeInto(view) \| root.MergeInto(root) | active\|finished | no-op | plan ruling: owner(src) == owner(dst) returns EvalError "merge source and target are the same environment" before any lock; today root.MergeInto(root) self-deadlocks (RLock then Lock on one RWMutex, env.go:1098-1101) |
| view.Rebuild() | active\|finished | no-op | forwarded to root.Rebuild; compaction is not a binding write |
| view.BumpMacroEpoch() | active\|finished | forced | root MacroEpoch +1, never journaled |
| view.SetEvaluator \| view.SetRetainedMeter \| view.SetLazyLayer | cfg-prior\|cfg-host | set | design.md table: journal any root configuration mutation reached through the view (the state becomes cfg-op) |
| raw root.SetEvaluator \| SetRetainedMeter \| SetLazyLayer | cfg-op | forced | same conflict policy: marks the field foreign (cfg-host) under root.mu |
| view read that materializes a lazy name | active | no-op | plan ruling: layer.LookupAndMaterialize receives the root and the install is unattributed; attributing materialization belongs to plugin-binding-rollback |

Forbidden: Find on a view returning the raw root; mergeInto locking a view mutex, or one root mutex twice; any lock order other than resolved-source RLock then resolved-target Lock; layer.LookupAndMaterialize, ForceAll or TombstoneForDelete called with a view env; SetLazyLayer storing without e.mu (the raw path now locks; cold path, one caller at runtime/lazy_template.go:699).

Seeding: Find owner: root.Set(x) before Begin; owner, _ := view.Find(x); child scope: child := view.Child(), or view.ChildVariadic(params, args, Symbol{}); drive set! with view.Evaluator().Eval(ctx, form, child); evaluator reentry: root.SetEvaluator(NewEvaluator()) BEFORE BeginRegistration, because the view snapshots eval; closure: lam, _ := view.Evaluator().Eval(ctx, (fn [] (set! x 3)), view); then view.Evaluator().Apply(ctx, lam, nil, view); merge target: src := NewEnv(nil); src.Set/SetFunc; src.MergeInto(reg.Env()); cfg-prior: raw root setter before Begin; cfg-op: view setter while active; cfg-host: view setter, then raw root setter.

Budgets: Find per scope level: +1 atomic load + 1 nil branch; self-merge refusal: 0 locks taken; config setters: cold path, one root lock hold each.

**abort-ownership** (tasks 1.1, 2.4) — states: `absent`, `tomb`, `live-prior`, `live-op`, `tomb-op`, `live-host`, `tomb-host`, `replaced-host`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| abort | live-op | set | spec: restore prior value, canonical status, presence when the current state is still the op's latest write |
| abort | tomb-op | set | spec: deletion state restored; the op delete is reverted in the same cell |
| abort of an op-added name | live-op | clear | spec: added names SHALL be absent; tombstone in place (ADR 0012: Rebuild is the only release path) |
| abort | live-host | no-op | spec: SHALL otherwise leave the current state untouched |
| abort | tomb-host | no-op | design.md: host deletion counts equally |
| abort | replaced-host | no-op | map cell != entry.last |
| view write | live-host\|tomb-host\|replaced-host | set | design.md: advance the before-image to the foreign state, then apply; the entry becomes live-op or tomb-op |
| raw write interleaved concurrently with view writes, then abort | live-op\|live-host | no-op | the per-key rule runs under root.mu; no written key ends holding an op-only value |

Forbidden: abort writing a key whose map cell != entry.last or whose version moved; abort installing a cell other than entry.prior, or creating a cell; abort calling a meter, the lazy layer, or BumpMacroEpoch; entries surviving Complete or Abort; after abort, a key the op wrote holding an op-written value (it holds its before-image or a host-written state).

Seeding: op, host, abort: view write, raw root write on the same name, reg.Abort(); op, host, op, abort: view write, raw root write, view write, reg.Abort(); host delete/recreate: view write, raw root.Delete, raw root.Set, reg.Abort(); host ReplaceCell: view write, raw root.ReplaceCell, reg.Abort(); every foreign-write test also writes an op-only control name that must be restored, keeping it red against the inert stage.

Budgets: abort: one root lock hold, O(len(entries)), 0 allocs, 0 meter calls, 0 new cells; NameGen +1 and MacroEpoch +1 exactly once per abort that restores at least 1 entry; +0 otherwise; race test: 4 goroutines x 200 iterations, well inside -timeout 5m under -race.

**identity-counters** (tasks 2.5) — states: `live-op`, `tomb-op`, `pinned-tomb-op`, `tomb-host`, `tomb`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| root.Rebuild() while active, cell == entry.last | tomb-op\|tomb-host | no-op | plan ruling: pinned; stays in the map, counted as cell.retainedBytes + 1 slot, not released, rebuilt flag untouched (tomb-op becomes pinned-tomb-op) |
| root.Rebuild() while active, tombstone of an unjournaled name | tomb | clear | env.go:897-906 unchanged: dropped and released |
| abort | pinned-tomb-op | set | spec: restore prior cells in place |
| root.Rebuild() after finish | tomb | clear | ADR 0012: the next Rebuild releases former pins and aborted additions |
| abort restoring at least 1 entry | live-op\|tomb-op | forced | spec: keep versions/NameGen/MacroEpoch monotone and invalidate cached lookups; each restored cell version +1, NameGen +1, MacroEpoch +1 |
| VM site hit after abort | live-op | clear | vm.go:1609-1618: the version/gen mismatch falls back to a locked ReadCell of the restored cell |

Forbidden: any decrease of Cell.version, NameGen or MacroEpoch; abort restoring a historical counter value; Rebuild dropping or releasing a tombstoned cell that is an active entry's last; abort installing a fresh *Cell.

Seeding: pinned-tomb-op: root.Set(x) before Begin; view.Delete(x); root.Rebuild(); held cell: c, _ := root.Cell(x) before Begin; view.Set(x, op); reg.Abort(); read root.ReadCellSnapshot(c); VM site: vm.New(root) running an OpGetGlobal x chunk (pattern vm_test.go:1723-1762) after view.Set(x, op); then reg.Abort(), vm.Reset(), rerun; macro: root.SetEvaluator(NewEvaluator()) before Begin; define the macro through view.Evaluator().Eval(ctx, (defmacro m [a] a), view), then read root.MacroEpoch().

Budgets: per restoring abort: NameGen +1, MacroEpoch +1, each restored cell version +1, and +1 on the op cell tombstoned by a ReplaceCell revert; Rebuild with an active registration: +1 map lookup per tombstoned cell; without one: +1 nil branch.

**validation-docs** (tasks 3.1) — states: `validated`.

| Input | State | Effect | Evidence |
| --- | --- | --- | --- |
| all 2.x chunks landed | validated | no-op | tasks.md 3.1 |

Forbidden: editing the expectations of existing TestEnv_* or TestMerge* tests; creating new architecture docs (update CONTEXT.md only).

Seeding: validated: reached only after c6 lands, by running the packet's verifyCommands in order, then make lint && make test; no test seeds it.

Budgets: focused run within -timeout 2m; race run within -timeout 5m.

### Floor, lenses, review

- **Floor:** `make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 5m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)' && openspec validate registration-journal --strict --json`
- **Lenses:** spec, quality, perf — `spec` always; `quality` for the heavy tier (lock discipline, journal logic); `perf` because the change adds a branch to `NameGen` on the VM site-hit path and to every root mutation, and `Env` must stay 208 bytes. No `arch` (no new package or moved boundary), no `sec` (no input, auth, or I/O surface).
- **Plan review:** pass by zarchitect, 2 round(s).
  - Round 1: 4 blockers (verify ran later chunks' red tests; c4 race leg too wide; phantom design.md anchor; coder-authored shared test helpers), each answered by a delta.
  - Round 2: merge_ready; 3 text warnings (stale c5 site texts, Find keeping lazy(), task 0.1 ticked by the orchestrator) fixed as residue without re-review.

### Requirements map

| SHALL | Tests |
| --- | --- |
| A registration view over a root environment SHALL forward every read and write to that root, and the root's identity SHALL NOT change. | `TestRegistration_ViewForwardsReadsAndKeepsRootIdentity`, `TestRegistration_ViewHoldsNoBindings`, `TestRegistration_ViewHoldsNoBindingsAfterMerge`, `TestRegistration_MergeFromViewReadsRoot` |
| While its operation is active, writes reached through the view SHALL be attributed to that operation, including writes reached through an owner returned by lookup, a child scope, evaluator reentry, a closure capturing the view, or a merge whose target is the view. | `TestRegistration_ViewWriteRecordsBeforeImage`, `TestRegistration_RegisterValueThroughViewIsAttributed`, `TestRegistration_FindOwnerWriteIsAttributed`, `TestRegistration_ChildScopeWriteIsAttributed`, `TestRegistration_EvaluatorReentryWriteIsAttributed`, `TestRegistration_CapturedClosureWriteIsAttributed`, `TestRegistration_MergeIntoViewIsAttributed` |
| Writes through the raw root SHALL be unattributed even when their value equals an attributed write. | `TestRegistration_HostEqualValueRebindIsUnowned`, `TestRegistration_HostEqualValueRebindSurvivesAbort`, `TestRegistration_RawRebindDuringOperationZeroAllocs` |
| Aborting the operation SHALL, for each name the operation wrote, restore the prior value, canonical status, presence, and deletion state only when the current state is still the operation's latest write, and SHALL otherwise leave the current state untouched. | `TestRegistration_AbortRestoresOverwrittenBindings`, `TestRegistration_AbortRemovesAddedNames`, `TestRegistration_AbortRestoresCanonicalStatus`, `TestRegistration_AbortRestoresDeletedBinding`, `TestRegistration_HostRebindAfterOperationSurvivesAbort`, `TestRegistration_HostAddSurvivesAbort`, `TestRegistration_HostDeleteAfterOperationSurvivesAbort`, `TestRegistration_HostDeleteRecreateSurvivesAbort`, `TestRegistration_HostReplaceCellSurvivesAbort`, `TestRegistration_OperationHostOperationAbortKeepsHostState`, `TestRegistration_ConcurrentHostWritesRace` |
| Abort SHALL restore prior cells in place rather than install replacement cells, SHALL keep cell versions, name generations, and macro epochs monotone, and SHALL invalidate cached lookups that observed a reverted definition. | `TestRegistration_AbortRestoresOverwrittenBindings`, `TestRegistration_AbortRestoresReplacedCell`, `TestRegistration_AbortInvalidatesCellCaches`, `TestRegistration_AbortBumpsMacroEpoch`, `TestRegistration_AbortWithoutOwnedEntriesKeepsCounters`, `TestRegistration_RebuildDuringOperationKeepsOwnedTombstone`, `TestRegistration_VMSiteDropsRevertedDefinition` |
| After completion or abort the view SHALL forward as an ordinary unattributed environment. | `TestRegistration_CompletedViewForwardsUnattributed`, `TestRegistration_AbortedViewForwardsUnattributed`, `TestRegistration_CompleteAndAbortAreIdempotent`, `TestRegistration_CapturedClosureWriteIsAttributed` |
| An environment with no active operation SHALL keep its existing binding behavior. | `TestEnv_Size`, `TestEnv_Get_ZeroAllocs`, `TestEnv_NameGen`, `TestEnv_CellVersion`, `TestEnv_RebuildReleasesDeadCapacity`, `TestMergeInto_ConcurrentSetNotLost`, `TestRegistration_ViewGetZeroAllocs` |
| prior values and canonical status SHALL be restored in the same cells, added names SHALL be absent, and version counters SHALL NOT decrease | `TestRegistration_AbortRestoresOverwrittenBindings`, `TestRegistration_AbortRemovesAddedNames`, `TestRegistration_AbortRestoresCanonicalStatus` |
| the raw-root write's resulting state SHALL remain | `TestRegistration_HostRebindAfterOperationSurvivesAbort`, `TestRegistration_HostAddSurvivesAbort`, `TestRegistration_HostDeleteAfterOperationSurvivesAbort`, `TestRegistration_HostDeleteRecreateSurvivesAbort`, `TestRegistration_OperationHostOperationAbortKeepsHostState` |
| each of those writes SHALL be reverted under the same conflict rule | `TestRegistration_FindOwnerWriteIsAttributed`, `TestRegistration_ChildScopeWriteIsAttributed`, `TestRegistration_EvaluatorReentryWriteIsAttributed`, `TestRegistration_CapturedClosureWriteIsAttributed`, `TestRegistration_MergeIntoViewIsAttributed` |
| reads through the view SHALL observe the rebinding and later writes through it SHALL be unattributed | `TestRegistration_CompletedViewForwardsUnattributed`, `TestRegistration_AbortedViewForwardsUnattributed` |
| the next use SHALL observe the restored binding | `TestRegistration_AbortBumpsMacroEpoch`, `TestRegistration_VMSiteDropsRevertedDefinition`, `TestRegistration_AbortInvalidatesCellCaches` |

## Plan appendix

```json
{
  "v": 2,
  "change": "registration-journal",
  "baseSha": "396afc06821d4680b36c4d90cc7af82669a87142",
  "generatedAt": "2026-09-11T13:53:38.011Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "c1-inert-api",
      "taskIds": [
        "0.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "view-forwarding",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "core/registration.go",
          "symbol": "new",
          "anchor": "",
          "change": "FIELD-FIRST inert members only: type Registration struct{ root, view *Env; entries map[registrationKey]*registrationEntry }, type registrationKey struct{ name string; fn bool }, type registrationEntry struct{ prior *Cell; v Value; canonical bool; last *Cell; lastVer uint64 }; func (e *Env) BeginRegistration() (*Registration, error) returning &Registration{root: e, view: e}, nil; func (r *Registration) Env() *Env returning r.view; Complete() and Abort() with empty bodies",
          "new": true
        },
        {
          "task": "2.1",
          "file": "core/error.go",
          "symbol": "CodeResourceLimit",
          "anchor": "const CodeResourceLimit = \"ResourceLimitError\"",
          "change": "add const CodeRegistrationActive = \"RegistrationActiveError\" and func NewRegistrationActiveError() *LispicoError with Message \"environment already has an active registration\", next to CodeConcurrentUse"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "Env",
          "anchor": "type Env struct {",
          "change": "add reg atomic.Pointer[Registration] next to newNameGen; remove cell0Used (TestEnv_Size stays 208)"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "localCell",
          "anchor": "func (e *Env) localCell(name string) *Cell {",
          "change": "replace cell0Used with e.cell0.version.Load() != 0; WHY comment: every localCell caller bumps the returned cell version before the next localCell under the same lock"
        }
      ],
      "contract": {
        "states": [
          "idle",
          "active",
          "finished",
          "active-other"
        ],
        "transitions": [
          {
            "input": "root.BeginRegistration()",
            "state": "idle",
            "effect": "set",
            "evidence": "spec: root identity SHALL NOT change; root.reg=r, view.reg=r, view.parent=root, view != root"
          },
          {
            "input": "root.BeginRegistration()",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: one active registration per root; returns *LispicoError with Code == CodeRegistrationActive, root.reg unchanged"
          },
          {
            "input": "view.BeginRegistration()",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: a view resolves to its root; refused with CodeRegistrationActive"
          },
          {
            "input": "view.BeginRegistration()",
            "state": "finished",
            "effect": "set",
            "evidence": "plan ruling: resolves to its root and starts r2 with a new view"
          },
          {
            "input": "r.Complete()",
            "state": "active",
            "effect": "clear",
            "evidence": "design.md: after completion the retained view forwards without an active operation"
          },
          {
            "input": "r.Abort()",
            "state": "active",
            "effect": "clear",
            "evidence": "spec: after completion or abort the view SHALL forward as an ordinary unattributed environment"
          },
          {
            "input": "r.Complete() or r.Abort()",
            "state": "finished",
            "effect": "no-op",
            "evidence": "plan ruling: idempotent"
          },
          {
            "input": "r.Complete() or r.Abort()",
            "state": "active-other",
            "effect": "no-op",
            "evidence": "plan ruling: root.reg == r2 != r; r2 unaffected"
          },
          {
            "input": "view read: Get, GetCanonical, GetFunc, GetFuncCanonical, Cell, FuncCell, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, LocalNames, VarNames, NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, RetainedUsage, Evaluator, LazyLayer",
            "state": "idle|active|finished|active-other",
            "effect": "no-op",
            "evidence": "spec: SHALL forward every read to that root; design.md surface table"
          },
          {
            "input": "raw root rebind, then a view read",
            "state": "finished",
            "effect": "no-op",
            "evidence": "spec scenario Completed view keeps forwarding: the view observes the rebinding"
          }
        ],
        "forbidden": [
          "view == root",
          "view.vars or view.funcs non-nil at any time",
          "view.reg changed after BeginRegistration returns",
          "two registrations active on one root",
          "root.reg holding a registration whose root is another env",
          "holding root.mu while acquiring any view mutex",
          "a Registration built by struct literal outside BeginRegistration (tests included)"
        ],
        "seeding": [
          "idle: NewEnv(nil) or NewEnvWithRetainedLimits(nil, b, s), no BeginRegistration",
          "active: reg, err := root.BeginRegistration() with err == nil",
          "finished: reg.Complete() or reg.Abort() after Begin, keeping view := reg.Env()",
          "active-other: after reg1 finishes, reg2, _ := root.BeginRegistration()"
        ],
        "budgets": [
          "unsafe.Sizeof(Env{}) == 208 (TestEnv_Size unchanged)",
          "Get through a view: 0 allocs; extra cost = one RLock/RUnlock of the view mutex + one nil-map probe + one atomic lazyLayer load, then the root's normal path",
          "Get/GetCanonical/GetFunc/GetFuncCanonical/GetMaterialized*/Cell/FuncCell on non-view envs: +0 instructions (code unchanged)",
          "forwarded non-walking reads (NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, Find per level, Evaluator, LazyLayer, RetainedUsage, RetainedMeter, Local*Names, *Names): +1 atomic pointer load + 1 nil branch on every env; NameGen sits on the VM site-hit path (vm.go:1607, 1641)",
          "BeginRegistration: 2 allocs (Registration, view Env); entries map allocated on first op write"
        ],
        "closes": [
          "FIELD-FIRST: every red test of c2..c6 compiles and fails on an assertion against this chunk",
          "task 0.1 has no site: the ownership checklist is recorded in design.md and the audit seam; the orchestrator ticks 0.1 in tasks.md when c1 closes, not the c1 coder"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [],
      "verify": "go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "c2",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": "c1-inert-api",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "view-forwarding",
      "shard": "",
      "redAfter": "c1-inert-api",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/registration_view_test.go",
          "symbol": "new",
          "anchor": "",
          "change": "red tests: TestRegistration_ViewForwardsReadsAndKeepsRootIdentity, TestRegistration_NestedBeginRefused, TestRegistration_CompleteAndAbortAreIdempotent, TestRegistration_ViewGetZeroAllocs",
          "new": true
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "HasLive",
          "anchor": "func (e *Env) HasLive(name string) bool {",
          "change": "forward through owner(): Reads e.vars directly: forward to owner."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "HasLiveFunc",
          "anchor": "func (e *Env) HasLiveFunc(name string) bool {",
          "change": "forward through owner(): Reads e.funcs directly: forward to owner."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "CellLocal",
          "anchor": "func (e *Env) CellLocal(name string) (*Cell, bool) {",
          "change": "forward through owner(); the forwarded call reaches the layer with env = root, never the view"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "FuncCellLocal",
          "anchor": "func (e *Env) FuncCellLocal(name string) (*Cell, bool) {",
          "change": "forward through owner(); the forwarded call reaches the layer with env = root, never the view"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "ReadCell",
          "anchor": "func (e *Env) ReadCell(c *Cell) (Value, bool, bool) {",
          "change": "forward through owner(): Must take the canonical owner's RLock, not the view's mu (VM calls entry.env.ReadCell where entry.env may be a view)."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "ReadCellSnapshot",
          "anchor": "func (e *Env) ReadCellSnapshot(",
          "change": "forward through owner(): Same: owner lock."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "Evaluator",
          "anchor": "func (e *Env) Evaluator() Evaluator {",
          "change": "forward through owner(): Unlocked read of e.eval: forward to owner."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "LazyLayer",
          "anchor": "func (e *Env) LazyLayer() LazyLayer {",
          "change": "forward through owner(): Forward to owner's atomic lazyLayer."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "RetainedMeter",
          "anchor": "func (e *Env) RetainedMeter() any {",
          "change": "forward through owner(): Forward to owner."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "NameGen",
          "anchor": "func (e *Env) NameGen() uint64 { return e.newNameGen.Load() }",
          "change": "forward through owner(): Return owner counter (VM site + runtime callCache compare it)."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "MacroEpoch",
          "anchor": "func (e *Env) MacroEpoch() int {",
          "change": "forward through owner(): Return owner counter."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "RetainedUsage",
          "anchor": "func (e *Env) RetainedUsage() (bytes, slots int64) {",
          "change": "forward through owner(): Return owner counters."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "VarNames",
          "anchor": "func (e *Env) VarNames() []string {",
          "change": "forward through owner(); the forwarded call reaches the layer with env = root, never the view"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "LocalNames",
          "anchor": "func (e *Env) LocalNames() []string {",
          "change": "forward through owner(): Reads e.vars: forward."
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "FuncNames",
          "anchor": "func (e *Env) FuncNames() []string {",
          "change": "forward through owner(); the forwarded call reaches the layer with env = root, never the view"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "LocalFuncNames",
          "anchor": "func (e *Env) LocalFuncNames() []string {",
          "change": "forward through owner(): Reads e.funcs: forward."
        },
        {
          "task": "2.1",
          "file": "core/registration.go",
          "symbol": "BeginRegistration",
          "anchor": "",
          "change": "real body: root := e.owner(); lock root.mu; refuse with NewRegistrationActiveError() when root.reg.Load() != nil; view := &Env{parent: root, eval: root.eval, maxRetainedBytes: root.maxRetainedBytes, maxRetainedSlots: root.maxRetainedSlots}; view.reg.Store(r); root.reg.Store(r); unlock. Env() returns the view. Complete: under root.mu, if root.reg.Load() == r clear it and drop entries. Abort in this chunk is lifecycle only (clear root.reg, drop entries); restore lands in c4",
          "new": true
        },
        {
          "task": "2.1",
          "file": "core/registration.go",
          "symbol": "owner/viewReg/lazy",
          "anchor": "",
          "change": "unexported helpers: owner() is r.root when reg is non-nil, else e; viewReg() is reg when e is a view (r.view == e), else nil; lazy() reads the lazyLayer field only",
          "new": true
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "Get",
          "anchor": "func (e *Env) Get(name string) (Value, bool) {",
          "change": "switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "GetCanonical",
          "anchor": "func (e *Env) GetCanonical(name string) (Value, bool, bool) {",
          "change": "switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "GetFunc",
          "anchor": "func (e *Env) GetFunc(name string) (Value, bool) {",
          "change": "switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "GetFuncCanonical",
          "anchor": "func (e *Env) GetFuncCanonical(name string) (Value, bool, bool) {",
          "change": "switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "Find",
          "anchor": "func (e *Env) Find(name string) (*Env, bool) {",
          "change": "switch the internal miss path from e.LazyLayer() to e.lazy(): once LazyLayer() forwards to owner(), a view would otherwise hand itself to the root layer; a view has no layer of its own, so the walk continues to the root, which calls its layer with env = root"
        }
      ],
      "contract": {
        "states": [
          "idle",
          "active",
          "finished",
          "active-other"
        ],
        "transitions": [
          {
            "input": "root.BeginRegistration()",
            "state": "idle",
            "effect": "set",
            "evidence": "spec: root identity SHALL NOT change; root.reg=r, view.reg=r, view.parent=root, view != root"
          },
          {
            "input": "root.BeginRegistration()",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: one active registration per root; returns *LispicoError with Code == CodeRegistrationActive, root.reg unchanged"
          },
          {
            "input": "view.BeginRegistration()",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: a view resolves to its root; refused with CodeRegistrationActive"
          },
          {
            "input": "view.BeginRegistration()",
            "state": "finished",
            "effect": "set",
            "evidence": "plan ruling: resolves to its root and starts r2 with a new view"
          },
          {
            "input": "r.Complete()",
            "state": "active",
            "effect": "clear",
            "evidence": "design.md: after completion the retained view forwards without an active operation"
          },
          {
            "input": "r.Abort()",
            "state": "active",
            "effect": "clear",
            "evidence": "spec: after completion or abort the view SHALL forward as an ordinary unattributed environment"
          },
          {
            "input": "r.Complete() or r.Abort()",
            "state": "finished",
            "effect": "no-op",
            "evidence": "plan ruling: idempotent"
          },
          {
            "input": "r.Complete() or r.Abort()",
            "state": "active-other",
            "effect": "no-op",
            "evidence": "plan ruling: root.reg == r2 != r; r2 unaffected"
          },
          {
            "input": "view read: Get, GetCanonical, GetFunc, GetFuncCanonical, Cell, FuncCell, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, LocalNames, VarNames, NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, RetainedUsage, Evaluator, LazyLayer",
            "state": "idle|active|finished|active-other",
            "effect": "no-op",
            "evidence": "spec: SHALL forward every read to that root; design.md surface table"
          },
          {
            "input": "raw root rebind, then a view read",
            "state": "finished",
            "effect": "no-op",
            "evidence": "spec scenario Completed view keeps forwarding: the view observes the rebinding"
          }
        ],
        "forbidden": [
          "view == root",
          "view.vars or view.funcs non-nil at any time",
          "view.reg changed after BeginRegistration returns",
          "two registrations active on one root",
          "root.reg holding a registration whose root is another env",
          "holding root.mu while acquiring any view mutex",
          "a Registration built by struct literal outside BeginRegistration (tests included)"
        ],
        "seeding": [
          "idle: NewEnv(nil) or NewEnvWithRetainedLimits(nil, b, s), no BeginRegistration",
          "active: reg, err := root.BeginRegistration() with err == nil",
          "finished: reg.Complete() or reg.Abort() after Begin, keeping view := reg.Env()",
          "active-other: after reg1 finishes, reg2, _ := root.BeginRegistration()"
        ],
        "budgets": [
          "unsafe.Sizeof(Env{}) == 208 (TestEnv_Size unchanged)",
          "Get through a view: 0 allocs; extra cost = one RLock/RUnlock of the view mutex + one nil-map probe + one atomic lazyLayer load, then the root's normal path",
          "Get/GetCanonical/GetFunc/GetFuncCanonical/GetMaterialized*/Cell/FuncCell on non-view envs: +0 instructions (code unchanged)",
          "forwarded non-walking reads (NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, Find per level, Evaluator, LazyLayer, RetainedUsage, RetainedMeter, Local*Names, *Names): +1 atomic pointer load + 1 nil branch on every env; NameGen sits on the VM site-hit path (vm.go:1607, 1641)",
          "BeginRegistration: 2 allocs (Registration, view Env); entries map allocated on first op write"
        ],
        "closes": [
          "view-forwarding: root.BeginRegistration() [idle]",
          "view-forwarding: root.BeginRegistration() [active]",
          "view-forwarding: view.BeginRegistration() [active]",
          "view-forwarding: view.BeginRegistration() [finished]",
          "view-forwarding: r.Complete() [active]",
          "view-forwarding: r.Abort() [active] — lifecycle leg only (clears root.reg); the restore leg closes in c4",
          "view-forwarding: r.Complete() or r.Abort() [finished]",
          "view-forwarding: r.Complete() or r.Abort() [active-other]",
          "view-forwarding: view read: Get, GetCanonical, GetFunc, GetFuncCanonical, Cell, FuncCell, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, LocalNames, VarNames, NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, RetainedUsage, Evaluator, LazyLayer",
          "view-forwarding: raw root rebind, then a view read [finished]"
        ],
        "codeScope": "Per-chunk code scope, replacing the seam codeTasks where they differ. c2's Abort only clears root.reg and drops entries. c3's beforeWrite creates an entry when none exists and otherwise only advances last/lastVer, with no rebase. c4 adds the restore and the rebase, and restored cells get version +1, but NameGen and MacroEpoch are not bumped. c6 adds the NameGen/MacroEpoch bumps and Rebuild pinning.",
        "redNotes": [
          "Helpers are file-local with chunk-specific names; there is no shared helpers file. Each file declares its own panic-recovering mutation helper (reports with t.Fatalf) and its own entry lookup that calls t.Fatalf on a missing reg.entries key before any field access.",
          "Describe failures in prose: a recovered nil-pointer panic prints \"runtime error\", which the red check reads as a non-assertion failure."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [
        "TestRegistration_ViewForwardsReadsAndKeepsRootIdentity",
        "TestRegistration_NestedBeginRefused",
        "TestRegistration_CompleteAndAbortAreIdempotent",
        "TestRegistration_ViewGetZeroAllocs"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_ViewForwardsReadsAndKeepsRootIdentity|TestRegistration_NestedBeginRefused|TestRegistration_CompleteAndAbortAreIdempotent|TestRegistration_ViewGetZeroAllocs)$'",
      "verify": "go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_ViewHoldsNoBindings|TestRegistration_ViewWriteRecordsBeforeImage|TestRegistration_HostEqualValueRebindIsUnowned|TestRegistration_RegisterValueThroughViewIsAttributed|TestRegistration_RawRebindDuringOperationZeroAllocs|TestRegistration_JournalBoundedByDistinctNames|TestRegistration_AbortRestoresOverwrittenBindings|TestRegistration_AbortRemovesAddedNames|TestRegistration_AbortRestoresCanonicalStatus|TestRegistration_AbortRestoresDeletedBinding|TestRegistration_AbortRestoresReplacedCell|TestRegistration_HostEqualValueRebindSurvivesAbort|TestRegistration_HostRebindAfterOperationSurvivesAbort|TestRegistration_HostAddSurvivesAbort|TestRegistration_HostDeleteAfterOperationSurvivesAbort|TestRegistration_HostDeleteRecreateSurvivesAbort|TestRegistration_HostReplaceCellSurvivesAbort|TestRegistration_OperationHostOperationAbortKeepsHostState|TestRegistration_CompletedViewForwardsUnattributed|TestRegistration_AbortedViewForwardsUnattributed|TestRegistration_ConcurrentHostWritesRace|TestRegistration_VMSiteDropsRevertedDefinition|TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration|TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "c3",
      "taskIds": [
        "1.1",
        "2.2"
      ],
      "prev": "c2",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "write-routing",
      "shard": "",
      "redAfter": "c1-inert-api",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/registration_journal_test.go",
          "symbol": "new",
          "anchor": "",
          "change": "red tests: TestRegistration_ViewHoldsNoBindings, TestRegistration_ViewWriteRecordsBeforeImage, TestRegistration_HostEqualValueRebindIsUnowned, TestRegistration_RegisterValueThroughViewIsAttributed, TestRegistration_RawRebindDuringOperationZeroAllocs, TestRegistration_JournalBoundedByDistinctNames",
          "new": true
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "setBoth",
          "anchor": "func (e *Env) setBoth(ctx context.Context, name string, val Value, canonical bool) error {",
          "change": "thread r; under root.mu compute j := root.active(r); after prepareFreshRetained succeeds call j.beforeWrite / j.afterWrite for the value key and the function key; entry creation only, no rebase (c4). Covers the SetBoth* wrappers"
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "SetWithContext",
          "anchor": "func (e *Env) SetWithContext(ctx context.Context, name string, val Value) error {",
          "change": "Route to owner under owner lock; journal value-cell before-image (new name => prior absent; tombstoned => revive) when view-originated. Set wraps it."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "SetCanonicalWithContext",
          "anchor": "func (e *Env) SetCanonicalWithContext(",
          "change": "Same routing/journaling; canonical marker in before-image. SetCanonical wraps it."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "SetFuncWithContext",
          "anchor": "func (e *Env) SetFuncWithContext(",
          "change": "Same for funcs namespace; note it never bumps newNameGen. SetFunc wraps it."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "SetFuncCanonicalWithContext",
          "anchor": "func (e *Env) SetFuncCanonicalWithContext(",
          "change": "Same for canonical func cell. SetFuncCanonical wraps it."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "ReplaceCellWithContext",
          "anchor": "func (e *Env) ReplaceCellWithContext(",
          "change": "Installs a fresh *Cell (identity change): journal must record prior cell pointer + map membership; route to owner. ReplaceCell wraps it. Only caller in core is loop recur on a forkCells child."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "Delete",
          "anchor": "func (e *Env) Delete(name string) {",
          "change": "Route to owner lock; journal tombstone of both cells; then layer.TombstoneForDelete receives the env passed (see lazy facts)."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "localCell",
          "anchor": "func (e *Env) localCell(name string) *Cell {",
          "change": "no change: owner-only; a forwarded write never reaches the view"
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "localFuncCell",
          "anchor": "func (e *Env) localFuncCell(name string) *Cell {",
          "change": "no change: owner-only; a forwarded write never reaches the view"
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "prepareFreshRetained",
          "anchor": "func (e *Env) prepareFreshRetained(",
          "change": "Reads/writes retainedBytes/retainedSlots and activeRetainedMeter/reserveRetainedBindings on e: must execute on owner."
        },
        {
          "task": "2.2",
          "file": "core/env.go",
          "symbol": "recordFreshRetained",
          "anchor": "func recordFreshRetained(",
          "change": "Stores env into pendingCellAlloc.env: must be the canonical owner (metering later locks pending.env.mu)."
        },
        {
          "task": "2.2",
          "file": "core/registration.go",
          "symbol": "beforeWrite/afterWrite",
          "anchor": "",
          "change": "create the entry on first view write; later writes advance last/lastVer only (no rebase until c4)",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "absent",
          "tomb",
          "live-prior",
          "live-op",
          "tomb-op",
          "live-host"
        ],
        "transitions": [
          {
            "input": "view Set/SetWithContext",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:364-390; spec: writes reached through the view SHALL be attributed"
          },
          {
            "input": "view SetCanonical/SetCanonicalWithContext",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:449-475"
          },
          {
            "input": "view SetFunc*/SetFuncCanonical*",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:680-738; key fn=true"
          },
          {
            "input": "view SetBoth*/SetBothCanonical*",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:280-324; one entry per namespace"
          },
          {
            "input": "view ReplaceCell/ReplaceCellWithContext",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:398-423; entry.last is the new cell, entry.prior the old one"
          },
          {
            "input": "view Delete",
            "state": "live-prior|live-op",
            "effect": "set",
            "evidence": "env.go:861-878; journals only the namespaces whose cell is live or canonical; layer.TombstoneForDelete still receives the root"
          },
          {
            "input": "view Delete",
            "state": "absent|tomb",
            "effect": "no-op",
            "evidence": "env.go:863, 868 guards: no mutation, no entry"
          },
          {
            "input": "view RegisterValue with no lazy layer",
            "state": "absent|live-prior",
            "effect": "set",
            "evidence": "env.go:120-128: the eager SetCanonical/Set runs on the view"
          },
          {
            "input": "view write refused by capacity (ResourceLimitError)",
            "state": "absent|tomb|live-prior|live-op",
            "effect": "no-op",
            "evidence": "env.go:190-200; beforeWrite runs only after prepareFreshRetained succeeds"
          },
          {
            "input": "view write repeated on the same key with no foreign write between",
            "state": "live-op|tomb-op",
            "effect": "set",
            "evidence": "design.md: before-image kept, only last/lastVer advance"
          },
          {
            "input": "raw root write (any mutator) on a journaled key",
            "state": "live-op|tomb-op",
            "effect": "forced",
            "evidence": "spec: raw-root writes SHALL be unattributed; the version bump makes the entry foreign (live-host) at abort; the journal is untouched at write time"
          },
          {
            "input": "raw root write with a value equal to the op's",
            "state": "live-op",
            "effect": "forced",
            "evidence": "spec: unattributed even when equal; Set always bumps the version (env.go:385)"
          },
          {
            "input": "view write after finish or under another registration",
            "state": "absent|tomb|live-prior|live-host",
            "effect": "no-op",
            "evidence": "root.active(r) returns nil, so the forward is unattributed"
          }
        ],
        "forbidden": [
          "an entry created for a write that did not mutate (capacity refusal; Delete of an absent or non-canonical tombstone)",
          "the raw write path reading or writing r.entries",
          "a Cell v/canonical assignment without version.Add(1) under the owner lock",
          "entry.last == nil after afterWrite",
          "a forwarded write reaching localCell/localFuncCell on the view"
        ],
        "seeding": [
          "live-prior: root.Set/SetFunc/SetCanonical/SetFuncCanonical before BeginRegistration",
          "tomb: root.Set then root.Delete before BeginRegistration",
          "absent: name never written",
          "live-op: reg.Env().Set* while active",
          "tomb-op: reg.Env().Delete of a live-prior name while active",
          "live-host: a view write, then raw root.Set on the same name"
        ],
        "budgets": [
          "raw public mutator on an env with no registration: +1 atomic pointer load + 1 nil branch, 0 extra allocs",
          "raw rebind of a live name on a root with an active registration: 0 allocs",
          "journal: at most one entry per distinct (namespace, name) written through the view; 1000 view rebinds of one name keep len(reg.entries) == 1",
          "view write: +1 map lookup under the root lock, +1 entry alloc on the first write per key"
        ],
        "closes": [
          "write-routing: view Set/SetWithContext",
          "write-routing: view SetCanonical/SetCanonicalWithContext",
          "write-routing: view SetFunc*/SetFuncCanonical*",
          "write-routing: view SetBoth*/SetBothCanonical*",
          "write-routing: view ReplaceCell/ReplaceCellWithContext",
          "write-routing: view Delete [live-prior|live-op]",
          "write-routing: view Delete [absent|tomb]",
          "write-routing: view RegisterValue with no lazy layer",
          "write-routing: view write refused by capacity (ResourceLimitError)",
          "write-routing: view write repeated on the same key with no foreign write between",
          "write-routing: raw root write (any mutator) on a journaled key — journal-state leg (lastVer mismatch); the abort effect closes in c4",
          "write-routing: raw root write with a value equal to the op's — journal-state leg; the abort effect closes in c4",
          "write-routing: view write after finish or under another registration"
        ],
        "codeScope": "Per-chunk code scope, replacing the seam codeTasks where they differ. c2's Abort only clears root.reg and drops entries. c3's beforeWrite creates an entry when none exists and otherwise only advances last/lastVer, with no rebase. c4 adds the restore and the rebase, and restored cells get version +1, but NameGen and MacroEpoch are not bumped. c6 adds the NameGen/MacroEpoch bumps and Rebuild pinning.",
        "redNotes": [
          "Helpers are file-local with chunk-specific names; there is no shared helpers file. Each file declares its own panic-recovering mutation helper (reports with t.Fatalf) and its own entry lookup that calls t.Fatalf on a missing reg.entries key before any field access.",
          "Describe failures in prose: a recovered nil-pointer panic prints \"runtime error\", which the red check reads as a non-assertion failure."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestRegistration_ViewHoldsNoBindings",
        "TestRegistration_ViewWriteRecordsBeforeImage",
        "TestRegistration_HostEqualValueRebindIsUnowned",
        "TestRegistration_RegisterValueThroughViewIsAttributed",
        "TestRegistration_RawRebindDuringOperationZeroAllocs",
        "TestRegistration_JournalBoundedByDistinctNames"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_ViewHoldsNoBindings|TestRegistration_ViewWriteRecordsBeforeImage|TestRegistration_HostEqualValueRebindIsUnowned|TestRegistration_RegisterValueThroughViewIsAttributed|TestRegistration_RawRebindDuringOperationZeroAllocs|TestRegistration_JournalBoundedByDistinctNames)$'",
      "verify": "go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_AbortRestoresOverwrittenBindings|TestRegistration_AbortRemovesAddedNames|TestRegistration_AbortRestoresCanonicalStatus|TestRegistration_AbortRestoresDeletedBinding|TestRegistration_AbortRestoresReplacedCell|TestRegistration_HostEqualValueRebindSurvivesAbort|TestRegistration_HostRebindAfterOperationSurvivesAbort|TestRegistration_HostAddSurvivesAbort|TestRegistration_HostDeleteAfterOperationSurvivesAbort|TestRegistration_HostDeleteRecreateSurvivesAbort|TestRegistration_HostReplaceCellSurvivesAbort|TestRegistration_OperationHostOperationAbortKeepsHostState|TestRegistration_CompletedViewForwardsUnattributed|TestRegistration_AbortedViewForwardsUnattributed|TestRegistration_ConcurrentHostWritesRace|TestRegistration_VMSiteDropsRevertedDefinition|TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration|TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "c4",
      "taskIds": [
        "1.1",
        "2.4"
      ],
      "prev": "c3",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "abort-ownership",
      "shard": "",
      "redAfter": "c1-inert-api",
      "pkgDirs": [
        "core",
        "core/vm"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core",
        "github.com/victorzhuk/go-lispico/core/vm"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/registration_abort_test.go",
          "symbol": "new",
          "anchor": "",
          "change": "red tests: TestRegistration_AbortRestoresOverwrittenBindings, TestRegistration_AbortRemovesAddedNames, TestRegistration_AbortRestoresCanonicalStatus, TestRegistration_AbortRestoresDeletedBinding, TestRegistration_AbortRestoresReplacedCell, TestRegistration_HostEqualValueRebindSurvivesAbort, TestRegistration_HostRebindAfterOperationSurvivesAbort, TestRegistration_HostAddSurvivesAbort, TestRegistration_HostDeleteAfterOperationSurvivesAbort, TestRegistration_HostDeleteRecreateSurvivesAbort, TestRegistration_HostReplaceCellSurvivesAbort, TestRegistration_OperationHostOperationAbortKeepsHostState, TestRegistration_CompletedViewForwardsUnattributed, TestRegistration_AbortedViewForwardsUnattributed, TestRegistration_ConcurrentHostWritesRace, TestRegistration_VMSiteDropsRevertedDefinition",
          "new": true
        },
        {
          "task": "2.4",
          "file": "core/registration.go",
          "symbol": "new",
          "anchor": "",
          "change": "Journal entry record/rebase/abort under root mu: before each view write compare installed cell ptr + version with entry's latest-write; if differs, advance before-image; abort restores only when current == latest op write; host delete counts as foreign.",
          "new": true
        },
        {
          "task": "2.4",
          "file": "core/vm/vm_test.go",
          "symbol": "TestVM_SiteReResolvesAfterGenerationBump",
          "anchor": "func TestVM_SiteReResolvesAfterGenerationBump(t *testing.T) {",
          "change": "add TestRegistration_VMSiteDropsRevertedDefinition next to it (package vm, white-box chunk.site)"
        }
      ],
      "contract": {
        "states": [
          "absent",
          "tomb",
          "live-prior",
          "live-op",
          "tomb-op",
          "live-host",
          "tomb-host",
          "replaced-host"
        ],
        "transitions": [
          {
            "input": "abort",
            "state": "live-op",
            "effect": "set",
            "evidence": "spec: restore prior value, canonical status, presence when the current state is still the op's latest write"
          },
          {
            "input": "abort",
            "state": "tomb-op",
            "effect": "set",
            "evidence": "spec: deletion state restored; the op delete is reverted in the same cell"
          },
          {
            "input": "abort of an op-added name",
            "state": "live-op",
            "effect": "clear",
            "evidence": "spec: added names SHALL be absent; tombstone in place (ADR 0012: Rebuild is the only release path)"
          },
          {
            "input": "abort",
            "state": "live-host",
            "effect": "no-op",
            "evidence": "spec: SHALL otherwise leave the current state untouched"
          },
          {
            "input": "abort",
            "state": "tomb-host",
            "effect": "no-op",
            "evidence": "design.md: host deletion counts equally"
          },
          {
            "input": "abort",
            "state": "replaced-host",
            "effect": "no-op",
            "evidence": "map cell != entry.last"
          },
          {
            "input": "view write",
            "state": "live-host|tomb-host|replaced-host",
            "effect": "set",
            "evidence": "design.md: advance the before-image to the foreign state, then apply; the entry becomes live-op or tomb-op"
          },
          {
            "input": "raw write interleaved concurrently with view writes, then abort",
            "state": "live-op|live-host",
            "effect": "no-op",
            "evidence": "the per-key rule runs under root.mu; no written key ends holding an op-only value"
          }
        ],
        "forbidden": [
          "abort writing a key whose map cell != entry.last or whose version moved",
          "abort installing a cell other than entry.prior, or creating a cell",
          "abort calling a meter, the lazy layer, or BumpMacroEpoch",
          "entries surviving Complete or Abort",
          "after abort, a key the op wrote holding an op-written value (it holds its before-image or a host-written state)"
        ],
        "seeding": [
          "op, host, abort: view write, raw root write on the same name, reg.Abort()",
          "op, host, op, abort: view write, raw root write, view write, reg.Abort()",
          "host delete/recreate: view write, raw root.Delete, raw root.Set, reg.Abort()",
          "host ReplaceCell: view write, raw root.ReplaceCell, reg.Abort()",
          "every foreign-write test also writes an op-only control name that must be restored, keeping it red against the inert stage"
        ],
        "budgets": [
          "abort: one root lock hold, O(len(entries)), 0 allocs, 0 meter calls, 0 new cells",
          "NameGen +1 and MacroEpoch +1 exactly once per abort that restores at least 1 entry; +0 otherwise",
          "race test: 4 goroutines x 200 iterations, well inside -timeout 5m under -race"
        ],
        "closes": [
          "abort-ownership: abort [live-op]",
          "abort-ownership: abort [tomb-op]",
          "abort-ownership: abort of an op-added name",
          "abort-ownership: abort [live-host]",
          "abort-ownership: abort [tomb-host]",
          "abort-ownership: abort [replaced-host]",
          "abort-ownership: view write [live-host|tomb-host|replaced-host] (rebase)",
          "abort-ownership: raw write interleaved concurrently with view writes, then abort",
          "view-forwarding: r.Abort() [active] — restore leg",
          "write-routing: raw root write (any mutator) on a journaled key — abort-effect leg",
          "write-routing: raw root write with a value equal to the op's — abort-effect leg",
          "identity-counters: VM site hit after abort"
        ],
        "codeScope": "Per-chunk code scope, replacing the seam codeTasks where they differ. c2's Abort only clears root.reg and drops entries. c3's beforeWrite creates an entry when none exists and otherwise only advances last/lastVer, with no rebase. c4 adds the restore and the rebase, and restored cells get version +1, but NameGen and MacroEpoch are not bumped. c6 adds the NameGen/MacroEpoch bumps and Rebuild pinning.",
        "redNotes": [
          "Helpers are file-local with chunk-specific names; there is no shared helpers file. Each file declares its own panic-recovering mutation helper (reports with t.Fatalf) and its own entry lookup that calls t.Fatalf on a missing reg.entries key before any field access.",
          "Describe failures in prose: a recovered nil-pointer panic prints \"runtime error\", which the red check reads as a non-assertion failure.",
          "TestRegistration_VMSiteDropsRevertedDefinition lives in package vm and cannot see core test helpers: inline its recover."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.4"
      ],
      "redTests": [
        "TestRegistration_AbortRestoresOverwrittenBindings",
        "TestRegistration_AbortRemovesAddedNames",
        "TestRegistration_AbortRestoresCanonicalStatus",
        "TestRegistration_AbortRestoresDeletedBinding",
        "TestRegistration_AbortRestoresReplacedCell",
        "TestRegistration_HostEqualValueRebindSurvivesAbort",
        "TestRegistration_HostRebindAfterOperationSurvivesAbort",
        "TestRegistration_HostAddSurvivesAbort",
        "TestRegistration_HostDeleteAfterOperationSurvivesAbort",
        "TestRegistration_HostDeleteRecreateSurvivesAbort",
        "TestRegistration_HostReplaceCellSurvivesAbort",
        "TestRegistration_OperationHostOperationAbortKeepsHostState",
        "TestRegistration_CompletedViewForwardsUnattributed",
        "TestRegistration_AbortedViewForwardsUnattributed",
        "TestRegistration_ConcurrentHostWritesRace",
        "TestRegistration_VMSiteDropsRevertedDefinition"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run '^(TestRegistration_AbortRestoresOverwrittenBindings|TestRegistration_AbortRemovesAddedNames|TestRegistration_AbortRestoresCanonicalStatus|TestRegistration_AbortRestoresDeletedBinding|TestRegistration_AbortRestoresReplacedCell|TestRegistration_HostEqualValueRebindSurvivesAbort|TestRegistration_HostRebindAfterOperationSurvivesAbort|TestRegistration_HostAddSurvivesAbort|TestRegistration_HostDeleteAfterOperationSurvivesAbort|TestRegistration_HostDeleteRecreateSurvivesAbort|TestRegistration_HostReplaceCellSurvivesAbort|TestRegistration_OperationHostOperationAbortKeepsHostState|TestRegistration_CompletedViewForwardsUnattributed|TestRegistration_AbortedViewForwardsUnattributed|TestRegistration_ConcurrentHostWritesRace|TestRegistration_VMSiteDropsRevertedDefinition)$'",
      "verify": "go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration|TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/... && go test -race -timeout 5m -p 2 -parallel 2 ./core ./core/vm -run '^TestRegistration_(ConcurrentHostWritesRace|VMSiteDropsRevertedDefinition)$'",
      "coder": "go-coder",
      "race": "abort and the journal hooks run under root.mu against concurrent raw writers and Rebuild"
    },
    {
      "id": "c5",
      "taskIds": [
        "1.1",
        "2.3"
      ],
      "prev": "c4",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "alias-routing",
      "shard": "",
      "redAfter": "c1-inert-api",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/registration_alias_test.go",
          "symbol": "new",
          "anchor": "",
          "change": "red tests: TestRegistration_ViewHoldsNoBindingsAfterMerge, TestRegistration_FindOwnerWriteIsAttributed, TestRegistration_ChildScopeWriteIsAttributed, TestRegistration_EvaluatorReentryWriteIsAttributed, TestRegistration_CapturedClosureWriteIsAttributed, TestRegistration_MergeIntoViewIsAttributed, TestRegistration_MergeFromViewReadsRoot, TestRegistration_MergeIntoSelfRefused, TestRegistration_AbortRestoresConfiguration, TestRegistration_AbortKeepsHostConfiguration",
          "new": true
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "Find",
          "anchor": "func (e *Env) Find(name string) (*Env, bool) {",
          "change": "owner-aware: when e is a view and the root owns name, return the view, never the raw root; keep c2's lazy() miss path; an owner above the root is returned raw (outside the guarantee)"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "Child",
          "anchor": "func (e *Env) Child() *Env {",
          "change": "NewEnv(e): child of a view keeps the view as parent so parent traversal retains attribution."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "ChildVariadic",
          "anchor": "func (e *Env) ChildVariadic(",
          "change": "Child + child.Set on the new local scope; no root write; verify parent is the view."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "NewEnv",
          "anchor": "func NewEnv(parent *Env) *Env {",
          "change": "no change: the view carries eval and retained limits snapshotted at BeginRegistration, so NewEnv(view) copies the root values"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "NewEnv",
          "anchor": "e.eval = parent.eval",
          "change": "no change: the view carries eval and retained limits snapshotted at BeginRegistration, so NewEnv(view) copies the root values"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "forkCells",
          "anchor": "func (e *Env) forkCells(parent *Env, names []Symbol) *Env {",
          "change": "no change: the receiver is always a loop child; NewEnv(view) copies the view's Begin-time snapshot of eval and limits"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "SetEvaluator",
          "anchor": "func (e *Env) SetEvaluator(eval Evaluator) {",
          "change": "Forward to owner; journal config mutation reached through view (conflict policy)."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "SetLazyLayer",
          "anchor": "func (e *Env) SetLazyLayer(layer LazyLayer) {",
          "change": "Forward + journal config mutation via view."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "SetRetainedMeter",
          "anchor": "func (e *Env) SetRetainedMeter(m any) {",
          "change": "Forward + journal via view; also called by metering settleRetained on pending.env."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "RegisterValueWithContext",
          "anchor": "return layer.RegisterValue(e, name, val, canonical)",
          "change": "no change: the layer comes from LazyLayer() (forwarded to owner in c2) and receives the view; the eager fallback writes through the view"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "RegisterSource",
          "anchor": "return layer.RegisterSource(e, name, source)",
          "change": "no change: same ruling as RegisterValueWithContext"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "BumpMacroEpoch",
          "anchor": "func (e *Env) BumpMacroEpoch() {",
          "change": "Bump owner counter (runtime chunk cache key reads be.globals.MacroEpoch())."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "Rebuild",
          "anchor": "func (e *Env) Rebuild() (freedBytes, freedSlots int64) {",
          "change": "forward to owner(); unattributed compaction (pinning of entry-held tombstones lands in c6)"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "mergeInto",
          "anchor": "func (e *Env) mergeInto(target *Env, canonical bool) error {",
          "change": "src := e.owner(); dst := target.owner(); r := target.viewReg(); when src == dst return evalErrorf(\"merge source and target are the same environment\") before taking any lock; otherwise src.mu.RLock then dst.mu.Lock as today; dst.applyMergePlan(&plan, dst.active(r)) calls beforeWrite/afterWrite per commit"
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "MergeInto",
          "anchor": "func (e *Env) MergeInto(target *Env) error {",
          "change": "Wrapper; covered by mergeInto."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "MergeIntoCanonical",
          "anchor": "func (e *Env) MergeIntoCanonical(target *Env) error {",
          "change": "Wrapper; covered by mergeInto."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "mergePlan.add",
          "anchor": "func (p *mergePlan) add(target *Env, name string, src *Cell, canonical, funcCell bool) error {",
          "change": "Reads target.funcs/target.vars + target.reserveRetainedBindings: target must be owner."
        },
        {
          "task": "2.3",
          "file": "core/env.go",
          "symbol": "applyMergePlan",
          "anchor": "func (e *Env) applyMergePlan(p *mergePlan) {",
          "change": "Writes via localCell/localFuncCell, cell fields, counters: owner only; journal each commit (new vs existing cell)."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "lookupBoundMacro",
          "anchor": "for e := env; e != nil; e = e.parent {",
          "change": "no change: view maps are nil, so the walk reaches the root"
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "macroRebindIsIdentical",
          "anchor": "if pm.Name != macro.Name || pm.Env != macro.Env || pm.Variadic != macro.Variadic ||",
          "change": "Env pointer identity: macro captured under a view never equals one captured under root -> extra epoch bump (fail-closed). Audit only."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "BindMacro",
          "anchor": "env.BumpMacroEpoch()",
          "change": "no change: env.BumpMacroEpoch() forwards to the root through the c5 BumpMacroEpoch site; never journaled"
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "evalFn",
          "anchor": "return nil, &LispicoError{Code: \"EvalError\", Message: fmt.Sprintf(\"fn: %s\", err), Cause: err}",
          "change": "Next lines build Lambda{Env: env}: closure captures the view; audit only."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "evalDefn",
          "anchor": "if err := e.bindOperator(ctx, env, name.V, lambda); err != nil {",
          "change": "Lambda{Env: env} capture + bindOperator -> SetFuncWithContext/SetWithContext; audit."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "evalDefmacro",
          "anchor": "if err := BindMacro(ctx, env, name.V, macro, e.lisp2); err != nil {",
          "change": "Macro{Env: env} capture; audit."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "apply Lambda",
          "anchor": "child, err := f.Env.ChildVariadic(f.Params, args, f.Variadic)",
          "change": "Captured-closure reentry: child of f.Env (may be view); audit."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "expandMacro",
          "anchor": "macroEnv, err := m.Env.ChildVariadic(m.Params, args, m.Variadic)",
          "change": "Macro expansion child of m.Env; audit."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "evalSet",
          "anchor": "defEnv, ok := env.Find(name.V)",
          "change": "set! owner lookup then defEnv.SetWithContext: owner must be owner-aware view."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "evalLoop",
          "anchor": "next := loopEnv.forkCells(env, loopVars)",
          "change": "forkCells(parent=env): NewEnv copy from possible view."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "pendingCellAlloc",
          "anchor": "type pendingCellAlloc struct {",
          "change": "env field receives recordFreshRetained env: must be owner."
        },
        {
          "task": "2.3",
          "file": "core/registration.go",
          "symbol": "Registration config before-images",
          "anchor": "",
          "change": "per config field (evaluator, retained meter, lazy layer): recorded prior, recorded flag, foreign flag. View setters record the prior on the first op write and rebase a foreign field; raw setters mark the field foreign under root.mu while root.reg is non-nil; Abort restores recorded && !foreign fields",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "active",
          "finished",
          "cfg-prior",
          "cfg-op",
          "cfg-host"
        ],
        "transitions": [
          {
            "input": "view.Find(name), name owned by the root",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "design.md table: Find returns an owner-aware view; returns the view, never the raw root"
          },
          {
            "input": "owner.Set* on the Find result",
            "state": "active",
            "effect": "set",
            "evidence": "spec: writes reached through an owner returned by lookup"
          },
          {
            "input": "view.Find(name), name owned above the root",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "plan ruling: returns the raw ancestor, outside the guarantee"
          },
          {
            "input": "child := view.Child(); child.Set(local)",
            "state": "active",
            "effect": "no-op",
            "evidence": "design.md: preserve lexical child scopes; child-local, no entry"
          },
          {
            "input": "set! of a root name evaluated in a Child or ChildVariadic scope of the view",
            "state": "active",
            "effect": "set",
            "evidence": "spec: a child scope"
          },
          {
            "input": "view.Evaluator().Eval(ctx, (def x v) | (set! x v) | (defmacro m [a] a), view)",
            "state": "active",
            "effect": "set",
            "evidence": "spec: evaluator reentry; eval.go:1217, 1543, 1306-1323"
          },
          {
            "input": "Apply of a Lambda whose Env is the view, body (set! x v)",
            "state": "active",
            "effect": "set",
            "evidence": "spec: a closure capturing the view"
          },
          {
            "input": "the same closure applied after finish",
            "state": "finished",
            "effect": "no-op",
            "evidence": "spec: forwards unattributed"
          },
          {
            "input": "src.MergeInto(view) or src.MergeIntoCanonical(view)",
            "state": "active",
            "effect": "set",
            "evidence": "spec: a merge whose target is the view; env.go:1097-1129"
          },
          {
            "input": "view.MergeInto(other)",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "plan ruling: the source resolves to the root and copies root locals; no journal, the target is not the root"
          },
          {
            "input": "view.MergeInto(root) | root.MergeInto(view) | view.MergeInto(view) | root.MergeInto(root)",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "plan ruling: owner(src) == owner(dst) returns EvalError \"merge source and target are the same environment\" before any lock; today root.MergeInto(root) self-deadlocks (RLock then Lock on one RWMutex, env.go:1098-1101)"
          },
          {
            "input": "view.Rebuild()",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "forwarded to root.Rebuild; compaction is not a binding write"
          },
          {
            "input": "view.BumpMacroEpoch()",
            "state": "active|finished",
            "effect": "forced",
            "evidence": "root MacroEpoch +1, never journaled"
          },
          {
            "input": "view.SetEvaluator | view.SetRetainedMeter | view.SetLazyLayer",
            "state": "cfg-prior|cfg-host",
            "effect": "set",
            "evidence": "design.md table: journal any root configuration mutation reached through the view (the state becomes cfg-op)"
          },
          {
            "input": "raw root.SetEvaluator | SetRetainedMeter | SetLazyLayer",
            "state": "cfg-op",
            "effect": "forced",
            "evidence": "same conflict policy: marks the field foreign (cfg-host) under root.mu"
          },
          {
            "input": "view read that materializes a lazy name",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: layer.LookupAndMaterialize receives the root and the install is unattributed; attributing materialization belongs to plugin-binding-rollback"
          }
        ],
        "forbidden": [
          "Find on a view returning the raw root",
          "mergeInto locking a view mutex, or one root mutex twice",
          "any lock order other than resolved-source RLock then resolved-target Lock",
          "layer.LookupAndMaterialize, ForceAll or TombstoneForDelete called with a view env",
          "SetLazyLayer storing without e.mu (the raw path now locks; cold path, one caller at runtime/lazy_template.go:699)"
        ],
        "seeding": [
          "Find owner: root.Set(x) before Begin; owner, _ := view.Find(x)",
          "child scope: child := view.Child(), or view.ChildVariadic(params, args, Symbol{}); drive set! with view.Evaluator().Eval(ctx, form, child)",
          "evaluator reentry: root.SetEvaluator(NewEvaluator()) BEFORE BeginRegistration, because the view snapshots eval",
          "closure: lam, _ := view.Evaluator().Eval(ctx, (fn [] (set! x 3)), view); then view.Evaluator().Apply(ctx, lam, nil, view)",
          "merge target: src := NewEnv(nil); src.Set/SetFunc; src.MergeInto(reg.Env())",
          "cfg-prior: raw root setter before Begin; cfg-op: view setter while active; cfg-host: view setter, then raw root setter"
        ],
        "budgets": [
          "Find per scope level: +1 atomic load + 1 nil branch",
          "self-merge refusal: 0 locks taken",
          "config setters: cold path, one root lock hold each"
        ],
        "closes": [
          "alias-routing: view.Find(name), name owned by the root",
          "alias-routing: owner.Set* on the Find result",
          "alias-routing: view.Find(name), name owned above the root",
          "alias-routing: child := view.Child(); child.Set(local)",
          "alias-routing: set! of a root name evaluated in a Child or ChildVariadic scope of the view",
          "alias-routing: view.Evaluator().Eval(ctx, (def x v) | (set! x v) | (defmacro m [a] a), view)",
          "alias-routing: Apply of a Lambda whose Env is the view, body (set! x v)",
          "alias-routing: the same closure applied after finish",
          "alias-routing: src.MergeInto(view) or src.MergeIntoCanonical(view)",
          "alias-routing: view.MergeInto(other)",
          "alias-routing: view.MergeInto(root) | root.MergeInto(view) | view.MergeInto(view) | root.MergeInto(root)",
          "alias-routing: view.Rebuild()",
          "alias-routing: view.BumpMacroEpoch()",
          "alias-routing: view.SetEvaluator | view.SetRetainedMeter | view.SetLazyLayer",
          "alias-routing: raw root.SetEvaluator | SetRetainedMeter | SetLazyLayer",
          "alias-routing: view read that materializes a lazy name"
        ],
        "codeScope": "Per-chunk code scope, replacing the seam codeTasks where they differ. c2's Abort only clears root.reg and drops entries. c3's beforeWrite creates an entry when none exists and otherwise only advances last/lastVer, with no rebase. c4 adds the restore and the rebase, and restored cells get version +1, but NameGen and MacroEpoch are not bumped. c6 adds the NameGen/MacroEpoch bumps and Rebuild pinning.",
        "redNotes": [
          "Helpers are file-local with chunk-specific names; there is no shared helpers file. Each file declares its own panic-recovering mutation helper (reports with t.Fatalf) and its own entry lookup that calls t.Fatalf on a missing reg.entries key before any field access.",
          "Describe failures in prose: a recovered nil-pointer panic prints \"runtime error\", which the red check reads as a non-assertion failure.",
          "TestRegistration_MergeIntoSelfRefused: use a fresh root per case. Against c1 (view == root) a self-merge leaves a goroutine holding root.mu RLock with a Lock pending, so later cases on the same root block behind it and each burns the 2s timeout."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.3"
      ],
      "redTests": [
        "TestRegistration_ViewHoldsNoBindingsAfterMerge",
        "TestRegistration_FindOwnerWriteIsAttributed",
        "TestRegistration_ChildScopeWriteIsAttributed",
        "TestRegistration_EvaluatorReentryWriteIsAttributed",
        "TestRegistration_CapturedClosureWriteIsAttributed",
        "TestRegistration_MergeIntoViewIsAttributed",
        "TestRegistration_MergeFromViewReadsRoot",
        "TestRegistration_MergeIntoSelfRefused",
        "TestRegistration_AbortRestoresConfiguration",
        "TestRegistration_AbortKeepsHostConfiguration"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_ViewHoldsNoBindingsAfterMerge|TestRegistration_FindOwnerWriteIsAttributed|TestRegistration_ChildScopeWriteIsAttributed|TestRegistration_EvaluatorReentryWriteIsAttributed|TestRegistration_CapturedClosureWriteIsAttributed|TestRegistration_MergeIntoViewIsAttributed|TestRegistration_MergeFromViewReadsRoot|TestRegistration_MergeIntoSelfRefused|TestRegistration_AbortRestoresConfiguration|TestRegistration_AbortKeepsHostConfiguration)$'",
      "verify": "go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -skip '^(TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$' && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "c6",
      "taskIds": [
        "1.1",
        "2.5"
      ],
      "prev": "c5",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "identity-counters",
      "shard": "",
      "redAfter": "c1-inert-api",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/registration_counters_test.go",
          "symbol": "new",
          "anchor": "",
          "change": "red tests: TestRegistration_AbortInvalidatesCellCaches, TestRegistration_AbortBumpsMacroEpoch, TestRegistration_AbortWithoutOwnedEntriesKeepsCounters, TestRegistration_RebuildDuringOperationKeepsOwnedTombstone",
          "new": true
        },
        {
          "task": "2.5",
          "file": "core/env.go",
          "symbol": "Rebuild",
          "anchor": "func (e *Env) Rebuild() (freedBytes, freedSlots int64) {",
          "change": "while a registration is active keep every tombstoned cell that is some entry.last (pinned: stays in the map, counted as cell.retainedBytes + 1 slot, not released, rebuilt untouched)"
        },
        {
          "task": "2.5",
          "file": "core/registration.go",
          "symbol": "new",
          "anchor": "",
          "change": "counter bumps in Abort: when the abort restores at least one entry, newNameGen.Add(1) and macroEpoch++ exactly once each, directly under the held root.mu (never BumpMacroEpoch, which re-locks); restoring nothing leaves both unchanged",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "live-op",
          "tomb-op",
          "pinned-tomb-op",
          "tomb-host",
          "tomb"
        ],
        "transitions": [
          {
            "input": "root.Rebuild() while active, cell == entry.last",
            "state": "tomb-op|tomb-host",
            "effect": "no-op",
            "evidence": "plan ruling: pinned; stays in the map, counted as cell.retainedBytes + 1 slot, not released, rebuilt flag untouched (tomb-op becomes pinned-tomb-op)"
          },
          {
            "input": "root.Rebuild() while active, tombstone of an unjournaled name",
            "state": "tomb",
            "effect": "clear",
            "evidence": "env.go:897-906 unchanged: dropped and released"
          },
          {
            "input": "abort",
            "state": "pinned-tomb-op",
            "effect": "set",
            "evidence": "spec: restore prior cells in place"
          },
          {
            "input": "root.Rebuild() after finish",
            "state": "tomb",
            "effect": "clear",
            "evidence": "ADR 0012: the next Rebuild releases former pins and aborted additions"
          },
          {
            "input": "abort restoring at least 1 entry",
            "state": "live-op|tomb-op",
            "effect": "forced",
            "evidence": "spec: keep versions/NameGen/MacroEpoch monotone and invalidate cached lookups; each restored cell version +1, NameGen +1, MacroEpoch +1"
          },
          {
            "input": "VM site hit after abort",
            "state": "live-op",
            "effect": "clear",
            "evidence": "vm.go:1609-1618: the version/gen mismatch falls back to a locked ReadCell of the restored cell"
          }
        ],
        "forbidden": [
          "any decrease of Cell.version, NameGen or MacroEpoch",
          "abort restoring a historical counter value",
          "Rebuild dropping or releasing a tombstoned cell that is an active entry's last",
          "abort installing a fresh *Cell"
        ],
        "seeding": [
          "pinned-tomb-op: root.Set(x) before Begin; view.Delete(x); root.Rebuild()",
          "held cell: c, _ := root.Cell(x) before Begin; view.Set(x, op); reg.Abort(); read root.ReadCellSnapshot(c)",
          "VM site: vm.New(root) running an OpGetGlobal x chunk (pattern vm_test.go:1723-1762) after view.Set(x, op); then reg.Abort(), vm.Reset(), rerun",
          "macro: root.SetEvaluator(NewEvaluator()) before Begin; define the macro through view.Evaluator().Eval(ctx, (defmacro m [a] a), view), then read root.MacroEpoch()"
        ],
        "budgets": [
          "per restoring abort: NameGen +1, MacroEpoch +1, each restored cell version +1, and +1 on the op cell tombstoned by a ReplaceCell revert",
          "Rebuild with an active registration: +1 map lookup per tombstoned cell; without one: +1 nil branch"
        ],
        "closes": [
          "identity-counters: root.Rebuild() while active, cell == entry.last",
          "identity-counters: root.Rebuild() while active, tombstone of an unjournaled name",
          "identity-counters: abort [pinned-tomb-op]",
          "identity-counters: root.Rebuild() after finish",
          "identity-counters: abort restoring at least 1 entry"
        ],
        "codeScope": "Per-chunk code scope, replacing the seam codeTasks where they differ. c2's Abort only clears root.reg and drops entries. c3's beforeWrite creates an entry when none exists and otherwise only advances last/lastVer, with no rebase. c4 adds the restore and the rebase, and restored cells get version +1, but NameGen and MacroEpoch are not bumped. c6 adds the NameGen/MacroEpoch bumps and Rebuild pinning.",
        "redNotes": [
          "Helpers are file-local with chunk-specific names; there is no shared helpers file. Each file declares its own panic-recovering mutation helper (reports with t.Fatalf) and its own entry lookup that calls t.Fatalf on a missing reg.entries key before any field access.",
          "Describe failures in prose: a recovered nil-pointer panic prints \"runtime error\", which the red check reads as a non-assertion failure."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.5"
      ],
      "redTests": [
        "TestRegistration_AbortInvalidatesCellCaches",
        "TestRegistration_AbortBumpsMacroEpoch",
        "TestRegistration_AbortWithoutOwnedEntriesKeepsCounters",
        "TestRegistration_RebuildDuringOperationKeepsOwnedTombstone"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run '^(TestRegistration_AbortInvalidatesCellCaches|TestRegistration_AbortBumpsMacroEpoch|TestRegistration_AbortWithoutOwnedEntriesKeepsCounters|TestRegistration_RebuildDuringOperationKeepsOwnedTombstone)$'",
      "verify": "go build ./core/... && go vet ./core/... && go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "c7-validate-docs",
      "taskIds": [
        "3.1"
      ],
      "prev": "c6",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "validation-docs",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "github.com/victorzhuk/go-lispico/core",
        "github.com/victorzhuk/go-lispico/core/vm"
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "CONTEXT.md",
          "symbol": "Owned capacity",
          "anchor": "**Owned capacity**:",
          "change": "add a **Registration view** entry next to it: forwards to its root; writes attributed while the operation is active; abort reverts owned writes in place; added names are tombstoned and released by the next Rebuild"
        },
        {
          "task": "3.1",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Added: Env.BeginRegistration, Registration.Env/Complete/Abort, CodeRegistrationActive; Changed: a merge whose source and target resolve to the same environment returns an EvalError instead of blocking"
        },
        {
          "task": "3.1",
          "file": "CLAUDE.md",
          "symbol": "Architecture tree",
          "anchor": "├── env.go      # Environment chain (lexical scope)",
          "change": "add core/registration.go to the tree"
        },
        {
          "task": "3.1",
          "file": "docs/adr/0012-retained-state-owned-capacity-accounting.md",
          "symbol": "Rebuild decision",
          "anchor": "**`Rebuild()` for in-place compaction.** `(*Env).Rebuild()` compacts the",
          "change": "amend the Rebuild decision: while a registration is active, Rebuild keeps every tombstoned cell that is some journal entry's last write (not released, still counted) until the operation closes; aborted additions stay tombstoned and are released by the next Rebuild"
        },
        {
          "task": "3.1",
          "file": "docs/adr/0003-concurrency-model.md",
          "symbol": "Consequences",
          "anchor": "- Environments remain individually synchronized;",
          "change": "note: a registration view forwards to its root and takes the root lock; the journal is updated under the same lock as the mutation"
        }
      ],
      "contract": {
        "states": [
          "validated"
        ],
        "transitions": [
          {
            "input": "all 2.x chunks landed",
            "state": "validated",
            "effect": "no-op",
            "evidence": "tasks.md 3.1"
          }
        ],
        "forbidden": [
          "editing the expectations of existing TestEnv_* or TestMerge* tests",
          "creating new architecture docs (update CONTEXT.md only)"
        ],
        "seeding": [
          "validated: reached only after c6 lands, by running the packet's verifyCommands in order, then make lint && make test; no test seeds it"
        ],
        "budgets": [
          "focused run within -timeout 2m; race run within -timeout 5m"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [],
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm && go test -race -timeout 5m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)' && golangci-lint run ./core/... && openspec validate registration-journal --strict --json && git diff --quiet master -- go.mod go.sum core/plugin.go",
      "race": "task 3.1 names the race run over the whole regex",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "audit",
      "tasks": [
        "0.1"
      ],
      "summary": "NO-RED-WAIVER: inspection-only baseline with no observable contract. NO-TESTER-WAIVER: the deliverable is this ownership checklist, verified at 396afc06. WALK-INHERITED through view.parent=root, no code change: Get, GetCanonical, GetFunc, GetFuncCanonical, GetMaterializedCanonical, GetMaterializedFuncCanonical, Cell, FuncCell (env.go:479-785) and lookupBoundMacro's direct e.parent/e.funcs/e.vars walk (eval.go:1346-1365); view maps are nil so each walk reaches the root. FORWARDED READS via owner(): HasLive, HasLiveFunc (env.go:141-158), ReadCell, ReadCellSnapshot (546-559), CellLocal, FuncCellLocal (591-626), NameGen (632), MacroEpoch (644), Evaluator (845), LazyLayer (92), RetainedMeter (107), RetainedUsage (881), FuncNames, LocalFuncNames, VarNames, LocalNames (949-997). OWNER-AWARE: Find (788). FORWARDED WRITES carrying r: Set*, SetCanonical*, SetFunc*, SetFuncCanonical*, SetBoth* (260-475, 676-738), ReplaceCell* (393-423), Delete (861), RegisterValue*, RegisterSource (116-139; the layer receives the view), MergeInto/MergeIntoCanonical as source and target (1087-1129), Rebuild (889, forwarded, unattributed), BumpMacroEpoch (637), SetEvaluator (850), SetLazyLayer (83), SetRetainedMeter (100). LEXICAL: Child/ChildVariadic through NewEnv(view) (160-171, 810-842); the forkCells receiver is always a loop child (eval.go:1931). DIRECT FIELD ACCESS OUTSIDE env.go: eval.go:1347-1357 (read-only walk, safe); metering.go:372-373 (pending.env is always the owning root, because recordFreshRetained runs on it; the raw SetRetainedMeter marks config foreign); metering.go:412-420 (retained fields only). core/vm has no private Env field access. It writes only through env.SetWithContext (vm.go:1087), env.Find + owner.SetWithContext (vm.go:1110-1123) and core.BindMacro (vm.go:1099), and caches cells through CellLocal/FuncCellLocal/ReadCell/ReadCellSnapshot/NameGen/Cell.Version (vm.go:1604-1666). Closures capture the frame env (vm.go:1331) and run with it (vm.go:2231, 2255). runtime runVM calls SetGlobals(env) (runtime/eval.go:577), so env == vm.globals holds for a view and the site publishes through the forwarded CellLocal/NameGen. The chunk cache keys MacroEpoch on be.globals, the root (runtime/eval.go:506, 525). Every Cell v/canonical assignment in env.go bumps the version under the owner lock (309-317, 383-385, 468-470, 696-698, 731-733, 864-871, 1073-1078); vm.go:1014/1043 write cellBox, not Cell. Bounded command: go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)' (Makefile GOTESTFLAGS ?= -timeout 2m).",
      "contract": {
        "states": [
          "checklist-recorded"
        ],
        "transitions": [
          {
            "input": "first source change of the change",
            "state": "checklist-recorded",
            "effect": "no-op",
            "evidence": "tasks.md 0.1"
          }
        ],
        "forbidden": [
          "source edits before the checklist is recorded"
        ],
        "seeding": [
          "checklist-recorded: reached only by recording the audit seam's ownership checklist in the change's apply notes before c1; no test seeds it, and its budget is checked by review against env.go's exported method list"
        ],
        "budgets": [
          "checklist covers 100% of exported Env methods and every direct Env/Cell field access in core and core/vm"
        ]
      }
    },
    {
      "id": "view-forwarding",
      "tasks": [
        "2.1"
      ],
      "summary": "A view is a *Env with parent=root and nil vars/funcs, holding eval and retained limits snapshotted from the root under root.mu at Begin, with reg=r. Get-family walks reach the root through the parent link at no cost to non-view envs; every non-walking method forwards through owner() or viewReg(). Lifecycle: idle, then active, then finished. FIELD-FIRST chunk (lands before the red stage, no behavior): add core/registration.go with type Registration struct{ root, view *Env; entries map[registrationKey]*registrationEntry } and the types registrationKey and registrationEntry exactly as named. Also add func (e *Env) BeginRegistration() (*Registration, error) returning &Registration{root: e, view: e}, nil; func (r *Registration) Env() *Env returning r.view; Complete() and Abort() with empty bodies; CodeRegistrationActive and NewRegistrationActiveError() in core/error.go; the Env field reg atomic.Pointer[Registration]; and the cell0Used removal (TestEnv_Size stays 208). With the inert view equal to the root, every red test compiles and fails on an assertion.",
      "contract": {
        "states": [
          "idle",
          "active",
          "finished",
          "active-other"
        ],
        "transitions": [
          {
            "input": "root.BeginRegistration()",
            "state": "idle",
            "effect": "set",
            "evidence": "spec: root identity SHALL NOT change; root.reg=r, view.reg=r, view.parent=root, view != root"
          },
          {
            "input": "root.BeginRegistration()",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: one active registration per root; returns *LispicoError with Code == CodeRegistrationActive, root.reg unchanged"
          },
          {
            "input": "view.BeginRegistration()",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: a view resolves to its root; refused with CodeRegistrationActive"
          },
          {
            "input": "view.BeginRegistration()",
            "state": "finished",
            "effect": "set",
            "evidence": "plan ruling: resolves to its root and starts r2 with a new view"
          },
          {
            "input": "r.Complete()",
            "state": "active",
            "effect": "clear",
            "evidence": "design.md: after completion the retained view forwards without an active operation"
          },
          {
            "input": "r.Abort()",
            "state": "active",
            "effect": "clear",
            "evidence": "spec: after completion or abort the view SHALL forward as an ordinary unattributed environment"
          },
          {
            "input": "r.Complete() or r.Abort()",
            "state": "finished",
            "effect": "no-op",
            "evidence": "plan ruling: idempotent"
          },
          {
            "input": "r.Complete() or r.Abort()",
            "state": "active-other",
            "effect": "no-op",
            "evidence": "plan ruling: root.reg == r2 != r; r2 unaffected"
          },
          {
            "input": "view read: Get, GetCanonical, GetFunc, GetFuncCanonical, Cell, FuncCell, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, LocalNames, VarNames, NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, RetainedUsage, Evaluator, LazyLayer",
            "state": "idle|active|finished|active-other",
            "effect": "no-op",
            "evidence": "spec: SHALL forward every read to that root; design.md surface table"
          },
          {
            "input": "raw root rebind, then a view read",
            "state": "finished",
            "effect": "no-op",
            "evidence": "spec scenario Completed view keeps forwarding: the view observes the rebinding"
          }
        ],
        "forbidden": [
          "view == root",
          "view.vars or view.funcs non-nil at any time",
          "view.reg changed after BeginRegistration returns",
          "two registrations active on one root",
          "root.reg holding a registration whose root is another env",
          "holding root.mu while acquiring any view mutex",
          "a Registration built by struct literal outside BeginRegistration (tests included)"
        ],
        "seeding": [
          "idle: NewEnv(nil) or NewEnvWithRetainedLimits(nil, b, s), no BeginRegistration",
          "active: reg, err := root.BeginRegistration() with err == nil",
          "finished: reg.Complete() or reg.Abort() after Begin, keeping view := reg.Env()",
          "active-other: after reg1 finishes, reg2, _ := root.BeginRegistration()"
        ],
        "budgets": [
          "unsafe.Sizeof(Env{}) == 208 (TestEnv_Size unchanged)",
          "Get through a view: 0 allocs; extra cost = one RLock/RUnlock of the view mutex + one nil-map probe + one atomic lazyLayer load, then the root's normal path",
          "Get/GetCanonical/GetFunc/GetFuncCanonical/GetMaterialized*/Cell/FuncCell on non-view envs: +0 instructions (code unchanged)",
          "forwarded non-walking reads (NameGen, MacroEpoch, ReadCell, ReadCellSnapshot, CellLocal, FuncCellLocal, HasLive, HasLiveFunc, Find per level, Evaluator, LazyLayer, RetainedUsage, RetainedMeter, Local*Names, *Names): +1 atomic pointer load + 1 nil branch on every env; NameGen sits on the VM site-hit path (vm.go:1607, 1641)",
          "BeginRegistration: 2 allocs (Registration, view Env); entries map allocated on first op write"
        ]
      },
      "redTasks": [
        "TestRegistration_ViewForwardsReadsAndKeepsRootIdentity",
        "TestRegistration_NestedBeginRefused",
        "TestRegistration_CompleteAndAbortAreIdempotent",
        "TestRegistration_ViewGetZeroAllocs"
      ],
      "codeTasks": [
        "FIELD-FIRST inert members exactly as listed in the summary — lands before the red stage",
        "BeginRegistration: root := e.owner(); lock root.mu; refuse when root.reg.Load() != nil; view := &Env{parent: root, eval: root.eval, maxRetainedBytes: root.maxRetainedBytes, maxRetainedSlots: root.maxRetainedSlots}; view.reg.Store(r); root.reg.Store(r); unlock",
        "Env(); Complete: lock root.mu, if root.reg.Load() == r clear it and drop entries; Abort calls the restore from the abort-ownership seam",
        "lazy() field accessor; LazyLayer() forwards to owner(); switch the internal miss paths to lazy()",
        "forward the non-walking reads through owner(): HasLive, HasLiveFunc, ReadCell, ReadCellSnapshot, CellLocal, FuncCellLocal, NameGen, MacroEpoch, Evaluator, RetainedMeter, RetainedUsage, VarNames, FuncNames, LocalNames, LocalFuncNames",
        "localCell: replace cell0Used with e.cell0.version.Load() != 0, plus a WHY comment stating the invariant that every localCell caller bumps the returned cell's version before the next localCell under the same lock"
      ]
    },
    {
      "id": "write-routing",
      "tasks": [
        "2.2"
      ],
      "summary": "Every public mutator runs one check: if r := e.viewReg(); r != nil it forwards to r.root carrying r, otherwise it runs the raw path with r == nil. Under root.mu the internal path computes j := root.active(r), which is r only while root.reg == r. It calls j.beforeWrite(key, cur) after capacity reservation succeeds and immediately before mutating, then j.afterWrite(key, cell). Raw writes never touch the journal; foreign-write detection is deferred until the next op write or abort and compares cell identity and version.",
      "contract": {
        "states": [
          "absent",
          "tomb",
          "live-prior",
          "live-op",
          "tomb-op",
          "live-host"
        ],
        "transitions": [
          {
            "input": "view Set/SetWithContext",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:364-390; spec: writes reached through the view SHALL be attributed"
          },
          {
            "input": "view SetCanonical/SetCanonicalWithContext",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:449-475"
          },
          {
            "input": "view SetFunc*/SetFuncCanonical*",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:680-738; key fn=true"
          },
          {
            "input": "view SetBoth*/SetBothCanonical*",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:280-324; one entry per namespace"
          },
          {
            "input": "view ReplaceCell/ReplaceCellWithContext",
            "state": "absent|tomb|live-prior",
            "effect": "set",
            "evidence": "env.go:398-423; entry.last is the new cell, entry.prior the old one"
          },
          {
            "input": "view Delete",
            "state": "live-prior|live-op",
            "effect": "set",
            "evidence": "env.go:861-878; journals only the namespaces whose cell is live or canonical; layer.TombstoneForDelete still receives the root"
          },
          {
            "input": "view Delete",
            "state": "absent|tomb",
            "effect": "no-op",
            "evidence": "env.go:863, 868 guards: no mutation, no entry"
          },
          {
            "input": "view RegisterValue with no lazy layer",
            "state": "absent|live-prior",
            "effect": "set",
            "evidence": "env.go:120-128: the eager SetCanonical/Set runs on the view"
          },
          {
            "input": "view write refused by capacity (ResourceLimitError)",
            "state": "absent|tomb|live-prior|live-op",
            "effect": "no-op",
            "evidence": "env.go:190-200; beforeWrite runs only after prepareFreshRetained succeeds"
          },
          {
            "input": "view write repeated on the same key with no foreign write between",
            "state": "live-op|tomb-op",
            "effect": "set",
            "evidence": "design.md: before-image kept, only last/lastVer advance"
          },
          {
            "input": "raw root write (any mutator) on a journaled key",
            "state": "live-op|tomb-op",
            "effect": "forced",
            "evidence": "spec: raw-root writes SHALL be unattributed; the version bump makes the entry foreign (live-host) at abort; the journal is untouched at write time"
          },
          {
            "input": "raw root write with a value equal to the op's",
            "state": "live-op",
            "effect": "forced",
            "evidence": "spec: unattributed even when equal; Set always bumps the version (env.go:385)"
          },
          {
            "input": "view write after finish or under another registration",
            "state": "absent|tomb|live-prior|live-host",
            "effect": "no-op",
            "evidence": "root.active(r) returns nil, so the forward is unattributed"
          }
        ],
        "forbidden": [
          "an entry created for a write that did not mutate (capacity refusal; Delete of an absent or non-canonical tombstone)",
          "the raw write path reading or writing r.entries",
          "a Cell v/canonical assignment without version.Add(1) under the owner lock",
          "entry.last == nil after afterWrite",
          "a forwarded write reaching localCell/localFuncCell on the view"
        ],
        "seeding": [
          "live-prior: root.Set/SetFunc/SetCanonical/SetFuncCanonical before BeginRegistration",
          "tomb: root.Set then root.Delete before BeginRegistration",
          "absent: name never written",
          "live-op: reg.Env().Set* while active",
          "tomb-op: reg.Env().Delete of a live-prior name while active",
          "live-host: a view write, then raw root.Set on the same name"
        ],
        "budgets": [
          "raw public mutator on an env with no registration: +1 atomic pointer load + 1 nil branch, 0 extra allocs",
          "raw rebind of a live name on a root with an active registration: 0 allocs",
          "journal: at most one entry per distinct (namespace, name) written through the view; 1000 view rebinds of one name keep len(reg.entries) == 1",
          "view write: +1 map lookup under the root lock, +1 entry alloc on the first write per key"
        ]
      },
      "redTasks": [
        "TestRegistration_ViewHoldsNoBindings",
        "TestRegistration_ViewWriteRecordsBeforeImage",
        "TestRegistration_HostEqualValueRebindIsUnowned",
        "TestRegistration_RegisterValueThroughViewIsAttributed",
        "TestRegistration_RawRebindDuringOperationZeroAllocs",
        "TestRegistration_JournalBoundedByDistinctNames"
      ],
      "codeTasks": [
        "thread a trailing r *Registration through setBoth and the Set/SetCanonical/SetFunc/SetFuncCanonical/ReplaceCell internal paths; the public methods forward through viewReg()",
        "Delete: forward; journal each namespace inside the existing live-or-canonical guard; call layer.TombstoneForDelete(root, name) outside the lock as today",
        "RegisterValueWithContext/RegisterSource: take the layer from e.LazyLayer() (the owner's), pass e (the view) to the layer, keep the eager fallback on e",
        "beforeWrite/afterWrite in core/registration.go; allocate entries on the first write"
      ]
    },
    {
      "id": "alias-routing",
      "tasks": [
        "2.3"
      ],
      "summary": "Owner-aware and alias surfaces. When the owner is its root, Find on a view returns the view itself, so evalSet (eval.go:1535-1543) and OpSetLexical (vm.go:1110-1123) write through the view. A strict ancestor above the root is returned raw and sits outside the guarantee; the runtime root has parent nil (runtime/engine.go:288). Child/ChildVariadic give ordinary NewEnv(view) children: their local writes stay local and unjournaled, and their Find/set! reaches the view. Evaluator reentry, closure capture (Lambda.Env at eval.go:1260/1289/1405, VM NewClosure at vm.go:1331) and VM frames (SetGlobals at runtime/eval.go:577) all carry the view pointer, so writes stay attributed until the registration finishes. mergeInto resolves source and target through owner(), takes r from target.viewReg(), and locks source RLock then target Lock exactly as today; it refuses a self-merge before taking any lock. Config setters forward and are journaled; raw config setters mark the field foreign. lookupBoundMacro needs no change: view maps are nil, so its walk reaches the root.",
      "contract": {
        "states": [
          "active",
          "finished",
          "cfg-prior",
          "cfg-op",
          "cfg-host"
        ],
        "transitions": [
          {
            "input": "view.Find(name), name owned by the root",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "design.md table: Find returns an owner-aware view; returns the view, never the raw root"
          },
          {
            "input": "owner.Set* on the Find result",
            "state": "active",
            "effect": "set",
            "evidence": "spec: writes reached through an owner returned by lookup"
          },
          {
            "input": "view.Find(name), name owned above the root",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "plan ruling: returns the raw ancestor, outside the guarantee"
          },
          {
            "input": "child := view.Child(); child.Set(local)",
            "state": "active",
            "effect": "no-op",
            "evidence": "design.md: preserve lexical child scopes; child-local, no entry"
          },
          {
            "input": "set! of a root name evaluated in a Child or ChildVariadic scope of the view",
            "state": "active",
            "effect": "set",
            "evidence": "spec: a child scope"
          },
          {
            "input": "view.Evaluator().Eval(ctx, (def x v) | (set! x v) | (defmacro m [a] a), view)",
            "state": "active",
            "effect": "set",
            "evidence": "spec: evaluator reentry; eval.go:1217, 1543, 1306-1323"
          },
          {
            "input": "Apply of a Lambda whose Env is the view, body (set! x v)",
            "state": "active",
            "effect": "set",
            "evidence": "spec: a closure capturing the view"
          },
          {
            "input": "the same closure applied after finish",
            "state": "finished",
            "effect": "no-op",
            "evidence": "spec: forwards unattributed"
          },
          {
            "input": "src.MergeInto(view) or src.MergeIntoCanonical(view)",
            "state": "active",
            "effect": "set",
            "evidence": "spec: a merge whose target is the view; env.go:1097-1129"
          },
          {
            "input": "view.MergeInto(other)",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "plan ruling: the source resolves to the root and copies root locals; no journal, the target is not the root"
          },
          {
            "input": "view.MergeInto(root) | root.MergeInto(view) | view.MergeInto(view) | root.MergeInto(root)",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "plan ruling: owner(src) == owner(dst) returns EvalError \"merge source and target are the same environment\" before any lock; today root.MergeInto(root) self-deadlocks (RLock then Lock on one RWMutex, env.go:1098-1101)"
          },
          {
            "input": "view.Rebuild()",
            "state": "active|finished",
            "effect": "no-op",
            "evidence": "forwarded to root.Rebuild; compaction is not a binding write"
          },
          {
            "input": "view.BumpMacroEpoch()",
            "state": "active|finished",
            "effect": "forced",
            "evidence": "root MacroEpoch +1, never journaled"
          },
          {
            "input": "view.SetEvaluator | view.SetRetainedMeter | view.SetLazyLayer",
            "state": "cfg-prior|cfg-host",
            "effect": "set",
            "evidence": "design.md table: journal any root configuration mutation reached through the view (the state becomes cfg-op)"
          },
          {
            "input": "raw root.SetEvaluator | SetRetainedMeter | SetLazyLayer",
            "state": "cfg-op",
            "effect": "forced",
            "evidence": "same conflict policy: marks the field foreign (cfg-host) under root.mu"
          },
          {
            "input": "view read that materializes a lazy name",
            "state": "active",
            "effect": "no-op",
            "evidence": "plan ruling: layer.LookupAndMaterialize receives the root and the install is unattributed; attributing materialization belongs to plugin-binding-rollback"
          }
        ],
        "forbidden": [
          "Find on a view returning the raw root",
          "mergeInto locking a view mutex, or one root mutex twice",
          "any lock order other than resolved-source RLock then resolved-target Lock",
          "layer.LookupAndMaterialize, ForceAll or TombstoneForDelete called with a view env",
          "SetLazyLayer storing without e.mu (the raw path now locks; cold path, one caller at runtime/lazy_template.go:699)"
        ],
        "seeding": [
          "Find owner: root.Set(x) before Begin; owner, _ := view.Find(x)",
          "child scope: child := view.Child(), or view.ChildVariadic(params, args, Symbol{}); drive set! with view.Evaluator().Eval(ctx, form, child)",
          "evaluator reentry: root.SetEvaluator(NewEvaluator()) BEFORE BeginRegistration, because the view snapshots eval",
          "closure: lam, _ := view.Evaluator().Eval(ctx, (fn [] (set! x 3)), view); then view.Evaluator().Apply(ctx, lam, nil, view)",
          "merge target: src := NewEnv(nil); src.Set/SetFunc; src.MergeInto(reg.Env())",
          "cfg-prior: raw root setter before Begin; cfg-op: view setter while active; cfg-host: view setter, then raw root setter"
        ],
        "budgets": [
          "Find per scope level: +1 atomic load + 1 nil branch",
          "self-merge refusal: 0 locks taken",
          "config setters: cold path, one root lock hold each"
        ]
      },
      "redTasks": [
        "TestRegistration_ViewHoldsNoBindingsAfterMerge",
        "TestRegistration_FindOwnerWriteIsAttributed",
        "TestRegistration_ChildScopeWriteIsAttributed",
        "TestRegistration_EvaluatorReentryWriteIsAttributed",
        "TestRegistration_CapturedClosureWriteIsAttributed",
        "TestRegistration_MergeIntoViewIsAttributed",
        "TestRegistration_MergeFromViewReadsRoot",
        "TestRegistration_MergeIntoSelfRefused",
        "TestRegistration_AbortRestoresConfiguration",
        "TestRegistration_AbortKeepsHostConfiguration"
      ],
      "codeTasks": [
        "Find: owner-aware branch for views",
        "mergeInto: src := e.owner(); dst := target.owner(); r := target.viewReg(); refuse src == dst; lock src.mu.RLock then dst.mu.Lock; dst.applyMergePlan(&plan, dst.active(r)) calling beforeWrite/afterWrite per commit",
        "Rebuild and BumpMacroEpoch forward to owner()",
        "SetEvaluator/SetRetainedMeter/SetLazyLayer: forward through viewReg(); journal via Registration config before-images under root.mu; raw setters mark the field foreign when root.reg is non-nil; SetLazyLayer takes e.mu"
      ]
    },
    {
      "id": "abort-ownership",
      "tasks": [
        "1.1",
        "2.4"
      ],
      "summary": "Owns task 1.1: author every redTask listed across all seams and run the red stage before any 2.x behavior lands, after the FIELD-FIRST inert chunk. Also owns 2.4. Abort holds root.mu once and checks every entry: cur := map[key]; the entry is owned iff cur == entry.last && cur.Version() == entry.lastVer. Owned entries are restored per abortTable; unowned ones are skipped. Then, if restored > 0, it runs newNameGen.Add(1) and macroEpoch++ directly (BumpMacroEpoch would re-lock). Config fields are restored when recorded && !foreign. Finally it clears root.reg and drops entries. Abort makes no meter calls, creates no cells and makes no lazy-layer calls. Rebase in beforeWrite: when cur != entry.last or the version moved, the before-image becomes cur (nil when absent) with its v/canonical.",
      "contract": {
        "states": [
          "absent",
          "tomb",
          "live-prior",
          "live-op",
          "tomb-op",
          "live-host",
          "tomb-host",
          "replaced-host"
        ],
        "transitions": [
          {
            "input": "abort",
            "state": "live-op",
            "effect": "set",
            "evidence": "spec: restore prior value, canonical status, presence when the current state is still the op's latest write"
          },
          {
            "input": "abort",
            "state": "tomb-op",
            "effect": "set",
            "evidence": "spec: deletion state restored; the op delete is reverted in the same cell"
          },
          {
            "input": "abort of an op-added name",
            "state": "live-op",
            "effect": "clear",
            "evidence": "spec: added names SHALL be absent; tombstone in place (ADR 0012: Rebuild is the only release path)"
          },
          {
            "input": "abort",
            "state": "live-host",
            "effect": "no-op",
            "evidence": "spec: SHALL otherwise leave the current state untouched"
          },
          {
            "input": "abort",
            "state": "tomb-host",
            "effect": "no-op",
            "evidence": "design.md: host deletion counts equally"
          },
          {
            "input": "abort",
            "state": "replaced-host",
            "effect": "no-op",
            "evidence": "map cell != entry.last"
          },
          {
            "input": "view write",
            "state": "live-host|tomb-host|replaced-host",
            "effect": "set",
            "evidence": "design.md: advance the before-image to the foreign state, then apply; the entry becomes live-op or tomb-op"
          },
          {
            "input": "raw write interleaved concurrently with view writes, then abort",
            "state": "live-op|live-host",
            "effect": "no-op",
            "evidence": "the per-key rule runs under root.mu; no written key ends holding an op-only value"
          }
        ],
        "forbidden": [
          "abort writing a key whose map cell != entry.last or whose version moved",
          "abort installing a cell other than entry.prior, or creating a cell",
          "abort calling a meter, the lazy layer, or BumpMacroEpoch",
          "entries surviving Complete or Abort",
          "after abort, a key the op wrote holding an op-written value (it holds its before-image or a host-written state)"
        ],
        "seeding": [
          "op, host, abort: view write, raw root write on the same name, reg.Abort()",
          "op, host, op, abort: view write, raw root write, view write, reg.Abort()",
          "host delete/recreate: view write, raw root.Delete, raw root.Set, reg.Abort()",
          "host ReplaceCell: view write, raw root.ReplaceCell, reg.Abort()",
          "every foreign-write test also writes an op-only control name that must be restored, keeping it red against the inert stage"
        ],
        "budgets": [
          "abort: one root lock hold, O(len(entries)), 0 allocs, 0 meter calls, 0 new cells",
          "NameGen +1 and MacroEpoch +1 exactly once per abort that restores at least 1 entry; +0 otherwise",
          "race test: 4 goroutines x 200 iterations, well inside -timeout 5m under -race"
        ]
      },
      "redTasks": [
        "TestRegistration_AbortRestoresOverwrittenBindings",
        "TestRegistration_AbortRemovesAddedNames",
        "TestRegistration_AbortRestoresCanonicalStatus",
        "TestRegistration_AbortRestoresDeletedBinding",
        "TestRegistration_AbortRestoresReplacedCell",
        "TestRegistration_HostEqualValueRebindSurvivesAbort",
        "TestRegistration_HostRebindAfterOperationSurvivesAbort",
        "TestRegistration_HostAddSurvivesAbort",
        "TestRegistration_HostDeleteAfterOperationSurvivesAbort",
        "TestRegistration_HostDeleteRecreateSurvivesAbort",
        "TestRegistration_HostReplaceCellSurvivesAbort",
        "TestRegistration_OperationHostOperationAbortKeepsHostState",
        "TestRegistration_CompletedViewForwardsUnattributed",
        "TestRegistration_AbortedViewForwardsUnattributed",
        "TestRegistration_ConcurrentHostWritesRace",
        "TestRegistration_VMSiteDropsRevertedDefinition"
      ],
      "codeTasks": [
        "Abort body in core/registration.go per abortTable",
        "foreign rebase inside beforeWrite",
        "config restore and clearing of root.reg and entries"
      ]
    },
    {
      "id": "identity-counters",
      "tasks": [
        "2.5"
      ],
      "summary": "Cell identity and cache invalidation. Abort restores into the cells that were in the map, or reinstalls entry.prior after a ReplaceCell. It bumps every touched cell's version and bumps NameGen and MacroEpoch once. So after a reverting abort, every cache that could hold a reverted definition misses: VM site entries (guarded by env identity, NameGen and cell version, vm.go:1604-1666), runtime call handles (Version compare, runtime/func.go:144, runtime/call_cache.go:58) and the bytecode chunk cache (keyed on MacroEpoch, runtime/eval.go:506). A Rebuild while a registration is active pins tombstoned entry.last cells, so the op's tombstone and its capacity survive until abort.",
      "contract": {
        "states": [
          "live-op",
          "tomb-op",
          "pinned-tomb-op",
          "tomb-host",
          "tomb"
        ],
        "transitions": [
          {
            "input": "root.Rebuild() while active, cell == entry.last",
            "state": "tomb-op|tomb-host",
            "effect": "no-op",
            "evidence": "plan ruling: pinned; stays in the map, counted as cell.retainedBytes + 1 slot, not released, rebuilt flag untouched (tomb-op becomes pinned-tomb-op)"
          },
          {
            "input": "root.Rebuild() while active, tombstone of an unjournaled name",
            "state": "tomb",
            "effect": "clear",
            "evidence": "env.go:897-906 unchanged: dropped and released"
          },
          {
            "input": "abort",
            "state": "pinned-tomb-op",
            "effect": "set",
            "evidence": "spec: restore prior cells in place"
          },
          {
            "input": "root.Rebuild() after finish",
            "state": "tomb",
            "effect": "clear",
            "evidence": "ADR 0012: the next Rebuild releases former pins and aborted additions"
          },
          {
            "input": "abort restoring at least 1 entry",
            "state": "live-op|tomb-op",
            "effect": "forced",
            "evidence": "spec: keep versions/NameGen/MacroEpoch monotone and invalidate cached lookups; each restored cell version +1, NameGen +1, MacroEpoch +1"
          },
          {
            "input": "VM site hit after abort",
            "state": "live-op",
            "effect": "clear",
            "evidence": "vm.go:1609-1618: the version/gen mismatch falls back to a locked ReadCell of the restored cell"
          }
        ],
        "forbidden": [
          "any decrease of Cell.version, NameGen or MacroEpoch",
          "abort restoring a historical counter value",
          "Rebuild dropping or releasing a tombstoned cell that is an active entry's last",
          "abort installing a fresh *Cell"
        ],
        "seeding": [
          "pinned-tomb-op: root.Set(x) before Begin; view.Delete(x); root.Rebuild()",
          "held cell: c, _ := root.Cell(x) before Begin; view.Set(x, op); reg.Abort(); read root.ReadCellSnapshot(c)",
          "VM site: vm.New(root) running an OpGetGlobal x chunk (pattern vm_test.go:1723-1762) after view.Set(x, op); then reg.Abort(), vm.Reset(), rerun",
          "macro: root.SetEvaluator(NewEvaluator()) before Begin; define the macro through view.Evaluator().Eval(ctx, (defmacro m [a] a), view), then read root.MacroEpoch()"
        ],
        "budgets": [
          "per restoring abort: NameGen +1, MacroEpoch +1, each restored cell version +1, and +1 on the op cell tombstoned by a ReplaceCell revert",
          "Rebuild with an active registration: +1 map lookup per tombstoned cell; without one: +1 nil branch"
        ]
      },
      "redTasks": [
        "TestRegistration_AbortInvalidatesCellCaches",
        "TestRegistration_AbortBumpsMacroEpoch",
        "TestRegistration_AbortWithoutOwnedEntriesKeepsCounters",
        "TestRegistration_RebuildDuringOperationKeepsOwnedTombstone"
      ],
      "codeTasks": [
        "Rebuild: under e.mu, r := e.reg.Load(); for each tombstoned cell, when r != nil and r.entries[key].last == cell, keep it in the new map and add cell.retainedBytes and 1 slot",
        "counter bumps inside Abort under the held lock"
      ]
    },
    {
      "id": "validation-docs",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-RED-WAIVER: validation and docs only. NO-TESTER-WAIVER: runs the suites authored under 1.1. Run the task-3.1 commands. Confirm no dependency and no Plugin interface change (git diff --stat -- go.mod go.sum core/plugin.go is empty), and that the existing TestEnv_* and TestMerge* tests pass without edits. Docs: add a **Registration view** entry to CONTEXT.md next to **Owned capacity**: the view forwards to its root, its writes are attributed while its operation is active, abort reverts owned writes in place, and added names are tombstoned and released by the next Rebuild. Add a CHANGELOG.md [Unreleased] Added entry for Env.BeginRegistration, Registration.Env, Complete, Abort and CodeRegistrationActive, noting that a self-merge now returns an EvalError instead of blocking.",
      "contract": {
        "states": [
          "validated"
        ],
        "transitions": [
          {
            "input": "all 2.x chunks landed",
            "state": "validated",
            "effect": "no-op",
            "evidence": "tasks.md 3.1"
          }
        ],
        "forbidden": [
          "editing the expectations of existing TestEnv_* or TestMerge* tests",
          "creating new architecture docs (update CONTEXT.md only)"
        ],
        "seeding": [
          "validated: reached only after c6 lands, by running the packet's verifyCommands in order, then make lint && make test; no test seeds it"
        ],
        "budgets": [
          "focused run within -timeout 2m; race run within -timeout 5m"
        ]
      },
      "codeTasks": [
        "CONTEXT.md glossary entry",
        "CHANGELOG.md [Unreleased] Added entry",
        "run the verify commands and fullFloor"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "A registration view over a root environment SHALL forward every read and write to that root, and the root's identity SHALL NOT change.",
      "tests": [
        "TestRegistration_ViewForwardsReadsAndKeepsRootIdentity",
        "TestRegistration_ViewHoldsNoBindings",
        "TestRegistration_ViewHoldsNoBindingsAfterMerge",
        "TestRegistration_MergeFromViewReadsRoot"
      ]
    },
    {
      "shall": "While its operation is active, writes reached through the view SHALL be attributed to that operation, including writes reached through an owner returned by lookup, a child scope, evaluator reentry, a closure capturing the view, or a merge whose target is the view.",
      "tests": [
        "TestRegistration_ViewWriteRecordsBeforeImage",
        "TestRegistration_RegisterValueThroughViewIsAttributed",
        "TestRegistration_FindOwnerWriteIsAttributed",
        "TestRegistration_ChildScopeWriteIsAttributed",
        "TestRegistration_EvaluatorReentryWriteIsAttributed",
        "TestRegistration_CapturedClosureWriteIsAttributed",
        "TestRegistration_MergeIntoViewIsAttributed"
      ]
    },
    {
      "shall": "Writes through the raw root SHALL be unattributed even when their value equals an attributed write.",
      "tests": [
        "TestRegistration_HostEqualValueRebindIsUnowned",
        "TestRegistration_HostEqualValueRebindSurvivesAbort",
        "TestRegistration_RawRebindDuringOperationZeroAllocs"
      ]
    },
    {
      "shall": "Aborting the operation SHALL, for each name the operation wrote, restore the prior value, canonical status, presence, and deletion state only when the current state is still the operation's latest write, and SHALL otherwise leave the current state untouched.",
      "tests": [
        "TestRegistration_AbortRestoresOverwrittenBindings",
        "TestRegistration_AbortRemovesAddedNames",
        "TestRegistration_AbortRestoresCanonicalStatus",
        "TestRegistration_AbortRestoresDeletedBinding",
        "TestRegistration_HostRebindAfterOperationSurvivesAbort",
        "TestRegistration_HostAddSurvivesAbort",
        "TestRegistration_HostDeleteAfterOperationSurvivesAbort",
        "TestRegistration_HostDeleteRecreateSurvivesAbort",
        "TestRegistration_HostReplaceCellSurvivesAbort",
        "TestRegistration_OperationHostOperationAbortKeepsHostState",
        "TestRegistration_ConcurrentHostWritesRace"
      ]
    },
    {
      "shall": "Abort SHALL restore prior cells in place rather than install replacement cells, SHALL keep cell versions, name generations, and macro epochs monotone, and SHALL invalidate cached lookups that observed a reverted definition.",
      "tests": [
        "TestRegistration_AbortRestoresOverwrittenBindings",
        "TestRegistration_AbortRestoresReplacedCell",
        "TestRegistration_AbortInvalidatesCellCaches",
        "TestRegistration_AbortBumpsMacroEpoch",
        "TestRegistration_AbortWithoutOwnedEntriesKeepsCounters",
        "TestRegistration_RebuildDuringOperationKeepsOwnedTombstone",
        "TestRegistration_VMSiteDropsRevertedDefinition"
      ]
    },
    {
      "shall": "After completion or abort the view SHALL forward as an ordinary unattributed environment.",
      "tests": [
        "TestRegistration_CompletedViewForwardsUnattributed",
        "TestRegistration_AbortedViewForwardsUnattributed",
        "TestRegistration_CompleteAndAbortAreIdempotent",
        "TestRegistration_CapturedClosureWriteIsAttributed"
      ]
    },
    {
      "shall": "An environment with no active operation SHALL keep its existing binding behavior.",
      "tests": [
        "TestEnv_Size",
        "TestEnv_Get_ZeroAllocs",
        "TestEnv_NameGen",
        "TestEnv_CellVersion",
        "TestEnv_RebuildReleasesDeadCapacity",
        "TestMergeInto_ConcurrentSetNotLost",
        "TestRegistration_ViewGetZeroAllocs"
      ]
    },
    {
      "shall": "prior values and canonical status SHALL be restored in the same cells, added names SHALL be absent, and version counters SHALL NOT decrease",
      "tests": [
        "TestRegistration_AbortRestoresOverwrittenBindings",
        "TestRegistration_AbortRemovesAddedNames",
        "TestRegistration_AbortRestoresCanonicalStatus"
      ]
    },
    {
      "shall": "the raw-root write's resulting state SHALL remain",
      "tests": [
        "TestRegistration_HostRebindAfterOperationSurvivesAbort",
        "TestRegistration_HostAddSurvivesAbort",
        "TestRegistration_HostDeleteAfterOperationSurvivesAbort",
        "TestRegistration_HostDeleteRecreateSurvivesAbort",
        "TestRegistration_OperationHostOperationAbortKeepsHostState"
      ]
    },
    {
      "shall": "each of those writes SHALL be reverted under the same conflict rule",
      "tests": [
        "TestRegistration_FindOwnerWriteIsAttributed",
        "TestRegistration_ChildScopeWriteIsAttributed",
        "TestRegistration_EvaluatorReentryWriteIsAttributed",
        "TestRegistration_CapturedClosureWriteIsAttributed",
        "TestRegistration_MergeIntoViewIsAttributed"
      ]
    },
    {
      "shall": "reads through the view SHALL observe the rebinding and later writes through it SHALL be unattributed",
      "tests": [
        "TestRegistration_CompletedViewForwardsUnattributed",
        "TestRegistration_AbortedViewForwardsUnattributed"
      ]
    },
    {
      "shall": "the next use SHALL observe the restored binding",
      "tests": [
        "TestRegistration_AbortBumpsMacroEpoch",
        "TestRegistration_VMSiteDropsRevertedDefinition",
        "TestRegistration_AbortInvalidatesCellCaches"
      ]
    }
  ],
  "testHarness": [
    "requireRetainedLimitError — core/env_test.go:'func requireRetainedLimitError(t *testing.T, err error) {' — asserts *LispicoError Code CodeResourceLimit + 'retained state capacity limit exceeded'",
    "barrierRetainedMeter — core/env_merge_test.go:'type barrierRetainedMeter struct {' — sessionMeter fake; ReleaseRetained blocks on released/proceed chans (2s timeouts); Charge/Lease no-op",
    "waitForSignal — core/env_merge_test.go:'func waitForSignal(t *testing.T, ch <-chan struct{}, message string) {' — 2s receive-or-fatal",
    "sendWithTimeout — core/env_merge_test.go:'func sendWithTimeout(t *testing.T, ch chan struct{}, message string) {' — 2s send-or-fatal",
    "sumLiveRetainedBytes — core/env_merge_test.go:'func sumLiveRetainedBytes(e *Env) int64 {' — sums cell.retainedBytes of live vars+funcs under RLock (private access)",
    "TestEnv_RebuildPreservesLiveCellIdentity / TestEnv_RebuildBumpsNameGenAndDropsDeadCells / TestEnv_CellVersion / TestEnv_FuncCellVersion / TestEnv_NameGen — core/env_test.go — existing cell-identity, version, NameGen assertion shapes to copy",
    "TestEnv_Cell_TombstonedDelete — core/env_test.go:'func TestEnv_Cell_TombstonedDelete(t *testing.T) {' — delete/tombstone shape",
    "TestEnv_RebuildConcurrentReadersWriters / TestEnv_ConcurrentSetGet — core/env_test.go — race-detector concurrency shapes",
    "TestMergeInto_ConcurrentSetNotLost / TestMergeInto_CapacityErrorLeavesTargetUnchanged — core/env_merge_test.go — merge-target shapes",
    "newTestEnv — core/eval_test.go:'func newTestEnv() *Env {' — NewEnv(nil), no evaluator",
    "newCoreEnv — core/integration_test.go:'func newCoreEnv() *Env {' — NewEnv(nil) + minimal GoFunc arithmetic builtins",
    "testEvalMeter / partialGrantEvalMeter — core/meter_test.go:'type testEvalMeter struct {' — sessionMeter fakes (retained no-op)",
    "chargeFailingMeter / panicChargeMeter / panicReleaseMeter / panicLeaseMeter — core/meter_settle_test.go — retained-charge failure/panic fakes; assertSettlementClosedOut helper",
    "depthLimitEvaluator / limitlessEvaluator — core/depth_with_test.go:'type depthLimitEvaluator struct{ limit int }' — Evaluator stubs returning Nil",
    "evalCountingEvaluator — core/bootstrap_definer_test.go:'type evalCountingEvaluator struct {' — wraps *engine, counts Eval (evaluator-reentry probe candidate)",
    "Makefile — 'GOTESTFLAGS ?= -timeout 2m'; targets: test (go test $(GOTESTFLAGS) ./...), test-unit (=test), lint (golangci-lint run), fmt, build, profile, profile-report; no race target",
    "Floor selection — 'go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run Test(Env|Merge|Registration)' selects 38 TestEnv* (core/env_test.go) + 4 TestMerge* (core/env_merge_test.go) and ZERO tests in core/vm (no top-level test name contains the pattern) -> core/vm reports no tests to run"
  ],
  "floor": "make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 5m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)' && openspec validate registration-journal --strict --json",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2,
    "notes": [
      "Round 1: 4 blockers (verify ran later chunks' red tests; c4 race leg too wide; phantom design.md anchor; coder-authored shared test helpers), each answered by a delta.",
      "Round 2: merge_ready; 3 text warnings (stale c5 site texts, Find keeping lazy(), task 0.1 ticked by the orchestrator) fixed as residue without re-review."
    ]
  }
}
```
