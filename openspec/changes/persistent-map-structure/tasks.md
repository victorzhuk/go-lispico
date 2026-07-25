## 1. Pin the current cost first

- [ ] 1.1 Add `BenchmarkHashMap_AssocChain` (the defect: chained `Assoc` at
      n=100/1000/10000) and `BenchmarkHashMap_SetBuild` (the exposure: `Set`
      builder loop at the same sizes) to `core/bench_test.go`. Both go in
      **before** the rewrite — a benchmark added afterwards has nothing to
      compare against.
- [ ] 1.2 Add `BenchmarkHashMap_GetLarge` — large-form `Get` is expected to get
      slower and nothing currently measures it. `BenchmarkHashMap_ScanVsMap`
      pins the small-form boundary and does not cover this.
- [ ] 1.3 Capture the baseline: `GOMAXPROCS=2`, `-benchtime=200ms`, `-count=10`,
      `TMPDIR` outside `/tmp` (a quota failure at the link step reads like a test
      failure and is not one). Record `allocs/op` and `B/op` — those are
      deterministic; timing on this box is not.
- [ ] 1.4 Record the ledger ceiling as it stands: the assoc-chain loop fails
      between n=1440 and n=1450 under both modes. This is the number the change
      is judged on.

## 2. The trie

- [ ] 2.1 Add `hamtNode` per `design.md` — `dataMap`/`nodeMap` disjoint,
      `entries`/`children` compacted. Reuse `vecBits`/`vecBranch`; do not
      introduce parallel constants for the same two numbers.
- [ ] 2.2 Fixed-seed 64-bit FNV-1a over `(typ, num, str)`, folded to 32 bits as
      `uint32(h ^ (h >> 32))`. Not `hash/maphash` — its per-process random seed
      would break the determinism invariant. State that in a WHY comment, since
      the next reader will reach for `maphash`.
- [ ] 2.3 Lookup, insert and remove with path copying. Guard descent on
      `shift >= 32`: `h >> 35` on a `uint32` is 0 in Go, so an unguarded descent
      does not terminate for colliding keys.
- [ ] 2.4 Collision nodes, marked `dataMap == 0 && nodeMap == 0 && len(entries) > 0`.
      Comment why that combination cannot occur for a normal node.
- [ ] 2.5 `Dissoc` drops emptied children. Single-entry inlining is deliberately
      out of scope (`design.md`) — do not add it.
- [ ] 2.6 `count int` on `HashMap`, maintained by `Assoc`/`Dissoc`/`Set`. Leave
      `MeterHashMapHeaderBytes` at 32; it is an approximation table, not a
      `sizeof` assertion.

## 3. Keep the package boundary still

- [ ] 3.1 `sortedEntries()`, `eachRaw(func(entry))` and `getByHashKey(hashKey)`
      keep their exact signatures — `core/depth.go:141,205,209` call them and
      must not need editing.
- [ ] 3.2 `sortedEntries()` keeps sorting, at the cost it pays today. Iteration
      order is unchanged; this change does not adopt hash order.
- [ ] 3.3 Small form and promotion untouched: still a sorted slice at ≤8, still
      promoting on the 9th distinct key, still one-way.

## 4. Charging

- [ ] 4.1 `assoc` charges the bytes the insert actually allocated plus
      `ValueDeepBytes` of the inserted value, replacing
      `HashMapShallowBytes(result.Len())`. Same rule for `dissoc`.
- [ ] 4.2 Delete the scope-exclusion comment at
      `plugins/stdlib/collections.go:440-448` — the exclusion is what this change
      removes. Do not reword it.
- [ ] 4.3 Leave `ValueShallowBytes(*HashMap)` (`core/metering.go:565`) alone; it
      is the apply-site fallback and `assoc` marks itself charged through
      `ChargeGoFuncResultBytes`.

## 5. Prove it

- [ ] 5.1 Property test against the current map form as oracle: random
      `Assoc`/`Dissoc`/`Get` sequences agree on every key, `Len`, and
      `sortedEntries()` order.
- [ ] 5.2 Adversarial collision test — keys colliding **by construction**, not by
      luck. Assert each resolves to its own value, `Dissoc` of one leaves the
      others, and `Len` counts them separately.
- [ ] 5.3 Determinism test: two independently built equal maps print identically,
      and repeated iteration of one map is stable.
- [ ] 5.4 `TestHashMap_PromotionBoundary` retargeted from the `m` field to the new
      storage field. It must still assert promotion at the 9th distinct key,
      no mutation of the receiver, and no demotion on `Dissoc`. Weakening any of
      those to make it pass is a failed task, not a passing one.
- [ ] 5.5 `TestAssocMonotonic_ChargesPerCallHonestly` retargeted to assert a
      **linear** total, its scope-exclusion comment deleted. Keep the per-call
      exactness assertion; only the expected quantity changes.
- [ ] 5.6 The ledger ceiling: an assoc chain well past 1450 — 100k, matching the
      sequence requirement's shape — completes under default limits in both
      modes. Report the charged total against 67108864.

## 6. Measure, and report honestly

- [ ] 6.1 Paired capture after, same parameters as 1.3.
      `BenchmarkHashMap_AssocChain` must go flat rather than linear in n.
- [ ] 6.2 `BenchmarkHashMap_SetBuild` delta reported as a number, not a
      characterization. This is the regression the change pays for; if it is
      material, say so and file the internal-builder follow-up from `design.md`
      rather than absorbing it silently.
- [ ] 6.3 `BenchmarkHashMap_GetLarge` delta reported. A slower large-form `Get`
      is expected and acceptable; an unmeasured one is not.
- [ ] 6.4 `BenchmarkHashMap_ScanVsMap` unchanged — it pins `hashMapSmallLimit`
      and this change must not move it.
- [ ] 6.5 State plainly in the report that no gold-set cell exercises the large
      form, so the perfgate verdict is a collateral-damage check and not evidence
      the change worked.

## 7. Verify

- [ ] 7.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `make lint` clean.
- [ ] 7.2 `go test ./... -count=1` full suite green; `go test ./... -race` with
      `TMPDIR` set. `TestDecodeHashMap_Scaling` is a known pre-existing
      wall-clock flake under race load and is filed separately — but this change
      touches `json/decode`'s builder path through `Set`, so confirm it in
      isolation rather than assuming the existing flake explains a failure.
- [ ] 7.3 Crossval `TestVMVsTreeWalker` green.
- [ ] 7.4 `go test ./internal/goldset/ -count=1` green, both modes.
- [ ] 7.5 `cmd/perfgate` verdict recorded, with the 6.5 caveat attached.
