# Classification profile — run 30630796967

**Superseded by `profile-30637802780`, and corrected on two points.** The
`BENCHTIME` row below reads 200ms, but this run's verdict was inconclusive
(exit code 2), so the "Rerun paired benchmark at doubled benchtime" step ran,
and that step deletes both bench files and regenerates them at doubled
benchtime before the upload. The files committed here are therefore a **400ms**
measurement — `Goldset/counter-closure-2` reads 57092 iterations here against
28188 in the 200ms `profile-30637802780`, on the same runner CPU and within 2%
on ns/op. For the same reason the statement below that `verdict.txt` is "the
pre-rerun verdict" is wrong: this run's actual pre-rerun verdict was computed
against the 200ms files the rerun step then deleted, and the file here is a
no-`-rerun`-flag verdict over post-rerun data. Neither correction invalidates
a tier this profile licensed — a percentage delta at 400ms is as sound as one
at 200ms, and every tier still holds on the successor profile's 200ms
figures — but the parameters this directory reports were not the parameters
that produced its files. The raw files are left untouched; correcting them
would destroy the evidence rather than the error.

The checked-in baseline profile ADR 0008's Thresholds section requires: the
paired Evaluator/VM measurement that licenses every tier in
`internal/perfgate/tiers.json`. It is not a stored non-regression baseline —
that artifact is the `bench-vm.txt` a passing release uploads to itself, and
no release carries one yet.

This profile measures the tree at `2910e79`, which lands two fixes over the
tree `profile-30614184386` measured: `sha256Hash` (`runtime/eval.go`) no
longer copies the source string on every VM `Eval`, and `internal/perfgate`'s
`evaluateStartup` applies the bytes and allocation-count non-increasing
bounds every other tier already applied. This is the first release gate
dispatch in the repo's history to pass all 26 cells.

## Provenance

| field | value |
| --- | --- |
| run id | `30630796967` |
| workflow | `.github/workflows/release.yml` ("Release consumer gate") |
| event | `workflow_dispatch` |
| ref | `master` |
| head sha | `2910e791fe7b769ffb33f4c2a09e7849785475a6` |
| date | 2026-07-31 |
| runner | `ubuntu-24.04`, AMD EPYC 7763 64-Core Processor, linux/amd64 |
| `GOMAXPROCS` | 2 |
| `BENCHTIME` | 200ms |
| `BENCH_COUNT` | 10 |
| conclusion | success (job `consumer-gate`, no failed steps) |

Cell names carry the `-2` suffix Go appends from `GOMAXPROCS`;
`perfgate.TrimProcsSuffix` strips it before the tiers lookup.

The runner CPU differs from `profile-30614184386`'s (Intel Xeon Platinum
8573C). That is an uncontrolled variable across runs, same as it was there —
a tier is licensed by one profile, never by agreement between runs, and this
README does not compare figures against the prior profile for that reason.

## What the run did

The job succeeded end to end: gold set under both execution modes, the race
suite, and the paired benchmark all completed clean. "Determine gate mode"
and "Store VM baseline on the authorized release" were skipped, same as
every other dispatch run: a `workflow_dispatch` run carries no release
identity, so it writes no asset and consumes no baseline slot.

`verdict.txt` is the **pre-rerun** verdict, produced by `cmd/perfgate` in
first-authorization mode against `tiers.json` as committed, without the
`-rerun` flag. Thirteen cells — twelve of the thirteen `GoldsetParse/*`
cells, all but `safe-parse`, plus `Goldset/guard-nil` — read INCONCLUSIVE because their
latency delta is not statistically significant; a real gate run would rerun
those at doubled benchtime before resolving them. That is expected of a
first attempt, not a defect in the measurements.

## Reproducing the comparison

```sh
go build -o bin/perfgate ./cmd/perfgate
bin/perfgate \
  -old internal/perfgate/testdata/profile-30630796967/bench-evaluator.txt \
  -candidate internal/perfgate/testdata/profile-30630796967/bench-vm.txt \
  -tiers internal/perfgate/tiers.json \
  -mode first-authorization \
  -out /dev/stdout
```

`benchstat.csv` in this directory is the same comparison, pre-rendered with
the exact `benchstat` version `cmd/perfgate` pins
(`golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d`), so
`TestPinnedProfile` can parse it without shelling out to the network.

The benchmark output is committed verbatim. Editing it invalidates the
provenance above, and with it every tier the profile licenses.

Replacing this profile with a newer one is a five-part act, not a file copy:
the two raw bench files, `benchstat.csv` (regenerated with whatever
`benchstat` version `cmd/perfgate` pins at the time), `verdict.txt`
(regenerated against the tiers current at commit time), and the SHA-256
digest constants `perfgate_test.go`'s `TestPinnedProfile` checks the raw
files against. Missing any one of the five leaves a stale artifact that
either the test or a later reader silently trusts.

Changing the gate's own logic invalidates `verdict.txt` the same way, without
any file here being touched: the verdict is a judgment over the benchmark
output, not a property of it. A change to how a tier is evaluated must
regenerate it against the new logic. The digest constants stay valid in that
case — the raw benchmark files have not moved — so the test will fail on the
verdict alone, which is the intended signal rather than a broken pin.

## Tier classification

