# gate-deferred-measurements

## Why

Three measurement obligations are live, unmet, and owned by nobody. All three
were raised by `release-gate-activation`, none is settleable inside it, and
each was explicitly declined by the change it was first pointed at.

**Two commits shipped with their performance claims unmeasured.**
`compiler-branch-arith-fusion` (527f03c, native-op fusion) recorded a local
near-miss and deferred its verdict "to the release runner".
`vm-batched-ledger-charging` (631b2ee, ledger batching) was archived with its
own tasks 1.1, 3.3, and 3.4 unchecked — no baseline, no verification floor, no
benchstat. `release-gate-activation` carried both forward as tasks 2.1 and 2.2
and then found they cannot be settled by a first-authorization gate run at all:
both commits are ancestors of every tag cut from `master`, neither has a build
tag or env knob that disables it (`core/vm` and `core/compiler` carry no
`go:build` constraint and no `os.Getenv` call), so any single run measures
*with* both and yields an Evaluator-vs-VM ratio, which is not an attribution to
either commit. Attribution needs a deliberate two-ref paired run at the gate's
fixed parameters — `527f03c^` vs `527f03c`, `631b2ee^` vs `631b2ee` — and
nothing in this repository can currently produce one.

**The `Engine.Call` boundary has no gate cell and no owner.**
`engine-lean-call-boundary` archived with its relative deltas confirmed (Call
rows −21..−37%) but its absolute bar unadjudicated: `Call ≤110ns` and the
composed ≤95ns target were never settled, because the dev box read 137.0-137.4
ns against 119.7-122.8 ns recorded the same day at the same HEAD — session
drift wider than the margin under test. `release-gate-activation` task 2.3
assigned the settlement to the hosted gate and then found the gate cannot see
it: `release.yml` benches `./internal/goldset/` only, whose entire cell set is
`Goldset/*` and `GoldsetParse/*`, while every `Call` figure lives in
`BenchmarkEngine_Call*` (`runtime/bench_test.go:307-366`). It reassigned the
work to `gate-corpus-cl-and-recursion`, which closed without adding any cell —
correctly, since its own candidate fixtures were CL-dialect and recursion,
never `Call`. The obligation has been unowned since.

The standing prohibition that follows is real and currently permanent: no
harness-facing document quotes a `Call` figure as a settled bar, and nothing
scheduled would ever lift it.

## What Changes

- Decide the attribution mechanism before building it: either add a two-ref
  paired benchmark capability that runs the gate's fixed parameters against two
  refs and emits a benchstat comparison, or record the attribution of 527f03c
  and 631b2ee as deliberately declined with the reasoning. Both are honest
  outcomes; leaving the obligation unowned is not. If built, the natural shape
  is `workflow_dispatch` inputs on a separate workflow rather than widening
  `release.yml`, whose run is the release verdict and should not grow modes.
- If built: run it for `527f03c^` vs `527f03c` and `631b2ee^` vs `631b2ee`, and
  record both verdicts — including a null result, which is what the local
  near-miss predicts for fusion.
- Add a `Call`-boundary cell to the gold-set corpus so the boundary is measured
  where the gate actually runs, with its tier justified by a checked-in
  classification profile per `gate-tier-reclassification`. Then adjudicate
  `engine-lean-call-boundary`'s absolute bar against a hosted figure, or restate
  the bar against something this repository measures. A hosted miss is a
  finding about the target, not a regression — the boundary cut itself is
  independently confirmed.
- Update `consumer-release-gate`'s stated exclusion for the `Call` boundary
  once a cell exists, and lift the prohibition on quoting a `Call` figure only
  at that point.

## Impact

- Affected specs: `consumer-release-gate` (the `Call` exclusion becomes
  coverage; a new requirement for attribution runs if the mechanism is built).
- Affected code: `internal/goldset/` (a `Call`-shaped fixture, its golden, its
  benchmark cell), `internal/perfgate/tiers.json` (the new cell's tier), a new
  workflow if the attribution mechanism is built. `release.yml` is not widened.
- Closes, if completed: `release-gate-activation` tasks 2.1, 2.2, and 2.3,
  which are re-scoped to this change.
- Risk: a new gate cell changes the pass/fail surface for every future release
  and SHALL NOT land without its own hosted profile. The attribution mechanism
  is new CI machinery whose only consumer is two historical commits — the
  first task exists so that cost is weighed rather than assumed.
- Sequencing: after `gate-tier-reclassification`, which establishes how a
  profile licenses a tier. The `Call` cell cannot be classified before that
  mechanism exists.
