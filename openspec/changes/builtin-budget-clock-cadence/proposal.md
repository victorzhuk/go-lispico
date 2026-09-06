## Why

The first non-regression run of the release gate failed on latency, and the cause
is a clock read the builtin work budget performs once per Builtin call.

**Run 34023365326**, `workflow_dispatch` on `ead5b38`, resolved non-regression
against `v0.12.0` and reported:

```
Goldset/queue-promote-2: FAIL (latency regressed 19.75%, exceeding the 5% tolerance)
```

Both arms recorded `Intel(R) Xeon(R) Platinum 8370C`, so this was a same-runner
comparison with latency enforced as designed. `queue-promote` is the only cell
that regressed; most others improved since `v0.12.0`
(`GoldsetParse/rule-load` −12.8%). Its own parse cell is −2.4%, so the cost is in
evaluation, not reading.

Profile attribution against a `v0.12.0` build isolates it:

- `time.runtimeNow` is **11.05%** of the candidate profile against **0.45%** at
  `v0.12.0`, about +2.8 µs of the +5.6 µs per-operation delta.
- 93% of that sits under `core.(*BuiltinWorkBudget).flushPending`
  (`core/builtin_budget.go:56`), which reads `time.Now()` on every flush.
- `stdlib.finishBuiltin` (`plugins/stdlib/charges.go:13`) flushes before every
  return, and each Builtin constructs its own budget
  (`plugins/stdlib/arithmetic.go:233` and about ten sibling sites), so the
  cadence never spans calls.

The batching works as specified — no clock read occurs per local step — but the
spec also requires a flush before every return, and every flush observes the
deadline. For a Builtin that performs a handful of units, that is one wall-clock
read per call. `queue-promote` builds a 40-element vector with `conj` across
`vectorFlatThreshold` and then folds it, which is by far the most Builtin calls
in the gold set, so it is where the cost became visible first.

The VM had the same defect and it is already settled there.
`vm-deadline-clock-cadence` gated `pollCancel`'s clock read behind a countdown
and amended `bytecode-vm` to permit it
(`openspec/specs/bytecode-vm/spec.md:516-525`). The rule was written for one
package and the core builtin path never received it.

**Consequence while this stands.** `Store VM baseline on the authorized release`
is guarded by the implicit `success()` on its `if:`, so a failing gate stores no
baseline. `v0.13.0` stays untagged, and the release it would authorize would
publish nothing for the next release to gate against.

## What Changes

- Permit the builtin work budget to observe the Evaluation deadline through clock
  reads at a reduced fixed multiple of the synchronization interval, with the
  staleness bound documented, mirroring the rule `bytecode-vm` already carries.
- Carry that cadence on the evaluation rather than on the budget. A budget is
  confined to one GoFunc call, so a per-budget countdown would still read the
  clock once per call and change nothing.
- Keep the Reduction charge and the caller-cancellation check unconditional at
  every synchronization.
- Make installing a deadline reset the cadence, so the first synchronization
  after arming reads the clock rather than inheriting a position from an earlier
  evaluation.

## Impact

- Affected specs: `core-engine`
- Affected code: `core/builtin_budget.go` (the deadline branch of `flushPending`),
  `core/eval.go` (`evalState` cadence field, and the two sites that install a
  deadline), plus a clock seam in package `core` for the tests — `nowFunc` exists
  only in `core/vm` today.
- Not in scope: `core/eval.go:316` already sits behind a 128-node budget, and
  `core/value_walk_context.go:39` carries no measured cost. Neither is touched.
- Release: `v0.13.0` is held untagged until the gate passes, so that the release
  it authorizes stores a baseline.
