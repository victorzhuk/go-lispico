# Classification profile — run 30637802780

The checked-in baseline profile ADR 0008's Thresholds section requires: the
paired Evaluator/VM measurement that licenses every tier in
`internal/perfgate/tiers.json`. It is not a stored non-regression baseline —
that artifact is the `bench-vm.txt` a passing release uploads to itself, and
no release carries one yet.

This profile measures the tree at `0a36275`, which adds one cell over the
tree `profile-30630796967` measured: `GoldsetCall/call-boundary`, the first
gate cell to exercise `Engine.Call` rather than `Engine.Eval`. It is the
profile that licenses that cell's tier, and it re-licenses the twenty-six
cells that were already committed.

## Provenance

| field | value |
| --- | --- |
| run id | `30637802780` |
| workflow | `.github/workflows/release.yml` ("Release consumer gate") |
| event | `workflow_dispatch` |
| ref | `zapply/gate-deferred-measurements`, merged into `master` by the change that added the cell |
| head sha | `0a36275cbbb15ecbb45b311982f242a874d835de` |
| date | 2026-07-31 |
| runner | `ubuntu-24.04` image 20260720.247.2, AMD EPYC 7763 64-Core Processor, linux/amd64 |
| `GOMAXPROCS` | 2 |
| `BENCHTIME` | 200ms |
| `BENCH_COUNT` | 10 |
| conclusion | failure (job `consumer-gate`, one failed step — by construction, see below) |

Cell names carry the `-2` suffix Go appends from `GOMAXPROCS`;
`perfgate.TrimProcsSuffix` strips it before the tiers lookup.

The runner CPU matches `profile-30630796967`'s. That is coincidence, not
control — a tier is licensed by one profile, never by agreement between runs,
and this README does not compare figures against the prior profile except
where it says so explicitly and for a reason.

## Why the run failed, and why that is the shape of a classification run

The job's correctness legs passed — the gold set under both execution modes
and the race suite — and the paired benchmark completed. The run then failed
at "Enforce performance gate verdict", on exactly one line of its own
verdict:

```
GoldsetCall/call-boundary-2: FAIL no committed tier for this cell
```

That is the only outcome a new cell can produce on the run that first
measures it. `cmd/perfgate` fails any cell absent from `tiers.json`, and
`TestPinnedProfile` requires `benchstat.csv`, `tiers.json`, and `verdict.txt`
to agree on which cells exist — so a tier cannot be committed before a
profile measures the cell, and a profile cannot pass before the tier is
committed. The cell was therefore added untiered, this run measured it, and
its tier was committed against these figures afterwards. Every other cell in
the run reports its own verdict normally; none of them regressed or changed
tier.

## These files are at the benchtime the table states

The run exited with a fail (exit code 1) rather than an inconclusive (exit
code 2), so the "Rerun paired benchmark at doubled benchtime" step never
ran, and `bench-evaluator.txt` / `bench-vm.txt` are the 200ms measurement the
provenance table names.

That is worth stating because the profile this one supersedes is not clean on
this axis. Run `30630796967` did hit the rerun path: its own step log records
"Rerun paired benchmark at doubled benchtime" and "Resolve inconclusive cells
after rerun" as executed, and that step begins by deleting both bench files
and regenerating them at doubled benchtime. So the files committed under
`profile-30630796967/` are a 400ms measurement, while that directory's
README records `BENCHTIME | 200ms`. The iteration counts show it directly —
`Goldset/counter-closure-2` reads 57092 iterations there against 28188 here,
a factor of 2.02, on the same runner CPU and within 2% on ns/op.

The tiers that profile licensed are not invalidated by this: a percentage
delta at 400ms is as sound as one at 200ms, and every tier it assigned still
holds on the 200ms figures below. What was wrong was the provenance line, and
the same run's README claim that its `verdict.txt` is "the pre-rerun
verdict" — the run's actual pre-rerun verdict was computed against the 200ms
files the rerun step then deleted. A note recording this now sits in that
profile's README. It is recorded here rather than quietly fixed because a
gate whose stored evidence does not match its stated parameters is the defect
this corpus exists to catch, and it went uncaught for one profile.

## Reproducing the comparison

```sh
go build -o bin/perfgate ./cmd/perfgate
bin/perfgate \
  -old internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt \
  -candidate internal/perfgate/testdata/profile-30637802780/bench-vm.txt \
  -tiers internal/perfgate/tiers.json \
  -mode first-authorization \
  -out /dev/stdout
```

