# Design — trie conversion charge determinism

## Context

See `proposal.md` — Why. In short: `trieFromBuildMap` ranges the Go map `h.large.m` and sums the
path-copy allocations `hamtNode.assoc` reports, so the byte figure it returns is decided by an
iteration order Go randomises per range.

The constraints the approach has to fit:

- The conversion is reached from exactly two places, `Assoc` and `Dissoc`, both under
  `if root == nil`, and only while the receiver is in builder form — `h.large != nil` with
  `h.large.root == nil`, the state a map built by a literal, `hash-map`, `merge` or `json/decode`
  sits in until its first update.
- `core/` has zero external imports, so the ordering has to come from what the file already has.
  It has one: `hashKey.less`, the `(typ, num, str)` comparator behind `find` and `sortedEntries`.
- The change's own delta spec forbids buying reproducibility by under-billing, which rules on one
  of the three mechanisms the proposal left open before any measurement is taken.
- `hashmap-builder-trie-conversion` rewrites the same function and is paused on this defect. It is
  waiting for a reproducible baseline, not for its mechanism to be chosen for it.

## Goals / Non-Goals

**Goals.** The conversion charge is a function of the map's contents alone, identical across
repeats and across processes. The charge stays honest: the intermediate path copies the build
actually allocates are still billed. The finished trie is untouched.

**Non-Goals.** Making the conversion cheaper — that is
`hashmap-builder-trie-conversion`'s argument and this change deliberately leaves it intact. Fixing
the reduction ledger: `equalsBounded` charges reduction units in `eachRaw` order for unequal
builder-form maps, which is a real defect on a different ledger and is tracked by
`equals-bounded-reduction-charge-order`. Changing what any operation returns, prints or compares
equal to.

## Decisions

### Insert in `sortedEntries()` order, and charge the buffer that sort obtains

`trieFromBuildMap` stops ranging `h.large.m` and inserts through the entries `sortedEntries()`
already returns in `(typ, num, str)` order. The charge becomes
`HashMapShallowBytes(n)` for the buffer, on top of the unchanged path-copy sum. It reuses `hashKey.less`, so no second ordering enters the file, and the
body of one function is the whole code diff.

**Rejected — one order-independent build.** It is a trie-construction rewrite: a bottom-up radix
partition plus the `shift >= 32` collision case `mergeEntries` owns today. Its trie-shape equality
has no test surface in this change, it is far larger than the requirement it serves, and it would
move the builder-form charge by two orders of magnitude inside the window in which
`hashmap-builder-trie-conversion` is re-baselining exactly those numbers.

**Rejected — charge the finished structure.** Inadmissible as the specs are written. The delta
spec's scenario *A reproducible charge is still an honest one* requires the charge to account for
storage the operation actually obtained. While the build still copies one root-to-leaf path per
entry, those copies are storage obtained and discarded; billing only the retained trie is exactly
the under-bill that scenario names.

### The entry buffer is charged with `HashMapShallowBytes`, not with a new term

Adding the sort adds an allocation to a metered path. Leaving it uncharged in the same change that
pins the honesty of that path would be self-defeating, so it is charged — but with the term the
file already uses. `HashMapShallowBytes(n)` is `MeterHashMapHeaderBytes + n*MeterHashMapEntryBytes`
(32 + 64n), and `core/types.go` already prices every other `make([]entry, ...)` allocation with it,
at three sites. Both of its constants are ADR 0011 rows, so task 3.1's clause *verify no unit is
published that no ADR table row owns* holds without touching the tables.

The first draft of this design charged `MeterCollectionHeaderBytes + 64n` (24 + 64n) instead. No
single ADR 0011 row owns that composite: the 24-byte rows are `| List header |`, `| Vector header |`
and `| Reader promoted-map construction header |`, and the ADR's prose explicitly separates that
last one from the 32-byte `HashMapShallowBytes` term. It would have published an unowned unit —
and sealed it into the honesty assertion — in the change whose own task forbids exactly that. The
8-byte difference is in the over-billing direction, which the honesty scenario permits; it forbids
only under-billing.

### The committed assertions sit at n = 100 and n = 1000, never at n = 9

At or below `vecBranch` (32) the keys can occupy 32 distinct level-0 slots, in which case insert
*i* charges `24 + 64*(i+1)` whatever the order and the total is already order-independent — the
base test would pass and the red stage would be false. At n ≥ 33 the pigeonhole forces at least
one merge, whose parent-clone cost depends on the root's population when it happens. n = 9 stays
evidence-only, for task 0.1.

### Task 0.1's claim was narrowed before it reached a coder

As written, 0.1 asked the coder to confirm that no `eachRaw` caller charges by traversal order.
That holds for the allocation ledger — `boundedEquals` takes no budget, `sortedEntries` sorts
first — and fails for the reduction ledger: `equalsBounded`'s callback short-circuits on
`if !equal || walkErr != nil`, and each surviving entry recurses through `budget.Step()`, so for
two unequal maps with a builder-form receiver the charged count is decided by where in the
randomised range the mismatch fell. `tasks.md` 0.1 now scopes the confirmation to allocation, and
the reduction finding is filed as its own change rather than absorbed here.

## Risks / Trade-offs

- **The charge moves up as well as becoming stable** — the buffer adds 6 432 bytes at n = 100 and
  64 032 at n = 1000 → both units are existing ADR 0011 rows, so nothing new is published, and task
  3.1 records the move in `CHANGELOG.md` without quoting a byte count that depends on the key set.
- **Collision-node entry order changes from Go map order to `hashKey.less` order** → verified
  harmless: `hamtNode.get` scans the collision node, `sortedEntries` re-sorts, and `hamtSizeBytes`
  counts rather than inspects. What does change is `eachRaw`'s order over a converted trie, whose
  only callers are the two membership walks and `sortedEntries`.
- **The sort adds O(n log n) comparisons and one allocation to a path that had neither** → no ADR
  0008 cell reaches it; the largest map in the gold set is the 3-key `merge-config` literal. The
  exposure is Lisp-level `(assoc m k v)` or `(dissoc m k)` over a map above 8 keys.
- **`hashmap-builder-trie-conversion` re-baselines on top of this, and one sentence of it expires**
  — its task 3.1 tells the changelog to avoid an exact byte count because the builder-arm charge
  moves up to 25% between runs → once this lands that clause needs rewording. No code conflict:
  this change rewrites the body of `trieFromBuildMap` while its task 2.1 factors the `Assoc` and
  `Dissoc` call arms into a `trieRoot()` helper.
- **`sortedEntries` returns `h.entries` directly for the small form, aliasing the receiver** →
  `trieFromBuildMap` is reached only under `h.large != nil && h.large.root == nil`, so that branch
  is unreachable from it today; a future caller that drops the guard would alias the receiver into
  the build.
- **No existing benchmark reaches `trieFromBuildMap`** — `BenchmarkHashMap_Assoc` uses an empty
  map, `AssocChain` and `GetLarge` build through `Assoc` so they are already in trie form, and
  `SetBuild` never updates → task 0.1's base figures come from the task 1.1 tests run before the
  fix, which is why c2 depends on c1's red output rather than on a benchmark arm.

## Migration Plan

None. No stored data, no wire format, no public Go signature changes. The only observable move is
the `int64` charge `Assoc`/`Dissoc` return for a builder-form receiver, which is why it is a
changelog entry rather than a migration.

- **Only two of the committed assertions are reliably red at base** — the n = 9 arm is green
  (at or below `vecBranch` the total is already order-independent), the honesty assertion is green
  (the base path-copy sum at n = 100 is 74 224–94 112 against a floor near 17 000: it guards the
  rejected charge-the-finished-structure mechanism, not this defect), and
  `TestHashMap_ConversionChargeIgnoresBuildOrder` is only probabilistically red, since both arms
  draw from the same randomised distribution and can coincide → the red claim rests on the
  8-repeat reproducibility assertions at n = 100 and n = 1000 alone, and the plan says so at the
  site so no stage reports a green arm as red evidence.
- **The cross-process half of the first scenario is argued, not asserted** — `go test` runs a
  package in one binary, so no committed test observes a second process → it holds by construction:
  `hashOfKey`'s FNV constants are fixed and `hashKey.less` is a strict total order over distinct
  keys, so the insertion sequence is identical in any process. Recorded as unasserted rather than
  covered.
- **The base figures exist only during c1's red stage** — c2 runs behind the fix, and no benchmark
  reaches `trieFromBuildMap` → c1's red stage records the per-repeat charges verbatim in its packet
  and c2 transcribes them into `tasks.md`; they cannot be recovered later.
- **`openspec/changes/DEPENDENCIES.md` records neither this change nor
  `hashmap-builder-trie-conversion`**, so the ordering between them lives only in prose, and that
  change's `tasks.md` cites `core/types.go` line ranges that shift once `trieFromBuildMap`'s body
  grows → both are follow-ups on that change, not on this one.

## Implementation plan

Tier **standard**, mode **existing-service-strict**, base `2638a704`. Four chunks: c1 and c2 are serial in `core`, c3 runs in parallel on its own shard, c4 closes behind c2. Every anchor below is a verbatim line at that base commit; a coder opens the file, finds the anchor string, and works from the `change` note. Line numbers are deliberately absent — they drift.

### `c1-charge-order` — tasks 1.1, 2.1

first, serial. Seam `conversion-charge`. Coder `go-coder`. Red tasks: 1.1. Code tasks: 2.1.

- **core/hashmap_test.go** — `TestHashMap_ConversionChargeIsReproducible` (task 1.1)
  - anchor: `func TestHashMap_TrieMatchesOracle(t *testing.T) {`
  - New test, placed as a sibling of TestHashMap_TrieMatchesOracle. Seeds one builder-form receiver via NewHashMap()+Set at n=9, n=100 and n=1000 (n=9 is asserted too — reproducibility must hold at every size — but carries no red guarantee, see below), asserts large.m != nil && large.root == nil && len(large.m) == n, then takes 8 consecutive Assoc calls against that same retained receiver WITHOUT reassigning it and requires every returned int64 charge to be identical with !=. Subcases: a Dissoc arm for the second conversion call site, a colliding-key arm seeded from findCollidingKeys, and the task-2.2 honesty assertion calling m.trieFromBuildMap() directly and requiring the charge to exceed retainedTrieBytes(root)+HashMapShallowBytes(n). retainedTrieBytes is NEW: a test-local helper summing hamtNodeBytes over the finished trie; it does not exist at baseSha. RED GUARANTEE: only the n=100 and n=1000 reproducibility assertions are reliably red at base. The n=9 arm is green at base (at or below vecBranch=32 the total is already order-independent), and the honesty assertion is green at base too (the base path-copy sum at n=100 is 74 224-94 112 against a floor near 17 000) — it guards the rejected charge-the-finished-structure mechanism, not this defect. Do not report either as red evidence. THE RED RUN IS ALSO THE ONLY EVIDENCE SOURCE FOR TASK 0.1: record the per-repeat charges it prints at n=9, 100 and 1000 verbatim in the packet of this chunk before any fix lands; c2 transcribes them into tasks.md and they cannot be re-measured afterwards.
- **core/hashmap_test.go** — `TestHashMap_ConversionChargeIgnoresBuildOrder` (task 1.1)
  - anchor: `func TestHashMap_LargeFormPrintsIndependentOfBuildOrder(t *testing.T) {`
  - New test, modelled on the adjacent TestHashMap_LargeFormPrintsIndependentOfBuildOrder but Set-built rather than Assoc-built. Seeds two builder-form receivers with the same n pairs in ascending and descending key order, asserts both are builder form, takes one Assoc against each with the same key and value, and requires the two charges to be exactly equal. Only probabilistically red at base — both arms draw from the same randomised distribution and can coincide — so the 8-repeat reproducibility test is what proves the red, and a green run of this one at base is not evidence the defect is absent.
- **core/types.go** — `trieFromBuildMap` (task 2.1)
  - anchor: `func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {`
  - Replace `for _, e := range h.large.m` with insertion through the entries sortedEntries() already returns in (typ, num, str) order, and charge the buffer it obtains as HashMapShallowBytes(n) on top of the unchanged path-copy sum — the helper in core/metering.go whose two constants are both ADR 0011 rows (Hash map header 32, Hash map entry 64), so no unowned unit is published; 24 + 64n is NOT the term — no single row owns a 24-byte header for an evaluator-side entry buffer. Reuse hashKey.less; do not add a second ordering. The receiver stays untouched and the resulting trie is unchanged in shape and contents.
