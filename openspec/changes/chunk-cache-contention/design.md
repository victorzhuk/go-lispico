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

**Numerator.** `-mutexprofile` accumulates process-wide, and since Go 1.22 it
also samples runtime-internal locks, so the raw total is not the answer — the
`hits` profile at `GOMAXPROCS=24` totals 36.57s, of which only 19.62s is this
mutex; the rest is `runtime.unlock` (scheduler/allocator) and
`_LostContendedRuntimeLock`. The figure below is the delay attributed to
`EvalCached` specifically, read with
`pprof -peek 'sync\.\(\*Mutex\)\.Unlock$'`. That attribution is unambiguous:
**98.3-100% of all `sync.Mutex` delay traces to `EvalCached` at every
concurrency level and in both arms**, with `sync.(*Pool).pinSlow` the only other
contributor at ≤1.7%.

**Denominator, and a defect that had to be fixed first.** Wall clock is
`ns/op × N`. Under an ordinary `-benchtime=5s` that pairing is **wrong**: Go
reaches the target duration by re-running the benchmark function with an
increasing `b.N`, reports `ns/op` and `N` from the final run only, but
`-mutexprofile` accumulates across every run in the ramp — all of them fully
parallel and contending. The numerator then covers a longer interval than the
denominator.

That is not a rounding concern; it produced impossible numbers. Aggregate
blocked time cannot exceed `GOMAXPROCS × wall`, since that is all the
goroutine-seconds that exist. A first pass at `-benchtime=5s` put `mixed` at
`GOMAXPROCS=8` at 60.5s of delay against a 47.8s ceiling — 1.27× over — and at
24 at 1.53× over. **Any ratio failing that bound is measuring two different
intervals.**

The figures below therefore come from a re-run at a fixed iteration count
(`-benchtime=4000000x` for `hits`, `1500000x` for `mixed`), so the benchmark
function executes exactly once and there is no ramp. Timed-loop wall then
matches process wall to within ~10ms (e.g. 5.240s vs 5.263s), the two possible
denominators converge, and every cell satisfies the bound. `ns/op` reproduced
the ramped run closely (`hits` 1535/800.6/1310 vs 1551/822.4/1302), so the
correction is one of accounting, not of behavior.

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

Fixed iteration count, go1.26.5, AMD Ryzen AI 9 HX 370 (24 logical CPUs),
`powersave` governor. `wall` is `ns/op × N`; `bound` checks
delay ≤ `GOMAXPROCS × wall`.

| arm | GOMAXPROCS | ns/op | N | wall (s) | `EvalCached` delay (s) | delay/wall | delay/(wall×P) | bound |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| hits | 2 | 1535.0 | 4000000 | 6.140 | 0.002 | 0.03% | 0.02% | ok |
| hits | 8 | 800.6 | 4000000 | 3.202 | 0.503 | **15.71%** | 1.96% | ok |
| hits | 24 | 1310.0 | 4000000 | 5.240 | 19.623 | **374.48%** | **15.60%** | ok |
| mixed | 2 | 3960.0 | 1500000 | 5.940 | 0.402 | **6.77%** | 3.39% | ok |
| mixed | 8 | 3736.0 | 1500000 | 5.604 | 35.049 | **625.42%** | **78.18%** | ok |
| mixed | 24 | 3887.0 | 1500000 | 5.830 | 128.122 | **2197.44%** | **91.56%** | ok |

Threshold was >5% at `GOMAXPROCS=8` or higher. Every cell at 8 and 24 clears it
under the stated convention. Under the per-CPU convention every cell at 8 and 24
clears it except `hits` at 8 (1.96%) — and `mixed` at 8 is 78.18% in the same
column, so the gate fires either way.

### Three confirmations that need no normalization at all

1. **`hits` throughput regresses past 8 cores** — 800.6 ns/op at
   `GOMAXPROCS=8`, 1310 ns/op at 24. Tripling the cores costs 64% throughput.
2. **`mixed` does not scale at all** — 3960 / 3736 / 3887 ns/op at 2 / 8 / 24.
   Twelve times the cores buys nothing measurable.
3. **Block profile, independent of the mutex profile** — at `GOMAXPROCS=24`,
   `EvalCached` accounts for **60.21%** of all blocking in `hits` and **87.74%**
   in `mixed`, of which 99.8-100% is `sync.(*Mutex).Lock`. Both shares are
   bounded by construction and so immune to the ramp defect above.

Scaling from 2→8 in `hits` is 1.92× against an available 4×, so the ceiling is
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
  where `hits` contention is negligible (0.03%). A gold-set concurrent cell,
  once the tiers loop allows one, would measure at the level least able to see
  this — worth carrying into that cell's design.
- **CI never runs this benchmark.** `.github/workflows/ci.yml:22` is
  `go test -race -count=1 ./...` with no `-bench`, so adding it costs CI
  nothing.

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
