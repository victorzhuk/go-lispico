# Classification profile — run 30614184386

The checked-in baseline profile ADR 0008's Thresholds section requires: the
paired Evaluator/VM measurement that licenses every tier in
`internal/perfgate/tiers.json`. It is not a stored non-regression baseline —
that artifact is the `bench-vm.txt` a passing release uploads to itself, and no
release carries one yet.

## Provenance

| field | value |
| --- | --- |
| run id | `30614184386` |
| workflow | `.github/workflows/release.yml` ("Release consumer gate") |
| event | `workflow_dispatch` |
| ref | `master` |
| head sha | `4607f1e2adb25d5fe24e788e51f9a22efb51528f` |
| date | 2026-07-31 |
| runner | `ubuntu-latest`, Intel Xeon Platinum 8573C, linux/amd64 |
| `GOMAXPROCS` | 2 |
| `BENCHTIME` | 200ms |
| `BENCH_COUNT` | 10 |

Cell names carry the `-2` suffix Go appends from `GOMAXPROCS`;
`perfgate.TrimProcsSuffix` strips it before the tiers lookup.

## What the run did

Gold set under both execution modes passed, the race suite passed, and the
paired benchmark completed — the three legs that decide whether a run is usable
as a profile. "Determine gate mode" and "Store VM baseline on the authorized
release" were skipped: a dispatch run carries no release identity, so it writes
no asset and consumes no baseline slot.

`verdict.txt` is the **pre-reclassification** verdict, produced by
`cmd/perfgate` in first-authorization mode against the tiers as they stood
before this profile licensed any correction. It fails on seven cells. That is
what the profile was collected to diagnose, not a defect in the measurements:
a verdict is a judgment applied to the numbers rather than a property of them.

## Reproducing the comparison

```sh
go build -o bin/perfgate ./cmd/perfgate
bin/perfgate \
  -old internal/perfgate/testdata/profile-30614184386/bench-evaluator.txt \
  -candidate internal/perfgate/testdata/profile-30614184386/bench-vm.txt \
  -tiers internal/perfgate/tiers.json \
  -mode first-authorization \
  -out /dev/stdout
```

The benchmark output is committed verbatim. Editing it invalidates the
provenance above, and with it every tier the profile licenses.

## Tier classification

Every figure is this profile's own, VM against Evaluator, `-` meaning the VM is
lower. ADR 0008's engine-sensitive rule needs latency at or below -15% **and**
bytes at or below -20%, with allocation count non-increasing; data-dominated
needs latency within +-5% two-sided at first authorization, with bytes and
allocation count non-increasing.

| cell | latency | bytes | allocs | tier | change |
| --- | --- | --- | --- | --- | --- |
| `route-decision` | -28.81% | -32.81% | -29.58% | engine-sensitive | kept |
| `loop-sum` | -93.55% | -90.26% | -85.19% | engine-sensitive | kept |
| `counter-closure` | -30.95% | -38.60% | -16.90% | engine-sensitive | kept |
| `safe-parse` | -40.84% | -47.79% | -29.81% | engine-sensitive | kept |
| `twice-macro` | -18.89% | -20.51% | -16.95% | engine-sensitive | kept |
| `kw-lookup` | -20.00% | -9.04% | -15.38% | engine-sensitive | kept |
| `guard-nil` | +2.65% | +19.40% | +3.12% | data-dominated | **changed** |
| `registry-fold` | -31.64% | -44.03% | -33.63% | engine-sensitive | **changed** |
| `merge-config` | -20.59% | -19.96% | -16.44% | engine-sensitive | **changed** |
| `text-render` | -25.46% | -35.23% | -22.81% | engine-sensitive | **changed** |
| `pipeline` | -46.20% | -72.69% | -51.32% | engine-sensitive | **changed** |
| `queue-promote` | -66.47% | -28.65% | -54.78% | engine-sensitive | **changed** |
| `rule-load` | -3.90% | -28.28% | -22.69% | startup | kept |

Five cells committed as data-dominated are the corpus's most engine-sensitive:
`queue-promote`, `pipeline`, `registry-fold`, `text-render`, and
`merge-config` each cut latency by 20% or more when the VM runs them, which a
mode-invariant cost cannot do. They failed only because a two-sided +-5% band
fails a cell for improving.

`guard-nil` moves the other way. Its latency is flat across modes, which is
what data-dominated describes, so that is the tier it now carries. It fails
there on allocated bytes.

`kw-lookup`, `merge-config`, and `guard-nil` fail under the tier that describes
them, and no other tier fits. All three fail on the bytes axis alone:

- `guard-nil` -- the VM allocates 19.40% more than the Evaluator. On the
  first hosted run's measurement of `fee885a` the same gap read +9.64%. The
  reader-allocation
  work between the two trees cut the shared denominator (1160 B/op down from
  2344), so a roughly fixed extra allocation now reads as a larger share. The
  gap is not new; it is more visible.
- `kw-lookup` -- bytes -9.04% against the -20% floor, with latency at -20.00%.
  The cell is engine-sensitive by latency and cannot clear the allocation half
  of its own tier.
- `merge-config` -- bytes -19.96% against the -20% floor. B/op carries a 0% CI
  and is deterministic per tree, so this is genuinely under the floor rather
  than noise, and a sub-1% allocation win on the VM path flips it.

## Disagreements with run 30561584997

Run `30561584997` measured `v0.11.0`'s tree (`fee885a`); this profile measures
`4607f1e`, 22 commits later. Three cells disagree, and the profile's figure is
the one that licenses a tier:

- `twice-macro` -- FAIL then, PASS now. Bytes moved -12.65% to -20.51%,
  clearing the engine-sensitive floor. Its committed tier was correct all
  along and needed no change.
- `kw-lookup` -- bytes -5.36% to -9.04%. Still short of the floor.
- `merge-config` -- latency -18.00% to -20.59%. Its engine sensitivity is
  clearer on this tree than on the one the first run measured.

Runs are not interchangeable even on one tree. Run `30561584997` and the
later dispatch `30610843591` both measured `fee885a` on an AMD EPYC 7763;
this profile ran on an Intel Xeon Platinum 8573C. Figures above are quoted
from run `30561584997`, the first hosted run, and a cross-run comparison
carries a runner difference the gate never controls for — which is why a
tier is licensed by one profile rather than by agreement between runs.
