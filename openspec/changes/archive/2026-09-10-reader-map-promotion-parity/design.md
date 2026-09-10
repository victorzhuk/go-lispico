# Reader map promotion parity — design

## Context

See `proposal.md` — Why. The constraint that shapes everything here: `Parser.mapSet`
promotes into `largeMap.root` through `newGuardedTrie`, `HashMap.Set` promotes into
`largeMap.m`, and the two forms are exclusive — `getByHashKey` tests `root` first and
never consults `m`. Every reader allocation is admitted before it happens, so changing
what the promotion allocates changes what it charges, and the charge is sealed by tests.

`core` is one package: `reader.go`, `types.go`, `reader_budget.go` and every affected
`_test.go` compile together, so there is no disjoint shard and the chunks run serially.

## Goals / Non-Goals

**Goals.** A map literal's representation does not depend on who built it. The promoted
entry storage is charged on the entry buffer's own doubling schedule. The duplicated
guarded trie builders are deleted with their last callers.

**Non-Goals.** No limit, default or gate threshold moves. `HashMap.Set`, `Get`, `Each`,
`Pairs`, `Assoc`, ordering and equality contracts are untouched, as are `ReaderStats` and
the legacy context-free readers. The evaluator's `Assoc`/`Dissoc` trie path stays exactly
as it is — including its `MeterTrieChildBytes` charge.

## Decisions

**The promoted header is `MeterCollectionHeaderBytes` (24), not `MeterHashMapHeaderBytes`
(32).** Three-way agreement: the promotion being deleted charged `hamtSizeBytes(0, 0)`,
which is 24; the sealed floor already reads `MeterCollectionHeaderBytes +
9*MeterHashMapEntryBytes`; and the spec says "the existing collection header". The 32-byte
constant belongs to `HashMapShallowBytes`, an output-node term — a different subterm, and
using it would move a sealed constant with no spec basis. The ADR amendment must say so, or
the two read as one double charge.

**The charge is discriminated by an exact-equality case, not by the `>=` floor.** The floor
(600 against 2008) passes for 24, for 32, and for no header at all. Task 1.2 adds the
promoted case to `TestGuardedRead_AdmitsOutputStorage`, whose small-map cases carry no
header term, so the promoted one separates all four outcomes.

**Alternative rejected: keep the guarded trie and charge it more cheaply.** It leaves two
promotion forms in the codebase, which is the defect the change exists to remove, and the
representation still survives the read.

## Risks / Trade-offs

**An unadmitted window between 2.1 and 2.2.** Landing the promotion before its charge
leaves the Go map allocated with nothing admitting it, against the requirement that reading
admits what it allocates. Both tasks are in one chunk, so the window is inter-commit only
and never reaches a merge.

**The gold set cannot validate this.** It builds only the small map form, so it cannot
regress on the change and cannot confirm it either. Task 3.4 is an allocation-axis
non-regression check, not evidence for the change. `TestGoldsetVMAllocations` is hard-wired
to `NewEngine(ModeVM)` and ignores `GOLDSET_MODE`, so the two-mode comparison is
`BenchmarkGoldsetParse` only. Latency on this machine is below the measurement floor — say
so rather than claim a verdict.

**Reductions move slightly** for promoted literals if the promotion copy is charged as work:
at most `hashMapSmallLimit` = 8 units per literal, and no test pins an exact reduction total
for a map.

**`make test` runs `./...`**, which includes the json scaling test this repo's history
records as flaky under load. A failure in `./plugins/json` is triaged against that known
flake before it is attributed to this change.

## Execution notes

A `noRed` chunk must be waived before `verifyRun` is called, not after. `golangci-lint` is
appended to every go-coder code stage regardless of the chunk's `verify` string, so
`parity-promote` will show `unused` on the four guarded builders — they are orphaned there
and deleted in `trie-deletion`, which is the first chunk whose verify runs the linter.


## Implementation plan

### baseline — tasks 0.1

