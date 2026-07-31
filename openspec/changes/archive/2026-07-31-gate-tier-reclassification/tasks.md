# Tasks — gate-tier-reclassification

## 1. Establish the profile of record

- [x] 1.1 Produce the source run and confirm it carries the paired output. Its
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

      **Settled by run `30614184386`.** The local commits (22 by then) were
      pushed on 2026-07-31, moving `origin/master` from `fee885a` to
      `4607f1e`, and the workflow was dispatched against it. The run's head
      sha is `4607f1e2adb25d5fe24e788e51f9a22efb51528f` — identical to the
      local `master` tip, which is the assertion this task exists to make.
      Gold set green under both modes, race suite green, paired benchmark
      completed, evidence artifact carries all three files; "Determine gate
      mode" and "Store VM baseline" skipped as a dispatch run must. Fixed
      parameters as committed in `release.yml`: `GOMAXPROCS=2`,
      `BENCHTIME=200ms`, `BENCH_COUNT=10`. Its verdict fails on seven cells,
      which is the expected pre-reclassification result.
- [x] 1.2 Commit the profile. Files go under
      `internal/perfgate/testdata/profile-30610843591/` (or the actual run id),
      with a short `README.md` recording provenance: run id, workflow, event,
      ref, head sha, date, fixed parameters, and the fact that its verdict
      reflects the *pre-reclassification* tiers. Do not edit the benchmark
      output itself.

      Committed verbatim under
      `internal/perfgate/testdata/profile-30614184386/`, with a `README.md`
      carrying the provenance table, what the run did, the reproduction
      command, the per-cell tier table, and the disagreements with run
      `30561584997`.
- [x] 1.3 Point `internal/perfgate/tiers.json`'s comment at the committed
      profile path. The comment already says "Reclassification requires a
      committed baseline profile justifying it"; it should now name which one.

      The comment names the profile path, the run id, the head sha, and the
      three fixed parameters, and points at the profile README for the
      per-cell figure and reasoning.

## 2. Reclassify

- [x] 2.1 Reclassify each of the eight cells the first hosted run found
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

      **Six cells changed tier; the eight split three ways.** The full
      thirteen-cell table with latency, bytes, and allocation figures lives in
      the profile README, which is the artifact that licenses them.

      Five reclassified data-dominated → engine-sensitive, each cutting
      latency by 20% or more under the VM, which a mode-invariant cost cannot
      do: `queue-promote` (−66.47%), `pipeline` (−46.20%), `registry-fold`
      (−31.64%), `text-render` (−25.46%), `merge-config` (−20.59%). They
      failed only because a two-sided ±5% band fails a cell for improving.

      One reclassified engine-sensitive → data-dominated: `guard-nil`, whose
      latency is flat across modes (+2.65%). Data-dominated names that shape
      correctly. It still fails, on bytes.

      Two needed no change. `twice-macro` passes as committed on this tree
      (latency −18.89%, bytes −20.51%); `kw-lookup` keeps engine-sensitive,
      which its −20.00% latency earns, and fails the allocation half of that
      tier at −9.04%.

      **Disagreements with run `30561584997`, all recorded in the profile
      README:** `twice-macro` FAIL → PASS (bytes −12.65% → −20.51%),
      `kw-lookup` bytes −5.36% → −9.04%, `merge-config` latency −18.00% →
      −20.59%. The two runs also used different hosted CPUs (AMD EPYC 7763
      against Intel Xeon Platinum 8573C), which is a further reason a tier is
      licensed by one profile rather than by agreement across runs. The first
      disagreement is the consequential one — the proposal's
      eight-misclassified-cells table is wrong on the tree that matters, and
      the proposal now says so.
- [x] 2.2 Confirm the corrected tiers produce a clean verdict against the
      committed profile:
      `go build -o bin/perfgate ./cmd/perfgate && bin/perfgate -old <profile>/bench-evaluator.txt -candidate <profile>/bench-vm.txt -tiers internal/perfgate/tiers.json -mode first-authorization -out /dev/stdout`.
      This is a self-consistency check, not evidence of a future release
      passing — the next release measures a different tree.

      **The verdict is not clean, and no tier assignment makes it clean.**
      Ten of thirteen `Goldset/*` cells pass. Three fail, all on the
      allocation axis and all under the tier that describes them honestly:

      | cell | tier | failure |
      | --- | --- | --- |
      | `Goldset/guard-nil` | data-dominated | bytes increased by 19.40% |
      | `Goldset/kw-lookup` | engine-sensitive | bytes −9.04%, floor is −20% |
      | `Goldset/merge-config` | engine-sensitive | bytes −19.96%, floor is −20% |

      Checked against every tier the gate defines. `engine-sensitive` fails
      all three: `kw-lookup` and `merge-config` on the 20% bytes floor, and
      `guard-nil` on the 15% latency floor before bytes are even reached.
      `data-dominated` and `concurrent` both
      apply `nonIncreasing` to bytes, which `guard-nil` breaks outright and
      which the other two clear only by moving latency well past a ±5% band.
      `startup` would pass all three — `evaluateStartup` never checks bytes at
      all, and every cell in this corpus clears its 1 ms / 256 KiB absolute
      bound — but none of the three is a startup cell, and labelling them so
      would buy a green verdict by describing them falsely. That `startup` is
      an unconditional pass for any cell in this corpus is a latent defect in
      the gate, recorded here and carried into the follow-up.

      The three failures are engine findings, not label errors. They are owned
      by `vm-allocation-parity`.
