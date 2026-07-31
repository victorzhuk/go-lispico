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

## What section 1 decided

The gate firing authorized sections 1-3, not a particular design. One
measurement constrained both answers: the profile shows the probe alone
(`hits`, one lock acquisition per call) already saturating, because a hit calls
`moveCacheEntryToHeadLocked` — a write under what reads as a read. A design that
only shortened the admit path would not have addressed the dominant cost.

### 1.1 Budget accounting — hybrid

Per-stripe map, LRU, and mutex for storage; three engine-wide `atomic.Int64`
counters (entries, bytes, nodes) as the sole authority on aggregate occupancy.
Admission, entirely inside the target stripe's own lock:

1. `cacheFitsAlone` against the **undivided** `MaxCacheBytes`/`MaxCacheNodes`/
   `MaxCacheEntries` — never a per-stripe fraction.
2. Bounded local pre-eviction: `for s.tail != nil && cacheBudgetExceeded(chunk)`.
3. CAS-charge the three counters (`used+n > max` → fail), with rollback across
   the three.
4. `engineMeter.ChargeRetained`, rolling the counters back on denial.
5. Insert and link at the stripe's LRU head.

Refusal is the only failure mode; nothing is ever inserted speculatively. The
configured ceiling therefore bounds the total across all stripes **exactly**, with
no tolerance term — which is what the "A sharded cache enforces its budget in
aggregate" scenario requires.

**Why the two proposed options lost.** *Fixed per-stripe quotas*
(`floor(Max/N)`) bound the aggregate trivially but divide the configured number:
the existing suite runs `MaxCacheEntries` of 1, 2, 4, 5, and 10 and
`MaxCacheBytes` set to exactly one chunk's size, so every stripe gets a quota of
0 and the cache goes silently dead — two of those tests would have passed
vacuously, since their assertions are upper bounds. A "minimum 1 per stripe"
guard fixes the dead cache but then caps the aggregate at N, an 8x ceiling
violation in exactly the configuration the aggregate scenario exists to police.
*A pure atomic global total with local-only eviction* cannot free enough when the
admitting stripe's LRU is short, so it either over-admits (violating the
scenario) or refuses — and refusing without the undivided fitness check of step 1
ossifies: a stripe whose LRU is exhausted while the global total is full can
never displace an entry parked elsewhere.

**Cross-stripe eviction is deliberately absent** — it would require a lock
ordering across stripes. The cost is that eviction picks the admitting stripe's
LRU victim rather than the global one. That only becomes observable at ceilings
too small to hold a working set, which is what 1.2's adaptive rule handles.

### 1.2 Stripe key and count

Routing uses a non-allocating reduction of `{sourceHash, formIndex}` only:
`(binary.LittleEndian.Uint64(h[:8]) ^ uint64(formIndex)) & uint64(n-1)`.
`macroEpoch` is excluded deliberately — including it would scatter a source's
stale-epoch entry into a different stripe from its fresh replacement, so a miss
could not find its own stale sibling co-located in the stripe it is about to
write. `dialectFP` is constant per engine and contributes no entropy. `cacheKey`
itself is unchanged and remains the exact map lookup key: routing and key
equality are separate functions.

The count is **adaptive**: 1 stripe below `MaxCacheEntries` 64, else 8. An engine
whose cache holds a handful of entries gains nothing from partitioning and would
only lose global LRU victim identity, so it keeps the single-lock behavior
exactly. This is what lets every pre-existing cache test keep its assertions
unmodified rather than being pinned to one stripe by a test-only knob. An
unexported `withCacheStripes(n)` option overrides the rule so a benchmark can
compare stripe counts in one binary.

### Stale-epoch reclamation

`flushCacheEpochLocked` previously walked the entire LRU on **every miss**. It is
now gated by one evaluator-global `lastFlushedEpoch` CAS: the winner sweeps every
stripe sequentially, never holding two stripe locks at once, and losers skip
straight to their own admit. Cost drops from O(entries) per miss to O(entries)
per macro-epoch bump.

This is safe because `macroEpoch` is part of `cacheKey`, so a probe for the
current epoch can never return a stale-epoch entry regardless of whether the
sweep has run. Reclamation is a memory concern, not a hit/miss correctness one,
and the capacity ceiling bounds occupancy independently. The sweep's predicate is
`macroEpoch < epoch`, not `!= epoch`: a sweep for an older epoch can still be
walking stripes when a redefinition bumps past it, and `!=` would let that
in-flight sweep delete an entry a newer epoch had just admitted into a stripe it
had not yet reached.

### An implementation note

Stripe maps are allocated lazily on each stripe's first admit. Allocating all
eight at construction cost 8 allocations on every `New`, which
`TestNew_NilLoggerAllocsBudget` caught (17 → 25). A nil Go map reads, deletes,
and ranges safely, so the deferral changes only when the allocation happens.

## Results after striping

Same protocol as the gate measurement: fixed iteration count (`-benchtime=Nx`,
one execution, no `b.N` ramp), each arm profiled in its own invocation, delay
attributed to `EvalCached` by `pprof -peek 'sync\.\(\*Mutex\)\.Unlock$'`
(96.8-100% of all `sync.Mutex` delay, `sync.(*Pool).pinSlow` the only other
contributor), `wall` = `ns/op × N`, every cell bound-checked against
`delay ≤ GOMAXPROCS × wall`.

| arm | P | ns/op before | ns/op after | delay/wall before | delay/wall after | bound |
| --- | --- | --- | --- | --- | --- | --- |
| hits | 2 | 1535.0 | 1554.0 | 0.03% | 0.03% | ok |
| hits | 8 | 800.6 | 704.3 | 15.71% | **4.80%** | ok |
| hits | 24 | 1310.0 | 1192.0 | 374.48% | **83.88%** | ok |
| mixed | 2 | 3960.0 | 2082.0 | 6.77% | **0.17%** | ok |
| mixed | 8 | 3736.0 | 921.3 | 625.42% | **14.77%** | ok |
| mixed | 24 | 3887.0 | 828.0 | 2197.44% | **371.34%** | ok |

Task 3.2's bar — contention share reduced at `GOMAXPROCS=8` and above — is met in
every cell: 3.3x and 4.5x on `hits`, 42x and 5.9x on `mixed`.

Two results need no normalization at all. `mixed` **now scales**: 2082 / 921.3 /
828.0 ns/op at 2 / 8 / 24, against 3960 / 3736 / 3887 before, where twelve times
the cores bought nothing. And most of `mixed`'s gain at `GOMAXPROCS=2` — where
striping cannot help, since contention there was already negligible — is the
epoch-flush gate removing an O(entries) walk from every miss, not the partitioning.

`hits` still regresses from 8 to 24 cores (704.3 → 1192 ns/op), less steeply than
before (800.6 → 1310). Its 4-source working set is the near-term limit: four keys
over eight stripes caps achievable reduction near 4x by occupancy alone, well
short of 8x. The `hits-wide` arm (32 sources) exists to measure past that ceiling.

**A rejected follow-up, recorded so it is not re-proposed.** `cacheStripe` is 32
bytes, so two stripes share a 64-byte cache line and the eight partitions form
four false-sharing pairs. Padding the struct to a full line was measured: `hits`
moved -1.5% at `GOMAXPROCS=8` and -1.9% at 24, while `mixed` at 24 went **+10%
worse**. That spread is inside this workstation's known drift, so the change is
not decidable here and does not earn its memory. It stays available if a
quieter machine ever shows the pairing matters.
