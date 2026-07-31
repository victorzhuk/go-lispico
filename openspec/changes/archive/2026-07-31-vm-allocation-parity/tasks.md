# Tasks — vm-allocation-parity

## 1. Locate the allocation

- [x] 1.1 Profile `Goldset/guard-nil` under both execution modes and identify
      where the VM's extra bytes come from. The profile figure is 1160 B/op
      under the Evaluator against 1385 under the VM (+19.40%), with allocation
      count +3.12% and latency flat at +2.65% — one to a few extra objects per
      operation, not a hot-path difference. Record the allocation site with
      evidence (`-benchmem` plus an allocation profile), not an inference from
      reading the code.

      The fixture is `internal/goldset/testdata/guard-nil.lisp`. Whatever the
      site turns out to be, state whether it is specific to this fixture's
      shape or common to every VM evaluation and merely visible here.
- [x] 1.2 Do the same for `Goldset/kw-lookup` (bytes −9.04%, latency −20.00%)
      and `Goldset/merge-config` (bytes −19.96%, latency −20.59%). Both are
      engine-sensitive by latency and short of the tier's 20% allocation floor.
      `merge-config` misses by 0.04 points, so establish first whether a
      sub-1% allocation win exists on its VM path before treating it as the
      same problem as `kw-lookup`, which is short by 11 points.
- [x] 1.3 State whether the three cells share one cause or have three. The
      answer decides whether this change carries one fix or several, and it is
      cheaper to answer from the profiles in 1.1 and 1.2 than to discover it
      mid-implementation.

### Finding — one cause, not three

`runtime/eval.go`'s `sha256Hash` did `sha256.Sum256([]byte(s))`. The `[]byte(s)`
conversion heap-copied the entire fixture source on every VM `Eval`; the site
keys the striped chunk cache (`runtime/eval.go:699`) and the Evaluator path
never reaches it.

Evidence, not inference: an allocation profile of `Goldset/guard-nil` under both
modes (`-memprofile`, `go tool pprof`) shows `sha256Hash` present only in the VM
profile, at 17.86% of `alloc_space` (~233 B/op of 1305) and 3.49% of
`alloc_objects` (~1.08 of 31 allocs/op). That is the whole of the measured
+225 B/op, +1 alloc/op gap. Every other per-site delta between the two profiles
is under 15 B/op, which is memprofile sampling noise at the default rate. The
fixture is 215 bytes, landing in Go's 224-byte size class — the observed figure.

The cost is common to every VM evaluation, not specific to this fixture's shape.
It is merely most visible on the smallest fixture, where a fixed per-`Eval` cost
is the largest share of the total. `kw-lookup` and `merge-config` pay the same
allocation; they were short of the engine-sensitive floor rather than above a
non-increasing bound, so it read as a smaller shortfall instead of a regression.

One fix therefore serves all three cells, and it is a `runtime/` change rather
than a `core/vm` one — contrary to this change's Impact section, which expected
`core/vm` to be implicated.

## 2. Close the startup-tier hole

- [x] 2.1 Settle what ADR 0008's startup tier bounds. Its stated rule is
      "within 5%, or at most 1 ms and 256 KiB absolute overhead", under a
      section whose opening rule is that no cell may regress beyond its tier's
      budget; `evaluateStartup` applies neither the bytes nor the
      allocation-count non-increase bound that every other tier applies, and
      every cell in the current corpus clears the absolute bound. Decide
      whether that is the intent.
- [x] 2.2 Make `internal/perfgate/perfgate.go` match the decision, and record
      the decision where a reader meets it: ADR 0008's Thresholds section and
      `internal/perfgate/tiers.json`'s comment. If the answer is that the
      absolute bound stands alone, the code needs no change and the two
      documents do.
- [x] 2.3 Cover the decision with a test in `internal/perfgate/perfgate_test.go`
      — a startup cell whose allocation moved, asserting whatever 2.1 settled.
      The current behavior has no test either way, which is why the hole was
      invisible.

## 3. Fix or amend

- [x] 3.1 Reduce the VM's allocation on the cells 1.3 says are fixable, and
      verify the reduction locally against the gold set under both modes before
      claiming it. A local figure does not license a tier and is not evidence
      the gate passes; it is evidence the change is worth taking to a hosted
      run.
