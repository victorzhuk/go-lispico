# Tasks — release-gate-activation

## 0. Decide the standing trigger

- [x] 0.1 Choose: keep `workflow_dispatch`-only (document a manual per-release
      cadence in `release.yml`'s header comment), or re-enable
      `release: published` with the post-hoc semantics already described
      there. Either choice, record the reasoning here.

      **Decided: re-enable `release: published`.** The comment that kept it
      off reasoned that firing on every release was premature because the
      workflow "has never been exercised end-to-end" — a condition that can
      only ever be cleared by firing it. Held long enough, that caution is
      what produced the gap this change exists to close. The post-hoc
      semantics are acceptable because the workflow cannot block a release
      either way (Actions has no pre-publish hook), so arming it costs
      nothing a manual cadence would have preserved.

      **Armed as `types: [released]`, not `types: [published]`.** The
      commented-out block said `published`, but `published` also fires for
      pre-releases, and the first armed run is one-shot: it runs in
      first-authorization mode and its `bench-vm.txt` becomes the baseline
      every later release is measured against. A pre-release would silently
      consume that slot and pin the baseline to an rc. `released` fires when
      a stable release goes public and when a pre-release is promoted to one
      — exactly when a baseline should be captured. The repo has never cut a
      pre-release, so this is a guard against a future footgun rather than a
      fix for a live one.

      The decision also settles 1.2. A `workflow_dispatch` run satisfies 1.1
      on its own — `release.yml`'s "Evaluate performance gate" step defaults
      `mode` to `first-authorization` when the skipped gate-mode step leaves
      it empty — but it carries no release identity, so both the baseline
      lookup and the `gh release upload` step are gated off
      (`if: github.event_name == 'release'`). Only a release-triggered run
      can store the asset that 1.2 requires without a hand-placed upload.

## 1. First real run

- [x] 1.1 Trigger `.github/workflows/release.yml` against **`v0.11.0`, the
      next tag cut from `master`** — naming the run this verdict is deferred
      to, per this change's own spec delta. Confirm: gold set green both
      modes, race suite green, paired benchmark run completes, `cmd/perfgate`
      produces a verdict (first-authorization mode, since no prior asset
      exists).

      Not `v0.10.0`: the gate code is byte-identical at that tag
      (`release.yml`, `internal/goldset`, `internal/perfgate`, `cmd/perfgate`
      all unchanged since), so a `v0.10.0` run would exercise the pipeline
      correctly — but `master` is 27 commits ahead with real evaluator work,
      including the lean call boundary, so the baseline would be stale the
      moment it was stored. Cutting the tag from `master` also brings the
      boundary work inside the measured tree, which is what 2.3 needs.

      **Done 2026-07-30.** `v0.11.0` was cut from `master` (`fee885a`) and
      published as a stable release, which fired the workflow for the first
      time in the repository's history — run `30561584997`. Every
      confirmation this task asks for holds: gold set green under both
      execution modes, race suite green, paired Evaluator/VM run completed,
      and `cmd/perfgate` produced a verdict in first-authorization mode.

      **The verdict is FAIL, and it is a finding about the tiers, not about
      the engine.** `internal/perfgate/tiers.json` classifications do not
      survive contact with the hosted runner, and several are close to
      inverted:

      | cell | committed tier | measured | verdict |
      | --- | --- | --- | --- |
      | `Goldset/queue-promote` | data-dominated (±5%) | latency −63.02% | FAIL |
      | `Goldset/pipeline` | data-dominated (±5%) | latency −40.50% | FAIL |
      | `Goldset/registry-fold` | data-dominated (±5%) | latency −27.97% | FAIL |
      | `Goldset/text-render` | data-dominated (±5%) | latency −22.78% | FAIL |
      | `Goldset/merge-config` | data-dominated (±5%) | latency −18.00% | FAIL |
      | `Goldset/guard-nil` | engine-sensitive (≥15%) | latency −2.83% | FAIL |
      | `Goldset/twice-macro` | engine-sensitive (≥20% bytes) | bytes −12.65% | FAIL |
      | `Goldset/kw-lookup` | engine-sensitive (≥20% bytes) | bytes −5.36% | FAIL |

      Five cells assumed mode-insensitive are the most engine-sensitive in
      the corpus — they fail for improving too much against a two-sided
      tolerance — while three assumed engine-sensitive barely move. PASS:
      `counter-closure`, `loop-sum`, `route-decision`, `rule-load`,
      `safe-parse`, and `GoldsetParse/text-render`.

      All 13 `GoldsetParse/*` cells came back INCONCLUSIVE ("latency delta
      not statistically significant"), which is what a mode-invariant cost
      *should* produce — the tiers.json comment predicted exactly this — but
      the gate reports it as inconclusive rather than as the invariant
      holding. A cell that is correct precisely because it does not move has
      no way to say so.

      Two mechanism defects this exposed, neither previously known:

      1. **An inconclusive cell is never resolved if any other cell fails.**
         The doubled-benchtime rerun is gated on
         `steps.perfgate.outputs.exit_code == '2'`. Real failures make
         perfgate exit 1, so exit 1 masks exit 2 and the rerun is skipped —
         it skipped here despite 12 inconclusive cells.
      2. **A failing verdict blocks baseline storage**, so 1.2 cannot
         complete while the tiers are wrong (see 1.2).

      Reclassification is deliberately NOT done here: ADR 0008 requires a
      committed baseline profile to justify a tier, this change's own Impact
      scopes `tiers.json` to `gate-corpus-cl-and-recursion`, and editing the
      pass/fail surface mid-activation is the drift task 2.3 already
      declined. What changed is that the profile now exists — run
      `30561584997`'s `bench-evaluator.txt` and `bench-vm.txt`, retained as
      the `consumer-gate-evidence` artifact.
- [x] 1.2 Store the run's `bench-vm.txt` as a release asset. Confirm it is
      downloadable via `gh release download` the way `release.yml`'s
      "Determine gate mode" step expects for the *next* release. With 0.1
      decided, the workflow's own "Store VM baseline on the authorized
      release" step does this; no manual upload is required.

      **Done 2026-07-31. `v0.12.0` carries `bench-vm.txt`, 29830 B, stored by
      the workflow itself.** Release-triggered run `30646304157` (event
      `release`, head `e300ae4`) passed 27/27 and reached "Store VM baseline on
      the authorized release" — the step that every prior run either skipped for
      want of release identity or never reached behind the implicit `success()`
      guard. No hand-upload: the correct order this task insisted on (fix the
      cause, cut a release, let the workflow store the baseline) is the order
      that ran.

      **The coin flip below was closed before the cut, not gambled on.** The
      ordering fix (`fb3ee4f`, bytes and allocs bounds applied before latency
      significance, with a named 4 B/op allowance on `guard-nil`) had never been
      exercised on a runner — it was committed locally and unpushed, and the two
      most recent hosted runs both failed. So `master` was pushed and a
      `workflow_dispatch` pre-flight was fired at `59b8483` first: run
      `30645498605`, PASS. Only then was the tag cut. `Goldset/guard-nil-2`
      reads `PASS (latency delta not statistically significant)` in the release
      run's verdict — the bytes bound is now reached and cleared rather than
      short-circuited past, which is exactly what the fix claimed.

      **Blocked on a coin flip, as of 2026-07-31.** The store step runs only
      after the verdict step passes, and the gate's pass is no longer
      reliable: dispatch run `30639778105` returned
      `Goldset/guard-nil-2: FAIL (bytes increased by 0.09%)` because that
      run's latency delta read +2.46% at p=0.000 — significant, where three
      earlier runs read `~`, so the evaluator reached the bytes bound it had
      always short-circuited past. No engine code differs between that run
      and the profile run that passed. So whether a release cut stores its
      asset now depends on whether that release's run happens to measure
      `guard-nil`'s latency as significant. `vm-allocation-parity` task 3.2
      owns the threshold decision that settles it; until then this task, 4.2,
      and 5.1 are all waiting on the same coin flip rather than on a release
      being cut. Recorded in
      `archive/2026-07-31-gate-deferred-measurements/tasks.md` task 4.3 and in
      `internal/perfgate/testdata/profile-30637802780/README.md`.

      **Not stored. `v0.11.0` carries no assets.** The store step inherits
      the implicit `success()` guard on its `if:`, so a failing verdict
      skips it — and 1.1's verdict failed on tier misclassification. The
      data exists (run `30561584997`'s `consumer-gate-evidence` artifact
      holds `bench-vm.txt`, `bench-evaluator.txt`, and `verdict.txt`), but
      it is a workflow artifact, which expires, not a release asset, which
      is what "Determine gate mode" looks up on the next release.

      Do not hand-upload it to close this task. A baseline captured from a
      run whose verdict failed would pin every future release to numbers the
      gate itself rejected, and hand-placing it defeats the point of 0.1's
      decision that only a release-triggered run can store the asset.
      Correct order: fix the tiers, cut the next release, let the workflow
      store the baseline itself.

## 2. Retroactive verdicts

- [x] 2.1 Against the run from 1.1, record 527f03c's fusion result: does the
      hosted runner's fib-adjacent cells (`loop-sum`, `queue-promote` — both
      contain `(= i N)`/`(+ i 1)`-shaped local/const native-op calls in a
      loop body per their own fixture source, `internal/goldset/testdata/
      loop-sum.lisp` and `queue-promote.lisp`; `docs/profiling-baseline.md`
      separately notes them as the two costliest cells by raw eval time, not
      as fusable-shape evidence) show any movement attributable to
      `OpFusedNativeOp`? Record the number even if it confirms the local
      near-miss.
- [x] 2.2 Run the interleaved benchstat that `vm-batched-ledger-charging`
      (631b2ee) never ran (its own tasks 1.1/3.3/3.4 were left unchecked at
      archive time). Use this run's `bench-vm.txt` as one side; do not edit
      the archived change's `tasks.md` — record the result here, referencing
      that archive by path.
> **Method finding covering 2.1 and 2.2 — read before running either.** Both
> commits under test are already inside the measured tree: `527f03c` (native-op
> fusion) and `631b2ee` (ledger batching) are ancestors of `v0.10.0`, hence of
> any tag cut from `master`. Neither has a build tag or env knob that disables
> it — `core/vm` and `core/compiler` carry no `go:build` constraint and no
> `os.Getenv` call. A first-authorization run therefore measures *with* both
> and produces no counterfactual side. What that run yields for these cells is
> an Evaluator-vs-VM ratio, which is not an attribution to either commit.
>
> Attributing movement needs a deliberate two-ref paired bench at fixed
> concurrency (`527f03c^` vs `527f03c`; `631b2ee^` vs `631b2ee`) on the hosted
> runner — a different exercise from "against the run from 1.1", and one this
> change did not scope. Neither task is dropped: the obligation stands, and
> whichever run settles it must name its two refs.

**Both re-scoped 2026-07-31 to `gate-deferred-measurements`.** The method
finding above is what closes them here: no run this change can produce
attributes either commit, so leaving the boxes open would have kept the change
hostage to work its own text proves is out of scope. The obligation is not
dropped — it now has an owner and a named home, `gate-deferred-measurements`
tasks 1.2 and 1.3, which must name both refs per that change's spec delta. That
change's task 0.1 may also decide to decline the attribution outright, which is
a legitimate outcome provided it is written down with reasoning; what it may

**Settled 2026-07-31: declined, with reasoning.** That change archived as
`archive/2026-07-31-gate-deferred-measurements`; its task 0.1 declined to build
the two-ref capability and recorded the attribution of both `527f03c` and
`631b2ee` as declined. Neither claim is confirmed or refuted, no run may be
cited as settling either, and nothing here reopens. The rule that attribution
needs a two-ref run naming both refs landed in `consumer-release-gate`
regardless, so a future claim must name the run that will settle it or decline
the same way. Read that archive's `tasks.md` sections 0 and 1 for the four
reasons; the paragraph below is the pre-decision framing, kept as written.

not do is leave it unowned again.

- [x] 2.3 Settle `engine-lean-call-boundary`'s absolute bar, which archived
      unmet. Its relative deltas were confirmed (Call rows −21..−37%), but
      the `Call ≤110ns` target — and the ≤95ns composed target — were never
      adjudicated: the dev box reads 137.0-137.4 ns median at HEAD c8645fb
      against 119.7-122.8 ns recorded the same day at the same HEAD, ~15%
      session drift wider than the margin under test, and `GOMAXPROCS` 2 vs
      24 makes no difference. Take the figure from this run's fixed-
      concurrency `bench-vm.txt`; see
      `archive/2026-07-29-engine-lean-call-boundary/tasks.md` tasks 1.1 and
      5.4 for the pinned local table. Do not edit that archive — record the
      verdict here. Only after it lands may a harness-facing doc quote a
      `Call` figure. If the hosted run also misses ≤110ns, that is a finding
      about the target, not a regression: the boundary cut itself is
      independently confirmed.

      **Scope finding: not settleable from `bench-vm.txt` on any ref.** This
      task assumes the gate's output carries a `Call` row. It does not.
      `release.yml` benches `./internal/goldset/` only, whose entire cell set
      is `Goldset/<fixture>` and `GoldsetParse/<fixture>` over the thirteen
      `.lisp` fixtures — `BenchmarkGoldset` and `BenchmarkGoldsetParse` are
      the package's only two benchmark functions. Every `Call` figure lives in
      `BenchmarkEngine_Call*` (`runtime/bench_test.go:307-366`), which the
      gate never runs. Cutting the tag from `master` fixes the other half of
      the problem (`runtime/call_boundary_*.go` is absent at `v0.10.0`) but
      not this half.

      Settling the bar therefore requires a Call-boundary cell inside the gate
      corpus, which is `gate-corpus-cl-and-recursion`'s scope, not this
      change's. Until such a cell exists and a hosted run reports it, the
      standing prohibition holds unchanged: no harness-facing doc quotes a
      `Call` figure. Do not widen `release.yml`'s bench target here — that
      enlarges the baseline's surface mid-activation, which is exactly the
      kind of silent scope drift this change exists to catch.

      **Amendment 2026-07-30: the assignment above no longer holds — the cell
      is owned by nobody.** `gate-corpus-cl-and-recursion` closed without
      adding any gate cell. Its own candidate fixtures were CL-dialect and
      recursion, never `Call`, so the assignment was mistaken even before it
      chose the narrow path; and it chose to state the corpus's scope
      boundary rather than widen it, because committing a new cell's tier
      needs a hosted baseline profile that the failed first run did not
      produce. Its `consumer-release-gate` delta now states the exclusion
      explicitly: no gate cell covers the `Call` boundary, and the
      prohibition on quoting a `Call` figure as a settled bar stands. Do not
      read that statement as ownership — settling this task still needs a
      future change that either adds the cell or restates the bar against
      something the repo measures.

      **Re-scoped 2026-07-31 to `gate-deferred-measurements`**, which is that
      future change: its tasks 2.1-2.5 add the `Call` cell to the gold set,
      license its tier against a checked-in profile, adjudicate the ≤110ns and
      composed ≤95ns targets against a hosted figure, and lift the quoting
      prohibition only at that point. **Done 2026-07-31** — see the closing
      note at the end of this file. Its `consumer-release-gate` delta moves
      the `Call` boundary out of the corpus's stated exclusions and into
      required coverage, so the ownership gap this amendment recorded is
      closed by assignment rather than by measurement — the measurement is
      that change's task, not this one's.
- [x] 2.4 Same run, cross-engine rows: GopherLua and goja are not in
      `go.mod` (testify, x/sync, x/term only), so no in-repo bench can
      produce the comparison this program's goal table is stated against.
      Either stand up an external harness and record its box and session
      alongside the numbers, or drop the cross-engine bar from the goal
      framing. Decide which; do not leave it implied.

      **Decided: drop the bar; parity stays diagnostic.** No external harness
      is stood up and none is added to `go.mod`. This is not a new position —
      `CONTEXT.md:204` already resolves it ("the **Consumer workload gate**
      governs releases; Lua/goja parity is diagnostic only") and the archived
      `2026-07-18-release-consumer-gate` proposal lists "Lua/goja parity as a
      gate" as out of scope. What was implied and is now explicit: cross-
      engine figures survive only inside archived proposals, which are
      historical records and are not edited. No harness-facing or release-
      facing doc may quote a cross-engine figure as a bar, and no gate cell
      is stated against one.

## 3. Fix documented gaps

- [x] 3.1 Add a fixture-source-edit rule to `release.yml`'s comment block
      (alongside the existing `GOMAXPROCS`/`BENCHTIME` invalidation rule):
      editing a `.lisp` fixture under `internal/goldset/testdata/`
      invalidates that fixture's stored baseline for non-regression
      comparison. (`guard-nil.lisp` was already rewritten in 3d253e2,
      `(unless true :b)` → `(when (not true) :b)`, before task 1.1's baseline
      is captured — so that baseline measures the current fixture from its
      first stored point and needs no separate correction; this rule exists
      so the *next* fixture edit doesn't repeat the gap.)

      **Delivered as a manual control, not an enforced one.** `perfgate`
      applies one global `mode` uniformly across every cell in a single loop
      (`cmd/perfgate/main.go:119`, `internal/perfgate/perfgate.go:134`);
      there is no per-cell override, and nothing in `release.yml` detects
      that a fixture's source changed since the stored baseline. So of the
      spec delta's two branches — run that cell in first-authorization mode,
      or explicitly note the comparison as measuring changed source — only
      the second is available today, and it depends on a human noticing.

      Moot until 1.1/1.2 land, because no baseline exists to compare
      against. Real the moment they do. Enforcing it in code means a
      per-cell mode override in `perfgate` plus a fixture-hash record
      alongside the stored `bench-vm.txt`; that is a follow-up change, not
      this one — widening the gate's own machinery mid-activation is the
      scope drift 2.3 already declined.
- [x] 3.2 Amend ADR 0013's account: replace the "the perfgate tiers decide the
      performance cells. Passing it was the condition; it passed" framing
      with what actually happened — the local goldset correctness gate plus
      the ad hoc benchstat evidence recorded in
      `archive/2026-07-20-engine-bytecode-default/tasks.md` task 4.3. Do not
      reopen whether the default flip was justified; only correct which
      artifact produced the evidence.
- [x] 3.3 CHANGELOG `[Unreleased]`: note the gate's first real activation and
      the ADR 0013 correction.

      Split, because half of it was not true yet when the first pass ran.
      **Landed then:** the trigger change (the gate now runs on published
      releases), the fixture-source invalidation rule, and the ADR 0013
      correction — written under `[Unreleased]` and since cut into
      `[0.11.0]`. **Withheld until 1.1 completed:** any line claiming the gate
      had actually activated. Announcing an activation that has not happened
      is the same class of error as 3.2's framing — a claim of a verdict no
      run produced.

      **Second half landed 2026-07-30, once 1.1 ran.** The new `[Unreleased]`
      entry records the first activation *with its outcome*: correctness legs
      green, performance verdict failed on tier misclassification in the
      gate's own corpus rather than on engine behavior, and no `bench-vm.txt`
      asset stored — so the next release still runs as a first authorization.
      An activation line without the outcome would repeat 3.2's error in the
      opposite direction.

## 4. Verify

- [x] 4.1 `openspec validate --strict` on this change.
- [x] 4.2 Confirm the stored `bench-vm.txt` asset is present on the release
      and downloadable by tag, matching `release.yml`'s own "Determine gate
      mode" lookup logic. Follows 1.2.

      **Verified 2026-07-31 with the lookup step's own command**, not a
      paraphrase of it: `gh release download v0.12.0 --pattern bench-vm.txt
      --output baseline-vm.txt` succeeds from a repository checkout and returns
      29830 bytes, byte-identical (`cmp`) to the `bench-vm.txt` in run
      `30646304157`'s `consumer-gate-evidence` artifact. The file is a
      well-formed benchstat input: 270 sample rows over 27 distinct cells
      (13 `Goldset/*`, 13 `GoldsetParse/*`, 1 `GoldsetCall/*`) at
      `BENCH_COUNT: 10`, carrying its `goos`/`goarch`/`cpu` header.

      This also discharges the three paths `release.yml:37-41` says a dispatch
      run "cannot rehearse at all" and that "run for real the first time or not
      at all": `gh` auth under `contents: write`, `tag_name` interpolation, and
      the asset round-trip. All three executed. "Determine gate mode" resolved
      `first-authorization` correctly, no release having carried the asset
      before this one.

## 5. Re-baseline after the reader axis (deferred until D and E land)

- [x] 5.1 Once changes `reader-allocation-floor` and `reader-state-reuse`
      land, re-trigger the workflow and re-store `bench-vm.txt`. Every
      `Goldset/*` `B/op` figure will drop for reader-only reasons; the
      baseline from task 1.2 stays valid for regression-bounding (one-sided)
      but would understate any real evaluator regression measured against it
      until this re-run replaces it.

      **Its stated blocker is cleared; a different one replaces it.** Both
      changes archived 2026-07-30 at 12/12 (`archive/2026-07-30-reader-
      allocation-floor`, `archive/2026-07-30-reader-state-reuse`), so the
      reader axis has landed and every `Goldset/*` `B/op` figure has already
      moved. What blocks 5.1 now is 1.2: there is no stored baseline to
      re-store *over*, because the first armed run's verdict failed. This
      task therefore collapses into 1.2 until the tiers are fixed — the same
      release cut settles both, and the re-baseline is not a second run.

      **Closed 2026-07-31 by that same first store, as this amendment
      predicted.** `v0.12.0`'s stored `bench-vm.txt` was measured at
      `e300ae4`, a tree that already contains both reader changes
      (`archive/2026-07-30-reader-allocation-floor`,
      `archive/2026-07-30-reader-state-reuse`), so every `Goldset/*` `B/op`
      figure in it is post-reader-axis. There is no pre-axis baseline for it to
      understate against and no second run to schedule. The one-sided
      regression-bounding caveat in this task's original text never became
      live.

---

## Status at the end of the first apply pass

Complete: 0.1, 2.4, 3.1, 3.2, 4.1. Those are the decisions and the in-repo
documentation edits — everything settleable without a hosted run.

Open, and why:

| Task | Blocker |
| --- | --- |
| 1.1, 1.2, 4.2 | Need `v0.11.0` cut from `master` and published; the cut is not this change's to make. |
| 2.1, 2.2 | Need a two-ref paired bench (see the method finding above); one first-authorization run cannot attribute either commit. |
| 2.3 | Needs a Call-boundary cell in the gate corpus — `gate-corpus-cl-and-recursion`'s scope. |
| 3.3 | Half landed; the activation line waits on 1.1. |
| 5.1 | `reader-allocation-floor` and `reader-state-reuse` both at 0/12. |

Follow-ups surfaced during the pass, deliberately left out of scope:

- **ADR 0006's Amendment now overclaims.** It lists "the gold-set
  parity/performance gate" as one undifferentiated item of completed
  promotion evidence. ADR 0013's corrected text splits that into a real
  correctness leg and an ad hoc, externally-gathered performance leg, so
  reading the two records together is now *more* inconsistent than before,
  not less. This change's own Impact section scopes ADR edits to 0013 only,
  so 0006 is untouched; a cross-reference or a matching correction there is
  a separate change.
- **Per-cell baseline invalidation is unenforced in code** — see 3.1.
- **`Determine gate mode` cannot distinguish "no baseline" from "`gh` failed".**
  Its loop (`release.yml`, "Determine gate mode") treats any non-zero
  `gh release download` as absence, and the surrounding `gh release list`
  has no failure check at all. A transient API error therefore reads as
  first-authorization, which compares the candidate VM against the Evaluator
  under improvement thresholds instead of running non-regression — failing a
  healthy release for the wrong reason. Harmless until a baseline exists, so
  it cannot affect the first armed run (1.1); it becomes live on the second.
  Fix before the release after 1.2, not in this change.

**This change is not archived while any of those stand open.** Archiving a
change whose measurement tasks never ran is the precise defect it was written
to indict — task 2.2 exists only because
`archive/2026-07-27-vm-batched-ledger-charging` was archived with its own 1.1,
3.3, and 3.4 unchecked.

---

## Status at the end of the second apply pass

The hosted gate ran between the two passes (run `30561584997`, event
`release`, tag `v0.11.0`, conclusion `failure`), which settled two tasks the
first pass could not reach and invalidated one of its stated blockers.

Complete: 0.1, **1.1**, 2.4, 3.1, 3.2, **3.3**, 4.1. 1.1's four confirmations
all hold and are recorded above; the task asked for a verdict to be produced,
not for it to pass. 3.3's withheld half is written now that there is an
activation to report, and it carries the outcome rather than the activation
alone.

Open, and why:

| Task | Blocker |
| --- | --- |
| 1.2, 4.2, 5.1 | The verdict failed, so `release.yml`'s store step was skipped by its implicit `success()` guard and `v0.11.0` carries no assets. Needs `tiers.json` fixed (`gate-corpus-cl-and-recursion`), then a release cut whose gate passes; the workflow stores the baseline itself. Do not hand-upload. 5.1's re-baseline is that same first store, not a second one — the reader axis landed before any baseline existed. |
| 2.1, 2.2 | Unchanged: need a two-ref paired bench on the hosted runner (`527f03c^` vs `527f03c`; `631b2ee^` vs `631b2ee`). No such run is scoped anywhere, in this change or another. |
| 2.3 | Needs a Call-boundary cell in the gate corpus. **Reassigned to nobody** — `gate-corpus-cl-and-recursion` closed without adding one; see the amendment under 2.3. |

A correction to this change's own Impact, which reads "`internal/perfgate/
tiers.json` (no change expected — corpus reclassification is
`gate-corpus-cl-and-recursion`'s scope)". That scoping is void. Neither change
can reclassify: `tiers.json`'s own rule and ADR 0008 require a committed
baseline profile, the only profile that exists is an expiring workflow
artifact from a run whose verdict failed, and 1.2 forbids hand-promoting it.
Fixing the eight misclassified cells needs a decision about what counts as a
profile of record — a `workflow_dispatch` artifact, or an ADR 0008 amendment
— before any change can own the edit.

One follow-up moves from future to present tense:

- **The inconclusive rerun has already misfired once.** Defect 1 under task
  1.1 — the rerun gated on `exit_code == '2'`, which exit 1 masks — is not a
  latent hazard. It fired on run `30561584997`, skipping the rerun despite 12
  inconclusive cells, so the gate's burden-of-proof rule went unapplied on
  its own first execution. Not fixed here: 2.3 already declined widening
  `release.yml`'s machinery mid-activation, and repairing the pass/fail
  mechanism while documenting that mechanism's first run is the same drift.

**Still not archived**, and the checkboxes are the weaker reason. The spec
delta's added requirement reads "SHALL execute against at least one real
release **and publish a stored non-regression baseline**" — the run happened,
the publish did not, so the requirement this change adds is half-unmet by its
own text. 2.1, 2.2, and 2.3 remain unrun on top of that, and none of them is
settleable inside this change's scope — 2.3 belongs to
`gate-corpus-cl-and-recursion`, and 2.1/2.2 belong to a hosted two-ref run
nothing currently schedules. Closing this change honestly requires either
running them or deliberately re-scoping them elsewhere; it does not require
another apply pass.

---

## Status at the end of the third apply pass

Nothing external moved between the second pass and this one. Every open task's
blocker was re-checked rather than carried forward:

| Fact | Command | Value |
| --- | --- | --- |
| `v0.11.0` assets | `gh release view v0.11.0 --json assets` | `[]` — still no `bench-vm.txt` |
| Gate runs | `gh run list --workflow=release.yml` | one, `30561584997`, conclusion `failure` |
| Evidence artifact | `gh api repos/:owner/:repo/actions/runs/30561584997/artifacts` | `consumer-gate-evidence`, 6615 B, `expired: false`, expires **2026-10-28** |
| `tiers.json`, `release.yml` | `git log -- internal/perfgate/tiers.json .github/workflows/release.yml` | untouched since `7747113` |
| An owner for the tiers fix | `openspec list --json` | none — the only other pending change is `value-layout-locality` (0/12) |

The second pass's blocker table therefore stands. What this pass adds is a
correction to how that table states the loop, and a deadline the table did not
carry.

**The loop was stated wrong: a classification profile and a stored baseline
asset are two different artifacts.** The second pass concluded that fixing the
tiers "needs a decision about what counts as a profile of record — a
`workflow_dispatch` artifact, or an ADR 0008 amendment", which treats the
expiring workflow artifact and the release asset as rival candidates for one
slot. They are not the same slot. ADR 0008's Thresholds section requires "a
checked-in baseline profile" — a file committed to this repository — to
classify each cell, while the stored `bench-vm.txt` (`release.yml`'s "Store VM
baseline on the authorized release") is the per-release non-regression
comparator and is by construction *not* checked in. Task 1.2 forbids
hand-placing the asset; it says nothing about committing hosted bench output as
classification evidence, which is a different artifact with a different
consumer.

**A `workflow_dispatch` run produces that evidence today, with no release
identity to abuse.** Only two steps in `release.yml` are release-gated:
"Determine gate mode" and "Store VM baseline on the authorized release". The
gold set, the race suite, the paired Evaluator/VM bench, and `perfgate` in
first-authorization mode all run under dispatch, and "Upload release evidence"
is `if: always()`, so `bench-evaluator.txt` and `bench-vm.txt` come back as an
artifact regardless of verdict. A dispatch run at `master` thus yields a hosted
paired profile that can be committed and cited without touching any release's
assets and without consuming the one-shot `released` slot that 0.1 reserved.

**What remains a judgment call is the ordering rule, not the artifact.** ADR
0008 places the checked-in profile "before candidate results are produced". A
profile taken from `master` and committed before the next tag is cut satisfies
that literally, but the tree it profiles is the tree the next release measures,
so the tiers would be fit to numbers already observed. That risk is inherent to
any first-authorization profile and is bounded by what a tier asserts — a
qualitative shape (engine-sensitive / data-dominated / startup), not a
per-cell threshold. Whether that bound suffices is ADR 0008's owner's call, and
it is the last question standing between here and 1.2.

**Deadline: the only hosted profile this repository has ever produced expires
2026-10-28.** After that, run `30561584997`'s data is gone and reopening 1.2,
2.1, 2.2, 4.2, or 5.1 costs a fresh hosted run. A copy was pulled during this
pass for inspection only; it lives outside the repository and is not repo
state.

**Decisions taken this pass, and two changes authored to carry them.**

- The dispatch route is adopted. `gate-tier-reclassification` owns committing a
  hosted `workflow_dispatch` profile as the classification profile of record
  and correcting the eight misclassified cells against it. Its
  `consumer-release-gate` delta writes down the distinction this pass found —
  the checked-in classification profile and the stored per-release baseline
  asset are separate artifacts with separate rules — so the loop cannot be
  restated the old way again.
- `gate-deferred-measurements` owns the three orphans. 2.1 and 2.2 become its
  tasks 1.2 and 1.3 (a two-ref hosted bench naming both refs, with declining
  the attribution an allowed outcome provided it is reasoned and written down);
  2.3 becomes its tasks 2.1-2.5 (a `Call` cell in the gold set, its tier
  licensed by a profile, the absolute bar adjudicated against a hosted figure).
  Its spec delta moves the `Call` boundary from stated exclusion to required
  coverage. All three tasks are checked here as re-scoped, not as measured.

  **Discharged 2026-07-31, at `archive/2026-07-31-gate-deferred-measurements`.**
  2.1 and 2.2: declined with reasoning, no two-ref capability built. 2.3: the
  cell `GoldsetCall/call-boundary` is committed, tiered engine-sensitive
  against hosted profile `30637802780`, and reported PASS in hosted run
  `30639778105`; the `≤110ns` and composed `≤95ns` targets were retired as not
  adjudicable — they name no machine, engine configuration, or timed region,
  and the cell reads 188.50 ns hosted under conditions none of them specify.
  The quoting prohibition is lifted against hosted figures carrying their
  qualifiers. That same run also failed on `guard-nil`, which is what now
  blocks 1.2, 4.2, and 5.1 — see the note on 1.2 above.

**A dispatch run was fired to prove the mechanism, and it did — while
disqualifying itself as the profile.** Run `30610843591` (2026-07-31, `--ref
master`) behaved exactly as `release.yml` predicts under dispatch: "Determine
gate mode" and "Store VM baseline" skipped, gold set green, race suite green,
paired bench complete, a first-authorization verdict produced, evidence
uploaded, and only "Enforce performance gate verdict" failing on the same eight
cells. Its head sha, however, is `fee885a`: GitHub resolves `--ref master`
against the remote branch, and `origin/master` sits 20 commits behind the local
one. That is `v0.11.0`'s tree, so the run re-measured the tree whose verdict is
already known — figures reproduce run `30561584997`'s to within a few tenths of
a percent — and using it would fit tiers to a judged release's candidate
results. A usable profile needs a dispatch against the tree the next release is
cut from, which needs those 20 commits pushed first. Recorded in
`gate-tier-reclassification` task 1.1; the push is nobody's task in either
change.

What stays open here: 1.2, 4.2, and 5.1, all waiting on the same event — a
release cut whose gate passes against corrected tiers, which stores the
baseline itself. The cut is not this change's to make. This change is at 10/13
and archives when that release lands.

---

## Status at the end of the fourth apply pass

That release landed. **13/13; this change archives.**

The event all three remaining tasks waited on was one release cut whose gate
passes, and it produced one artifact that closes all three — 5.1's own
amendment had already established that the re-baseline is that same first
store, not a second run.

| Step | Evidence |
| --- | --- |
| Push `master` | `2fe50d2..59b8483`, ff, 8 commits. Remote head re-read as `59b8483` before dispatching |
| Pre-flight | dispatch run `30645498605` at `59b8483` — **PASS** |
| Cut | `chore(release): cut v0.12.0` (`e300ae4`), tag `v0.12.0`, published non-draft non-prerelease |
| Release run | `30646304157`, event `release`, head `e300ae4` — **success**, verdict 27/27 PASS |
| Store | `bench-vm.txt`, 29830 B, on `v0.12.0` |
| Lookup | `gh release download v0.12.0 --pattern bench-vm.txt` → byte-identical to the run artifact |

**The pre-flight was the whole method, and it is worth naming as such.** The
fix that closed the `guard-nil` coin flip (`fb3ee4f`) was committed locally,
unpushed, and had never run on a hosted runner; the two most recent hosted runs
had both failed. Cutting a tag straight into that state would have been the
same bet this change spent three passes refusing to make. Pushing first, then
dispatching at the exact tree, converted the cut from a coin flip into a
checked step — and `gh workflow run --ref master` resolves against
`origin/master`, so the push was not optional but a precondition for the
pre-flight to measure anything. That trap is what disqualified run
`30610843591` (see the third pass), and re-reading `git ls-remote` before
dispatching is what proves it was avoided.

**The tag is not the pre-flight's sha, and that is accounted for, not
overlooked.** `e300ae4` is `59b8483` plus one commit that edits `CHANGELOG.md`
only — no Go source, no `internal/goldset/testdata/` fixture, no `tiers.json`.
The measured tree is behaviorally identical to the one the pre-flight passed
on, which is why the pre-flight's verdict transfers. A cut that had carried
code between pre-flight and tag would not have this property, and the check
would need re-running.

**Two mechanism defects this change recorded are now discharged by
observation, not by argument.**

1. **The exit-2 rerun fired correctly.** Defect 1 under task 1.1 — the rerun
   gated on `exit_code == '2'`, which a real failure's exit 1 masks — misfired
   on run `30561584997`, skipping the rerun despite 12 inconclusive cells. Both
   runs today took the exit-2 path: "Rerun paired benchmark at doubled
   benchtime" and "Resolve inconclusive cells after rerun" executed and passed.
   The burden-of-proof rule ADR 0008 specifies has now been applied on a real
   verdict for the first time. The defect was never fixed — it simply requires
   a run with inconclusive cells and no failing cell to express correctly, and
   that is what these were.
2. **The three unrehearsable paths ran.** `release.yml:37-41` states that `gh`
   auth, `tag_name` interpolation, and the asset round-trip "run for real the
   first time or not at all" because dispatch carries no release identity. All
   three ran, in "Determine gate mode" and "Store VM baseline". "Determine gate
   mode" had executed to success exactly once before, on the failed
   `30561584997`; this is its first execution on a run that reached the store.

**One consequence to hand forward: the next release is the first
non-regression run, which arms a known-unfixed defect.** Until now "Determine
gate mode" found no baseline and fell through to first-authorization, which
made its error handling harmless. From the next release it will find
`v0.12.0`'s asset and run non-regression. Its loop treats any non-zero
`gh release download` as absence and the surrounding `gh release list` has no
failure check at all, so a transient API error will silently read as
first-authorization — comparing the candidate VM against the Evaluator under
improvement thresholds instead of running non-regression, and failing a healthy
release for the wrong reason. The first pass filed this as "harmless until a
baseline exists ... becomes live on the second". It is live now. It is not
fixed here for the reason 2.3 established and this change held to throughout:
repairing the gate's machinery inside the pass that documents that machinery's
first execution is the drift this change exists to catch. It needs an owner
before the next cut.

**What this change set out to prove is now true by demonstration.** Its spec
delta reads "SHALL execute against at least one real release **and publish a
stored non-regression baseline**". The second pass recorded that half-unmet —
the run happened, the publish did not. Both halves hold: the gate ran on a real
release, and a real release carries the baseline every later release will be
measured against.
