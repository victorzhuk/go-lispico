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

- [ ] 1.1 Trigger `.github/workflows/release.yml` against **`v0.11.0`, the
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
- [ ] 1.2 Store the run's `bench-vm.txt` as a release asset. Confirm it is
      downloadable via `gh release download` the way `release.yml`'s
      "Determine gate mode" step expects for the *next* release. With 0.1
      decided, the workflow's own "Store VM baseline on the authorized
      release" step does this; no manual upload is required.

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

- [ ] 2.1 Against the run from 1.1, record 527f03c's fusion result: does the
      hosted runner's fib-adjacent cells (`loop-sum`, `queue-promote` — both
      contain `(= i N)`/`(+ i 1)`-shaped local/const native-op calls in a
      loop body per their own fixture source, `internal/goldset/testdata/
      loop-sum.lisp` and `queue-promote.lisp`; `docs/profiling-baseline.md`
      separately notes them as the two costliest cells by raw eval time, not
      as fusable-shape evidence) show any movement attributable to
      `OpFusedNativeOp`? Record the number even if it confirms the local
      near-miss.
- [ ] 2.2 Run the interleaved benchstat that `vm-batched-ledger-charging`
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

- [ ] 2.3 Settle `engine-lean-call-boundary`'s absolute bar, which archived
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
- [ ] 3.3 CHANGELOG `[Unreleased]`: note the gate's first real activation and
      the ADR 0013 correction.

      Split, because half of it is not true yet. **Landed:** the trigger
      change (the gate now runs on published releases), the fixture-source
      invalidation rule, and the ADR 0013 correction. **Withheld until 1.1
      completes:** any line claiming the gate has actually activated.
      Announcing an activation that has not happened is the same class of
      error as 3.2's framing — a claim of a verdict no run produced.

## 4. Verify

- [x] 4.1 `openspec validate --strict` on this change.
- [ ] 4.2 Confirm the stored `bench-vm.txt` asset is present on the release
      and downloadable by tag, matching `release.yml`'s own "Determine gate
      mode" lookup logic. Follows 1.2.

## 5. Re-baseline after the reader axis (deferred until D and E land)

- [ ] 5.1 Once changes `reader-allocation-floor` and `reader-state-reuse`
      land, re-trigger the workflow and re-store `bench-vm.txt`. Every
      `Goldset/*` `B/op` figure will drop for reader-only reasons; the
      baseline from task 1.2 stays valid for regression-bounding (one-sided)
      but would understate any real evaluator regression measured against it
      until this re-run replaces it.

      Blocked by construction: both changes stand at 0/12 tasks.

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