- **core/types.go** — `hashKey.less / hashOfKey / sortedEntries` (task 2.1)
  - anchor: `func (hk hashKey) less(other hashKey) bool {`
  - Read-only: sortedEntries is the ordering the fix reuses. Note its small-form branch returns h.entries directly (aliases the receiver); trieFromBuildMap is reachable only under h.large != nil && h.large.root == nil so that branch is unreachable from it today.
- **core/types.go** — `Assoc` (task 2.1)
  - anchor: `func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {`
  - No structural edit — the conversion arm under `if root == nil` keeps its shape; only the value it returns moves.
- **core/types.go** — `Dissoc` (task 2.1)
  - anchor: `func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {`
  - No structural edit — same conversion arm as Assoc.

Red tests (binding — an absent name fails RED): `TestHashMap_ConversionChargeIsReproducible`, `TestHashMap_ConversionChargeIgnoresBuildOrder`

```sh
# red
go test -timeout 2m -run 'TestHashMap_ConversionCharge' ./core
# verify
go test -timeout 2m ./core && go vet ./core
```

### `c2-charge-evidence` — tasks 0.1, 2.2

serial behind `c1-charge-order` (shared package `core`). Seam `conversion-charge`. Coder `go-coder`. Red tasks: — (see the seam waiver). Code tasks: 0.1, 2.2.

- **core/types.go** — `eachRaw` (task 0.1)
  - anchor: `func (h *HashMap) eachRaw(fn func(e entry)) {`
  - No edit. Confirm the ALLOCATION-ledger claim only: boundedEquals (core/depth.go) takes no budget and charges nothing, and sortedEntries sorts before anything reads it. equalsBounded DOES charge reduction units in eachRaw order for unequal builder-form receivers; that is out of scope here and tracked by the change equals-bounded-reduction-charge-order.
- **openspec/changes/trie-conversion-charge-determinism/tasks.md** — `0. Baseline` (task 0.1)
  - anchor: `## 0. Baseline`
  - THE WRITABLE FILE OF THIS CHUNK. Transcribe the per-repeat charges the c1 red stage recorded at n = 9, 100 and 1000 as nested bullets under task 0.1, and the honesty and after figures under task 2.2. They cannot be re-measured here: core/types.go already carries the fix by the time this chunk runs, and no benchmark reaches trieFromBuildMap. Report the n = 9 row as the expected no-spread case (at or below vecBranch the total is already order-independent), not as a failure to reproduce the defect. This chunk is not green until both bullet sets exist.
- **core/types.go** — `trieFromBuildMap` (task 2.2)
  - anchor: `func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {`
  - No new edit beyond c1. Confirm the charge still sums hamtNodeBytes for every intermediate node the build allocates, not only the nodes the finished trie retains, and record the measured before/after conversion charge at n=9, 100 and 1000 in the task notes. The before figures come from c1 red stage output (task 0.1), not from a benchmark: no existing benchmark reaches trieFromBuildMap.

```sh
# verify
go test -timeout 2m ./core && go vet ./core && openspec validate trie-conversion-charge-determinism --strict --json
```

### `c3-charge-docs` — tasks 3.1

parallel, shard `docs`. Seam `charge-documentation`. Coder `coder`. Red tasks: — (see the seam waiver). Code tasks: 3.1.

- **docs/adr/0011-reduction-and-allocation-metering.md** — `Determinism requirement` (task 3.1)
  - anchor: `## Determinism requirement`
  - Extend the existing statement from "no runtime-derived measurement" to also forbid a charge derived from Go map iteration order. No new unit is published and no table row is added: the node term keeps the "Evaluator persistent-map node" row, and the entry buffer is HashMapShallowBytes(n), composed of the existing "Hash map header | 32 bytes" and "Hash map entry | 64 bytes per key/value pair" rows. Do NOT write 24 + 64n or MeterCollectionHeaderBytes into the ADR: no row owns a 24-byte header for an evaluator-side entry buffer, and this chunk runs in parallel with prev null, so it can reach the ADR before the code.
- **docs/adr/0008-consumer-performance-gate.md** — `runner comparability note` (task 3.1)
  - anchor: `Note (runner comparability): a latency conclusion is only sound when the`
  - Note that the gate's bytes and allocation-count axes read those figures directly rather than through benchstat, so they are only decidable while each charge reproduces for its input. Anchor near the existing note; the sentence "The bytes and / allocation-count axes no longer pass through benchstat at all" wraps across two source lines, so match on the second line.
- **CHANGELOG.md** — `[Unreleased] / Fixed` (task 3.1)
  - anchor: `- `json/decode` now charges its decoded result exactly once per call. Public`
  - New bullet under the ### Fixed section nested beneath ## [Unreleased] (### Fixed also appears under six released version headings, so anchor on the adjacent json/decode bullet, not on the heading text). Record that the builder-to-trie conversion charge is now reproducible and that the charged value moved; give the shape of the move, not an exact byte count, since it depends on the key set.

```sh
# verify
openspec validate trie-conversion-charge-determinism --strict --json
```

### `c4-floor-goldset` — tasks 3.2, 3.3

serial behind `c2-charge-evidence` (shared package `core`). Seam `floor-verification`. Coder `coder`. Red tasks: — (see the seam waiver). Code tasks: 3.2, 3.3.

- **internal/goldset/alloc_test.go** — `TestGoldsetVMAllocations` (task 3.3)
  - anchor: `func TestGoldsetVMAllocations(t *testing.T) {`
  - No edit — comparison target. Expected delta 0: no gold-set fixture builds a map above hashMapSmallLimit (the largest is the 3-key merge-config literal), so no cell reaches trieFromBuildMap. A moved count is a finding, and the report must name the fixture that reached the conversion.
- **internal/goldset/bench_test.go** — `BenchmarkGoldsetParse` (task 3.3)
  - anchor: `func BenchmarkGoldsetParse(b *testing.B) {`
  - No edit — comparison target, run in both GOLDSET_MODE=eval and GOLDSET_MODE=vm at the Makefile's gate-mirroring parameters (GOMAXPROCS=2, -benchtime=200ms). Report the bytes and allocation-count axes separately from timing; a local latency delta is not a gate verdict.

```sh
# verify
go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -timeout 2m ./internal/goldset -run TestGoldsetVMAllocations
```

### Seam contracts

#### `conversion-charge` — tasks 0.1, 1.1, 2.1, 2.2

Mechanism chosen: (a) data-derived insertion order. `trieFromBuildMap` stops ranging `h.large.m` and inserts through the entries `(*HashMap).sortedEntries()` already returns in (typ, num, str) order, charging the buffer it obtains as `HashMapShallowBytes(n)` on top of the unchanged path-copy sum. It reuses `hashKey.less` — the file's single ordering, already driving `find` and `sortedEntries` — so no second ordering enters the file, and the body of one function is the whole code diff. (b) one order-independent build was rejected: it is a trie-construction rewrite (bottom-up radix partition plus the shift>=32 collision case that `mergeEntries` owns today) whose trie-shape equality this change has no test surface for, it is far larger than the requirement it serves, and it would move the builder-form charge by two orders of magnitude inside the window in which `hashmap-builder-trie-conversion` is re-baselining the same numbers. (c) charge the finished structure is inadmissible as written: the delta spec's scenario `A reproducible charge is still an honest one` requires the charge to account for storage the operation actually obtained, and while the build still copies one root-to-leaf path per entry those copies are storage obtained and discarded; charging only the retained trie is exactly the under-bill that scenario names. Scope conflict, declared rather than absorbed: `hashmap-builder-trie-conversion` rewrites the same function, but its tasks 2.1-2.4 choose a per-value memo (`trieRoot()` helper, memo slot on `largeMap`, CAS publication, once-per-value charging), not a one-pass build — its own proposal states a one-pass build 'does not by itself reach the bound'. Mechanism (a) leaves that change's entire mechanism and argument intact and gives its paused before/after a reproducible baseline, which is what it is paused for. What is left of it: all of it, minus the sentence in its task 3.1 quoted under risks.

**States.** `builder-form` · `trie-form` · `small-form` · `reproducible-charge` · `no-charge`

**Transitions.**

- builder-form receiver above hashMapSmallLimit (h.large != nil, h.large.root == nil), one Assoc → `reproducible-charge` [forced] — core/types.go:trieFromBuildMap — `// trieFromBuildMap converts Set-built staging storage into trie form. Called` — the body's `for _, e := range h.large.m {` is the ranged Go map being replaced
- one retained builder-form receiver, 8 consecutive Assoc calls without reassigning it → `reproducible-charge` [forced] — spec scenario `One value converted repeatedly charges one number`; core/types.go:Assoc — `func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {`, whose returned bytes are the conversion term plus `next, b, added := root.assoc(e, hashOfKey(hk), 0)`
- two builder-form maps of equal contents, seeded by Set in ascending and descending key order, one Assoc each with the same key and value → `reproducible-charge` [forced] — spec scenario `Equal contents charge equally regardless of build order`; core/types.go — `// sortedEntries returns every entry in deterministic (typ, num, str) order.`
- receiver already in trie form (h.large.root != nil), one Assoc or Dissoc → `trie-form` [no-op] — core/types.go:Assoc — the conversion runs only under `if root == nil`, so the conversion term stays 0 and this change moves no charge on the trie path
- builder-form receiver holding a pair of keys with identical 32-bit hashes, one Assoc → `reproducible-charge` [forced] — core/types.go — `func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {` bottoms out into a collision node at `if shift >= 32 {`; the node's charge is `func hamtSizeBytes(entries, children int) int64 {`, a function of counts only, so the collided pair's entry order changes but its charge does not
- builder-form receiver, Dissoc instead of Assoc → `reproducible-charge` [forced] — core/types.go:Dissoc — `func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {`, whose conversion arm is byte-identical to Assoc's and whose own term is `next, b, removed := root.dissoc(hk, hashOfKey(hk), 0)`
- receiver in small form (h.large == nil, at or below hashMapSmallLimit keys), one Assoc → `small-form` [no-op] — core/types.go — `const hashMapSmallLimit = 8`; the small path returns `HashMapShallowBytes(len(entries))` and never enters the conversion
- builder-form map below the small limit → `small-form` [no-op] — unreachable by construction: core/types.go:Set promotes only at the 9th distinct key (`m := make(map[hashKey]entry, len(h.entries)+1)` then `h.large = &largeMap{m: m}`), and core/types.go contains no `delete(` at all, so `large.m` only grows and builder form always holds at least 9 entries. A test that appears to reach this class has seeded illegally
- unhashable key (a List) passed to Assoc or Dissoc on a builder-form receiver → `no-charge` [no-op] — core/types.go:Assoc — `hk, err := toHashKey(key)` returns `nil, 0, err` before the `h.large != nil` branch, so the conversion never runs and nothing is charged
- builder-form receiver grown by a further Set between two conversions → `reproducible-charge` [forced] — core/types.go:Set — `h.large.m[hk] = e`; the charge is a function of the contents at conversion time, never of the Set history that produced them
- the same seeded receiver converted in a fresh process → `reproducible-charge` [forced] — core/types.go — `// hashOfKey hashes a key with FNV-1a over fixed constants. The seed must not` vary per process, and `func (hk hashKey) less(other hashKey) bool {` is a strict total order over distinct hashKeys, so the insertion sequence is fixed across processes
- honesty probe: n-entry builder-form receiver, conversion term taken alone via trieFromBuildMap → `reproducible-charge` [forced] — spec scenario `A reproducible charge is still an honest one` — the charge must exceed retainedTrieBytes(root) + HashMapShallowBytes(n), because every one of the n inserts copies at least the root node and all but the last copy is discarded

**Forbidden.**

