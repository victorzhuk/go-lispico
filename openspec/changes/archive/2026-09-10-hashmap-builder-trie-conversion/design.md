## Context

See proposal.md — Why. This section records only what the measurement added.

The gap was measured on the current tree (commit `e45c916`) with a throwaway probe
in `package core`, since `trieFromBuildMap` and the two large forms are
unexported. One `Assoc` against a retained receiver, receiver built by `Set`
(builder form) versus by repeated `Assoc` (trie form):

| n | charge, builder | charge, trie | allocs/op, builder | allocs/op, trie | B/op, builder | B/op, trie |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 9 | 3 792 | 696 | 37 | 7 | 8 183 | 1 481 |
| 100 | 75 440 | 624 | 554 | 8 | 126 018 | 891 |
| 1 000 | 1 085 048 | 1 600 | 6 480 | 10 | 1 794 146 | 2 325 |

Benchmarks at `-benchtime=500x -benchmem`. The timing followed the same shape (354 ns
versus 4.2 µs at n=9, 681 ns versus 506 µs at n=1000) but a latency claim is not
decidable here and is recorded as observation only.

**Those digits are one run's, and they do not reproduce.** Re-measuring at the
implementation base found the same order of magnitude and the same shape, but the
builder arm's charge moved up to ~25% between runs of the identical benchmark —
74 224 / 79 200 / 94 112 at n=100. The cause is in the function this change is
about: `trieFromBuildMap` iterates `h.large.m`, a Go map, so insertion order into
the trie is randomised per process. The finished trie is the same either way, but
the path copies made along the way are not, and their bytes are what the charge
sums. The trie arm is stable across repeats of one key but is path-dependent too:
a different retained key selects a different branch depth, so a probe using an
existing key rather than a new one gave 600 / 560 / 1448.

Two consequences, both load-bearing:

- **The exact digits are not the contract, the shape is.** Every assertion this
  change adds compares the arms or bounds growth against n; none pins a charge to
  an exact value, and the CHANGELOG quotes an order-of-magnitude effect rather
  than a number that will not reproduce on the reader's machine.
- **The conversion's charge is not deterministic today.** ADR 0011 describes the
  metering terms as deterministic and the kernel invariants call evaluation
  deterministic for the same input and environment; a charge that moves 25% on an
  unchanged input does not meet that. It is pre-existing — the memo changes how
  *often* the conversion is charged, not how much — and it is recorded here rather
  than fixed here.

  **Superseded before implementation.** The non-determinism this section records was
  fixed by `trie-conversion-charge-determinism`, archived on 2026-09-10, which made
  `trieFromBuildMap` insert through `sortedEntries()` in `hashKey.less` order instead
  of ranging the Go map. This change was implemented on top of that fix, so the
  ~25% run-to-run spread described above no longer occurs: task 0.1 re-measured the
  builder arm at this change's own base and two runs agreed exactly on charge and
  allocations at every size. The reasoning above is kept because it is why the design
  refuses to pin exact digits, and that conclusion still holds — the figures depend on
  the key set. Only the instability is gone.

The trie arm is flat — 7 to 10 allocations across two orders of magnitude, which
is the bound the requirement states. The builder arm rises linearly with entry
count: at n=1000 a single update allocates 6 480 objects and charges 1.06 MB.
Against `DefaultMaxAllocationBytes` (64 MiB) that is **62 updates before one
evaluation's entire allocation budget is gone**, for storage no caller retains.
The failure mode is therefore not only slowness: a Lisp program doing fan-out
`assoc` against a large map literal can take a `ResourceLimitError` it did nothing
to deserve.

## Goals / Non-Goals

**Goals:**

- Repeated updates against one bulk-built receiver charge and allocate per update
  on the same order as the trie form already does.
- The conversion is paid once per map value, the way promotion past
  `hashMapSmallLimit` is already paid once.
- The gap stays measured: a committed benchmark covers both arms at sizes spanning
  two orders of magnitude.

**Non-Goals:**

- Changing which representation any builder produces. Reader and `Set` both keep
  landing in builder form; the parity requirement that landed with
  `reader-map-promotion-parity` is not reopened.
- Touching `getByHashKey`, `Len`, iteration order, equality or printing.
- Making the *first* update on a bulk-built map O(depth). Building a trie from n
  entries is inherently O(n); the goal is that it happens once, not that it stops
  happening.

## Decisions

### Memoise the converted trie on the receiver, rather than making the conversion cheaper

Building the trie in one bottom-up pass instead of n path-copying `assoc` calls
was the obvious contained alternative, and it is not enough. It removes the
`ceil(log32 n)` factor and the throwaway paths, so n=1000 would fall from ~6 480
allocations to roughly the ~65 nodes the finished trie needs — a large constant
win, but still O(n) *per update*. The measured shape stays linear, the budget
hazard survives at larger n, and the requirement's bound is still not met. It
would be optimising the symptom.

Memoisation is what changes the shape: the first update on a value pays O(n), and
every later update against that same receiver pays O(depth), which is the trie
arm already measured. Fan-out of k updates goes from O(k·n) to O(n + k·depth).

The one-pass build stays available as a later, independent improvement to the
once-per-value cost. It is not in this change.

### The memo lives beside `large.m`, not in `large.root`

`large.root` is not a cache slot. `getByHashKey` tests `root` before `m`, and
`Len()` reads `large.count` — zero for a builder-form map — so writing `root`
would silently move the read path onto the trie and break `Len`. The memo is a
separate field on `largeMap`, written and read only by `Assoc` and `Dissoc`.
`large.m` stays authoritative for every read, for `Len`, and for iteration.

Consequence worth stating: the two large forms remain mutually exclusive as the
spec describes them. A memoised map is still a builder-form map; it merely knows
what its trie would be.

### Publication is atomic, and a lost race is discarded

Two goroutines may reach the conversion at once. The memo is an
`atomic.Pointer`-style slot: each converts, one compare-and-swap wins, the loser
drops its copy and proceeds with its own root. Both allocated, so both charge —
that is honest accounting, not a leak. The fixed-seed hash makes the two tries
structurally identical, so which one wins is not observable.

This is why a plain field plus a mutex is not proposed: reads of the memo sit on
the update path of an immutable value that callers are entitled to share across
goroutines, and ADR 0003 already forbids introducing a lock there.

### The conversion is charged once per value, and that is a metering change

Today every update re-charges the conversion. After this change the first update
charges it and later ones do not, which is a truer statement of what was
allocated. Two consequences are real and must be written down rather than
discovered:

- Exact-charge tests that assoc twice against one receiver will see different
  totals for the two calls. They are pinning a defect today.
- When a map value is shared across evaluations, whichever evaluation updates it
  first bears the conversion. Today every evaluation bears it. First-toucher-pays
  is the same rule already used for other lazily obtained storage, but ADR 0011
  must say so, because "the same source charges the same total" no longer holds
  for a *shared receiver* the way it holds for a source read.

- A first toucher that is then *refused* still bears it. `Assoc` takes no context
  and returns bytes for its caller to charge, and the allocation ceiling is
  enforced after it returns (`plugins/stdlib/collections.go:553`,
  `core/metering.go:479-498`), so the memo is already published before any ledger
  decision exists and `Assoc` has no way to unpublish it. The refused evaluation
  pays for a conversion the next one uses for free. Making that not so would mean
  changing `Assoc`'s signature, which the rollback boundary below rules out.

The two refusals therefore have opposite contracts, and the tests must not conflate
them: a key-domain refusal publishes no memo, because the error precedes the
conversion; an allocation-ceiling refusal publishes it and is billed for it.

Recorded as a decision rather than an open question because it changes ADR 0011's
text and the task breakdown.

### Immutability gets an explicit carve-out, not a silent exception

`CLAUDE.md` lists immutable data structures as a kernel invariant and
`trieFromBuildMap`'s own comment promises the receiver is untouched. A memo
mutates a field of a shared object. The carve-out is that the memo is derived
state: it is a pure function of `large.m`, which never changes after bulk
construction ends, and no observable — value, equality, iteration order, printing,
`Len` — differs before or after it is populated. ADR 0003 states the carve-out in
those terms, so the invariant keeps meaning something.

The narrow claim this rests on: `Set` is documented as the bulk-construction
escape hatch used "while nothing is shared yet". A `Set` after a memo is populated
would invalidate it. Implementation clears the memo on `Set`, which is one store
on a path that is already mutating.

## Risks / Trade-offs

- **A `Set` call after the memo is populated leaves a stale trie** → `Set` clears
  the memo. Cheap, and the case is already outside the documented contract. Note
  the ordering cost this leaves: the memo starts publishing one task before `Set`
  learns to clear it, so the branch carries an intermediate commit where that
  window is open and no test closes it. Every intermediate commit reaches the
  default branch, because the run merges fast-forward — so this is a bisect
  hazard, not a release one, and it is accepted knowingly rather than paid for
  with a larger task.
- **First-toucher-pays makes one evaluation's charge depend on another's
  timing** → stated in ADR 0011 and pinned by a test that shares a map across two
  meters and asserts exactly one of them is charged the conversion.
- **The memo keeps a trie alive for a map that is never updated again** → it is
  only ever populated by an update, so a read-only map never grows one. A map
  updated once and then retained holds both forms; that is the same trade the
  trie form already makes, bounded by the map's own size.
- **Retained accounting will not count the memo** → the compiler folds a constant
  map literal into one builder-form `*HashMap` in the constant pool
  (`core/compiler/compiler.go:1285-1301`) and the VM pushes that same pointer on
  every execution (`core/vm/vm.go:966-969`), so a memo on it lives for the chunk's
  lifetime while ADR 0012's retained walk still prices the map by `Len` alone
  (`core/value_walk_context.go:262-263`). ADR 0011 states that the memo is charged
  to the evaluation that built it and is not re-charged as retained capacity;
  counting it is an ADR 0012 change and is **raised as its own change, not absorbed
  here** — adding it would put this change past the 12-task size gate.
- **`go vet` copylocks, once `largeMap` holds an atomic** → every construction site
  already takes `&largeMap{…}` and every reference is a `*largeMap`, so the type is
  never copied; the vet run in each chunk's verify is what proves it stayed that way.
- **The benchmark becomes a flaky gate if it asserts timing** → it asserts
  allocations and bytes, which are exact here; timing is reported, never gated.
  This follows the same rule ADR 0008's gate already uses.
- **Regression risk to the metering tests is broad** → the change is sequenced so
  the charge tests are amended in the same step that changes the charge, the way
  `reader-map-promotion-parity` sequenced its own charge change.

## Migration Plan

No data or API migration. The change is additive to `largeMap` and invisible to
embedders except through the charge, which is recorded in `CHANGELOG.md` under
`[Unreleased]` as a metering change, with the measured before and after from the
table above.

Rollback is deleting the memo field and its two call sites; nothing persists it
and no format depends on it.

## Open Questions

None. The two questions that were open at proposal time — whether amortisation is
worth its cost, and which mechanism to use — are answered above by the
measurement.

## Implementation plan

Tier **heavy**, testing mode **existing-service-strict**, true at `d40567e6`. 8 chunks, dispatched in the order below; every chunk is serial behind its predecessor because they all write `core`, so there is no parallel wave and no shard.

### 1. `baseline` — tasks 0.1

- Seam `S0-baseline`, first chunk. Coder **coder**.
- No red stage. NO-RED-WAIVER: measurement only, no production symbol and no observable contract.
- Seals `_test.go` under `core`; packages `./core`.
- Sites:
  - `openspec/changes/archive/2026-09-10-reader-map-promotion-parity/proposal.md` :: archived change directory — anchor `## Why`
    confirmed present at base d40567e; its requirement text is live at openspec/specs/core-engine/spec.md:152 and does NOT yet carry the builder-form paragraph or the new scenario
  - `core/bench_test.go` :: pre-change fan-out measurement — anchor `func BenchmarkHashMap_GetLarge(b *testing.B) {`
    record charge / allocs-op / B-op at n = 9, 100, 1000 for both arms off the base commit; builder arm = receiver built by Set, trie arm = receiver built by repeated Assoc (the distinction BenchmarkHashMap_SetBuild :531 and BenchmarkHashMap_GetLarge :549 already encode in their comments)
- Verify: `go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestHashMap_'`

### 2. `memo-scaffold` — tasks 2.1

