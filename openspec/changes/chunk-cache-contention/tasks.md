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

  Delay attributed to `EvalCached` by `pprof -peek` (98.8-99.9% of all
  `sync.Mutex` delay at every level, both arms), over wall clock taken as
  `ns/op × N` — the timed loop only:

  | arm | P=2 | P=8 | P=24 |
  | --- | --- | --- | --- |
  | hits | 0.05% | 19.52% | 485.71% |
  | mixed | 11.53% | 1014.35% | 3667.15% |

  The numerator sums delay across goroutines while the denominator is one
  timeline, so these exceed 100% and are not a share of wall time. Dividing
  by `GOMAXPROCS` gives 0.02 / 2.44 / 20.24% (hits) and 5.77 / 126.79 /
  152.80% (mixed); the threshold is cleared at 8 and 24 under both
  conventions, the sole exception being `hits` at 8 per-CPU (2.44%), where
  `mixed` reads 126.79%.

  Three confirmations that need no normalization: `hits` throughput
  **regresses** past 8 cores (822.4 → 1302 ns/op at 8 → 24); `mixed` does not
  scale at all across 2/8/24 (3861 / 3729 / 3886 ns/op); and an independent
  `-blockprofile` pass puts `EvalCached` at 60.21% (hits) and 87.74% (mixed)
  of all blocking at `GOMAXPROCS=24`, of which 99.8-100% is
  `sync.(*Mutex).Lock`.

## 1. Design decision (only if the gate fires)

- [ ] 1.1 Decide the budget-accounting mechanism for sharded state: an
      atomic global running total for `MaxCacheBytes`/`MaxCacheNodes`
      (approximate cross-stripe eviction ordering), or fixed per-stripe
      quotas with a documented tolerance. Prototype both against the
      existing "Compiled-chunk cache" requirement's scenarios before
      picking; record the loser's reasoning in `design.md`.
- [ ] 1.2 Decide the stripe key and count: hash of `{sourceHash,
      dialectFingerprint, macroEpoch, formIndex}` (the existing cache key)
      is the natural candidate; stripe count sized to expected concurrent
      Engine usage, not maximal.

## 2. Implementation

- [ ] 2.1 Stripe the chunk cache map and its intrusive LRU by the chosen key.
      Each stripe: its own mutex, its own LRU list, matching the
      correctness invariants of the existing single-lock implementation
      exactly (hit skips macro expansion, redefinition invalidates,
      indistinguishable-rebind does not).
- [ ] 2.2 Wire the chosen budget-accounting mechanism from 1.1.
- [ ] 2.3 Confirm every existing "Compiled-chunk cache" scenario still holds
      per-stripe and in aggregate (macro invalidation, indistinguishable
      rebind, process-level plugin artifact sharing untouched).

## 3. Verify

- [ ] 3.1 Full floor: build/vet/gofmt/lint, full suite, `-race`, crossval,
      goldset both modes non-increasing.
- [ ] 3.2 Concurrent benchmark cell from 0.1: contention share reduced
      relative to 0.2's baseline at `GOMAXPROCS=8`+; no correctness
      regression at any concurrency level.
- [ ] 3.3 Budget-enforcement test: fill the cache past `MaxCacheBytes`/
      `MaxCacheNodes` under concurrent load from multiple stripes; confirm
      the aggregate stays within the documented tolerance from 1.1, not
      merely within one stripe's local view.
- [ ] 3.4 `openspec validate --strict` on this change.
