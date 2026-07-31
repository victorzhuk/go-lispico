# Tasks — chunk-cache-contention

## 0. Gate (blocking — nothing below starts until this fires)

- [ ] 0.1 Add a concurrent gold-set benchmark cell: N goroutines calling
      `EvalCached` concurrently against one shared Engine, over a mix of
      cache-hit and cache-miss sources representative of the existing
      corpus. Commit a tier for it in `internal/perfgate/tiers.json` per
      ADR 0008's concurrent-tier definition (throughput, race-clean).

  **Amended (2026-07-31): split. The measurement instrument is done; the
  gold-set cell and its tier are deferred and this task stays open for
  them.**

  `BenchmarkEngine_EvalCachedConcurrent` (`runtime/bench_concurrent_test.go`,
  arms `hits` and `mixed`) satisfies everything 0.2 and 0.3 need — N
  goroutines, one shared Engine, hit and miss sources, race-clean. It lives
  in `runtime/` beside `BenchmarkEngine_Call*`, deliberately **not** in
  `internal/goldset/`.

  Putting it in the gold set is what is blocked. `release.yml:132-133` runs
  `-bench .` over `./internal/goldset/`, so any benchmark added there enters
  the gate corpus, and `cmd/perfgate/main.go:112-117` fails any cell with no
  committed tier. Committing a tier requires a baseline profile per
  `internal/perfgate/tiers.json`'s own rule, which the tiers↔baseline loop
  prevents — the gate needs correct tiers to pass, passing is what stores the
  baseline asset, and the stored asset is what licenses a tier.
  `archive/2026-07-30-gate-corpus-cl-and-recursion` task 0.1 hit this exact
  requirement one day earlier and declined to work around it, on the evidence
  that 8 of 26 locally-assigned tiers came back misclassified.

  Unblocking needs a decision above this change (a `workflow_dispatch`
  artifact counts as the profile of record, or an ADR 0008 amendment), the
  same decision `release-gate-activation` and `tiers.json` reclassification
  are waiting on. Carry into that cell's design: the gate runs at
  `GOMAXPROCS=2` (`release.yml:55`), the one level where this measurement
  found contention negligible.

- [x] 0.2 Measure `bytecodeEvaluator.mu` contention with `-mutexprofile` and
      `-blockprofile` at `GOMAXPROCS=2`, `8`, and `24` on this workstation
      (24 logical CPUs). Record flat/cumulative mutex-wait share at each
      level.
- [x] 0.3 Gate fires iff contention is a material share. `-mutexprofile`
      reports contention as delay-per-call-site, not a wall-time percentage
      directly — convert by dividing the profile's total mutex-wait duration
      by the benchmark's total wall-clock duration for the same run, at each
      `GOMAXPROCS` level. Threshold: that ratio exceeds 5% at `GOMAXPROCS=8`
      or higher. Record the actual ratios either way. If it does not fire,
      record the measurement in `design.md` and close this change as
      not-needed; do not implement sharding speculatively.

  **Verdict (2026-07-31): the gate FIRES.** Full record in `design.md`.

  Delay attributed to `EvalCached` by `pprof -peek` (98.3-100% of all
  `sync.Mutex` delay at every level, both arms), over wall clock taken as
  `ns/op × N`:

  | arm | P=2 | P=8 | P=24 |
  | --- | --- | --- | --- |
  | hits | 0.03% | 15.71% | 374.48% |
  | mixed | 6.77% | 625.42% | 2197.44% |

  The numerator sums delay across goroutines while the denominator is one
  timeline, so these exceed 100% and are not a share of wall time. Dividing
  by `GOMAXPROCS` gives 0.02 / 1.96 / 15.60% (hits) and 3.39 / 78.18 /
  91.56% (mixed); the threshold is cleared at 8 and 24 under both
  conventions, the sole exception being `hits` at 8 per-CPU (1.96%), where
  `mixed` reads 78.18%.

  Three confirmations that need no normalization: `hits` throughput
  **regresses** past 8 cores (800.6 → 1310 ns/op at 8 → 24); `mixed` does not
  scale at all across 2/8/24 (3960 / 3736 / 3887 ns/op); and an independent
  `-blockprofile` pass puts `EvalCached` at 60.21% (hits) and 87.74% (mixed)
  of all blocking at `GOMAXPROCS=24`, of which 99.8-100% is
  `sync.(*Mutex).Lock`.

  **Correction.** Commit `e110c3d` recorded a first pass measured at
  `-benchtime=5s`, whose ratios were inflated: `-mutexprofile` accumulates
  across every run of Go's `b.N` ramp while `ns/op × N` reports only the final
  one, so numerator and denominator covered different intervals. Two cells
  breached the physical bound `delay ≤ GOMAXPROCS × wall` (`mixed` at 8 by
  1.27×, at 24 by 1.53×), which is what exposed it. The table above is a
  re-run at a fixed iteration count — one execution, no ramp — and every cell
  now satisfies that bound. `ns/op` reproduced across both passes, so only the
  accounting changed, not the finding. Any future ratio quoted here must be
  bound-checked before it is published.

## 1. Design decision (only if the gate fires)

