## Why

A failed plugin operation leaks retained credits. In `Use`, the deferred `FinishEval` runs `settleRetained` and charges the meter for cells the operation created; `rollbackPluginUse` then only deletes those names, and deletion tombstones without releasing (ADR 0012). `plugin-binding-rollback` restores bindings but leaves retained ownership unchanged, so the leak survives it.

## What Changes

- Track operation-owned retained capacity and ownership transfers in the registration journal.
- On a failed `Use` or `ReloadPlugin`, settle retained ownership exactly once: cells the operation created and rollback removed end uncharged, charges backing surviving bindings stay, no charge is released twice. External meter calls run outside env and lazy locks; error precedence and unused compute-lease return stay intact.
- Order settlement against rollback so a failed operation never charges the meter for cells it then removes.
- Record the chosen release path and adoption rule in ADR 0012 and `CONTEXT.md`.

Open before planning:

- [x] OPEN: pending-charge adoption — who owns the retained charge when a concurrent host write adopts a cell the operation created while its charge is still pending, and settlement then denies, including host-meter denial. The host-write survival contract from `plugin-binding-rollback` holds. Candidates: fork a host-owned cell instead of adopting; transfer the pending charge to a host settlement; let the adopted cell survive meter-uncharged, bounded by per-env caps.
  **Resolved (design.md Decisions):** an op-created cell a concurrent host write adopts backs a surviving binding — Abort skips it (`core/registration.go:92-94`), its pending allocation settles normally, its charge stays. No fork, no meter-uncharged survival; drift risk none because the charge is settled, not dropped.
- [x] OPEN: release path — releasing capacity for abort-removed cells contradicts ADR 0012 and the `CONTEXT.md` **Owned capacity** entry, which make `Rebuild` the only release path. Decide between a recorded second release path and routing abort cleanup through `Rebuild` semantics.
  **Resolved (design.md Decisions):** registration abort becomes a second, narrow release path for operation-owned cells the failed op actually removes — counters refunded per removed entry, settled charges released exactly once, meter calls outside env/lazy locks. No arbitrary root `Rebuild`. ADR 0012, `CONTEXT.md`, and both spec requirements amended by task 3.1.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: add **Failed plugin operations settle retained ownership once**.

## Impact

`core/env.go`, `core/metering.go`, `runtime/plugin.go`, `core/meter_settle_test.go`, `runtime/meter_test.go`, ADR 0012, `CONTEXT.md`, CHANGELOG. The release-path decision may also modify the `core-engine` requirement **Env owned-capacity accounting**. No dependency or `Plugin` interface change.

Depends on `registration-journal` and `plugin-binding-rollback`; third of three changes split from the original `plugin-binding-rollback`.
