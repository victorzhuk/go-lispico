## 0. Baseline

- [x] 0.1 Record the defect as evidence at this change's base commit: for n = 9, 100 and 1000, the reduction units `equalsBounded` charges comparing two unequal builder-form maps, over at least three repeats each, showing the spread. Confirm the spread appears within a single process, and confirm both the equal-maps case and the trie-form receiver case are already stable, so the fix is scoped to the arm that actually moves.

  Measured at the base arm by running the committed test against it, 8 repeats per size,
  one process. `valueMismatch` — the receiver in builder form, one value differing:

  | n | reductions per repeat | want |
  | --- | --- | --- |
  | 9 | `[4 8 10 6 2 5 2 8]` | 10 |
  | 100 | `[47 14 7 31 14 16 2 89]` | 101 |
  | 1000 | `[861 364 996 267 697 412 420 185]` | 1001 |

  A second run of the same sizes gave a different spread again (`[5 10 10 2 7 9 2 9]`,
  `[61 36 4 60 18 28 3 28]`, `[827 277 137 24 75 259 886 759]`), which is the point: the
  count is not a property of the pair. 6, 7 and 8 distinct values appeared across the 8
  repeats at n = 9, 100 and 1000, so 8 repeats expose the spread reliably at every size.

  Controls, both already stable at base, which scopes the defect to the arm that moves:
  `equalMaps` charged `n+1` on every repeat (10 / 101 / 1001), and `trieReceiver` charged
  a constant 9 / 79 / 760. The trie row is the one worth keeping: it never varies, because
  `hamtNode.each` walks in a fixed order and `hashOfKey` is seeded from fixed FNV
  constants, yet it does not equal the builder receiver's count for the same contents.
  Form-dependence is therefore a separate defect from the randomization, and a fix that
  only de-randomized the builder branch would have left it standing — which is why the
  builder-form-only remedy was rejected in 2.1.

  `disjointKeys` charged a deterministic 1 at every size: the first absent key clears the
  answer and returns before any recursion, so nothing further is billed. That constant is
  what makes the red deterministic rather than probabilistic.

## 1. Failing contract

- [x] 1.1 Use existing-service-strict testing. Add `TestEqualsBounded_ReductionChargeIgnoresIterationOrder` in `core`: build two unequal maps above `hashMapSmallLimit` with the receiver left in builder form (`large.m != nil`, `large.root == nil`), compare them under a `BuiltinWorkBudget` repeatedly, and require every charged reduction count to be identical. Assert exact equality, not a bound. Verify it fails at base, and report how many repeats expose the spread reliably at each size so the committed test is not itself flaky.

  The test asserts the literal `n+1`, not self-consistency: `hashOfKey` is seeded from
  fixed FNV constants (`core/types.go`), so the count holds across processes, which is
  what Scenario A's cross-process clause requires and what a self-consistency assertion
  could not express. Sub-cases `valueMismatch`, `disjointKeys`, `equalMaps` and
  `trieReceiver` at n = 9, 100, 1000, 8 repeats each, charges collected into a slice and
  asserted after the loop so a failure prints the whole spread rather than aborting at the
  first bad repeat. Helpers `builderMapOf` and `trieMapOf` take key and value functions,
  which `setBuiltMap`/`assocBuiltMap` cannot express at their fixed `Int{V:i}` shape;
  `assertTrieForm` was extracted from `assocBuiltMap` so the trie-form invariant has one
  owner.

  Flake, both directions: after the change every sub-case charges exactly `n+1`, so there
  is no green-direction flake. In the base direction a false pass on `valueMismatch` needs
  all 8 repeats to land the mismatch last — about (1/9)^8 ≈ 2.3e-8 at n = 9. That figure
  is an estimate: it assumes Go's map range puts each entry last with uniform probability,
  which the runtime does not promise. The suite does not rest on it — `disjointKeys` and
  `trieReceiver` are deterministic reds needing no repeats at all. Repeat counts and the
  measured spreads are in 0.1.

  `smallFormMismatchFirst` is green on arrival and is not a red assertion: it guards the
  `equal = eq` → `if !eq { equal = false }` rewrite. A 5-key small-form pair differing at
  `Int{V:0}`, whose entries are held sorted by `hashKey`, so the mismatch is visited first
  and a surviving bare assignment would be overwritten by the four matching entries and
  return true on every run.

  The scenario "equal contents charge equally regardless of build order" is covered by
  `insertionOrder`, added after review. For the builder and the trie form it builds the
  same key set ascending and descending, and compares them with the descending map as the
  receiver — the side the arm walks and charges. The argument is only probed, through
  `getByHashKey`, and charged nothing, so its build order cannot reach the count. Both an
  equal and an unequal pair must charge exactly `n+1`. The first version passed the
  ascending map as the receiver, which varied build order only on the side that cannot
  matter; a second review round caught it. The test is green on arrival for the equal
  pair — the old code already charged `n+1` for any equal pair — so it pins the property
  against a future regression rather than catching the historical defect. It replaces a
  post-loop check that `trieReceiver` and `valueMismatch` charged the same total, which
  the plan had cited as this scenario's coverage but which could never fire: both
  sub-tests already demand exactly `n+1`, so the two were equal by construction.

