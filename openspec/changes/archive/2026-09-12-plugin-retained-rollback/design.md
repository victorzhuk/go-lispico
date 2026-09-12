## Context

A plugin operation runs `Init` inside one evaluation: `Use` calls `StartEval`, fresh bindings made during `Init` record pending cell allocations, and the deferred `FinishEval` runs `settleRetained`, which charges the meter. On failure the same defer then calls `rollbackPluginUse`, which deletes added names. Deletion tombstones without releasing (ADR 0012), so the meter keeps charges for cells the failed operation removed.

`registration-journal` records per-entry ownership for operation writes and `plugin-binding-rollback` wires `Use`/`ReloadPlugin` abort through it. This change extends journal entries with retained owner and capacity and settles them exactly once.

## Goals / Non-Goals

**Goals:** a failed operation leaves no charge for cells it removed, keeps charges backing surviving bindings, releases nothing twice, and keeps error precedence and lease return intact.

**Non-Goals:** deep identity-traced retained accounting (deferred by ADR 0012), successful-operation charging changes, retained accounting for ordinary `Eval`.

## Decisions

### Retained ownership is settled with the write journal

Retain old owned capacity until the outcome is known. Track only operation-owned new capacity and ownership transfers; never restore aggregate counters by assigning saved totals. Abort drops or refunds only capacity for cells it actually removes and preserves charges backing surviving host writes.

Settle the operation's retained delta exactly once before metadata publication. A failed charge undoes earlier successful charges through the existing symmetric release semantics of `settleRetained`. Apply journal and counter changes under the env lock, collect external meter calls, and run them outside env and lazy locks. No arbitrary root `Rebuild` substitutes for owned-capacity cleanup.

### Settlement order

Abort decides which pending allocations still back a live binding before any meter is charged, so a failed operation never charges the meter for cells it removes. Pending allocations for removed cells are dropped; allocations backing surviving bindings settle normally.

## Open Questions

Both block planning; they are recorded as `OPEN:` in the proposal.

- **Pending-charge adoption.** A concurrent host write can adopt an operation-created cell while its charge is pending (a rebind through an existing cell is free, and `SetWithContext` charges only new names); settlement may then deny, including host-meter denial. The host binding survives either way. Candidates: fork a host-owned cell instead of adopting; transfer the pending charge to a host settlement; let the adopted cell survive meter-uncharged, bounded by per-env caps.
- **Release path.** Releasing capacity for abort-removed cells contradicts ADR 0012 and the `CONTEXT.md` **Owned capacity** entry, which make `Rebuild` the only release path. Either record a second release path and modify **Env owned-capacity accounting**, or route abort cleanup through `Rebuild` semantics.

## Risks / Trade-offs

- Env counters and meter charges drift → accounting fixtures assert exact charge and release amounts for every failure path.
- Meter calls under locks deadlock host meters → calls collected under the lock and executed after release.
- A panicking meter during abort settlement → the existing `finishEval` recovery and `releaseAll` semantics apply.

## Migration Plan

Land after `registration-journal` and `plugin-binding-rollback`, once both open questions are recorded. Amend ADR 0012 and `CONTEXT.md`; add a CHANGELOG entry. No stored data or dependency migration. Reversion restores the pre-change leak.

## Implementation plan (human-readable)

Tier **heavy** (concurrency/locks), mode **existing-service-strict**, lenses `spec` + `quality`. Plan review: **pass** (zarchitect, 3 rounds — sequencing, red-classification defects fixed; round 3 verified red sets against base). Machine appendix below carries the contracts verbatim; this section is the reading order.

### c-core — tasks 1.1, 2.1 (parallel, shard `core`, go-coder)

RED (fail on base — base `Abort` refunds nothing, releases nothing): `TestAbortRefundsOwnedRetainedCapacity`, `TestAbortReleasesSettledChargeOnce` in `core/meter_settle_test.go`. Keep-green pins written in the same file, verify-gated: `TestAbortKeepsAdoptedCellCharge`, `TestAbortRestoredBindingKeepsCharge`, `TestAbortNoMeterCallUnderEnvLock` (the lock-freedom pin is vacuous on base — base Abort never calls a meter — and gains force against the coder change).

Code: `registrationEntry` gains op-owned retained capacity + settled marker; `Abort` refunds counters for removed owned entries, releases settled charges exactly once, executes `ReleaseRetained` after `root.mu` unlock (Rebuild pattern, `core/env.go:1042-1046`); charging path (`prepareFreshRetained`/`recordFreshRetained`) hands capacity to the journal entry; journal-aware `pendingCellAlloc` filter with counter refunds under the env lock and collected — never executed — meter calls. Error types asserted: `*core.LispicoError` with `core.CodeResourceLimit` (`core.NewResourceLimitError`).

redRun: `go test -timeout 2m ./core -run 'TestAbort(RefundsOwnedRetainedCapacity|ReleasesSettledChargeOnce)'`
verify: `go build ./core/... && go test -timeout 2m ./core -run 'Test(Abort|SettleRetained|FinishEval)' && go vet ./core && golangci-lint run ./core/...`

### c-docs — task 3.1 (parallel, shard `docs`, zpatcher, waived)

**NO-RED-WAIVER:** documentation and floor — no observable contract beyond seams 2/3. **NO-TESTER-WAIVER:** same. ADR 0012 second-release-path + adoption-rule amendment (`only path that releases dead backing` wording), `CONTEXT.md` Owned capacity entry (`Deletion tombstones without release`), `CHANGELOG.md [Unreleased]` Fixed entry. The spec **MODIFIED deltas** (runtime-api `Rebuild`-only wording at `openspec/specs/runtime-api/spec.md:501-505`; core-engine tombstone wording at `openspec/specs/core-engine/spec.md:473-484`) are authored by the orchestrator under this change's `specs/` before the strict validate — workers never touch `openspec/`.

### c-rtm — task 2.2 + runtime leg of 1.1 (serial after c-core, sharedPkg `runtime`, integration worktree, go-coder)

RED dispatches in wave 1 against the base-state worktree (`redAfter` absent — engine-level tests name no new core symbol); `prev: c-core` orders the CODE stage only. RED (fail on base): `TestUseFailedInitLeavesNoRetainedCharge`, `TestUseFailedInitNetRetainedUsageEqualsPreOp`, `TestUseCapacityRejectionMidInitLeavesNoCharge`, `TestUseConcurrentHostBindingKeepsCharge` (op-charges-gone leg; the host-charge-stays leg is green on base), `TestUsePublishConflictReleasesSettledCharges`, `TestReloadPluginFailedInitSettlesRetainedOnce`. Keep-green, verify-gated: `TestUseSettlementDenialReleasesExactlyOnce`, `TestUseHostMeterDenialReleasesExactlyOnce`, `TestUseAdoptedPendingCellSettlesNormally`, `TestUseFailedOpReturnsLeaseExactlyOnce`, `TestUseFailedOpKeepsErrorPrecedence`. Seeding only via contract paths — channel-barrier host `Set` (2s guards), first-version register; no sleeps.

