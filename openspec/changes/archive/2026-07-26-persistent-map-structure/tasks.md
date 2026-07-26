## 1. Pin the current cost first

- [x] 1.1 Added `BenchmarkHashMap_AssocChain` and `BenchmarkHashMap_SetBuild` to
      `core/bench_test.go` before the rewrite (`ecc484e`).
- [x] 1.2 Added `BenchmarkHashMap_GetLarge`. It builds through `Assoc`, not
      `Set` — a `Set`-built map stays in the staging form and would measure the
      wrong representation.
- [x] 1.3 Baseline captured at `GOMAXPROCS=2`, `-benchtime=200ms`, `-count=6`,
      `TMPDIR` outside `/tmp`. `B/op` deterministic at ±0%:
      AssocChain 829KiB / 82.88MiB / 8.465GiB — ~100× per 10× of n, quadratic.
      SetBuild 25.6KiB / 432KiB / 3.404MiB — linear.
- [x] 1.4 Ledger ceiling before: the assoc-chain loop completes at n=1440 and
      fails at n=1450 with `ResourceLimitError: allocation limit 67108864 bytes
      exceeded`, identically under both execution modes.

## 2. The trie

- [x] 2.1 `hamtNode` with disjoint `dataMap`/`nodeMap` and compacted
      `entries`/`children`, reusing `vecBits`/`vecBranch`.
- [x] 2.2 Fixed-seed 64-bit FNV-1a folded to 32 bits, with the WHY comment on
      why `hash/maphash` is wrong here.
- [x] 2.3 Lookup, insert and remove with path copying; descent guarded on
      `shift >= 32`.
- [x] 2.4 Collision nodes marked `dataMap == 0 && nodeMap == 0 && len(entries) > 0`.
- [x] 2.5 `Dissoc` drops emptied children; single-entry inlining left out.
- [x] 2.6 `count` maintained on the large form. `MeterHashMapHeaderBytes` left
      at 32. Added `MeterTrieChildBytes = 8`: a child slot is a bare pointer,
      not an interface value, and billing it at `MeterValueSlotBytes` was an
      over-count. Verified this moved no threshold — the measured ceiling was
      unchanged at the sampled sizes before and after.

## 3. Keep the package boundary still

- [x] 3.1 `sortedEntries()`, `eachRaw()` and `getByHashKey()` kept their exact
      signatures; `core/depth.go` needed no edit.
- [x] 3.2 `sortedEntries()` still sorts. Iteration order unchanged.
- [x] 3.3 Small form and promotion untouched: sorted slice at ≤8, promotion on
      the 9th distinct key, still one-way.

## 4. Charging

- [x] 4.1 `assoc` and `dissoc` charge the bytes the call allocated plus the
      inserted value's deep size. `conj`'s map branch follows the same rule.
- [x] 4.2 The scope-exclusion comment at `collections.go:440-448` is deleted,
      not reworded.
- [x] 4.3 `ValueShallowBytes(*HashMap)` untouched.
- [x] 4.4 **Not foreseen, and the larger win:** `assoc` charged through
      `chargeCollectionResult`, whose `CheckConstructionDepth` calls `Each` →
      `sortedEntries()` — allocating and sorting every entry on every call.
      That is O(n log n) per assoc, and it only became visible once the ledger
      stopped failing first: a 20k chain hit the 30s context deadline instead.
      Switched to `chargeConsResult`, the guarded form `conj` already used, so
      the walk runs only when an inserted element could actually nest. The
      whole ceiling probe went from 122s to 0.287s.

## 5. Prove it

- [x] 5.1 `TestHashMap_TrieMatchesOracle` — 20000 mixed Assoc/Dissoc/Get steps
      against a plain Go map oracle, checking values, `Len`, absence, and
      `sortedEntries()` ordering.
- [x] 5.2 `TestHashMap_HashCollisions` — keys found to collide in all 32 bits
      by construction, asserting separate retrieval, `Len`, sibling survival
      after `Dissoc`, and receiver immutability.