`benchstat.csv` in this directory is the same comparison, pre-rendered with
the exact `benchstat` version `cmd/perfgate` pins
(`golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d`), so
`TestPinnedProfile` can parse it without shelling out to the network.

`verdict.txt` is regenerated locally against the tiers as committed here,
without the `-rerun` flag — so it reports the new cell's real verdict (PASS)
rather than the run's own "no committed tier" line, and it leaves the
fourteen non-significant cells INCONCLUSIVE where a real gate run would rerun
them once at doubled benchtime and resolve them. Regenerating it produces
exit code 2, which is that pending rerun and not a failure.

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
| `Goldset/counter-closure` | -30.81% | -46.13% | -18.31% | engine-sensitive | PASS |
| `Goldset/guard-nil` | ~ (p=0.055) | +0.09% | ~ (p=1.000) | data-dominated | INCONCLUSIVE |
| `Goldset/kw-lookup` | -21.61% | -21.70% | -17.95% | engine-sensitive | PASS |
| `Goldset/loop-sum` | -93.35% | -91.08% | -85.35% | engine-sensitive | PASS |
| `Goldset/merge-config` | -20.81% | -26.57% | -17.81% | engine-sensitive | PASS |
| `Goldset/pipeline` | -45.04% | -75.01% | -51.97% | engine-sensitive | PASS |
| `Goldset/queue-promote` | -66.92% | -30.65% | -55.04% | engine-sensitive | PASS |
| `Goldset/registry-fold` | -31.59% | -51.06% | -34.51% | engine-sensitive | PASS |
| `Goldset/route-decision` | -29.18% | -44.01% | -30.99% | engine-sensitive | PASS |
| `Goldset/rule-load` | -5.42% | -36.22% | -23.15% | startup | PASS |
| `Goldset/safe-parse` | -42.70% | -55.85% | -30.77% | engine-sensitive | PASS |
| `Goldset/text-render` | -27.19% | -42.72% | -24.56% | engine-sensitive | PASS |
| `Goldset/twice-macro` | -21.67% | -30.69% | -18.64% | engine-sensitive | PASS |
| `GoldsetCall/call-boundary` | -80.66% | -100.00% | -100.00% | engine-sensitive | PASS |

The thirteen `GoldsetParse/*` cells (all data-dominated) are not tabulated
here: all thirteen read `~`/`~`/`~` at first authorization (INCONCLUSIVE
pending a rerun). None of them licenses or changes a tier; `tiers.json`'s
comment already covers why the tier is data-dominated for the whole group.
On the prior profile twelve of them read that way and `GoldsetParse/safe-parse`
read +0.79% at p=0.041; here it reads not significant at p=0.225, which is
the mode-invariance the tier expects rather than a change in the reader.

### No tier changes among the twenty-six existing cells

Every previously committed tier still describes its measured shape. The
eleven engine-sensitive cells above all clear both floors — latency at or
below -15% and bytes at or below -20% — on merit, not on an escape.
`rule-load` again takes ADR 0008's absolute-overhead escape on latency
(-5.42%, just outside the +-5% tolerance, at 22.3us and 5843 B against the
1 ms / 256 KiB floor) and clears the startup tier's bytes and
allocation-count bounds at -36.22% and -23.15%.

### `GoldsetCall/call-boundary` — what it measures, and what its figure excludes

The cell `Eval`s `(defn call-boundary [a b] a)` once, untimed, then makes
exactly one `Engine.Call` per timed iteration. The callee is GoFunc-free —
its body is a single local read — so the timed cost is the boundary's own:
argument marshalling, function-cell lookup, and frame setup.

| arm | latency | bytes | allocs |
| --- | --- | --- | --- |
| Evaluator | 974.75 ns/op (CI 1%) | 768 B/op | 6 |
| VM | 188.50 ns/op (CI 1%) | 0 B/op | 0 |
| delta | -80.66% (p=0.000, n=10) | -100.00% (p=0.000) | -100.00% (p=0.000) |

Four qualifiers travel with the 188.50 ns figure wherever it is quoted:

- **It is a hosted-runner figure.** Measuring the same cell on a developer box
  does not check it: ten samples at these same parameters span 120.4-196.2 ns
  in one session on one box, a range that straddles this figure. A single fast
  sample from such a box (89.57 ns/op at `-benchtime=50ms -count=1`) is inside
  that noise, not evidence against the hosted number. This is why the corpus
  admits hosted figures only.