Code: `loadPlugin` failure path runs the journal-aware pending filter between the lazy fence and the deferred `FinishEval`; success path unchanged; settlement precedes `publishPlugin`; error precedence (`runtime/plugin.go:158-161`) and unused lease return (`returnEvalLease`) intact.

redRun: `go test -timeout 2m ./runtime -run 'TestUse(FailedInit|Capacity|ConcurrentHost|PublishConflict)|TestReloadPluginFailedInitSettlesRetainedOnce'`
verify: `go build ./runtime/... && go test -timeout 2m ./runtime -run 'TestUse(FailedInit|Settlement|HostMeter|Capacity|ConcurrentHost|Adopted|PublishConflict|FailedOp)|TestReloadPluginFailedInit|TestMeter_' && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run 'TestMeter_(Concurrent|RebuildRaces|Reentrant)|TestUse(ConcurrentHost|Adopted)' && go vet ./runtime && golangci-lint run ./runtime/...`

### c-baseline — task 0.1 (waived: **NO-RED-WAIVER**/**NO-TESTER-WAIVER** — evidence-only; closes last)

Baseline = the c-core and c-rtm redRun assertion failures recorded on base. Both proposal `OPEN:` boxes ticked by the orchestrator with the recorded decisions: adoption (adopted cell backs a surviving binding, settles normally, charge stays, host-write survival contract holds) and release path (registration abort is a second, narrow release path; ADR 0012 + CONTEXT.md amended).

### Floor

`make lint && make test GOTESTFLAGS='-timeout 10m -p 2 -parallel 2' && go test -race -timeout 10m -p 2 -parallel 2 ./core ./runtime -run 'Test(Meter|Settle|Use|ReloadPlugin|Retained|Registration|Env|Abort)'`

### Risks (from the design packet, evidence there)

Double release across the three release sites; publish-conflict charges-then-removes (Abort release covers it); meter calls under `root.mu` (collect, run after unlock); defer-order regression in `loadPlugin`; adopted-cell drift bounded by per-env caps (32 MiB / 100,000 slots); Rebuild-mid-op interplay (refund keys on journal ownership, not cell presence); nested-evaluator early settlement (Abort release path covers); ReloadPlugin restore refund keyed on `ent.prior == nil`.

## Plan appendix