- [x] 3.2 For any cell 1.3 says is not fixable, amend ADR 0008's threshold for
      that tier with the profile and figures behind the amendment, per this
      change's spec delta. Do not reassign the cell's tier to pass it. An
      amendment that cannot name its evidence is a relabelling with extra
      steps.

      `guard-nil` is the one cell 1.3 leaves unfixed: it drops from +20.83% to
      +0.09% bytes and still fails its data-dominated non-increasing bound, by
      a byte. A matched-N profile diff shows the residual is not a removable
      site — `core/vm.(*VM).run`'s ~96 B/op is offset almost exactly by
      `core.(*engine).Eval`'s ~−96 B/op, two engines' honest cost for the same
      logical work, plus ~10–13 B/op of `sync.Pool` GC-cadence churn on
      `vmPool` that varies run to run.

      **Resolved: a named 4 B/op allowance, and the ordering defect fixed with
      it.** The two were one decision, as the note under section 4 said. The
      allowance is sized from hosted figures rather than the local ones this
      task originally carried: three dispatch runs — 30630796967, 30637802780,
      and 30639778105 — each measured 1128 B/op under the Evaluator against
      1129 under the VM, +0.09%, p=0.000, 0% CI on both arms, allocations
      bit-identical at 32. The allowance lives in `internal/perfgate/tiers.json`'s
      `bytesAllowanceBOp` map and its number and evidence in ADR 0008's
      Thresholds section, so the bound is answerable without reading
      `perfgate.go` as the spec delta requires. It is named and per-cell: the
      other thirteen data-dominated cells are reader-only, measure
      bit-identical bytes across modes, and keep the exact bound. No cell's
      tier changed.

      The ordering fix hoists the bytes and allocation non-increase checks
      above the latency-significance gate in `evaluateNonRegression`,
      `evaluateWithinTolerance`, and `evaluateStartup`.
      `evaluateEngineSensitiveImprovement` hoists only its allocations check:
      its bytes check is a 20%-improvement floor, and benchstat `~` parses to
      `DeltaPct = 0`, so hoisting a floor would turn "not yet significant" into
      an immediate FAIL and delete the doubled-benchtime rerun for that tier.

      `verdict.txt` for profile 30637802780 is byte-identical after both
      changes, verified by running the gate against the committed profile —
      `guard-nil`'s bytes now clear the allowance instead of never being
      checked, and the cell still reads INCONCLUSIVE on its non-significant
      latency (p=0.055). That is why `TestPinnedProfile` cannot discriminate
      this fix, and why direct `Evaluate` tests pin the new ordering and the
      allowance's within/past/unlisted cases instead.
- [x] 3.3 CHANGELOG `[Unreleased]`: what moved, on which cells, and by how
      much. If a threshold was amended rather than an allocation reduced, say
      that plainly — the two are not interchangeable in a release note.

## 4. Re-profile

- [x] 4.1 Push the work to `origin/master` and dispatch
      `.github/workflows/release.yml` against it, per `consumer-release-gate`'s
      rule that a `workflow_dispatch` run is a valid profile source and that
      `--ref master` resolves against the remote branch. Confirm the run's head
      sha matches the tree the profile is meant to measure.
- [x] 4.2 Commit the new profile under
      `internal/perfgate/testdata/profile-<run id>/` with the same provenance
      README shape as `profile-30614184386`, and point
      `internal/perfgate/tiers.json`'s comment at it. The existing profile
      measures a tree without these fixes and stops licensing the tiers the
      moment the allocation figures move.
- [x] 4.3 Re-check every cell's tier against the new profile, not only the
      three this change targets. An allocation change can move a cell that was
      not its target across the engine-sensitive bytes floor in either
      direction.

### Hazard — the race leg can flake the hosted run out of being a profile

`plugins/json`'s `TestDecodeHashMap_Scaling` (`plugins/json/json_test.go:789`)
asserts a 4000-key/2000-key decode-time ratio below 3.0. It is a wall-clock
assertion in a package that uses `t.Parallel()`, so it measures whatever
contention the rest of the suite creates. Observed here at 3.16, 4.46, and 6.25
— worse than the 3.83/3.94 recorded previously — failing roughly one run in
eight under load, on `master` and on this change's tree alike.

It is not reachable from this change: `plugins/json`'s test binary links `core`
only, never `runtime/` or `internal/perfgate`, and `plugins/` is byte-identical
to `master` here. But `release.yml` treats a clean race suite as one of the
three legs that decide whether a run is usable as a classification profile, so
a flake on the hosted run discards the run rather than the test. If task 4.1's
dispatch comes back with only this cell red, re-dispatch; do not read it as a
finding about the engine. Fixing the assertion belongs to its own change.

### Note — how to read `guard-nil` on the new profile

The next hosted run is first-authorization mode: `release.yml` selects
non-regression only when a stored baseline exists, and none does. Under that
mode `guard-nil`, as the only data-dominated `Goldset/*` cell, routes to
`evaluateWithinTolerance`, whose latency check is **two-sided** — `math.Abs`
against ±5%.

Removing the per-`Eval` source copy can only make the cell faster. The committed
profile records it at +2.65% latency. A swing past −5% therefore registers as a
gate finding by that two-sided rule, and the finding would be that a cell
classified mode-invariant is no longer mode-invariant — a tier question, not an
allocation one. The alternative outcome is that the delta loses significance,
which returns INCONCLUSIVE and then resolves to PASS. Neither is a surprise;
record which one happened rather than re-opening section 2.

**Outcome: the second one, and it exposed a third defect.** Run `30630796967`
reads `guard-nil`'s latency as `~` at p=0.424 — INCONCLUSIVE, resolved to PASS.
Its bytes read 1128 B/op under the Evaluator against 1129 under the VM: +0.09%
at p=0.000, reproducing the local one-byte figure exactly.