- Two conversions of equal-content builder-form maps charging different byte totals — within one process or across processes.
- A conversion charge equal to retainedTrieBytes(root) + the entry buffer: that is the under-bill the third scenario forbids, not a reproducible charge.
- Any charged accumulation whose value depends on `for _, e := range h.large.m` order.
- trieFromBuildMap writing to h.large.m, h.large.root, h.large.count or h.entries — the receiver stays untouched and concurrent readers of h stay unaffected.
- A second key ordering alongside hashKey.less (a sort keyed on hashOfKey would be one).
- A test reaching builder form by assigning h.large or h.large.m directly instead of driving Set.
- A test that reassigns its receiver between repeats — the second call would then run against trie form and measure nothing.
- Comparing charges with a tolerance, a ratio or a bound instead of int64 equality.
- Committing the reproducibility assertion at n = 9 only (see budgets: the base test can pass there).

**Seeding.**

- Builder form above the limit — the only legal path: `m := NewHashMap()`, then `m.Set(Int{V: int64(i)}, Int{V: int64(i)})` for i in [0, n) with n >= 9. Set promotes on the 9th distinct key and leaves `large.m` populated with `large.root` nil.
- Assert the form before measuring anything: `m.large != nil`, `m.large.root == nil`, `len(m.large.m) == n`. Package `core` tests are internal, so these fields are readable (`TestHashMap_PromotionBoundary` already reads `m.large`).
- Repeated conversion: call `m.Assoc(key, val)` in a loop and discard the result — Assoc never mutates its receiver, so `m` stays builder form and every call re-enters the conversion. Do not write `m, _, _ = m.Assoc(...)`.
- Different build orders: seed two receivers with the same n pairs, one ascending and one descending, both through Set; assert both are builder form, then take one Assoc against each. `TestHashMap_LargeFormPrintsIndependentOfBuildOrder` is the existing forward/backward pattern, written for Assoc rather than Set.
- Colliding keys: `findCollidingKeys(t)` (core/hashmap_test.go) returns the fixed Int pair whose hashes agree in every bit; Set both into the receiver alongside the filler keys before measuring.
- Trie form, for the skipped-conversion row: seed with repeated `Assoc` instead of Set — past the limit that yields `large.root != nil` — and assert `m.large.root != nil`.
- Conversion term in isolation, for the honesty assertion only: call `m.trieFromBuildMap()` directly from the internal test. The reproducibility rows go through the public Assoc/Dissoc charge instead, because that is what the requirement binds.

**Budgets.**

- Repeats: 8 conversions per receiver in TestHashMap_ConversionChargeIsReproducible. Derived from the change's recorded base measurement — three consecutive conversions at n=100 already differed (74 224 / 79 200 / 94 112) — with margin, at roughly 8x a sub-millisecond conversion.
- Sizes in the committed tests: n = 9, n = 100 and n = 1000. All three are asserted — reproducibility must hold at every size — but only n = 100 and n = 1000 carry a red guarantee; the n = 9 arm is green at base and exists so the red run also prints the charge task 0.1 records at that size. Omitting it makes 0.1 unsatisfiable and unrecoverable once the fix lands.
- Red guarantee: any size asserted on must exceed vecBranch (`vecBranch = 1 << vecBits`, 32). At n <= 32 the keys can occupy 32 distinct level-0 slots, in which case insert i charges 24 + 64*(i+1) whatever the order and the total is already order-independent — the base test would pass and the red stage would be false. At n >= 33 the pigeonhole forces at least one merge, whose parent-clone cost depends on the root's population when it happens.
- Honesty floor: conversion charge > retainedTrieBytes(root) + HashMapShallowBytes(n), asserted at n = 100 and n = 1000.
- Cross-process clause: unasserted by construction, not covered by a test. go test runs a package in one binary, so no committed test observes a second process; the clause holds because hashOfKey's FNV constants are fixed and hashKey.less is a strict total order over distinct keys. Record it as argued, never as asserted. Wall clock: both tests under 1 s. 8 conversions at n = 1000 is on the order of 52 000 node allocations, inside ./core's -timeout 2m with room to spare.
- Size of the move (Decision 2), analytically: new charge = HashMapShallowBytes(n) (MeterHashMapHeaderBytes 32 + 64n) for the entry buffer, plus the path-copy sum taken in hashKey.less order, plus the trailing assoc term. Each insert i charges, per node on its copied path, 24 + 64*entries + 8*children, over a depth of ceil(log32 n) plus any collision depth, so the sum stays O(n * ceil(log32 n)) node allocations — the same count as today. Allocation count rises by exactly one, the entries slice. Against the current unstable band the path-copy sum becomes one fixed member of it: at n=100 a fixed value inside [74 224, 94 112] plus 6 432; at n=1000 a fixed value inside [1 071 440, 1 084 640] plus 64 032. Which member cannot be derived without running it — task 2.2 records the measured after-figure.
- Published units (Decision 2): none change. The conversion's node charges stay owned by ADR 0011's row "| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |"; the new buffer term reuses "| Hash map entry | 64 bytes per key/value pair |" plus MeterHashMapHeaderBytes, the 32-byte constant HashMapShallowBytes already applies to every other []entry allocation in the file. The reproducibility statement joins the heading "## Determinism requirement", which today forbids only runtime-derived measurement and says nothing about iteration order.

**Names the tests spell exactly.**

- TestHashMap_ConversionChargeIsReproducible — package core, file core/hashmap_test.go
- TestHashMap_ConversionChargeIgnoresBuildOrder — package core, file core/hashmap_test.go
- setBuiltMap(t *testing.T, n int) *HashMap — NEW test helper; no existing helper builds a Set-built builder-form receiver anywhere in core
- retainedTrieBytes(root *hamtNode) int64 — NEW test helper, recursive sum of hamtNodeBytes over the finished trie, used by the honesty assertion
- findCollidingKeys(t *testing.T) (int64, int64) — EXISTING, core/hashmap_test.go
- (*HashMap).trieFromBuildMap — the only function whose body changes
- (*HashMap).sortedEntries — reused as-is, no signature change
- (hashKey).less — the reused comparator; no new ordering
- hamtNodeBytes, hamtSizeBytes, hashOfKey, hashMapSmallLimit, vecBranch — read by the tests, unchanged
- HashMapShallowBytes(n) (core/metering.go) — MeterHashMapHeaderBytes (32) + int64(n)*MeterHashMapEntryBytes (64); this is the buffer term, and 24 + 64n is NOT
- Error type: none. The conversion cannot fail; Assoc and Dissoc return an error only from toHashKey, so the tests use Int/Keyword keys and assert err == nil.
- Charge comparison: `!=` on int64, exact equality, never a bound.

#### `charge-documentation` — tasks 3.1

NO-RED-WAIVER: documentation-only, no observable behavior to assert. Add the reproducibility property to ADR 0011 under the existing heading `## Determinism requirement`, extending it from 'no runtime-derived measurement' to 'no charge derived from Go map iteration order'; note in ADR 0008 that the gate's allocation axis reads the bytes and allocation-count figures directly and is only decidable if those figures reproduce; record under `[Unreleased]` in CHANGELOG.md that the builder-to-trie conversion now charges a reproducible number and that the number moved, giving the shape of the move rather than a byte count. Verify no unit is published that no ADR table row owns: none is added — the node term keeps `| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |` and the entry buffer is priced by the existing HashMapShallowBytes helper from `| Hash map entry | 64 bytes per key/value pair |` plus MeterHashMapHeaderBytes, which is what HashMapShallowBytes composes.

**States.** `documented`

**Transitions.**

- ADR 0011 read after the change → `documented` [set] — docs/adr/0011-reduction-and-allocation-metering.md — `## Determinism requirement`, whose current text binds only `unsafe.Sizeof`, allocator classes, pointer width and map bucket layout
- ADR 0008 read after the change → `documented` [set] — docs/adr/0008-consumer-performance-gate.md — `allocation-count axes no longer pass through benchstat at all — the gate` — the axis that now depends on the stated property
- CHANGELOG.md `[Unreleased]` read after the change → `documented` [set] — change tasks 3.1

**Forbidden.**

- Publishing a new allocation unit that no ADR 0011 table row owns.
- Quoting an exact byte figure in the CHANGELOG for a value that depends on the key set.

**Seeding.**

- Not applicable — documentation seam.

**Budgets.**

- No numeric budget: no measurement is taken by this seam.

**Names the tests spell exactly.**

- docs/adr/0011-reduction-and-allocation-metering.md
- docs/adr/0008-consumer-performance-gate.md
- CHANGELOG.md

#### `floor-verification` — tasks 3.2, 3.3

NO-RED-WAIVER and NO-TESTER-WAIVER: evidence-only, it runs the floor and records it. The goldset comparison is expected to move nothing and the reason is checked, not assumed: no gold-set fixture builds a map above hashMapSmallLimit — the largest map literal in the corpus is the 3-key `(def base {:mode :tree :depth 10 :trace false})` in merge-config.lisp — so no cell reaches trieFromBuildMap in either evaluator mode. Report allocation evidence separately from timing; a local latency delta is not a gate verdict, while bytes and allocation counts are exact locally.

**States.** `verified`

**Transitions.**

- TestGoldsetVMAllocations after the change → `verified` [no-op] — internal/goldset/alloc_test.go — `func TestGoldsetVMAllocations(t *testing.T) {` pins per-fixture allocation counts; no fixture builds a map above 8 keys, so no pinned count may move
- BenchmarkGoldsetParse in GOLDSET_MODE=eval and GOLDSET_MODE=vm after the change → `verified` [no-op] — internal/goldset/bench_test.go — `func BenchmarkGoldsetParse(b *testing.B) {`; the reader builds no trie node, so the parse cells cannot reach the changed function
- openspec validate trie-conversion-charge-determinism --strict --json → `verified` [no-op] — change tasks 3.3

**Forbidden.**

- Reporting a local latency delta as a gate verdict.
- Recording a moved goldset allocation count as accepted without naming the fixture that reached the conversion.

**Seeding.**

- Not applicable — verification seam.

**Budgets.**

- Benchmark parameters mirror the release gate via the Makefile: GOMAXPROCS=2 (PROFILE_GOMAXPROCS) and -benchtime=200ms (PROFILE_BENCHTIME).
- Expected goldset delta: 0 on both the bytes and allocation-count axes, in both modes.

**Names the tests spell exactly.**

- BenchmarkGoldsetParse
- TestGoldsetVMAllocations
- internal/goldset
- GOLDSET_MODE

### Requirements map

| SHALL | Test |
| --- | --- |
| An allocation charge SHALL be a function of the input that produced it. | `TestHashMap_ConversionChargeIsReproducible`, `TestHashMap_ConversionChargeIgnoresBuildOrder` |
| Charging the same operation over the same value SHALL yield the same number of bytes every time, within one process and across processes, so that a difference between two measurements is evidence of a difference in the work. | `TestHashMap_ConversionChargeIsReproducible` |
| Where an operation's storage cost depends on the order in which it visits a collection, that order SHALL be derived from the data rather than from Go map iteration, which is randomised per range. | `TestHashMap_ConversionChargeIgnoresBuildOrder`, `TestHashMap_ConversionChargeIsReproducible` |
| An operation MAY visit in any order it likes; what it charges SHALL NOT depend on which order it chose. | `TestHashMap_ConversionChargeIgnoresBuildOrder` |
| every one of those updates SHALL charge the identical number of bytes for the conversion, and repeating the whole sequence in a new process SHALL charge that same number again | `TestHashMap_ConversionChargeIsReproducible` |
| both updates SHALL charge the same number of bytes, because the charge follows the contents and not the construction history | `TestHashMap_ConversionChargeIgnoresBuildOrder` |
| the charge SHALL account for the storage the operation actually obtained rather than only the storage it kept, so that reproducibility is not bought by under-billing | `TestHashMap_ConversionChargeIsReproducible` |

The cross-process half of the first scenario is argued, not asserted: `go test` runs a package in one binary. It holds because `hashOfKey`'s FNV constants are fixed and `hashKey.less` is a strict total order over distinct keys.

### Test harness already in `core`