- [x] 5.3 `TestHashMap_LargeFormPrintsIndependentOfBuildOrder` — two large maps
      with equal pairs built in opposite orders print identically and are Equal.
- [x] 5.4 `TestHashMap_PromotionBoundary` retargeted at the new storage field.
      It still asserts promotion on the 9th distinct key, no receiver mutation,
      and no demotion on `Dissoc`.
- [x] 5.5 `TestAssocMonotonic_ChargesPerCallHonestly` retargeted: the per-call
      charge must stay bounded as the map grows, and the total must stay far
      below the quadratic sum. Its scope-exclusion comment is deleted.
- [x] 5.6 `TestMapAccumulation20k` in `runtime/`, both modes, beside the
      sequence gate. Ceiling after: completes at n=40000, fails at n=45000 —
      up from 1440/1450, about 28×. Not unbounded, and it cannot be: building
      n keys one at a time copies a path per call, so O(n log n) is inherent.
      The spec scenario was corrected from 100,000 to 20,000 to state what is
      true rather than what would have been nicer.

## 6. Measure, and report honestly

- [x] 6.1 AssocChain went flat. sec/op −88.0% / −98.1% / −99.86%
      (4.407s → 6.10ms at n=10000); B/op −77.8% / −97.8% / −99.82%
      (8.465GiB → 15.4MiB). B/op now grows ~10× per 10× of n — linear.
- [x] 6.2 **SetBuild: the regression was real, and was fixed rather than
      absorbed.** Path-copying `Set` cost +293–321% time, +388–3721% allocs,
      and ~5× end-to-end on `json/decode` of a 4000-key object
      (820µs → 4.32ms). Keeping a Go map as a build-only staging form, which
      `Assoc` converts to a trie once on first use, returned `SetBuild` to
      byte-identical allocations (14 / 1513 / 19570, ±0%) and decode to its
      baseline range.
- [x] 6.3 GetLarge: predicted slower, measured ~2.6× **faster** — ~21ns against
      ~54ns at n=10000. `hashKey` embeds a string, so Go's map hashing covers
      the whole struct while FNV over a small int does not. Recorded because
      the prediction was wrong.
- [x] 6.4 `BenchmarkHashMap_ScanVsMap` unchanged; `hashMapSmallLimit` not moved.
- [x] 6.5 Stated: no gold-set cell reaches the large form, so perfgate is a
      collateral-damage check here and not evidence the change worked.

## 7. Verify

- [x] 7.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      all clean — 0 issues.
- [x] 7.2 `go test ./... -count=1` 2428 passed; `go test ./... -race` 2430
      passed. `TestDecodeHashMap_Scaling` passes, and was measured directly
      rather than assumed: ratio 1.35–2.30 against a 3.0 bound, absolute times
      back at baseline.
- [x] 7.3 Crossval `TestVMVsTreeWalker` 218 passed.
- [x] 7.4 `go test ./internal/goldset/ -count=1` 27 passed.
- [x] 7.5 **Perfgate: allocation-neutral, latency inconclusive.** Every
      `GoldsetParse` cell is byte-identical (p=1.000) and every cell's
      allocs/op is unchanged (p=1.000); bytes geomean −0.00%. The gate still
      reports 2 latency FAILs out of 26 at `-benchtime=400ms -count=10`, but
      they are noise: the failing set moves between runs (`rule-load` −17.65%
      and `route-decision` in one, `queue-promote` +9.16% and `twice-macro`
      −6.43% in the next), several are *improvements* tripping a two-sided
      tolerance, and a focused re-measure of `rule-load` at n=10 returned
      `~ (p=0.853)`. Filed as a gate-configuration follow-up: the
      `GoldsetParse/*` cells added in `4c3f852` are microbenchmarks in the
      single-digit µs and are too noisy for a two-sided ±5% latency gate on
      this machine. Not adjusted here — loosening a gate to pass one's own
      change is the wrong direction.
