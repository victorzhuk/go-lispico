## Context

See proposal.md — Why. The short form: `equalsBounded`'s `*HashMap` arm short-circuits
inside an `eachRaw` callback, so the number of nested `budget.Step()` charges is decided
by where in the walk the first mismatch fell. For a receiver in builder form `eachRaw`
ranges `h.large.m` (core/types.go:1036-1050), and Go randomises that order per range.

Three constraints shape the fix:

- `EqualsBounded` takes only a `*BuiltinWorkBudget`. There is no allocation ledger on
  this path, so anything it allocates is charged to nobody. Giving it one means changing
  a signature `plugins/stdlib/comparison.go:48` calls.
- The depth cap is tested *before* the step (core/equals_bounded.go:17-23) so a refused
  node is not billed for work the walk never did. Task 2.2 requires that to survive.
- ADR 0011 already states the order-independence property for the allocation ledger
  (docs/adr/0011-reduction-and-allocation-metering.md:70). The reduction ledger has no
  such sentence, which is how this reached production.

The stdlib `=` is the only production caller. The VM's canonical `=` answers through
`nativeEq` → `Value.Equals` → `boundedEquals` (core/vm/vm.go:1923, core/depth.go:190) and
charges no per-node units at all, so no VM total moves either way.

## Goals / Non-Goals

**Goals:**

- The units charged for a map pair are a function of the pair, across repeats, across
  processes, and across the small / builder / trie storage forms.
- The boolean answer and the budget-exhaustion behaviour do not move.
- No allocation is introduced on the comparison path.

**Non-Goals:**

- `core/depth.go`'s `boundedEquals`. Same walk shape, no budget, charges nothing.
- `eachRaw` itself. Its other callers (`sortedEntries`, `boundedEquals`) gain nothing
  from a deterministic order and would pay an allocation for it.
- Lowering the charge. A reproducible count cannot also be the current unstable one.

## Decisions

### The charge becomes a sum over every receiver entry

Neither shape the proposal named is taken. The arm drops its early return on a found
mismatch — keeping the one on a budget error — and charges one unit for an absent key,
so the total is

```
units(a, b) = 1                                  when b is not a *HashMap, or lengths differ
units(a, b) = 1 + SUM over e in a of             otherwise
                ( 1                              if e.hk is absent from b
                  units(e.v, b[e.hk])            if present )
```

A sum is order-free by construction: permuting the entries permutes the addends, not the
total. No ordering, no sort buffer, no allocation. For flat maps of n scalar-valued
entries the total is exactly **n+1** whether the pair is equal, differs in one value, or
shares no keys, and it is identical in all three storage forms.

**Why not a data-derived visiting order.** It cannot be applied to the builder form
alone — the visiting order would then differ between storage forms, and two receivers
with the same contents built through `Set` versus `Assoc` would charge differently for
the same unequal pair, which is Scenario B itself. Applied uniformly it means
`sortedEntries()` (core/types.go:1052-1065) on every large-map comparison: an `h.Len()`
entry buffer plus an O(n log n) sort added to the common *equal* path, which the chosen
shape leaves untouched. That buffer would also be uncharged, and with the early exit
retained the sort's O(n log n) work would bill as few as 2 units — a preemptible path
doing unbilled work, which is the defect this change exists to remove, in a new place.
Using `trieRoot()` instead of sorting is worse: it allocates a whole trie and publishes a
memo (core/types.go:1115-1125) with no ledger to charge it to.

**Why not charging from the collection's size up front.** It does not satisfy SHALL-1.
It fixes only flat maps: with collection-valued entries the walk still exits early and
the nested recursions at core/equals_bounded.go:78 still charge by where the mismatch
fell, so a 100-entry map of 50-element lists still varies by up to ~50 units per
displaced entry. It also bills n units for work the call never performs, which breaks the
one-unit-per-compared-node model and falsifies the shipped inventory proof text at
internal/inventory/work_data.go:236-239.

Neither rejection is a cost question, so no benchmark could rescue either. What a
benchmark *is* owed is a number for the chosen shape's own consequence: removing the
early exit turns an unequal large-map comparison from O(k) into O(n), and no ADR 0008
cell covers it. `BenchmarkEqualsBounded_MapMismatch` is part of chunk c1 for that record.

### The count is asserted as a literal, not as self-consistency

`hashOfKey` uses fixed FNV constants and is explicitly not seeded per process
(core/types.go:726-749), so `n+1` holds across processes. Asserting the literal is what
expresses Scenario A's cross-process clause, and it makes the base red deterministic: at
base the disjoint-key case charges exactly 1, because the first absent key ends the walk.
Self-consistency alone would leave the base red probabilistic.

### Scenario C is read as answer-plus-error-shape, not as ceiling-insensitivity