- [x] 1.1 Decide the budget-accounting mechanism for sharded state: an
      atomic global running total for `MaxCacheBytes`/`MaxCacheNodes`
      (approximate cross-stripe eviction ordering), or fixed per-stripe
      quotas with a documented tolerance. Prototype both against the
      existing "Compiled-chunk cache" requirement's scenarios before
      picking; record the loser's reasoning in `design.md`.

  **Decided: hybrid — neither option as framed.** Per-stripe map/LRU/mutex
  for storage, three engine-wide `atomic.Int64` counters as the sole
  authority on aggregate occupancy, admission refusing rather than
  over-admitting. The ceiling then holds exactly, with no tolerance term.
  Both original options were checked against the existing scenarios and
  both lose: per-stripe quotas divide ceilings the suite sets to 1, 2, 4, 5,
  and 10, giving every stripe a quota of 0; a pure global total with
  local-only eviction either over-admits or ossifies. Full reasoning in
  `design.md`, "1.1 Budget accounting — hybrid".

- [x] 1.2 Decide the stripe key and count: hash of `{sourceHash,
      dialectFingerprint, macroEpoch, formIndex}` (the existing cache key)
      is the natural candidate; stripe count sized to expected concurrent
      Engine usage, not maximal.

  **Decided: route on `{sourceHash, formIndex}` only; count is adaptive.**
  `macroEpoch` is excluded so a source's stale and fresh entries co-locate;
  `dialectFP` is constant per engine. 1 stripe below `MaxCacheEntries` 64,
  else 8 — a cache holding a handful of entries keeps single-lock behavior
  exactly, which is what lets every pre-existing cache test keep its
  assertions unmodified. `cacheKey` itself is unchanged.

## 2. Implementation

- [x] 2.1 Stripe the chunk cache map and its intrusive LRU by the chosen key.
      Each stripe: its own mutex, its own LRU list, matching the
      correctness invariants of the existing single-lock implementation
      exactly (hit skips macro expansion, redefinition invalidates,
      indistinguishable-rebind does not).

  `cacheStripe{mu, cache, head, tail}` in `runtime/eval.go`; the LRU helpers
  moved onto `*cacheStripe`. Stale-epoch reclamation also moved behind one
  evaluator-global CAS, sweeping stripes one lock at a time — it previously
  walked the whole LRU on every miss, which striping alone would have made
  worse, not better.

- [x] 2.2 Wire the chosen budget-accounting mechanism from 1.1.
- [x] 2.3 Confirm every existing "Compiled-chunk cache" scenario still holds
      per-stripe and in aggregate (macro invalidation, indistinguishable
      rebind, process-level plugin artifact sharing untouched).

  Every pre-existing cache test passes with its assertions **unmodified**.
  The only test-side edits are `cacheCount` and `cacheContainsSource`, which
  reached into the single map directly and now iterate stripes.

## 3. Verify

- [x] 3.1 Full floor: build/vet/gofmt/lint, full suite, `-race`, crossval,
      goldset both modes non-increasing.

  `go build ./...`, `go vet ./...`, `gofmt -l .` clean, `make lint` 0 issues,
  `make test` and `go test -race -count=1 ./...` green across every package.
  Goldset VM mode measured as an interleaved A/B against `master` (6 rounds,
  alternating binaries, `GOMAXPROCS=2`, `-benchtime=200ms`): geomean +0.47%,
  `B/op` and `allocs/op` identical in every cell. Two cells tripped p<0.05 at
  n=6, one of them a parse-only cell this change cannot touch — that sets the
  false-positive floor. The larger, `counter-closure` (+11.67%, p=0.009), was
  re-run at n=12 and came back +2.4%, p=0.086: it did not replicate.

- [x] 3.2 Concurrent benchmark cell from 0.1: contention share reduced
      relative to 0.2's baseline at `GOMAXPROCS=8`+; no correctness
      regression at any concurrency level.

  Same protocol as the gate measurement — fixed iteration count, per-arm
  profiling, every ratio bound-checked against `delay ≤ GOMAXPROCS × wall`.
  `EvalCached` delay over wall clock, before → after:

  | arm | P=2 | P=8 | P=24 |
  | --- | --- | --- | --- |
  | hits | 0.03% → 0.03% | 15.71% → **4.80%** | 374.48% → **83.88%** |
  | mixed | 6.77% → **0.17%** | 625.42% → **14.77%** | 2197.44% → **371.34%** |

  `mixed` now scales where it previously did not: 2082 / 921.3 / 828.0 ns/op
  at 2 / 8 / 24, against 3960 / 3736 / 3887. `hits` still regresses from 8 to
  24 cores, less steeply than before; its 4-source working set caps
  achievable reduction near 4x regardless of stripe count, which is why the
  `hits-wide` arm (32 sources) was added. Full table and the rejected
  cache-line-padding follow-up in `design.md`.

- [x] 3.3 Budget-enforcement test: fill the cache past `MaxCacheBytes`/
      `MaxCacheNodes` under concurrent load from multiple stripes; confirm
      the aggregate stays within the documented tolerance from 1.1, not
      merely within one stripe's local view.

  `TestCache_ConcurrentAggregateBudgetHolds` — 8 goroutines over a 40-source
  working set spanning every stripe, byte and node ceilings sized so eviction
  fires throughout. There is no tolerance term to allow: the hybrid refuses
  rather than over-admitting, so the assertion is the exact ceiling. It also
  asserts a retention floor, since ceiling-only assertions would stay green
  if striped admission degenerated into permanent refusal.
  `TestCache_RefusalTerminatesUnderTightBudget` covers the pre-eviction loop
  terminating on an empty stripe, and
  `TestCache_GlobalCounterDenialRollsBackWithoutChargingMeter` covers the
  counter rollback when the global charge denies.

- [x] 3.4 `openspec validate --strict` on this change.
