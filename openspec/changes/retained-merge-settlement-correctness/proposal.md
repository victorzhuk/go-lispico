## Why

The retained-state accounting has three correctness gaps on the merge and
settlement paths, all reachable through the hot-reload flow:

- **Lost-update race.** `MergeInto`/`MergeIntoCanonical` (`core/env.go`) document
  "Target is locked during merge" but actually lock, compute commits/releases,
  **unlock** to call `ReleaseRetained`, then **re-lock** and blind-overwrite each
  cell with the precomputed value (no version re-check). `watch.go` calls
  `childEnv.MergeInto(rootEnv)` from the watcher goroutine while user goroutines
  run `Bind`/`Eval`/`Set` on the same `rootEnv`; `Bind` takes only the engine
  lock, which the merge never acquires, so nothing serializes them. A concurrent
  `Set`/`def` landing in the unlock window is silently overwritten by the merge's
  stale value — a lost update, no error, no log.
- **Aggregate drift.** `MergeInto`'s overwrite branch computes a release for the
  old cell's bytes but never updates the env's `retainedBytes`/`retainedSlots`
  aggregate (the not-found branch does). That aggregate gates
  `MaxRetainedBytesPerEnv`; repeated overwrites (iterative hot-reload of the same
  names) drift it away from the true per-cell sum, and it only self-heals via
  `Rebuild()`, which the hot-reload path never calls — so a long watch session
  never corrects.
- **No rollback on partial multi-meter settlement.** `settleRetained`
  (`core/metering.go`) groups pending allocations by meter and charges each in
  **map-iteration order** (nondeterministic); if charge N of M fails it returns
  immediately, leaving charges 1..N−1 applied with no compensating release, and
  the per-cell finalization loop never runs so those cells never get
  `retainedMeter` set — so a later `Rebuild()` cannot find them to release
  either. Leaked charge, no recovery path.

## What Changes

- `MergeInto`/`MergeIntoCanonical` SHALL hold the target lock for the whole
  merge, or re-validate each cell's version before commit and skip/retry on
  conflict, so a concurrent write is never silently lost. The doc comment and
  the implementation SHALL agree.
- The overwrite branch SHALL adjust the env retained aggregate by
  `(newBytes − oldBytes)` (and the slot count) so the aggregate stays equal to
  the true per-cell sum across repeated merges.
- `settleRetained` SHALL charge meters in a deterministic (sorted) order and, on
  a partial failure, unwind the already-succeeded charges before returning the
  error, so no charge is leaked without an owner.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: strengthen the retained-state accounting requirements — merge
  is atomic against concurrent writes, the retained aggregate stays consistent
  across overwrites, and multi-meter settlement is all-or-nothing.

## Impact

- Code: `core/env.go` (`MergeInto`/`MergeIntoCanonical`, overwrite-branch
  aggregate), `core/metering.go` (`settleRetained` order + rollback).
- Behavior: hot-reload (`Watch`) no longer risks a lost update or a slowly
  drifting retained aggregate; multi-meter settlement failures don't leak
  charges.
- Concurrency: closes a doc-contradicting race on the shared `rootEnv` between
  the watcher goroutine and user calls.
