## Why

The release consumer gate stored its first VM baseline on `v0.12.0`, so `v0.13.0`
is the first release whose cells are judged as non-regression against a previous
release. Cutting it surfaced four defects, none of which the gate could show
while it had never compared two separate runs.

**The baseline and the candidate are not measured on the same machine.**
`v0.12.0`'s stored `bench-vm.txt` carries `cpu: Intel(R) Xeon(R) Platinum 8370C`;
the `v0.13.0` candidate ran on `cpu: AMD EPYC 9V74`. `benchstat` treats `cpu:` as
a configuration key and refuses to pair samples across different values, so it
emits a single-group table and `perfgate` fails to parse it at all
(`benchstat csv data row too short`). Deleting the `cpu:` line makes it parse and
produces latency verdicts that are microarchitecture, not code: on measured data
`queue-promote` reads +19.93%, `loop-sum` +8.81%, `text-render` +6.54`%` and
`merge-config` +5.87%, none of which corresponds to any change in the range.
GitHub's hosted runners mix vendors, so which of the two failures a release gets
is a coin flip. `benchstat`'s refusal is correct behaviour; the gate is what
assumes a comparison it never established.

**A per-cell bytes bound of zero is applied to a sampled average.** `B/op` is
total bytes divided by iterations, so it drifts by a few bytes between runs of
identical code — measured on the stored baseline, `registry-fold` reports
2939-2940 across ten samples and `rule-load` 5840-5842. Only
`Goldset/guard-nil` carries a `bytesAllowanceBOp`; every other cell resolves to an
implicit zero, so ordinary sampling wobble fails a release. Allocation *counts*
carry no such problem: all ten samples agree exactly, on every cell.

**A pre-flight cannot predict the release it precedes.** `Determine gate mode`
runs only `if: github.event_name == 'release'`, so a `workflow_dispatch` falls
through to `first-authorization` and compares the Evaluator arm against the VM
arm instead of the candidate against the baseline. The method recorded for
`v0.12.0` — dispatch pre-flight, then tag — was sound while no baseline existed
and became misleading the moment one did. The two `v0.13.0` pre-flights failed on
`guard-nil` under a rule no release would apply, while the release's own rule
would have passed that cell.

**Baseline resolution cannot distinguish absence from failure.** The lookup loop
treats any non-zero `gh release download` as "no baseline", and the surrounding
`gh release list` is unchecked, so a transient API error silently downgrades a
post-authorization release to first-authorization and judges it against
improvement thresholds. This was recorded as harmless while no baseline existed;
that condition expired with `v0.12.0`.

## What Changes

- Record the runner identity with the stored baseline, and make a latency
  comparison against a baseline measured on different hardware inconclusive by
  construction rather than passed or failed. Allocation counts and bytes stay
  enforced across runners; they do not depend on the CPU.
- Require every gate cell to state a bytes allowance explicitly. An absent
  allowance becomes an error rather than an implicit zero.
- Resolve the gate mode the same way for a dispatch run and a release run, so a
  pre-flight verdict predicts the release verdict.
- Fail the gate when baseline resolution errors, instead of reading an error as
  absence.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `consumer-release-gate`: cross-runner comparability, explicit bytes allowances,
  and trustworthy baseline resolution.

## Impact

- `.github/workflows/release.yml`, `internal/perfgate/` and
  `internal/perfgate/tiers.json`.
- A release whose runner does not match the baseline's still gates on correctness,
  the race suite, allocation counts and bytes; it reports latency as inconclusive
  and says why, instead of failing on hardware or passing on a parse it never made.
- ADR 0008 gains the runner-comparability rule its non-regression check assumed
  but never stated.
- No change to the language, the engines, or any embedder-visible API.