- Seam `S8-memo-scaffold`, serial behind `baseline` (shared `core`). Coder **go-coder**.
- No red stage. NO-RED-WAIVER: declaration and pure refactor with no observable contract of its own.
- Seals `_test.go` under `core`; packages `./core`.
- Sites:
  - `core/types.go` :: largeMap — anchor `type largeMap struct {`
    Declare the memo slot, pinned as `memo atomic.Pointer[hamtNode]` — one shape, so the next chunk's tests name the field the coder declared. Declaration only: nothing reads or writes it in this task.
  - `core/types.go` :: import block — anchor `"math/bits"`
    Add "sync/atomic". Package core does not import it yet; the CAS idiom in core/vm/chunk.go:149-152 is the shape to follow.
  - `core/types.go` :: (*HashMap).trieRoot *(new)* — anchor `func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {`
    Create trieRoot() (root *hamtNode, count int, bytes int64) beside trieFromBuildMap. In THIS task it only carries what the two call sites already do: trie form returns (large.root, large.count, 0); builder form calls trieFromBuildMap and returns its root, len(large.m) and the full conversion bytes. No memo read, no memo write — that is task 2.2. Leave trieFromBuildMap itself unchanged.
  - `core/types.go` :: HashMap.Assoc — anchor `func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {`
    Replace the conversion arm at core/types.go:1118-1123 with a trieRoot() call. Byte-for-byte behaviour and charges, so the existing suite stays green unchanged.
  - `core/types.go` :: HashMap.Dissoc — anchor `func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {`
    Replace the byte-identical arm at core/types.go:1155-1160 with the same trieRoot() call. This is why the helper exists: one conversion path, two callers.