With a budget sufficient for the comparison the boolean answer is unchanged; when the
budget is exhausted the call still returns the budget's error, by identity, rather than a
boolean. A ceiling sized against the old low count can now trip — the proposal states
that outcome for the option it named ("raises the charged count for a mismatching pair
from as low as 0 to n"), so it is the accepted consequence, not a contract break. The
alternative reading forbids every mechanism the proposal itself offers.

### Budget exhaustion is order-free without extra machinery

Charging is one stream of one-unit `Step` calls. Permuting entries permutes which entry a
unit came from, not the cumulative count at which the ceiling is crossed, so the failing
step falls at the same unit index under every order. After the latch, `Step` returns
`b.latched` without incrementing pending (core/builtin_budget.go:24-26) and the callback
short-circuits on `walkErr`. The observable total is a function of the ceiling alone,
quantized to the 128-unit batch (`checkInterval`, core/eval.go:303). No extra red test is
required. One caveat for test authors: `flushPending` also checks the armed deadline and
`ctx.Err()`, so cancellation must be keyed to the ledger, not to wall-clock time.

## Risks / Trade-offs

- Comparing two large unequal maps now scans every entry instead of stopping at the first
  mismatch → the new cost never exceeds the equal-case cost of the same pair, which `=`
  already pays whenever the maps match, and every unit of it is charged, so the budget
  preempts it at the same ceiling. `BenchmarkEqualsBounded_MapMismatch` records the
  before/after.
- An embedder whose ceiling was sized against the old low count can now trip on the same
  program → the CHANGELOG entry states the n+1 formula explicitly and ADR 0011 carries
  the property.
- Rewriting `equal = eq` to `if !eq { equal = false }` is the one place an exhaustive
  walk can silently flip an answer: a later matching entry would otherwise resurrect a
  false → forbidden in c1's contract and guarded deterministically by the
  `smallFormMismatchFirst` sub-test, whose mismatch on `Int{V:0}` is visited first
  because small-form entries are held sorted by `hashKey`.
- `(*HashMap).Set` mutates its receiver → every fixture map is built independently;
  copying a handle and setting a differing value into it would silently turn
  `valueMismatch` into `equalMaps`.

## Implementation plan

Standing rules for any agent executing this, inlined because an uninlined rule is an
unfollowed rule: work in the assigned worktree, never the primary checkout; terse output;
no AI/tool/process references in code, comments, commits or docs; native file tools for
reading and searching; Conventional Commits, identity from the repo's configured git user,
never `--no-verify`; a contract test once written is read-only — if it looks wrong, stop
and report rather than editing it; every test command carries its resource limits.

**Mode:** existing-service-strict. **Tier:** standard. **Lenses:** spec, quality, perf —
perf because the change removes an early exit on a hot comparison path.

### c1 — `c1-order-free-charge` (tasks 1.1, 2.1) · parallel · go-coder

Sites: `core/equals_bounded.go` `equalsBounded`, anchor `av.eachRaw(func(e entry) {`;
`core/equals_bounded_test.go`, anchor `func TestEqualsBounded_ReturnsBudgetErrorUnchanged(t *testing.T) {`
(new test goes after it, at file end).

Red: two helpers `builderMapOf` / `trieMapOf` (parameterized key and value funcs — the
fixed `Int{V:i}` shape of `setBuiltMap`/`assocBuiltMap` cannot express the disjoint and
mismatch fixtures), then
`TestEqualsBounded_ReductionChargeIgnoresIterationOrder` with sub-tests `valueMismatch`,
`disjointKeys`, `equalMaps`, `trieReceiver` at n in {9, 100, 1000}, 8 repeats each,
charges collected into a `[]int64` and asserted *after* the loop, plus
`smallFormMismatchFirst` (green on arrival, the rewrite guard).
Read the count with `budgetCtx(context.Background(), DefaultMaxReductions)` →
`NewBuiltinWorkBudget` → `EqualsBounded` → `Flush()` →
`EvalMeterFrom(ctx).Snapshot().Reductions`. Never `budget.pending` above 127 units.

Code: drop `if !equal || walkErr != nil { return }` down to `if walkErr != nil { return }`;
charge `budget.Step()` in the not-found branch before clearing `equal`; replace
`equal = eq` with `if !eq { equal = false }`; add `BenchmarkEqualsBounded_MapMismatch`.
Leave `eachRaw`, `boundedEquals`, and the List/Vector/default arms untouched.

Asserted error shape: `IsTerminalEvalError`, `errCode(t, err) == CodeResourceLimit`,
budget error by identity against `b.latched`.

- `redRun`: `go test -timeout 2m -run TestEqualsBounded_ReductionChargeIgnoresIterationOrder -v ./core`
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core ./plugins/stdlib`

### c2 — `c2-baseline-budget-depth` (tasks 0.1, 2.2) · serial after c1, `sharedPkg core`, `redAfter c1`

`redAfter` is c1 rather than a full serial gate because c2's writer needs only c1's
helpers, not its production change.

Red: `TestEqualsBounded_MapArmBudgetAndDepth` with `terminalUnderCeiling` and
`budgetErrorByIdentity` (both red at base — a disjoint pair charges 1 there and never
trips a ceiling), plus `depthCapRefusedNodeIsFree` and `depthCapThroughMapArm` (green on
arrival). Both depth cases charge exactly `DefaultMaxStructuralDepth + 1` = 1025, bare and
map-wrapped, because the node past the cap is refused before its `Step`.

Task 0.1's record comes from c1's captured `-v` run at base; the base worktree is the
fallback.

- `redRun`: `go test -timeout 2m -run TestEqualsBounded_MapArmBudgetAndDepth -v ./core`
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core ./plugins/stdlib ./runtime`

### c3 — `c3-docs` (task 3.1) · parallel, shard `docs` · NO-RED-WAIVER / NO-TESTER-WAIVER

Prose only, no observable behaviour of its own. ADR 0011's *Determinism requirement*
section, immediately after the allocation-side sentence at :70 — scoped to a charged
walk, not to `=`, since the VM path charges no per-node units. CHANGELOG.md under
`[Unreleased]` → `### Fixed` (:132), anchored on the `json/decode` bullet at :134.
`internal/inventory/work_data.go:236` stays as it is: an absent key is a compared node,
so "one Step per compared node" remains true.

- `verify`: `make build`

### c4 — `c4-verification` (tasks 3.2, 3.3) · serial after c2 · NO-RED-WAIVER / NO-TESTER-WAIVER

Runs the floor, records pass evidence, confirms no ADR 0008 threshold moves — no gate
cell reaches the `*HashMap` arm; every `=` in `internal/goldset/testdata` compares Ints or
Keywords — and runs `openspec validate --strict`.

### Floor

`go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`, then
`go test -race -timeout 2m -p 2 -parallel 2 ./core`, then
`golangci-lint run ./core/...`, then `make build`, `make lint` and
`make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`.

`golangci-lint` must run with `env -C <worktree>`: an absolute path from a foreign cwd
reports "No issues found" with exit 7.

### Plan review

Verdict **pass** (zarchitect, 2 rounds). Round 1 returned one blocker — c3 named both
`### Changed` and `### Fixed`, both of which exist inside `[Unreleased]`, with no tiebreak
— resolved to `### Fixed`. Round 1 also found Scenario C's boolean-answer coverage
mis-traced to `TestHashMap_Equals_RepresentationBlind`, which never calls `EqualsBounded`;
the `smallFormMismatchFirst` guard and a corrected requirements map replaced it. Round 2
confirmed the repairs and left five warnings, all folded in.

## Plan appendix

```json
{
  "v": 2,
  "change": "equals-bounded-reduction-charge-order",
  "baseSha": "f61dd59a3a1957bfd585c00f5c4e6cbea33bc688",
  "generatedAt": "2026-09-11T06:40:57.005Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "c1-order-free-charge",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "map-arm-order-free-charge",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "coder": "go-coder",
      "sites": [
        {
          "task": "1.1",
          "file": "core/equals_bounded_test.go",
          "symbol": "TestEqualsBounded_ReductionChargeIgnoresIterationOrder (new)",
          "anchor": "func TestEqualsBounded_ReturnsBudgetErrorUnchanged(t *testing.T) {",
          "change": "Add the new test after this function (file end). Same package as setBuiltMap/assertBuilderForm, so both are directly callable. Read the count with Flush()+EvalMeterFrom(ctx).Snapshot().Reductions on a per-repeat ctx (a second read of the same ctx accumulates); b.pending is only valid below 128 units (core/builtin_budget.go:23-32), so it works for n=9 but not n=100/1000. Assert every repeat's count == the first repeat's count exactly. Existing file conventions: t.Parallel() at the top, a WHY-first doc comment naming the contract, one t.Fatalf per assertion carrying the numbers (core/equals_bounded_test.go:35-41, :66-68)."
        },
        {
          "task": "2.1",
          "file": "core/equals_bounded.go",
          "symbol": "equalsBounded (case *HashMap arm)",
          "anchor": "\t\tav.eachRaw(func(e entry) {",
          "change": "Replace the order-dependent charged walk. Option A (data-derived order): iterate av.sortedEntries() (core/types.go:1052-1065) instead of eachRaw — deterministic (typ,num,str) order via hashKey.less (core/types.go:665); early exit survives, charge becomes mismatch-position in a fixed order; costs an O(n) entry buffer + O(n log n) sort per comparison in large/trie form, zero extra in small form (sortedEntries returns h.entries as-is when large == nil). This is exactly the remedy the allocation side already took for the same hazard: trieFromBuildMap orders through sortedEntries for the same reason (core/types.go:1088-1097). Option B (charge from size): charge av.Len() units up front (Len is O(1) in all three forms, core/types.go:1204) and stop charging per entry — early exit stops saving budget, and the pre-charge bills nodes the depth guard would refuse (see 2.2). Note: internal/inventory/work_data.go:236 states '= ... charges one Step per compared node'; option B falsifies that string and the row needs updating."
        }
      ],
      "contract": {
        "states": [
          "chargedTotal",
          "equal",
          "walkErr"
        ],
        "transitions": [
          {
            "input": "b is not *HashMap, or av.Len() != bv.Len()",
            "state": "chargedTotal",
            "effect": "forced: exactly 1 (the map node charged at function entry), no entry is probed",
            "evidence": "core/equals_bounded.go:21-23,62-66"
          },
          {
            "input": "b is not *HashMap, or av.Len() != bv.Len()",
            "state": "equal",
            "effect": "forced: false",
            "evidence": "core/equals_bounded.go:62-66"
          },
          {
            "input": "receiver entry e whose e.hk is absent from bv",
            "state": "chargedTotal",
            "effect": "set: +1 -- a new explicit budget.Step() in the not-found branch, so the probe is billed even though no recursion follows",
            "evidence": "SHALL-1 (delta spec) + core/equals_bounded.go:73-77 (today this branch charges 0)"
          },
          {
            "input": "receiver entry e whose e.hk is absent from bv",
            "state": "equal",
            "effect": "clear: false; the walk CONTINUES to the remaining entries",
            "evidence": "SHALL-1; the retained early return is what makes the count order-dependent, core/equals_bounded.go:70-72"
          },
          {
            "input": "receiver entry e present in bv, values compare equal",
            "state": "chargedTotal",
            "effect": "set: + units(e.v, other), at least 1 (the recursion's own entry Step)",
            "evidence": "core/equals_bounded.go:21-23,78"
          },
          {
            "input": "receiver entry e present in bv, values compare equal",
            "state": "equal",
            "effect": "no-op",
            "evidence": "core/equals_bounded.go:83"
          },
          {
            "input": "receiver entry e present in bv, values compare unequal",
            "state": "chargedTotal",
            "effect": "set: + units(e.v, other); the walk CONTINUES, so every later entry is still probed and still charged",
            "evidence": "SHALL-1; core/equals_bounded.go:69-84"
          },
          {
            "input": "receiver entry e present in bv, values compare unequal",
            "state": "equal",
            "effect": "clear: false, and it must never be reset to true by a later entry -- the assignment 'equal = eq' at core/equals_bounded.go:83 becomes 'if !eq { equal = false }'",
            "evidence": "core/equals_bounded.go:83 (the existing assignment is only safe because the walk stopped)"
          },
          {
            "input": "budget.Step() returns an error, at the function entry or in the absent-key branch",
            "state": "walkErr",
            "effect": "set: that error value, unchanged, by identity; the walk stops",
            "evidence": "core/equals_bounded.go:21-23,85-87; core/builtin_budget.go:23-32"
          },
          {
            "input": "budget already latched before or during the walk",
            "state": "chargedTotal",
            "effect": "forced: frozen -- a latched budget returns the latched error without incrementing pending, so a continued walk after a latch charges nothing",
            "evidence": "core/builtin_budget.go:24-26"
          },
          {
            "input": "a receiver value nested past DefaultMaxStructuralDepth",
            "state": "chargedTotal",
            "effect": "no-op: 0 for the refused node -- the cap is tested before the Step",
            "evidence": "core/equals_bounded.go:17-23"
          },
          {
            "input": "same key set and same values, receiver in small / builder / trie storage form",
            "state": "chargedTotal",
            "effect": "forced: identical across the three forms, because the total is a sum over all entries and never a function of the visiting order eachRaw produces",
            "evidence": "core/types.go:1036-1050 (builder branch ranges a Go map); Scenario B of the delta spec"
          },
          {
            "input": "same pair compared repeatedly in one process, and again in a new process",
            "state": "chargedTotal",
            "effect": "forced: the same literal number every time; for flat n-entry scalar maps that number is n+1",
            "evidence": "Scenario A of the delta spec; core/types.go:732-749 (hashOfKey uses a fixed FNV seed, so no storage form carries per-process randomness once the sum is order-free)"
          }
        ],
        "forbidden": [
          "A charged total that differs between two comparisons of the same pair in one process.",
          "A charged total that differs between two receivers holding the same contents in different storage forms (small / builder / trie) or built in different insertion orders.",
          "Returning from the eachRaw callback because equal is already false -- that is the defect.",
          "'equal = eq' as a bare assignment inside the callback: with the walk now exhaustive, a later matching entry would resurrect a false answer to true.",
          "Any allocation on the comparison path: no sortedEntries(), no materialised entry slice, no trieRoot() call. trieRoot() would also publish a memo and allocate trie nodes that this path has no allocation ledger to charge.",
          "Charging for a node refused by the DefaultMaxStructuralDepth cap.",
          "Wrapping, replacing or re-creating the budget error: it leaves by identity.",
          "Changing the EqualsBounded exported signature -- plugins/stdlib/comparison.go:48 is a caller."
        ],
        "seeding": [
          "Builder form (large.m != nil, large.root == nil): only by repeated (*HashMap).Set past hashMapSmallLimit (8), as core/hashmap_test.go:376-389 setBuiltMap does, then assertBuilderForm (core/hashmap_test.go:391-402). Never by writing m.large directly.",
          "Trie form (large.root != nil): only by repeated (*HashMap).Assoc past hashMapSmallLimit, as core/hashmap_test.go:946-963 assocBuiltMap does.",
          "Small form: at most 8 distinct keys through Set or Assoc.",
          "Reduction total: budgetCtx(context.Background(), DefaultMaxReductions) (core/builtin_budget_test.go:13), one fresh ctx and one fresh NewBuiltinWorkBudget per repeat, then (*BuiltinWorkBudget).Flush() before reading EvalMeterFrom(ctx).Snapshot().Reductions -- totals below the 128-unit batch never reach the meter without the Flush (core/builtin_budget.go:28-31, core/eval.go:303).",
          "Never read budget.pending as the charge for a walk longer than 128 units; only TestEqualsBounded_HostValueNotStepped's single-unit case may do that (core/equals_bounded_test.go:146).",
          "(*HashMap).Set MUTATES its receiver (core/types.go:1227). Build every fixture map independently through builderMapOf/trieMapOf with its own key and value functions. Never copy the receiver handle and Set a differing value into it: that corrupts the receiver and silently turns 'valueMismatch' into 'equalMaps'."
        ],
        "budgets": [
          "Charged units for a flat n-entry pair of scalar-valued maps, after the change: exactly n+1, in all three cases (equal, one value differs, disjoint key sets) and in all three storage forms.",
          "Charged units for a Len mismatch or a non-map b: exactly 1.",
          "Charged units at base for the disjoint-key case: exactly 1 (deterministic) -- this sub-assertion is the deterministic red.",
          "Charged units at base for the one-value-differs case: 1 + k, k in [1, n] the position the mismatch fell at; spread is the defect.",
          "Test sizes: n = 9 (first size above hashMapSmallLimit = 8), 100, 1000.",
          "Repeats per size: 8. With the literal n+1 asserted, a base run passes only if every repeat put the mismatch last: probability (1/n)^8, at worst 4.6e-8 at n = 9. After the change the count is exact, so the committed test has no flake in the green direction.",
          "Batch interval: 128 units (core/eval.go:303) -- a walk shorter than that only reaches the meter at Flush."
        ]
      },
      "redTasks": [
        "Add core/equals_bounded_test.go helpers, exactly these names and signatures: 'func builderMapOf(t *testing.T, n int, key, val func(i int64) Value) *HashMap' -- builds by n Set calls and ends with assertBuilderForm(t, m, n); 'func trieMapOf(t *testing.T, n int, key, val func(i int64) Value) *HashMap' -- builds by n Assoc calls and asserts trie form with the SAME failure shape assocBuiltMap already uses (core/hashmap_test.go:959-960, whose mapForm(m) message is at :960), or through an extracted assertTrieForm so the trie-form invariant has one owner — do not inline a bespoke check. Both t.Helper(). Both fail the test when n <= hashMapSmallLimit.",
        "Add 'func TestEqualsBounded_ReductionChargeIgnoresIterationOrder(t *testing.T)', t.Parallel(), sub-tests per size n in {9, 100, 1000} and, inside each, these named sub-tests: 'valueMismatch' (a = builderMapOf(keys Int{V:i}, vals Int{V:i}), b = the same with key Int{V:0} bound to Int{V:-1}); 'disjointKeys' (b = builderMapOf with keys Int{V:int64(n)+i}); 'equalMaps' (b identical to a); 'trieReceiver' (a built by trieMapOf, b as in valueMismatch).",
        "Each sub-test runs 8 repeats; each repeat: ctx := budgetCtx(context.Background(), DefaultMaxReductions); budget := NewBuiltinWorkBudget(ctx); got, err := EqualsBounded(a, b, budget); require err == nil; require got == false ('equalMaps': true); require budget.Flush() == nil; charge := EvalMeterFrom(ctx).Snapshot().Reductions. COLLECT every repeat's charge into a []int64 and assert AFTER the loop — never t.Fatalf inside it. A Fatalf in the loop aborts at the first offending repeat and prints one number, which is exactly the spread task 0.1 has to show.",
        "Assert exact equality, not a bound: every repeat's charge == int64(n+1), and every repeat's charge equals the first repeat's. One t.Fatalf after the loop printing the WHOLE []int64 of charges plus the size and the sub-test name — that single print is the task 0.1 evidence. Also t.Logf the whole []int64 unconditionally before asserting, so a PASSING sub-test still leaves its numbers in -v output for task 0.1's record.",
        "Assert form independence in one place: the 'trieReceiver' charge equals the 'valueMismatch' charge for the same n -- Scenario B.",
        "Task 0.1 evidence: run the test at the base commit with -v at all three sizes and record the per-repeat charges for 'valueMismatch' (spread, within one process), 'disjointKeys' (constant 1), 'equalMaps' (constant n+1, already stable), 'trieReceiver' (constant within a process, already stable). That covers 0.1's spread, single-process and control clauses without a throwaway harness.",
        "Add sub-test 'smallFormMismatchFirst' — Green on arrival: it passes at base, because the early return there already stops at the first mismatch and answers false. It is a preservation guard for the 'equal = eq' -> 'if !eq { equal = false }' rewrite, not a red assertion, so it is NOT listed in redTests. Mark it with the repo's 'Green on arrival:' wording (core/equals_bounded_test.go:40,118,159). Shape: a 5-key small-form pair (at most hashMapSmallLimit = 8 keys, so large == nil) with keys Int{V:0}..Int{V:4}, differing only at Int{V:0}. Small-form entries are held sorted by hashKey (core/types.go:1008-1018 find breaks on hk.less; core/types.go:1052-1054 'the small form is already sorted'), so the mismatch is visited FIRST and a surviving bare 'equal = eq' is overwritten by the four matching entries and returns true. Require got == false. Do NOT assert n+1 here: this pair's own charge is 6 (1 map node + 5 entries), and the sub-test sits outside the n in {9,100,1000} loop.",
        "Add sub-test 'smallFormMismatchFirst', the deterministic guard for the 'equal = eq' rewrite: a 5-key small-form pair (at most hashMapSmallLimit = 8 keys, so large == nil) built with keys Int{V:0}..Int{V:4}, differing only at Int{V:0}. Small-form entries are held sorted by hashKey, so the mismatch is visited FIRST and a surviving bare 'equal = eq' returns true on every run. Require got == false. Without this the rewrite is covered only probabilistically (~4.6e-8 at n=9 that a bare assignment survives 8 repeats)."
      ],
      "codeTasks": [
        "core/equals_bounded.go, the *HashMap arm only: delete the 'if !equal || walkErr != nil { return }' guard and replace it with 'if walkErr != nil { return }'.",
        "In the not-found branch add the explicit probe charge before clearing equal: 'if err := budget.Step(); err != nil { walkErr = err; return }' then 'equal = false; return'.",
        "Replace 'equal = eq' with 'if !eq { equal = false }' -- mandatory, the exhaustive walk would otherwise let a later matching entry reset a false answer to true.",
        "Leave the entry, Len, error-return and depth handling untouched; leave the List, Vector and default arms untouched.",
        "Add one comment above the callback stating the invariant, not the mechanics: the total charged is a sum over every receiver entry, which is why it cannot depend on the order eachRaw yields them in.",
        "Do not touch eachRaw (core/types.go:1036-1050) and do not touch core/depth.go's boundedEquals.",
        "Task 2.1's rejection record: the design's decisions array carries it; copy the chosen/rejected reasoning into the change's tasks.md completion note, not into the source.",
        "Add BenchmarkEqualsBounded_MapMismatch to core/bench_test.go: a builder-form n=1000 pair differing at one entry, plus an equal-pair control. Run it before and after the arm change and record ns/op and B/op for both arms. B/op is the load-bearing half: RECORD both arms and require that after does not exceed before — do not assert a literal 0, since the eachRaw callback captures equal and walkErr by reference and base may already be nonzero. Allocation counts are exact on this box while latency is not decisive. tasks.md 2.1 names the surviving early exit as 'a cost worth measuring' and the chosen shape removes that exit outright (an unequal large-map comparison goes from O(k) to O(n)), with no ADR 0008 cell covering it. Copy the before/after numbers into the 2.1 completion note beside the rejection record."
      ],
      "redTests": [
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder"
      ],
      "redRun": "go test -timeout 2m -run TestEqualsBounded_ReductionChargeIgnoresIterationOrder -v ./core",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./plugins/stdlib"
    },
    {
      "id": "c2-baseline-budget-depth",
      "taskIds": [
        "0.1",
        "2.2"
      ],
      "prev": "c1-order-free-charge",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "map-arm-budget-and-depth",
      "shard": "",
      "redAfter": "c1-order-free-charge",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "coder": "go-coder",
      "sites": [
        {
          "task": "0.1",
          "file": "core/equals_bounded.go",
          "symbol": "equalsBounded (case *HashMap arm)",
          "anchor": "av.eachRaw(func(e entry) {",
          "change": "No repo edit; evidence only. The arm is reached only from inside package core when the receiver is builder form, because large/eachRaw/entry are unexported — an out-of-package harness cannot build or assert builder form. Measure from a package-core scratch test: budgetCtx(context.Background(), 1_000_000) (core/builtin_budget_test.go:13) -> NewBuiltinWorkBudget -> EqualsBounded -> b.Flush() -> EvalMeterFrom(ctx).Snapshot().Reductions (pattern verbatim at core/equals_bounded_test.go:57-68). Receivers: setBuiltMap(t, n) (core/hashmap_test.go:376) for n=9,100,1000 (hashMapSmallLimit=8, core/types.go:724); make the pair unequal by Set-ing one differing value into a second identically built map (HashMap.Set, core/types.go:1227). Controls: equal-maps = two identically built builder maps; trie-form receiver = one Assoc on a copy (core/types.go:1137) then assert large.root != nil (inverse of assertBuilderForm, core/hashmap_test.go:391). Repeats within one process: a fresh ctx+budget per repeat, same pair values."
        },
        {
          "task": "2.2",
          "file": "core/equals_bounded.go",
          "symbol": "equalsBounded (depth guard and budget step ordering)",
          "anchor": "\tif depth > DefaultMaxStructuralDepth {",
          "change": "No production edit expected beyond 2.1; the guard-before-Step order (core/equals_bounded.go:18-23, rationale in the doc comment at :11-16) is what makes a refused node uncharged and must survive. Existing coverage to keep green and extend to a map pair: TestEqualsBounded_MatchesEqualsAtDepthLimit (core/equals_bounded_test.go:83), TestEqualsBounded_ReturnsBudgetErrorUnchanged (:161, pins error identity against b.latched), TestEqualsBounded_StepsPerComparedNode/terminalUnderReductionCeiling (:46), and the EqualsBounded case of TestValueWalk_WorkCap (core/value_walk_context_test.go:148-151, 192-unit ceiling on a 352-visit Cons fixture). Boolean-answer controls for maps: TestHashMap_Equals_RepresentationBlind (core/hashmap_test.go:148) and the insertion-order pair at core/hashmap_test.go:764."
        }
      ],
      "contract": {
        "states": [
          "chargedTotal",
          "equal",
          "walkErr"
        ],
        "transitions": [
          {
            "input": "two builder-form 1000-entry maps with disjoint key sets, ceiling 100 reductions",
            "state": "walkErr",
            "effect": "set: a terminal *LispicoError with code CodeResourceLimit, returned instead of a boolean -- the 128th pending unit flushes and chargeReductions refuses it",
            "evidence": "core/builtin_budget.go:28-31,66-72; core/metering.go:458-476; core/error.go:113"
          },
          {
            "input": "the same comparison repeated on an already-latched budget",
            "state": "walkErr",
            "effect": "forced: the identical error value, ==, not merely errors.Is",
            "evidence": "core/builtin_budget.go:24-26; core/equals_bounded_test.go:161-178"
          },
          {
            "input": "nestedList(DefaultMaxStructuralDepth) compared with itself under a generous ceiling",
            "state": "chargedTotal",
            "effect": "forced: DefaultMaxStructuralDepth + 1 units; equal is true",
            "evidence": "core/equals_bounded.go:17-23; core/depth_test.go:8-14"
          },
          {
            "input": "nestedList(DefaultMaxStructuralDepth+1) compared with itself under a generous ceiling",
            "state": "chargedTotal",
            "effect": "forced: DefaultMaxStructuralDepth + 1 units -- the same number, because the node past the cap is refused before it is charged; equal is false",
            "evidence": "core/equals_bounded.go:17-23"
          },
          {
            "input": "a one-entry map whose value is nestedList(DefaultMaxStructuralDepth), and the same with nestedList(DefaultMaxStructuralDepth+1)",
            "state": "chargedTotal",
            "effect": "forced: DefaultMaxStructuralDepth + 1 units in both cases (1 for the map node, cap for the layers reached at depths 1..cap, 0 for the refused node); equal is false in both",
            "evidence": "core/equals_bounded.go:17-23,62-88"
          }
        ],
        "forbidden": [
          "Returning a boolean when the budget has latched an error.",
          "Returning a wrapped or re-created copy of the budget error.",
          "A charge for a node the depth cap refused.",
          "Reading the charge through budget.pending for a walk longer than 128 units.",
          "Keying a cancellation test to wall-clock time: flushPending also checks the armed deadline and ctx.Err() (core/builtin_budget.go:73-87), so a time-keyed cancellation makes the stopping point time-dependent rather than ledger-dependent. Key cancellation to the ledger, as core/reader_budget_cancel_test.go:24 does."
        ],
        "seeding": [
          "Disjoint builder-form pair: builderMapOf from the first seam, keys Int{V:i} against keys Int{V:int64(n)+i}, n = 1000.",
          "Ceilings: budgetCtx(context.Background(), 100) for the terminal case, budgetCtx(context.Background(), DefaultMaxReductions) for the depth cases.",
          "Nested values: nestedList (core/depth_test.go:8) only.",
          "Error shape read with IsTerminalEvalError(err) and errCode(t, err) == CodeResourceLimit (core/builtin_budget_test.go:197, core/error.go:113), matching core/equals_bounded_test.go:50."
        ],
        "budgets": [
          "n = 1000 for the terminal case: 1001 units against a ceiling of 100, and 1001 > 128 so the walk trips mid-flight rather than only at Flush.",
          "Ceiling for the terminal case: 100 reductions.",
          "Depth charge, both at-cap and past-cap, bare and map-wrapped: exactly DefaultMaxStructuralDepth + 1 = 1025 units.",
          "Budget exhaustion is order-free: charging is one stream of one-unit Step calls, so permuting entries permutes which entry a unit came from, not the cumulative count at which the ceiling is crossed; after the latch Step returns b.latched without incrementing pending (core/builtin_budget.go:24-26). The observable total is a function of the ceiling alone, quantized to the 128-unit batch. No extra red test is needed for it."
        ]
      },
      "redTasks": [
        "Add 'func TestEqualsBounded_MapArmBudgetAndDepth(t *testing.T)', t.Parallel(), in core/equals_bounded_test.go with sub-tests named 'terminalUnderCeiling', 'budgetErrorByIdentity', 'depthCapRefusedNodeIsFree' and 'depthCapThroughMapArm'.",
        "'terminalUnderCeiling' (RED at base): two 1000-entry builder-form maps with disjoint keys under budgetCtx(context.Background(), 100); require IsTerminalEvalError(err) and errCode(t, err) == CodeResourceLimit. At base this returns (false, nil).",
        "'budgetErrorByIdentity' (RED at base, same fixture): capture the returned error and require err == budget.latched, then a second EqualsBounded on the latched budget returns the identical value -- the same shape as core/equals_bounded_test.go:161-178.",
        "'depthCapRefusedNodeIsFree' (green on arrival): charge for nestedList(DefaultMaxStructuralDepth) vs itself and for nestedList(DefaultMaxStructuralDepth+1) vs itself, each under budgetCtx(..., DefaultMaxReductions) with a fresh budget and a Flush; require both == int64(DefaultMaxStructuralDepth+1) and require the booleans are true and false respectively.",
        "'depthCapThroughMapArm' (green on arrival): the same two values each held as the single value of a one-entry HashMap; require both charges == int64(DefaultMaxStructuralDepth+1) and both booleans false.",
        "Mark the two green-on-arrival sub-tests in the doc comment with the repo's 'Green on arrival:' wording (core/equals_bounded_test.go:40,118,159)."
      ],
      "codeTasks": [
        "Task 0.1 baseline evidence. PRIMARY: the output c1's red stage captured running TestEqualsBounded_ReductionChargeIgnoresIterationOrder -v at base. For this to carry numbers rather than a bare PASS, c1's test must t.Logf the whole []int64 of the 8 repeat charges for EVERY sub-test unconditionally, before asserting — a passing sub-test (equalMaps, disjointKeys, trieReceiver) otherwise leaves no slice to copy into the record. The Fatalf-after-the-loop carries the failing ones. Record from it: valueMismatch (the spread, within one process), disjointKeys (constant 1), equalMaps (constant n+1, already stable), trieReceiver (constant, already stable), and how many repeats exposed the spread at each size. FALLBACK, only if that output was not captured: git worktree add <tmp> f61dd59a3a1957bfd585c00f5c4e6cbea33bc688, copy the committed test and its builderMapOf/trieMapOf helpers into core/ there, run 'go test -timeout 2m -run TestEqualsBounded_ReductionChargeIgnoresIterationOrder -v ./core', record the same table, then remove the worktree. Either way: no repo file changes.",
        "No production change beyond the first seam's. This seam's job is to run green after that seam lands and to prove the exhaustion and depth behaviour were preserved.",
        "Task 2.1's 'boolean answer of every existing equality test is unchanged' clause is settled by the verify command below plus the floor, not by new assertions."
      ],
      "redTests": [
        "core/equals_bounded_test.go :: TestEqualsBounded_MapArmBudgetAndDepth"
      ],
      "redRun": "go test -timeout 2m -run TestEqualsBounded_MapArmBudgetAndDepth -v ./core",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./plugins/stdlib ./runtime"
    },
    {
      "id": "c3-docs",
      "taskIds": [
        "3.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "docs-reduction-reproducibility",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "coder": "coder",
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Determinism requirement",
          "anchor": "It also MUST NOT depend on Go map iteration order:",
          "change": "The sentence at that anchor is the allocation-side property (it governs 'charges the same total'); state the reduction-side twin in the same section, before the 'The requirement binds a source to a total' paragraph that follows it. CHANGELOG.md: add ONE entry under '## [Unreleased]' -> '### Fixed' (CHANGELOG.md:132, inside [Unreleased]; the next release header is CHANGELOG.md:283). Not '### Changed' — the delta spec frames this as a defect (a count that 'varied by up to n-1 units on unchanged input'). Unique insertion anchor: the existing first bullet of that Fixed block, '- `json/decode` now charges its decoded result exactly once per call.' (CHANGELOG.md:134). '### Fixed' alone is not unique in the file."
        }
      ],
      "contract": {
        "states": [
          "adrText",
          "changelogText"
        ],
        "transitions": [
          {
            "input": "ADR 0011, Determinism requirement section",
            "state": "adrText",
            "effect": "set: a paragraph after line 70 stating that the reduction ledger carries the same property as the allocation ledger -- the units a charged walk bills for visiting a collection are a function of the pair it compares, never of the order the collection was visited in, and that a walk which stops early on a mismatch therefore bills the whole collection rather than the prefix it happened to reach",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md:68-72; tasks.md 3.1"
          },
          {
            "input": "CHANGELOG.md [Unreleased]",
            "state": "changelogText",
            "effect": "set: one ### Fixed entry naming the new formula -- bounded equality over two hash maps of n entries now charges 1 + one unit per receiver entry (a missing key costs its probe, a present key costs the comparison of its value), so a flat n-entry pair costs n+1 units whether it matches, differs or shares no keys; it previously charged as little as 1 and varied run to run for a builder-form receiver",
            "evidence": "CHANGELOG.md:8,132 ([Unreleased] / ### Fixed already exist)"
          }
        ],
        "forbidden": [
          "A new ADR file -- 0011 is the owner of this property.",
          "Touching ADR 0008 or internal/perfgate/tiers.json: no threshold moves.",
          "Editing internal/inventory/work_data.go:236 -- its proof text ('charges one Step per compared node') stays true, since an absent key is a compared node.",
          "Phrasing the new ADR paragraph as a property of '=' rather than of a charged walk. The VM's canonical '=' answers through nativeEq -> Value.Equals -> boundedEquals (core/vm/vm.go:1923, core/depth.go:190) and charges no per-node units at all, so an '='-scoped sentence would read as false against the VM path. Keep it parallel to the allocation-side sentence at :70, which is correctly scoped today."
        ],
        "seeding": [
          "Not applicable: prose."
        ],
        "budgets": [
          "ADR addition: one paragraph. CHANGELOG: one entry naming the n+1 figure."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "docs/adr/0011-reduction-and-allocation-metering.md: add the reduction-side paragraph after line 70.",
        "CHANGELOG.md: add the ### Fixed entry under [Unreleased]."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make build"
    },
    {
      "id": "c4-verification",
      "taskIds": [
        "3.2",
        "3.3"
      ],
      "prev": "c2-baseline-budget-depth",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "verification",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core",
        "./runtime"
      ],
      "coder": "coder",
      "sites": [],
      "contract": {
        "states": [
          "floorEvidence",
          "gateEvidence",
          "specValidation"
        ],
        "transitions": [
          {
            "input": "task 3.2's command sequence",
            "state": "floorEvidence",
            "effect": "set: the recorded pass output of all six commands, in order",
            "evidence": "tasks.md 3.2"
          },
          {
            "input": "ADR 0008 gate cells",
            "state": "gateEvidence",
            "effect": "no-op: no threshold moves -- no gate cell exercises the *HashMap arm. The 13 gold-set fixtures plus their parse cells, the call cell and the startup cell are the whole corpus (internal/perfgate/tiers.json), and the five fixtures that use = compare scalars and keywords only",
            "evidence": "internal/goldset/testdata/{loop-sum,pipeline,queue-promote,route-decision,safe-parse}.lisp; internal/perfgate/tiers.json"
          },
          {
            "input": "openspec validate equals-bounded-reduction-charge-order --strict --json",
            "state": "specValidation",
            "effect": "set: a clean validation result",
            "evidence": "tasks.md 3.3"
          }
        ],
        "forbidden": [
          "Editing internal/perfgate/tiers.json or ADR 0008 thresholds.",
          "Running any test command without a timeout.",
          "Closing 3.2 on a -run-scoped run: each package runs whole."
        ],
        "seeding": [
          "Not applicable."
        ],
        "budgets": [
          "-timeout 2m on every test command; -p 2 -parallel 2 as the floor states."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "Run the floor verbatim and record each command's result.",
        "Record the ADR 0008 no-move argument with the fixture evidence above.",
        "Run the openspec validation."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime"
    }
  ],
  "seams": [
    {
      "id": "map-arm-order-free-charge",
      "tasks": [
        "0.1",
        "1.1",
        "2.1"
      ],
      "summary": "equalsBounded's *HashMap arm stops charging by walk progress: the early return on a found mismatch is dropped, an absent key charges one unit explicitly, and the total becomes a sum over every receiver entry -- 1 + SUM over e in a of (e.hk in b ? units(e.v, b[e.hk]) : 1). A sum is order-free by construction, so no ordering, no sort buffer and no allocation is introduced. For flat maps of n scalar-valued entries the total is exactly n+1 whether the pair is equal, differs in a value, or has disjoint keys, and it is identical for small, builder and trie receivers.",
      "contract": {
        "states": [
          "chargedTotal",
          "equal",
          "walkErr"
        ],
        "transitions": [
          {
            "input": "b is not *HashMap, or av.Len() != bv.Len()",
            "state": "chargedTotal",
            "effect": "forced: exactly 1 (the map node charged at function entry), no entry is probed",
            "evidence": "core/equals_bounded.go:21-23,62-66"
          },
          {
            "input": "b is not *HashMap, or av.Len() != bv.Len()",
            "state": "equal",
            "effect": "forced: false",
            "evidence": "core/equals_bounded.go:62-66"
          },
          {
            "input": "receiver entry e whose e.hk is absent from bv",
            "state": "chargedTotal",
            "effect": "set: +1 -- a new explicit budget.Step() in the not-found branch, so the probe is billed even though no recursion follows",
            "evidence": "SHALL-1 (delta spec) + core/equals_bounded.go:73-77 (today this branch charges 0)"
          },
          {
            "input": "receiver entry e whose e.hk is absent from bv",
            "state": "equal",
            "effect": "clear: false; the walk CONTINUES to the remaining entries",
            "evidence": "SHALL-1; the retained early return is what makes the count order-dependent, core/equals_bounded.go:70-72"
          },
          {
            "input": "receiver entry e present in bv, values compare equal",
            "state": "chargedTotal",
            "effect": "set: + units(e.v, other), at least 1 (the recursion's own entry Step)",
            "evidence": "core/equals_bounded.go:21-23,78"
          },
          {
            "input": "receiver entry e present in bv, values compare equal",
            "state": "equal",
            "effect": "no-op",
            "evidence": "core/equals_bounded.go:83"
          },
          {
            "input": "receiver entry e present in bv, values compare unequal",
            "state": "chargedTotal",
            "effect": "set: + units(e.v, other); the walk CONTINUES, so every later entry is still probed and still charged",
            "evidence": "SHALL-1; core/equals_bounded.go:69-84"
          },
          {
            "input": "receiver entry e present in bv, values compare unequal",
            "state": "equal",
            "effect": "clear: false, and it must never be reset to true by a later entry -- the assignment 'equal = eq' at core/equals_bounded.go:83 becomes 'if !eq { equal = false }'",
            "evidence": "core/equals_bounded.go:83 (the existing assignment is only safe because the walk stopped)"
          },
          {
            "input": "budget.Step() returns an error, at the function entry or in the absent-key branch",
            "state": "walkErr",
            "effect": "set: that error value, unchanged, by identity; the walk stops",
            "evidence": "core/equals_bounded.go:21-23,85-87; core/builtin_budget.go:23-32"
          },
          {
            "input": "budget already latched before or during the walk",
            "state": "chargedTotal",
            "effect": "forced: frozen -- a latched budget returns the latched error without incrementing pending, so a continued walk after a latch charges nothing",
            "evidence": "core/builtin_budget.go:24-26"
          },
          {
            "input": "a receiver value nested past DefaultMaxStructuralDepth",
            "state": "chargedTotal",
            "effect": "no-op: 0 for the refused node -- the cap is tested before the Step",
            "evidence": "core/equals_bounded.go:17-23"
          },
          {
            "input": "same key set and same values, receiver in small / builder / trie storage form",
            "state": "chargedTotal",
            "effect": "forced: identical across the three forms, because the total is a sum over all entries and never a function of the visiting order eachRaw produces",
            "evidence": "core/types.go:1036-1050 (builder branch ranges a Go map); Scenario B of the delta spec"
          },
          {
            "input": "same pair compared repeatedly in one process, and again in a new process",
            "state": "chargedTotal",
            "effect": "forced: the same literal number every time; for flat n-entry scalar maps that number is n+1",
            "evidence": "Scenario A of the delta spec; core/types.go:732-749 (hashOfKey uses a fixed FNV seed, so no storage form carries per-process randomness once the sum is order-free)"
          }
        ],
        "forbidden": [
          "A charged total that differs between two comparisons of the same pair in one process.",
          "A charged total that differs between two receivers holding the same contents in different storage forms (small / builder / trie) or built in different insertion orders.",
          "Returning from the eachRaw callback because equal is already false -- that is the defect.",
          "'equal = eq' as a bare assignment inside the callback: with the walk now exhaustive, a later matching entry would resurrect a false answer to true.",
          "Any allocation on the comparison path: no sortedEntries(), no materialised entry slice, no trieRoot() call. trieRoot() would also publish a memo and allocate trie nodes that this path has no allocation ledger to charge.",
          "Charging for a node refused by the DefaultMaxStructuralDepth cap.",
          "Wrapping, replacing or re-creating the budget error: it leaves by identity.",
          "Changing the EqualsBounded exported signature -- plugins/stdlib/comparison.go:48 is a caller."
        ],
        "seeding": [
          "Builder form (large.m != nil, large.root == nil): only by repeated (*HashMap).Set past hashMapSmallLimit (8), as core/hashmap_test.go:376-389 setBuiltMap does, then assertBuilderForm (core/hashmap_test.go:391-402). Never by writing m.large directly.",
          "Trie form (large.root != nil): only by repeated (*HashMap).Assoc past hashMapSmallLimit, as core/hashmap_test.go:946-963 assocBuiltMap does.",
          "Small form: at most 8 distinct keys through Set or Assoc.",
          "Reduction total: budgetCtx(context.Background(), DefaultMaxReductions) (core/builtin_budget_test.go:13), one fresh ctx and one fresh NewBuiltinWorkBudget per repeat, then (*BuiltinWorkBudget).Flush() before reading EvalMeterFrom(ctx).Snapshot().Reductions -- totals below the 128-unit batch never reach the meter without the Flush (core/builtin_budget.go:28-31, core/eval.go:303).",
          "Never read budget.pending as the charge for a walk longer than 128 units; only TestEqualsBounded_HostValueNotStepped's single-unit case may do that (core/equals_bounded_test.go:146).",
          "(*HashMap).Set MUTATES its receiver (core/types.go:1227). Build every fixture map independently through builderMapOf/trieMapOf with its own key and value functions. Never copy the receiver handle and Set a differing value into it: that corrupts the receiver and silently turns 'valueMismatch' into 'equalMaps'."
        ],
        "budgets": [
          "Charged units for a flat n-entry pair of scalar-valued maps, after the change: exactly n+1, in all three cases (equal, one value differs, disjoint key sets) and in all three storage forms.",
          "Charged units for a Len mismatch or a non-map b: exactly 1.",
          "Charged units at base for the disjoint-key case: exactly 1 (deterministic) -- this sub-assertion is the deterministic red.",
          "Charged units at base for the one-value-differs case: 1 + k, k in [1, n] the position the mismatch fell at; spread is the defect.",
          "Test sizes: n = 9 (first size above hashMapSmallLimit = 8), 100, 1000.",
          "Repeats per size: 8. With the literal n+1 asserted, a base run passes only if every repeat put the mismatch last: probability (1/n)^8, at worst 4.6e-8 at n = 9. After the change the count is exact, so the committed test has no flake in the green direction.",
          "Batch interval: 128 units (core/eval.go:303) -- a walk shorter than that only reaches the meter at Flush."
        ]
      }
    },
    {
      "id": "map-arm-budget-and-depth",
      "tasks": [
        "2.2"
      ],
      "summary": "The raised charge must not change how the map arm ends: a comparison that outruns its ceiling still returns the budget's terminal error by identity rather than a boolean, and a node past DefaultMaxStructuralDepth is still refused without being charged. The terminal assertion is red at base (a disjoint-key pair charges 1 there and so never trips a ceiling); the depth assertions are green on arrival and are preservation checks.",
      "contract": {
        "states": [
          "chargedTotal",
          "equal",
          "walkErr"
        ],
        "transitions": [
          {
            "input": "two builder-form 1000-entry maps with disjoint key sets, ceiling 100 reductions",
            "state": "walkErr",
            "effect": "set: a terminal *LispicoError with code CodeResourceLimit, returned instead of a boolean -- the 128th pending unit flushes and chargeReductions refuses it",
            "evidence": "core/builtin_budget.go:28-31,66-72; core/metering.go:458-476; core/error.go:113"
          },
          {
            "input": "the same comparison repeated on an already-latched budget",
            "state": "walkErr",
            "effect": "forced: the identical error value, ==, not merely errors.Is",
            "evidence": "core/builtin_budget.go:24-26; core/equals_bounded_test.go:161-178"
          },
          {
            "input": "nestedList(DefaultMaxStructuralDepth) compared with itself under a generous ceiling",
            "state": "chargedTotal",
            "effect": "forced: DefaultMaxStructuralDepth + 1 units; equal is true",
            "evidence": "core/equals_bounded.go:17-23; core/depth_test.go:8-14"
          },
          {
            "input": "nestedList(DefaultMaxStructuralDepth+1) compared with itself under a generous ceiling",
            "state": "chargedTotal",
            "effect": "forced: DefaultMaxStructuralDepth + 1 units -- the same number, because the node past the cap is refused before it is charged; equal is false",
            "evidence": "core/equals_bounded.go:17-23"
          },
          {
            "input": "a one-entry map whose value is nestedList(DefaultMaxStructuralDepth), and the same with nestedList(DefaultMaxStructuralDepth+1)",
            "state": "chargedTotal",
            "effect": "forced: DefaultMaxStructuralDepth + 1 units in both cases (1 for the map node, cap for the layers reached at depths 1..cap, 0 for the refused node); equal is false in both",
            "evidence": "core/equals_bounded.go:17-23,62-88"
          }
        ],
        "forbidden": [
          "Returning a boolean when the budget has latched an error.",
          "Returning a wrapped or re-created copy of the budget error.",
          "A charge for a node the depth cap refused.",
          "Reading the charge through budget.pending for a walk longer than 128 units.",
          "Keying a cancellation test to wall-clock time: flushPending also checks the armed deadline and ctx.Err() (core/builtin_budget.go:73-87), so a time-keyed cancellation makes the stopping point time-dependent rather than ledger-dependent. Key cancellation to the ledger, as core/reader_budget_cancel_test.go:24 does."
        ],
        "seeding": [
          "Disjoint builder-form pair: builderMapOf from the first seam, keys Int{V:i} against keys Int{V:int64(n)+i}, n = 1000.",
          "Ceilings: budgetCtx(context.Background(), 100) for the terminal case, budgetCtx(context.Background(), DefaultMaxReductions) for the depth cases.",
          "Nested values: nestedList (core/depth_test.go:8) only.",
          "Error shape read with IsTerminalEvalError(err) and errCode(t, err) == CodeResourceLimit (core/builtin_budget_test.go:197, core/error.go:113), matching core/equals_bounded_test.go:50."
        ],
        "budgets": [
          "n = 1000 for the terminal case: 1001 units against a ceiling of 100, and 1001 > 128 so the walk trips mid-flight rather than only at Flush.",
          "Ceiling for the terminal case: 100 reductions.",
          "Depth charge, both at-cap and past-cap, bare and map-wrapped: exactly DefaultMaxStructuralDepth + 1 = 1025 units.",
          "Budget exhaustion is order-free: charging is one stream of one-unit Step calls, so permuting entries permutes which entry a unit came from, not the cumulative count at which the ceiling is crossed; after the latch Step returns b.latched without incrementing pending (core/builtin_budget.go:24-26). The observable total is a function of the ceiling alone, quantized to the 128-unit batch. No extra red test is needed for it."
        ]
      }
    },
    {
      "id": "docs-reduction-reproducibility",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-RED-WAIVER: prose only, no observable behaviour of its own. NO-TESTER-WAIVER: same. State the reduction-side reproducibility property in ADR 0011's 'Determinism requirement' section, immediately after the allocation-side sentence at docs/adr/0011-reduction-and-allocation-metering.md:70 ('It also MUST NOT depend on Go map iteration order...'), and record the changed count in CHANGELOG.md under [Unreleased] / ### Fixed.",
      "contract": {
        "states": [
          "adrText",
          "changelogText"
        ],
        "transitions": [
          {
            "input": "ADR 0011, Determinism requirement section",
            "state": "adrText",
            "effect": "set: a paragraph after line 70 stating that the reduction ledger carries the same property as the allocation ledger -- the units a charged walk bills for visiting a collection are a function of the pair it compares, never of the order the collection was visited in, and that a walk which stops early on a mismatch therefore bills the whole collection rather than the prefix it happened to reach",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md:68-72; tasks.md 3.1"
          },
          {
            "input": "CHANGELOG.md [Unreleased]",
            "state": "changelogText",
            "effect": "set: one ### Fixed entry naming the new formula -- bounded equality over two hash maps of n entries now charges 1 + one unit per receiver entry (a missing key costs its probe, a present key costs the comparison of its value), so a flat n-entry pair costs n+1 units whether it matches, differs or shares no keys; it previously charged as little as 1 and varied run to run for a builder-form receiver",
            "evidence": "CHANGELOG.md:8,132 ([Unreleased] / ### Fixed already exist)"
          }
        ],
        "forbidden": [
          "A new ADR file -- 0011 is the owner of this property.",
          "Touching ADR 0008 or internal/perfgate/tiers.json: no threshold moves.",
          "Editing internal/inventory/work_data.go:236 -- its proof text ('charges one Step per compared node') stays true, since an absent key is a compared node.",
          "Phrasing the new ADR paragraph as a property of '=' rather than of a charged walk. The VM's canonical '=' answers through nativeEq -> Value.Equals -> boundedEquals (core/vm/vm.go:1923, core/depth.go:190) and charges no per-node units at all, so an '='-scoped sentence would read as false against the VM path. Keep it parallel to the allocation-side sentence at :70, which is correctly scoped today."
        ],
        "seeding": [
          "Not applicable: prose."
        ],
        "budgets": [
          "ADR addition: one paragraph. CHANGELOG: one entry naming the n+1 figure."
        ]
      }
    },
    {
      "id": "verification",
      "tasks": [
        "3.2",
        "3.3"
      ],
      "summary": "NO-RED-WAIVER: verification run, no behaviour of its own. NO-TESTER-WAIVER: same. Run the floor and record pass evidence, confirm no ADR 0008 threshold moves, and validate the change.",
      "contract": {
        "states": [
          "floorEvidence",
          "gateEvidence",
          "specValidation"
        ],
        "transitions": [
          {
            "input": "task 3.2's command sequence",
            "state": "floorEvidence",
            "effect": "set: the recorded pass output of all six commands, in order",
            "evidence": "tasks.md 3.2"
          },
          {
            "input": "ADR 0008 gate cells",
            "state": "gateEvidence",
            "effect": "no-op: no threshold moves -- no gate cell exercises the *HashMap arm. The 13 gold-set fixtures plus their parse cells, the call cell and the startup cell are the whole corpus (internal/perfgate/tiers.json), and the five fixtures that use = compare scalars and keywords only",
            "evidence": "internal/goldset/testdata/{loop-sum,pipeline,queue-promote,route-decision,safe-parse}.lisp; internal/perfgate/tiers.json"
          },
          {
            "input": "openspec validate equals-bounded-reduction-charge-order --strict --json",
            "state": "specValidation",
            "effect": "set: a clean validation result",
            "evidence": "tasks.md 3.3"
          }
        ],
        "forbidden": [
          "Editing internal/perfgate/tiers.json or ADR 0008 thresholds.",
          "Running any test command without a timeout.",
          "Closing 3.2 on a -run-scoped run: each package runs whole."
        ],
        "seeding": [
          "Not applicable."
        ],
        "budgets": [
          "-timeout 2m on every test command; -p 2 -parallel 2 as the floor states."
        ]
      }
    }
  ],
  "requirements": [
    {
      "shall": "The reduction units charged for evaluating a form SHALL be a function of that form and the values it operates on. Where a charged walk visits a collection, the number of units it charges SHALL NOT depend on the order it visited that collection in.",
      "tests": [
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder"
      ]
    },
    {
      "shall": "- **THEN** every comparison SHALL charge the identical number of reduction units, and repeating the sequence in a new process SHALL charge that same number again",
      "tests": [
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder/valueMismatch",
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder/disjointKeys"
      ]
    },
    {
      "shall": "- **THEN** both comparisons SHALL charge the same number of reduction units",
      "tests": [
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder/equalMaps",
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder/trieReceiver"
      ]
    },
    {
      "shall": "- **THEN** the boolean answer and the budget-exhaustion behavior SHALL be what they were before, so that only the number charged moves",
      "tests": [
        "core/equals_bounded_test.go :: TestEqualsBounded_MapArmBudgetAndDepth",
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder/smallFormMismatchFirst",
        "core/equals_bounded_test.go :: TestEqualsBounded_ReductionChargeIgnoresIterationOrder/equalMaps",
        "core/equals_bounded_test.go :: TestEqualsBounded_StepsPerComparedNode",
        "core/equals_bounded_test.go :: TestEqualsBounded_ReturnsBudgetErrorUnchanged"
      ]
    }
  ],
  "testHarness": [
    "setBuiltMap — core/hashmap_test.go:376 — builds an n-entry map past hashMapSmallLimit through Set alone and asserts it is left in builder form; fails the test if n <= hashMapSmallLimit.",
    "assertBuilderForm — core/hashmap_test.go:391 — asserts large != nil, large.root == nil, len(large.m) == n.",
    "retainedTrieBytes — core/hashmap_test.go:406 — sums a finished trie's node bytes (allocation-side helper, not needed for reductions).",
    "budgetCtx — core/builtin_budget_test.go:13 — context with a reduction ceiling and a 1<<30 allocation ceiling.",
    "errCode — core/builtin_budget_test.go:197 — LispicoError code via errors.As, t.Fatalf on a non-LispicoError.",
    "stepN — core/builtin_budget_test.go:207 — n Steps, each required to return nil.",
    "budgetContext — core/reader_budget_cancel_test.go:13 — returns (ctx, EvalMeter) for a reduction ceiling.",
    "chargedReductions — core/reader_budget_cancel_test.go:18 — m.Snapshot().Reductions.",
    "cancelAtReductions — core/reader_budget_cancel_test.go:24-33 — a ctx whose Err() trips once the ledger passes a chosen count; how core tests drive mid-walk cancellation.",
    "equalsNodeList — core/equals_bounded_test.go:11 — n-element List of distinct Ints; two of them cost n+1 units.",
    "hostEqValue — core/equals_bounded_test.go:22-33 — host Value with a call counter, for the unstepped default branch.",
    "nestedList — core/depth_test.go:8 — an Int wrapped in depth layers of List; the depth-cap fixture.",
    "sharedConsChain — core/value_walk_context_test.go:14 — shared-reference fixture, 11*2^levels logical visits.",
    "runWalk — core/value_walk_context_test.go:22 — wraps a walk assertion with its contract string.",
    "newTestEnv — core/eval_test.go:45 — core Env for eval-level tests (no stdlib).",
    "evalStr — core/eval_test.go:13 — read+eval a source string against an Env; evalStrErr at :30 for the error form.",
    "NewBuiltinWorkBudget — core/builtin_budget.go:16 — budget over the eval state carried by ctx; reads no clock.",
    "EvalMeterFrom — core/metering.go:145 — the meter for ctx; Snapshot().Reductions is the charged total.",
    "core/map_determinism_test.go — TestEval_MapLiteralDeterministic:5 (100 repeats of a map literal against an Assoc-built oracle) and TestHashMap_StringDeterministic:21 (50 repeats of String()) — the repo's existing shape for 'repeat N times, require identical result'; neither builds a budget or reads a reduction count.",
    "core/metering_test.go — contains only TestVectorLedgerBytesIndependentOfLayout:13 (allocation bytes); no budget builder, no reduction reader.",
    "core/hashmap_test.go:148 TestHashMap_Equals_RepresentationBlind and core/hashmap_test.go:764 (forward/backward insertion order Equals) — the boolean-answer controls for maps that task 2.1 must leave unchanged."
  ],
  "floor": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime, then go test -race -timeout 2m -p 2 -parallel 2 ./core, then golangci-lint run ./core/..., then make build, make lint and make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'; record pass evidence.",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
