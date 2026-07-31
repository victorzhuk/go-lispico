## Context

Task 0's gate asked whether `bytecodeEvaluator.mu` (`runtime/eval.go:52`) is a
load-bearing serialization point under concurrent shared-Engine use, before any
striping is designed. This document records that measurement and the verdict.

**The gate fires.** Contention is not marginal: at `GOMAXPROCS=24` the
cache-hit steady state spends 29.5s of aggregate mutex wait against 6.1s of
wall clock, and adding cores past 8 makes the workload *slower*.

## The instrument

`runtime/bench_concurrent_test.go` — `BenchmarkEngine_EvalCachedConcurrent`,
two arms over one shared Engine (`WithBytecode` + `clojure.Dialect()` + stdlib,
mirroring `internal/goldset.NewEngine`'s `ModeVM` construction):

- `hits` — a warm 4-source working set (branching, closure state, keyword
  lookup, collection fold); every evaluation hits the chunk cache, so the only
  lock section per call is the probe at `eval.go:363-365`.
- `mixed` — the same working set with roughly 1 evaluation in 8 forced to miss
  via a fixed-shape literal carrying a counter, so the macro-epoch flush
  (`:378-380`) and admit (`:402-409`) sections are exercised and the LRU sees
  real evict traffic.

`b.RunParallel` ties goroutine count to `GOMAXPROCS`; each goroutine starts at
its own offset into the working set so they do not march in lockstep on one key.

### Why it is not a gold-set cell

Task 0.1 called for a gold-set cell with a committed tier in
`internal/perfgate/tiers.json`. That half is **deferred, not done** — see
`tasks.md` for the amendment. `release.yml:132-133` runs `-bench .` over
`./internal/goldset/`, and `cmd/perfgate/main.go:112-117` fails any cell with no
committed tier, so a cell cannot be added without a tier; committing a tier is
blocked by the tiers↔baseline loop, which `gate-corpus-cl-and-recursion` task
0.1 declined to work around on 2026-07-30. The benchmark therefore lives in
`runtime/` alongside `BenchmarkEngine_Call*`, where it measures the same thing
without touching the release gate's pass/fail surface.

## Method

**Numerator.** `-mutexprofile` accumulates process-wide, so the raw total is not
the answer — at `GOMAXPROCS=24` it also carries 19.7s of `runtime.unlock`
(scheduler/allocator locks) and 4.4s of `_LostContendedRuntimeLock`. The figure
below is the delay attributed to `EvalCached` specifically, read with
`pprof -peek 'sync\.\(\*Mutex\)\.Unlock$'`. That attribution is unambiguous:
**98.8-99.9% of all `sync.Mutex` delay traces to `EvalCached` at every
concurrency level and in both arms**, with `sync.(*Pool).pinSlow` the only other
contributor at ≤1.2%.

**Denominator.** Wall clock is taken as `ns/op × N` from the benchmark's own
output — the timed loop only. Process wall clock would include engine
construction and cache warmup and would understate the ratio. Warmup runs
single-goroutine, and a mutex profile records only *contended* wait, so setup
contributes ~0 to the numerator even though the profile spans the process.

**Normalization is stated, not assumed.** Task 0.3 specifies total mutex-wait ÷
wall clock. That numerator is delay summed across all goroutines while the
denominator is a single timeline, so the ratio can exceed 100% and is not a
"share of wall time." Both conventions appear below; the verdict does not depend
on the choice.

Each arm was profiled in its own invocation (`-bench` scoped to the single
sub-benchmark) so the process-wide profile contains only that arm.
`-blockprofile` ran as a separate confirmatory pass — the two profilers perturb
each other and are never combined.

## Results

`-benchtime=5s`, go1.26.5, AMD Ryzen AI 9 HX 370 (24 logical CPUs),
`powersave` governor.

| arm | GOMAXPROCS | ns/op | N | wall (s) | `EvalCached` delay (s) | delay/wall | delay/(wall×P) |
| --- | --- | --- | --- | --- | --- | --- | --- |
| hits | 2 | 1551.0 | 3863582 | 5.992 | 0.003 | 0.05% | 0.02% |
| hits | 8 | 822.4 | 7509444 | 6.176 | 1.205 | **19.52%** | 2.44% |
| hits | 24 | 1302.0 | 4658684 | 6.066 | 29.461 | **485.71%** | **20.24%** |
| mixed | 2 | 3861.0 | 1566564 | 6.049 | 0.698 | **11.53%** | **5.77%** |
| mixed | 8 | 3729.0 | 1600610 | 5.969 | 60.543 | **1014.35%** | **126.79%** |
| mixed | 24 | 3886.0 | 1539045 | 5.981 | 219.322 | **3667.15%** | **152.80%** |

Threshold was >5% at `GOMAXPROCS=8` or higher. Every cell at 8 and 24 clears it
under the stated convention. Under the per-CPU convention every cell at 8 and 24
clears it except `hits` at 8 (2.44%) — and `mixed` at 8 is 126.79% in the same
column, so the gate fires either way.

### Three confirmations that need no normalization at all

1. **`hits` throughput regresses past 8 cores** — 822.4 ns/op at
   `GOMAXPROCS=8`, 1302 ns/op at 24. Tripling the cores costs 58% throughput.
2. **`mixed` does not scale at all** — 3861 / 3729 / 3886 ns/op at 2 / 8 / 24.
   Twelve times the cores buys nothing measurable.
3. **Block profile, independent of the mutex profile** — at `GOMAXPROCS=24`,
   `EvalCached` accounts for **60.21%** of all blocking in `hits` and **87.74%**
   in `mixed`, of which 99.8-100% is `sync.(*Mutex).Lock`.

Scaling from 2→8 in `hits` is 1.89× against an available 4×, so the ceiling is
already visible below the threshold level.

## Limitations

- **One box, one session.** The absolute ns/op figures are subject to this
  workstation's known drift and should not be quoted as bars. The verdict does
  not rest on them: the ratios are computed within each run, and the two
  scaling facts are direction-only.
- **`Eval` re-parses per call.** The benchmark drives the public
  `Eval(ctx, name, source)` path, so `core.Read` runs every iteration and its
  cost sits in the denominator. This makes the reported ratio *conservative* —
  removing parse from the denominator would raise every figure.
- **`mixed`'s miss rate is a construction, not a measurement.** One in eight is
  a chosen ratio, not a consumer-observed one. It bounds admit/evict traffic;
  it does not claim a real workload misses that often.
- **`engineImpl.mu` is on the same path** and is explicitly out of this change's
  scope (proposal, "Explicitly out of scope"). It does not appear in the
  attribution above, so it is not distorting the numerator.
- **The gate's own runner is `GOMAXPROCS=2`** (`release.yml:55`), the one level
  where `hits` contention is negligible (0.05%). A gold-set concurrent cell,
  once the tiers loop allows one, would measure at the level least able to see
  this — worth carrying into that cell's design.

## What section 1 must now decide

The gate firing authorizes sections 1-3, not a particular design. Both open
questions stand as the proposal framed them:

- **1.1 budget accounting** — atomic global running total for
  `MaxCacheBytes`/`MaxCacheNodes` (approximate cross-stripe eviction ordering)
  versus fixed per-stripe quotas with a documented tolerance. The hazard named
  in the proposal's Impact — a cache silently exceeding `MaxCacheBytes` across
  stripes — is an existing tested invariant and the reason 3.3 exists.
- **1.2 stripe key and count** — hash of the existing `cacheKey`
  (`{sourceHash, formIndex, dialectFP, macroEpoch}`), count sized to expected
  concurrent use rather than maximal.

One measurement note for 1.1: the profile shows the probe alone
(`hits`, one lock acquisition per call) already saturating, so a design that
only shortens the admit path would not address the dominant cost.