- newTestEnv — core/map_determinism_test.go (used at line 11, defined elsewhere in the core test package) — builds a fresh eval Env for TestEval_MapLiteralDeterministic
- findCollidingKeys — core/hashmap_test.go:373 — searches Int keys for a fixed-seed hash collision, used by TestHashMap_HashCollisions; not relevant to build-order/Set construction
- TestHashMap_PromotionBoundary — core/hashmap_test.go:84 — builds a small-form map to exactly hashMapSmallLimit via Assoc, then one more Assoc to force promotion; promotes via Assoc, not Set, so the promoted map's large.root is already non-nil (never exercises trieFromBuildMap)
- TestHashMap_TrieMatchesOracle — core/hashmap_test.go:299 — 20000-step randomized Assoc/Dissoc/Get against a Go-map oracle, well past the small-map limit; builds via Assoc throughout, not Set, so it never leaves a map in builder (large.m, large.root == nil) form
- TestHashMap_LargeFormPrintsIndependentOfBuildOrder — core/hashmap_test.go:532 — closest existing pattern for the two new tests: builds two 200-entry maps (forward/backward key order) via repeated Assoc and compares String()/Equals(); NOT Set-built (large.root is populated incrementally by Assoc, trieFromBuildMap is never invoked here since large.m/large.root == nil never occurs above the limit in this test)
- TestVectorLedgerBytesIndependentOfLayout — core/metering_test.go:13 — pattern for asserting a ledger charge is representation-independent, on Vector (flat vs trie), not HashMap; structurally close to what 1.1's tests need but for a different type and a different property (charge-independent-of-layout vs charge-independent-of-build-order)
- BenchmarkHashMap_Assoc — core/bench_test.go:463 — single Keyword key against a fresh NewHashMap() every b.N iteration; never crosses hashMapSmallLimit, not a promotion benchmark
- BenchmarkHashMap_SetBuild — core/bench_test.go:531 — for n in {100,1000,10000}, builds a fresh map via repeated m.Set(Int,Int) each b.N iteration and discards it; demonstrates the Set-build recipe (`m := NewHashMap(); for j := range n { _ = m.Set(Int{V:int64(j)}, Int{V:int64(j)}) }`) but does not retain the map or ever call Assoc/Dissoc on it, so trieFromBuildMap is never exercised here
- BenchmarkHashMap_AssocChain — core/bench_test.go:513 — for n in {100,1000,10000}, threads a map through n immutable Assoc calls from empty; builds via Assoc not Set, never produces a builder-form (large.m) map
- No existing helper builds and retains a Set-built (builder-form, large.m != nil && large.root == nil) map above hashMapSmallLimit for a subsequent Assoc/Dissoc call. Both new tests in 1.1, and the baseline evidence in 0.1, must build one directly: NewHashMap(), call .Set(key, val) hashMapSmallLimit+1 or more times (BenchmarkHashMap_SetBuild's loop body is the only existing precedent for the Set call shape), keep the resulting *HashMap, and only then call Assoc/Dissoc to trigger trieFromBuildMap and read its charge.

### Floor

```sh
go test -timeout 2m -p 2 -parallel 2 ./core ./runtime
go test -race -timeout 2m -p 2 -parallel 2 ./core
golangci-lint run ./core/...
make build
make lint
make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'
go test -timeout 2m ./internal/goldset -run TestGoldsetVMAllocations
GOMAXPROCS=2 GOLDSET_MODE=eval go test -timeout 2m ./internal/goldset -run '^$' -bench BenchmarkGoldsetParse -benchtime=200ms -benchmem
GOMAXPROCS=2 GOLDSET_MODE=vm go test -timeout 2m ./internal/goldset -run '^$' -bench BenchmarkGoldsetParse -benchtime=200ms -benchmem
openspec validate trie-conversion-charge-determinism --strict --json
```

Run `golangci-lint` from the repository root: invoked with a foreign working directory it reports "No issues found" and exits 7 instead of linting.

### Review lenses

`spec`, `quality`, `perf` — `spec` and `quality` from the standard tier; `perf` because the diff adds an O(n log n) sort and one allocation to a metered path and moves a charge the release gate compares releases on. No `arch` (no package or boundary moves), no `sec` (no input, auth, crypto or I/O).

### Plan review

Reviewer `zarchitect`, 3 round(s), verdict **pass**.

## Plan appendix

```json
{
  "v": 2,
  "change": "trie-conversion-charge-determinism",
  "baseSha": "2638a7049f4a536fe1cc32d49f1069f9e4df3b83",
  "generatedAt": "2026-09-10T14:27:30.870Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "c1-charge-order",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "conversion-charge",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/hashmap_test.go",
          "symbol": "TestHashMap_ConversionChargeIsReproducible",
          "anchor": "func TestHashMap_TrieMatchesOracle(t *testing.T) {",
          "change": "New test, placed as a sibling of TestHashMap_TrieMatchesOracle. Seeds one builder-form receiver via NewHashMap()+Set at n=9, n=100 and n=1000 (n=9 is asserted too — reproducibility must hold at every size — but carries no red guarantee, see below), asserts large.m != nil && large.root == nil && len(large.m) == n, then takes 8 consecutive Assoc calls against that same retained receiver WITHOUT reassigning it and requires every returned int64 charge to be identical with !=. Subcases: a Dissoc arm for the second conversion call site, a colliding-key arm seeded from findCollidingKeys, and the task-2.2 honesty assertion calling m.trieFromBuildMap() directly and requiring the charge to exceed retainedTrieBytes(root)+HashMapShallowBytes(n). retainedTrieBytes is NEW: a test-local helper summing hamtNodeBytes over the finished trie; it does not exist at baseSha. RED GUARANTEE: only the n=100 and n=1000 reproducibility assertions are reliably red at base. The n=9 arm is green at base (at or below vecBranch=32 the total is already order-independent), and the honesty assertion is green at base too (the base path-copy sum at n=100 is 74 224-94 112 against a floor near 17 000) — it guards the rejected charge-the-finished-structure mechanism, not this defect. Do not report either as red evidence. THE RED RUN IS ALSO THE ONLY EVIDENCE SOURCE FOR TASK 0.1: record the per-repeat charges it prints at n=9, 100 and 1000 verbatim in the packet of this chunk before any fix lands; c2 transcribes them into tasks.md and they cannot be re-measured afterwards."
        },
        {
          "task": "1.1",
          "file": "core/hashmap_test.go",
          "symbol": "TestHashMap_ConversionChargeIgnoresBuildOrder",
          "anchor": "func TestHashMap_LargeFormPrintsIndependentOfBuildOrder(t *testing.T) {",
          "change": "New test, modelled on the adjacent TestHashMap_LargeFormPrintsIndependentOfBuildOrder but Set-built rather than Assoc-built. Seeds two builder-form receivers with the same n pairs in ascending and descending key order, asserts both are builder form, takes one Assoc against each with the same key and value, and requires the two charges to be exactly equal. Only probabilistically red at base — both arms draw from the same randomised distribution and can coincide — so the 8-repeat reproducibility test is what proves the red, and a green run of this one at base is not evidence the defect is absent."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "trieFromBuildMap",
          "anchor": "func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {",
          "change": "Replace `for _, e := range h.large.m` with insertion through the entries sortedEntries() already returns in (typ, num, str) order, and charge the buffer it obtains as HashMapShallowBytes(n) on top of the unchanged path-copy sum — the helper in core/metering.go whose two constants are both ADR 0011 rows (Hash map header 32, Hash map entry 64), so no unowned unit is published; 24 + 64n is NOT the term — no single row owns a 24-byte header for an evaluator-side entry buffer. Reuse hashKey.less; do not add a second ordering. The receiver stays untouched and the resulting trie is unchanged in shape and contents."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "hashKey.less / hashOfKey / sortedEntries",
          "anchor": "func (hk hashKey) less(other hashKey) bool {",
          "change": "Read-only: sortedEntries is the ordering the fix reuses. Note its small-form branch returns h.entries directly (aliases the receiver); trieFromBuildMap is reachable only under h.large != nil && h.large.root == nil so that branch is unreachable from it today."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "Assoc",
          "anchor": "func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {",
          "change": "No structural edit — the conversion arm under `if root == nil` keeps its shape; only the value it returns moves."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "Dissoc",
          "anchor": "func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {",
          "change": "No structural edit — same conversion arm as Assoc."
        }
      ],
      "contract": {
        "states": [
          "builder-form",
          "trie-form",
          "small-form",
          "reproducible-charge",
          "no-charge"
        ],
        "transitions": [
          {
            "input": "builder-form receiver above hashMapSmallLimit (h.large != nil, h.large.root == nil), one Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:trieFromBuildMap — `// trieFromBuildMap converts Set-built staging storage into trie form. Called` — the body's `for _, e := range h.large.m {` is the ranged Go map being replaced"
          },
          {
            "input": "one retained builder-form receiver, 8 consecutive Assoc calls without reassigning it",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `One value converted repeatedly charges one number`; core/types.go:Assoc — `func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {`, whose returned bytes are the conversion term plus `next, b, added := root.assoc(e, hashOfKey(hk), 0)`"
          },
          {
            "input": "two builder-form maps of equal contents, seeded by Set in ascending and descending key order, one Assoc each with the same key and value",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `Equal contents charge equally regardless of build order`; core/types.go — `// sortedEntries returns every entry in deterministic (typ, num, str) order.`"
          },
          {
            "input": "receiver already in trie form (h.large.root != nil), one Assoc or Dissoc",
            "state": "trie-form",
            "effect": "no-op",
            "evidence": "core/types.go:Assoc — the conversion runs only under `if root == nil`, so the conversion term stays 0 and this change moves no charge on the trie path"
          },
          {
            "input": "builder-form receiver holding a pair of keys with identical 32-bit hashes, one Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go — `func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {` bottoms out into a collision node at `if shift >= 32 {`; the node's charge is `func hamtSizeBytes(entries, children int) int64 {`, a function of counts only, so the collided pair's entry order changes but its charge does not"
          },
          {
            "input": "builder-form receiver, Dissoc instead of Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:Dissoc — `func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {`, whose conversion arm is byte-identical to Assoc's and whose own term is `next, b, removed := root.dissoc(hk, hashOfKey(hk), 0)`"
          },
          {
            "input": "receiver in small form (h.large == nil, at or below hashMapSmallLimit keys), one Assoc",
            "state": "small-form",
            "effect": "no-op",
            "evidence": "core/types.go — `const hashMapSmallLimit = 8`; the small path returns `HashMapShallowBytes(len(entries))` and never enters the conversion"
          },
          {
            "input": "builder-form map below the small limit",
            "state": "small-form",
            "effect": "no-op",
            "evidence": "unreachable by construction: core/types.go:Set promotes only at the 9th distinct key (`m := make(map[hashKey]entry, len(h.entries)+1)` then `h.large = &largeMap{m: m}`), and core/types.go contains no `delete(` at all, so `large.m` only grows and builder form always holds at least 9 entries. A test that appears to reach this class has seeded illegally"
          },
          {
            "input": "unhashable key (a List) passed to Assoc or Dissoc on a builder-form receiver",
            "state": "no-charge",
            "effect": "no-op",
            "evidence": "core/types.go:Assoc — `hk, err := toHashKey(key)` returns `nil, 0, err` before the `h.large != nil` branch, so the conversion never runs and nothing is charged"
          },
          {
            "input": "builder-form receiver grown by a further Set between two conversions",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:Set — `h.large.m[hk] = e`; the charge is a function of the contents at conversion time, never of the Set history that produced them"
          },
          {
            "input": "the same seeded receiver converted in a fresh process",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go — `// hashOfKey hashes a key with FNV-1a over fixed constants. The seed must not` vary per process, and `func (hk hashKey) less(other hashKey) bool {` is a strict total order over distinct hashKeys, so the insertion sequence is fixed across processes"
          },
          {
            "input": "honesty probe: n-entry builder-form receiver, conversion term taken alone via trieFromBuildMap",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `A reproducible charge is still an honest one` — the charge must exceed retainedTrieBytes(root) + HashMapShallowBytes(n), because every one of the n inserts copies at least the root node and all but the last copy is discarded"
          }
        ],
        "forbidden": [
          "Two conversions of equal-content builder-form maps charging different byte totals — within one process or across processes.",
          "A conversion charge equal to retainedTrieBytes(root) + the entry buffer: that is the under-bill the third scenario forbids, not a reproducible charge.",
          "Any charged accumulation whose value depends on `for _, e := range h.large.m` order.",
          "trieFromBuildMap writing to h.large.m, h.large.root, h.large.count or h.entries — the receiver stays untouched and concurrent readers of h stay unaffected.",
          "A second key ordering alongside hashKey.less (a sort keyed on hashOfKey would be one).",
          "A test reaching builder form by assigning h.large or h.large.m directly instead of driving Set.",
          "A test that reassigns its receiver between repeats — the second call would then run against trie form and measure nothing.",
          "Comparing charges with a tolerance, a ratio or a bound instead of int64 equality.",
          "Committing the reproducibility assertion at n = 9 only (see budgets: the base test can pass there)."
        ],
        "seeding": [
          "Builder form above the limit — the only legal path: `m := NewHashMap()`, then `m.Set(Int{V: int64(i)}, Int{V: int64(i)})` for i in [0, n) with n >= 9. Set promotes on the 9th distinct key and leaves `large.m` populated with `large.root` nil.",
          "Assert the form before measuring anything: `m.large != nil`, `m.large.root == nil`, `len(m.large.m) == n`. Package `core` tests are internal, so these fields are readable (`TestHashMap_PromotionBoundary` already reads `m.large`).",
          "Repeated conversion: call `m.Assoc(key, val)` in a loop and discard the result — Assoc never mutates its receiver, so `m` stays builder form and every call re-enters the conversion. Do not write `m, _, _ = m.Assoc(...)`.",
          "Different build orders: seed two receivers with the same n pairs, one ascending and one descending, both through Set; assert both are builder form, then take one Assoc against each. `TestHashMap_LargeFormPrintsIndependentOfBuildOrder` is the existing forward/backward pattern, written for Assoc rather than Set.",
          "Colliding keys: `findCollidingKeys(t)` (core/hashmap_test.go) returns the fixed Int pair whose hashes agree in every bit; Set both into the receiver alongside the filler keys before measuring.",
          "Trie form, for the skipped-conversion row: seed with repeated `Assoc` instead of Set — past the limit that yields `large.root != nil` — and assert `m.large.root != nil`.",
          "Conversion term in isolation, for the honesty assertion only: call `m.trieFromBuildMap()` directly from the internal test. The reproducibility rows go through the public Assoc/Dissoc charge instead, because that is what the requirement binds."
        ],
        "budgets": [
          "Repeats: 8 conversions per receiver in TestHashMap_ConversionChargeIsReproducible. Derived from the change's recorded base measurement — three consecutive conversions at n=100 already differed (74 224 / 79 200 / 94 112) — with margin, at roughly 8x a sub-millisecond conversion.",
          "Sizes in the committed tests: n = 9, n = 100 and n = 1000. All three are asserted — reproducibility must hold at every size — but only n = 100 and n = 1000 carry a red guarantee; the n = 9 arm is green at base and exists so the red run also prints the charge task 0.1 records at that size. Omitting it makes 0.1 unsatisfiable and unrecoverable once the fix lands.",
          "Red guarantee: any size asserted on must exceed vecBranch (`vecBranch = 1 << vecBits`, 32). At n <= 32 the keys can occupy 32 distinct level-0 slots, in which case insert i charges 24 + 64*(i+1) whatever the order and the total is already order-independent — the base test would pass and the red stage would be false. At n >= 33 the pigeonhole forces at least one merge, whose parent-clone cost depends on the root's population when it happens.",
          "Honesty floor: conversion charge > retainedTrieBytes(root) + HashMapShallowBytes(n), asserted at n = 100 and n = 1000.",
          "Cross-process clause: unasserted by construction, not covered by a test. go test runs a package in one binary, so no committed test observes a second process; the clause holds because hashOfKey's FNV constants are fixed and hashKey.less is a strict total order over distinct keys. Record it as argued, never as asserted. Wall clock: both tests under 1 s. 8 conversions at n = 1000 is on the order of 52 000 node allocations, inside ./core's -timeout 2m with room to spare.",
          "Size of the move (Decision 2), analytically: new charge = HashMapShallowBytes(n) (MeterHashMapHeaderBytes 32 + 64n) for the entry buffer, plus the path-copy sum taken in hashKey.less order, plus the trailing assoc term. Each insert i charges, per node on its copied path, 24 + 64*entries + 8*children, over a depth of ceil(log32 n) plus any collision depth, so the sum stays O(n * ceil(log32 n)) node allocations — the same count as today. Allocation count rises by exactly one, the entries slice. Against the current unstable band the path-copy sum becomes one fixed member of it: at n=100 a fixed value inside [74 224, 94 112] plus 6 432; at n=1000 a fixed value inside [1 071 440, 1 084 640] plus 64 032. Which member cannot be derived without running it — task 2.2 records the measured after-figure.",
          "Published units (Decision 2): none change. The conversion's node charges stay owned by ADR 0011's row \"| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |\"; the new buffer term reuses \"| Hash map entry | 64 bytes per key/value pair |\" plus MeterHashMapHeaderBytes, the 32-byte constant HashMapShallowBytes already applies to every other []entry allocation in the file. The reproducibility statement joins the heading \"## Determinism requirement\", which today forbids only runtime-derived measurement and says nothing about iteration order."
        ],
        "names": [
          "TestHashMap_ConversionChargeIsReproducible — package core, file core/hashmap_test.go",
          "TestHashMap_ConversionChargeIgnoresBuildOrder — package core, file core/hashmap_test.go",
          "setBuiltMap(t *testing.T, n int) *HashMap — NEW test helper; no existing helper builds a Set-built builder-form receiver anywhere in core",
          "retainedTrieBytes(root *hamtNode) int64 — NEW test helper, recursive sum of hamtNodeBytes over the finished trie, used by the honesty assertion",
          "findCollidingKeys(t *testing.T) (int64, int64) — EXISTING, core/hashmap_test.go",
          "(*HashMap).trieFromBuildMap — the only function whose body changes",
          "(*HashMap).sortedEntries — reused as-is, no signature change",
          "(hashKey).less — the reused comparator; no new ordering",
          "hamtNodeBytes, hamtSizeBytes, hashOfKey, hashMapSmallLimit, vecBranch — read by the tests, unchanged",
          "HashMapShallowBytes(n) (core/metering.go) — MeterHashMapHeaderBytes (32) + int64(n)*MeterHashMapEntryBytes (64); this is the buffer term, and 24 + 64n is NOT",
          "Error type: none. The conversion cannot fail; Assoc and Dissoc return an error only from toHashKey, so the tests use Int/Keyword keys and assert err == nil.",
          "Charge comparison: `!=` on int64, exact equality, never a bound."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [
        "TestHashMap_ConversionChargeIsReproducible",
        "TestHashMap_ConversionChargeIgnoresBuildOrder"
      ],
      "redRun": "go test -timeout 2m -run 'TestHashMap_ConversionCharge' ./core",
      "verify": "go test -timeout 2m ./core && go vet ./core",
      "coder": "go-coder"
    },
    {
      "id": "c2-charge-evidence",
      "taskIds": [
        "0.1",
        "2.2"
      ],
      "prev": "c1-charge-order",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "conversion-charge",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "0.1",
          "file": "core/types.go",
          "symbol": "eachRaw",
          "anchor": "func (h *HashMap) eachRaw(fn func(e entry)) {",
          "change": "No edit. Confirm the ALLOCATION-ledger claim only: boundedEquals (core/depth.go) takes no budget and charges nothing, and sortedEntries sorts before anything reads it. equalsBounded DOES charge reduction units in eachRaw order for unequal builder-form receivers; that is out of scope here and tracked by the change equals-bounded-reduction-charge-order."
        },
        {
          "task": "0.1",
          "file": "openspec/changes/trie-conversion-charge-determinism/tasks.md",
          "symbol": "0. Baseline",
          "anchor": "## 0. Baseline",
          "change": "THE WRITABLE FILE OF THIS CHUNK. Transcribe the per-repeat charges the c1 red stage recorded at n = 9, 100 and 1000 as nested bullets under task 0.1, and the honesty and after figures under task 2.2. They cannot be re-measured here: core/types.go already carries the fix by the time this chunk runs, and no benchmark reaches trieFromBuildMap. Report the n = 9 row as the expected no-spread case (at or below vecBranch the total is already order-independent), not as a failure to reproduce the defect. This chunk is not green until both bullet sets exist."
        },
        {
          "task": "2.2",
          "file": "core/types.go",
          "symbol": "trieFromBuildMap",
          "anchor": "func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {",
          "change": "No new edit beyond c1. Confirm the charge still sums hamtNodeBytes for every intermediate node the build allocates, not only the nodes the finished trie retains, and record the measured before/after conversion charge at n=9, 100 and 1000 in the task notes. The before figures come from c1 red stage output (task 0.1), not from a benchmark: no existing benchmark reaches trieFromBuildMap."
        }
      ],
      "contract": {
        "states": [
          "builder-form",
          "trie-form",
          "small-form",
          "reproducible-charge",
          "no-charge"
        ],
        "transitions": [
          {
            "input": "builder-form receiver above hashMapSmallLimit (h.large != nil, h.large.root == nil), one Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:trieFromBuildMap — `// trieFromBuildMap converts Set-built staging storage into trie form. Called` — the body's `for _, e := range h.large.m {` is the ranged Go map being replaced"
          },
          {
            "input": "one retained builder-form receiver, 8 consecutive Assoc calls without reassigning it",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `One value converted repeatedly charges one number`; core/types.go:Assoc — `func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {`, whose returned bytes are the conversion term plus `next, b, added := root.assoc(e, hashOfKey(hk), 0)`"
          },
          {
            "input": "two builder-form maps of equal contents, seeded by Set in ascending and descending key order, one Assoc each with the same key and value",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `Equal contents charge equally regardless of build order`; core/types.go — `// sortedEntries returns every entry in deterministic (typ, num, str) order.`"
          },
          {
            "input": "receiver already in trie form (h.large.root != nil), one Assoc or Dissoc",
            "state": "trie-form",
            "effect": "no-op",
            "evidence": "core/types.go:Assoc — the conversion runs only under `if root == nil`, so the conversion term stays 0 and this change moves no charge on the trie path"
          },
          {
            "input": "builder-form receiver holding a pair of keys with identical 32-bit hashes, one Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go — `func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {` bottoms out into a collision node at `if shift >= 32 {`; the node's charge is `func hamtSizeBytes(entries, children int) int64 {`, a function of counts only, so the collided pair's entry order changes but its charge does not"
          },
          {
            "input": "builder-form receiver, Dissoc instead of Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:Dissoc — `func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {`, whose conversion arm is byte-identical to Assoc's and whose own term is `next, b, removed := root.dissoc(hk, hashOfKey(hk), 0)`"
          },
          {
            "input": "receiver in small form (h.large == nil, at or below hashMapSmallLimit keys), one Assoc",
            "state": "small-form",
            "effect": "no-op",
            "evidence": "core/types.go — `const hashMapSmallLimit = 8`; the small path returns `HashMapShallowBytes(len(entries))` and never enters the conversion"
          },
          {
            "input": "builder-form map below the small limit",
            "state": "small-form",
            "effect": "no-op",
            "evidence": "unreachable by construction: core/types.go:Set promotes only at the 9th distinct key (`m := make(map[hashKey]entry, len(h.entries)+1)` then `h.large = &largeMap{m: m}`), and core/types.go contains no `delete(` at all, so `large.m` only grows and builder form always holds at least 9 entries. A test that appears to reach this class has seeded illegally"
          },
          {
            "input": "unhashable key (a List) passed to Assoc or Dissoc on a builder-form receiver",
            "state": "no-charge",
            "effect": "no-op",
            "evidence": "core/types.go:Assoc — `hk, err := toHashKey(key)` returns `nil, 0, err` before the `h.large != nil` branch, so the conversion never runs and nothing is charged"
          },
          {
            "input": "builder-form receiver grown by a further Set between two conversions",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:Set — `h.large.m[hk] = e`; the charge is a function of the contents at conversion time, never of the Set history that produced them"
          },
          {
            "input": "the same seeded receiver converted in a fresh process",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go — `// hashOfKey hashes a key with FNV-1a over fixed constants. The seed must not` vary per process, and `func (hk hashKey) less(other hashKey) bool {` is a strict total order over distinct hashKeys, so the insertion sequence is fixed across processes"
          },
          {
            "input": "honesty probe: n-entry builder-form receiver, conversion term taken alone via trieFromBuildMap",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `A reproducible charge is still an honest one` — the charge must exceed retainedTrieBytes(root) + HashMapShallowBytes(n), because every one of the n inserts copies at least the root node and all but the last copy is discarded"
          }
        ],
        "forbidden": [
          "Two conversions of equal-content builder-form maps charging different byte totals — within one process or across processes.",
          "A conversion charge equal to retainedTrieBytes(root) + the entry buffer: that is the under-bill the third scenario forbids, not a reproducible charge.",
          "Any charged accumulation whose value depends on `for _, e := range h.large.m` order.",
          "trieFromBuildMap writing to h.large.m, h.large.root, h.large.count or h.entries — the receiver stays untouched and concurrent readers of h stay unaffected.",
          "A second key ordering alongside hashKey.less (a sort keyed on hashOfKey would be one).",
          "A test reaching builder form by assigning h.large or h.large.m directly instead of driving Set.",
          "A test that reassigns its receiver between repeats — the second call would then run against trie form and measure nothing.",
          "Comparing charges with a tolerance, a ratio or a bound instead of int64 equality.",
          "Committing the reproducibility assertion at n = 9 only (see budgets: the base test can pass there)."
        ],
        "seeding": [
          "Builder form above the limit — the only legal path: `m := NewHashMap()`, then `m.Set(Int{V: int64(i)}, Int{V: int64(i)})` for i in [0, n) with n >= 9. Set promotes on the 9th distinct key and leaves `large.m` populated with `large.root` nil.",
          "Assert the form before measuring anything: `m.large != nil`, `m.large.root == nil`, `len(m.large.m) == n`. Package `core` tests are internal, so these fields are readable (`TestHashMap_PromotionBoundary` already reads `m.large`).",
          "Repeated conversion: call `m.Assoc(key, val)` in a loop and discard the result — Assoc never mutates its receiver, so `m` stays builder form and every call re-enters the conversion. Do not write `m, _, _ = m.Assoc(...)`.",
          "Different build orders: seed two receivers with the same n pairs, one ascending and one descending, both through Set; assert both are builder form, then take one Assoc against each. `TestHashMap_LargeFormPrintsIndependentOfBuildOrder` is the existing forward/backward pattern, written for Assoc rather than Set.",
          "Colliding keys: `findCollidingKeys(t)` (core/hashmap_test.go) returns the fixed Int pair whose hashes agree in every bit; Set both into the receiver alongside the filler keys before measuring.",
          "Trie form, for the skipped-conversion row: seed with repeated `Assoc` instead of Set — past the limit that yields `large.root != nil` — and assert `m.large.root != nil`.",
          "Conversion term in isolation, for the honesty assertion only: call `m.trieFromBuildMap()` directly from the internal test. The reproducibility rows go through the public Assoc/Dissoc charge instead, because that is what the requirement binds."
        ],
        "budgets": [
          "Repeats: 8 conversions per receiver in TestHashMap_ConversionChargeIsReproducible. Derived from the change's recorded base measurement — three consecutive conversions at n=100 already differed (74 224 / 79 200 / 94 112) — with margin, at roughly 8x a sub-millisecond conversion.",
          "Sizes in the committed tests: n = 9, n = 100 and n = 1000. All three are asserted — reproducibility must hold at every size — but only n = 100 and n = 1000 carry a red guarantee; the n = 9 arm is green at base and exists so the red run also prints the charge task 0.1 records at that size. Omitting it makes 0.1 unsatisfiable and unrecoverable once the fix lands.",
          "Red guarantee: any size asserted on must exceed vecBranch (`vecBranch = 1 << vecBits`, 32). At n <= 32 the keys can occupy 32 distinct level-0 slots, in which case insert i charges 24 + 64*(i+1) whatever the order and the total is already order-independent — the base test would pass and the red stage would be false. At n >= 33 the pigeonhole forces at least one merge, whose parent-clone cost depends on the root's population when it happens.",
          "Honesty floor: conversion charge > retainedTrieBytes(root) + HashMapShallowBytes(n), asserted at n = 100 and n = 1000.",
          "Cross-process clause: unasserted by construction, not covered by a test. go test runs a package in one binary, so no committed test observes a second process; the clause holds because hashOfKey's FNV constants are fixed and hashKey.less is a strict total order over distinct keys. Record it as argued, never as asserted. Wall clock: both tests under 1 s. 8 conversions at n = 1000 is on the order of 52 000 node allocations, inside ./core's -timeout 2m with room to spare.",
          "Size of the move (Decision 2), analytically: new charge = HashMapShallowBytes(n) (MeterHashMapHeaderBytes 32 + 64n) for the entry buffer, plus the path-copy sum taken in hashKey.less order, plus the trailing assoc term. Each insert i charges, per node on its copied path, 24 + 64*entries + 8*children, over a depth of ceil(log32 n) plus any collision depth, so the sum stays O(n * ceil(log32 n)) node allocations — the same count as today. Allocation count rises by exactly one, the entries slice. Against the current unstable band the path-copy sum becomes one fixed member of it: at n=100 a fixed value inside [74 224, 94 112] plus 6 432; at n=1000 a fixed value inside [1 071 440, 1 084 640] plus 64 032. Which member cannot be derived without running it — task 2.2 records the measured after-figure.",
          "Published units (Decision 2): none change. The conversion's node charges stay owned by ADR 0011's row \"| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |\"; the new buffer term reuses \"| Hash map entry | 64 bytes per key/value pair |\" plus MeterHashMapHeaderBytes, the 32-byte constant HashMapShallowBytes already applies to every other []entry allocation in the file. The reproducibility statement joins the heading \"## Determinism requirement\", which today forbids only runtime-derived measurement and says nothing about iteration order."
        ],
        "names": [
          "TestHashMap_ConversionChargeIsReproducible — package core, file core/hashmap_test.go",
          "TestHashMap_ConversionChargeIgnoresBuildOrder — package core, file core/hashmap_test.go",
          "setBuiltMap(t *testing.T, n int) *HashMap — NEW test helper; no existing helper builds a Set-built builder-form receiver anywhere in core",
          "retainedTrieBytes(root *hamtNode) int64 — NEW test helper, recursive sum of hamtNodeBytes over the finished trie, used by the honesty assertion",
          "findCollidingKeys(t *testing.T) (int64, int64) — EXISTING, core/hashmap_test.go",
          "(*HashMap).trieFromBuildMap — the only function whose body changes",
          "(*HashMap).sortedEntries — reused as-is, no signature change",
          "(hashKey).less — the reused comparator; no new ordering",
          "hamtNodeBytes, hamtSizeBytes, hashOfKey, hashMapSmallLimit, vecBranch — read by the tests, unchanged",
          "HashMapShallowBytes(n) (core/metering.go) — MeterHashMapHeaderBytes (32) + int64(n)*MeterHashMapEntryBytes (64); this is the buffer term, and 24 + 64n is NOT",
          "Error type: none. The conversion cannot fail; Assoc and Dissoc return an error only from toHashKey, so the tests use Int/Keyword keys and assert err == nil.",
          "Charge comparison: `!=` on int64, exact equality, never a bound."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "0.1",
        "2.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./core && go vet ./core && openspec validate trie-conversion-charge-determinism --strict --json",
      "coder": "go-coder"
    },
    {
      "id": "c3-charge-docs",
      "taskIds": [
        "3.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "charge-documentation",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Determinism requirement",
          "anchor": "## Determinism requirement",
          "change": "Extend the existing statement from \"no runtime-derived measurement\" to also forbid a charge derived from Go map iteration order. No new unit is published and no table row is added: the node term keeps the \"Evaluator persistent-map node\" row, and the entry buffer is HashMapShallowBytes(n), composed of the existing \"Hash map header | 32 bytes\" and \"Hash map entry | 64 bytes per key/value pair\" rows. Do NOT write 24 + 64n or MeterCollectionHeaderBytes into the ADR: no row owns a 24-byte header for an evaluator-side entry buffer, and this chunk runs in parallel with prev null, so it can reach the ADR before the code."
        },
        {
          "task": "3.1",
          "file": "docs/adr/0008-consumer-performance-gate.md",
          "symbol": "runner comparability note",
          "anchor": "Note (runner comparability): a latency conclusion is only sound when the",
          "change": "Note that the gate's bytes and allocation-count axes read those figures directly rather than through benchstat, so they are only decidable while each charge reproduces for its input. Anchor near the existing note; the sentence \"The bytes and / allocation-count axes no longer pass through benchstat at all\" wraps across two source lines, so match on the second line."
        },
        {
          "task": "3.1",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased] / Fixed",
          "anchor": "- `json/decode` now charges its decoded result exactly once per call. Public",
          "change": "New bullet under the ### Fixed section nested beneath ## [Unreleased] (### Fixed also appears under six released version headings, so anchor on the adjacent json/decode bullet, not on the heading text). Record that the builder-to-trie conversion charge is now reproducible and that the charged value moved; give the shape of the move, not an exact byte count, since it depends on the key set."
        }
      ],
      "contract": {
        "states": [
          "documented"
        ],
        "transitions": [
          {
            "input": "ADR 0011 read after the change",
            "state": "documented",
            "effect": "set",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md — `## Determinism requirement`, whose current text binds only `unsafe.Sizeof`, allocator classes, pointer width and map bucket layout"
          },
          {
            "input": "ADR 0008 read after the change",
            "state": "documented",
            "effect": "set",
            "evidence": "docs/adr/0008-consumer-performance-gate.md — `allocation-count axes no longer pass through benchstat at all — the gate` — the axis that now depends on the stated property"
          },
          {
            "input": "CHANGELOG.md `[Unreleased]` read after the change",
            "state": "documented",
            "effect": "set",
            "evidence": "change tasks 3.1"
          }
        ],
        "forbidden": [
          "Publishing a new allocation unit that no ADR 0011 table row owns.",
          "Quoting an exact byte figure in the CHANGELOG for a value that depends on the key set."
        ],
        "seeding": [
          "Not applicable — documentation seam."
        ],
        "budgets": [
          "No numeric budget: no measurement is taken by this seam."
        ],
        "names": [
          "docs/adr/0011-reduction-and-allocation-metering.md",
          "docs/adr/0008-consumer-performance-gate.md",
          "CHANGELOG.md"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "openspec validate trie-conversion-charge-determinism --strict --json",
      "coder": "coder"
    },
    {
      "id": "c4-floor-goldset",
      "taskIds": [
        "3.2",
        "3.3"
      ],
      "prev": "c2-charge-evidence",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "floor-verification",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core",
        "./runtime",
        "./internal/goldset"
      ],
      "sites": [
        {
          "task": "3.3",
          "file": "internal/goldset/alloc_test.go",
          "symbol": "TestGoldsetVMAllocations",
          "anchor": "func TestGoldsetVMAllocations(t *testing.T) {",
          "change": "No edit — comparison target. Expected delta 0: no gold-set fixture builds a map above hashMapSmallLimit (the largest is the 3-key merge-config literal), so no cell reaches trieFromBuildMap. A moved count is a finding, and the report must name the fixture that reached the conversion."
        },
        {
          "task": "3.3",
          "file": "internal/goldset/bench_test.go",
          "symbol": "BenchmarkGoldsetParse",
          "anchor": "func BenchmarkGoldsetParse(b *testing.B) {",
          "change": "No edit — comparison target, run in both GOLDSET_MODE=eval and GOLDSET_MODE=vm at the Makefile's gate-mirroring parameters (GOMAXPROCS=2, -benchtime=200ms). Report the bytes and allocation-count axes separately from timing; a local latency delta is not a gate verdict."
        }
      ],
      "contract": {
        "states": [
          "verified"
        ],
        "transitions": [
          {
            "input": "TestGoldsetVMAllocations after the change",
            "state": "verified",
            "effect": "no-op",
            "evidence": "internal/goldset/alloc_test.go — `func TestGoldsetVMAllocations(t *testing.T) {` pins per-fixture allocation counts; no fixture builds a map above 8 keys, so no pinned count may move"
          },
          {
            "input": "BenchmarkGoldsetParse in GOLDSET_MODE=eval and GOLDSET_MODE=vm after the change",
            "state": "verified",
            "effect": "no-op",
            "evidence": "internal/goldset/bench_test.go — `func BenchmarkGoldsetParse(b *testing.B) {`; the reader builds no trie node, so the parse cells cannot reach the changed function"
          },
          {
            "input": "openspec validate trie-conversion-charge-determinism --strict --json",
            "state": "verified",
            "effect": "no-op",
            "evidence": "change tasks 3.3"
          }
        ],
        "forbidden": [
          "Reporting a local latency delta as a gate verdict.",
          "Recording a moved goldset allocation count as accepted without naming the fixture that reached the conversion."
        ],
        "seeding": [
          "Not applicable — verification seam."
        ],
        "budgets": [
          "Benchmark parameters mirror the release gate via the Makefile: GOMAXPROCS=2 (PROFILE_GOMAXPROCS) and -benchtime=200ms (PROFILE_BENCHTIME).",
          "Expected goldset delta: 0 on both the bytes and allocation-count axes, in both modes."
        ],
        "names": [
          "BenchmarkGoldsetParse",
          "TestGoldsetVMAllocations",
          "internal/goldset",
          "GOLDSET_MODE"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.2",
        "3.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -timeout 2m ./internal/goldset -run TestGoldsetVMAllocations",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "conversion-charge",
      "tasks": [
        "0.1",
        "1.1",
        "2.1",
        "2.2"
      ],
      "summary": "Mechanism chosen: (a) data-derived insertion order. `trieFromBuildMap` stops ranging `h.large.m` and inserts through the entries `(*HashMap).sortedEntries()` already returns in (typ, num, str) order, charging the buffer it obtains as `HashMapShallowBytes(n)` on top of the unchanged path-copy sum. It reuses `hashKey.less` — the file's single ordering, already driving `find` and `sortedEntries` — so no second ordering enters the file, and the body of one function is the whole code diff. (b) one order-independent build was rejected: it is a trie-construction rewrite (bottom-up radix partition plus the shift>=32 collision case that `mergeEntries` owns today) whose trie-shape equality this change has no test surface for, it is far larger than the requirement it serves, and it would move the builder-form charge by two orders of magnitude inside the window in which `hashmap-builder-trie-conversion` is re-baselining the same numbers. (c) charge the finished structure is inadmissible as written: the delta spec's scenario `A reproducible charge is still an honest one` requires the charge to account for storage the operation actually obtained, and while the build still copies one root-to-leaf path per entry those copies are storage obtained and discarded; charging only the retained trie is exactly the under-bill that scenario names. Scope conflict, declared rather than absorbed: `hashmap-builder-trie-conversion` rewrites the same function, but its tasks 2.1-2.4 choose a per-value memo (`trieRoot()` helper, memo slot on `largeMap`, CAS publication, once-per-value charging), not a one-pass build — its own proposal states a one-pass build 'does not by itself reach the bound'. Mechanism (a) leaves that change's entire mechanism and argument intact and gives its paused before/after a reproducible baseline, which is what it is paused for. What is left of it: all of it, minus the sentence in its task 3.1 quoted under risks.",
      "contract": {
        "states": [
          "builder-form",
          "trie-form",
          "small-form",
          "reproducible-charge",
          "no-charge"
        ],
        "transitions": [
          {
            "input": "builder-form receiver above hashMapSmallLimit (h.large != nil, h.large.root == nil), one Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:trieFromBuildMap — `// trieFromBuildMap converts Set-built staging storage into trie form. Called` — the body's `for _, e := range h.large.m {` is the ranged Go map being replaced"
          },
          {
            "input": "one retained builder-form receiver, 8 consecutive Assoc calls without reassigning it",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `One value converted repeatedly charges one number`; core/types.go:Assoc — `func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {`, whose returned bytes are the conversion term plus `next, b, added := root.assoc(e, hashOfKey(hk), 0)`"
          },
          {
            "input": "two builder-form maps of equal contents, seeded by Set in ascending and descending key order, one Assoc each with the same key and value",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `Equal contents charge equally regardless of build order`; core/types.go — `// sortedEntries returns every entry in deterministic (typ, num, str) order.`"
          },
          {
            "input": "receiver already in trie form (h.large.root != nil), one Assoc or Dissoc",
            "state": "trie-form",
            "effect": "no-op",
            "evidence": "core/types.go:Assoc — the conversion runs only under `if root == nil`, so the conversion term stays 0 and this change moves no charge on the trie path"
          },
          {
            "input": "builder-form receiver holding a pair of keys with identical 32-bit hashes, one Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go — `func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {` bottoms out into a collision node at `if shift >= 32 {`; the node's charge is `func hamtSizeBytes(entries, children int) int64 {`, a function of counts only, so the collided pair's entry order changes but its charge does not"
          },
          {
            "input": "builder-form receiver, Dissoc instead of Assoc",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:Dissoc — `func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {`, whose conversion arm is byte-identical to Assoc's and whose own term is `next, b, removed := root.dissoc(hk, hashOfKey(hk), 0)`"
          },
          {
            "input": "receiver in small form (h.large == nil, at or below hashMapSmallLimit keys), one Assoc",
            "state": "small-form",
            "effect": "no-op",
            "evidence": "core/types.go — `const hashMapSmallLimit = 8`; the small path returns `HashMapShallowBytes(len(entries))` and never enters the conversion"
          },
          {
            "input": "builder-form map below the small limit",
            "state": "small-form",
            "effect": "no-op",
            "evidence": "unreachable by construction: core/types.go:Set promotes only at the 9th distinct key (`m := make(map[hashKey]entry, len(h.entries)+1)` then `h.large = &largeMap{m: m}`), and core/types.go contains no `delete(` at all, so `large.m` only grows and builder form always holds at least 9 entries. A test that appears to reach this class has seeded illegally"
          },
          {
            "input": "unhashable key (a List) passed to Assoc or Dissoc on a builder-form receiver",
            "state": "no-charge",
            "effect": "no-op",
            "evidence": "core/types.go:Assoc — `hk, err := toHashKey(key)` returns `nil, 0, err` before the `h.large != nil` branch, so the conversion never runs and nothing is charged"
          },
          {
            "input": "builder-form receiver grown by a further Set between two conversions",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go:Set — `h.large.m[hk] = e`; the charge is a function of the contents at conversion time, never of the Set history that produced them"
          },
          {
            "input": "the same seeded receiver converted in a fresh process",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "core/types.go — `// hashOfKey hashes a key with FNV-1a over fixed constants. The seed must not` vary per process, and `func (hk hashKey) less(other hashKey) bool {` is a strict total order over distinct hashKeys, so the insertion sequence is fixed across processes"
          },
          {
            "input": "honesty probe: n-entry builder-form receiver, conversion term taken alone via trieFromBuildMap",
            "state": "reproducible-charge",
            "effect": "forced",
            "evidence": "spec scenario `A reproducible charge is still an honest one` — the charge must exceed retainedTrieBytes(root) + HashMapShallowBytes(n), because every one of the n inserts copies at least the root node and all but the last copy is discarded"
          }
        ],
        "forbidden": [
          "Two conversions of equal-content builder-form maps charging different byte totals — within one process or across processes.",
          "A conversion charge equal to retainedTrieBytes(root) + the entry buffer: that is the under-bill the third scenario forbids, not a reproducible charge.",
          "Any charged accumulation whose value depends on `for _, e := range h.large.m` order.",
          "trieFromBuildMap writing to h.large.m, h.large.root, h.large.count or h.entries — the receiver stays untouched and concurrent readers of h stay unaffected.",
          "A second key ordering alongside hashKey.less (a sort keyed on hashOfKey would be one).",
          "A test reaching builder form by assigning h.large or h.large.m directly instead of driving Set.",
          "A test that reassigns its receiver between repeats — the second call would then run against trie form and measure nothing.",
          "Comparing charges with a tolerance, a ratio or a bound instead of int64 equality.",
          "Committing the reproducibility assertion at n = 9 only (see budgets: the base test can pass there)."
        ],
        "seeding": [
          "Builder form above the limit — the only legal path: `m := NewHashMap()`, then `m.Set(Int{V: int64(i)}, Int{V: int64(i)})` for i in [0, n) with n >= 9. Set promotes on the 9th distinct key and leaves `large.m` populated with `large.root` nil.",
          "Assert the form before measuring anything: `m.large != nil`, `m.large.root == nil`, `len(m.large.m) == n`. Package `core` tests are internal, so these fields are readable (`TestHashMap_PromotionBoundary` already reads `m.large`).",
          "Repeated conversion: call `m.Assoc(key, val)` in a loop and discard the result — Assoc never mutates its receiver, so `m` stays builder form and every call re-enters the conversion. Do not write `m, _, _ = m.Assoc(...)`.",
          "Different build orders: seed two receivers with the same n pairs, one ascending and one descending, both through Set; assert both are builder form, then take one Assoc against each. `TestHashMap_LargeFormPrintsIndependentOfBuildOrder` is the existing forward/backward pattern, written for Assoc rather than Set.",
          "Colliding keys: `findCollidingKeys(t)` (core/hashmap_test.go) returns the fixed Int pair whose hashes agree in every bit; Set both into the receiver alongside the filler keys before measuring.",
          "Trie form, for the skipped-conversion row: seed with repeated `Assoc` instead of Set — past the limit that yields `large.root != nil` — and assert `m.large.root != nil`.",
          "Conversion term in isolation, for the honesty assertion only: call `m.trieFromBuildMap()` directly from the internal test. The reproducibility rows go through the public Assoc/Dissoc charge instead, because that is what the requirement binds."
        ],
        "budgets": [
          "Repeats: 8 conversions per receiver in TestHashMap_ConversionChargeIsReproducible. Derived from the change's recorded base measurement — three consecutive conversions at n=100 already differed (74 224 / 79 200 / 94 112) — with margin, at roughly 8x a sub-millisecond conversion.",
          "Sizes in the committed tests: n = 9, n = 100 and n = 1000. All three are asserted — reproducibility must hold at every size — but only n = 100 and n = 1000 carry a red guarantee; the n = 9 arm is green at base and exists so the red run also prints the charge task 0.1 records at that size. Omitting it makes 0.1 unsatisfiable and unrecoverable once the fix lands.",
          "Red guarantee: any size asserted on must exceed vecBranch (`vecBranch = 1 << vecBits`, 32). At n <= 32 the keys can occupy 32 distinct level-0 slots, in which case insert i charges 24 + 64*(i+1) whatever the order and the total is already order-independent — the base test would pass and the red stage would be false. At n >= 33 the pigeonhole forces at least one merge, whose parent-clone cost depends on the root's population when it happens.",
          "Honesty floor: conversion charge > retainedTrieBytes(root) + HashMapShallowBytes(n), asserted at n = 100 and n = 1000.",
          "Cross-process clause: unasserted by construction, not covered by a test. go test runs a package in one binary, so no committed test observes a second process; the clause holds because hashOfKey's FNV constants are fixed and hashKey.less is a strict total order over distinct keys. Record it as argued, never as asserted. Wall clock: both tests under 1 s. 8 conversions at n = 1000 is on the order of 52 000 node allocations, inside ./core's -timeout 2m with room to spare.",
          "Size of the move (Decision 2), analytically: new charge = HashMapShallowBytes(n) (MeterHashMapHeaderBytes 32 + 64n) for the entry buffer, plus the path-copy sum taken in hashKey.less order, plus the trailing assoc term. Each insert i charges, per node on its copied path, 24 + 64*entries + 8*children, over a depth of ceil(log32 n) plus any collision depth, so the sum stays O(n * ceil(log32 n)) node allocations — the same count as today. Allocation count rises by exactly one, the entries slice. Against the current unstable band the path-copy sum becomes one fixed member of it: at n=100 a fixed value inside [74 224, 94 112] plus 6 432; at n=1000 a fixed value inside [1 071 440, 1 084 640] plus 64 032. Which member cannot be derived without running it — task 2.2 records the measured after-figure.",
          "Published units (Decision 2): none change. The conversion's node charges stay owned by ADR 0011's row \"| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |\"; the new buffer term reuses \"| Hash map entry | 64 bytes per key/value pair |\" plus MeterHashMapHeaderBytes, the 32-byte constant HashMapShallowBytes already applies to every other []entry allocation in the file. The reproducibility statement joins the heading \"## Determinism requirement\", which today forbids only runtime-derived measurement and says nothing about iteration order."
        ],
        "names": [
          "TestHashMap_ConversionChargeIsReproducible — package core, file core/hashmap_test.go",
          "TestHashMap_ConversionChargeIgnoresBuildOrder — package core, file core/hashmap_test.go",
          "setBuiltMap(t *testing.T, n int) *HashMap — NEW test helper; no existing helper builds a Set-built builder-form receiver anywhere in core",
          "retainedTrieBytes(root *hamtNode) int64 — NEW test helper, recursive sum of hamtNodeBytes over the finished trie, used by the honesty assertion",
          "findCollidingKeys(t *testing.T) (int64, int64) — EXISTING, core/hashmap_test.go",
          "(*HashMap).trieFromBuildMap — the only function whose body changes",
          "(*HashMap).sortedEntries — reused as-is, no signature change",
          "(hashKey).less — the reused comparator; no new ordering",
          "hamtNodeBytes, hamtSizeBytes, hashOfKey, hashMapSmallLimit, vecBranch — read by the tests, unchanged",
          "HashMapShallowBytes(n) (core/metering.go) — MeterHashMapHeaderBytes (32) + int64(n)*MeterHashMapEntryBytes (64); this is the buffer term, and 24 + 64n is NOT",
          "Error type: none. The conversion cannot fail; Assoc and Dissoc return an error only from toHashKey, so the tests use Int/Keyword keys and assert err == nil.",
          "Charge comparison: `!=` on int64, exact equality, never a bound."
        ]
      },
      "redTasks": [
        "0.1",
        "1.1"
      ],
      "codeTasks": [
        "2.1",
        "2.2"
      ]
    },
    {
      "id": "charge-documentation",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-RED-WAIVER: documentation-only, no observable behavior to assert. Add the reproducibility property to ADR 0011 under the existing heading `## Determinism requirement`, extending it from 'no runtime-derived measurement' to 'no charge derived from Go map iteration order'; note in ADR 0008 that the gate's allocation axis reads the bytes and allocation-count figures directly and is only decidable if those figures reproduce; record under `[Unreleased]` in CHANGELOG.md that the builder-to-trie conversion now charges a reproducible number and that the number moved, giving the shape of the move rather than a byte count. Verify no unit is published that no ADR table row owns: none is added — the node term keeps `| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |` and the entry buffer is priced by the existing HashMapShallowBytes helper from `| Hash map entry | 64 bytes per key/value pair |` plus MeterHashMapHeaderBytes, which is what HashMapShallowBytes composes.",
      "contract": {
        "states": [
          "documented"
        ],
        "transitions": [
          {
            "input": "ADR 0011 read after the change",
            "state": "documented",
            "effect": "set",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md — `## Determinism requirement`, whose current text binds only `unsafe.Sizeof`, allocator classes, pointer width and map bucket layout"
          },
          {
            "input": "ADR 0008 read after the change",
            "state": "documented",
            "effect": "set",
            "evidence": "docs/adr/0008-consumer-performance-gate.md — `allocation-count axes no longer pass through benchstat at all — the gate` — the axis that now depends on the stated property"
          },
          {
            "input": "CHANGELOG.md `[Unreleased]` read after the change",
            "state": "documented",
            "effect": "set",
            "evidence": "change tasks 3.1"
          }
        ],
        "forbidden": [
          "Publishing a new allocation unit that no ADR 0011 table row owns.",
          "Quoting an exact byte figure in the CHANGELOG for a value that depends on the key set."
        ],
        "seeding": [
          "Not applicable — documentation seam."
        ],
        "budgets": [
          "No numeric budget: no measurement is taken by this seam."
        ],
        "names": [
          "docs/adr/0011-reduction-and-allocation-metering.md",
          "docs/adr/0008-consumer-performance-gate.md",
          "CHANGELOG.md"
        ]
      },
      "codeTasks": [
        "3.1"
      ]
    },
    {
      "id": "floor-verification",
      "tasks": [
        "3.2",
        "3.3"
      ],
      "summary": "NO-RED-WAIVER and NO-TESTER-WAIVER: evidence-only, it runs the floor and records it. The goldset comparison is expected to move nothing and the reason is checked, not assumed: no gold-set fixture builds a map above hashMapSmallLimit — the largest map literal in the corpus is the 3-key `(def base {:mode :tree :depth 10 :trace false})` in merge-config.lisp — so no cell reaches trieFromBuildMap in either evaluator mode. Report allocation evidence separately from timing; a local latency delta is not a gate verdict, while bytes and allocation counts are exact locally.",
      "contract": {
        "states": [
          "verified"
        ],
        "transitions": [
          {
            "input": "TestGoldsetVMAllocations after the change",
            "state": "verified",
            "effect": "no-op",
            "evidence": "internal/goldset/alloc_test.go — `func TestGoldsetVMAllocations(t *testing.T) {` pins per-fixture allocation counts; no fixture builds a map above 8 keys, so no pinned count may move"
          },
          {
            "input": "BenchmarkGoldsetParse in GOLDSET_MODE=eval and GOLDSET_MODE=vm after the change",
            "state": "verified",
            "effect": "no-op",
            "evidence": "internal/goldset/bench_test.go — `func BenchmarkGoldsetParse(b *testing.B) {`; the reader builds no trie node, so the parse cells cannot reach the changed function"
          },
          {
            "input": "openspec validate trie-conversion-charge-determinism --strict --json",
            "state": "verified",
            "effect": "no-op",
            "evidence": "change tasks 3.3"
          }
        ],
        "forbidden": [
          "Reporting a local latency delta as a gate verdict.",
          "Recording a moved goldset allocation count as accepted without naming the fixture that reached the conversion."
        ],
        "seeding": [
          "Not applicable — verification seam."
        ],
        "budgets": [
          "Benchmark parameters mirror the release gate via the Makefile: GOMAXPROCS=2 (PROFILE_GOMAXPROCS) and -benchtime=200ms (PROFILE_BENCHTIME).",
          "Expected goldset delta: 0 on both the bytes and allocation-count axes, in both modes."
        ],
        "names": [
          "BenchmarkGoldsetParse",
          "TestGoldsetVMAllocations",
          "internal/goldset",
          "GOLDSET_MODE"
        ]
      },
      "codeTasks": [
        "3.2",
        "3.3"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "An allocation charge SHALL be a function of the input that produced it.",
      "tests": [
        "TestHashMap_ConversionChargeIsReproducible",
        "TestHashMap_ConversionChargeIgnoresBuildOrder"
      ]
    },
    {
      "shall": "Charging the same operation over the same value SHALL yield the same number of bytes every time, within one process and across processes, so that a difference between two measurements is evidence of a difference in the work.",
      "tests": [
        "TestHashMap_ConversionChargeIsReproducible"
      ]
    },
    {
      "shall": "Where an operation's storage cost depends on the order in which it visits a collection, that order SHALL be derived from the data rather than from Go map iteration, which is randomised per range.",
      "tests": [
        "TestHashMap_ConversionChargeIgnoresBuildOrder",
        "TestHashMap_ConversionChargeIsReproducible"
      ]
    },
    {
      "shall": "An operation MAY visit in any order it likes; what it charges SHALL NOT depend on which order it chose.",
      "tests": [
        "TestHashMap_ConversionChargeIgnoresBuildOrder"
      ]
    },
    {
      "shall": "every one of those updates SHALL charge the identical number of bytes for the conversion, and repeating the whole sequence in a new process SHALL charge that same number again",
      "tests": [
        "TestHashMap_ConversionChargeIsReproducible"
      ]
    },
    {
      "shall": "both updates SHALL charge the same number of bytes, because the charge follows the contents and not the construction history",
      "tests": [
        "TestHashMap_ConversionChargeIgnoresBuildOrder"
      ]
    },
    {
      "shall": "the charge SHALL account for the storage the operation actually obtained rather than only the storage it kept, so that reproducibility is not bought by under-billing",
      "tests": [
        "TestHashMap_ConversionChargeIsReproducible"
      ]
    }
  ],
  "testHarness": [
    "newTestEnv — core/map_determinism_test.go (used at line 11, defined elsewhere in the core test package) — builds a fresh eval Env for TestEval_MapLiteralDeterministic",
    "findCollidingKeys — core/hashmap_test.go:373 — searches Int keys for a fixed-seed hash collision, used by TestHashMap_HashCollisions; not relevant to build-order/Set construction",
    "TestHashMap_PromotionBoundary — core/hashmap_test.go:84 — builds a small-form map to exactly hashMapSmallLimit via Assoc, then one more Assoc to force promotion; promotes via Assoc, not Set, so the promoted map's large.root is already non-nil (never exercises trieFromBuildMap)",
    "TestHashMap_TrieMatchesOracle — core/hashmap_test.go:299 — 20000-step randomized Assoc/Dissoc/Get against a Go-map oracle, well past the small-map limit; builds via Assoc throughout, not Set, so it never leaves a map in builder (large.m, large.root == nil) form",
    "TestHashMap_LargeFormPrintsIndependentOfBuildOrder — core/hashmap_test.go:532 — closest existing pattern for the two new tests: builds two 200-entry maps (forward/backward key order) via repeated Assoc and compares String()/Equals(); NOT Set-built (large.root is populated incrementally by Assoc, trieFromBuildMap is never invoked here since large.m/large.root == nil never occurs above the limit in this test)",
    "TestVectorLedgerBytesIndependentOfLayout — core/metering_test.go:13 — pattern for asserting a ledger charge is representation-independent, on Vector (flat vs trie), not HashMap; structurally close to what 1.1's tests need but for a different type and a different property (charge-independent-of-layout vs charge-independent-of-build-order)",
    "BenchmarkHashMap_Assoc — core/bench_test.go:463 — single Keyword key against a fresh NewHashMap() every b.N iteration; never crosses hashMapSmallLimit, not a promotion benchmark",
    "BenchmarkHashMap_SetBuild — core/bench_test.go:531 — for n in {100,1000,10000}, builds a fresh map via repeated m.Set(Int,Int) each b.N iteration and discards it; demonstrates the Set-build recipe (`m := NewHashMap(); for j := range n { _ = m.Set(Int{V:int64(j)}, Int{V:int64(j)}) }`) but does not retain the map or ever call Assoc/Dissoc on it, so trieFromBuildMap is never exercised here",
    "BenchmarkHashMap_AssocChain — core/bench_test.go:513 — for n in {100,1000,10000}, threads a map through n immutable Assoc calls from empty; builds via Assoc not Set, never produces a builder-form (large.m) map",
    "No existing helper builds and retains a Set-built (builder-form, large.m != nil && large.root == nil) map above hashMapSmallLimit for a subsequent Assoc/Dissoc call. Both new tests in 1.1, and the baseline evidence in 0.1, must build one directly: NewHashMap(), call .Set(key, val) hashMapSmallLimit+1 or more times (BenchmarkHashMap_SetBuild's loop body is the only existing precedent for the Set call shape), keep the resulting *HashMap, and only then call Assoc/Dissoc to trigger trieFromBuildMap and read its charge."
  ],
  "floor": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -race -timeout 2m -p 2 -parallel 2 ./core && golangci-lint run ./core/... && make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -timeout 2m ./internal/goldset -run TestGoldsetVMAllocations && GOMAXPROCS=2 GOLDSET_MODE=eval go test -timeout 2m ./internal/goldset -run '^$' -bench BenchmarkGoldsetParse -benchtime=200ms -benchmem && GOMAXPROCS=2 GOLDSET_MODE=vm go test -timeout 2m ./internal/goldset -run '^$' -bench BenchmarkGoldsetParse -benchtime=200ms -benchmem && openspec validate trie-conversion-charge-determinism --strict --json",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 3
  }
}
```
