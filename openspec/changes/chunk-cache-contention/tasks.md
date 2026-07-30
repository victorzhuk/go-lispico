# Tasks — chunk-cache-contention

## 0. Gate (blocking — nothing below starts until this fires)

- [ ] 0.1 Add a concurrent gold-set benchmark cell: N goroutines calling
      `EvalCached` concurrently against one shared Engine, over a mix of
      cache-hit and cache-miss sources representative of the existing
      corpus. Commit a tier for it in `internal/perfgate/tiers.json` per
      ADR 0008's concurrent-tier definition (throughput, race-clean).
- [ ] 0.2 Measure `bytecodeEvaluator.mu` contention with `-mutexprofile` and
      `-blockprofile` at `GOMAXPROCS=2`, `8`, and `24` on this workstation
      (24 logical CPUs). Record flat/cumulative mutex-wait share at each
      level.
- [ ] 0.3 Gate fires iff contention is a material share. `-mutexprofile`
      reports contention as delay-per-call-site, not a wall-time percentage
      directly — convert by dividing the profile's total mutex-wait duration
      by the benchmark's total wall-clock duration for the same run, at each
      `GOMAXPROCS` level. Threshold: that ratio exceeds 5% at `GOMAXPROCS=8`
      or higher. Record the actual ratios either way. If it does not fire,
      record the measurement in `design.md` and close this change as
      not-needed; do not implement sharding speculatively.

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
