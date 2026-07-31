# Tasks — gate-tier-reclassification

## 1. Establish the profile of record

- [ ] 1.1 Produce the source run and confirm it carries the paired output. Its
      `consumer-gate-evidence` artifact must contain `bench-evaluator.txt`,
      `bench-vm.txt`, and `verdict.txt`. Record the run id, ref, head sha,
      date, and the three fixed parameters the run used (`GOMAXPROCS=2`,
      `BENCHTIME=200ms`, `BENCH_COUNT=10`) — a profile without its parameters
      cannot license anything, because a tier is only meaningful against the
      conditions it was measured under.

      If the run fails on the correctness or race leg, it is not a usable
      profile: dispatch another. A failed *verdict* is expected and does not
      disqualify it — that is the whole point of the second scenario in this
      change's spec delta.

      **Run `30610843591` proves the mechanism and cannot serve as the
      profile.** Dispatched 2026-07-31 against `master`, it did exactly what
      this change claims a dispatch run does: "Determine gate mode" and "Store
      VM baseline" skipped, gold set green, race suite green, paired bench
      completed, `perfgate` produced a first-authorization verdict, and
      "Upload release evidence" published all three files. Only "Enforce
      performance gate verdict" failed, on the same eight misclassified cells.

      But its head sha is `fee885a` — GitHub resolves `--ref master` against
      the *remote* branch, and `origin/master` is 20 commits behind the local
      one. `fee885a` is `v0.11.0`'s tree, so this run re-measured the exact
      tree whose verdict is already known, and its figures reproduce run
      `30561584997`'s to within a few tenths of a percent (`queue-promote`
      −62.37% against −63.02%, `pipeline` −40.66% against −40.50%,
      `twice-macro` bytes −12.65% in both). Using it would re-fit the tiers to
      a judged release's candidate results, which this change's own fourth
      scenario forbids.

      A usable profile therefore needs a dispatch against the tree the next
      release is cut from, which means the 20 local commits must be pushed
      first. That push is not this change's to make; whoever makes it should
      dispatch afterwards and record the new run id here.
- [ ] 1.2 Commit the profile. Files go under
      `internal/perfgate/testdata/profile-30610843591/` (or the actual run id),
      with a short `README.md` recording provenance: run id, workflow, event,
      ref, head sha, date, fixed parameters, and the fact that its verdict
      reflects the *pre-reclassification* tiers. Do not edit the benchmark
      output itself.
- [ ] 1.3 Point `internal/perfgate/tiers.json`'s comment at the committed
      profile path. The comment already says "Reclassification requires a
      committed baseline profile justifying it"; it should now name which one.

## 2. Reclassify

- [ ] 2.1 Reclassify each of the eight cells the first hosted run found
      misclassified, against the committed profile's own figures — not against
      run `30561584997`'s, which measured a different tree and is the judged
      candidate of a release whose verdict is known. Record the profile's
      figure next to each cell that changes tier, and the reasoning for the
      tier chosen. Cells to settle: `Goldset/queue-promote`,
      `Goldset/pipeline`, `Goldset/registry-fold`, `Goldset/text-render`,
      `Goldset/merge-config`, `Goldset/guard-nil`, `Goldset/twice-macro`,
      `Goldset/kw-lookup`.

      A cell whose profile figure disagrees with the first run's is a finding
      worth recording, not a reason to pick whichever supports the existing
      tier.
- [ ] 2.2 Confirm the corrected tiers produce a clean verdict against the
      committed profile:
      `go build -o bin/perfgate ./cmd/perfgate && bin/perfgate -old <profile>/bench-evaluator.txt -candidate <profile>/bench-vm.txt -tiers internal/perfgate/tiers.json -mode first-authorization -out /dev/stdout`.
      This is a self-consistency check, not evidence of a future release
      passing — the next release measures a different tree.
- [ ] 2.3 Decide what to do about the `GoldsetParse/*` cells. All thirteen came
      back INCONCLUSIVE on the first hosted run ("latency delta not
      statistically significant"), which is what a mode-invariant cost
      *should* produce and what `tiers.json`'s own comment predicted — but the
      gate reports it as inconclusive rather than as the invariant holding, and
      an inconclusive improvement cell fails after rerun. Decide: leave them
      data-dominated and accept that they can only ever be inconclusive or
      failing under first authorization, or state what mechanism change would
      let a cell assert "correct because it did not move". Do not build that
      mechanism here — decide, and name the owner if it needs building.

## 3. Record

- [ ] 3.1 CHANGELOG `[Unreleased]`: the tier correction and the profile that
      licensed it, with the run id. Do not claim the gate now passes — no
      release has been cut against the corrected tiers.

## 4. Verify

- [ ] 4.1 `openspec validate --strict` on this change.
- [ ] 4.2 `go build ./... && go test ./internal/perfgate/... ./internal/goldset/ -count=1`.
- [ ] 4.3 State plainly what remains open after this change lands:
      `release-gate-activation` 1.2, 4.2, and 5.1 need a release cut whose gate
      passes, and the cut is not this change's to make either. This change
      makes them reachable; it does not close them.
