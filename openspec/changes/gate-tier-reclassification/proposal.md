# gate-tier-reclassification

## Why

The consumer gate's first hosted run (`30561584997`, event `release`, tag
`v0.11.0`, head `fee885a`) produced a FAIL verdict that is a finding about
`internal/perfgate/tiers.json`, not about the engine. Eight of twenty-six
committed cells are misclassified, and five are close to inverted — cells
assumed mode-insensitive are the most engine-sensitive in the corpus, failing
because they improve too much against a two-sided tolerance:

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

Because the verdict failed, `release.yml`'s "Store VM baseline on the
authorized release" step was skipped by the implicit `success()` guard on its
`if:`, so `v0.11.0` carries no `bench-vm.txt`. That blocks
`release-gate-activation` tasks 1.2, 4.2, and 5.1, and both that change and
`gate-corpus-cl-and-recursion` concluded the loop was unbreakable from inside
their scope: the gate needs correct tiers to pass, passing is what stores the
baseline asset, and a stored baseline is what licenses a tier.

That loop rests on a conflation of two different artifacts. ADR 0008's
Thresholds section requires "a checked-in baseline profile" — a file committed
to this repository — to classify each cell before candidate results are
produced. The stored `bench-vm.txt` is something else: the per-release
non-regression comparator, held as a release asset and by construction not
checked in. `consumer-release-gate`'s own scenario "A new gate cell requires a
hosted baseline profile" asks for "a hosted run at the gate's fixed
parameters", never for a release asset.

A `workflow_dispatch` run satisfies that requirement today. Only two steps in
`release.yml` are release-gated — "Determine gate mode" and "Store VM baseline
on the authorized release" — while the gold set, the race suite, the paired
Evaluator/VM bench, and `perfgate` in first-authorization mode all run under
dispatch, and "Upload release evidence" is `if: always()`. A dispatch run
therefore produces the paired profile with no release identity to abuse, no
asset write, and without consuming the one-shot `released` slot that
`release-gate-activation` task 0.1 reserved.

## What Changes

- Commit a hosted `workflow_dispatch` profile as the repository's
  classification profile of record: `bench-evaluator.txt`, `bench-vm.txt`, and
  `verdict.txt` from a run at the gate's fixed parameters (`GOMAXPROCS=2`,
  `BENCHTIME=200ms`, `BENCH_COUNT=10`), stored with the run id, ref, and date
  that produced them. The run SHALL measure the tree the next release is cut
  from, not the tree of a release whose verdict is already known — see task 1.1,
  where run `30610843591` demonstrates the mechanism and is disqualified as the
  profile for exactly that reason.
- Reclassify the misclassified cells in `internal/perfgate/tiers.json` against
  that profile, tier by tier, with the profile's own figure recorded next to
  each change.
- State in `consumer-release-gate` that the classification profile and the
  stored non-regression baseline are distinct artifacts with distinct rules:
  the profile is checked in and licenses tiers; the asset is stored per release
  and answers non-regression. A profile SHALL be checked in before the release
  whose candidate results its tiers judge.
- Do not hand-upload any `bench-vm.txt` to a release. The first stored baseline
  is produced by the workflow itself on the next release whose gate passes,
  which is what `release-gate-activation` tasks 1.2, 4.2, and 5.1 wait for.

## Impact

- Affected specs: `consumer-release-gate` (new requirement separating the
  classification profile from the stored baseline asset, and stating the
  ordering rule the profile must satisfy).
- Affected code/docs: `internal/perfgate/tiers.json` (the reclassification and
  its comment), the committed profile files, CHANGELOG `[Unreleased]`.
- Unblocks: `release-gate-activation` 1.2 / 4.2 / 5.1 become reachable — not
  complete — once a release is cut whose gate passes against corrected tiers.
  The cut is not this change's to make either.
- Risk: this change edits the gate's pass/fail surface, which is exactly what
  `release-gate-activation` declined to do mid-activation. It is safe to do
  here because the activation is finished: the gate has run, its verdict is
  recorded, and the misclassification is a measured finding rather than an
  assumption. The residual risk is that tiers fitted to a profile of `master`
  are fitted to the tree the next release measures — bounded by what a tier
  asserts, which is a qualitative shape (engine-sensitive / data-dominated /
  startup), not a per-cell threshold.
- Sequencing: after `release-gate-activation`'s first hosted run, which has
  happened. Before any change that adds a gate cell, since a new cell's tier
  needs the same profile mechanism this change establishes.
