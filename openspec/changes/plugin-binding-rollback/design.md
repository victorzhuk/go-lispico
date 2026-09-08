## Context

See proposal.md for both verified failures. `snapshotBindings` tracks only name additions. `snapshotRootEnv` stores values without canonical flags, cell identity, or lazy state; `restoreRootEnv` rewrites the entire root and rebuilds it. That cannot preserve writes from `Eval`, `RootEnv().Set`, or lazy lookup that overlap initialization.

`Plugin.Init` accepts a concrete `*core.Env`, without a context or registration token. Holding `engine.mu` does not serialize direct environment access. Holding the environment lock across `Init` would deadlock its own binding writes. Shared-root identity and live `Cell` identity are observable through handles, closures, and VM caches.

## Goals / Non-Goals

**Goals:** restore failed-operation writes with explicit ownership, preserving concurrent host writes and existing alias behavior; keep registry and plugin bookkeeping consistent with the returned result.

**Non-Goals:** read isolation during initialization, rollback of arbitrary Go/external effects, successful-unload ownership redesign, freezing host evaluation during initialization, or transactional ordinary `Eval`.

## Decisions

### Registration view identifies writes without changing Plugin

Introduce a core registration view and mutation journal; both are new implementation structures, not existing APIs. Runtime obtains a `*core.Env` view for one root and passes it to `Init`. The view delegates reads and writes to that root, adding an opaque operation identity only to mutations reached through the view. It is a forwarding scope, not a lexical child, copied environment, or copied mutex.

Root writes remain visible immediately, matching current registration behavior. The canonical root returned by `RootEnv()` never changes. After successful completion the retained view forwards without an active operation; closures and plugin-held view references continue to see root updates. An aborted view also becomes an ordinary forwarding view after all owned writes are reverted; later writes through a retained reference are new host effects, outside the completed operation.

**Compatibility:** `Init`'s argument is no longer pointer-identical to `RootEnv()`. No `Plugin` method or signature changes. This identity change must be documented. A lexical child followed by `MergeInto` is rejected because it changes deletion, capture, and owner-lookup semantics; merely replaying snapshots is rejected because it cannot identify concurrent writers.

### Cover every route back to the root

Route the view through the existing Env surface, not only `Set`:

| Surface | Required behavior |
| --- | --- |
| `Set`, `SetBoth`, `SetFunc`, canonical and context variants, `ReplaceCell` | Delegate to the owner root under its lock with operation identity. |
| `Get`, canonical/materialized lookups, `Cell`, `FuncCell`, local cell lookups, name enumeration | Read the live root; lazy resolution retains the initiating operation identity. |
| `Find` | Return an owner-aware view for a transaction-reachable owner; never leak a raw root that silently loses attribution. |
| `Child`, `ChildVariadic`, closure capture and evaluator reentry | Preserve lexical child scopes; their parent/owner traversal retains the registration view until operation completion. |
| `Delete`, `Rebuild`, `MergeInto`, `MergeIntoCanonical` | Attribute root mutations, including a view used as merge target; preserve foreign writes and pinned before-images. |
| `Evaluator`, `SetEvaluator`, `LazyLayer`, `SetLazyLayer`, retained-meter accessors | Delegate consistently; journal any root configuration mutation reached through the view with the same conflict policy. |
| `ReadCell`, `ReadCellSnapshot`, `NameGen`, `MacroEpoch`, `BumpMacroEpoch`, `RetainedUsage` | Use the canonical owner's lock/counters and actual cells, not empty view fields. |

The implementation must also audit direct private-field access inside core and VM. A `Cell` itself exposes no value setter; mutations through Env remain the interception point. Returning a separately retained environment explicitly supplied by host code is a bypass boundary, not part of the registration view's ownership guarantee.

### Undo owned cell changes under the owner lock

Add an optional active journal on the canonical root. Normal engines without an active plugin operation pay only an absent-journal branch on affected mutations. Each namespace/name entry records the existing cell pointer, live/tombstoned state, value, canonical marker, retained owner/capacity, and the version of the operation's latest write. New-name and replacement-cell writes also record the prior map membership.

Before each operation write, compare the currently installed cell/version with the operation's latest write. If a host write intervened, advance the before-image to that host state before applying the next plugin write. On abort, restore an entry only when the current state is still the operation's latest owned write. Otherwise leave the host's state untouched. Thus both plugin→host→abort and plugin→host→plugin→abort preserve the host value; host deletion is equally significant.