- Verify: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go build ./... && go vet ./core/...`

### 3. `fanout-and-memo` — tasks 1.1, 2.2

- Seam `S1-fanout-bound`, serial behind `memo-scaffold` (shared `core`). Coder **go-coder**.
- Red 1.1 → code 2.2. Red tests: `TestHashMap_FanOutAssocStaysBounded`, `TestHashMap_FanOutDissocStaysBounded`, `TestHashMap_FanOutFromMapLiteralStaysBounded`, `TestHashMap_FanOutUnderDefaultAllocationCeiling`, `TestHashMap_MemoHoldsConvertedTrie`, `TestHashMap_MemoLeavesReadPathOnBuilderForm`, `TestHashMap_ConversionChargedOncePerValue`, `TestMetering_MapConversionChargedToFirstToucher`.
- Seals `_test.go` under `core`, `runtime`; packages `./core`, `./runtime`.
- Sites:
  - `core/bench_test.go` :: BenchmarkHashMapFanOutAssoc *(new)* — anchor `func BenchmarkHashMap_AssocChain(b *testing.B) {`
    new benchmark beside the existing map benchmarks; both arms at n = 9, 100, 1000; b.ReportAllocs(); receiver RETAINED across iterations (fan-out), not threaded — the threaded shape is already BenchmarkHashMap_AssocChain
  - `core/hashmap_test.go` :: test binding scenario Repeated updates on a bulk-built map do not re-pay its conversion *(new)* — anchor `func TestHashMap_Assoc_AllocsPerRun(t *testing.T) {`
    new test beside the existing allocation pins; asserts the int64 Assoc returns (the charge) and testing.AllocsPerRun over k updates against one Set-built receiver, plus a map-literal-built receiver via the reader; never asserts timing; must fail today at n=100 and n=1000 and pass for the trie arm
  - `core/hashmap_test.go` :: cross-meter first-toucher-pays test *(new)* — anchor `func TestHashMap_Immutability(t *testing.T) {`
    new test: one builder-form receiver, two independent contexts from allocCeilingContext (core/reader_budget_admission_test.go:70), each charging its Assoc result through the ledger; exactly one sees the conversion bytes. NOTE: core Assoc does not charge, so the test charges the returned bytes itself, or lives in stdlib driving the real assoc builtin (monotonic_test.go pattern) if a real meter is required
  - `plugins/stdlib/monotonic_test.go` :: TestAssocMonotonic_ChargesPerCallHonestly — anchor `func TestAssocMonotonic_ChargesPerCallHonestly(t *testing.T) {`
    the only existing per-call charge assertion on the assoc builtin. It CHAINS (m = next each iteration) from an empty map, so it never reaches the builder-form fan-out shape and its bounds are unaffected. Nearest existing home for a stdlib-level cross-meter test
  - `core/types.go` :: (*HashMap).trieRoot *(new)* — anchor `func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {`
    Give trieRoot its memo semantics, inside the helper task 2.1 created and nowhere else. The field is `largeMap.memo atomic.Pointer[hamtNode]`, declared by task 2.1 — that exact spelling, so a test naming it compiles. On a builder-form receiver: load `large.memo`; on a hit return (memo, len(h.large.m), 0) — zero bytes, because nothing was allocated. On a miss build through trieFromBuildMap, CompareAndSwap nil -> root, and return its own root with the full conversion bytes whether or not the CAS won, since a losing racer allocated too. large.root and large.count are not written; large.m stays authoritative for every read and for Len.
- Red run: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime -run 'TestHashMap_FanOut|TestHashMap_Memo|TestHashMap_ConversionChargedOncePerValue|TestMetering_MapConversionChargedToFirstToucher'`
- Verify: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go build ./... && go vet ./core/...`

### 4. `set-clears-memo` — tasks 2.3

- Seam `S4-set-clears-memo`, serial behind `fanout-and-memo` (shared `core`). Coder **go-coder**.
- Red 2.3 → code 2.3. Red tests: `TestHashMap_SetClearsMemo`, `TestHashMap_SetThenAssocMatchesConversionFreePath`.
- Seals `_test.go` under `core`; packages `./core`.
- Sites:
  - `core/types.go` :: HashMap.Set — anchor `h.large.m[hk] = e`
    clear the memo at the head of the h.large != nil branch (:1217) so a bulk write after a memoised update cannot leave a stale trie; the trie sub-branch (:1218-1224) mutates large.root in place and is unaffected, but sits inside the same branch, so one clear at the branch head covers both
- Red run: `go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestHashMap_Set(ClearsMemo|ThenAssocMatchesConversionFreePath)'`
- Verify: `go test -timeout 2m -p 2 -parallel 2 ./core && go build ./... && go vet ./core/...`

### 5. `refusal-and-race` — tasks 2.4

- Seam `S5-refusal-and-race`, serial behind `set-clears-memo` (shared `core`). Coder **go-coder**.
- Red 2.4 → code 2.4. Red tests: `TestHashMap_InvalidKeyPublishesNoMemo`, `TestHashMap_ConcurrentUpdatesPublishOneMemo`, `TestMetering_RefusedConversionStillSettlesTheCharge`.
- Seals `_test.go` under `core`, `runtime`; packages `./core`, `./runtime`.
- Sites:
  - `core/types.go` :: conversion charge on the Assoc/Dissoc path — anchor `root, bytes = h.trieFromBuildMap()`
    this exact line appears TWICE (:1121 Assoc, :1158 Dissoc) — anchor on the enclosing func signatures instead. After the change the conversion bytes are returned only by the call that actually built the trie; a memo hit returns only the path bytes b
  - `plugins/stdlib/collections.go` :: chargeConsResult — anchor `return core.ChargeGoFuncResultBytes(ctx, bytes)`
    the refusal point. The charge is admitted AFTER Assoc allocated and after the memo would be published, so a refused conversion publishes no memo has no in-core enforcement point today — see risks
  - `core/reader_budget_accounting_test.go` :: TestGuardedRead_PromotionRefusedBeforeStorage — anchor `func TestGuardedRead_PromotionRefusedBeforeStorage(t *testing.T) {`
    the existing wide-map admit-before-allocate pin; it covers reader promotion into builder form only and builds no trie (ADR 0011 line 56: no reader path builds a trie node). Must stay green unchanged
- Red run: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime -run 'TestHashMap_(InvalidKeyPublishesNoMemo|ConcurrentUpdatesPublishOneMemo)|TestMetering_RefusedConversionStillSettlesTheCharge'`
- Verify: `go test -race -timeout 2m -p 2 -parallel 2 ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime && go build ./... && go vet ./core/...`

### 6. `charge-docs` — tasks 3.1

- Seam `S6-docs`, serial behind `refusal-and-race` (shared `repo`). Coder **zpatcher**.
- No red stage. NO-RED-WAIVER: prose deliverables, no runtime behaviour and no assertable transition.
- Sites:
  - `docs/adr/0011-reduction-and-allocation-metering.md` :: Evaluator persistent-map node unit row — anchor `| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |`
    the unit row that owns the conversion bytes; the paragraph at line 56 already states it is an evaluator charge applied through hamtSizeBytes on the Assoc/Dissoc path. Add that the builder-to-trie conversion is a per-value charge settled by the first update
  - `docs/adr/0011-reduction-and-allocation-metering.md` :: Determinism requirement — anchor `## Determinism requirement`
    state the cross-evaluation attribution: a shared receiver's conversion is borne by the first evaluation that updates it, so same source charges the same total holds per source read but not for a shared receiver
  - `docs/adr/0003-concurrency-model.md` :: Consequences — anchor `## Consequences`
    state the derived-state carve-out to immutability: the memo is a pure function of large.m, published atomically, and no observable (value, equality, iteration order, printing, Len) differs before or after. ADR 0003 is 13 lines today and currently governs only per-evaluation counters and env locking — the carve-out widens its stated scope
  - `CHANGELOG.md` :: [Unreleased] Changed — anchor `## [Unreleased]`
    metering entry under Changed (### Added at :10, ### Changed at :28) carrying the measured before/after from task 0.1
- Verify: `openspec validate hashmap-builder-trie-conversion --strict --json && grep -q "not re-charged as retained" docs/adr/0011-reduction-and-allocation-metering.md && grep -q "derived state" docs/adr/0003-concurrency-model.md && grep -q "once per value" CHANGELOG.md`

### 7. `verify-core` — tasks 3.2, 3.3

- Seam `S7-verification`, serial behind `charge-docs` (shared `repo`). Coder **coder**.
- No red stage. NO-RED-WAIVER and NO-TESTER-WAIVER: verification and evidence recording only, it asserts no new behaviour of its own.
- Sites:
  - `core/types.go` :: verification commands — anchor `type largeMap struct {`
    go test -timeout 2m -p 2 -parallel 2 ./core ./runtime; go test -race -timeout 2m -p 2 -parallel 2 ./core; golangci-lint run ./core/... — run with env -C at the worktree root, since absolute paths from a foreign cwd give exit 7 with No issues found
  - `Makefile` :: build / lint / test targets — anchor `GOTESTFLAGS ?= -timeout 2m`
    make build (:10 builds bin/lispico), make lint (:19 bare golangci-lint run), make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' (:14)
- Verify: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -race -timeout 2m -p 2 -parallel 2 ./core && make build && make lint`

### 8. `goldset-evidence` — tasks 3.4, 3.5

- Seam `S7-verification`, serial behind `verify-core` (shared `repo`). Coder **coder**.
- No red stage. NO-RED-WAIVER and NO-TESTER-WAIVER: verification and evidence recording only, it asserts no new behaviour of its own.
- Sites:
  - `internal/goldset/bench_test.go` :: BenchmarkGoldsetParse — anchor `func BenchmarkGoldsetParse(b *testing.B) {`
    mode comes from GOLDSET_MODE (eval|vm) via the switch at :16-22; the Makefile profile target (:27-28) shows the exact invocation shape. Allocations/bytes are the verdict axis, timing observation only
  - `internal/goldset/alloc_test.go` :: TestGoldsetVMAllocations — anchor `var vmAllocCeilings = map[string]int{`
    13 pinned per-fixture counts; every fixture builds only small maps (below hashMapSmallLimit), so no count is expected to move. File is //go:build !race (:1), so it does not run in the -race pass
  - `openspec/changes/hashmap-builder-trie-conversion/specs/core-engine/spec.md` :: openspec validate --strict — anchor `## MODIFIED Requirements`
    openspec validate hashmap-builder-trie-conversion --strict --json; the delta modifies Map representation efficiency, live at openspec/specs/core-engine/spec.md:152, by adding one paragraph and one scenario
- Verify: `go test -timeout 2m -p 2 -parallel 2 ./internal/goldset && openspec validate hashmap-builder-trie-conversion --strict --json`

### Floor, lenses and verdict

- Floor: `make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 2m -p 2 -parallel 2 ./core && openspec validate hashmap-builder-trie-conversion --strict --json`
- Lenses: `spec`, `quality`, `perf` — `spec` and `quality` from the heavy tier, `perf` because the change exists to move an allocation bound on a hot path. No `arch` (no package or boundary moves) and no `sec` (no auth, input, SQL, secrets or I/O).
- Plan review: **pass** by zarchitect after 3 rounds.
- Waivers: `S0-baseline` (NO-RED-WAIVER: measurement only, no production symbol and no observable contract.) `S6-docs` (NO-RED-WAIVER: prose deliverables, no runtime behaviour and no assertable transition.) `S7-verification` (NO-TESTER-WAIVER: verification and evidence recording only, it asserts no new behaviour of its own.) `S8-memo-scaffold` (NO-RED-WAIVER: declaration and pure refactor with no observable contract of its own.)

### Rules for an agent without the kernel

If this plan is executed by an agent that does not load the run kernel, these are not optional and are not inlined anywhere else:

- Assert the worktree before touching anything: `git -C <worktree> rev-parse --show-toplevel` must equal the worktree path; stop if it does not. Every path is worktree-absolute, every git call is `git -C <dir>`, never `cd <dir> &&`.
- A contract test, once written, is read-only. A coder that needs one changed stops and says so; it does not edit it.
- One conventional commit per finished task: subject ≤72 chars, imperative, lowercase, type from `feat fix refactor perf docs test build ci chore revert`; body lines ≤100. Never `--no-verify`, never a git identity override.
- Terse, high-signal output. No AI/tool/process references in code, comments, commits or prose. Native file tools for reading and searching; the shell only for commands that run something.
- Every test run is resource-limited: `-timeout 2m -p 2 -parallel 2` on unit runs, and `-race` only where a chunk says so.
- Assert allocations and bytes, never timing: latency is not decidable on this machine and is reported as observation only.

## Plan appendix

```json
{
  "v": 2,
  "change": "hashmap-builder-trie-conversion",
  "baseSha": "990fa5554173f4ba6750cece6c59ecb425054695",
  "generatedAt": "2026-09-10T16:50:57.000Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "estimateHours": 3.3,
  "chunks": [
    {
      "id": "baseline",
      "taskIds": [
        "0.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "S0-baseline",
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
          "file": "openspec/changes/archive/2026-09-10-reader-map-promotion-parity/proposal.md",
          "symbol": "archived change directory",
          "anchor": "## Why",
          "change": "confirmed present at base d40567e; its requirement text is live at openspec/specs/core-engine/spec.md:152 and does NOT yet carry the builder-form paragraph or the new scenario"
        },
        {
          "task": "0.1",
          "file": "core/bench_test.go",
          "symbol": "pre-change fan-out measurement",
          "anchor": "func BenchmarkHashMap_GetLarge(b *testing.B) {",
          "change": "record charge / allocs-op / B-op at n = 9, 100, 1000 for both arms off the base commit; builder arm = receiver built by Set, trie arm = receiver built by repeated Assoc (the distinction BenchmarkHashMap_SetBuild :531 and BenchmarkHashMap_GetLarge :549 already encode in their comments)"
        }
      ],
      "contract": {
        "states": [
          "builder-arm",
          "trie-arm"
        ],
        "transitions": [
          {
            "input": "one Assoc against a retained receiver, receiver built by Set, n in {9,100,1000}",
            "state": "builder-arm",
            "effect": "forced",
            "evidence": "design.md:10-14 records charge 3792/75440/1085048, allocs 37/554/6480, B/op 8183/126018/1794146 — a pre-trie-conversion-charge-determinism reading, superseded at this baseSha: task 0.1 re-records all three and those figures are what every later budget compares against"
          },
          {
            "input": "one Assoc against a retained receiver, receiver built by repeated Assoc, n in {9,100,1000}",
            "state": "trie-arm",
            "effect": "no-op",
            "evidence": "design.md:10-14 records charge 696/624/1600, allocs 7/8/10, B/op 1481/891/2325"
          }
        ],
        "forbidden": [
          "recording a latency number as a verdict (memory: perfgate-not-local; design.md:16-19 records timing as observation only)",
          "recording figures from any commit other than the change base"
        ],
        "seeding": {
          "builder-arm": "m := NewHashMap(); for i := range n { m.Set(Int{V:int64(i)}, Int{V:int64(i)}) } — precedent core/bench_test.go:531-543",
          "trie-arm": "m := NewHashMap(); for i := range n { m, _, _ = m.Assoc(Int{V:int64(i)}, Int{V:int64(i)}) } — precedent core/bench_test.go:549-558"
        },
        "budgets": {
          "benchtime": "-benchtime=500x, matching design.md:16",
          "sizes": "exactly n in {9,100,1000}",
          "verdictAxes": "allocs/op and B/op only; ns/op recorded as observation"
        }
      },
      "redTasks": [],
      "codeTasks": [
        "0.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestHashMap_'",
      "coder": "coder"
    },
    {
      "id": "memo-scaffold",
      "taskIds": [
        "2.1"
      ],
      "prev": "baseline",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "S8-memo-scaffold",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "largeMap",
          "anchor": "type largeMap struct {",
          "change": "Declare the memo slot, pinned as `memo atomic.Pointer[hamtNode]` — one shape, so the next chunk's tests name the field the coder declared. Declaration only: nothing reads or writes it in this task."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "import block",
          "anchor": "\t\"math/bits\"",
          "change": "Add \"sync/atomic\". Package core does not import it yet; the CAS idiom in core/vm/chunk.go:149-152 is the shape to follow."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "(*HashMap).trieRoot",
          "anchor": "func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {",
          "new": true,
          "change": "Create trieRoot() (root *hamtNode, count int, bytes int64) beside trieFromBuildMap. In THIS task it only carries what the two call sites already do: trie form returns (large.root, large.count, 0); builder form calls trieFromBuildMap and returns its root, len(large.m) and the full conversion bytes. No memo read, no memo write — that is task 2.2. Leave trieFromBuildMap itself unchanged."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "HashMap.Assoc",
          "anchor": "func (h *HashMap) Assoc(key, val Value) (*HashMap, int64, error) {",
          "change": "Replace the conversion arm at core/types.go:1118-1123 with a trieRoot() call. Byte-for-byte behaviour and charges, so the existing suite stays green unchanged."
        },
        {
          "task": "2.1",
          "file": "core/types.go",
          "symbol": "HashMap.Dissoc",
          "anchor": "func (h *HashMap) Dissoc(key Value) (*HashMap, int64, error) {",
          "change": "Replace the byte-identical arm at core/types.go:1155-1160 with the same trieRoot() call. This is why the helper exists: one conversion path, two callers."
        }
      ],
      "contract": {
        "states": [
          "declared"
        ],
        "transitions": [
          {
            "input": "any update on a builder-form map",
            "state": "declared",
            "effect": "no-op",
            "evidence": "core/types.go:1118-1123 and :1155-1160 are byte-identical and move into trieRoot unchanged"
          }
        ],
        "forbidden": [
          "reading or writing the memo in this task",
          "any change to charges, Len, getByHashKey, iteration order, equality or printing"
        ],
        "seeding": {
          "declared": "add the field and the helper; run the existing suite"
        },
        "budgets": {
          "behaviour changes": 0,
          "conversion arms factored": 2
        }
      },
      "redTasks": [],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go build ./... && go vet ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "fanout-and-memo",
      "taskIds": [
        "1.1",
        "2.2"
      ],
      "prev": "memo-scaffold",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "S1-fanout-bound",
      "shard": "",
      "pkgDirs": [
        "core",
        "runtime"
      ],
      "pkgs": [
        "./core",
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/bench_test.go",
          "symbol": "BenchmarkHashMapFanOutAssoc",
          "anchor": "func BenchmarkHashMap_AssocChain(b *testing.B) {",
          "change": "new benchmark beside the existing map benchmarks; both arms at n = 9, 100, 1000; b.ReportAllocs(); receiver RETAINED across iterations (fan-out), not threaded — the threaded shape is already BenchmarkHashMap_AssocChain",
          "new": true
        },
        {
          "task": "1.1",
          "file": "core/hashmap_test.go",
          "symbol": "test binding scenario Repeated updates on a bulk-built map do not re-pay its conversion",
          "anchor": "func TestHashMap_Assoc_AllocsPerRun(t *testing.T) {",
          "change": "new test beside the existing allocation pins; asserts the int64 Assoc returns (the charge) and testing.AllocsPerRun over k updates against one Set-built receiver, plus a map-literal-built receiver via the reader; never asserts timing; must fail today at n=100 and n=1000 and pass for the trie arm",
          "new": true
        },
        {
          "task": "1.1",
          "file": "core/hashmap_test.go",
          "symbol": "cross-meter first-toucher-pays test",
          "anchor": "func TestHashMap_Immutability(t *testing.T) {",
          "change": "new test: one builder-form receiver, two independent contexts from allocCeilingContext (core/reader_budget_admission_test.go:70), each charging its Assoc result through the ledger; exactly one sees the conversion bytes. NOTE: core Assoc does not charge, so the test charges the returned bytes itself, or lives in stdlib driving the real assoc builtin (monotonic_test.go pattern) if a real meter is required",
          "new": true
        },
        {
          "task": "1.1",
          "file": "plugins/stdlib/monotonic_test.go",
          "symbol": "TestAssocMonotonic_ChargesPerCallHonestly",
          "anchor": "func TestAssocMonotonic_ChargesPerCallHonestly(t *testing.T) {",
          "change": "the only existing per-call charge assertion on the assoc builtin. It CHAINS (m = next each iteration) from an empty map, so it never reaches the builder-form fan-out shape and its bounds are unaffected. Nearest existing home for a stdlib-level cross-meter test"
        },
        {
          "task": "2.2",
          "file": "core/types.go",
          "symbol": "(*HashMap).trieRoot",
          "anchor": "func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {",
          "new": true,
          "change": "Give trieRoot its memo semantics, inside the helper task 2.1 created and nowhere else. The field is `largeMap.memo atomic.Pointer[hamtNode]`, declared by task 2.1 — that exact spelling, so a test naming it compiles. On a builder-form receiver: load `large.memo`; on a hit return (memo, len(h.large.m), 0) — zero bytes, because nothing was allocated. On a miss build through trieFromBuildMap, CompareAndSwap nil -> root, and return its own root with the full conversion bytes whether or not the CAS won, since a losing racer allocated too. large.root and large.count are not written; large.m stays authoritative for every read and for Len."
        }
      ],
      "contract": {
        "states": [
          "builder-no-memo",
          "builder-memo",
          "trie",
          "small",
          "unconverted-shared",
          "converted-shared",
          "key-error"
        ],
        "transitions": [
          {
            "input": "1st Assoc against a Set-built receiver, n>8",
            "state": "builder-no-memo",
            "effect": "forced",
            "evidence": "core/types.go:1120-1123 converts and returns the conversion bytes; requirement openspec/changes/hashmap-builder-trie-conversion/specs/core-engine/spec.md:56-59 allows exactly one such payment"
          },
          {
            "input": "2nd..k-th Assoc against that same receiver",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "spec.md:58-59 'k updates against one receiver do not each charge in proportion to its entry count'"
          },
          {
            "input": "2nd..k-th Dissoc against that same receiver",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1157-1160 is the identical conversion call site as Assoc's; spec.md:58 names Assoc or Dissoc"
          },
          {
            "input": "1st Assoc against a map-literal receiver read past hashMapSmallLimit, n>8",
            "state": "builder-no-memo",
            "effect": "forced",
            "evidence": "core/reader_budget_accounting_test.go:445-456 pins that a promoted literal is in large.m form"
          },
          {
            "input": "any Assoc/Dissoc against a trie-form receiver",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1118-1119 takes large.root and charges 0 for conversion"
          },
          {
            "input": "any Assoc/Dissoc against a receiver at or below hashMapSmallLimit",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1130-1145, :1167-1173"
          },
          {
            "input": "meter A charges the bytes of the 1st update on a shared builder-form value",
            "state": "unconverted-shared",
            "effect": "forced",
            "evidence": "design.md:100-107 'whichever evaluation updates it first bears the conversion'"
          },
          {
            "input": "meter B charges the bytes of a later update on that same value",
            "state": "converted-shared",
            "effect": "no-op",
            "evidence": "spec.md:22-24 'converting one to the other is a per-value cost, not a per-update one'"
          },
          {
            "input": "meter A's update is refused by the allocation ceiling after Assoc returned",
            "state": "converted-shared",
            "effect": "forced",
            "evidence": "core/metering.go:479-498 adds the charge to the counter and then returns ResourceLimitError, so the refusing evaluation is billed; core/types.go:1111-1128 takes no context and holds no ledger, so the memo is already published when the refusal lands and the next update on that value charges only its own path (tasks.md 2.3)"
          },
          {
            "input": "1st evaluation, bytecode mode, of a source whose folded map literal is the shared value",
            "state": "unconverted-shared",
            "effect": "forced",
            "evidence": "core/compiler/compiler.go:1285-1301 and :1347-1365 fold a constant map literal into one builder-form *HashMap in the chunk constant pool"
          },
          {
            "input": "2nd evaluation of that same cached source, bytecode mode",
            "state": "converted-shared",
            "effect": "no-op",
            "evidence": "core/vm/vm.go:966-969 pushes that same pointer on every execution, so the second evaluation meets a value whose conversion the first already settled"
          },
          {
            "input": "the same source is evaluated twice in tree-walker mode",
            "state": "unconverted-shared",
            "effect": "forced",
            "evidence": "no constant folding without a compile step: the literal is rebuilt per evaluation, so both evaluations pay. The cross-evaluation test is bytecode-mode only and must say so"
          },
          {
            "input": "Assoc, large.root == nil, large.memo == nil",
            "state": "builder-no-memo",
            "effect": "set",
            "evidence": "core/types.go:1120-1123 is the conversion call site; the memo replaces the discard"
          },
          {
            "input": "Assoc, large.root == nil, large.memo != nil",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "spec.md:22-24 per-value cost"
          },
          {
            "input": "Dissoc, large.root == nil, large.memo == nil",
            "state": "builder-no-memo",
            "effect": "set",
            "evidence": "core/types.go:1157-1160 is byte-identical to the Assoc arm at :1120-1123, so one helper serves both"
          },
          {
            "input": "Dissoc, large.root == nil, large.memo != nil",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "same call site"
          },
          {
            "input": "Assoc/Dissoc, large.root != nil",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1118-1119, :1155-1156 — the memo is never consulted for a trie-form map"
          },
          {
            "input": "Assoc/Dissoc, h.large == nil",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1130-1145, :1167-1173"
          },
          {
            "input": "Assoc/Dissoc with a key outside the key domain",
            "state": "key-error",
            "effect": "no-op",
            "evidence": "core/types.go:1112-1115 and :1150-1153 return before h.large is examined, so no conversion is attempted and no memo can be published"
          },
          {
            "input": "two goroutines convert the same builder-form receiver, one CAS wins",
            "state": "builder-no-memo",
            "effect": "set",
            "evidence": "design.md:82-88 — the winner's root is published, the loser discards its own and proceeds with it; both charge"
          },
          {
            "input": "the losing goroutine's own update",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "design.md:85-88 — its result map is built from its own root and is structurally identical, so which one won is unobservable"
          },
          {
            "input": "small map crosses hashMapSmallLimit through Set",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1234-1241 installs a fresh &largeMap{m: m} whose memo is the zero value"
          },
          {
            "input": "Assoc/Dissoc derives a new map from a receiver that had no memo",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1102-1104 newTrie allocates a fresh largeMap whose memo is the zero value"
          },
          {
            "input": "Assoc/Dissoc derives a new map from a memoised receiver",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1128, :1165 return newTrie(...), so the result is trie form and inherits no memo"
          },
          {
            "input": "Assoc/Dissoc derives a new map from a trie-form receiver",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1102-1104 — the derived largeMap carries root and count only"
          }
        ],
        "forbidden": [
          "asserting on ns/op anywhere in this seam",
          "measuring the chained shape (m = m.Assoc(...)) and calling it fan-out — the receiver must be the same retained value on every call",
          "seeding the receiver by repeated Assoc (that yields trie form and the assertion becomes vacuous)",
          "asserting equal charge for two evaluations that share one map value",
          "asserting cross-evaluation attribution in tree-walker mode",
          "relying on chunk-cache residency to make the two evaluations share a value — bind the map instead (see seeding)",
          "large.memo != nil while large.root != nil — unreachable by construction (a builder-form largeMap never gains a root: core/types.go:1218 requires root != nil already, and newTrie at :1102-1104 makes a fresh largeMap) and must be asserted as an invariant",
          "large.memo != nil while large.m == nil",
          "the memo's key set differing from large.m's key set",
          "getByHashKey (core/types.go:1018-1030), Len (:1188-1196), eachRaw (:1034-1048), sortedEntries (:1053-1063), Pairs, Each, String or Equals reading the memo",
          "Assoc or Dissoc writing large.root or large.count on the receiver",
          "mapForm(h) reporting anything but \"builder\" for a memoised builder map (core/reader_map_parity_test.go:189-200)",
          "a non-atomic field, a mutex, or sync.Once for the memo",
          "copying largeMap by value anywhere (atomic.Pointer carries noCopy; go vet copylocks must stay clean — every current site takes &largeMap{...})"
        ],
        "seeding": {
          "builder-no-memo": "n Sets from NewHashMap, or a read map literal past the threshold",
          "builder-memo": "exactly one Assoc or Dissoc against a builder-no-memo receiver — the ONLY legal path; a test must never call h.large.memo.Store",
          "trie": "repeated Assoc from NewHashMap",
          "unconverted-shared": "core-level: one Set-built n=1000 map, two contexts from core.WithEvalResourceLimits(context.Background(), int(core.DefaultMaxReductions), int(core.DefaultMaxAllocationBytes)); charge each Assoc's returned bytes into its own ctx with core.ChargeEvalAllocBytes. runtime-level: eng.Bind(\"m\", <Set-built n=1000 map>) once, then eng.Eval under ctx1 and ctx2 of `(assoc m :x 1)`, reading core.EvalMeterFrom(ctx).Snapshot().AllocationBytes — precedent runtime/resource_limits_test.go:605-611",
          "converted-shared": "the same value after exactly one prior Assoc; never by touching the field",
          "key-error": "m.Assoc(NewList([]Value{Int{V:1}}), Int{V:1}) — precedent core/types_test.go:330",
          "concurrent": "one builder-no-memo receiver, 8 goroutines x 32 Assoc calls each with disjoint keys, joined before any assertion"
        },
        "budgets": {
          "perUpdateChargeAfterTheFirst": "<= 4096 bytes at every n in {9,100,1000} (measured trie arm max 1600)",
          "perUpdateAllocsAfterTheFirst": "testing.AllocsPerRun(100, ...) <= 16 at every n (measured trie arm max 10)",
          "shape": "max/min of the 2nd-update charge across n in {9,100,1000} <= 4 (measured trie arm ratio 1600/624 = 2.56)",
          "firstUpdateCharge": "within +/-10% of the first-update charge task 0.1 records at this plan's baseSha for that n: this change must not move the first payment. No literal is pinned here — trie-conversion-charge-determinism, archived 2026-09-10, moved the conversion charge after the earlier reading of 3792 / 75440 / 1085048 was taken, so 0.1's re-recording at this base is the only valid comparand.",
          "fanOutTotal": "64 successive Assoc calls against one n=1000 builder receiver sum to <= 2 MiB of returned bytes; at base the same loop sums to ~69.4 MB and exceeds DefaultMaxAllocationBytes (64 MiB) at update 62 (design.md:24-28)",
          "k": "k = 64 for the ceiling case, k = 8 for the per-update cases",
          "separation": "at n=1000 the first ledger's delta >= 500000 bytes and the second ledger's delta <= 8192 bytes — a 60x gap, far outside any measurement noise since both are exact ledger arithmetic, not sampling",
          "chargedOnce": "the sum of returned bytes over k=8 updates against one n=1000 builder receiver <= conversionCharge + 8*4096, where conversionCharge is the first update's own return value",
          "amendmentSweep": "the set of existing exact-charge expectations to amend is expected to be EMPTY: the searched corpus (core, runtime, plugins/stdlib, plugins/json) has no test that updates twice against one retained builder-form receiver above hashMapSmallLimit. The chained shape (plugins/stdlib/monotonic_test.go:10-68, runtime/stdlib_result_ownership_test.go:140) is unaffected because the 2nd link is already trie form (core/types.go:1102-1104). runtime/stdlib_family_goldens_test.go:534-536 builds a 2-key map, below the threshold. The coder must still prove the set empty rather than assume it",
          "publicationStores": "exactly one successful CAS per map value; the memo is written at most once and never overwritten",
          "memoNodeCount": "the memo trie holds exactly len(large.m) entries",
          "readPathCost": "TestHashMap_Each_AllocsPerRun (core/hashmap_test.go:262-277) and TestHashMap_Assoc_AllocsPerRun (:279-293) must not move",
          "concurrency": "8 goroutines x 32 updates; -race clean; no wall-time assertion"
        }
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestHashMap_FanOutAssocStaysBounded",
        "TestHashMap_FanOutDissocStaysBounded",
        "TestHashMap_FanOutFromMapLiteralStaysBounded",
        "TestHashMap_FanOutUnderDefaultAllocationCeiling",
        "TestHashMap_MemoHoldsConvertedTrie",
        "TestHashMap_MemoLeavesReadPathOnBuilderForm",
        "TestHashMap_ConversionChargedOncePerValue",
        "TestMetering_MapConversionChargedToFirstToucher"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime -run 'TestHashMap_FanOut|TestHashMap_Memo|TestHashMap_ConversionChargedOncePerValue|TestMetering_MapConversionChargedToFirstToucher'",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go build ./... && go vet ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "set-clears-memo",
      "taskIds": [
        "2.3"
      ],
      "prev": "fanout-and-memo",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "S4-set-clears-memo",
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
          "symbol": "HashMap.Set",
          "anchor": "\t\th.large.m[hk] = e",
          "change": "clear the memo at the head of the h.large != nil branch (:1217) so a bulk write after a memoised update cannot leave a stale trie; the trie sub-branch (:1218-1224) mutates large.root in place and is unaffected, but sits inside the same branch, so one clear at the branch head covers both"
        }
      ],
      "contract": {
        "states": [
          "builder-memo",
          "builder-no-memo",
          "trie",
          "small"
        ],
        "transitions": [
          {
            "input": "Set on a builder-form map with a populated memo",
            "state": "builder-memo",
            "effect": "clear",
            "evidence": "design.md:121-126 'Implementation clears the memo on Set, which is one store on a path that is already mutating'"
          },
          {
            "input": "Set on a builder-form map with no memo",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1226 — the store of nil onto an already-nil slot changes nothing observable"
          },
          {
            "input": "Set on a trie-form map",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1218-1224 — a trie-form largeMap can never carry a memo, so nothing is there to clear"
          },
          {
            "input": "Set that promotes a small map past hashMapSmallLimit",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1234-1241 installs a fresh largeMap with a zero memo"
          },
          {
            "input": "Set with a key outside the key domain against a memoised map",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1213-1215 returns before h.large is touched, so a rejected Set must leave a populated memo standing"
          },
          {
            "input": "Set with a key outside the key domain against a map with no memo",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1213-1215 returns before h.large is touched"
          },
          {
            "input": "Set concurrent with an Assoc on the same receiver",
            "state": "builder-memo",
            "effect": "forced",
            "evidence": "outside the contract already: core/types.go:1198-1201 documents Set as pre-sharing only, and a concurrent Set/Assoc pair races on the Go map at :1226 and :1094 regardless of the memo. Not tested, and stated as a carve-out"
          }
        ],
        "forbidden": [
          "leaving a memo whose key set predates a Set",
          "clearing the memo anywhere other than Set",
          "a concurrent Set-plus-Assoc test — it would assert a race the type never promised"
        ],
        "seeding": {
          "builder-memo": "n Sets, then exactly one Assoc",
          "sequence-under-test": "n Sets -> one Assoc (memo set) -> one Set of a new key -> one Assoc; the last Assoc's result must equal what the same sequence produces on a map that never had a memo"
        },
        "budgets": {
          "clearCost": "one atomic store; no allocation added to Set (assert with testing.AllocsPerRun(100, ...) on a large-form Set: unchanged from base)",
          "equality": "the Set-then-Assoc result must Equals a conversion-free control map built by n+1 Sets plus one Assoc, and must agree on Len, Pairs order and String"
        }
      },
      "redTasks": [
        "2.3"
      ],
      "codeTasks": [
        "2.3"
      ],
      "redTests": [
        "TestHashMap_SetClearsMemo",
        "TestHashMap_SetThenAssocMatchesConversionFreePath"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core -run 'TestHashMap_Set(ClearsMemo|ThenAssocMatchesConversionFreePath)'",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core && go build ./... && go vet ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "refusal-and-race",
      "taskIds": [
        "2.4"
      ],
      "prev": "set-clears-memo",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "S5-refusal-and-race",
      "shard": "",
      "pkgDirs": [
        "core",
        "runtime"
      ],
      "pkgs": [
        "./core",
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.4",
          "file": "core/types.go",
          "symbol": "conversion charge on the Assoc/Dissoc path",
          "anchor": "\t\t\troot, bytes = h.trieFromBuildMap()",
          "change": "this exact line appears TWICE (:1121 Assoc, :1158 Dissoc) — anchor on the enclosing func signatures instead. After the change the conversion bytes are returned only by the call that actually built the trie; a memo hit returns only the path bytes b"
        },
        {
          "task": "2.4",
          "file": "plugins/stdlib/collections.go",
          "symbol": "chargeConsResult",
          "anchor": "\treturn core.ChargeGoFuncResultBytes(ctx, bytes)",
          "change": "the refusal point. The charge is admitted AFTER Assoc allocated and after the memo would be published, so a refused conversion publishes no memo has no in-core enforcement point today — see risks"
        },
        {
          "task": "2.4",
          "file": "core/reader_budget_accounting_test.go",
          "symbol": "TestGuardedRead_PromotionRefusedBeforeStorage",
          "anchor": "func TestGuardedRead_PromotionRefusedBeforeStorage(t *testing.T) {",
          "change": "the existing wide-map admit-before-allocate pin; it covers reader promotion into builder form only and builds no trie (ADR 0011 line 56: no reader path builds a trie node). Must stay green unchanged"
        }
      ],
      "contract": {
        "states": [
          "builder-no-memo",
          "builder-memo",
          "key-error",
          "ceiling-refused"
        ],
        "transitions": [
          {
            "input": "Assoc/Dissoc with a key outside the key domain against a builder-form receiver",
            "state": "key-error",
            "effect": "no-op",
            "evidence": "core/types.go:1112-1115, :1150-1153 — the error precedes every use of h.large, so no partial trie exists and no memo is published"
          },
          {
            "input": "the conversion completes but the caller's charge is refused by the ledger",
            "state": "ceiling-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/collections.go:553 calls Assoc, :566 accumulates the bytes and :576 charges them afterwards; core/metering.go:479-498 records the charge before returning ResourceLimitError, so the memo is already published and the refusing evaluation is billed for it, and the next update on that value charges only its own path (tasks.md 2.3)"
          },
          {
            "input": "a mid-conversion observation of the receiver by another goroutine",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1091-1099 builds into a local root; publication is a single atomic store, so a partially built trie is unreachable by construction"
          }
        ],
        "forbidden": [
          "asserting absence of a memo after an allocation-ceiling refusal: the ceiling is settled after Assoc returns (plugins/stdlib/collections.go:576), so the published memo and the refusing evaluation's bill are the contract, and absence is the key-domain refusal's row only",
          "asserting the same conversion is charged to two ledgers when neither refused",
          "moving or adding a wide-map charge site: core/vm/vm.go:1291-1294 must still admit HashMapShallowBytes(pairCount) before OpMakeMap pushes",
          "moving the guarded reader's admission terms: docs/adr/0011-reduction-and-allocation-metering.md:83-112 stays as written and :56 keeps 'no reader path builds a trie node'",
          "any wall-time assertion in the concurrency test"
        ],
        "seeding": {
          "key-error": "builder-no-memo receiver, then m.Assoc(NewList([]Value{Int{V:1}}), Int{V:1}) and m.Dissoc(NewList(...)) — precedent core/types_test.go:330",
          "ceiling-refused": "runtime level: eng.Bind(\"m\", <Set-built n=1000 map>), ctx from core.WithEvalResourceLimits with maxAllocBytes just under the n=1000 conversion charge, evaluate `(assoc m :x 1)`, expect ResourceLimitError; then evaluate the same form under a fresh ample ctx and assert its charge is bounded (the conversion was already paid for by the refused evaluation)",
          "concurrent": "one builder-no-memo n=1000 receiver, 8 goroutines x 32 disjoint-key Assoc calls, joined before assertions"
        },
        "budgets": {
          "afterRefusal": "the follow-up evaluation's charge <= 8192 bytes at n=1000",
          "concurrency": "8 goroutines x 32 updates; every result map has Len == 1001 and resolves its own key; the receiver still reports Len == 1000 and mapForm \"builder\"",
          "raceRuns": "one -race run of ./core is the gate; no repeat-count loop"
        }
      },
      "redTasks": [
        "2.4"
      ],
      "codeTasks": [
        "2.4"
      ],
      "redTests": [
        "TestHashMap_InvalidKeyPublishesNoMemo",
        "TestHashMap_ConcurrentUpdatesPublishOneMemo",
        "TestMetering_RefusedConversionStillSettlesTheCharge"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime -run 'TestHashMap_(InvalidKeyPublishesNoMemo|ConcurrentUpdatesPublishOneMemo)|TestMetering_RefusedConversionStillSettlesTheCharge'",
      "verify": "go test -race -timeout 2m -p 2 -parallel 2 ./core && go test -timeout 2m -p 2 -parallel 2 ./runtime && go build ./... && go vet ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "charge-docs",
      "taskIds": [
        "3.1"
      ],
      "prev": "refusal-and-race",
      "sharedPkg": "repo",
      "parallel": false,
      "seam": "S6-docs",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Evaluator persistent-map node unit row",
          "anchor": "| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |",
          "change": "the unit row that owns the conversion bytes; the paragraph at line 56 already states it is an evaluator charge applied through hamtSizeBytes on the Assoc/Dissoc path. Add that the builder-to-trie conversion is a per-value charge settled by the first update"
        },
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Determinism requirement",
          "anchor": "## Determinism requirement",
          "change": "state the cross-evaluation attribution: a shared receiver's conversion is borne by the first evaluation that updates it, so same source charges the same total holds per source read but not for a shared receiver"
        },
        {
          "task": "3.1",
          "file": "docs/adr/0003-concurrency-model.md",
          "symbol": "Consequences",
          "anchor": "## Consequences",
          "change": "state the derived-state carve-out to immutability: the memo is a pure function of large.m, published atomically, and no observable (value, equality, iteration order, printing, Len) differs before or after. ADR 0003 is 13 lines today and currently governs only per-evaluation counters and env locking — the carve-out widens its stated scope"
        },
        {
          "task": "3.1",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased] Changed",
          "anchor": "## [Unreleased]",
          "change": "metering entry under Changed (### Added at :10, ### Changed at :28) carrying the measured before/after from task 0.1"
        }
      ],
      "contract": {
        "states": [
          "adr-0011",
          "adr-0003",
          "changelog"
        ],
        "transitions": [
          {
            "input": "the trie conversion charge",
            "state": "adr-0011",
            "effect": "set",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md:54-56 owns the persistent-map node row and :71-81 the charge-site list; :67-69 states the determinism requirement the per-value rule qualifies"
          },
          {
            "input": "the memo as a mutation of a shared value",
            "state": "adr-0003",
            "effect": "set",
            "evidence": "docs/adr/0003-concurrency-model.md:7-12 owns the concurrency model; design.md:112-126 states the carve-out terms"
          },
          {
            "input": "the charge change visible to embedders",
            "state": "changelog",
            "effect": "set",
            "evidence": "design.md:145-150 requires the measured before and after under [Unreleased]"
          },
          {
            "input": "a new published unit",
            "state": "adr-0011",
            "effect": "no-op",
            "evidence": "this change publishes NO new unit: the conversion is already priced by the existing persistent-map node row (:54). Only when it is charged changes, not what it costs. Task 3.1's 'no unit published that no ADR table row owns' therefore has nothing to add"
          }
        ],
        "forbidden": [
          "adding a row to the ADR 0011 fixed size table for the memo — it is the same storage the persistent-map node row already prices",
          "stating that a shared receiver's total is history-independent (docs/adr/0011:67-69 must be qualified, not contradicted): the source-level determinism claim holds; the shared-receiver claim does not",
          "leaving ADR 0012 retained accounting unmentioned if the maintainer decides the memo must be counted — see risk R4"
        ],
        "seeding": {
          "adr-0011": "the per-value rule goes under Charge sites (:71-81), stated as: the trie conversion is charged to the first update on a value; a later update on the same value charges only its path; an evaluation whose update is refused after the conversion has still been billed for it",
          "adr-0003": "the carve-out goes under Consequences (:9-12), stated as: derived state that is a pure function of frozen storage may be published atomically on a shared value, because no observable differs before or after",
          "changelog": "[Unreleased], Changed, with the S0 table's builder-arm before and the measured after"
        },
        "budgets": {
          "newUnits": "0",
          "movedTableRows": "0 — the one-pass build that would have moved the first-update bytes is explicitly out of scope (design.md:66-68)"
        }
      },
      "redTasks": [],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "openspec validate hashmap-builder-trie-conversion --strict --json && grep -q \"not re-charged as retained\" docs/adr/0011-reduction-and-allocation-metering.md && grep -q \"derived state\" docs/adr/0003-concurrency-model.md && grep -q \"once per value\" CHANGELOG.md",
      "coder": "zpatcher"
    },
    {
      "id": "verify-core",
      "taskIds": [
        "3.2",
        "3.3"
      ],
      "prev": "charge-docs",
      "sharedPkg": "repo",
      "parallel": false,
      "seam": "S7-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.2",
          "file": "core/types.go",
          "symbol": "verification commands",
          "anchor": "type largeMap struct {",
          "change": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime; go test -race -timeout 2m -p 2 -parallel 2 ./core; golangci-lint run ./core/... — run with env -C at the worktree root, since absolute paths from a foreign cwd give exit 7 with No issues found"
        },
        {
          "task": "3.3",
          "file": "Makefile",
          "symbol": "build / lint / test targets",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "make build (:10 builds bin/lispico), make lint (:19 bare golangci-lint run), make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' (:14)"
        }
      ],
      "contract": {
        "states": [
          "core-runtime",
          "race",
          "lint",
          "project-floor",
          "goldset",
          "openspec"
        ],
        "transitions": [
          {
            "input": "3.2 unit run",
            "state": "core-runtime",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.2 race run",
            "state": "race",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.2 lint run",
            "state": "lint",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.3",
            "state": "project-floor",
            "effect": "forced",
            "evidence": "tasks.md:20; Makefile targets build/test/lint with GOTESTFLAGS ?= -timeout 2m"
          },
          {
            "input": "3.4",
            "state": "goldset",
            "effect": "no-op",
            "evidence": "tasks.md:21 — the gold set builds only small maps (memory: the reader-map-promotion-parity measurement was allocation-neutral there), so no ADR 0008 threshold is expected to move; report allocation evidence separately from timing"
          },
          {
            "input": "3.5",
            "state": "openspec",
            "effect": "forced",
            "evidence": "tasks.md:22"
          }
        ],
        "forbidden": [
          "declaring a latency delta a verdict on this machine (memory: perfgate-not-local — LATENCY cells false-FAIL locally; bytes and allocs are locally exact)",
          "closing a chunk on a narrowed -run regex while its package is red (memory: zapply-narrow-verify-false-pass)",
          "running golangci-lint with absolute paths from a foreign cwd (memory: golangci exit 7 reports 'No issues found'; use env -C)"
        ],
        "seeding": {
          "goldset": "both evaluator modes, GOLDSET_MODE=eval and GOLDSET_MODE=vm, against the pre-change baseline"
        },
        "budgets": {
          "goldsetThresholds": "0 ADR 0008 thresholds may move",
          "fanOutReport": "the S0 six-cell table plus the same six cells after, in the same units"
        }
      },
      "redTasks": [],
      "codeTasks": [
        "3.2",
        "3.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -race -timeout 2m -p 2 -parallel 2 ./core && make build && make lint",
      "coder": "coder"
    },
    {
      "id": "goldset-evidence",
      "taskIds": [
        "3.4",
        "3.5"
      ],
      "prev": "verify-core",
      "sharedPkg": "repo",
      "parallel": false,
      "seam": "S7-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.4",
          "file": "internal/goldset/bench_test.go",
          "symbol": "BenchmarkGoldsetParse",
          "anchor": "func BenchmarkGoldsetParse(b *testing.B) {",
          "change": "mode comes from GOLDSET_MODE (eval|vm) via the switch at :16-22; the Makefile profile target (:27-28) shows the exact invocation shape. Allocations/bytes are the verdict axis, timing observation only"
        },
        {
          "task": "3.4",
          "file": "internal/goldset/alloc_test.go",
          "symbol": "TestGoldsetVMAllocations",
          "anchor": "var vmAllocCeilings = map[string]int{",
          "change": "13 pinned per-fixture counts; every fixture builds only small maps (below hashMapSmallLimit), so no count is expected to move. File is //go:build !race (:1), so it does not run in the -race pass"
        },
        {
          "task": "3.5",
          "file": "openspec/changes/hashmap-builder-trie-conversion/specs/core-engine/spec.md",
          "symbol": "openspec validate --strict",
          "anchor": "## MODIFIED Requirements",
          "change": "openspec validate hashmap-builder-trie-conversion --strict --json; the delta modifies Map representation efficiency, live at openspec/specs/core-engine/spec.md:152, by adding one paragraph and one scenario"
        }
      ],
      "contract": {
        "states": [
          "core-runtime",
          "race",
          "lint",
          "project-floor",
          "goldset",
          "openspec"
        ],
        "transitions": [
          {
            "input": "3.2 unit run",
            "state": "core-runtime",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.2 race run",
            "state": "race",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.2 lint run",
            "state": "lint",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.3",
            "state": "project-floor",
            "effect": "forced",
            "evidence": "tasks.md:20; Makefile targets build/test/lint with GOTESTFLAGS ?= -timeout 2m"
          },
          {
            "input": "3.4",
            "state": "goldset",
            "effect": "no-op",
            "evidence": "tasks.md:21 — the gold set builds only small maps (memory: the reader-map-promotion-parity measurement was allocation-neutral there), so no ADR 0008 threshold is expected to move; report allocation evidence separately from timing"
          },
          {
            "input": "3.5",
            "state": "openspec",
            "effect": "forced",
            "evidence": "tasks.md:22"
          }
        ],
        "forbidden": [
          "declaring a latency delta a verdict on this machine (memory: perfgate-not-local — LATENCY cells false-FAIL locally; bytes and allocs are locally exact)",
          "closing a chunk on a narrowed -run regex while its package is red (memory: zapply-narrow-verify-false-pass)",
          "running golangci-lint with absolute paths from a foreign cwd (memory: golangci exit 7 reports 'No issues found'; use env -C)"
        ],
        "seeding": {
          "goldset": "both evaluator modes, GOLDSET_MODE=eval and GOLDSET_MODE=vm, against the pre-change baseline"
        },
        "budgets": {
          "goldsetThresholds": "0 ADR 0008 thresholds may move",
          "fanOutReport": "the S0 six-cell table plus the same six cells after, in the same units"
        }
      },
      "redTasks": [],
      "codeTasks": [
        "3.4",
        "3.5"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./internal/goldset && openspec validate hashmap-builder-trie-conversion --strict --json",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "S0-baseline",
      "tasks": [
        "0.1"
      ],
      "summary": "NO-RED-WAIVER: measurement only, no production symbol and no observable contract. Re-record the pre-change fan-out figures at base d40567e so the CHANGELOG delta in 3.1 is measured rather than quoted.",
      "contract": {
        "states": [
          "builder-arm",
          "trie-arm"
        ],
        "transitions": [
          {
            "input": "one Assoc against a retained receiver, receiver built by Set, n in {9,100,1000}",
            "state": "builder-arm",
            "effect": "forced",
            "evidence": "design.md:10-14 records charge 3792/75440/1085048, allocs 37/554/6480, B/op 8183/126018/1794146 — a pre-trie-conversion-charge-determinism reading, superseded at this baseSha: task 0.1 re-records all three and those figures are what every later budget compares against"
          },
          {
            "input": "one Assoc against a retained receiver, receiver built by repeated Assoc, n in {9,100,1000}",
            "state": "trie-arm",
            "effect": "no-op",
            "evidence": "design.md:10-14 records charge 696/624/1600, allocs 7/8/10, B/op 1481/891/2325"
          }
        ],
        "forbidden": [
          "recording a latency number as a verdict (memory: perfgate-not-local; design.md:16-19 records timing as observation only)",
          "recording figures from any commit other than the change base"
        ],
        "seeding": {
          "builder-arm": "m := NewHashMap(); for i := range n { m.Set(Int{V:int64(i)}, Int{V:int64(i)}) } — precedent core/bench_test.go:531-543",
          "trie-arm": "m := NewHashMap(); for i := range n { m, _, _ = m.Assoc(Int{V:int64(i)}, Int{V:int64(i)}) } — precedent core/bench_test.go:549-558"
        },
        "budgets": {
          "benchtime": "-benchtime=500x, matching design.md:16",
          "sizes": "exactly n in {9,100,1000}",
          "verdictAxes": "allocs/op and B/op only; ns/op recorded as observation"
        }
      }
    },
    {
      "id": "S4-set-clears-memo",
      "tasks": [
        "2.3"
      ],
      "summary": "Set is the bulk-construction escape hatch and invalidates a memo; it clears the slot on the large path. The guarantee is sequential-only, because Set on a shared receiver already races on large.m itself.",
      "contract": {
        "states": [
          "builder-memo",
          "builder-no-memo",
          "trie",
          "small"
        ],
        "transitions": [
          {
            "input": "Set on a builder-form map with a populated memo",
            "state": "builder-memo",
            "effect": "clear",
            "evidence": "design.md:121-126 'Implementation clears the memo on Set, which is one store on a path that is already mutating'"
          },
          {
            "input": "Set on a builder-form map with no memo",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1226 — the store of nil onto an already-nil slot changes nothing observable"
          },
          {
            "input": "Set on a trie-form map",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1218-1224 — a trie-form largeMap can never carry a memo, so nothing is there to clear"
          },
          {
            "input": "Set that promotes a small map past hashMapSmallLimit",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1234-1241 installs a fresh largeMap with a zero memo"
          },
          {
            "input": "Set with a key outside the key domain against a memoised map",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1213-1215 returns before h.large is touched, so a rejected Set must leave a populated memo standing"
          },
          {
            "input": "Set with a key outside the key domain against a map with no memo",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1213-1215 returns before h.large is touched"
          },
          {
            "input": "Set concurrent with an Assoc on the same receiver",
            "state": "builder-memo",
            "effect": "forced",
            "evidence": "outside the contract already: core/types.go:1198-1201 documents Set as pre-sharing only, and a concurrent Set/Assoc pair races on the Go map at :1226 and :1094 regardless of the memo. Not tested, and stated as a carve-out"
          }
        ],
        "forbidden": [
          "leaving a memo whose key set predates a Set",
          "clearing the memo anywhere other than Set",
          "a concurrent Set-plus-Assoc test — it would assert a race the type never promised"
        ],
        "seeding": {
          "builder-memo": "n Sets, then exactly one Assoc",
          "sequence-under-test": "n Sets -> one Assoc (memo set) -> one Set of a new key -> one Assoc; the last Assoc's result must equal what the same sequence produces on a map that never had a memo"
        },
        "budgets": {
          "clearCost": "one atomic store; no allocation added to Set (assert with testing.AllocsPerRun(100, ...) on a large-form Set: unchanged from base)",
          "equality": "the Set-then-Assoc result must Equals a conversion-free control map built by n+1 Sets plus one Assoc, and must agree on Len, Pairs order and String"
        }
      }
    },
    {
      "id": "S5-refusal-and-race",
      "tasks": [
        "2.4"
      ],
      "summary": "The refusal and concurrency edges of the charge-once rule: a key-domain refusal publishes nothing, an allocation-ceiling refusal publishes and is billed to the refusing evaluation, the wide-map admission sites are untouched, and the concurrent update path is -race clean.",
      "contract": {
        "states": [
          "builder-no-memo",
          "builder-memo",
          "key-error",
          "ceiling-refused"
        ],
        "transitions": [
          {
            "input": "Assoc/Dissoc with a key outside the key domain against a builder-form receiver",
            "state": "key-error",
            "effect": "no-op",
            "evidence": "core/types.go:1112-1115, :1150-1153 — the error precedes every use of h.large, so no partial trie exists and no memo is published"
          },
          {
            "input": "the conversion completes but the caller's charge is refused by the ledger",
            "state": "ceiling-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/collections.go:553 calls Assoc, :566 accumulates the bytes and :576 charges them afterwards; core/metering.go:479-498 records the charge before returning ResourceLimitError, so the memo is already published and the refusing evaluation is billed for it, and the next update on that value charges only its own path (tasks.md 2.3)"
          },
          {
            "input": "a mid-conversion observation of the receiver by another goroutine",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1091-1099 builds into a local root; publication is a single atomic store, so a partially built trie is unreachable by construction"
          }
        ],
        "forbidden": [
          "asserting absence of a memo after an allocation-ceiling refusal: the ceiling is settled after Assoc returns (plugins/stdlib/collections.go:576), so the published memo and the refusing evaluation's bill are the contract, and absence is the key-domain refusal's row only",
          "asserting the same conversion is charged to two ledgers when neither refused",
          "moving or adding a wide-map charge site: core/vm/vm.go:1291-1294 must still admit HashMapShallowBytes(pairCount) before OpMakeMap pushes",
          "moving the guarded reader's admission terms: docs/adr/0011-reduction-and-allocation-metering.md:83-112 stays as written and :56 keeps 'no reader path builds a trie node'",
          "any wall-time assertion in the concurrency test"
        ],
        "seeding": {
          "key-error": "builder-no-memo receiver, then m.Assoc(NewList([]Value{Int{V:1}}), Int{V:1}) and m.Dissoc(NewList(...)) — precedent core/types_test.go:330",
          "ceiling-refused": "runtime level: eng.Bind(\"m\", <Set-built n=1000 map>), ctx from core.WithEvalResourceLimits with maxAllocBytes just under the n=1000 conversion charge, evaluate `(assoc m :x 1)`, expect ResourceLimitError; then evaluate the same form under a fresh ample ctx and assert its charge is bounded (the conversion was already paid for by the refused evaluation)",
          "concurrent": "one builder-no-memo n=1000 receiver, 8 goroutines x 32 disjoint-key Assoc calls, joined before assertions"
        },
        "budgets": {
          "afterRefusal": "the follow-up evaluation's charge <= 8192 bytes at n=1000",
          "concurrency": "8 goroutines x 32 updates; every result map has Len == 1001 and resolves its own key; the receiver still reports Len == 1000 and mapForm \"builder\"",
          "raceRuns": "one -race run of ./core is the gate; no repeat-count loop"
        }
      }
    },
    {
      "id": "S6-docs",
      "tasks": [
        "3.1"
      ],
      "summary": "NO-RED-WAIVER: prose deliverables, no runtime behaviour and no assertable transition. ADR 0011 gains the per-value charge rule and its cross-evaluation attribution; ADR 0003 gains the derived-state carve-out; CHANGELOG [Unreleased] records the measured before/after from 0.1.",
      "contract": {
        "states": [
          "adr-0011",
          "adr-0003",
          "changelog"
        ],
        "transitions": [
          {
            "input": "the trie conversion charge",
            "state": "adr-0011",
            "effect": "set",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md:54-56 owns the persistent-map node row and :71-81 the charge-site list; :67-69 states the determinism requirement the per-value rule qualifies"
          },
          {
            "input": "the memo as a mutation of a shared value",
            "state": "adr-0003",
            "effect": "set",
            "evidence": "docs/adr/0003-concurrency-model.md:7-12 owns the concurrency model; design.md:112-126 states the carve-out terms"
          },
          {
            "input": "the charge change visible to embedders",
            "state": "changelog",
            "effect": "set",
            "evidence": "design.md:145-150 requires the measured before and after under [Unreleased]"
          },
          {
            "input": "a new published unit",
            "state": "adr-0011",
            "effect": "no-op",
            "evidence": "this change publishes NO new unit: the conversion is already priced by the existing persistent-map node row (:54). Only when it is charged changes, not what it costs. Task 3.1's 'no unit published that no ADR table row owns' therefore has nothing to add"
          }
        ],
        "forbidden": [
          "adding a row to the ADR 0011 fixed size table for the memo — it is the same storage the persistent-map node row already prices",
          "stating that a shared receiver's total is history-independent (docs/adr/0011:67-69 must be qualified, not contradicted): the source-level determinism claim holds; the shared-receiver claim does not",
          "leaving ADR 0012 retained accounting unmentioned if the maintainer decides the memo must be counted — see risk R4"
        ],
        "seeding": {
          "adr-0011": "the per-value rule goes under Charge sites (:71-81), stated as: the trie conversion is charged to the first update on a value; a later update on the same value charges only its path; an evaluation whose update is refused after the conversion has still been billed for it",
          "adr-0003": "the carve-out goes under Consequences (:9-12), stated as: derived state that is a pure function of frozen storage may be published atomically on a shared value, because no observable differs before or after",
          "changelog": "[Unreleased], Changed, with the S0 table's builder-arm before and the measured after"
        },
        "budgets": {
          "newUnits": "0",
          "movedTableRows": "0 — the one-pass build that would have moved the first-update bytes is explicitly out of scope (design.md:66-68)"
        }
      }
    },
    {
      "id": "S7-verification",
      "tasks": [
        "3.2",
        "3.3",
        "3.4",
        "3.5"
      ],
      "summary": "NO-RED-WAIVER and NO-TESTER-WAIVER: verification and evidence recording only, it asserts no new behaviour of its own.",
      "contract": {
        "states": [
          "core-runtime",
          "race",
          "lint",
          "project-floor",
          "goldset",
          "openspec"
        ],
        "transitions": [
          {
            "input": "3.2 unit run",
            "state": "core-runtime",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.2 race run",
            "state": "race",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.2 lint run",
            "state": "lint",
            "effect": "forced",
            "evidence": "tasks.md:19"
          },
          {
            "input": "3.3",
            "state": "project-floor",
            "effect": "forced",
            "evidence": "tasks.md:20; Makefile targets build/test/lint with GOTESTFLAGS ?= -timeout 2m"
          },
          {
            "input": "3.4",
            "state": "goldset",
            "effect": "no-op",
            "evidence": "tasks.md:21 — the gold set builds only small maps (memory: the reader-map-promotion-parity measurement was allocation-neutral there), so no ADR 0008 threshold is expected to move; report allocation evidence separately from timing"
          },
          {
            "input": "3.5",
            "state": "openspec",
            "effect": "forced",
            "evidence": "tasks.md:22"
          }
        ],
        "forbidden": [
          "declaring a latency delta a verdict on this machine (memory: perfgate-not-local — LATENCY cells false-FAIL locally; bytes and allocs are locally exact)",
          "closing a chunk on a narrowed -run regex while its package is red (memory: zapply-narrow-verify-false-pass)",
          "running golangci-lint with absolute paths from a foreign cwd (memory: golangci exit 7 reports 'No issues found'; use env -C)"
        ],
        "seeding": {
          "goldset": "both evaluator modes, GOLDSET_MODE=eval and GOLDSET_MODE=vm, against the pre-change baseline"
        },
        "budgets": {
          "goldsetThresholds": "0 ADR 0008 thresholds may move",
          "fanOutReport": "the S0 six-cell table plus the same six cells after, in the same units"
        }
      }
    },
    {
      "id": "S1-fanout-bound",
      "tasks": [
        "1.1",
        "2.2"
      ],
      "summary": "The observable bound the new spec scenario states: k updates against one bulk-built retained receiver charge and allocate per update on the same order as the trie arm. This is the change's acceptance seam; it is red at base and stays red until S3 lands. First-toucher-pays: the conversion is charged once per map value, so when one value is updated under two ledgers exactly one of them sees the conversion. Also the amendment sweep for exact-charge expectations this moves. The memo slot on largeMap, published by compare-and-swap from a fully built local trie, read by both Assoc and Dissoc through one shared helper, invisible to every read path.",
      "contract": {
        "states": [
          "builder-no-memo",
          "builder-memo",
          "trie",
          "small",
          "unconverted-shared",
          "converted-shared",
          "key-error"
        ],
        "transitions": [
          {
            "input": "1st Assoc against a Set-built receiver, n>8",
            "state": "builder-no-memo",
            "effect": "forced",
            "evidence": "core/types.go:1120-1123 converts and returns the conversion bytes; requirement openspec/changes/hashmap-builder-trie-conversion/specs/core-engine/spec.md:56-59 allows exactly one such payment"
          },
          {
            "input": "2nd..k-th Assoc against that same receiver",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "spec.md:58-59 'k updates against one receiver do not each charge in proportion to its entry count'"
          },
          {
            "input": "2nd..k-th Dissoc against that same receiver",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1157-1160 is the identical conversion call site as Assoc's; spec.md:58 names Assoc or Dissoc"
          },
          {
            "input": "1st Assoc against a map-literal receiver read past hashMapSmallLimit, n>8",
            "state": "builder-no-memo",
            "effect": "forced",
            "evidence": "core/reader_budget_accounting_test.go:445-456 pins that a promoted literal is in large.m form"
          },
          {
            "input": "any Assoc/Dissoc against a trie-form receiver",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1118-1119 takes large.root and charges 0 for conversion"
          },
          {
            "input": "any Assoc/Dissoc against a receiver at or below hashMapSmallLimit",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1130-1145, :1167-1173"
          },
          {
            "input": "meter A charges the bytes of the 1st update on a shared builder-form value",
            "state": "unconverted-shared",
            "effect": "forced",
            "evidence": "design.md:100-107 'whichever evaluation updates it first bears the conversion'"
          },
          {
            "input": "meter B charges the bytes of a later update on that same value",
            "state": "converted-shared",
            "effect": "no-op",
            "evidence": "spec.md:22-24 'converting one to the other is a per-value cost, not a per-update one'"
          },
          {
            "input": "meter A's update is refused by the allocation ceiling after Assoc returned",
            "state": "converted-shared",
            "effect": "forced",
            "evidence": "core/metering.go:479-498 adds the charge to the counter and then returns ResourceLimitError, so the refusing evaluation is billed; core/types.go:1111-1128 takes no context and holds no ledger, so the memo is already published when the refusal lands and the next update on that value charges only its own path (tasks.md 2.3)"
          },
          {
            "input": "1st evaluation, bytecode mode, of a source whose folded map literal is the shared value",
            "state": "unconverted-shared",
            "effect": "forced",
            "evidence": "core/compiler/compiler.go:1285-1301 and :1347-1365 fold a constant map literal into one builder-form *HashMap in the chunk constant pool"
          },
          {
            "input": "2nd evaluation of that same cached source, bytecode mode",
            "state": "converted-shared",
            "effect": "no-op",
            "evidence": "core/vm/vm.go:966-969 pushes that same pointer on every execution, so the second evaluation meets a value whose conversion the first already settled"
          },
          {
            "input": "the same source is evaluated twice in tree-walker mode",
            "state": "unconverted-shared",
            "effect": "forced",
            "evidence": "no constant folding without a compile step: the literal is rebuilt per evaluation, so both evaluations pay. The cross-evaluation test is bytecode-mode only and must say so"
          },
          {
            "input": "Assoc, large.root == nil, large.memo == nil",
            "state": "builder-no-memo",
            "effect": "set",
            "evidence": "core/types.go:1120-1123 is the conversion call site; the memo replaces the discard"
          },
          {
            "input": "Assoc, large.root == nil, large.memo != nil",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "spec.md:22-24 per-value cost"
          },
          {
            "input": "Dissoc, large.root == nil, large.memo == nil",
            "state": "builder-no-memo",
            "effect": "set",
            "evidence": "core/types.go:1157-1160 is byte-identical to the Assoc arm at :1120-1123, so one helper serves both"
          },
          {
            "input": "Dissoc, large.root == nil, large.memo != nil",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "same call site"
          },
          {
            "input": "Assoc/Dissoc, large.root != nil",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1118-1119, :1155-1156 — the memo is never consulted for a trie-form map"
          },
          {
            "input": "Assoc/Dissoc, h.large == nil",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1130-1145, :1167-1173"
          },
          {
            "input": "Assoc/Dissoc with a key outside the key domain",
            "state": "key-error",
            "effect": "no-op",
            "evidence": "core/types.go:1112-1115 and :1150-1153 return before h.large is examined, so no conversion is attempted and no memo can be published"
          },
          {
            "input": "two goroutines convert the same builder-form receiver, one CAS wins",
            "state": "builder-no-memo",
            "effect": "set",
            "evidence": "design.md:82-88 — the winner's root is published, the loser discards its own and proceeds with it; both charge"
          },
          {
            "input": "the losing goroutine's own update",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "design.md:85-88 — its result map is built from its own root and is structurally identical, so which one won is unobservable"
          },
          {
            "input": "small map crosses hashMapSmallLimit through Set",
            "state": "small",
            "effect": "no-op",
            "evidence": "core/types.go:1234-1241 installs a fresh &largeMap{m: m} whose memo is the zero value"
          },
          {
            "input": "Assoc/Dissoc derives a new map from a receiver that had no memo",
            "state": "builder-no-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1102-1104 newTrie allocates a fresh largeMap whose memo is the zero value"
          },
          {
            "input": "Assoc/Dissoc derives a new map from a memoised receiver",
            "state": "builder-memo",
            "effect": "no-op",
            "evidence": "core/types.go:1128, :1165 return newTrie(...), so the result is trie form and inherits no memo"
          },
          {
            "input": "Assoc/Dissoc derives a new map from a trie-form receiver",
            "state": "trie",
            "effect": "no-op",
            "evidence": "core/types.go:1102-1104 — the derived largeMap carries root and count only"
          }
        ],
        "forbidden": [
          "asserting on ns/op anywhere in this seam",
          "measuring the chained shape (m = m.Assoc(...)) and calling it fan-out — the receiver must be the same retained value on every call",
          "seeding the receiver by repeated Assoc (that yields trie form and the assertion becomes vacuous)",
          "asserting equal charge for two evaluations that share one map value",
          "asserting cross-evaluation attribution in tree-walker mode",
          "relying on chunk-cache residency to make the two evaluations share a value — bind the map instead (see seeding)",
          "large.memo != nil while large.root != nil — unreachable by construction (a builder-form largeMap never gains a root: core/types.go:1218 requires root != nil already, and newTrie at :1102-1104 makes a fresh largeMap) and must be asserted as an invariant",
          "large.memo != nil while large.m == nil",
          "the memo's key set differing from large.m's key set",
          "getByHashKey (core/types.go:1018-1030), Len (:1188-1196), eachRaw (:1034-1048), sortedEntries (:1053-1063), Pairs, Each, String or Equals reading the memo",
          "Assoc or Dissoc writing large.root or large.count on the receiver",
          "mapForm(h) reporting anything but \"builder\" for a memoised builder map (core/reader_map_parity_test.go:189-200)",
          "a non-atomic field, a mutex, or sync.Once for the memo",
          "copying largeMap by value anywhere (atomic.Pointer carries noCopy; go vet copylocks must stay clean — every current site takes &largeMap{...})"
        ],
        "seeding": {
          "builder-no-memo": "n Sets from NewHashMap, or a read map literal past the threshold",
          "builder-memo": "exactly one Assoc or Dissoc against a builder-no-memo receiver — the ONLY legal path; a test must never call h.large.memo.Store",
          "trie": "repeated Assoc from NewHashMap",
          "unconverted-shared": "core-level: one Set-built n=1000 map, two contexts from core.WithEvalResourceLimits(context.Background(), int(core.DefaultMaxReductions), int(core.DefaultMaxAllocationBytes)); charge each Assoc's returned bytes into its own ctx with core.ChargeEvalAllocBytes. runtime-level: eng.Bind(\"m\", <Set-built n=1000 map>) once, then eng.Eval under ctx1 and ctx2 of `(assoc m :x 1)`, reading core.EvalMeterFrom(ctx).Snapshot().AllocationBytes — precedent runtime/resource_limits_test.go:605-611",
          "converted-shared": "the same value after exactly one prior Assoc; never by touching the field",
          "key-error": "m.Assoc(NewList([]Value{Int{V:1}}), Int{V:1}) — precedent core/types_test.go:330",
          "concurrent": "one builder-no-memo receiver, 8 goroutines x 32 Assoc calls each with disjoint keys, joined before any assertion"
        },
        "budgets": {
          "perUpdateChargeAfterTheFirst": "<= 4096 bytes at every n in {9,100,1000} (measured trie arm max 1600)",
          "perUpdateAllocsAfterTheFirst": "testing.AllocsPerRun(100, ...) <= 16 at every n (measured trie arm max 10)",
          "shape": "max/min of the 2nd-update charge across n in {9,100,1000} <= 4 (measured trie arm ratio 1600/624 = 2.56)",
          "firstUpdateCharge": "within +/-10% of the first-update charge task 0.1 records at this plan's baseSha for that n: this change must not move the first payment. No literal is pinned here — trie-conversion-charge-determinism, archived 2026-09-10, moved the conversion charge after the earlier reading of 3792 / 75440 / 1085048 was taken, so 0.1's re-recording at this base is the only valid comparand.",
          "fanOutTotal": "64 successive Assoc calls against one n=1000 builder receiver sum to <= 2 MiB of returned bytes; at base the same loop sums to ~69.4 MB and exceeds DefaultMaxAllocationBytes (64 MiB) at update 62 (design.md:24-28)",
          "k": "k = 64 for the ceiling case, k = 8 for the per-update cases",
          "separation": "at n=1000 the first ledger's delta >= 500000 bytes and the second ledger's delta <= 8192 bytes — a 60x gap, far outside any measurement noise since both are exact ledger arithmetic, not sampling",
          "chargedOnce": "the sum of returned bytes over k=8 updates against one n=1000 builder receiver <= conversionCharge + 8*4096, where conversionCharge is the first update's own return value",
          "amendmentSweep": "the set of existing exact-charge expectations to amend is expected to be EMPTY: the searched corpus (core, runtime, plugins/stdlib, plugins/json) has no test that updates twice against one retained builder-form receiver above hashMapSmallLimit. The chained shape (plugins/stdlib/monotonic_test.go:10-68, runtime/stdlib_result_ownership_test.go:140) is unaffected because the 2nd link is already trie form (core/types.go:1102-1104). runtime/stdlib_family_goldens_test.go:534-536 builds a 2-key map, below the threshold. The coder must still prove the set empty rather than assume it",
          "publicationStores": "exactly one successful CAS per map value; the memo is written at most once and never overwritten",
          "memoNodeCount": "the memo trie holds exactly len(large.m) entries",
          "readPathCost": "TestHashMap_Each_AllocsPerRun (core/hashmap_test.go:262-277) and TestHashMap_Assoc_AllocsPerRun (:279-293) must not move",
          "concurrency": "8 goroutines x 32 updates; -race clean; no wall-time assertion"
        }
      }
    },
    {
      "id": "S8-memo-scaffold",
      "tasks": [
        "2.1"
      ],
      "summary": "NO-RED-WAIVER: declaration and pure refactor with no observable contract of its own. It declares largeMap.memo so the tests of 1.1 compile, and factors the two byte-identical conversion arms of Assoc and Dissoc into one trieRoot helper that does exactly what they do today. Every behaviour it touches is already pinned by the existing suite, which must stay green unchanged; the memo semantics it enables are pinned by 1.1 in the next chunk.",
      "contract": {
        "states": [
          "declared"
        ],
        "transitions": [
          {
            "input": "any update on a builder-form map",
            "state": "declared",
            "effect": "no-op",
            "evidence": "core/types.go:1118-1123 and :1155-1160 are byte-identical and move into trieRoot unchanged"
          }
        ],
        "forbidden": [
          "reading or writing the memo in this task",
          "any change to charges, Len, getByHashKey, iteration order, equality or printing"
        ],
        "seeding": {
          "declared": "add the field and the helper; run the existing suite"
        },
        "budgets": {
          "behaviour changes": 0,
          "conversion arms factored": 2
        }
      }
    }
  ],
  "requirements": [
    {
      "shall": "`HashMap` SHALL keep its public semantics — immutable operations, key domain, `Int`/`Float` key distinctness, deterministic iteration — while meeting efficiency bounds: a map operation SHALL NOT format a key into a string; iterating a map SHALL NOT allocate or re-sort per call for maps at or below the small-map threshold; and constructing, reading, or copying a small map SHALL allocate O(1) objects. Promotion between the small and large representations SHALL be semantically invisible: equality, iteration order rules, printing, and immutability are identical at both representations.",
      "tests": [
        "TestReaderBuiltMapMatchesSetBuilt",
        "TestHashMap_Equals_RepresentationBlind",
        "TestHashMap_PromotionBoundary",
        "TestHashMap_KeyIdentity",
        "TestHashMap_Immutability",
        "TestHashMap_Get_AllocsPerRun",
        "TestHashMap_Each_AllocsPerRun",
        "TestHashMap_Assoc_AllocsPerRun"
      ]
    },
    {
      "shall": "Above the small-map threshold an immutable update SHALL NOT copy the whole map: `Assoc` and `Dissoc` SHALL share the untouched majority of the structure with the receiver, so that the storage a single update allocates is bounded by the depth of the structure rather than by its entry count, and extending a map n times costs O(n log n) in total rather than O(n²). The receiver SHALL be unaffected by an update derived from it.",
      "tests": [
        "TestAssocMonotonic_ChargesPerCallHonestly",
        "TestHashMap_Assoc_AllocsPerRun",
        "TestHashMap_Immutability"
      ]
    },
    {
      "shall": "That bound SHALL hold whichever builder produced the receiver. A map above the threshold may be held in either large representation, and converting one to the other is a per-value cost, not a per-update one: repeated updates against one retained receiver SHALL NOT each pay a cost proportional to its entry count.",
      "tests": [
        "TestHashMap_FanOutAssocStaysBounded",
        "TestHashMap_FanOutDissocStaysBounded",
        "TestHashMap_FanOutFromMapLiteralStaysBounded",
        "TestHashMap_FanOutUnderDefaultAllocationCeiling",
        "TestHashMap_ConversionChargedOncePerValue",
        "TestMetering_MapConversionChargedToFirstToucher",
        "TestHashMap_MemoHoldsConvertedTrie",
        "TestHashMap_MemoLeavesReadPathOnBuilderForm"
      ]
    },
    {
      "shall": "The hash backing that structure SHALL be derived from fixed constants rather than a per-process random seed. A randomized seed would make the structure's shape, and anything derived from it, differ across restarts for identical input, which contradicts the determinism this requirement states.",
      "tests": [
        "TestHashMap_LargeFormPrintsIndependentOfBuildOrder",
        "TestHashMap_TrieMatchesOracle"
      ]
    },
    {
      "shall": "- **THEN** `Get` and iteration SHALL allocate nothing and `Assoc` SHALL allocate only the new map's storage",
      "tests": [
        "TestHashMap_Get_AllocsPerRun",
        "TestHashMap_Each_AllocsPerRun",
        "TestHashMap_Assoc_AllocsPerRun"
      ]
    },
    {
      "shall": "- **THEN** the operation SHALL NOT allocate a formatted string representation of the key",
      "tests": [
        "TestHashMap_KeyIdentity"
      ]
    },
    {
      "shall": "- **THEN** equality with a same-pairs map, iteration determinism, and immutability SHALL hold identically before and after promotion",
      "tests": [
        "TestHashMap_PromotionBoundary",
        "TestHashMap_Equals_RepresentationBlind"
      ]
    },
    {
      "shall": "- **THEN** the order SHALL be identical on every iteration and identical across both evaluators",
      "tests": [
        "TestHashMap_LargeFormPrintsIndependentOfBuildOrder"
      ]
    },
    {
      "shall": "- **THEN** the bytes and allocations a single call charges SHALL stay bounded as the map grows rather than rising in proportion to its entry count, and the receiver SHALL remain unchanged and independently readable",
      "tests": [
        "TestAssocMonotonic_ChargesPerCallHonestly",
        "TestHashMap_Assoc_AllocsPerRun"
      ]
    },
    {
      "shall": "- **THEN** the bytes and allocations charged SHALL stay bounded per update as the map grows, so that k updates against one receiver do not each charge in proportion to its entry count, and the receiver SHALL remain unchanged and independently readable",
      "tests": [
        "TestHashMap_FanOutAssocStaysBounded",
        "TestHashMap_FanOutDissocStaysBounded",
        "TestHashMap_FanOutFromMapLiteralStaysBounded",
        "TestHashMap_FanOutUnderDefaultAllocationCeiling",
        "TestHashMap_ConversionChargedOncePerValue",
        "TestMetering_MapConversionChargedToFirstToucher",
        "TestHashMap_MemoHoldsConvertedTrie"
      ]
    },
    {
      "shall": "- **THEN** each key SHALL resolve to its own value, `Dissoc` of one SHALL leave the others intact, and `Len` SHALL count them separately",
      "tests": [
        "TestHashMap_HashCollisions",
        "TestHamtNode_CollisionAssoc"
      ]
    },
    {
      "shall": "- **THEN** the resulting map SHALL print identically and iterate identically in every run",
      "tests": [
        "TestHashMap_LargeFormPrintsIndependentOfBuildOrder"
      ]
    }
  ],
  "testHarness": [
    "findCollidingKeys — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:373 — scans Int keys for a fixed-seed full-hash collision pair, returns (a, b)",
    "TestHashMap_PromotionBoundary — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:84 — builds to the limit via Assoc, checks m.large across the 9th key and Dissoc hysteresis",
    "TestHashMap_Assoc_AllocsPerRun — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:279 — testing.AllocsPerRun(1000) over one Assoc against a retained small receiver, allocs <= 2; the fan-out test's shape template",
    "TestHashMap_Get_AllocsPerRun — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:66 — same AllocsPerRun shape for Get",
    "TestHashMap_Each_AllocsPerRun — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:262 — AllocsPerRun over Each",
    "TestHashMap_TrieMatchesOracle — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:299 — 20000-step deterministic LCG mix of Assoc/Dissoc/Get against a Go-map oracle; the correctness net for any trie change",
    "TestHamtNode_CollisionAssoc — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:465 — builds a collision hamtNode directly, asserts node.assoc returns exactly hamtNodeBytes(out)",
    "TestHashMap_LargeFormPrintsIndependentOfBuildOrder — /home/zhuk/Projects/own/go-lispico/core/hashmap_test.go:532 — 200-key build-order determinism through String()/Equals()",
    "listSource / vectorSource — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:14,15 — n-item list and vector sources",
    "pairSource — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:19 — an n-pair map literal source; the map-literal fixture builder for the builder-form arm",
    "sameKeySource — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:33 — an n-pair literal rebinding one key",
    "promotionControlSource — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:41 — the non-promoting control literal isolating the promotion charge",
    "entryBufferBytes — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:57 — the doubling entry-buffer charge model for n entries",
    "allocationProbe — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:72 — a context wrapper recording the ledger total at every terminal-state check",
    "admittedForSource — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:83 — reads a source under a fresh ceiling, returns admitted bytes",
    "TestGuardedRead_ExactCharges — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:95 — the differencing method for exact charges: two reads sharing every term but one",
    "TestGuardedRead_MapConstructionContracts — /home/zhuk/Projects/own/go-lispico/core/reader_budget_accounting_test.go:416 — pins that a promoted literal lands in builder form, not trie",
    "planBytes — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:20 — tokens * readerTokenPlanBytes",
    "workBufferBytes — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:32 — parser workspace charge for n slots",
    "flatFormBytes — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:49 — workspace + slots for a flat n-child form",
    "outputBytes — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:58 — the context-free reader's reported output storage for a source",
    "allocCeilingContext — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:70 — (context.Context, EvalMeter) at a given MaxAllocationBytes; the per-meter isolation primitive the cross-meter test needs",
    "admittedBytes — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:75 — m.Snapshot().AllocationBytes",
    "readOwnedScratch — /home/zhuk/Projects/own/go-lispico/core/reader_budget_admission_test.go:80 — a guarded read against a caller-owned readerScratch",
    "chargedReductions — /home/zhuk/Projects/own/go-lispico/core/reader_budget_cancel_test.go:18 — m.Snapshot().Reductions",
    "readContextStats — /home/zhuk/Projects/own/go-lispico/core/reader_context_parity_test.go:14 — guarded read entry point wrapper returning (forms, stats, err)",
    "readerEntries / readSingleForm — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:25,35 — reader entry-point table and single-form read",
    "mapFixture / intMapFixture / collisionMapFixture / mixedKeyMapFixture — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:52,64,87,115 — map fixtures at 1, 8, 9, 33, 128 keys plus collision and mixed-key shapes, each carrying src, pairs, absent key",
    "collisionKeys — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:50 — var collisionKeys = [2]int64{3367, 6372}",
    "mapForm — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:190 — names the active storage form: entries | trie | builder | large-empty",
    "assertMapFormInvariants — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:202 — asserts the two large forms are mutually exclusive and no small entries survive",
    "assertMapParity — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:217 — full observable parity: form, Len, Get, Pairs, Each, String, Equals both ways",
    "assertCollisionArm — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:290 — drives a builder-form map through one Assoc, asserts trie form, large.count and collision-node placement; the only existing assertion exercising the builder-to-trie conversion",
    "nodeHolding — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:320 — walks hamtNode.get slot arithmetic, returns the node storing a hashKey",
    "eachPairs — /home/zhuk/Projects/own/go-lispico/core/reader_map_parity_test.go:341 — collects Each's pairs into a slice",
    "BenchmarkHashMap_AssocChain — /home/zhuk/Projects/own/go-lispico/core/bench_test.go:513 — threaded assoc at n = 100/1000/10000; the shape the fan-out benchmark must NOT duplicate",
    "BenchmarkHashMap_SetBuild — /home/zhuk/Projects/own/go-lispico/core/bench_test.go:531 — bulk Set build at n = 100/1000/10000; the builder-form fixture builder",
    "BenchmarkHashMap_GetLarge — /home/zhuk/Projects/own/go-lispico/core/bench_test.go:549 — Assoc-built (trie-form) fixture at n = 100/1000/10000; the trie-form fixture builder",
    "setupEnv — /home/zhuk/Projects/own/go-lispico/plugins/stdlib/stdlib_test.go:11 — a *core.Env with stdlib loaded",
    "collectionGoFunc — /home/zhuk/Projects/own/go-lispico/plugins/stdlib/collections_extra_test.go:328 — pulls a named collection builtin out of an env as a core.GoFunc",
    "TestAssocMonotonic_ChargesPerCallHonestly — /home/zhuk/Projects/own/go-lispico/plugins/stdlib/monotonic_test.go:19 — the real assoc builtin under core.WithEvalResourceLimits, reading deltas off core.EvalMeterFrom(ctx).Snapshot().AllocationBytes; the meter-observation template",
    "testEvalMeter — /home/zhuk/Projects/own/go-lispico/core/meter_test.go:134 — a sessionMeter stub in package core",
    "recordingEvalMeter — /home/zhuk/Projects/own/go-lispico/core/vm/const_charged_test.go:152 — a sessionMeter capturing charges",
    "TestConstCharged_SharingReturnsSameHashMap — /home/zhuk/Projects/own/go-lispico/core/vm/const_charged_test.go:77 — pins that two evaluations of one map literal return the SAME *core.HashMap pointer (chunk-cached folded constant)"
  ],
  "floor": "make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 2m -p 2 -parallel 2 ./core && openspec validate hashmap-builder-trie-conversion --strict --json",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 3
  }
}
```