Every figure below is this profile's own, VM against Evaluator, from
`benchstat.csv`; `-` means the VM is lower. ADR 0008's engine-sensitive rule
needs latency at or below -15% **and** bytes at or below -20%, with
allocation count non-increasing; data-dominated needs latency within +-5%
two-sided at first authorization, with bytes and allocation count
non-increasing; startup takes the same bounds but may clear the latency leg
through the absolute "1 ms / 256 KiB" overhead escape instead.

| cell | latency | bytes | allocs | tier | verdict |
| --- | --- | --- | --- | --- | --- |
| `counter-closure` | -30.95% | -46.12% | -18.31% | engine-sensitive | PASS |
| `guard-nil` | ~ (p=0.424) | +0.09% | ~ (p=1.000) | data-dominated | INCONCLUSIVE |
| `kw-lookup` | -21.82% | -21.70% | -17.95% | engine-sensitive | PASS |
| `loop-sum` | -93.36% | -91.08% | -85.35% | engine-sensitive | PASS |
| `merge-config` | -21.72% | -26.59% | -17.81% | engine-sensitive | PASS |
| `pipeline` | -43.64% | -75.01% | -51.97% | engine-sensitive | PASS |
| `queue-promote` | -65.63% | -30.65% | -55.04% | engine-sensitive | PASS |
| `registry-fold` | -31.59% | -51.07% | -34.51% | engine-sensitive | PASS |
| `route-decision` | -29.58% | -44.04% | -30.99% | engine-sensitive | PASS |
| `rule-load` | -6.36% | -36.25% | -23.15% | startup | PASS |
| `safe-parse` | -42.00% | -55.85% | -30.77% | engine-sensitive | PASS |
| `text-render` | -27.20% | -42.72% | -24.56% | engine-sensitive | PASS |
| `twice-macro` | -21.66% | -30.69% | -18.64% | engine-sensitive | PASS |

The thirteen `GoldsetParse/*` cells (all data-dominated) are not tabulated
here: twelve read `~`/`~`/`~` at first authorization (INCONCLUSIVE pending a
rerun) and `GoldsetParse/safe-parse` reads +0.79%/`~`/`~` (PASS, within the
5% tolerance). None of them license or change a tier; `tiers.json`'s comment
already covers why the tier is data-dominated for the whole group.

### No tier changes

Every cell's committed tier still describes its measured shape. The eleven
engine-sensitive cells above all clear both floors — latency at or below
-15% and bytes at or below -20% — on merit, not on an escape. `kw-lookup`
and `merge-config`, the two cells this change targeted, now clear the bytes
floor they missed on the prior profile (-9.04% and -19.96% there, against
the -20% floor). `guard-nil`'s latency reads as not statistically
significant here, which is stronger evidence for a mode-invariant,
data-dominated cost than the prior profile's +2.65% was.

### `rule-load` exercises the new startup bound live

`rule-load`'s latency reads -6.36%, outside the +-5% tolerance, so under
first-authorization it passes through ADR 0008's absolute-overhead escape:
its latency (21.6us) and bytes (5841 B) both sit far under the 1 ms / 256
KiB floor. Having taken that escape, it still has to clear the bytes and
allocation-count bounds this change added to the startup tier — and does,
at -36.25% and -23.15%. This run is the first time both the escape path and
the new bound have fired together on a real measurement rather than only in
a unit test.

### `guard-nil` passes without its bytes bound ever being evaluated

This is the most important thing the run showed. `guard-nil`'s bytes read
1128 B/op under the Evaluator against 1129 under the VM — +0.09%, p=0.000,
0% CI on both arms, a deterministic one-byte increase. Its tier is
data-dominated, which bounds bytes non-increasing, so this figure violates
the bound. The gate never checks it: under first-authorization a
data-dominated cell routes to `evaluateWithinTolerance`, which returns
INCONCLUSIVE the moment `cell.Latency.Significant` is false — `guard-nil`'s
latency read `~` at p=0.424 — before the function ever reaches
`nonIncreasing`. `Resolve(TierDataDominated, ModeFirstAuthorization)` then
collapses that INCONCLUSIVE to PASS under ADR 0008's burden-of-proof rule.
The cell's green is burden-of-proof, not merit.

`evaluateWithinTolerance`, `evaluateNonRegression`,
`evaluateEngineSensitiveImprovement`, and `evaluateStartup` all share this
ordering: each returns INCONCLUSIVE on a non-significant latency delta
before it looks at bytes or allocations at all. That is all four of
`perfgate.go`'s tier-evaluator functions, not three of them — and
`evaluateStartup` is the one this change modified. `rule-load`, the cell it
evaluates, sits two rows above in the tier table above: the bytes and
allocation-count bounds this change added to the startup tier carry the
identical blind spot on any future startup cell whose latency goes
non-significant, the same way `guard-nil`'s data-dominated bound does here.

No earlier profile could have shown this. Every other cell in this run that
reports INCONCLUSIVE — the twelve non-significant `GoldsetParse/*` cells —
is bit-identical on bytes (p=1.000), so a non-significant latency has never
before come paired with a significant bytes delta. `guard-nil` is the first
cell in the corpus where it does.

This is recorded as a finding for a later change to own. Fixing the
ordering, or amending a threshold, would change the verdict of all 26
cells, and both are the repo owner's call, not this profile's.