Root write methods update this provenance while holding the same lock as the mutation. A host write through the raw root is unowned even when its value equals the plugin's value; equality is not ownership. Rollback restores prior live cells in place. While journal entries refer to removed cells, `Rebuild` must retain their before-images and distinguish host replacement from operation replacement. Never install an old map over the current one.

Name generations, cell versions, and macro epochs remain monotone. Rollback bumps versions/epochs to invalidate failed definitions; it does not restore historical counters. Existing `Fn`/`PinnedFn` references must keep observing the restored cell unless a host explicitly replaced or deleted it.

### Lazy state and metadata follow the same ownership rule

Keep old registry entries, `bindings`, and active-plugin counts pending until initialization, vocabulary work, and settlement succeed. Root binding deletion for reload and vocabulary writes run through the registration view. If public registry mutation races the final publication, compare its entry generation/identity with the operation's starting observation; preserve the host entry and abort with a conflict error rather than overwriting it. This needs a new conditional publication seam in `core.Registry`; no public plugin-interface change is required.

Lazy bookkeeping requires operation-tagged changes for active version, tombstones, installed names, and materialization count. Pass registration identity through `RegisterValue`, `RegisterSource`, and lookup/materialization reached from the view; do not infer identity solely from mutable `loadingPlugin`. Restore only entries still owned by the failed operation, with before-images rebased after host materialization/deletion. Never restore all of `state.active`, `state.installed`, or `state.tombstoned` from a snapshot. Published process-level templates remain immutable and sibling engines remain untouched.

Operation completion must fence in-flight materialization started through its view: close registration, wait for those operations without holding env/lazy locks, then settle and publish or undo. Host materialization remains independent. A late use of the completed view is an ordinary host operation, not a mutation of a retired journal.

### Retained ownership is settled with the write journal

Retain old owned capacity until the outcome is known. Track only operation-owned new capacity and ownership transfers; do not restore aggregate counters by assigning saved totals. Abort refunds only successfully charged capacity that it actually removes and preserves charges backing surviving host writes. If a host adopts an operation-created cell, that cell and its capacity survive; rebinding does not permit rollback to release its backing.

Settle the operation's retained delta exactly once before metadata publication. A failed charge undoes successful earlier charges through existing symmetric release semantics. Apply journal/counter changes under the env lock, collect external meter calls, and execute those calls outside env/lazy locks. Keep error precedence and unused compute-lease return intact. No arbitrary root `Rebuild` is used as a substitute for owned-capacity cleanup.

### Verification focuses on interleavings and aliases

Existing-service-strict, regression-first. Extend `runtime/plugin_test.go`, `runtime/lazy_materialize_test.go`, `runtime/meter_test.go`, and core env/merge tests. Channel barriers inside an initializing plugin make host writes deterministic; no scheduling sleeps. Cover disjoint and same-name host writes, host delete/recreate, host writes between two plugin writes, concurrent lazy first touch, and host registry change at publication. Repeat these under the race detector.

Cross both namespaces, canonical operators, macros, existing call handles, partial lazy state, shared templates, retained rejection, capacity rejection, successful retry, closures capturing the supplied view, `Find`-derived owners, child scopes, evaluator reentry, merge targets, and `Rebuild`. Tests of successful unload pin its existing last-writer semantics from the archived `2026-07-10-review-bugfix-batch/design.md`.

## Risks / Trade-offs

- Missing a view escape path silently loses ownership → enumerate every Env method and core/VM direct access before implementation; require alias and capture tests before enabling the runtime path.
- Journaling expands core mutation logic → keep one optional root journal and short owner-lock sections; no general transaction framework or goroutine-identity inference.
- Readers may observe transient plugin definitions → retain current read visibility and document that rollback guarantees post-return state, not read isolation.
- Before-images retain memory during initialization → account journal capacity against the operation's resource budget; never silently drop undo records on exhaustion.
- Root/cell aliasing and retained ownership can drift → use existing cells, monotone versions, and accounting assertions; successful host writes are never refunded by failed plugin cleanup.
- Registry publication can conflict with direct host registry edits → return a conflict failure and preserve the host mutation; never claim restoration overwrites a newer host decision.

## Migration Plan

Implement the core ownership seam and regression matrix before routing plugin lifecycle operations through it. Keep successful ownership semantics unchanged. Document the `Init` argument identity change, post-return rollback guarantee, and excluded external effects in existing architecture/plugin docs; add a CHANGELOG entry. No stored data or dependency migration. Reversion restores the incomplete rollback behavior. Merge after the two runtime boundary changes for file coordination only.