## 2. Reproducible reduction charge

- [x] 2.1 Make the charged count a function of the compared pair alone, by the mechanism the design fixed: drop the early return on a found mismatch, keep the one on a budget error, and charge one unit for an absent key, so the total is `1 + SUM over e in a of (1 if e.hk is absent from b, else units(e.v, b[e.hk]))`. A sum is order-free by construction, so it needs no visiting order and allocates nothing. The design records why a data-derived visiting order and an up-front size charge were both rejected; carry that rejection record into the completion note. Verify the boolean answer of every existing equality test is unchanged.

  Landed as three edits to the `*HashMap` arm and nothing else: the callback's early
  return narrowed from `if !equal || walkErr != nil` to `if walkErr != nil`, an explicit
  `budget.Step()` added to the not-found branch before the answer is cleared, and
  `equal = eq` replaced by `if !eq { equal = false }` — mandatory, since an exhaustive
  walk would otherwise let a later matching entry resurrect a false answer. `eachRaw`,
  `core/depth.go`'s `boundedEquals`, and the List, Vector and default arms are untouched.

  **Rejected — a data-derived visiting order.** It cannot be applied to the builder form
  alone: the visiting order would then differ by storage form, and two receivers holding
  the same contents built through `Set` versus `Assoc` would charge differently for the
  same unequal pair, which is Scenario B. 0.1 measured that exact gap at base (trie 9/79/760
  against builder 10/101/1001). Applied uniformly it means `sortedEntries()` on every
  large-map comparison — an `h.Len()` entry buffer plus an O(n log n) sort added to the
  common equal path, which the chosen shape leaves untouched. That buffer would also be
  uncharged: `EqualsBounded` takes only a `*BuiltinWorkBudget`, so there is no allocation
  ledger to bill it to, and giving it one changes a signature `plugins/stdlib/comparison.go`
  calls. Worse, with the early exit retained the sort's O(n log n) work could bill as few
  as 2 units — unbilled work on a preemptible path, which is this defect in a new place.
  `trieRoot()` instead of sorting is worse still: it allocates a trie and publishes a memo
  with no ledger to charge it to.

  **Rejected — charging from the collection's size up front.** It does not satisfy SHALL-1.
  It fixes flat maps only: with collection-valued entries the walk still exits early and
  the nested recursions still charge by where the mismatch fell, so a 100-entry map of
  50-element lists would still vary by up to ~50 units per displaced entry. It also bills
  n units for work the call never performs, which breaks the one-unit-per-compared-node
  model and would falsify the shipped proof text at `internal/inventory/work_data.go:236`.

  Neither rejection turns on cost, so no benchmark could have rescued either. Under the
  chosen shape that inventory proof text stays true as written — an absent key is now a
  stepped probe, so it is a compared node — and no edit to it is needed.

  The number the chosen shape does owe is its own consequence, which no ADR 0008 cell
  covers. It is smaller than "O(k) to O(n)" suggests: `eachRaw` has no break, so the old
  walk already made all n callback calls after a mismatch, each returning at once. What
  the change adds is the work those later callbacks now do — n − k more map probes and
  `Step` calls, where k is the mismatch's position, with a flush every 128 units. An
  absent key costs one probe and one `Step`, less than a present key's probe plus a
  recursive comparison, so an unequal pair never costs more than an equal pair of the
  same size.
  `BenchmarkEqualsBounded_MapMismatch` (`core/bench_test.go`), a builder-form n = 1000
  pair differing at one entry against an equal-pair control, measured at
  `-benchmem -benchtime 2000x -count 3`:

  | arm | ns/op | B/op | allocs/op |
  | --- | --- | --- | --- |
  | `mismatch` | 129167 / 132328 / 132402 | 0 / 0 / 0 | 0 / 0 / 0 |
  | `equalControl` | 132038 / 106393 / 93227 | 0 / 0 / 0 | 0 / 0 / 0 |

  **0 B/op and 0 allocs/op on all six rows** — the load-bearing result, and the
  measurement that proves the contract's "no allocation on the comparison path". It is
  exact on any host, because an allocation count does not depend on machine load.

  No latency conclusion is drawn. The two arms overlap on this box (93k–132k ns/op across
  the control's own three runs), so the before/after comparison is not decidable locally.
  The expected shape — the mismatch arm now scanning every entry and so converging on,
  but never exceeding, the equal-pair cost for the same pair — follows from the code, not
  from these figures.

  An earlier before/after reading of this benchmark (mismatch 75522 → 132734 ns/op) is
  superseded and should not be quoted. As first written the benchmark had its budget
  setup inside the timed region: `b.ReportAllocs` only sets a flag, and `testing.runN`
  starts the timer before the benchmark body, so that setup was amortized into the
  figures. `b.ResetTimer()` after the setup, the file's own idiom, fixed it, together with
  a one-shot boolean check outside the timed region so a fixture defect can no longer pass
  silently. The benchmark lives in a file the red stage sealed; the amend that owns it is
  purely additive (no existing line of `core/bench_test.go` removed).

  Boolean answers unchanged: `go test -timeout 2m -p 2 -parallel 2 ./core ./plugins/stdlib`
  passes, `golangci-lint run ./core/...` reports 0 issues.
- [x] 2.2 Verify budget exhaustion still behaves as before: a comparison that would exceed its budget SHALL still return the budget error rather than a boolean, and the depth cap SHALL still refuse a node past `DefaultMaxStructuralDepth` without charging for it.

  `TestEqualsBounded_MapArmBudgetAndDepth`, four sub-tests. `terminalUnderCeiling` and
  `budgetErrorByIdentity` compare two disjoint 1000-entry builder-form maps under a
  100-reduction ceiling and require the budget's terminal `CodeResourceLimit` error,
  returned by identity against the budget's latched value rather than wrapped. Both are
  red at the base commit — there a disjoint pair charged exactly 1 and never reached a
  ceiling, returning `(false, nil)` — and pass once the arm charges per entry: 1001 units
  cross the 128-unit batch mid-walk and the ceiling refuses the flush.

  `depthCapRefusedNodeIsFree` and `depthCapThroughMapArm` are preservation checks, green
  at base and after. `nestedList(DefaultMaxStructuralDepth)` and
  `nestedList(DefaultMaxStructuralDepth+1)` both charge exactly
  `DefaultMaxStructuralDepth + 1` = 1025 units, bare and wrapped as the value of a
  one-entry map: the node past the cap is refused before its `Step`, so it is never
  billed. Budget exhaustion also stays independent of iteration order — charging is one
  stream of one-unit steps, so permuting entries changes which entry a unit came from,
  never the unit count at which the ceiling is crossed.

## 3. Documentation and verification

- [x] 3.1 State the reduction-side reproducibility property in ADR 0011 next to the allocation-side one, and record the changed reduction count in `CHANGELOG.md` under `[Unreleased]`.
- [x] 3.2 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then `go test -race -timeout 2m -p 2 -parallel 2 ./core`, then `golangci-lint run ./core/...`, then `make build`, `make lint` and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`; record pass evidence.

  Final floor, run on the tree the merge sees — after the last review-remediation commit —
  green in 40 s: `go test ./core ./runtime` ok; `go test -race ./core` ok (28.1 s);
  `golangci-lint run ./core/...` 0 issues; `make build` ok; `make lint` 0 issues;
  `make test` ok in every package, `plugins/json`'s load-sensitive scaling test included.
  Packages the remediation could not reach report cached results: its commits changed only
  `core` test files and prose, and those results come from the first floor, which already
  ran on the final production code. Two earlier floors — on the pre-remediation tree and
  after the second review round — were green as well; each was re-run because a
  remediation moved the tree.
- [x] 3.3 Confirm no ADR 0008 threshold moves and run `openspec validate equals-bounded-reduction-charge-order --strict --json`.

  No threshold moves. No gold-set cell reaches the `*HashMap` arm — every `=` in
  `internal/goldset/testdata` compares Ints or Keywords — ADR 0008 states no reduction
  threshold, and neither `docs/adr/0008-consumer-performance-gate.md` nor
  `internal/perfgate/tiers.json` is in the change's diff. `openspec validate --strict`
  passes.