```json
{
  "v": 2,
  "change": "plugin-retained-rollback",
  "baseSha": "b758bf559e2e8105ed99218466c20600c4e9d1a6",
  "generatedAt": "2026-09-11T20:20:19.975Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality"
  ],
  "chunks": [
    {
      "id": "c-core",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "journal-retained-ownership",
      "shard": "core",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/meter_settle_test.go",
          "symbol": "TestSettleRetained_PartialFailureRollsBackCharges",
          "anchor": "func TestSettleRetained_PartialFailureRollsBackCharges",
          "change": "add Abort-ownership regressions after the settle tests: TestAbortRefundsOwnedRetainedCapacity, TestAbortReleasesSettledChargeOnce, TestAbortKeepsAdoptedCellCharge, TestAbortRestoredBindingKeepsCharge, TestAbortNoMeterCallUnderEnvLock — fixtures chargeFailingMeter (:10), settleRetainedCell (:314), settleRetainedRebuiltCell (:331), assertSettlementClosedOut (:346), panicReleaseMeter (:158)"
        },
        {
          "task": "2.1",
          "file": "core/registration.go",
          "symbol": "Registration",
          "anchor": "// Registration is the handle for one registration operation on a root Env",
          "change": "extend registrationEntry (:42-48) with op-owned retained capacity + settled marker recorded at afterWrite time from the charging path"
        },
        {
          "task": "2.1",
          "file": "core/registration.go",
          "symbol": "Abort",
          "anchor": "// Abort rolls back",
          "change": "ownership-keyed refund in the tombstone loop: refund root.retainedBytes/retainedSlots for removed op-owned entries (ent.prior == nil, cell is op last write at version), collect releases for settled cells (cell.retainedMeter != nil), execute ReleaseRetained after root.mu unlock (Rebuild pattern core/env.go:1042-1046); adopted foreign-touched entries skipped (:92-94); restored entries never refunded"
        },
        {
          "task": "2.1",
          "file": "core/env.go",
          "symbol": "recordFreshRetained",
          "anchor": "recordFreshRetained",
          "change": "charging path (prepareFreshRetained :244, recordFreshRetained :264) hands op-owned capacity to the active journal entry; add the journal-aware pendingCellAlloc filter for the runtime failure path — counter refunds under env lock, meter calls collected never executed inside it"
        },
        {
          "task": "2.1",
          "file": "core/metering.go",
          "symbol": "settleRetained",
          "anchor": "settleRetained",
          "change": "charge/release symmetry unchanged; denial compensation (:391-408) and rebuilt-release (:413-416) paths must not regress; finalize (:417-421) only after the charge loop"
        },
        {
          "task": "2.1",
          "file": "core/metering.go",
          "symbol": "releaseAll",
          "anchor": "releaseAll",
          "change": "panicking-meter compensation pattern (:422) — unchanged, keep green"
        }
      ],
      "contract": {
        "budgets": [
          "ReleaseRetained calls per removed settled cell == 1, amounts exact (bytes, slots)",
          "counter refund per removed entry == reserved retainedBindingBytes (core/env.go:206-208) + 1 slot",
          "adopted meter-uncharged capacity bounded by per-env caps 32 MiB / 100,000 slots (openspec/specs/runtime-api/spec.md:492-494)",
          "core tests -timeout 2m; barrier guards 2s"
        ],
        "forbidden": [
          "ReleaseRetained for a cell with cell.retainedMeter == nil",
          "releaseCalls > charges attributable to one cell (double release across compensation, rebuilt-release, abort)",
          "root.mu or env.mu held across ChargeRetained/ReleaseRetained (Abort holds root.mu, core/registration.go:84-85)",
          "aggregate counter restore by assigning saved totals",
          "refund or release for a foreign-touched (adopted) entry",
          "refund for an entry Abort restored rather than removed"
        ],
        "seeding": [
          "cell-pending core-level: newEvalState + st.pendingCellAllocs = []pendingCellAlloc{...} per core/meter_settle_test.go:100-106; env/cell from settleRetainedCell (:314)",
          "cell-pending engine-level: New(..., WithEngineMeter(m)) + Use(plugin whose Init env.Sets) per runtime/meter_test.go:612-635",
          "cell-settled: evaluatorSetupPlugin inner eval (runtime/meter_test.go:725-768) or successful settleRetained",
          "cell-adopted: channel-barrier host env.Set of the op-created name between two Init writes, 2s guards, no sleeps",
          "rebuilt pending: settleRetainedRebuiltCell (:331)"
        ],
        "states": [
          "cell-pending",
          "cell-settled",
          "cell-dropped",
          "cell-released-once",
          "cell-adopted",
          "counters-refunded"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "core/env.go:244-296 (prepareFreshRetained/recordFreshRetained, pendingCellAlloc append :270-274); counters reserved core/env.go:215-229",
            "input": "op new-slot write through a registration view with an active meter",
            "state": "cell-pending"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:417-421 sets cell.retainedMeter/cell.retainedBytes after the charge loop",
            "input": "settlement finalizes a pending cell",
            "state": "cell-settled"
          },
          {
            "effect": "clear",
            "evidence": "core/metering.go:402-408 returns before finalize; pinned by TestSettleRetained_PartialFailureRollsBackCharges (core/meter_settle_test.go:78-142)",
            "input": "settlement denial or charge panic before the finalize loop",
            "state": "cell-pending"
          },
          {
            "effect": "set",
            "evidence": "design.md Settlement order; spec scenario Failed initialization leaves no retained charge",
            "input": "failed-op filter drops a pendingCellAlloc whose cell the op owns and Abort removes",
            "state": "cell-dropped"
          },
          {
            "effect": "set",
            "evidence": "core/registration.go:92-107 ownership test; design.md 'drops or refunds only capacity for cells it actually removes'",
            "input": "Abort removes an op-owned entry (its last write at that version)",
            "state": "counters-refunded"
          },
          {
            "effect": "set",
            "evidence": "requirement 'released with its exact amount' + 'No charge SHALL be released twice'; pattern core/env.go:1021-1023,1042-1046",
            "input": "Abort removes an op-owned entry already settled (cell.retainedMeter != nil)",
            "state": "cell-released-once"
          },
          {
            "effect": "no-op",
            "evidence": "charge never applied; a release would double-release the denial compensation (core/metering.go:391-408)",
            "input": "Abort removes an op-owned entry never settled",
            "state": "cell-dropped"
          },
          {
            "effect": "no-op",
            "evidence": "core/registration.go:92-94 skips restore; host-write survival contract; design.md 'preserves charges backing surviving host writes'",
            "input": "foreign host write touched the entry (adoption)",
            "state": "cell-adopted"
          },
          {
            "effect": "no-op",
            "evidence": "core/registration.go:96-106 reinstalls prior cells; charges backing surviving bindings stay",
            "input": "Abort restores a prior cell/binding",
            "state": "cell-settled"
          },
          {
            "effect": "clear",
            "evidence": "core/metering.go:413-416 rebuilt pendings release; pins keep op tombstones (core/env.go:990-995, core/registration.go:208-213)",
            "input": "Rebuild compacted an op cell away mid-op",
            "state": "cell-pending"
          },
          {
            "effect": "forced",
            "evidence": "requirement 'Meter calls SHALL NOT run while environment or lazy-state locks are held'; reentrant meters re-enter the env (runtime/meter_test.go:806-820)",
            "input": "ReleaseRetained executed for a removed settled cell",
            "state": "cell-released-once"
          }
        ],
        "staging": "RED stage on base: TestAbortRefundsOwnedRetainedCapacity and TestAbortReleasesSettledChargeOnce fail (base Abort refunds nothing, releases nothing); the adopted/restored/lock-free pins pass on base and are verify-gated."
      },
      "redTasks": [
        "1.1 write Abort-ownership red regressions in core/meter_settle_test.go (fail on base — no refund, zero releases today): TestAbortRefundsOwnedRetainedCapacity, TestAbortReleasesSettledChargeOnce",
        "keep-green pins asserted by verify, NOT red (already hold on base): TestAbortKeepsAdoptedCellCharge, TestAbortRestoredBindingKeepsCharge, TestAbortNoMeterCallUnderEnvLock — write them in the same file, they gate verify only"
      ],
      "codeTasks": [
        "2.1 registration journal: op-owned retained capacity on registrationEntry; Abort refunds removed owned entries, releases settled charges once, collects meter calls and runs them after root.mu is released",
        "2.1 core/env.go: charging path hands capacity to the journal entry; journal-aware pendingCellAlloc filter (drop pendings for op-removed cells before any meter charge)",
        "2.1 verify: capacity rejection mid-Init leaves nothing charged, partial settlement failure compensation intact, no double release across denial compensation + rebuilt release + abort release"
      ],
      "redTests": [
        "TestAbortRefundsOwnedRetainedCapacity",
        "TestAbortReleasesSettledChargeOnce"
      ],
      "redRun": "go test -timeout 2m ./core -run 'TestAbort(RefundsOwnedRetainedCapacity|ReleasesSettledChargeOnce)'",
      "verify": "go build ./core/... && go test -timeout 2m ./core -run 'Test(Abort|SettleRetained|FinishEval)' && go vet ./core && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "c-docs",
      "taskIds": [
        "3.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "docs-and-floor",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0012-retained-state-owned-capacity-accounting.md",
          "symbol": "Rebuild-only release wording",
          "anchor": "only path that releases dead backing",
          "change": "amend: registration abort is a second, narrow release path for operation-owned cells a failed op removes; adoption rule (adopted cell settles normally, charge stays, bounded by per-env caps 32 MiB / 100,000 slots); settlement ordering rule"
        },
        {
          "task": "3.1",
          "file": "CONTEXT.md",
          "symbol": "Owned capacity entry",
          "anchor": "Deletion tombstones without release",
          "change": "same two amendments in vocabulary wording at the Owned capacity entry (:161-163)"
        },
        {
          "task": "3.1",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Fixed: failed Use/ReloadPlugin no longer leaves retained charges for cells rollback removed; counters refunded; adoption rule recorded"
        }
      ],
      "contract": {
        "budgets": [
          "floor commands exactly as recorded in fullFloor"
        ],
        "forbidden": [
          "ADR/CONTEXT amended without the matching spec deltas (blockers[0])",
          "CHANGELOG entry missing from [Unreleased] (CHANGELOG.md:8)"
        ],
        "seeding": [
          "none: no test seeds this seam"
        ],
        "states": [
          "docs-amended",
          "floor-green"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "tasks.md 3.1; CONTEXT.md:161-163; ADR 0012 Rebuild section",
            "input": "ADR 0012 + CONTEXT.md + spec deltas + CHANGELOG updated",
            "state": "docs-amended"
          },
          {
            "effect": "set",
            "evidence": "tasks.md 3.1",
            "input": "make lint + full test + race legs + openspec validate --strict pass",
            "state": "floor-green"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.1 ADR 0012 + CONTEXT.md Owned capacity amendments (second release path, adoption rule, ordering rule)",
        "3.1 CHANGELOG [Unreleased] Fixed entry",
        "3.1 spec MODIFIED deltas — BOTH runtime-api (Rebuild-only release wording, openspec/specs/runtime-api/spec.md:501-505) and core-engine (Env owned-capacity accounting tombstone wording, openspec/specs/core-engine/spec.md:473-484) — are written by the orchestrator under openspec/changes/plugin-retained-rollback/specs/ BEFORE the Phase 5 openspec validate --strict; the change dir owns the deltas, nobody edits openspec/specs/ during the change"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make lint",
      "coder": "zpatcher"
    },
    {
      "id": "c-rtm",
      "taskIds": [
        "2.2"
      ],
      "prev": "c-core",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "abort-settlement-order",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.2",
          "file": "runtime/meter_test.go",
          "symbol": "TestMeter_UseRollsBackPluginOnRetainedChargeError",
          "anchor": "func TestMeter_UseRollsBackPluginOnRetainedChargeError",
          "change": "add engine-level regressions after this test: TestUseFailedInitLeavesNoRetainedCharge, TestUseFailedInitNetRetainedUsageEqualsPreOp, TestUseSettlementDenialReleasesExactlyOnce, TestUseHostMeterDenialReleasesExactlyOnce, TestUseCapacityRejectionMidInitLeavesNoCharge, TestUseConcurrentHostBindingKeepsCharge, TestUseAdoptedPendingCellSettlesNormally, TestUsePublishConflictReleasesSettledCharges, TestReloadPluginFailedInitSettlesRetainedOnce, TestUseFailedOpReturnsLeaseExactlyOnce, TestUseFailedOpKeepsErrorPrecedence — fixtures recordingMeter (:15), setupPlugin (:606), evaluatorSetupPlugin (:725), rebuildDuringChargeMeter (:767), reentrantReleaseMeter (:806); channel barriers with 2s guards, no sleeps"
        },
        {
          "task": "2.2",
          "file": "runtime/plugin.go",
          "symbol": "Use",
          "anchor": "func (e *engineImpl) Use",
          "change": "failure path runs the journal-aware pending filter between lazy fence and deferred FinishEval; error precedence (op error wins, settlement error only when err == nil, :158-161) and unused lease return unchanged"
        },
        {
          "task": "2.2",
          "file": "runtime/plugin.go",
          "symbol": "loadPlugin",
          "anchor": "func (e *engineImpl) loadPlugin",
          "change": "defer order on failure: fence, filter, FinishEval; success path fence-then-FinishEval unchanged; no settlement after publishPlugin (:177-181)"
        },
        {
          "task": "2.2",
          "file": "runtime/plugin.go",
          "symbol": "ReloadPlugin",
          "anchor": "func (e *engineImpl) ReloadPlugin",
          "change": "same failure-path ordering; removePluginBindings deletions journaled as op writes; restored old bindings keep charges and counted capacity"
        }
      ],
      "contract": {
        "budgets": [
          "meter net retained delta after a failed op with no concurrent writes == 0 (bytes and slots)",
          "ReleaseRetained count per earlier successful charge == 1 under denial; abort adds 0 more for those cells",
          "lease ReturnEval calls == 1 per failed op",
          "runtime tests -timeout 2m; race legs -timeout 2m -p 2 -parallel 2; barrier guards 2s"
        ],
        "forbidden": [
          "any meter showing a net retained charge for a cell rollback removed (spec scenario 1)",
          "two releases of one charge across compensation, rebuilt-release, and Abort release",
          "ChargeRetained/ReleaseRetained while env.mu, root.mu, or lazy state.mu is held (engine e.mu is not an env/lazy lock; settlement already runs under it today)",
          "settlement after publishBindings/incPlugins",
          "lease leaked on the publish-conflict path",
          "filtering pendings for cells whose bindings survive (adopted or host-created)"
        ],
        "seeding": [
          "engine: New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m)); m.reset(); eng.Use(failing plugin) — runtime/meter_test.go:612-620 pattern",
          "later-meter denial: evaluatorSetupPlugin{ctx: WithMeter(t.Context(), meterB)} over engine meter meterA (first-seen meter order, core/metering.go:376-389)",
          "host-meter denial: recordingMeter{chargeErr: errors.New(\"retained denied\")} (runtime/meter_test.go:694-695)",
          "concurrent host binding / adoption: channel barrier inside Init around the second env.Set; host goroutine RootEnv().Set between writes; 2s guards, no sleeps",
          "capacity rejection: Init write refused by reserveRetainedBindings (core/env.go:215-229) → CodeResourceLimit",
          "ReloadPlugin: first version registered, reload version whose Init fails after new writes"
        ],
        "states": [
          "op-evaluating",
          "op-failed-pre-settle",
          "pending-dropped",
          "settle-denied",
          "op-failed-post-settle",
          "aborted"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:115-150, 152-175, 219-255; core/registration.go:52",
            "input": "Use/ReloadPlugin opens registration, Init + vocabulary run inside StartEval",
            "state": "op-evaluating"
          },
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:162-168; capacity refusal core/env.go:215-229 (spec scenario Capacity rejection mid-operation)",
            "input": "init error, vocabulary error, or per-env capacity rejection",
            "state": "op-failed-pre-settle"
          },
          {
            "effect": "set",
            "evidence": "design.md 'abort decides which pending allocations still back a live binding before any meter is charged'",
            "input": "op-failed-pre-settle with pendings the op owns and Abort removes",
            "state": "pending-dropped"
          },
          {
            "effect": "no-op",
            "evidence": "settleRetained charges only surviving allocations; survivors settle normally",
            "input": "FinishEval after pending-dropped",
            "state": "op-evaluating"
          },
          {
            "effect": "clear",
            "evidence": "core/metering.go:402-408; spec scenario Settlement denial releases exactly once; TestMeter_UseRollsBackPluginOnRetainedChargeError runtime/meter_test.go:694-738",
            "input": "ChargeRetained denial by a later meter or the host meter",
            "state": "settle-denied"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/plugin.go:158-161: finishErr discarded unless err == nil — op error precedence intact; compensation still ran",
            "input": "settle-denied while an op error already exists",
            "state": "op-failed-pre-settle"
          },
          {
            "effect": "forced",
            "evidence": "settlement error returned as *core.LispicoError CodeResourceLimit (core/metering.go:403-405); abortPlugin rolls back (runtime/plugin.go:107-112)",
            "input": "settle-denied with no earlier op error",
            "state": "settle-denied"
          },
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:162-165 settle precedes publishPlugin (:177-181); Abort releases settled op-owned charges",
            "input": "publishPlugin PublishIf conflict after successful settlement",
            "state": "op-failed-post-settle"
          },
          {
            "effect": "forced",
            "evidence": "abortPlugin: endOp(false), reg.Abort(), callCache.drop (runtime/plugin.go:107-112); spec 'The operation's error SHALL be returned'",
            "input": "any failure path completes",
            "state": "aborted"
          },
          {
            "effect": "no-op",
            "evidence": "spec scenario Surviving host binding keeps its charge",
            "input": "concurrent host write binds a new name during the op",
            "state": "op-evaluating"
          },
          {
            "effect": "no-op",
            "evidence": "design.md adoption decision: binding survives (core/registration.go:92-94), charge settles normally and stays",
            "input": "host write adopts an op-created cell (rebind through existing cell, free)",
            "state": "op-evaluating"
          },
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:227-255 journaled deletions restored; old bindings' charges and capacity remain",
            "input": "ReloadPlugin failure after old-name deletion through the view",
            "state": "aborted"
          },
          {
            "effect": "forced",
            "evidence": "finishEval's returnEvalLease (core/metering.go:323-341); spec 'unused compute lease SHALL be returned'",
            "input": "any failure path unwinds",
            "state": "aborted"
          }
        ],
        "staging": "RED stage dispatches in wave 1 against the base-state integration worktree (redAfter absent — engine-level tests name no new core symbol); the CODE stage waits for c-core closed+merged. prev orders code only."
      },
      "redTasks": [
        "1.1/2.2 write engine-level red regressions in runtime/meter_test.go (fail on base): TestUseFailedInitLeavesNoRetainedCharge, TestUseFailedInitNetRetainedUsageEqualsPreOp, TestUseCapacityRejectionMidInitLeavesNoCharge, TestUseConcurrentHostBindingKeepsCharge, TestUsePublishConflictReleasesSettledCharges, TestReloadPluginFailedInitSettlesRetainedOnce",
        "keep-green pins asserted by verify, NOT red (already hold on base): TestUseSettlementDenialReleasesExactlyOnce, TestUseHostMeterDenialReleasesExactlyOnce, TestUseAdoptedPendingCellSettlesNormally, TestUseFailedOpReturnsLeaseExactlyOnce, TestUseFailedOpKeepsErrorPrecedence — write them in the same file, they gate verify only"
      ],
      "codeTasks": [
        "2.2 loadPlugin (:152-175): on err != nil run the journal-aware pending filter after the fence defer and before the deferred FinishEval; success path untouched",
        "2.2 Use/ReloadPlugin failure paths: abortPlugin stays after FinishEval; publish-conflict path relies on Abort settled-charge release; settlement precedes publishBindings/incPlugins",
        "2.2 verify error precedence, unused lease return exactly once, and every section-1 case from tasks.md"
      ],
      "redTests": [
        "TestUseFailedInitLeavesNoRetainedCharge",
        "TestUseFailedInitNetRetainedUsageEqualsPreOp",
        "TestUseCapacityRejectionMidInitLeavesNoCharge",
        "TestUseConcurrentHostBindingKeepsCharge",
        "TestUsePublishConflictReleasesSettledCharges",
        "TestReloadPluginFailedInitSettlesRetainedOnce"
      ],
      "redRun": "go test -timeout 2m ./runtime -run 'TestUse(FailedInit|Capacity|ConcurrentHost|PublishConflict)|TestReloadPluginFailedInitSettlesRetainedOnce'",
      "verify": "go build ./runtime/... && go test -timeout 2m ./runtime -run 'TestUse(FailedInit|Settlement|HostMeter|Capacity|ConcurrentHost|Adopted|PublishConflict|FailedOp)|TestReloadPluginFailedInit|TestMeter_' && go test -race -timeout 2m -p 2 -parallel 2 ./runtime -run 'TestMeter_(Concurrent|RebuildRaces|Reentrant)|TestUse(ConcurrentHost|Adopted)' && go vet ./runtime && golangci-lint run ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "c-baseline",
      "taskIds": [
        "0.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "baseline-leak-evidence",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [],
      "contract": {
        "forbidden": [
          "any production-file edit in this seam",
          "baseline recorded from a compile failure rather than an assertion failure"
        ],
        "seeding": [
          "git worktree at b758bf5 with only the new red test files"
        ],
        "states": [
          "baseline-recorded",
          "decisions-recorded"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "tasks.md 0.1",
            "input": "seam 2/3 red files run on base b758bf5",
            "state": "baseline-recorded"
          },
          {
            "effect": "set",
            "evidence": "design.md Decisions",
            "input": "proposal.md OPEN adoption + release path resolved",
            "state": "decisions-recorded"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "0.1 record baseline AFTER the c-core and c-rtm red stages ran on base: keep each redRun assertion-failure output as the recorded baseline; for TestUseConcurrentHostBindingKeepsCharge note which leg fired (host-charge-stays is green on base, op-charges-gone is the failing leg)",
        "0.1 proposal.md OPEN checkboxes ticked by orchestrator with the two recorded decisions"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go build ./core/... ./runtime/...",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "baseline-leak-evidence",
      "tasks": [
        "0.1"
      ],
      "summary": "NO-RED-WAIVER: evidence-only chunk — its assertions are the red stages of journal-retained-ownership and abort-settlement-order run on unmodified base. NO-TESTER-WAIVER: baseline evidence chunk, no sealed contract of its own. baseline evidence chunk — its assertions are the seam-2/3 red tests run on the unmodified base and recorded failing on an assertion; records bounded commands and ticks both proposal OPEN checkboxes with the design.md resolutions.",
      "contract": {
        "forbidden": [
          "any production-file edit in this seam",
          "baseline recorded from a compile failure rather than an assertion failure"
        ],
        "seeding": [
          "git worktree at b758bf5 with only the new red test files"
        ],
        "states": [
          "baseline-recorded",
          "decisions-recorded"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "tasks.md 0.1",
            "input": "seam 2/3 red files run on base b758bf5",
            "state": "baseline-recorded"
          },
          {
            "effect": "set",
            "evidence": "design.md Decisions",
            "input": "proposal.md OPEN adoption + release path resolved",
            "state": "decisions-recorded"
          }
        ]
      },
      "codeTasks": [
        "0.1 run the seam-2/3 redRun commands on the unmodified base; record each failing test's assertion output as the baseline failure",
        "0.1 proposal.md: tick both OPEN checkboxes with one line each naming the recorded decision"
      ]
    },
    {
      "id": "journal-retained-ownership",
      "tasks": [
        "1.1",
        "2.1"
      ],
      "summary": "Core seam: registration journal records op-owned retained capacity; Abort refunds counters for removed owned entries, releases settled charges exactly once, keeps charges backing surviving/restored bindings, collects releases under root.mu and runs them after unlock (Rebuild pattern, core/env.go:1042-1046); journal-aware filter drops pendingCellAllocs for op-removed cells before any meter is charged. Never restores counters by assigning saved totals.",
      "contract": {
        "budgets": [
          "ReleaseRetained calls per removed settled cell == 1, amounts exact (bytes, slots)",
          "counter refund per removed entry == reserved retainedBindingBytes (core/env.go:206-208) + 1 slot",
          "adopted meter-uncharged capacity bounded by per-env caps 32 MiB / 100,000 slots (openspec/specs/runtime-api/spec.md:492-494)",
          "core tests -timeout 2m; barrier guards 2s"
        ],
        "forbidden": [
          "ReleaseRetained for a cell with cell.retainedMeter == nil",
          "releaseCalls > charges attributable to one cell (double release across compensation, rebuilt-release, abort)",
          "root.mu or env.mu held across ChargeRetained/ReleaseRetained (Abort holds root.mu, core/registration.go:84-85)",
          "aggregate counter restore by assigning saved totals",
          "refund or release for a foreign-touched (adopted) entry",
          "refund for an entry Abort restored rather than removed"
        ],
        "seeding": [
          "cell-pending core-level: newEvalState + st.pendingCellAllocs = []pendingCellAlloc{...} per core/meter_settle_test.go:100-106; env/cell from settleRetainedCell (:314)",
          "cell-pending engine-level: New(..., WithEngineMeter(m)) + Use(plugin whose Init env.Sets) per runtime/meter_test.go:612-635",
          "cell-settled: evaluatorSetupPlugin inner eval (runtime/meter_test.go:725-768) or successful settleRetained",
          "cell-adopted: channel-barrier host env.Set of the op-created name between two Init writes, 2s guards, no sleeps",
          "rebuilt pending: settleRetainedRebuiltCell (:331)"
        ],
        "states": [
          "cell-pending",
          "cell-settled",
          "cell-dropped",
          "cell-released-once",
          "cell-adopted",
          "counters-refunded"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "core/env.go:244-296 (prepareFreshRetained/recordFreshRetained, pendingCellAlloc append :270-274); counters reserved core/env.go:215-229",
            "input": "op new-slot write through a registration view with an active meter",
            "state": "cell-pending"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:417-421 sets cell.retainedMeter/cell.retainedBytes after the charge loop",
            "input": "settlement finalizes a pending cell",
            "state": "cell-settled"
          },
          {
            "effect": "clear",
            "evidence": "core/metering.go:402-408 returns before finalize; pinned by TestSettleRetained_PartialFailureRollsBackCharges (core/meter_settle_test.go:78-142)",
            "input": "settlement denial or charge panic before the finalize loop",
            "state": "cell-pending"
          },
          {
            "effect": "set",
            "evidence": "design.md Settlement order; spec scenario Failed initialization leaves no retained charge",
            "input": "failed-op filter drops a pendingCellAlloc whose cell the op owns and Abort removes",
            "state": "cell-dropped"
          },
          {
            "effect": "set",
            "evidence": "core/registration.go:92-107 ownership test; design.md 'drops or refunds only capacity for cells it actually removes'",
            "input": "Abort removes an op-owned entry (its last write at that version)",
            "state": "counters-refunded"
          },
          {
            "effect": "set",
            "evidence": "requirement 'released with its exact amount' + 'No charge SHALL be released twice'; pattern core/env.go:1021-1023,1042-1046",
            "input": "Abort removes an op-owned entry already settled (cell.retainedMeter != nil)",
            "state": "cell-released-once"
          },
          {
            "effect": "no-op",
            "evidence": "charge never applied; a release would double-release the denial compensation (core/metering.go:391-408)",
            "input": "Abort removes an op-owned entry never settled",
            "state": "cell-dropped"
          },
          {
            "effect": "no-op",
            "evidence": "core/registration.go:92-94 skips restore; host-write survival contract; design.md 'preserves charges backing surviving host writes'",
            "input": "foreign host write touched the entry (adoption)",
            "state": "cell-adopted"
          },
          {
            "effect": "no-op",
            "evidence": "core/registration.go:96-106 reinstalls prior cells; charges backing surviving bindings stay",
            "input": "Abort restores a prior cell/binding",
            "state": "cell-settled"
          },
          {
            "effect": "clear",
            "evidence": "core/metering.go:413-416 rebuilt pendings release; pins keep op tombstones (core/env.go:990-995, core/registration.go:208-213)",
            "input": "Rebuild compacted an op cell away mid-op",
            "state": "cell-pending"
          },
          {
            "effect": "forced",
            "evidence": "requirement 'Meter calls SHALL NOT run while environment or lazy-state locks are held'; reentrant meters re-enter the env (runtime/meter_test.go:806-820)",
            "input": "ReleaseRetained executed for a removed settled cell",
            "state": "cell-released-once"
          }
        ],
        "staging": "RED stage on base: TestAbortRefundsOwnedRetainedCapacity and TestAbortReleasesSettledChargeOnce fail (base Abort refunds nothing, releases nothing); the adopted/restored/lock-free pins pass on base and are verify-gated."
      },
      "redTasks": [
        "TestAbortRefundsOwnedRetainedCapacity",
        "TestAbortReleasesSettledChargeOnce",
        "TestAbortKeepsAdoptedCellCharge",
        "TestAbortRestoredBindingKeepsCharge",
        "TestAbortNoMeterCallUnderEnvLock"
      ],
      "codeTasks": [
        "2.1 core/registration.go: extend registrationEntry (:42-48) with op-owned retained capacity + settled marker; Abort (:82-118) refunds counters for removed owned entries, collects releases for settled ones, executes after root.mu release",
        "2.1 core/env.go: charging path (prepareFreshRetained :244, recordFreshRetained :264) hands op-owned capacity to the journal entry; journal-aware pendingCellAlloc filter applies counter refunds under env lock and collects — never executes — meter calls inside",
        "2.1 verify capacity rejection, partial settlement failure, no double release"
      ]
    },
    {
      "id": "abort-settlement-order",
      "tasks": [
        "2.2"
      ],
      "summary": "Runtime seam: on failure inside loadPlugin the journal filter runs between the lazy fence and the deferred FinishEval so settleRetained never charges cells Abort removes; post-settlement failures (PublishIf conflict) rely on Abort's release; error precedence (runtime/plugin.go:158-161), lease return (core/metering.go:323-341), settlement-before-publication unchanged. (owns the runtime leg of task 1.1: engine-level regressions)",
      "contract": {
        "budgets": [
          "meter net retained delta after a failed op with no concurrent writes == 0 (bytes and slots)",
          "ReleaseRetained count per earlier successful charge == 1 under denial; abort adds 0 more for those cells",
          "lease ReturnEval calls == 1 per failed op",
          "runtime tests -timeout 2m; race legs -timeout 2m -p 2 -parallel 2; barrier guards 2s"
        ],
        "forbidden": [
          "any meter showing a net retained charge for a cell rollback removed (spec scenario 1)",
          "two releases of one charge across compensation, rebuilt-release, and Abort release",
          "ChargeRetained/ReleaseRetained while env.mu, root.mu, or lazy state.mu is held (engine e.mu is not an env/lazy lock; settlement already runs under it today)",
          "settlement after publishBindings/incPlugins",
          "lease leaked on the publish-conflict path",
          "filtering pendings for cells whose bindings survive (adopted or host-created)"
        ],
        "seeding": [
          "engine: New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m)); m.reset(); eng.Use(failing plugin) — runtime/meter_test.go:612-620 pattern",
          "later-meter denial: evaluatorSetupPlugin{ctx: WithMeter(t.Context(), meterB)} over engine meter meterA (first-seen meter order, core/metering.go:376-389)",
          "host-meter denial: recordingMeter{chargeErr: errors.New(\"retained denied\")} (runtime/meter_test.go:694-695)",
          "concurrent host binding / adoption: channel barrier inside Init around the second env.Set; host goroutine RootEnv().Set between writes; 2s guards, no sleeps",
          "capacity rejection: Init write refused by reserveRetainedBindings (core/env.go:215-229) → CodeResourceLimit",
          "ReloadPlugin: first version registered, reload version whose Init fails after new writes"
        ],
        "states": [
          "op-evaluating",
          "op-failed-pre-settle",
          "pending-dropped",
          "settle-denied",
          "op-failed-post-settle",
          "aborted"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:115-150, 152-175, 219-255; core/registration.go:52",
            "input": "Use/ReloadPlugin opens registration, Init + vocabulary run inside StartEval",
            "state": "op-evaluating"
          },
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:162-168; capacity refusal core/env.go:215-229 (spec scenario Capacity rejection mid-operation)",
            "input": "init error, vocabulary error, or per-env capacity rejection",
            "state": "op-failed-pre-settle"
          },
          {
            "effect": "set",
            "evidence": "design.md 'abort decides which pending allocations still back a live binding before any meter is charged'",
            "input": "op-failed-pre-settle with pendings the op owns and Abort removes",
            "state": "pending-dropped"
          },
          {
            "effect": "no-op",
            "evidence": "settleRetained charges only surviving allocations; survivors settle normally",
            "input": "FinishEval after pending-dropped",
            "state": "op-evaluating"
          },
          {
            "effect": "clear",
            "evidence": "core/metering.go:402-408; spec scenario Settlement denial releases exactly once; TestMeter_UseRollsBackPluginOnRetainedChargeError runtime/meter_test.go:694-738",
            "input": "ChargeRetained denial by a later meter or the host meter",
            "state": "settle-denied"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/plugin.go:158-161: finishErr discarded unless err == nil — op error precedence intact; compensation still ran",
            "input": "settle-denied while an op error already exists",
            "state": "op-failed-pre-settle"
          },
          {
            "effect": "forced",
            "evidence": "settlement error returned as *core.LispicoError CodeResourceLimit (core/metering.go:403-405); abortPlugin rolls back (runtime/plugin.go:107-112)",
            "input": "settle-denied with no earlier op error",
            "state": "settle-denied"
          },
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:162-165 settle precedes publishPlugin (:177-181); Abort releases settled op-owned charges",
            "input": "publishPlugin PublishIf conflict after successful settlement",
            "state": "op-failed-post-settle"
          },
          {
            "effect": "forced",
            "evidence": "abortPlugin: endOp(false), reg.Abort(), callCache.drop (runtime/plugin.go:107-112); spec 'The operation's error SHALL be returned'",
            "input": "any failure path completes",
            "state": "aborted"
          },
          {
            "effect": "no-op",
            "evidence": "spec scenario Surviving host binding keeps its charge",
            "input": "concurrent host write binds a new name during the op",
            "state": "op-evaluating"
          },
          {
            "effect": "no-op",
            "evidence": "design.md adoption decision: binding survives (core/registration.go:92-94), charge settles normally and stays",
            "input": "host write adopts an op-created cell (rebind through existing cell, free)",
            "state": "op-evaluating"
          },
          {
            "effect": "set",
            "evidence": "runtime/plugin.go:227-255 journaled deletions restored; old bindings' charges and capacity remain",
            "input": "ReloadPlugin failure after old-name deletion through the view",
            "state": "aborted"
          },
          {
            "effect": "forced",
            "evidence": "finishEval's returnEvalLease (core/metering.go:323-341); spec 'unused compute lease SHALL be returned'",
            "input": "any failure path unwinds",
            "state": "aborted"
          }
        ],
        "staging": "RED stage dispatches in wave 1 against the base-state integration worktree (redAfter absent — engine-level tests name no new core symbol); the CODE stage waits for c-core closed+merged. prev orders code only."
      },
      "redTasks": [
        "TestUseFailedInitLeavesNoRetainedCharge",
        "TestUseFailedInitNetRetainedUsageEqualsPreOp",
        "TestUseSettlementDenialReleasesExactlyOnce",
        "TestUseHostMeterDenialReleasesExactlyOnce",
        "TestUseCapacityRejectionMidInitLeavesNoCharge",
        "TestUseConcurrentHostBindingKeepsCharge",
        "TestUseAdoptedPendingCellSettlesNormally",
        "TestUsePublishConflictReleasesSettledCharges",
        "TestReloadPluginFailedInitSettlesRetainedOnce",
        "TestUseFailedOpReturnsLeaseExactlyOnce",
        "TestUseFailedOpKeepsErrorPrecedence"
      ],
      "codeTasks": [
        "2.2 runtime/plugin.go loadPlugin (:152-175): on err != nil run the journal-aware pending filter between the fence defer and the deferred FinishEval (defers currently run fence, then FinishEval — LIFO, runtime/plugin.go:156-161)",
        "2.2 Use/ReloadPlugin failure paths: keep abortPlugin after FinishEval; publish-conflict path relies on Abort's release; no settlement after publishBindings/incPlugins",
        "2.2 verify error precedence, unused lease return, every section-1 case"
      ]
    },
    {
      "id": "docs-and-floor",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-RED-WAIVER / NO-TESTER-WAIVER: documentation and the full floor — no observable contract beyond what seams 2 and 3 already seal.",
      "contract": {
        "budgets": [
          "floor commands exactly as recorded in fullFloor"
        ],
        "forbidden": [
          "ADR/CONTEXT amended without the matching spec deltas (blockers[0])",
          "CHANGELOG entry missing from [Unreleased] (CHANGELOG.md:8)"
        ],
        "seeding": [
          "none: no test seeds this seam"
        ],
        "states": [
          "docs-amended",
          "floor-green"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "tasks.md 3.1; CONTEXT.md:161-163; ADR 0012 Rebuild section",
            "input": "ADR 0012 + CONTEXT.md + spec deltas + CHANGELOG updated",
            "state": "docs-amended"
          },
          {
            "effect": "set",
            "evidence": "tasks.md 3.1",
            "input": "make lint + full test + race legs + openspec validate --strict pass",
            "state": "floor-green"
          }
        ]
      },
      "codeTasks": [
        "3.1 ADR 0012: registration abort is a second, narrow release path for operation-owned removed cells; adoption rule recorded",
        "3.1 CONTEXT.md:161-163 Owned capacity entry: same amendments",
        "3.1 add MODIFIED spec deltas for openspec/specs/runtime-api/spec.md:501-505 and openspec/specs/core-engine/spec.md:473-484 (blockers[0])",
        "3.1 CHANGELOG [Unreleased] Fixed entry"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "When `Use` or `ReloadPlugin` returns an initialization, vocabulary, capacity, or retained-settlement error, retained charges for cells the failed operation created and rollback removed SHALL NOT remain charged to any meter.",
      "tests": [
        "TestUseFailedInitLeavesNoRetainedCharge",
        "TestUseFailedInitNetRetainedUsageEqualsPreOp",
        "TestUseCapacityRejectionMidInitLeavesNoCharge",
        "TestUsePublishConflictReleasesSettledCharges",
        "TestReloadPluginFailedInitSettlesRetainedOnce",
        "TestAbortRefundsOwnedRetainedCapacity"
      ]
    },
    {
      "shall": "Charges backing bindings that survive rollback SHALL remain charged.",
      "tests": [
        "TestUseConcurrentHostBindingKeepsCharge",
        "TestUseAdoptedPendingCellSettlesNormally",
        "TestAbortKeepsAdoptedCellCharge",
        "TestAbortRestoredBindingKeepsCharge"
      ]
    },
    {
      "shall": "No charge SHALL be released twice, and every charge a denied settlement already applied SHALL be released with its exact amount.",
      "tests": [
        "TestUseSettlementDenialReleasesExactlyOnce",
        "TestUseHostMeterDenialReleasesExactlyOnce",
        "TestAbortReleasesSettledChargeOnce"
      ]
    },
    {
      "shall": "Meter calls SHALL NOT run while environment or lazy-state locks are held.",
      "tests": [
        "TestAbortNoMeterCallUnderEnvLock"
      ]
    },
    {
      "shall": "The operation's error SHALL be returned and unused compute lease SHALL be returned.",
      "tests": [
        "TestUseFailedOpKeepsErrorPrecedence",
        "TestUseFailedOpReturnsLeaseExactlyOnce"
      ]
    },
    {
      "shall": "- **THEN** every metered retained charge made for those names SHALL be released and the meter's net retained usage SHALL equal its value before the operation",
      "tests": [
        "TestUseFailedInitLeavesNoRetainedCharge",
        "TestUseFailedInitNetRetainedUsageEqualsPreOp",
        "TestAbortRefundsOwnedRetainedCapacity"
      ]
    },
    {
      "shall": "- **THEN** each earlier charge SHALL be released exactly once with its charged amount, the error SHALL be returned, and the operation's bindings SHALL be rolled back",
      "tests": [
        "TestUseSettlementDenialReleasesExactlyOnce",
        "TestUseHostMeterDenialReleasesExactlyOnce",
        "TestAbortReleasesSettledChargeOnce"
      ]
    },
    {
      "shall": "- **THEN** the host binding's retained charge SHALL remain charged after rollback",
      "tests": [
        "TestUseConcurrentHostBindingKeepsCharge",
        "TestAbortKeepsAdoptedCellCharge"
      ]
    },
    {
      "shall": "- **THEN** a `ResourceLimitError`-coded error SHALL be returned and no retained charge for the operation's removed cells SHALL remain",
      "tests": [
        "TestUseCapacityRejectionMidInitLeavesNoCharge"
      ]
    }
  ],
  "testHarness": [
    "chargeFailingMeter - core/meter_settle_test.go:12 - test meter counting charges/releases, denies on failOn",
    "chargeFailingMeter.ChargeRetained - core/meter_settle_test.go:40 - records charge, error when charges >= failOn",
    "chargeFailingMeter.ReleaseRetained - core/meter_settle_test.go:52 - records release amounts",
    "chargeFailingMeter.snapshot - core/meter_settle_test.go:58 - frozen copy of counters",
    "settleRetainedCell - core/meter_settle_test.go:116 - binds name in fresh env, returns (*Env, *Cell)",
    "settleRetainedRebuiltCell - core/meter_settle_test.go:125 - compacted-away cell for release path testing",
    "assertSettlementClosedOut - core/meter_settle_test.go:138 - failure reported, ledger clean, lease returned once",
    "panicChargeMeter - core/meter_settle_test.go:150 - panics from ChargeRetained after recording",
    "panicReleaseMeter - core/meter_settle_test.go:161 - records release then panics",
    "panicLeaseMeter - core/meter_settle_test.go:170 - one reduction per lease, panics after initial",
    "catchSettlementPanic - core/meter_settle_test.go:183 - recovers panic for assertion",
    "recordingMeter - runtime/meter_test.go:13 - full-fidelity meter with denyAfter/chargeErr",
    "recordingMeter.reset - runtime/meter_test.go:88 - zeroes counters preserving denyAfter/chargeErr",
    "recordingMeter.snapshot - runtime/meter_test.go:101 - frozen copy",
    "setupPlugin - runtime/meter_test.go:606 - minimal plugin writing setup/value=Int{V:1}",
    "evaluatorSetupPlugin - runtime/meter_test.go:723 - Init calls env.Evaluator().Eval with a def",
    "rebuildDuringChargeMeter - runtime/meter_test.go:780 - calls Rebuild() inside ChargeRetained",
    "reentrantReleaseMeter - runtime/meter_test.go:799 - reenters env ops from ReleaseRetained",
    "TestSettleRetained_PartialFailureRollsBackCharges - core/meter_settle_test.go:82 - baseline: denial releases earlier charges",
    "TestMeter_UseRollsBackPluginOnRetainedChargeError - runtime/meter_test.go:694 - baseline: Use rolls back on charge error"
  ],
  "floor": "make lint && make test GOTESTFLAGS='-timeout 10m -p 2 -parallel 2' && go test -race -timeout 10m -p 2 -parallel 2 ./core ./runtime -run 'Test(Meter|Settle|Use|ReloadPlugin|Retained|Registration|Env|Abort)'",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 3
  }
}
```