- Order: first chunk. Seam `map-charge-baseline`. Coder: `coder`.
- No red stage — the seam carries its waiver marker.
- Code: tasks 0.1
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core -run TestGuardedRead_`
- Site — `core/reader_budget_accounting_test.go` · `admittedForSource` · anchor `func admittedForSource(t *testing.T, src string) int64 {`
  No edit. Baseline instrument: admittedForSource(t, pairSource(n)) for n = 1, 8, 9, 33, 128 records the pre-change admitted totals this change moves. pairSource is at the same file's `func pairSource(pairs int) string {`. Archive check: openspec/changes/archive/2026-09-09-reader-budget-enforcement exists. Record the five totals through the run's own note trail so they outlive the chunk report.
- Contract states: baseline-recorded

  | input | state | effect | evidence |
  | --- | --- | --- | --- |
  | a map literal at each of the five sizes read under DefaultMaxAllocatio | baseline-recorded | set | core/reader_budget_accounting_test.go: func admittedForSource |

  Forbidden: publishing a before/after charge number in CHANGELOG.md or ADR 0011 that was not read off a run at HEAD 410e3eb8

  Seeding: baseline-recorded: run the existing core tests with a temporary print, or compute admittedForSource on the five fixtures; record and discard the scaffolding

  Budgets: 5 fixture sizes: 1, 8, 9, 33, 128 keys; hashMapSmallLimit = 8 is the boundary all five straddle

### parity-promote — tasks 1.1, 2.1

- Order: serial behind `baseline` (shared package `core`). Seam `reader-map-storage-form`. Coder: `go-coder`.
- Red: tasks 1.1 → tests TestReaderBuiltMapMatchesSetBuilt, TestReaderBuiltListMatchesNewList
- `redRun`: `go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestReaderBuilt'`
- Code: tasks 2.1
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestReaderBuilt' && go build ./... && go vet ./core/...`
- Site — `core/reader_map_parity_test.go` · `assertMapParity` · anchor `func assertMapParity(t *testing.T, got, want *HashMap, pairs [][2]Valu`
  Add the failing characterization: read `pairSource`/`intMapFixture(9)` through core.Read and build the same content through HashMap.Set, then assert both hold the same large form. Today the read map has large.root != nil && large.m == nil, the Set map has large.m != nil && large.root == nil — the assertion must fail here before task 2.1. Then the per-fixture assertions: Add representation assertions to the shared assert helpers: assertMapParity compares got/want storage form (entries vs large.m vs large.root, and large.count where the trie form is active); assertListParity — anchor `func assertListParity(t *testing.T, got, want List, items []Value) {` — compares flat vs shared-tail form across listFlatThreshold. Keep the collision fixture reaching its collision arm (isCollision on the node holding collisionKeys). Fails today for intMapFixture(9)/(33)/(128), collisionMapFixture, mixedKeyMapFixture (9 pairs); passes for intMapFixture(1)/(8) and every list size.
- Site — `core/reader.go` · `Parser.mapSet` · anchor `	if len(m.entries) >= hashMapSmallLimit {`
  Replace the newGuardedTrie promotion with the builder form HashMap.Set uses: make(map[hashKey]entry, len(m.entries)+1), copy the sorted entries in, add e, m.large = &largeMap{m: m}, m.entries = nil. Also replace the already-large arm — anchor `		root, added, err := m.large.root.assocGuarded(p.budget, e, hashOfKey(hk), 0)` — with the Set-equivalent store into m.large.m (root stays nil for reader-built maps, so the trie arm is unreachable from the reader; mirror Set's shape exactly: branch root-first, then h.large.m[hk] = e — but probe m.large.m before charging, so a key the map already holds takes the no-op arm and grows nothing). Update the mapSet doc comment, which currently claims the trie is built directly because the Go-map branch has no interruption point. Duplicate keys, mixed key types, collisions and ReaderStats must not move. golangci-lint is appended to every go-coder code stage by the kernel, so expect `unused` on newGuardedTrie, assocGuarded, assocCollisionGuarded and mergeEntriesGuarded here: they are orphaned but not deleted — that is 2.3 — so this chunk's verify stops at build and vet; golangci-lint's `unused` would fire on the orphan window and runs first in trie-deletion.
- Contract states: small, builder, trie, unhashable-refusal, flat, shared-tail

  | input | state | effect | evidence |
  | --- | --- | --- | --- |
  | new key, len(m.entries) < 8 | small | set | core/reader.go: 'if len(m.entries) >= hashMapSmallLimit' falls through to the sorted inser |
  | key already in m.entries | small | no-op | core/reader.go: 'if found { m.entries[i] = e; return nil }' |
  | new key, len(m.entries) == 8 | small -> builder | forced | core/types.go Set: 'm := make(map[hashKey]entry, len(h.entries)+1)' then 'h.large = &large |
  | new key while m.large.m != nil | builder | set | core/types.go Set: 'h.large.m[hk] = e' |
  | key already in m.large.m | builder | no-op | core/types.go Set: 'h.large.m[hk] = e' overwrites; Len() reads len(h.large.m) so the size  |
  | two keys colliding under hashOfKey, both below and above the limit | builder | no-op | hashKey equality, not hashOfKey, keys the Go map: a hash collision has no representational |
  | a List or Vector key | unhashable-refusal | forced | core/reader.go mapSet: 'hk, err := toHashKey(key)' returns before any entry, buffer or pro |
  | the same contents through HashMap.Set at 1 and 8 keys | small | no-op | spec requirement: 'A collection produced by reading source SHALL use the same internal rep |
  | the same contents through HashMap.Set at 9, 33 and 128 keys | builder | no-op | spec requirement: 'A collection produced by reading source SHALL use the same internal rep |
  | a list literal at 1 and 32 elements against NewList | flat | no-op | core/types.go: newGuardedListChain 'builds the same chain newListChain does'; both branch  |
  | a list literal at 33 and 100 elements against NewList | shared-tail | no-op | core/types.go: newGuardedListChain 'builds the same chain newListChain does'; both branch  |

  Forbidden: trie reached by any read: a *HashMap produced by core.Read or ReadWithContextStats holds large.root != nil; builder with large.root != nil (the two large forms are exclusive; core/types.go getByHashKey tests root first and would never consult m); small with large != nil; builder with entries != nil (core/types.go Set clears h.entries = nil at promotion; leaving them makes Len() and eachRaw disagree); a reader-built map and a Set-built map of the same contents in different states

  Seeding: small: read a map literal of 1..8 distinct keys, e.g. pairSource(4); builder: read a map literal of >= 9 distinct keys, e.g. pairSource(9); the only other legal path is HashMap.Set past hashMapSmallLimit; trie: NOT reachable by reading after this change; reach it only through Assoc/Dissoc on a builder-form or small-form map (core/types.go Assoc calls trieFromBuildMap when large.root == nil), never by writing largeMap.root in a test; unhashable-refusal: a map literal whose key is a List or Vector, refused by toHashKey before any entry storage exists

  Budgets: hashMapSmallLimit = 8: the last size held in the sorted small form; promotion copies exactly hashMapSmallLimit = 8 entries into the new Go map; the uninterruptible span is 8 map stores, replacing the current per-insert path copy of depth ceil(log32 n); parity fixtures: 1, 8, 9, 33, 128 keys plus the collision and mixed-key fixtures already in core/reader_map_parity_test.go

### entry-charge — tasks 1.2, 2.2

- Order: serial behind `parity-promote` (shared package `core`). Seam `reader-map-entry-charge`. Coder: `go-coder`.
- Red: tasks 1.2 → tests TestGuardedRead_ConstructionStorage, TestGuardedRead_MapConstructionContracts, TestGuardedRead_AllowanceBoundary, TestGuardedRead_PromotionRefusedBeforeStorage, TestGuardedRead_AdmitsOutputStorage
- `redRun`: `go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestGuardedRead_(ConstructionStorage|MapConstructionContracts|AllowanceBoundary|PromotionRefusedBeforeStorage|AdmitsOutputStorage)'`
- Code: tasks 2.2
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core && go build ./... && go vet ./core/...`
- Site — `core/reader_budget_accounting_test.go` · `TestGuardedRead_ConstructionStorage` · anchor `MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes,`
  Amend the sealed promoted-map expectation from the trie-node term to the entry-buffer term: entryBufferBytes(9) (header term: MeterCollectionHeaderBytes = 24, admitted once per promoted literal), keeping the `>=` floor shape and the reason string honest. Same file, the `promoted-map` case in TestGuardedRead_AllowanceBoundary (anchor `			{"promoted-map", pairSource(9), 21},`) stays self-derived (total, total-1) and needs no constant. TestGuardedRead_MapConstructionContracts must invert its promoted-form assertion — anchor `			if m.large.root == nil || m.large.m != nil {` — to require large.m != nil && large.root == nil, and its doc comment (`// checkpoint expose ... Go-map branch of Set cannot expose a checkpoint`) must be rewritten. Verify all amended expectations fail before section 2. Add TestGuardedRead_PromotionRefusedBeforeStorage, built on allocationProbe, for the spec scenario 'Promotion is refused before its storage exists'. Of the five names, MapConstructionContracts is red once its representation guard is inverted, PromotionRefusedBeforeStorage is new, and AdmitsOutputStorage goes red as soon as its promoted exact-equality case is added; AllowanceBoundary is self-derived and ConstructionStorage is a >= floor, so only a run decides those two.
- Site — `core/reader.go` · `Parser.mapSet (entry-buffer charge)` · anchor `	if err := plan.admit(p.budget, int64(len(m.entries)+1)); err != nil {`
  Charge the promoted entry storage on the same growthPlan the small form uses. The plan is created per map literal in parseHashMap (anchor `	entryPlan := growthPlan{unit: MeterHashMapEntryBytes}`) and admits before allocating; extend it across the promotion so the Go-map's entry storage rides the doubling schedule (plan.admit for len+1 before make(), plus MeterCollectionHeaderBytes = 24, admitted exactly once per promoted literal) instead of hamtSizeBytes per node. Admission must precede every allocation so a map literal too wide for the allowance is refused before its storage exists; pooled and cold reads charge the identical logical schedule (growthPlan is logical, core/reader_budget.go `// admit reserves whatever growth reaching a logical capacity of n entries`).
- Contract states: capacity-n, header-charged, refused

  | input | state | effect | evidence |
  | --- | --- | --- | --- |
  | new key raising the logical entry count above the current capacity | capacity-n -> capacity-2n | set | core/reader_budget.go: 'func (g *growthPlan) admit' charges 'g.capacity * g.unit' before t |
  | a key the map already holds, in either form | capacity-n | no-op | core/reader.go: the 'if found' arm returns before plan.admit; the builder arm must probe m |
  | the 9th distinct key | capacity-8 -> header-charged + capacity-16 | forced | spec requirement: 'Map construction SHALL add the existing collection header and map entry |
  | a doubling whose charge exceeds the remaining allowance | refused | forced | spec scenario: 'a map literal whose promoted entry storage exceeds the remaining allocatio |
  | the same source read on retained pooled scratch | capacity-n | no-op | core/reader_budget.go: 'The schedule is logical, so a pooled buffer's retained capacity av |
  | the same source through the context-free core.Read (nil budget) | capacity-n | no-op | core/reader_budget.go: 'A nil *readerBudget is the absent budget: the context-free reader  |
  | ReaderStats.Nodes and ReaderStats.Bytes for any map literal | capacity-n | no-op | spec requirement: 'Successful ReaderStats.Nodes and ReaderStats.Bytes SHALL remain unchang |

  Forbidden: any admitAlloc of hamtSizeBytes or hamtNodeBytes on a reader path; the promotion header charged more than once per map literal; a growth charge for an insert that rewrites a key the map already holds (ADR 0011 T6: 'charged only when inserting a new key grows the buffer'); the Go map allocated or an entry stored before the growth charge for the capacity that holds it is admitted; a charge that differs between a cold read and a read on retained pooled scratch

  Seeding: capacity-n: read a map literal of n distinct keys; the growthPlan is created fresh per literal in parseHashMap ('entryPlan := growthPlan{unit: MeterHashMapEntryBytes}'), so n is the only input; header-charged: read a map literal of >= 9 distinct keys; refused: read under allocCeilingContext(total-1) where total is the read's own admittedForSource, the self-derived boundary shape TestGuardedRead_AllowanceBoundary already uses; pooled: readOwnedScratch against a *readerScratch the test owns, three times, as TestGuardedRead_PoolReuseChargesTheSameConstruction does

  Budgets: logical capacities: 1, 2, 4, 8, 16, 32, ... ; each doubling admits the whole new capacity times MeterHashMapEntryBytes = 64; MeterCollectionHeaderBytes = 24, admitted exactly once per promoted literal; a 9-key literal admits 24 + entryBufferBytes(9) = 24 + (1+2+4+8+16)*64 = 24 + 1984 = 2008 bytes of construction storage; the existing >= floor MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes = 600 stays satisfied (2008 >= 600), so TestGuardedRead_ConstructionStorage's comparison shape does not change; promotion charges at most hashMapSmallLimit = 8 reduction units for the copy into the Go map; exact-equality case owned by task 1.2, in TestGuardedRead_AdmitsOutputStorage: planBytes(21) + workBufferBytes(1) + MeterCollectionHeaderBytes + entryBufferBytes(9) + outputBytes(t, pairSource(9)) — the >= floor cannot tell 24 from 32 or from no header at all

### trie-deletion — tasks 2.3

- Order: serial behind `entry-charge` (shared package `core`). Seam `guarded-trie-deletion`. Coder: `go-coder`.
- No red stage — the seam carries its waiver marker.
- Code: tasks 2.3
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core && go build ./... && go vet ./core/... && golangci-lint run ./core/...`
- Site — `core/types.go` · `newGuardedTrie / assocGuarded / assocCollisionGuarded / mergeEntriesGuarded` · anchor `func newGuardedTrie(entries []entry, extra entry, b *readerBudget) (*h`
  Delete all four (types.go: newGuardedTrie, assocGuarded, assocCollisionGuarded, mergeEntriesGuarded) once the two reader call sites are gone; they have no other non-test callers. Their unguarded originals stay: `func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {` and `func (n *hamtNode) assoc(e entry, h uint32, shift uint) (*hamtNode, int64, bool) {`, plus the collision arm inside assoc — confirm each deleted case (replace-in-place, data→node merge, child descent, fresh data slot, collision replace, collision append, shift>=32 merge, same-fragment recursion) is still carried there. hamtSizeBytes stays live via hamtNodeBytes; hamtNodeBytes stays live via assoc/dissoc. readerBudget.admitAlloc keeps its other callers.

### charge-docs — tasks 3.1

- Order: serial behind `trie-deletion` (shared package `core`). Seam `charge-model-docs`. Coder: `zpatcher`.
- No red stage — the seam carries its waiver marker.
- Code: tasks 3.1
- `verify`: `openspec validate reader-map-promotion-parity --strict --json && grep -q "MeterTrieChildBytes" docs/adr/0011-reduction-and-allocation-metering.md && grep -q "entry buffer" docs/adr/0011-reduction-and-allocation-metering.md && grep -q "subterms" docs/adr/0011-reduction-and-allocation-metering.md && grep -q "reader map" CHANGELOG.md`
- Site — `docs/adr/0011-reduction-and-allocation-metering.md` · `T6 construction storage` · anchor `- **T6** has four subterms: the flat collection copy-out, `ValueSlotsB`
  Replace the fourth subterm (`and, per persistent-map node, its exact occupied size MeterCollectionHeaderBytes + 64*entries + 8*children, admitted before that node is allocated`) with the entry-buffer term for reader map construction above hashMapSmallLimit, and widen the small-map subterm to cover both sides of the limit. The T6 table row `| T6 construction storage | four subterms, below | before the collection's storage is allocated |` needs its count updated if the subterm count changes. Add an Unreleased CHANGELOG.md entry under the existing `## [Unreleased]` `### Changed` heading for the charge change. Verify no published unit lacks an owning ADR row (MeterTrieChildBytes is still owned only if some row still names it). State in the ADR that the promoted-buffer header MeterCollectionHeaderBytes = 24 is a construction-storage term, distinct from HashMapShallowBytes's MeterHashMapHeaderBytes = 32 output term, so the two are not read as one double charge.

### verify-core — tasks 3.2, 3.3

- Order: serial behind `charge-docs` (shared package `core`). Seam `change-verification`. Coder: `coder`.
- No red stage — the seam carries its waiver marker.
- Code: tasks 3.2, 3.3
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./core && go test -race -timeout 2m -p 2 -parallel 2 ./core && make build && make lint`
- Site — `Makefile` · `package verification commands` · anchor `GOTESTFLAGS ?= -timeout 2m`
  No edit. Run: go test -timeout 2m -p 2 -parallel 2 ./core ./runtime; go test -race -timeout 2m -p 2 -parallel 2 ./core; golangci-lint run ./core/... (use `env -C <worktree>` for the lint run). Record pass evidence per command.
- Site — `Makefile` · `build / lint / test targets` · anchor `	go test $(GOTESTFLAGS) ./...`
  No edit. Run make build, make lint, make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'; record pass/failure evidence. Known flake to expect and report rather than chase: TestDecodeHashMap_Scaling in plugins/json has ~0.1 headroom on its 3.0 threshold under load.

### goldset-evidence — tasks 3.4, 3.5

- Order: serial behind `verify-core` (shared package `core`). Seam `change-verification`. Coder: `coder`.
- No red stage — the seam carries its waiver marker.
- Code: tasks 3.4, 3.5
- `verify`: `go test -timeout 2m -p 2 -parallel 2 ./internal/goldset && openspec validate reader-map-promotion-parity --strict --json`
- Site — `internal/goldset/alloc_test.go` · `TestGoldsetVMAllocations` · anchor `func TestGoldsetVMAllocations(t *testing.T) {`
  No edit expected. Compare against the pre-change baseline. TestGoldsetVMAllocations is hard-wired to NewEngine(ModeVM) and ignores GOLDSET_MODE, so the two-mode comparison is BenchmarkGoldsetParse only; the per-fixture ceilings live in the vmAllocCeilings map above the test. BenchmarkGoldsetParse is at internal/goldset/bench_test.go `func BenchmarkGoldsetParse(b *testing.B) {`; report B/op and allocs/op separately from ns/op and state plainly that latency deltas under this machine's floor are not a verdict. Confirm no ADR 0008 threshold moves. The gold set builds only the small map form, so a promoted-map change should be allocation-neutral there — say so if it is.
- Site — `openspec/changes/reader-map-promotion-parity/specs/core-engine/spec.md` · `Collection representation does not depend on its builder` · anchor `### Requirement: Collection representation does not depend on its buil`
  No edit expected. Run openspec validate reader-map-promotion-parity --strict --json, then check the landed code against both scenarios of this ADDED requirement and the two MODIFIED scenarios (`A promoted map literal is charged on the entry-buffer schedule`, `Promotion is refused before its storage exists`), and against ADR 0011 T6 and the CHANGELOG entry.

## Verification

- Floor: `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`
- Testing mode: `existing-service-strict` · tier `heavy` · lenses spec, quality, perf
- Plan review: zarchitect, 0 rounds, verdict `pending`

### Requirements map

| SHALL | tests |
| --- | --- |
| A runtime-owned read SHALL reserve deterministic allocation charges before allocating to | TestGuardedRead_TokenPlanAdmission, TestGuardedRead_AdmitsOutputStorage, TestGuardedRead_ConstructionStorage |
| The allocation model SHALL add 32 bytes per admitted token | TestGuardedRead_ExactCharges, TestGuardedRead_ConstructionStorage, TestGuardedRead_NumericConversionStorage |
| Successful `ReaderStats.Nodes` and `ReaderStats.Bytes` SHALL remain unchanged for the sa | TestGuardedRead_StatsUnchanged, TestReadWithContextStats_LegacyParity |
| reading SHALL fail with terminal `ResourceLimitError` before allocating the full token a | TestGuardedRead_TokenPlanAdmission |
| reading SHALL reject the payload before materializing an oversized decoded buffer | TestGuardedRead_EscapedPayloadAdmission, TestGuardedRead_PrepaidPayloadKeepsOutputTotals |
| reading SHALL return terminal `ResourceLimitError` before conversion or formatting the t | TestGuardedRead_NumericConversionStorage, TestGuardedRead_InvalidNumberDiagnosticIsBounded |
| it SHALL succeed with that total charge, and the same read with a ceiling one byte lower | TestGuardedRead_AllowanceBoundary, TestGuardedRead_ExactCharges |
| their values and `ReaderStats` SHALL be equal even though only the metered read charges  | TestReadWithContextStats_LegacyParity, TestGuardedRead_StatsUnchanged |
| the admitted construction storage SHALL be the collection header plus the map entry unit | TestGuardedRead_ConstructionStorage, TestGuardedRead_AdmitsOutputStorage, TestGuardedRead_MapConstructionContracts |
| reading SHALL fail with terminal `ResourceLimitError` before that storage is allocated | TestGuardedRead_PromotionRefusedBeforeStorage, TestGuardedRead_AllowanceBoundary |
| A collection produced by reading source SHALL use the same internal representation | TestReaderBuiltMapMatchesSetBuilt, TestReaderBuiltListMatchesNewList |
| Reading SHALL continue to admit the storage it allocates before allocating it | TestGuardedRead_ConstructionStorage, TestGuardedRead_PoolReuseChargesTheSameConstruction |
| both SHALL hold their entries in the same storage form, and every observable | TestReaderBuiltMapMatchesSetBuilt |
| both SHALL hold their elements in the same storage form, and every observable SHALL agre | TestReaderBuiltListMatchesNewList |

### Test harness

- readerEntry / readerEntries — core/reader_map_parity_test.go:18,23 — the two public source-to-forms paths (Read, and readContextStats over FullDialect) every parity fixture is run through
- readSingleForm — core/reader_map_parity_test.go:33 — reads src through one entry and fatals unless exactly one form comes back
- collisionKeys — core/reader_map_parity_test.go:48 — [2]int64{3367, 6372}, two Int keys colliding under hashOfKey so the trie exhausts its bits
- mapFixture — core/reader_map_parity_test.go:50 — {name, src, pairs, absent}: literal source plus the same content as constructor input
- intMapFixture — core/reader_map_parity_test.go:59 — n pairs of Int keys 0..n-1 mapped to i*10, as source and pairs
- collisionMapFixture — core/reader_map_parity_test.go:82 — 8 plain Int keys plus the two colliding ones, so the map promotes and reaches the collision arm
- mixedKeyMapFixture — core/reader_map_parity_test.go:109 — 9 pairs covering Keyword/String/Symbol/Int/Float/Bool/Nil keys, one past the small limit
- assertMapParity — core/reader_map_parity_test.go:179 — compares Len, Get per pair, Get(absent), Pairs, Each, String and Equals both ways between reader-built and Set-built maps
- eachPairs — core/reader_map_parity_test.go:243 — collects Each's visit order into [][2]Value
- assertListParity — core/reader_map_parity_test.go:286 — compares Len, At, ToSlice, Rest, String and Equals both ways between reader-built and NewList-built lists
- listCellBytes — core/reader_budget_accounting_test.go:12 — 32, one shared-tail list cell's construction charge
- listSource / vectorSource — core/reader_budget_accounting_test.go:14,15 — n-element list and vector literals of the same symbol, the pair that isolates the linked-cell term
- pairSource — core/reader_budget_accounting_test.go:19 — a map literal of n distinct keyword keys with symbol values, so only construction is admitted on top of the token plan
- sameKeySource — core/reader_budget_accounting_test.go:33 — n repeats of one key, so every insert past the first rewrites an existing entry
- entryBufferBytes — core/reader_budget_accounting_test.go:40 — the doubling-schedule total for an entry buffer of n entries, in MeterHashMapEntryBytes units; the test-side mirror of growthPlan
- allocationProbe — core/reader_budget_accounting_test.go:55 — a context whose Err() records the allocation ledger at every terminal-state check the read makes
- admittedForSource — core/reader_budget_accounting_test.go:66 — reads src under DefaultMaxAllocationBytes and returns the total admitted bytes; the baseline instrument for task 0.1
- readerTokenPlanBytes / planBytes — core/reader_budget_admission_test.go:14,20 — 32 bytes per planned token; planBytes(tokens) is the T1 term subtracted out of every construction expectation
- conversionBytes — core/reader_budget_admission_test.go:25 — 2*n + 256, the numeric-conversion storage term
- workBufferBytes — core/reader_budget_admission_test.go:32 — the value-slot doubling total for a reader work buffer of logical capacity n
- flatFormBytes — core/reader_budget_admission_test.go:49 — workBufferBytes(1) + workBufferBytes(children) + ValueSlotsBytes(children), one top-level flat collection's construction storage
- outputBytes — core/reader_budget_admission_test.go:58 — the output-node total for a source, derived from the context-free reader's own ReaderStats via ReaderAllocationBytes
- allocCeilingContext — core/reader_budget_admission_test.go:70 — a context with an ample reduction budget and exactly maxAllocBytes of allocation allowance, plus its EvalMeter
- admittedBytes — core/reader_budget_admission_test.go:75 — Snapshot().AllocationBytes off a meter
- readOwnedScratch — core/reader_budget_admission_test.go:80 — drives a test-owned readerScratch through one guarded read, so repeat reads hit retained capacity deterministically instead of sync.Pool
- budgetContext — core/reader_budget_cancel_test.go:13 — a context with a reduction ceiling, for work-side (not storage-side) limits
- chargedReductions — core/reader_budget_cancel_test.go:18 — Snapshot().Reductions off a meter
- readErrorCode — core/reader_budget_cancel_test.go:67 — the error code string of a read failure, compared against CodeResourceLimit
- longKeySource — core/reader_budget_cancel_test.go:79 — a map literal of long string keys, driving the per-key hash work charge
- collidingKeySource — core/reader_budget_cancel_test.go:92 — a map literal of keys that collide, driving the collision-scan work charge
- readContextStats — core/reader_context_parity_test.go:14 — the guarded entry point every budget test reads through (forms, ReaderStats, err)
- guardedScratchRead — core/reader_scratch_release_test.go:17 — one guarded read on a caller-owned scratch with the budget still installed at release time
- assertScratchCleared — core/reader_scratch_release_test.go:31 — checks a released scratch pins no source, token vals, nodes or budget, to full retained capacity
- Fixtures — internal/goldset/goldset.go:77 — the gold-set fixture loader BenchmarkGoldsetParse and TestGoldsetVMAllocations both read
- vmAllocCeilings — internal/goldset/alloc_test.go (map above line 45) — the per-fixture allocation counts the release gate measures

## Plan appendix

```json
{
  "v": 2,
  "change": "reader-map-promotion-parity",
  "baseSha": "410e3eb8e9dceaf08a4dc1784dfa6d16af25d1ca",
  "generatedAt": "2026-09-10T07:12:18Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "estimateHours": 2.59,
  "chunks": [
    {
      "id": "baseline",
      "taskIds": [
        "0.1"
      ],
      "prev": null,
      "parallel": false,
      "sharedPkg": null,
      "seam": "map-charge-baseline",
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
          "file": "core/reader_budget_accounting_test.go",
          "symbol": "admittedForSource",
          "anchor": "func admittedForSource(t *testing.T, src string) int64 {",
          "change": "No edit. Baseline instrument: admittedForSource(t, pairSource(n)) for n = 1, 8, 9, 33, 128 records the pre-change admitted totals this change moves. pairSource is at the same file's `func pairSource(pairs int) string {`. Archive check: openspec/changes/archive/2026-09-09-reader-budget-enforcement exists. Record the five totals through the run's own note trail so they outlive the chunk report."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "0.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core -run TestGuardedRead_",
      "coder": "coder",
      "contract": {
        "states": [
          "baseline-recorded"
        ],
        "forbidden": [
          "publishing a before/after charge number in CHANGELOG.md or ADR 0011 that was not read off a run at HEAD 410e3eb8"
        ],
        "seeding": [
          "baseline-recorded: run the existing core tests with a temporary print, or compute admittedForSource on the five fixtures; record and discard the scaffolding"
        ],
        "budgets": [
          "5 fixture sizes: 1, 8, 9, 33, 128 keys",
          "hashMapSmallLimit = 8 is the boundary all five straddle"
        ],
        "transitions": [
          {
            "input": "a map literal at each of the five sizes read under DefaultMaxAllocationBytes",
            "state": "baseline-recorded",
            "effect": "set",
            "evidence": "core/reader_budget_accounting_test.go: func admittedForSource"
          }
        ]
      }
    },
    {
      "id": "parity-promote",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": "baseline",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "reader-map-storage-form",
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
          "file": "core/reader_map_parity_test.go",
          "symbol": "assertMapParity",
          "anchor": "func assertMapParity(t *testing.T, got, want *HashMap, pairs [][2]Value, absent Value) {",
          "change": "Add the failing characterization: read `pairSource`/`intMapFixture(9)` through core.Read and build the same content through HashMap.Set, then assert both hold the same large form. Today the read map has large.root != nil && large.m == nil, the Set map has large.m != nil && large.root == nil — the assertion must fail here before task 2.1. Then the per-fixture assertions: Add representation assertions to the shared assert helpers: assertMapParity compares got/want storage form (entries vs large.m vs large.root, and large.count where the trie form is active); assertListParity — anchor `func assertListParity(t *testing.T, got, want List, items []Value) {` — compares flat vs shared-tail form across listFlatThreshold. Keep the collision fixture reaching its collision arm (isCollision on the node holding collisionKeys). Fails today for intMapFixture(9)/(33)/(128), collisionMapFixture, mixedKeyMapFixture (9 pairs); passes for intMapFixture(1)/(8) and every list size."
        },
        {
          "task": "2.1",
          "file": "core/reader.go",
          "symbol": "Parser.mapSet",
          "anchor": "\tif len(m.entries) >= hashMapSmallLimit {",
          "change": "Replace the newGuardedTrie promotion with the builder form HashMap.Set uses: make(map[hashKey]entry, len(m.entries)+1), copy the sorted entries in, add e, m.large = &largeMap{m: m}, m.entries = nil. Also replace the already-large arm — anchor `\t\troot, added, err := m.large.root.assocGuarded(p.budget, e, hashOfKey(hk), 0)` — with the Set-equivalent store into m.large.m (root stays nil for reader-built maps, so the trie arm is unreachable from the reader; mirror Set's shape exactly: branch root-first, then h.large.m[hk] = e — but probe m.large.m before charging, so a key the map already holds takes the no-op arm and grows nothing). Update the mapSet doc comment, which currently claims the trie is built directly because the Go-map branch has no interruption point. Duplicate keys, mixed key types, collisions and ReaderStats must not move. golangci-lint is appended to every go-coder code stage by the kernel, so expect `unused` on newGuardedTrie, assocGuarded, assocCollisionGuarded and mergeEntriesGuarded here: they are orphaned but not deleted — that is 2.3 — so this chunk's verify stops at build and vet; golangci-lint's `unused` would fire on the orphan window and runs first in trie-deletion."
        }
      ],
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [
        "TestReaderBuiltMapMatchesSetBuilt",
        "TestReaderBuiltListMatchesNewList"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestReaderBuilt'",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestReaderBuilt' && go build ./... && go vet ./core/...",
      "coder": "go-coder",
      "contract": {
        "states": [
          "small",
          "builder",
          "trie",
          "unhashable-refusal",
          "flat",
          "shared-tail"
        ],
        "forbidden": [
          "trie reached by any read: a *HashMap produced by core.Read or ReadWithContextStats holds large.root != nil",
          "builder with large.root != nil (the two large forms are exclusive; core/types.go getByHashKey tests root first and would never consult m)",
          "small with large != nil",
          "builder with entries != nil (core/types.go Set clears h.entries = nil at promotion; leaving them makes Len() and eachRaw disagree)",
          "a reader-built map and a Set-built map of the same contents in different states"
        ],
        "seeding": [
          "small: read a map literal of 1..8 distinct keys, e.g. pairSource(4)",
          "builder: read a map literal of >= 9 distinct keys, e.g. pairSource(9); the only other legal path is HashMap.Set past hashMapSmallLimit",
          "trie: NOT reachable by reading after this change; reach it only through Assoc/Dissoc on a builder-form or small-form map (core/types.go Assoc calls trieFromBuildMap when large.root == nil), never by writing largeMap.root in a test",
          "unhashable-refusal: a map literal whose key is a List or Vector, refused by toHashKey before any entry storage exists"
        ],
        "budgets": [
          "hashMapSmallLimit = 8: the last size held in the sorted small form",
          "promotion copies exactly hashMapSmallLimit = 8 entries into the new Go map; the uninterruptible span is 8 map stores, replacing the current per-insert path copy of depth ceil(log32 n)",
          "parity fixtures: 1, 8, 9, 33, 128 keys plus the collision and mixed-key fixtures already in core/reader_map_parity_test.go"
        ],
        "transitions": [
          {
            "input": "new key, len(m.entries) < 8",
            "state": "small",
            "effect": "set",
            "evidence": "core/reader.go: 'if len(m.entries) >= hashMapSmallLimit' falls through to the sorted insert"
          },
          {
            "input": "key already in m.entries",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/reader.go: 'if found { m.entries[i] = e; return nil }'"
          },
          {
            "input": "new key, len(m.entries) == 8",
            "state": "small -> builder",
            "effect": "forced",
            "evidence": "core/types.go Set: 'm := make(map[hashKey]entry, len(h.entries)+1)' then 'h.large = &largeMap{m: m}' and 'h.entries = nil'"
          },
          {
            "input": "new key while m.large.m != nil",
            "state": "builder",
            "effect": "set",
            "evidence": "core/types.go Set: 'h.large.m[hk] = e'"
          },
          {
            "input": "key already in m.large.m",
            "state": "builder",
            "effect": "no-op",
            "evidence": "core/types.go Set: 'h.large.m[hk] = e' overwrites; Len() reads len(h.large.m) so the size does not move"
          },
          {
            "input": "two keys colliding under hashOfKey, both below and above the limit",
            "state": "builder",
            "effect": "no-op",
            "evidence": "hashKey equality, not hashOfKey, keys the Go map: a hash collision has no representational effect in either large form (core/types.go: 'e, ok := h.large.m[hk]')"
          },
          {
            "input": "a List or Vector key",
            "state": "unhashable-refusal",
            "effect": "forced",
            "evidence": "core/reader.go mapSet: 'hk, err := toHashKey(key)' returns before any entry, buffer or promotion exists"
          },
          {
            "input": "the same contents through HashMap.Set at 1 and 8 keys",
            "state": "small",
            "effect": "no-op",
            "evidence": "spec requirement: 'A collection produced by reading source SHALL use the same internal representation as the same collection produced through the public constructors, at every size.'"
          },
          {
            "input": "the same contents through HashMap.Set at 9, 33 and 128 keys",
            "state": "builder",
            "effect": "no-op",
            "evidence": "spec requirement: 'A collection produced by reading source SHALL use the same internal representation as the same collection produced through the public constructors, at every size.'"
          },
          {
            "input": "a list literal at 1 and 32 elements against NewList",
            "state": "flat",
            "effect": "no-op",
            "evidence": "core/types.go: newGuardedListChain 'builds the same chain newListChain does'; both branch at listFlatThreshold = 32, so these assertions are green on arrival and are regression pins, not red"
          },
          {
            "input": "a list literal at 33 and 100 elements against NewList",
            "state": "shared-tail",
            "effect": "no-op",
            "evidence": "core/types.go: newGuardedListChain 'builds the same chain newListChain does'; both branch at listFlatThreshold = 32, so these assertions are green on arrival and are regression pins, not red"
          }
        ]
      }
    },
    {
      "id": "entry-charge",
      "taskIds": [
        "1.2",
        "2.2"
      ],
      "prev": "parity-promote",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "reader-map-entry-charge",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "core/reader_budget_accounting_test.go",
          "symbol": "TestGuardedRead_ConstructionStorage",
          "anchor": "MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes,",
          "change": "Amend the sealed promoted-map expectation from the trie-node term to the entry-buffer term: entryBufferBytes(9) (header term: MeterCollectionHeaderBytes = 24, admitted once per promoted literal), keeping the `>=` floor shape and the reason string honest. Same file, the `promoted-map` case in TestGuardedRead_AllowanceBoundary (anchor `\t\t\t{\"promoted-map\", pairSource(9), 21},`) stays self-derived (total, total-1) and needs no constant. TestGuardedRead_MapConstructionContracts must invert its promoted-form assertion — anchor `\t\t\tif m.large.root == nil || m.large.m != nil {` — to require large.m != nil && large.root == nil, and its doc comment (`// checkpoint expose ... Go-map branch of Set cannot expose a checkpoint`) must be rewritten. Verify all amended expectations fail before section 2. Add TestGuardedRead_PromotionRefusedBeforeStorage, built on allocationProbe, for the spec scenario 'Promotion is refused before its storage exists'. Of the five names, MapConstructionContracts is red once its representation guard is inverted, PromotionRefusedBeforeStorage is new, and AdmitsOutputStorage goes red as soon as its promoted exact-equality case is added; AllowanceBoundary is self-derived and ConstructionStorage is a >= floor, so only a run decides those two."
        },
        {
          "task": "2.2",
          "file": "core/reader.go",
          "symbol": "Parser.mapSet (entry-buffer charge)",
          "anchor": "\tif err := plan.admit(p.budget, int64(len(m.entries)+1)); err != nil {",
          "change": "Charge the promoted entry storage on the same growthPlan the small form uses. The plan is created per map literal in parseHashMap (anchor `\tentryPlan := growthPlan{unit: MeterHashMapEntryBytes}`) and admits before allocating; extend it across the promotion so the Go-map's entry storage rides the doubling schedule (plan.admit for len+1 before make(), plus MeterCollectionHeaderBytes = 24, admitted exactly once per promoted literal) instead of hamtSizeBytes per node. Admission must precede every allocation so a map literal too wide for the allowance is refused before its storage exists; pooled and cold reads charge the identical logical schedule (growthPlan is logical, core/reader_budget.go `// admit reserves whatever growth reaching a logical capacity of n entries`)."
        }
      ],
      "redTasks": [
        "1.2"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestGuardedRead_ConstructionStorage",
        "TestGuardedRead_MapConstructionContracts",
        "TestGuardedRead_AllowanceBoundary",
        "TestGuardedRead_PromotionRefusedBeforeStorage",
        "TestGuardedRead_AdmitsOutputStorage"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestGuardedRead_(ConstructionStorage|MapConstructionContracts|AllowanceBoundary|PromotionRefusedBeforeStorage|AdmitsOutputStorage)'",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core && go build ./... && go vet ./core/...",
      "coder": "go-coder",
      "contract": {
        "states": [
          "capacity-n",
          "header-charged",
          "refused"
        ],
        "forbidden": [
          "any admitAlloc of hamtSizeBytes or hamtNodeBytes on a reader path",
          "the promotion header charged more than once per map literal",
          "a growth charge for an insert that rewrites a key the map already holds (ADR 0011 T6: 'charged only when inserting a new key grows the buffer')",
          "the Go map allocated or an entry stored before the growth charge for the capacity that holds it is admitted",
          "a charge that differs between a cold read and a read on retained pooled scratch"
        ],
        "seeding": [
          "capacity-n: read a map literal of n distinct keys; the growthPlan is created fresh per literal in parseHashMap ('entryPlan := growthPlan{unit: MeterHashMapEntryBytes}'), so n is the only input",
          "header-charged: read a map literal of >= 9 distinct keys",
          "refused: read under allocCeilingContext(total-1) where total is the read's own admittedForSource, the self-derived boundary shape TestGuardedRead_AllowanceBoundary already uses",
          "pooled: readOwnedScratch against a *readerScratch the test owns, three times, as TestGuardedRead_PoolReuseChargesTheSameConstruction does"
        ],
        "budgets": [
          "logical capacities: 1, 2, 4, 8, 16, 32, ... ; each doubling admits the whole new capacity times MeterHashMapEntryBytes = 64",
          "MeterCollectionHeaderBytes = 24, admitted exactly once per promoted literal",
          "a 9-key literal admits 24 + entryBufferBytes(9) = 24 + (1+2+4+8+16)*64 = 24 + 1984 = 2008 bytes of construction storage",
          "the existing >= floor MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes = 600 stays satisfied (2008 >= 600), so TestGuardedRead_ConstructionStorage's comparison shape does not change",
          "promotion charges at most hashMapSmallLimit = 8 reduction units for the copy into the Go map",
          "exact-equality case owned by task 1.2, in TestGuardedRead_AdmitsOutputStorage: planBytes(21) + workBufferBytes(1) + MeterCollectionHeaderBytes + entryBufferBytes(9) + outputBytes(t, pairSource(9)) — the >= floor cannot tell 24 from 32 or from no header at all"
        ],
        "transitions": [
          {
            "input": "new key raising the logical entry count above the current capacity",
            "state": "capacity-n -> capacity-2n",
            "effect": "set",
            "evidence": "core/reader_budget.go: 'func (g *growthPlan) admit' charges 'g.capacity * g.unit' before the growth that fills it"
          },
          {
            "input": "a key the map already holds, in either form",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "core/reader.go: the 'if found' arm returns before plan.admit; the builder arm must probe m.large.m first and take the same shortcut"
          },
          {
            "input": "the 9th distinct key",
            "state": "capacity-8 -> header-charged + capacity-16",
            "effect": "forced",
            "evidence": "spec requirement: 'Map construction SHALL add the existing collection header and map entry units for its allocated entry storage, on the same deterministic growth schedule below and above the small-map threshold.'"
          },
          {
            "input": "a doubling whose charge exceeds the remaining allowance",
            "state": "refused",
            "effect": "forced",
            "evidence": "spec scenario: 'a map literal whose promoted entry storage exceeds the remaining allocation allowance ... SHALL fail with terminal ResourceLimitError before that storage is allocated'; core/reader_budget.go admitAlloc sets b.failed sticky"
          },
          {
            "input": "the same source read on retained pooled scratch",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "core/reader_budget.go: 'The schedule is logical, so a pooled buffer's retained capacity avoids the Go allocation but never the charge'; the per-literal entryPlan is fresh either way"
          },
          {
            "input": "the same source through the context-free core.Read (nil budget)",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "core/reader_budget.go: 'A nil *readerBudget is the absent budget: the context-free reader keeps its exact current behavior because every guarded site is a no-op without one'; the storage form must still match, which the parity test's 'read' entry pins"
          },
          {
            "input": "ReaderStats.Nodes and ReaderStats.Bytes for any map literal",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "spec requirement: 'Successful ReaderStats.Nodes and ReaderStats.Bytes SHALL remain unchanged for the same source'; TestGuardedRead_StatsUnchanged already covers promoted-map"
          }
        ]
      }
    },
    {
      "id": "trie-deletion",
      "taskIds": [
        "2.3"
      ],
      "prev": "entry-charge",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "guarded-trie-deletion",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "2.3",
          "file": "core/types.go",
          "symbol": "newGuardedTrie / assocGuarded / assocCollisionGuarded / mergeEntriesGuarded",
          "anchor": "func newGuardedTrie(entries []entry, extra entry, b *readerBudget) (*hamtNode, error) {",
          "change": "Delete all four (types.go: newGuardedTrie, assocGuarded, assocCollisionGuarded, mergeEntriesGuarded) once the two reader call sites are gone; they have no other non-test callers. Their unguarded originals stay: `func mergeEntries(a entry, ha uint32, b entry, hb uint32, shift uint) *hamtNode {` and `func (n *hamtNode) assoc(e entry, h uint32, shift uint) (*hamtNode, int64, bool) {`, plus the collision arm inside assoc — confirm each deleted case (replace-in-place, data→node merge, child descent, fresh data slot, collision replace, collision append, shift>=32 merge, same-fragment recursion) is still carried there. hamtSizeBytes stays live via hamtNodeBytes; hamtNodeBytes stays live via assoc/dissoc. readerBudget.admitAlloc keeps its other callers."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "2.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core && go build ./... && go vet ./core/... && golangci-lint run ./core/...",
      "coder": "go-coder",
      "contract": {
        "states": [
          "deleted",
          "retained"
        ],
        "forbidden": [
          "deleting hamtSizeBytes, hamtNodeBytes, MeterTrieChildBytes, assoc, mergeEntries or assocCollision handling",
          "leaving any of the four guarded symbols with zero callers in the tree",
          "a *readerBudget parameter surviving anywhere in core/types.go's map code other than newGuardedListChain"
        ],
        "seeding": [
          "deleted: remove the four functions; the compiler is the oracle, since an unused unexported func is not a build error but an unresolved call is",
          "retained: no action; hamtNodeBytes at 'return hamtSizeBytes(len(n.entries), len(n.children))' keeps hamtSizeBytes live"
        ],
        "budgets": [
          "exactly 4 symbols deleted",
          "0 remaining references to each, in production and test code alike"
        ]
      }
    },
    {
      "id": "charge-docs",
      "taskIds": [
        "3.1"
      ],
      "prev": "trie-deletion",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "charge-model-docs",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "T6 construction storage",
          "anchor": "- **T6** has four subterms: the flat collection copy-out, `ValueSlotsBytes(n)`;",
          "change": "Replace the fourth subterm (`and, per persistent-map node, its exact occupied size MeterCollectionHeaderBytes + 64*entries + 8*children, admitted before that node is allocated`) with the entry-buffer term for reader map construction above hashMapSmallLimit, and widen the small-map subterm to cover both sides of the limit. The T6 table row `| T6 construction storage | four subterms, below | before the collection's storage is allocated |` needs its count updated if the subterm count changes. Add an Unreleased CHANGELOG.md entry under the existing `## [Unreleased]` `### Changed` heading for the charge change. Verify no published unit lacks an owning ADR row (MeterTrieChildBytes is still owned only if some row still names it). State in the ADR that the promoted-buffer header MeterCollectionHeaderBytes = 24 is a construction-storage term, distinct from HashMapShallowBytes's MeterHashMapHeaderBytes = 32 output term, so the two are not read as one double charge."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "openspec validate reader-map-promotion-parity --strict --json && grep -q \"MeterTrieChildBytes\" docs/adr/0011-reduction-and-allocation-metering.md && grep -q \"entry buffer\" docs/adr/0011-reduction-and-allocation-metering.md && grep -q \"subterms\" docs/adr/0011-reduction-and-allocation-metering.md && grep -q \"reader map\" CHANGELOG.md",
      "coder": "zpatcher",
      "contract": {
        "states": [
          "adr-consistent",
          "changelog-recorded"
        ],
        "forbidden": [
          "a Meter* constant charged in code with no ADR 0011 table row owning it (MeterTrieChildBytes is the one this change can orphan)",
          "an ADR row describing a reader charge the reader no longer makes",
          "a CHANGELOG number not read off a run in task 0.1"
        ],
        "seeding": [
          "adr-consistent: edit docs/adr/0011-reduction-and-allocation-metering.md unit table and the T6 bullet",
          "changelog-recorded: add to the existing '## [Unreleased]' section of CHANGELOG.md"
        ],
        "budgets": [
          "3 ADR edits: 1 retitled row, 1 generalised row plus 1 added header row, 1 rewritten T6 bullet"
        ]
      }
    },
    {
      "id": "verify-core",
      "taskIds": [
        "3.2",
        "3.3"
      ],
      "prev": "charge-docs",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "change-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.2",
          "file": "Makefile",
          "symbol": "package verification commands",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "No edit. Run: go test -timeout 2m -p 2 -parallel 2 ./core ./runtime; go test -race -timeout 2m -p 2 -parallel 2 ./core; golangci-lint run ./core/... (use `env -C <worktree>` for the lint run). Record pass evidence per command."
        },
        {
          "task": "3.3",
          "file": "Makefile",
          "symbol": "build / lint / test targets",
          "anchor": "\tgo test $(GOTESTFLAGS) ./...",
          "change": "No edit. Run make build, make lint, make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'; record pass/failure evidence. Known flake to expect and report rather than chase: TestDecodeHashMap_Scaling in plugins/json has ~0.1 headroom on its 3.0 threshold under load."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "3.2",
        "3.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core && go test -race -timeout 2m -p 2 -parallel 2 ./core && make build && make lint",
      "coder": "coder",
      "contract": {
        "states": [
          "verified"
        ],
        "forbidden": [
          "claiming a latency verdict from a local BenchmarkGoldsetParse run",
          "moving any ADR 0008 gate threshold as part of this change",
          "closing 3.3 on a partial run: make test covers ./... and the json scaling test is a known load-sensitive flake"
        ],
        "seeding": [
          "verified: run the commands in verifyCommands and fullFloor and record their output"
        ],
        "budgets": [
          "-timeout 2m -p 2 -parallel 2 on every run",
          "both evaluator modes for 3.4 apply to BenchmarkGoldsetParse; TestGoldsetVMAllocations is hard-wired to ModeVM"
        ]
      }
    },
    {
      "id": "goldset-evidence",
      "taskIds": [
        "3.4",
        "3.5"
      ],
      "prev": "verify-core",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "change-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.4",
          "file": "internal/goldset/alloc_test.go",
          "symbol": "TestGoldsetVMAllocations",
          "anchor": "func TestGoldsetVMAllocations(t *testing.T) {",
          "change": "No edit expected. Compare against the pre-change baseline. TestGoldsetVMAllocations is hard-wired to NewEngine(ModeVM) and ignores GOLDSET_MODE, so the two-mode comparison is BenchmarkGoldsetParse only; the per-fixture ceilings live in the vmAllocCeilings map above the test. BenchmarkGoldsetParse is at internal/goldset/bench_test.go `func BenchmarkGoldsetParse(b *testing.B) {`; report B/op and allocs/op separately from ns/op and state plainly that latency deltas under this machine's floor are not a verdict. Confirm no ADR 0008 threshold moves. The gold set builds only the small map form, so a promoted-map change should be allocation-neutral there — say so if it is."
        },
        {
          "task": "3.5",
          "file": "openspec/changes/reader-map-promotion-parity/specs/core-engine/spec.md",
          "symbol": "Collection representation does not depend on its builder",
          "anchor": "### Requirement: Collection representation does not depend on its builder",
          "change": "No edit expected. Run openspec validate reader-map-promotion-parity --strict --json, then check the landed code against both scenarios of this ADDED requirement and the two MODIFIED scenarios (`A promoted map literal is charged on the entry-buffer schedule`, `Promotion is refused before its storage exists`), and against ADR 0011 T6 and the CHANGELOG entry."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "3.4",
        "3.5"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./internal/goldset && openspec validate reader-map-promotion-parity --strict --json",
      "coder": "coder",
      "contract": {
        "states": [
          "verified"
        ],
        "forbidden": [
          "claiming a latency verdict from a local BenchmarkGoldsetParse run",
          "moving any ADR 0008 gate threshold as part of this change",
          "closing 3.3 on a partial run: make test covers ./... and the json scaling test is a known load-sensitive flake"
        ],
        "seeding": [
          "verified: run the commands in verifyCommands and fullFloor and record their output"
        ],
        "budgets": [
          "-timeout 2m -p 2 -parallel 2 on every run",
          "both evaluator modes for 3.4 apply to BenchmarkGoldsetParse; TestGoldsetVMAllocations is hard-wired to ModeVM"
        ]
      }
    }
  ],
  "seams": [
    {
      "id": "map-charge-baseline",
      "tasks": [
        "0.1"
      ],
      "summary": "NO-TESTER-WAIVER: measurement record only. Run the current code and write down the admitted total for pairSource(1), pairSource(8), pairSource(9), pairSource(33) and pairSource(128) minus planBytes(tokens), plus the reductions charged, so the BREAKING delta this change publishes in CHANGELOG.md is a measured number rather than an asserted one. No production or test file changes. The 9-key figure is the one the CHANGELOG entry and the ADR 0011 amendment both quote; do not invent it, read it off a run.",
      "contract": {
        "states": [
          "baseline-recorded"
        ],
        "forbidden": [
          "publishing a before/after charge number in CHANGELOG.md or ADR 0011 that was not read off a run at HEAD 410e3eb8"
        ],
        "seeding": [
          "baseline-recorded: run the existing core tests with a temporary print, or compute admittedForSource on the five fixtures; record and discard the scaffolding"
        ],
        "budgets": [
          "5 fixture sizes: 1, 8, 9, 33, 128 keys",
          "hashMapSmallLimit = 8 is the boundary all five straddle"
        ],
        "transitions": [
          {
            "input": "a map literal at each of the five sizes read under DefaultMaxAllocationBytes",
            "state": "baseline-recorded",
            "effect": "set",
            "evidence": "core/reader_budget_accounting_test.go: func admittedForSource"
          }
        ]
      }
    },
    {
      "id": "reader-map-storage-form",
      "tasks": [
        "1.1",
        "2.1"
      ],
      "summary": "Parser.mapSet stops building its own trie and promotes into the builder form HashMap.Set uses: a plain map[hashKey]entry hung on largeMap.m, largeMap.root left nil. Task 1.1 writes the representation assertions (opening with the characterization of today's divergence) and 2.1 turns them green in the same chunk.",
      "contract": {
        "states": [
          "small",
          "builder",
          "trie",
          "unhashable-refusal",
          "flat",
          "shared-tail"
        ],
        "forbidden": [
          "trie reached by any read: a *HashMap produced by core.Read or ReadWithContextStats holds large.root != nil",
          "builder with large.root != nil (the two large forms are exclusive; core/types.go getByHashKey tests root first and would never consult m)",
          "small with large != nil",
          "builder with entries != nil (core/types.go Set clears h.entries = nil at promotion; leaving them makes Len() and eachRaw disagree)",
          "a reader-built map and a Set-built map of the same contents in different states"
        ],
        "seeding": [
          "small: read a map literal of 1..8 distinct keys, e.g. pairSource(4)",
          "builder: read a map literal of >= 9 distinct keys, e.g. pairSource(9); the only other legal path is HashMap.Set past hashMapSmallLimit",
          "trie: NOT reachable by reading after this change; reach it only through Assoc/Dissoc on a builder-form or small-form map (core/types.go Assoc calls trieFromBuildMap when large.root == nil), never by writing largeMap.root in a test",
          "unhashable-refusal: a map literal whose key is a List or Vector, refused by toHashKey before any entry storage exists"
        ],
        "budgets": [
          "hashMapSmallLimit = 8: the last size held in the sorted small form",
          "promotion copies exactly hashMapSmallLimit = 8 entries into the new Go map; the uninterruptible span is 8 map stores, replacing the current per-insert path copy of depth ceil(log32 n)",
          "parity fixtures: 1, 8, 9, 33, 128 keys plus the collision and mixed-key fixtures already in core/reader_map_parity_test.go"
        ],
        "transitions": [
          {
            "input": "new key, len(m.entries) < 8",
            "state": "small",
            "effect": "set",
            "evidence": "core/reader.go: 'if len(m.entries) >= hashMapSmallLimit' falls through to the sorted insert"
          },
          {
            "input": "key already in m.entries",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/reader.go: 'if found { m.entries[i] = e; return nil }'"
          },
          {
            "input": "new key, len(m.entries) == 8",
            "state": "small -> builder",
            "effect": "forced",
            "evidence": "core/types.go Set: 'm := make(map[hashKey]entry, len(h.entries)+1)' then 'h.large = &largeMap{m: m}' and 'h.entries = nil'"
          },
          {
            "input": "new key while m.large.m != nil",
            "state": "builder",
            "effect": "set",
            "evidence": "core/types.go Set: 'h.large.m[hk] = e'"
          },
          {
            "input": "key already in m.large.m",
            "state": "builder",
            "effect": "no-op",
            "evidence": "core/types.go Set: 'h.large.m[hk] = e' overwrites; Len() reads len(h.large.m) so the size does not move"
          },
          {
            "input": "two keys colliding under hashOfKey, both below and above the limit",
            "state": "builder",
            "effect": "no-op",
            "evidence": "hashKey equality, not hashOfKey, keys the Go map: a hash collision has no representational effect in either large form (core/types.go: 'e, ok := h.large.m[hk]')"
          },
          {
            "input": "a List or Vector key",
            "state": "unhashable-refusal",
            "effect": "forced",
            "evidence": "core/reader.go mapSet: 'hk, err := toHashKey(key)' returns before any entry, buffer or promotion exists"
          },
          {
            "input": "the same contents through HashMap.Set at 1 and 8 keys",
            "state": "small",
            "effect": "no-op",
            "evidence": "spec requirement: 'A collection produced by reading source SHALL use the same internal representation as the same collection produced through the public constructors, at every size.'"
          },
          {
            "input": "the same contents through HashMap.Set at 9, 33 and 128 keys",
            "state": "builder",
            "effect": "no-op",
            "evidence": "spec requirement: 'A collection produced by reading source SHALL use the same internal representation as the same collection produced through the public constructors, at every size.'"
          },
          {
            "input": "a list literal at 1 and 32 elements against NewList",
            "state": "flat",
            "effect": "no-op",
            "evidence": "core/types.go: newGuardedListChain 'builds the same chain newListChain does'; both branch at listFlatThreshold = 32, so these assertions are green on arrival and are regression pins, not red"
          },
          {
            "input": "a list literal at 33 and 100 elements against NewList",
            "state": "shared-tail",
            "effect": "no-op",
            "evidence": "core/types.go: newGuardedListChain 'builds the same chain newListChain does'; both branch at listFlatThreshold = 32, so these assertions are green on arrival and are regression pins, not red"
          }
        ]
      }
    },
    {
      "id": "reader-map-entry-charge",
      "tasks": [
        "1.2",
        "2.2"
      ],
      "summary": "The promoted entry storage is admitted on the same growthPlan doubling schedule the small form already rides, continued past the threshold, plus one MeterCollectionHeaderBytes = 24 admitted once at promotion. Trie-node units leave the reader entirely. Task 1.2 writes the amended and new charge expectations, 2.2 turns them green.",
      "contract": {
        "states": [
          "capacity-n",
          "header-charged",
          "refused"
        ],
        "forbidden": [
          "any admitAlloc of hamtSizeBytes or hamtNodeBytes on a reader path",
          "the promotion header charged more than once per map literal",
          "a growth charge for an insert that rewrites a key the map already holds (ADR 0011 T6: 'charged only when inserting a new key grows the buffer')",
          "the Go map allocated or an entry stored before the growth charge for the capacity that holds it is admitted",
          "a charge that differs between a cold read and a read on retained pooled scratch"
        ],
        "seeding": [
          "capacity-n: read a map literal of n distinct keys; the growthPlan is created fresh per literal in parseHashMap ('entryPlan := growthPlan{unit: MeterHashMapEntryBytes}'), so n is the only input",
          "header-charged: read a map literal of >= 9 distinct keys",
          "refused: read under allocCeilingContext(total-1) where total is the read's own admittedForSource, the self-derived boundary shape TestGuardedRead_AllowanceBoundary already uses",
          "pooled: readOwnedScratch against a *readerScratch the test owns, three times, as TestGuardedRead_PoolReuseChargesTheSameConstruction does"
        ],
        "budgets": [
          "logical capacities: 1, 2, 4, 8, 16, 32, ... ; each doubling admits the whole new capacity times MeterHashMapEntryBytes = 64",
          "MeterCollectionHeaderBytes = 24, admitted exactly once per promoted literal",
          "a 9-key literal admits 24 + entryBufferBytes(9) = 24 + (1+2+4+8+16)*64 = 24 + 1984 = 2008 bytes of construction storage",
          "the existing >= floor MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes = 600 stays satisfied (2008 >= 600), so TestGuardedRead_ConstructionStorage's comparison shape does not change",
          "promotion charges at most hashMapSmallLimit = 8 reduction units for the copy into the Go map",
          "exact-equality case owned by task 1.2, in TestGuardedRead_AdmitsOutputStorage: planBytes(21) + workBufferBytes(1) + MeterCollectionHeaderBytes + entryBufferBytes(9) + outputBytes(t, pairSource(9)) — the >= floor cannot tell 24 from 32 or from no header at all"
        ],
        "transitions": [
          {
            "input": "new key raising the logical entry count above the current capacity",
            "state": "capacity-n -> capacity-2n",
            "effect": "set",
            "evidence": "core/reader_budget.go: 'func (g *growthPlan) admit' charges 'g.capacity * g.unit' before the growth that fills it"
          },
          {
            "input": "a key the map already holds, in either form",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "core/reader.go: the 'if found' arm returns before plan.admit; the builder arm must probe m.large.m first and take the same shortcut"
          },
          {
            "input": "the 9th distinct key",
            "state": "capacity-8 -> header-charged + capacity-16",
            "effect": "forced",
            "evidence": "spec requirement: 'Map construction SHALL add the existing collection header and map entry units for its allocated entry storage, on the same deterministic growth schedule below and above the small-map threshold.'"
          },
          {
            "input": "a doubling whose charge exceeds the remaining allowance",
            "state": "refused",
            "effect": "forced",
            "evidence": "spec scenario: 'a map literal whose promoted entry storage exceeds the remaining allocation allowance ... SHALL fail with terminal ResourceLimitError before that storage is allocated'; core/reader_budget.go admitAlloc sets b.failed sticky"
          },
          {
            "input": "the same source read on retained pooled scratch",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "core/reader_budget.go: 'The schedule is logical, so a pooled buffer's retained capacity avoids the Go allocation but never the charge'; the per-literal entryPlan is fresh either way"
          },
          {
            "input": "the same source through the context-free core.Read (nil budget)",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "core/reader_budget.go: 'A nil *readerBudget is the absent budget: the context-free reader keeps its exact current behavior because every guarded site is a no-op without one'; the storage form must still match, which the parity test's 'read' entry pins"
          },
          {
            "input": "ReaderStats.Nodes and ReaderStats.Bytes for any map literal",
            "state": "capacity-n",
            "effect": "no-op",
            "evidence": "spec requirement: 'Successful ReaderStats.Nodes and ReaderStats.Bytes SHALL remain unchanged for the same source'; TestGuardedRead_StatsUnchanged already covers promoted-map"
          }
        ]
      }
    },
    {
      "id": "guarded-trie-deletion",
      "tasks": [
        "2.3"
      ],
      "summary": "NO-RED-WAIVER: pure symbol removal with no observable contract of its own. newGuardedTrie, hamtNode.assocGuarded, hamtNode.assocCollisionGuarded and mergeEntriesGuarded lose their only non-test callers when seam reader-map-storage-form lands (core/reader.go 'root, added, err := m.large.root.assocGuarded(...)' and 'root, err := newGuardedTrie(m.entries, e, p.budget)'); a grep at HEAD shows no test or plugin caller. Their behaviour is already carried by assoc, mergeEntries and the isCollision arm, which the collision fixture reaches through Assoc. hamtSizeBytes and MeterTrieChildBytes MUST survive: hamtNodeBytes still calls hamtSizeBytes and the evaluator's Assoc/Dissoc charge still prices child slots. Deleting them too would silently drop the evaluator's persistent-map charge.",
      "contract": {
        "states": [
          "deleted",
          "retained"
        ],
        "forbidden": [
          "deleting hamtSizeBytes, hamtNodeBytes, MeterTrieChildBytes, assoc, mergeEntries or assocCollision handling",
          "leaving any of the four guarded symbols with zero callers in the tree",
          "a *readerBudget parameter surviving anywhere in core/types.go's map code other than newGuardedListChain"
        ],
        "seeding": [
          "deleted: remove the four functions; the compiler is the oracle, since an unused unexported func is not a build error but an unresolved call is",
          "retained: no action; hamtNodeBytes at 'return hamtSizeBytes(len(n.entries), len(n.children))' keeps hamtSizeBytes live"
        ],
        "budgets": [
          "exactly 4 symbols deleted",
          "0 remaining references to each, in production and test code alike"
        ]
      }
    },
    {
      "id": "charge-model-docs",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-TESTER-WAIVER: prose deliverable. ADR 0011 needs three edits, not one. (a) The unit table row '| Reader persistent-map node | 24 bytes + 64 per entry + 8 per child |' is a reader row for a charge the reader will no longer make: retitle it to the evaluator's persistent-map node, which hamtNodeBytes still charges through Assoc/Dissoc, rather than deleting it -- deleting it orphans MeterTrieChildBytes = 8, which is exactly the 'a unit is published that no table row owns' failure task 3.1 asks you to check for. (b) Generalise '| Reader small-map entry slot | 64 bytes per logical slot |' to cover both sides of the threshold and add the once-per-promotion 24-byte header. (c) The T6 bullet says 'T6 has four subterms' and ends with the per-persistent-map-node subterm and a citation to TestGuardedRead_MapConstructionContracts; it becomes three subterms and the citation's meaning inverts. CHANGELOG.md [Unreleased] records the charge change with the measured before/after from task 0.1.",
      "contract": {
        "states": [
          "adr-consistent",
          "changelog-recorded"
        ],
        "forbidden": [
          "a Meter* constant charged in code with no ADR 0011 table row owning it (MeterTrieChildBytes is the one this change can orphan)",
          "an ADR row describing a reader charge the reader no longer makes",
          "a CHANGELOG number not read off a run in task 0.1"
        ],
        "seeding": [
          "adr-consistent: edit docs/adr/0011-reduction-and-allocation-metering.md unit table and the T6 bullet",
          "changelog-recorded: add to the existing '## [Unreleased]' section of CHANGELOG.md"
        ],
        "budgets": [
          "3 ADR edits: 1 retitled row, 1 generalised row plus 1 added header row, 1 rewritten T6 bullet"
        ]
      }
    },
    {
      "id": "change-verification",
      "tasks": [
        "3.2",
        "3.3",
        "3.4",
        "3.5"
      ],
      "summary": "NO-TESTER-WAIVER: verification and evidence capture, no new contract. 3.4's gold-set comparison is allocation-first: per the repo's own history the latency axis is not decidable on this machine, so report B/op and allocs/op as the verdict and say explicitly that timing deltas sit under the measurement floor rather than claiming a win. No ADR 0008 threshold moves in this change.",
      "contract": {
        "states": [
          "verified"
        ],
        "forbidden": [
          "claiming a latency verdict from a local BenchmarkGoldsetParse run",
          "moving any ADR 0008 gate threshold as part of this change",
          "closing 3.3 on a partial run: make test covers ./... and the json scaling test is a known load-sensitive flake"
        ],
        "seeding": [
          "verified: run the commands in verifyCommands and fullFloor and record their output"
        ],
        "budgets": [
          "-timeout 2m -p 2 -parallel 2 on every run",
          "both evaluator modes for 3.4 apply to BenchmarkGoldsetParse; TestGoldsetVMAllocations is hard-wired to ModeVM"
        ]
      }
    }
  ],
  "requirements": [
    {
      "shall": "A runtime-owned read SHALL reserve deterministic allocation charges before allocating token storage",
      "tests": [
        "TestGuardedRead_TokenPlanAdmission",
        "TestGuardedRead_AdmitsOutputStorage",
        "TestGuardedRead_ConstructionStorage"
      ]
    },
    {
      "shall": "The allocation model SHALL add 32 bytes per admitted token",
      "tests": [
        "TestGuardedRead_ExactCharges",
        "TestGuardedRead_ConstructionStorage",
        "TestGuardedRead_NumericConversionStorage"
      ]
    },
    {
      "shall": "Successful `ReaderStats.Nodes` and `ReaderStats.Bytes` SHALL remain unchanged for the same source",
      "tests": [
        "TestGuardedRead_StatsUnchanged",
        "TestReadWithContextStats_LegacyParity"
      ]
    },
    {
      "shall": "reading SHALL fail with terminal `ResourceLimitError` before allocating the full token array",
      "tests": [
        "TestGuardedRead_TokenPlanAdmission"
      ]
    },
    {
      "shall": "reading SHALL reject the payload before materializing an oversized decoded buffer",
      "tests": [
        "TestGuardedRead_EscapedPayloadAdmission",
        "TestGuardedRead_PrepaidPayloadKeepsOutputTotals"
      ]
    },
    {
      "shall": "reading SHALL return terminal `ResourceLimitError` before conversion or formatting the token",
      "tests": [
        "TestGuardedRead_NumericConversionStorage",
        "TestGuardedRead_InvalidNumberDiagnosticIsBounded"
      ]
    },
    {
      "shall": "it SHALL succeed with that total charge, and the same read with a ceiling one byte lower SHALL fail",
      "tests": [
        "TestGuardedRead_AllowanceBoundary",
        "TestGuardedRead_ExactCharges"
      ]
    },
    {
      "shall": "their values and `ReaderStats` SHALL be equal even though only the metered read charges workspace",
      "tests": [
        "TestReadWithContextStats_LegacyParity",
        "TestGuardedRead_StatsUnchanged"
      ]
    },
    {
      "shall": "the admitted construction storage SHALL be the collection header plus the map entry units",
      "tests": [
        "TestGuardedRead_ConstructionStorage",
        "TestGuardedRead_AdmitsOutputStorage",
        "TestGuardedRead_MapConstructionContracts"
      ]
    },
    {
      "shall": "reading SHALL fail with terminal `ResourceLimitError` before that storage is allocated",
      "tests": [
        "TestGuardedRead_PromotionRefusedBeforeStorage",
        "TestGuardedRead_AllowanceBoundary"
      ]
    },
    {
      "shall": "A collection produced by reading source SHALL use the same internal representation",
      "tests": [
        "TestReaderBuiltMapMatchesSetBuilt",
        "TestReaderBuiltListMatchesNewList"
      ]
    },
    {
      "shall": "Reading SHALL continue to admit the storage it allocates before allocating it",
      "tests": [
        "TestGuardedRead_ConstructionStorage",
        "TestGuardedRead_PoolReuseChargesTheSameConstruction"
      ]
    },
    {
      "shall": "both SHALL hold their entries in the same storage form, and every observable",
      "tests": [
        "TestReaderBuiltMapMatchesSetBuilt"
      ]
    },
    {
      "shall": "both SHALL hold their elements in the same storage form, and every observable SHALL agree",
      "tests": [
        "TestReaderBuiltListMatchesNewList"
      ]
    }
  ],
  "testHarness": [
    "readerEntry / readerEntries — core/reader_map_parity_test.go:18,23 — the two public source-to-forms paths (Read, and readContextStats over FullDialect) every parity fixture is run through",
    "readSingleForm — core/reader_map_parity_test.go:33 — reads src through one entry and fatals unless exactly one form comes back",
    "collisionKeys — core/reader_map_parity_test.go:48 — [2]int64{3367, 6372}, two Int keys colliding under hashOfKey so the trie exhausts its bits",
    "mapFixture — core/reader_map_parity_test.go:50 — {name, src, pairs, absent}: literal source plus the same content as constructor input",
    "intMapFixture — core/reader_map_parity_test.go:59 — n pairs of Int keys 0..n-1 mapped to i*10, as source and pairs",
    "collisionMapFixture — core/reader_map_parity_test.go:82 — 8 plain Int keys plus the two colliding ones, so the map promotes and reaches the collision arm",
    "mixedKeyMapFixture — core/reader_map_parity_test.go:109 — 9 pairs covering Keyword/String/Symbol/Int/Float/Bool/Nil keys, one past the small limit",
    "assertMapParity — core/reader_map_parity_test.go:179 — compares Len, Get per pair, Get(absent), Pairs, Each, String and Equals both ways between reader-built and Set-built maps",
    "eachPairs — core/reader_map_parity_test.go:243 — collects Each's visit order into [][2]Value",
    "assertListParity — core/reader_map_parity_test.go:286 — compares Len, At, ToSlice, Rest, String and Equals both ways between reader-built and NewList-built lists",
    "listCellBytes — core/reader_budget_accounting_test.go:12 — 32, one shared-tail list cell's construction charge",
    "listSource / vectorSource — core/reader_budget_accounting_test.go:14,15 — n-element list and vector literals of the same symbol, the pair that isolates the linked-cell term",
    "pairSource — core/reader_budget_accounting_test.go:19 — a map literal of n distinct keyword keys with symbol values, so only construction is admitted on top of the token plan",
    "sameKeySource — core/reader_budget_accounting_test.go:33 — n repeats of one key, so every insert past the first rewrites an existing entry",
    "entryBufferBytes — core/reader_budget_accounting_test.go:40 — the doubling-schedule total for an entry buffer of n entries, in MeterHashMapEntryBytes units; the test-side mirror of growthPlan",
    "allocationProbe — core/reader_budget_accounting_test.go:55 — a context whose Err() records the allocation ledger at every terminal-state check the read makes",
    "admittedForSource — core/reader_budget_accounting_test.go:66 — reads src under DefaultMaxAllocationBytes and returns the total admitted bytes; the baseline instrument for task 0.1",
    "readerTokenPlanBytes / planBytes — core/reader_budget_admission_test.go:14,20 — 32 bytes per planned token; planBytes(tokens) is the T1 term subtracted out of every construction expectation",
    "conversionBytes — core/reader_budget_admission_test.go:25 — 2*n + 256, the numeric-conversion storage term",
    "workBufferBytes — core/reader_budget_admission_test.go:32 — the value-slot doubling total for a reader work buffer of logical capacity n",
    "flatFormBytes — core/reader_budget_admission_test.go:49 — workBufferBytes(1) + workBufferBytes(children) + ValueSlotsBytes(children), one top-level flat collection's construction storage",
    "outputBytes — core/reader_budget_admission_test.go:58 — the output-node total for a source, derived from the context-free reader's own ReaderStats via ReaderAllocationBytes",
    "allocCeilingContext — core/reader_budget_admission_test.go:70 — a context with an ample reduction budget and exactly maxAllocBytes of allocation allowance, plus its EvalMeter",
    "admittedBytes — core/reader_budget_admission_test.go:75 — Snapshot().AllocationBytes off a meter",
    "readOwnedScratch — core/reader_budget_admission_test.go:80 — drives a test-owned readerScratch through one guarded read, so repeat reads hit retained capacity deterministically instead of sync.Pool",
    "budgetContext — core/reader_budget_cancel_test.go:13 — a context with a reduction ceiling, for work-side (not storage-side) limits",
    "chargedReductions — core/reader_budget_cancel_test.go:18 — Snapshot().Reductions off a meter",
    "readErrorCode — core/reader_budget_cancel_test.go:67 — the error code string of a read failure, compared against CodeResourceLimit",
    "longKeySource — core/reader_budget_cancel_test.go:79 — a map literal of long string keys, driving the per-key hash work charge",
    "collidingKeySource — core/reader_budget_cancel_test.go:92 — a map literal of keys that collide, driving the collision-scan work charge",
    "readContextStats — core/reader_context_parity_test.go:14 — the guarded entry point every budget test reads through (forms, ReaderStats, err)",
    "guardedScratchRead — core/reader_scratch_release_test.go:17 — one guarded read on a caller-owned scratch with the budget still installed at release time",
    "assertScratchCleared — core/reader_scratch_release_test.go:31 — checks a released scratch pins no source, token vals, nodes or budget, to full retained capacity",
    "Fixtures — internal/goldset/goldset.go:77 — the gold-set fixture loader BenchmarkGoldsetParse and TestGoldsetVMAllocations both read",
    "vmAllocCeilings — internal/goldset/alloc_test.go (map above line 45) — the per-fixture allocation counts the release gate measures"
  ],
  "floor": "make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 5
  }
}
```