- [x] 2.3 Decide what to do about the `GoldsetParse/*` cells. All thirteen came
      back INCONCLUSIVE on the first hosted run ("latency delta not
      statistically significant"), which is what a mode-invariant cost
      *should* produce and what `tiers.json`'s own comment predicted — but the
      gate reports it as inconclusive rather than as the invariant holding, and
      an inconclusive improvement cell fails after rerun. Decide: leave them
      data-dominated and accept that they can only ever be inconclusive or
      failing under first authorization, or state what mechanism change would
      let a cell assert "correct because it did not move". Do not build that
      mechanism here — decide, and name the owner if it needs building.

      **Decision: they stay data-dominated.** The committed profile reports
      all thirteen as not statistically significant on latency (p = 0.22–0.93)
      and bit-identical on bytes and allocations (p = 1.000). The invariant is
      structural, not just observed: `BenchmarkGoldsetParse`
      (`internal/goldset/bench_test.go`) never reads `GOLDSET_MODE`, so both
      runs execute identical code for these cells.

      **The task's premise is wrong on one cell, and the correction supports
      the decision.** Not all thirteen came back INCONCLUSIVE on the first
      hosted run: `30561584997`'s verdict records
      `GoldsetParse/text-render-2: PASS`, because that run measured its
      latency delta as significant (+1.39%, p = 0.033) and small enough to
      clear the ±5% band, with bytes and allocations unchanged. A
      data-dominated cell can therefore pass on its own evidence rather than
      by burden-of-proof default — the tier working as intended, on the very
      cell the premise counted as inconclusive. Twelve of thirteen were
      inconclusive; the thirteenth passed.

      Data-dominated is the decidable classification: under first
      authorization these are non-regression claims, so `Resolve()` collapses
      a still-inconclusive cell to PASS after the one allowed rerun. The
      task's premise that "an inconclusive improvement cell fails after rerun"
      applies to engine-sensitive, which is exactly why they are not
      classified there.

      Two mechanism gaps are recorded rather than built:

      1. No tier lets a cell assert that not moving is the correct outcome.
         Adding one means an `invariant` tier in `internal/perfgate` that
         treats a non-significant delta as PASS on its own evidence instead of
         by burden-of-proof default.
      2. `cmd/perfgate` returns exit 1 whenever any cell fails, and
         `release.yml`'s rerun step is gated on exit 2 — so while any cell
         fails, these thirteen never reach the rerun that would resolve them.
         A positive invariance verdict is worth little without a rerun path
         that survives an unrelated failure.

      Owner: whoever next widens the gate's machinery. Both gaps are named in
      `tiers.json`'s comment so the next reader of that file meets them there.
      Neither blocks this change: the classification is already correct and
      already decidable.

## 3. Record

- [x] 3.1 CHANGELOG `[Unreleased]`: the tier correction and the profile that
      licensed it, with the run id. Do not claim the gate now passes — no
      release has been cut against the corrected tiers.

      Entry added under `[Unreleased] / Changed`, naming the profile path and
      run `30614184386`, and stating plainly that the gate does not pass on
      the corrected tiers and why.

## 4. Verify

- [x] 4.1 `openspec validate --strict` on this change.
- [x] 4.2 `go build ./... && go test ./internal/perfgate/... ./internal/goldset/ -count=1`.
- [x] 4.3 State plainly what remains open after this change lands:
      `release-gate-activation` 1.2, 4.2, and 5.1 need a release cut whose gate
      passes, and the cut is not this change's to make either. This change
      makes them reachable; it does not close them.

      **They are not yet reachable, and this change is no longer what stands
      between them and completion.** `release-gate-activation` 1.2 (store the
      `bench-vm.txt` asset), 4.2 (confirm it downloads by tag), and 5.1
      (re-baseline after the reader axis) all wait on a release whose gate
      passes. The corrected tiers do not produce one: `guard-nil`,
      `kw-lookup`, and `merge-config` still fail on allocated bytes. Cutting a
      release today would skip the store step exactly as `v0.11.0` did.

      What changed is which finding blocks them. Before this change it was a
      misclassification in the gate's own corpus; now it is a measured
      allocation gap in the VM, owned by `vm-allocation-parity`. The release
      cut remains outside this change either way.
