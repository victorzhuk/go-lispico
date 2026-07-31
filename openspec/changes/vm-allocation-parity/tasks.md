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
- [ ] 3.2 For any cell 1.3 says is not fixable, amend ADR 0008's threshold for
      that tier with the profile and figures behind the amendment, per this
      change's spec delta. Do not reassign the cell's tier to pass it. An
      amendment that cannot name its evidence is a relabelling with extra
      steps.

      Open, and deliberately so. `guard-nil` is the one cell 1.3 leaves
      unfixed: it drops from +20.83% to +0.09% bytes (1080 B/op under the
      Evaluator against 1081 under the VM, locally) and still fails its
      data-dominated non-increasing bound, by a byte. A matched-N profile diff
      shows the residual is not a removable site — `core/vm.(*VM).run`'s
      ~96 B/op is offset almost exactly by `core.(*engine).Eval`'s ~−96 B/op,
      two engines' honest cost for the same logical work, plus ~10–13 B/op of
      `sync.Pool` GC-cadence churn on `vmPool` that varies run to run.

      The amendment is therefore the expected outcome rather than a fallback,
      but it cannot be written yet: this change's own spec delta requires an
      amendment to name the profile and figures behind it, and the only figures
      that license one come from a hosted run. Local B/op is deterministic per
      tree *and toolchain* — CI pins Go 1.24 against this box's 1.26.5 — so a
      one-byte margin is exactly the size of figure that does not survive the
      move. Size it from section 4's profile, and keep the allowance named and
      per-cell rather than loosening the data-dominated tier as a whole: the
      other twelve cells carrying that tier are reader-only, measure
      bit-identical bytes across modes, and should keep the exact bound.
- [x] 3.3 CHANGELOG `[Unreleased]`: what moved, on which cells, and by how
      much. If a threshold was amended rather than an allocation reduced, say
      that plainly — the two are not interchangeable in a release note.

## 4. Re-profile

- [ ] 4.1 Push the work to `origin/master` and dispatch
      `.github/workflows/release.yml` against it, per `consumer-release-gate`'s
      rule that a `workflow_dispatch` run is a valid profile source and that
      `--ref master` resolves against the remote branch. Confirm the run's head
      sha matches the tree the profile is meant to measure.
- [ ] 4.2 Commit the new profile under
      `internal/perfgate/testdata/profile-<run id>/` with the same provenance
      README shape as `profile-30614184386`, and point
      `internal/perfgate/tiers.json`'s comment at it. The existing profile
      measures a tree without these fixes and stops licensing the tiers the
      moment the allocation figures move.
- [ ] 4.3 Re-check every cell's tier against the new profile, not only the
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

- [ ] 4.4 Pin the committed profile with a test in
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
- [ ] 5.3 `bin/perfgate` against the new committed profile in
      first-authorization mode. Record the verdict as it comes out. A cell
      still failing is a result to report, not a reason to revisit section 2.
- [ ] 5.4 State what remains open. If the verdict is clean,
      `release-gate-activation` 1.2, 4.2, and 5.1 become reachable on the next
      release cut whose gate passes — the cut is not this change's to make
      either.