The cell therefore passes with its tier's bytes bound **never evaluated**.
`evaluateWithinTolerance` returns INCONCLUSIVE the moment latency is not
significant, before it reaches `nonIncreasing`, and `Resolve` collapses that to
PASS under ADR 0008's burden-of-proof rule. All four tier evaluators share the
ordering — `evaluateWithinTolerance`, `evaluateNonRegression`,
`evaluateEngineSensitiveImprovement`, and `evaluateStartup`, the last being the
function this change modified.

This is a third distinct defect, not the startup hole section 2 closed and not
the benchstat-`~` blind spot ADR 0008 now records. It is also this change's own
spec delta being violated by the gate it ships: a tier states a bytes bound and
the implementation does not apply it. No earlier profile could have surfaced it
— every other INCONCLUSIVE cell is bit-identical on bytes at p=1.000, so
`guard-nil` is the first cell in the corpus with a non-significant latency and a
significant bytes delta.

Fixed under 3.2, together with the amendment it was bound to. The verdict path
did change for every tier, but only in one class of case: a cell whose bytes or
allocations regressed while its latency delta was not significant now fails on
attempt one instead of resolving to a pass. No cell in the committed profile
falls in that class, so `verdict.txt` did not move.

A later run made the ordering defect concrete rather than theoretical. Dispatch
run `30639778105`, on a tree byte-identical to the profile's on every engine
path, read `guard-nil`'s latency as significant for the first time (+2.46%,
p=0.000) — which cleared the significance gate, cleared the ±5% tolerance,
reached `nonIncreasing`, and returned `FAIL (bytes increased by 0.09%)`. The
cell's green was never a property of the engine; it was a property of whether
that run's latency happened to reach significance.

- [x] 4.4 Pin the committed profile with a test in
      `internal/perfgate/perfgate_test.go`: parse the profile's two benchmark
      files, evaluate every cell against `tiers.json`, and assert the verdict
      the profile's own `verdict.txt` records. Nothing reads that directory
      today, so a tier and the profile licensing it can drift apart with only
      a README warning between them. The test is also what makes replacing a
      profile a deliberate act — swapping the files fails it until the
      expected verdict is updated alongside.

## 5. Verify

- [x] 5.1 `openspec validate --strict` on this change.
- [x] 5.2 `go build ./... && go test ./... -count=1` and
      `go test -race -count=1 ./...`.
- [x] 5.3 `bin/perfgate` against the new committed profile in
      first-authorization mode. Record the verdict as it comes out. A cell
      still failing is a result to report, not a reason to revisit section 2.
- [x] 5.4 State what remains open. If the verdict is clean,
      `release-gate-activation` 1.2, 4.2, and 5.1 become reachable on the next
      release cut whose gate passes — the cut is not this change's to make
      either.

### What remains open

The gate passes. Hosted run `30630796967` against `2910e79` returned all 26
cells green, the first passing gate in the project's history. `tiers.json` is
now licensed by the later profile `30637802780`, which added the Call cell and
reproduced every figure this section quotes. Locally,
`bin/perfgate` against that committed profile reports 13 PASS, 13 INCONCLUSIVE,
0 FAIL, exit 2 — the pre-rerun state, since the hosted job reruns
non-significant cells at doubled benchtime before resolving them.

Open, in the order they have to be settled:

1. **`release-gate-activation` 1.2, 4.2, 5.1.** Reachable but not reached. All
   three wait on a *release cut* whose gate passes, which stores the
   `bench-vm.txt` baseline asset. A `workflow_dispatch` run carries no release
   identity, so this one stored nothing and consumed no baseline slot. The cut
   is not this change's to make. Do not hand-upload the asset to close them.

2. **The bytes allowance reads wider than the one cell using it.** An entry in
   `bytesAllowanceBOp` is honored wherever a bytes non-increasing bound is
   applied — data-dominated, concurrent, and startup cells in either mode, and
   engine-sensitive cells once in non-regression. It is inert against the
   engine-sensitive 20% improvement floor, which `nonIncreasing` does not
   govern, so an entry on such a cell would do nothing at first authorization
   and take effect only afterwards. `LoadTierConfig` validates that an
   allowance names a known cell, not that its tier can honor it. Documented in
   ADR 0008 and in the loader's own comment; no cell triggers it today.

3. **`plugins/json`'s `TestDecodeHashMap_Scaling`.** A wall-clock ratio
   assertion that fails roughly one run in eight under load, on `master` as
   much as here. Harmless to this change and outside it, but the hosted job
   treats a clean race suite as one of the three legs that make a run usable as
   a profile, so it can discard a future run. Its own change.

4. **The first non-test `unsafe` import.** `runtime/eval.go` now carries one.
   The usage is sound and tested, but nothing in the repo states when `unsafe`
   is acceptable here and no linter gates it; the only durable record is a
   function comment. Worth an explicit invariant rather than a precedent set by
   accident.
