## 1. Behavior contracts

- [ ] 1.1 Red test (`-race`): concurrent `Set` on `rootEnv` while a `MergeInto`
  touches the same name — the concurrent write is not silently lost and the race
  detector is clean. Model the `watch.go` merge-vs-`Bind` shape.
- [ ] 1.2 Red test: repeated overwrite merges of the same names with changing
  value sizes leave `e.retainedBytes` equal to the true sum of live-cell retained
  bytes (assert via a sum-check helper).
- [ ] 1.3 Red test: a `settleRetained` batch spanning two meters where the second
  charge fails releases the first charge and leaves no cell charged-without-owner
  (assert both meters' net charge is zero and no leaked backing).
- [ ] 1.4 Characterization: ordinary single-threaded hot-reload merge and
  single-meter settlement behave exactly as today.

## 2. Implementation

- [ ] 2.1 `MergeInto`/`MergeIntoCanonical`: make the merge atomic — hold the
  target lock across commit (moving `ReleaseRetained` out of the committed
  section) or version-revalidate each cell before overwrite; align the doc
  comment with the chosen guarantee.
- [ ] 2.2 Overwrite branch: adjust `e.retainedBytes`/`retainedSlots` by the
  new−old delta.
- [ ] 2.3 `settleRetained`: sort meter groups by a stable key; on partial failure
  release already-applied charges before returning; keep per-cell finalization
  all-or-nothing.

## 3. Integration

- [ ] 3.1 `go test ./... -race` green, including the concurrent merge-vs-write
  test.
- [ ] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing.
- [ ] 3.3 Shared-engine concurrency characterization pins still hold.

## 4. Verification

- [ ] 4.1 `openspec validate --strict retained-merge-settlement-correctness`.
- [ ] 4.2 CHANGELOG `[Unreleased]` under Fixed: env merge is now atomic against
  concurrent writes, the retained aggregate stays consistent across overwrites,
  and multi-meter settlement rolls back on partial failure.
