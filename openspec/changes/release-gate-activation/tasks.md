# Tasks — release-gate-activation

## 0. Decide the standing trigger

- [ ] 0.1 Choose: keep `workflow_dispatch`-only (document a manual per-release
      cadence in `release.yml`'s header comment), or re-enable
      `release: published` with the post-hoc semantics already described
      there. Either choice, record the reasoning here.

## 1. First real run

- [ ] 1.1 Trigger `.github/workflows/release.yml` against `v0.10.0` (or the
      next cut). Confirm: gold set green both modes, race suite green,
      paired benchmark run completes, `cmd/perfgate` produces a verdict
      (first-authorization mode, since no prior asset exists).
- [ ] 1.2 Store the run's `bench-vm.txt` as a release asset. Confirm it is
      downloadable via `gh release download` the way `release.yml`'s
      "Determine gate mode" step expects for the *next* release.

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
- [ ] 2.4 Same run, cross-engine rows: GopherLua and goja are not in
      `go.mod` (testify, x/sync, x/term only), so no in-repo bench can
      produce the comparison this program's goal table is stated against.
      Either stand up an external harness and record its box and session
      alongside the numbers, or drop the cross-engine bar from the goal
      framing. Decide which; do not leave it implied.

## 3. Fix documented gaps

- [ ] 3.1 Add a fixture-source-edit rule to `release.yml`'s comment block
      (alongside the existing `GOMAXPROCS`/`BENCHTIME` invalidation rule):
      editing a `.lisp` fixture under `internal/goldset/testdata/`
      invalidates that fixture's stored baseline for non-regression
      comparison. (`guard-nil.lisp` was already rewritten in 3d253e2,
      `(unless true :b)` → `(when (not true) :b)`, before task 1.1's baseline
      is captured — so that baseline measures the current fixture from its
      first stored point and needs no separate correction; this rule exists
      so the *next* fixture edit doesn't repeat the gap.)
- [ ] 3.2 Amend ADR 0013's account: replace the "the perfgate tiers decide the
      performance cells. Passing it was the condition; it passed" framing
      with what actually happened — the local goldset correctness gate plus
      the ad hoc benchstat evidence recorded in
      `archive/2026-07-20-engine-bytecode-default/tasks.md` task 4.3. Do not
      reopen whether the default flip was justified; only correct which
      artifact produced the evidence.
- [ ] 3.3 CHANGELOG `[Unreleased]`: note the gate's first real activation and
      the ADR 0013 correction.

## 4. Verify

- [ ] 4.1 `openspec validate --strict` on this change.
- [ ] 4.2 Confirm the stored `bench-vm.txt` asset is present on the release
      and downloadable by tag, matching `release.yml`'s own "Determine gate
      mode" lookup logic.

## 5. Re-baseline after the reader axis (deferred until D and E land)

- [ ] 5.1 Once changes `reader-allocation-floor` and `reader-state-reuse`
      land, re-trigger the workflow and re-store `bench-vm.txt`. Every
      `Goldset/*` `B/op` figure will drop for reader-only reasons; the
      baseline from task 1.2 stays valid for regression-bounding (one-sided)
      but would understate any real evaluator regression measured against it
      until this re-run replaces it.