- **It excludes the caller's argument slice.** The cell passes a pre-built
  `[]core.Value` (`CallCell.Args`), so the variadic slice is hoisted out of
  the timed loop. `runtime`'s own Call benchmarks construct it inline and
  still measure 32 B/op and 1 alloc/op for it — two 16-byte interface
  headers — which is the whole of the difference between their non-zero
  allocation figure and this cell's zero. The cell measures engine-side
  boundary cost; an embedder building fresh arguments per call pays that
  slice on top.
- **Part of the delta is which Call path each mode takes, not boundary work
  either mode could avoid.** `Engine.Call` routes to the lean boundary only
  when a bytecode evaluator is present (`e.fastPath` is
  `bytecodeEvaluator != nil && engineMeter == nil`, `runtime/engine.go`), so
  the Evaluator arm always falls through to the general `callBoundary`, which
  builds a resource context and an eval state (`evalResourceContext`,
  `StartEval`/`FinishEval`) that `callBoundaryLean` never touches. On an
  `alloc_space` profile of the Evaluator arm that setup is 33.1% of the arm's
  allocated bytes — `newEvalStateWithLimits` 25.4% plus `context.WithValue`
  7.7% — against roughly two thirds for `Env.ChildVariadic`, the call frame
  itself. Both arms pay what that mode really costs a consumer, so the figure
  is honest as a mode comparison; it is not a measurement of boundary work
  alone.
- **It is a warm, uncontended figure.** The call-site cache is warm from the
  second iteration on, and the engine's private VM slot is claimed by an
  uncontended CAS in a single-goroutine loop, so the cell reports the
  best-case boundary. That matches every other `Goldset*` cell's methodology,
  and is stated here because this cell's absolute figure is the one an
  external target gets adjudicated against.

The VM arm's `0 B/op` is therefore a real measurement of the engine's own
allocation on this path, not a claim that a `Call` can never allocate. The
returned value here is a small integer; nothing in this profile establishes
what the figure would be for a return shape that must be boxed onto the heap.

### `guard-nil` still passes without its bytes bound being evaluated

The finding `profile-30630796967` recorded reproduces here, on a second
runner-independent measurement. `guard-nil`'s bytes read 1128 B/op under the
Evaluator against 1129 under the VM — +0.09%, p=0.000, 0% CI on both arms,
the same deterministic one-byte increase. Its tier is data-dominated, which
bounds bytes non-increasing, so this figure violates the bound. The gate
never checks it: under first-authorization a data-dominated cell routes to
`evaluateWithinTolerance`, which returns INCONCLUSIVE the moment
`cell.Latency.Significant` is false — `guard-nil`'s latency reads `~` at
p=0.055 here — before the function ever reaches `nonIncreasing`.
`Resolve(TierDataDominated, ModeFirstAuthorization)` then collapses that
INCONCLUSIVE to PASS under ADR 0008's burden-of-proof rule. The cell's green
is burden-of-proof, not merit.

`evaluateWithinTolerance`, `evaluateNonRegression`,
`evaluateEngineSensitiveImprovement`, and `evaluateStartup` all share this
ordering: each returns INCONCLUSIVE on a non-significant latency delta
before it looks at bytes or allocations at all. That the finding survives a
second profile, with the p-value moving from 0.424 to 0.055 while the byte
figure stays exactly +1, is evidence it is structural rather than an artifact
of one run's noise. It remains a finding for a later change to own — fixing
the ordering, or amending a threshold, would change the verdict of every cell
in the corpus, and both are the repo owner's call.

`vm-allocation-parity` task 3.2 holds the open question of which threshold,
if any, should be amended for this cell; nothing here settles it.

**The finding stopped being hypothetical on the next run.** Dispatch run
30639778105, on the tree that adds this profile and the Call cell's tier —
`internal/goldset` and every other engine path byte-identical to the tree
measured here — reported:

```
Goldset/guard-nil-2: FAIL (bytes increased by 0.09%)
```

Its latency delta came out at +2.46% with p=0.000: significant for the first
time in the corpus's history, and inside the +-5% tolerance, so
`evaluateWithinTolerance` cleared the significance gate, cleared the tolerance
check, and then reached `nonIncreasing` — which failed on the same 1128
against 1129 B/op this profile records. Nothing about the cell changed. The
only thing that changed is that one run's latency measurement happened to
reach significance where three earlier runs read `~`.

So the gate's green on `guard-nil` was never a property of the code; it was a
property of whether that run's latency delta cleared significance, and the
cell fails whenever it does. This is not a regression, not attributable to the
Call cell, and not fixable by any tier reassignment — the byte is real and
`vm-allocation-parity` task 3.2 already records why it is not a removable
allocation site. What is now settled is that the question that task holds is
load-bearing for every release, not a documentation nicety.
