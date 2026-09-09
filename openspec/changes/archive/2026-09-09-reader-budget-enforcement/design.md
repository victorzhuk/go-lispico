## Context

See `proposal.md` for the measured failure. `Reader.countTokens` already performs
a counting pass without decoding escaped payloads. `tokenizeInto` then allocates
the entire token slice, and `readerScratch.read` builds all forms before
`runtime.readForms` invokes `ChargeEvalReader`. The source string is already
owned by the caller of the reader.

`ReaderStats` is a published output metric. Existing allocation-efficiency and
pooling tests require its values to remain stable; they do not account temporary
reader storage. This design adds explicit workspace charges rather than changing
what an output node means.

## Goals / Non-Goals

Goals: bound work and input-dependent storage before materialization, share the
evaluation ledger and deadline, and preserve successful language results,
positions, and reader statistics.

Non-goals: new resource configuration fields, a streaming source API, filesystem
read limits, changed core value representations, or a global rewrite of metering.
Context-free core reads remain available for trusted host code.

## Decisions

### Add one guarded reader entry point

New API proposed by this change:

```go
func (d Dialect) ReadWithContextStats(ctx context.Context, src string, maxDepth int) ([]Value, ReaderStats, error)
```

Keep `Read`, `ReadOne`, and existing dialect reader methods unchanged. Share the
scanner/parser implementation, with per-read budget state installed only for the
new path. `runtime.readForms` uses it and removes its unconditional
`ChargeEvalReader` call; the reader now owns that output charge. Direct legacy
callers that explicitly charge `ReaderStats` keep their existing contract.

Arm `Eval`'s deadline before `readForms`, as the binding-scope path already does.
The watcher creates one detached evaluation context with its engine deadline for
the read/evaluate/merge lifecycle. Reuse the predecessor changes' meter start,
settlement, and outcome ordering; do not reset state between read and execute.

### Budget both scanning passes and parsing

Use a small private reader budget over the existing `EvalMeter`. It captures
`EvalDeadlineFrom(ctx)`, owns a pending work count, and retains the first terminal
failure. Its new private checkpoint flushes the pending count exactly once
through `EvalMeter.ChargeReductions`, then checks the captured deadline through
`nowFunc` and checks `ctx.Err()` directly. Check cancellation/deadline even if the
pending count is zero. Do not wrap `PollEvalState` or `pollCancel`: those already
batch and charge evaluator work, so nesting them would both delay observation
and introduce undocumented reductions. Allocation admission uses
`EvalMeter.ChargeAllocBytes` before allocation.

Charge one reduction for every byte consumed by each scan, one for every output
node including quote-generated nodes, one per copied, hashed, or compared byte,
and one per visited, linked, compared, cleared, or copied collection/workspace slot. Numeric
conversion has the separate token-length charge below. Synchronize after at
most 128 interruptible units and on every return. Check before beginning work,
including empty input, and when settling a syntax failure. Tests must pin the
complete charge without hidden checkpoint overhead.

Place scan accounting where bytes advance, so comments, whitespace, long numeric
or symbol tokens, and escape handling cannot hide uninterruptible loops.
Opaque numeric conversions may run only after their token length has passed a
deterministic work admission check; charge the conversion's token length before
entry, in addition to the two tokenizer traversals. Their maximum input is
bounded by the remaining work allowance, not by an unchecked source suffix.
This is the sole explicit opaque reader exception: at entry, a token cannot
exceed the configured total reduction budget divided by three, and earlier work
tightens that bound. Check terminal state again immediately after conversion.
The 128-unit guarantee applies to interruptible scanner/parser work, with this
single admitted conversion's duration added when cancellation occurs inside it.

Keeping an entry/exit context check alone would leave the current long-token
failure intact. Replacing the two-pass reader with a new streaming parser is
unnecessary for the admission guarantee.

### Cover construction and diagnostics

The guarded path must not call input-sized constructors that bypass checkpoints.
Use new private budget-aware construction helpers within `core`; preserve the
existing value representations and legacy constructor signatures.

| Phase | Guarded implementation |
| --- | --- |
| Escaped-string prefix and payload copies | Pre-admit decoded storage; copy in batches of at most 128 bytes, including the prefix currently passed to `strings.Builder.WriteString` |
| Parser buffers and final collection copies | Admit the destination first; copy bounded slot batches |
| Large list finalization | Build the existing `listNode` chain with checkpoints while linking, after child parsing; do not call the unguarded linear `NewList` path |
| Small map insertion | Preserve sorted small entries; compare key bytes and move entries in bounded batches |
| Large map insertion | Build directly into the existing HAMT representation with private owned construction helpers; hash key bytes, scan collisions, compare keys, and copy/grow node storage under the reader budget |
| Numeric conversion | Pre-admit token work and conversion/error storage; check immediately before and after conversion |

The guarded map builder must not route through the Go-map branch of `HashMap.Set`:
hashing and growth there cannot expose reader checkpoints. Reuse the existing
fixed hash algorithm, key identity, small-map threshold, and HAMT geometry.
Budget every collision scan and key comparison, including comparisons against
previously admitted long keys. Reader-owned nodes are unpublished until the
read succeeds, so the private builder can mutate those nodes; no shared value
may be mutated. This does not change public `Set` or `Assoc`, JSON construction,
or the map's existing observable ordering and equality contracts.

Numeric conversion receives a new deterministic temporary-storage charge of
`2 * tokenBytes + 256` bytes, with checked arithmetic, before entering `strconv`.
This models conversion/error token copies plus bounded diagnostic storage; it is
not an exact Go heap measurement. Render at most 128 source bytes in an invalid
number diagnostic, with a truncation marker for a longer token, retaining the
error kind and source position. Never format the complete long token or call
the converter's error renderer. Pre-admit this storage even when conversion
succeeds, so success/failure and pool state cannot bypass admission. Legacy
context-free diagnostics remain unchanged. Verify a long overflowing number
with ample reductions and a 1 KB allocation ceiling is rejected before conversion.

### Admit token storage during counting

The counting pass tracks the token count with overflow-safe arithmetic. A token
costs a new deterministic 32-byte workspace unit, including the EOF token. Stop
counting when the token plan cannot fit the current remaining allocation
allowance; do not allocate the token slice to discover that failure.

Reserve the complete admitted token plan before `tokenizeInto` obtains storage.
Pool reuse does not waive the charge. Track decoded lengths while counting so
escaped strings can reserve their output payload before decoding on the second
pass. Charge copied payload once when admitted; credit that existing charge when
the corresponding AST string node is created. Zero-copy payloads retain the
existing output-byte charge without introducing a second copy charge.

### Charge output and parser workspace separately

Each output node retains the existing 32-byte reader charge, plus its existing
payload bytes. Reserve synthetic quote nodes and collection construction before
allocating them. `ReaderStats` still records the final output totals even when a
payload was prepaid; the ledger records only the unpaid delta.

Parser node scratch and the top-level form work buffer use the existing 16-byte
value-slot unit. Their logical allocation schedule starts fresh per read: an
initial required slot, then checked doubling when that logical capacity fills.
Charge the full new logical buffer before each growth, because old and new
buffers can coexist during copying. Reused physical capacity may avoid the
allocation but not its deterministic logical charge. Final flat collection
copies remain covered by reader output accounting. Also admit construction
storage not covered by those flat copies: 32 bytes per linked-list cell, and the
existing `MeterCollectionHeaderBytes`, `MeterHashMapEntryBytes`, and
`MeterTrieChildBytes` units for each HAMT node and its entry/child buffers.
Small-map entry buffers use `MeterHashMapEntryBytes`. Buffer growth charges the
complete new logical allocation before bounded copying; use checked doubling
for variable collision buffers and exact occupied sizes for fixed-width trie
nodes. These are additional construction charges, separate from AST node and
payload charges. Maintain counters in the private reader state rather than
exposing another public statistics type.

Before release, clear the input pointer and all reference-bearing scratch slots
used by this or a previous read. Drop retained buffers whose modeled capacities
exceed the current read's allocation ceiling. A low-budget read must not inherit
either a larger allowance or permanently retained high-water storage. Clear in
bounded batches during successful cleanup; on terminal failure discard buffers
whose cleanup would require further input-sized work instead of traversing them.
Keep the returned AST's backing storage independent as required today.

### Verify admission, not a particular Go heap total

Use deterministic scanner-position/admitted-storage checks on a directly owned
scratch object. A fixed low limit must reject before constructing the large
suffix; enlarging that suffix must not increase admitted storage. Use the public
guarded reader for exact-charge and one-byte-below tests. Compare successful
output/statistics against legacy reads. Test pool reuse on the same scratch
object rather than assuming `sync.Pool` returns a particular entry.

Runtime tests cover both evaluators and dialects, context and engine meters,
inherited/disabled deadlines, failed reads, and all source entry points. Keep
clock and cancellation tests deterministic with existing package-private seams
or local test contexts; no sleep-based primary regression.

## Risks / Trade-offs

- Additional reader charges reject some previously accepted sources under tight
  budgets. Document the table and version-specific reduction model; preserve
  configured defaults and do not hide the change by inflating test budgets.
- Counting and decoding can account the same payload twice. Track its accounting
  owner explicitly and pin exact charges for escaped and unescaped strings.
- Pools retain references even after slice truncation. Clear backing slots before
  returning scratch, without touching separately owned returned collections.
- The legacy core reader and file acquisition remain outside the guarded path.
  Describe that boundary explicitly in the existing resource documentation.
- More reader checkpoints can affect parsing throughput. Compare bounded parse
  benchmarks against the prerequisite baseline; retain the existing consumer
  performance gate rather than weakening its thresholds.
- Guarded large-map construction uses the existing trie instead of the Go-map
  builder. Verify both sides of promotion, long keys, duplicate keys, collisions,
  and construction cost; keep the small-map allocation contract intact.

## Migration Plan

After both runtime lifecycle predecessors are archived, add failing admission
and cancellation tests, implement the guarded reader, and route runtime parsing
through it. Amend existing ADR 0007/0011, `ARCHITECTURE.md`, `README.md`, and the
unreleased changelog. No data migration or new dependency is required. Reverting
the change restores the old acceptance boundary and its resource-safety gap;
switching evaluators does not bypass reader accounting.

## Frozen charge model

The accepted spec, not this document's prose, governs what a guarded read admits.
`specs/core-engine/spec.md` adds the 32-byte token unit and the 16-byte workspace slot unit
*to the existing reader node and payload model*, and requires that reader output retain its
existing node and payload charges, with no second post-parse charge. ADR 0011 carries the same
table for readers outside this change.

| Term | Unit and formula | Admitted |
| --- | --- | --- |
| T1 token plan | `32 * tokens, EOF token included` | once, by readerBudget.reservePlan, after countTokens completes and before tokenizeInto obtains storage |
| T2 escaped-string decoded payload | `sum over escaped string tokens of len(decoded); counted into Reader.copiedPayload on pass 1 (reader.go:375-378)` | in the same reservePlan call, second admitAlloc, before pass 2 decodes anything |
| T3 numeric-conversion temporary storage | `2*len(tok.val) + 256, checked arithmetic (checkedConversionBytes)` | per numeric token, in parseNumberToken, before strconv is entered, on success and failure alike |
| T4 output-node storage | `per node: MeterReaderNodeBytes (32) + that node's payload bytes; payload term SKIPPED when the node comes from a token with copied == true` | in Parser.addNode, BEFORE the node value is constructed. Every call site already precedes its allocation: reader.go:715-718 (list), 747-750 (vector), 782-785 (map), 791-797 (wrapForm), and each atom arm at 639-680. |
| T5 parser workspace | `MeterValueSlotBytes (16) per logical slot, on a fresh-per-read logical doubling schedule: capacity 1, then doubling; the WHOLE new logical capacity is charged before each growth, because old and new buffers coexist during the copy` | before each logical growth of the parser node scratch and the top-level form buffer |
| T6 construction storage | `flat collection copy-out ValueSlotsBytes(n) for the items := make([]Value, n) at reader.go:707, 739, 827; linked-list cells readerListCellBytes (32) * n, only when n > listFlatThreshold (32); small-map entry buffer MeterHashMapEntryBytes (64) per logical slot on the SAME doubling schedule as T5 - helper entryBufferBytes(n); HAMT node hamtNodeBytes(out) = MeterCollectionHeaderBytes + 64*len(out.entries) + 8*len(out.children), per node the guarded builder allocates, admitted before allocating it` | each subterm immediately before the storage it names is allocated |

**Worked totals.** Each is the full T1-T6 sum for one source, derived from the units above.

| Source | Admitted total |
| --- | --- |
| `(a b c)` | 499 |
| `'a` | 246 |
| `pairSource(4) = {:k0 v :k1 v :k2 v :k3 v }` | 1116 |
| `pairSource(9) - past the small-map threshold` | >= 2883 (floor; the exact total is derived, not literal) |
| `listSource(40) - past listFlatThreshold` | 6696 |
| `({:k0 v :k1 v :k2 v } [a a a a ]) - the sealed nested-collections case` | 1773 |
| `"a\nb" (escaped)` | 115 |
| `"ab" (zero-copy)` | 114 |
| `12345` | 378 |
| `3000 comment lines then (a)` | 241 |
| `one 8000-byte symbol` | 8112 |
| `(a b c) 1.2.3 - the malformed-suffix case` | 829 |

**Credit ruling.** The credit path is removed. `EvalMeter.creditAllocBytes`,
`evalState.creditAllocBytes` and `readerBudget.creditAlloc` are deleted along with their call
site; `addNode` instead skips the payload term when the source token has `copied == true`.
Totals are byte-for-byte identical, and `AllocationBytes` stays monotonically non-decreasing
for one `evalState` — the invariant `addCharge`, `saturateCounter` and `publishedTotal` rest on.

**Sealed-test reconciliation.** Eleven named amendments to two already-committed test files.
Every row names its file, test, case, the old expectation and the new one.

| File | Test | Case | Was | Now |
| --- | --- | --- | --- | --- |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_TokenPlanAdmission` | long-trivia/costs-no-token-storage | planBytes(4)+flatFormBytes(1) = 176 | planBytes(4)+flatFormBytes(1)+outputBytes(t, src) = 241 |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_TokenPlanAdmission` | long-token/costs-one-workspace-unit | planBytes(2)+workBufferBytes(1) = 80 under a 1024-byte ceiling, read expected to SUCCEED | RESTRUCTURE, not renumber. Ceiling 1024 -> DefaultMaxAllocationBytes; want = planBytes(2)+workBufferBytes(1)+MeterReaderNodeBytes+8000 = 8112. Preserve the case's actual point ('token storage is per token, not per byte') by adding a paired read of a 16000-byte symbol and asserting the delta is exactly 8000 - payload only, with no additional plan bytes. |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_TokenPlanAdmission` | exact-ceiling/admits-the-plan AND one-byte-below/rejects-the-plan | want := planBytes(6) + flatFormBytes(3) = 368 | want := planBytes(6) + flatFormBytes(3) + outputBytes(t, "(a b c)") = 499 |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_PoolReuseKeepsPayingThePlan` | - | read*(planBytes(6)+flatFormBytes(3)) = read*368 | read*499 |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_MalformedSuffixAdmission` | - | planBytes(7)+flatFormBytes(3)+conversionBytes(5) = 666 | + outputBytes for 5 nodes and 3 payload bytes = 829 |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_EscapedPayloadAdmission` | credits-the-copy-when-the-node-lands | planBytes(2)+workBufferBytes(1) = 80, named for a credit | RENAME to charges-the-prepaid-payload-once; want = planBytes(2)+workBufferBytes(1)+MeterReaderNodeBytes = 115 (64 plan + 3 reserved payload + 32 node + 16 buffer). Nothing is credited any more. |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_EscapedPayloadAdmission` | zero-copy-payload-gains-no-copy-charge | 80 | planBytes(2)+workBufferBytes(1)+MeterReaderNodeBytes+2 = 114 |
| `core/reader_budget_admission_test.go` | `TestGuardedRead_NumericConversionStorage` | all four sub-cases derive from want | planBytes(2)+workBufferBytes(1)+conversionBytes(5) = 346 | + MeterReaderNodeBytes = 378 |
| `core/reader_budget_accounting_test.go` | `TestGuardedRead_ExactCharges` | generated-quote-node | planBytes(1) + 2*MeterValueSlotBytes = 64 | planBytes(1) + 2*MeterValueSlotBytes + 2*MeterReaderNodeBytes + int64(len("quote")) = 133 |
| `core/reader_budget_accounting_test.go` | `TestGuardedRead_ExactCharges` | parser-workspace-doubling | planBytes(1) + MeterValueSlotBytes + 8*MeterValueSlotBytes = 176 | + MeterReaderNodeBytes + int64(len("a")) = 209 |
| `core/reader_budget_accounting_test.go` | `TestGuardedRead_PrepaidPayloadKeepsOutputTotals` | - | assertion is CORRECT and stays - both ("a\nb") and ("axb") admit 243 | comment only: 'the copy is credited when its node lands' and 'only the unpaid delta' describe a credit that no longer exists. Restate as: the payload is charged once, at reservation, and the node it becomes admits only its node unit. |

## Implementation plan

Tier **heavy**, testing mode **existing-service-strict**, base `75399eb6`.
Lenses: spec, quality, arch, perf, sec. Review verdict: pass (zarchitect, 3 rounds).

Seven of the change's nineteen tasks are already implemented on `zapply/reader-budget-enforcement`.
The chunks below cover the remaining twelve.

### `construct` — tasks 1.3, 2.4

- **shape**: first; seam `S1`; coder `go-coder`
- **red / code**: ['1.3'] / ['2.4']
- **red tests**: TestGuardedRead_AdmitsOutputStorage, TestGuardedRead_AdmittedBytesNeverDecrease, TestGuardedRead_FailedConversionKeepsItsNodeCharge, TestGuardedRead_WorkspaceHighWaterSpansNesting, outputBytes (helper), entryBufferBytes (helper)
- **sites**:
  - `core/reader_budget_admission_test.go` — TestGuardedRead_TokenPlanAdmission — anchor `func TestGuardedRead_TokenPlanAdmission`
    - already authored and committed (46db694, f68e4f2, amended 75399eb): the red stage AMENDS it to the frozen model — 8 of the 11 rows in amendments[] land here, including the long-token case that is restructured rather than renumbered — adds the outputBytes helper, and does not re-author it. Listed as a site so guard admits the amendment instead of reading it as a sealed-test violation.
  - `core/reader_budget_accounting_test.go` — TestGuardedRead_ExactCharges — anchor `func TestGuardedRead_ExactCharges`
    - already authored and committed (e077766, amended 75399eb): the red stage AMENDS it to the frozen model per amendments[], adds the four new red tests, and does not re-author it
  - `core/reader.go` — Parser.parseList — anchor `func (p *Parser) parseList() (Value, error) {`
    - supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[] Flat-copy shape: admit ValueSlotsBytes(n) for the destination BEFORE the copy, then copy in bounded slot batches; the same shape applies to parseVector and the map literal path.
  - `core/reader.go` — Parser.parseVector / Parser.parseReaderVector — anchor `func (p *Parser) parseVector() (Value, error) {`
    - Same flat-copy shape as parseList (and duplicated again in parseReaderVector, anchor `func (p *Parser) parseReaderVector() (Value, error) {`): admit ValueSlotsBytes(children) before `make`, then bounded copy. NewVector stays flat at every size, so no cell charge here. These three bodies are byte-identical apart from the closing token and constructor; keep them consistent.
  - `core/reader.go` — Parser.parseHashMap — anchor `		err = m.Set(key, val)`
    - supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]
  - `core/reader.go` — readerScratch.read (top-level form buffer) — anchor `	var forms []Value`
    - The top-level form buffer grows by bare append with no charge. Charge the logical value-slot growth schedule the sealed helper models — workBufferBytes in core/reader_budget_admission_test.go:32: an initial 1-slot buffer, then the whole doubled capacity charged before each growth, never discounted by retained physical capacity. flatFormBytes (same file:49) = workBufferBytes(1) + workBufferBytes(children) + ValueSlotsBytes(children), i.e. this buffer and Parser.nodes both follow that schedule.
  - `core/reader.go` — Parser.nodes (parser node scratch) — anchor `	nodes []Value`
    - supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]
  - `core/reader.go` — Parser.wrapForm / Parser.addNode — anchor `func (p *Parser) wrapForm(sym string, form Value) (Value, error) {`
    - supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]
  - `core/types.go` — newListChain / NewList / listFlatThreshold — anchor `func newListChain(items []Value) *listNode {`
    - supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]
  - `core/types.go` — HashMap.Set (Go-map promotion branch) — anchor `		m := make(map[hashKey]entry, len(h.entries)+1)`
    - This is the branch the guarded builder must never reach. Do not change Set, Assoc, hashMapSmallLimit=8, hashOfKey, the entry layout, or the observable ordering/equality contracts; add the private guarded construction beside them. Reader-owned nodes are unpublished until the read succeeds, so the private builder may mutate its own nodes, never a shared one. core/hashmap_test.go:414 already asserts the trie form for the Assoc path — do not weaken it.
  - `core/types.go` — hamtNode.assoc / hamtNodeBytes / hashOfKey / HashMap.find — anchor `func (n *hamtNode) assoc(e entry, h uint32, shift uint) (*hamtNode, int64, bool) {`
    - Geometry to reuse verbatim: vecBits/vecBranch shift math, compacted entries/children, mergeEntries collision fallback past shift 32, and hamtNodeBytes = MeterCollectionHeaderBytes + len(entries)*MeterHashMapEntryBytes + len(children)*MeterTrieChildBytes (`func hamtNodeBytes(n *hamtNode) int64 {`). The guarded builder charges: hashOfKey per key byte (`func hashOfKey(hk hashKey) uint32 {` iterates hk.str), collision-scan and key comparisons (`func (h *HashMap) find(hk hashKey) (int, bool) {` is the small-form linear scan), and hamtNodeBytes for every node it allocates, exact-size copies (core/types.go:827,832), never checked doubling — checked doubling applies only to variable collision buffers. Sealed floors (reader_budget_accounting_test.go:106): small-map 4 -> 4*MeterHashMapEntryBytes, 8 -> 8*, promoted 9 -> MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes, 12 duplicate keys -> at least one MeterHashMapEntryBytes.
  - `core/metering.go` — EvalMeter.creditAllocBytes — anchor `func (m EvalMeter) creditAllocBytes`
    - DELETE EvalMeter.creditAllocBytes and evalState.creditAllocBytes. They are the only writers that move a meter counter down and sit outside the addCharge/saturateCounter/publishedTotal wrap-safety proof.
  - `core/reader_budget.go` — readerBudget.creditAlloc — anchor `func (b *readerBudget) creditAlloc`
    - DELETE readerBudget.creditAlloc; add readerListCellBytes and the growthPlan doubling helper. addNode skips the payload term when tok.copied is set instead of crediting after the fact.
- **redRun**: `go test -timeout 2m -p 2 -parallel 2 ./core`
- **verify**: `go test -timeout 2m -p 2 -parallel 2 ./core && go vet ./core && golangci-lint run ./core/...`
- **seam summary**: Amend the two sealed reader budget test files to the frozen charge model, add the four output-storage red tests, then admit output-node, parser-workspace and construction storage in the guarded reader and delete the credit path.

### `scratch` — tasks 2.5

- **shape**: serial after `construct` (shares `core`); seam `S2`; coder `go-coder`
- **red / code**: none (waived) / ['2.5']
- **red tests**: TestGuardedRead_ScratchReleaseDropsOversizedCapacity, TestGuardedRead_FailedReadReleasesScratch, TestGuardedRead_RetainedASTSurvivesScratchReuse, TestGuardedRead_ConcurrentCrossDialectReads
- **sites**:
  - `core/reader.go` — readerScratch.Reset — anchor `func (s *readerScratch) Reset() {`
    - Extend to clear every reference-bearing slot used by this or a previous read, in bounded batches: reader.input (currently NOT cleared — it is only overwritten at the next checkout, so a returned scratch pins the whole source string), the tail of s.tokens beyond the new length (token.val strings), and the tail of parser.nodes and parser.tokens. Add the discard rule the spec requires: drop retained buffers whose modeled logical capacity exceeds the current read's allocation ceiling instead of returning them to the pool, and on terminal failure discard rather than traverse. Note TestReaderScratch_ResetClearsAllFields (core/reader_pool_test.go:36) is hand-written, not reflective — it will not fail on a field you forget to clear.
  - `core/reader.go` — readerScratchPool / Dialect.ReadWithContextStats put-back — anchor `var readerScratchPool = sync.Pool{`
    - The pool is shared across every Dialect and both entry points; the guarded put-back is `defer func() { s.budget = nil; readerScratchPool.Put(s) }()` in core/dialect.go (anchor `func (d Dialect) ReadWithContextStats(ctx context.Context, src string, maxDepth int) ([]Value, ReaderStats, error) {`). Route the put-back through the new clear/discard decision so an oversized buffer is not returned. Preserve TestReaderScratch_RetainedTreeSurvivesReuse and TestReadWithMaxDepthStats_PooledNoEscapeStringSharesBackingArray: returned ASTs keep aliasing the caller's source string, so clearing must not touch storage a returned value owns.
  - `core/reader_scratch_release_test.go` *(new)* — TestGuardedRead_FailedReadReleasesScratch
    - new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)
- **verify**: `go test -timeout 2m -p 2 -parallel 2 ./core && go test -race -timeout 2m -p 2 -parallel 2 ./core && golangci-lint run ./core/...`
- **seam summary**: NO-RED-WAIVER: 2.5 is a single self-contained lifecycle task whose own text names its verification clauses; splitting it would invent tasks the change does not have, so the coder writes its four named tests and the -race floor gates them. Clear scratch references on every exit and discard retained buffers above the current ceiling. The chunk sites are the guard allowlist: they are the only test files this coder may add or change, and every other test in its pkgDirs is pre-sealed before it runs and guarded after.

### `routing` — tasks 0.2, 3.1

- **shape**: serial after `scratch` (shares `core`); seam `S3`; coder `go-coder`
- **red / code**: ['0.2'] / ['3.1']
- **red tests**: TestEval_ReaderBudget_Characterization
- **sites**:
  - `runtime/eval_reader_budget_test.go` *(new)* — TestEval_ReaderBudget_Characterization
    - File does not exist on the branch: it was deleted by commit 4ecc086 ("test: defer reader budget characterization to routing chunk") to return with the routing chunk. Restore it verbatim from `git show 4ecc086^:runtime/eval_reader_budget_test.go` (111 lines, package runtime, testify) alongside task 3.1 and let it go green. It defines readerBudgetBytes=1<<10, readerBudgetReductions=1_000_000, wideQuotedList, newReaderBudgetEngine, readerBudgetCtx, admittedBytes(ctx), legacyReaderBytes and four subtests: refuses_before_the_full_parse, unread_suffix_does_not_widen_admission, cancelled_context_rejects_comment_only_source, cancellation_precedes_syntax_error. It reuses meteringLimits + isResourceLimit from runtime/resource_limits_test.go. (new file: no anchor at baseSha)
  - `runtime/eval.go` — engineImpl.readForms — anchor `	forms, stats, err := e.config.dialect.ReadWithMaxDepthStats(input, e.config.limits.MaxReaderDepth)`
    - Switch to e.config.dialect.ReadWithContextStats(ctx, input, e.config.limits.MaxReaderDepth) and delete the following `if err := core.ChargeEvalReader(ctx, stats); err != nil {` block (the duplicate post-parse output charge). Three call sites, all named below. Note core.ChargeEvalReader itself stays exported for legacy/host callers.
  - `runtime/eval.go` — engineImpl.Eval (deadline arming order) — anchor `	if d := e.evalDeadline(ctx, start); !d.IsZero() && core.EvalDeadlineFrom(ctx).IsZero() {`
    - This block currently runs AFTER `forms, err := e.readForms(ctx, input)`, so a guarded read in Eval sees no engine deadline. Move it above the readForms call, matching evalWithBindingScope, which already arms with `ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))` before reading. Keep StartEval/leased before the read so one ledger spans read, compile and execute.
  - `runtime/watch.go` — fileWatcher.reloadFile — anchor `	ctx := w.engine.evalResourceContext(core.DetachEvalState(w.ctx))`
    - The reload context carries a meter and limits but no deadline and no StartEval lease. Arm the engine deadline on this detached context before `forms, err := w.engine.readForms(ctx, string(content))` so background reloads are interruptible; the existing early return on a read error already prevents childEnv.MergeInto, which is the no-replacement-bindings guarantee task 3.2 asserts.
  - `runtime/eval_reader_budget_test.go` *(new)* — TestEval_ReaderBudget_Characterization
    - new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)
- **redRun**: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`
- **verify**: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && golangci-lint run ./core/... ./runtime/...`
- **seam summary**: Route readForms through the guarded reader, remove the post-parse ChargeEvalReader, arm the deadline before parsing on every runtime-owned path, and restore task 0.2's characterization test.

### `entries` — tasks 3.2

- **shape**: serial after `routing` (shares `runtime`); seam `S4`; coder `go-coder`
- **red / code**: none (waived) / ['3.2']
- **red tests**: TestPublicEntries_ReaderBudget
- **sites**:
  - `runtime/eval.go` — engineImpl.Eval / EvalWithBindings / LoadScope — anchor `func (e *engineImpl) evalWithBindingScope(ctx context.Context, source string, bindings map[string]core.Value) (result core.Value, childEnv *core.Env, err error) {`
    - Public entry tests (new runtime _test.go). EvalWithBindings and LoadScope both funnel through evalWithBindingScope (`forms, err := e.readForms(ctx, source)`); Eval reads at `forms, err := e.readForms(ctx, input)`. Cover both evaluator modes (WithBytecode / WithTreeWalker, last-wins), both dialect reader surfaces (cl.Dialect(), clojure.Dialect(), plus WithReaderVector/WithFunctionRef), context meter vs engine meter (WithMeter / engineMeter), and assert an over-budget source executes no forms. Existing builders to reuse: newLimitsEngine, meteringLimits, evalLimits, isResourceLimit, evalModeName (runtime/resource_limits_test.go).
  - `runtime/watch.go` — fileWatcher.reloadFile (hot-reload publication) — anchor `	if err := childEnv.MergeInto(w.engine.rootEnv); err != nil {`
    - Assert a failed reload publishes no replacement bindings: the read error path returns before MergeInto. Existing precedent tests to extend: TestReloadFile_SyntaxErrorKeepsOldDefinitions (runtime/watch_test.go:115), TestReloadFile_EvalErrorKeepsOldDefinitions (:143), TestReloadFile_PanicSurfacesErrorAndKeepsOldDefinitions (:171) — all drive reloadFile directly rather than through the ticker, which keeps the test deterministic.
  - `runtime/eval_reader_entry_test.go` *(new)* — TestPublicEntries_ReaderBudget
    - new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)
- **verify**: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && golangci-lint run ./runtime/...`
- **seam summary**: NO-RED-WAIVER: 3.2 is a test-only task with no paired code task in tasks.md; the routing it exercises lands in the routing chunk, so the coder writes these public-entry tests over an already-routed reader. Public entry tests for Eval, EvalWithBindings, LoadScope and background hot reload, across both evaluator modes, both dialect reader surfaces, and context vs engine meters. The chunk sites are the guard allowlist: they are the only test files this coder may add or change, and every other test in its pkgDirs is pre-sealed before it runs and guarded after.

### `settle` — tasks 3.3

- **shape**: serial after `entries` (shares `runtime`); seam `S5`; coder `go-coder`
- **red / code**: none (waived) / ['3.3']
- **red tests**: TestGuardedRead_SingleSettledOutcome
- **sites**:
  - `runtime/eval.go` — engineImpl.Eval / evalWithBindingScope settlement defer — anchor `func (e *engineImpl) Eval(ctx context.Context, source, input string) (result core.Value, err error) {`
    - supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]
  - `runtime/settlement_observation_test.go` — runSettlementOutcomes / assertSettledOnce / settlementProbe — anchor `func runSettlementOutcomes(t *testing.T, entry string, invoke settlementInvoke, extra func(t *testing.T, tc settlementOutcome, scope *core.Env)) {`
    - The predecessors' (metered-call-deadlines, evaluation-outcome-settlement) lifecycle harness — extend it with a reader-failure outcome rather than building a second one. settlementOutcomes()/outcomeEntries() enumerate the outcome cases, assertSettledOnce pins one event + one stats increment + one error, and TestEval_TerminalSettlementErrorWinsOverEvalError / TestEval_TerminalSettlementErrorWinsOverRecoveredPanic pin precedence.
  - `runtime/eval_reader_settlement_test.go` *(new)* — TestGuardedRead_SingleSettledOutcome
    - new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)
- **verify**: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && golangci-lint run ./runtime/...`
- **seam summary**: NO-RED-WAIVER: 3.3 is a verification task over the predecessors' settled lifecycle with no paired code task in tasks.md; the coder writes its outcome tests. Exactly one settled outcome for source errors, meter denial and cancelled empty reads, preserving the predecessors' lifecycle and panic behavior. The chunk sites are the guard allowlist: they are the only test files this coder may add or change, and every other test in its pkgDirs is pre-sealed before it runs and guarded after.

### `docs` — tasks 4.1

- **shape**: parallel; seam `S6`; coder `zdocs`
- **red / code**: none (waived) / ['4.1']
- **sites**:
  - `docs/adr/0011-reduction-and-allocation-metering.md` — Fixed size table / Charge sites — anchor `- reader output, charged immediately after `Read` and before the first form runs;`
    - This bullet is the statement task 3.1 invalidates for the guarded path. Amend the charge-site list and the fixed size table (anchor `| Reader node | 32 bytes per parsed node |`) with the guarded-reader rows the change introduces: 32-byte token plan unit, decoded-payload reservation and the skip-at-source rule (addNode omits the payload term when tok.copied is set), 2*tokenBytes+256 numeric-conversion storage, 32 bytes per linked list cell, the value-slot workspace growth schedule, and the no-duplicate-charge rule. Also state the opaque numeric-conversion bound (token <= MaxReductions/3) and the 128-byte bounded diagnostic. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns.
  - `docs/adr/0007-resource-limits.md` — Consequences — anchor `## Consequences`
    - Record that the reader is no longer depth-only: the guarded entry point observes ctx cancellation, the engine deadline and the allocation ledger, while the legacy context-free reader (core.Read / ReadOne / Dialect.Read / ReadWithMaxDepth[Stats]) keeps its depth-only contract, and filesystem source acquisition stays outside the boundary. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns.
  - `ARCHITECTURE.md` — Resource Limits / Reader — anchor `Reader output is also charged into the evaluation's allocation ledger before the`
    - Rewrite this sentence for the guarded boundary (the reader now admits its own storage during the read, no separate post-parse charge on the runtime path) and add the guarded-reader description to the `#### Reader` section: two scan passes, both charged per byte; token plan admitted during counting; guarded collection construction; bounded diagnostics. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns.
  - `README.md` — Resource limits — anchor `the fixed deterministic size table in ADR 0011. Reader output is charged`
    - Same correction in the user-facing table section (anchor `## Resource limits` for the section itself): the deterministic work/storage table, the numeric-conversion exception, and that some previously accepted sources are refused under tight budgets. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns.
  - `CHANGELOG.md` — Unreleased — anchor `## [Unreleased]`
    - Add the entry under the existing `### Added` / `### Changed` subsections: new Dialect.ReadWithContextStats; runtime source paths now read under the evaluation ledger and deadline; reader charges changed, so tightly budgeted sources can now be refused during reading. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns.
- **verify**: `go build ./...`
- **seam summary**: NO-RED-WAIVER: documentation. Amend ADR 0007 and ADR 0011, ARCHITECTURE.md, README.md and the unreleased CHANGELOG.md with the frozen charge table, the guarded-reader boundary, the opaque numeric-conversion bound, bounded diagnostics, guarded collection construction and the no-duplicate-charge rule. Prose deliverables carry no behavior contract.

### `gate` — tasks 4.2, 4.3

- **shape**: serial after `settle` (shares `runtime`); seam `S7`; coder `zpatcher`
- **red / code**: none (waived) / ['4.2', '4.3']
- **sites**:
- **verify**: `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 2m -p 2 -parallel 2 ./core ./runtime`
- **seam summary**: NO-TESTER-WAIVER: verification and archive readiness. Not code seams - these run commands and record evidence.

### `perfgate` — tasks 4.4, 4.5

- **shape**: serial after `gate` (shares `runtime`); seam `S7b`; coder `zpatcher`
- **red / code**: none (waived) / ['4.4', '4.5']
- **sites**:
- **verify**: `make profile && go test -timeout 10m -run '^$' -bench . -benchmem ./internal/goldset/ && openspec validate reader-budget-enforcement --strict --json`
- **seam summary**: NO-TESTER-WAIVER: performance comparison and archive readiness; these run commands and record evidence.

**Floor**: `make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 2m -p 2 -parallel 2 ./core ./runtime && openspec validate reader-budget-enforcement --strict --json`

**Waivers.** `scratch`, `entries`, `settle`, `docs`, `gate` and `perfgate` carry no red stage;
each seam summary states its `NO-RED-WAIVER:` / `NO-TESTER-WAIVER:` reason. For the three that
still add test files, the chunk sites are the guard allowlist: only those files may be added or
changed, and every other test in the chunk's `pkgDirs` is pre-sealed before the coder runs and
guarded after.

**For an agent without the kernel.** Every path is worktree-absolute; every git call is
`git -C <worktree>`, never `cd`. A contract test once written is read-only — the only way to
change one is an AMEND naming the ruling that superseded it. Commits are conventional:
subject ≤72 chars, imperative, lowercase, type from `feat fix refactor perf docs test build ci
chore revert`; body lines ≤100. Never stage `openspec/` from a worktree.

## Plan appendix

```json
{
  "v": 2,
  "change": "reader-budget-enforcement",
  "baseSha": "75399eb685b40ddfa5159b6a76e576fd8469006a",
  "generatedAt": "2026-09-09T13:19:04.931354+00:00",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "arch",
    "perf",
    "sec"
  ],
  "chunks": [
    {
      "id": "construct",
      "taskIds": [
        "1.3",
        "2.4"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "S1",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "redTasks": [
        "1.3"
      ],
      "codeTasks": [
        "2.4"
      ],
      "redTests": [
        "TestGuardedRead_AdmitsOutputStorage",
        "TestGuardedRead_AdmittedBytesNeverDecrease",
        "TestGuardedRead_FailedConversionKeepsItsNodeCharge",
        "TestGuardedRead_WorkspaceHighWaterSpansNesting"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core && go vet ./core && golangci-lint run ./core/...",
      "coder": "go-coder",
      "sites": [
        {
          "task": "1.3",
          "file": "core/reader_budget_admission_test.go",
          "symbol": "TestGuardedRead_TokenPlanAdmission",
          "anchor": "func TestGuardedRead_TokenPlanAdmission",
          "new": false,
          "change": "already authored and committed (46db694, f68e4f2, amended 75399eb): the red stage AMENDS it to the frozen model \u2014 8 of the 11 rows in amendments[] land here, including the long-token case that is restructured rather than renumbered \u2014 adds the outputBytes helper, and does not re-author it. Listed as a site so guard admits the amendment instead of reading it as a sealed-test violation."
        },
        {
          "task": "1.3",
          "file": "core/reader_budget_accounting_test.go",
          "symbol": "TestGuardedRead_ExactCharges",
          "anchor": "func TestGuardedRead_ExactCharges",
          "change": "already authored and committed (e077766, amended 75399eb): the red stage AMENDS it to the frozen model per amendments[], adds the four new red tests, and does not re-author it",
          "new": false
        },
        {
          "task": "2.4",
          "file": "core/reader.go",
          "symbol": "Parser.parseList",
          "anchor": "func (p *Parser) parseList() (Value, error) {",
          "change": "supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[] Flat-copy shape: admit ValueSlotsBytes(n) for the destination BEFORE the copy, then copy in bounded slot batches; the same shape applies to parseVector and the map literal path."
        },
        {
          "task": "2.4",
          "file": "core/reader.go",
          "symbol": "Parser.parseVector / Parser.parseReaderVector",
          "anchor": "func (p *Parser) parseVector() (Value, error) {",
          "change": "Same flat-copy shape as parseList (and duplicated again in parseReaderVector, anchor `func (p *Parser) parseReaderVector() (Value, error) {`): admit ValueSlotsBytes(children) before `make`, then bounded copy. NewVector stays flat at every size, so no cell charge here. These three bodies are byte-identical apart from the closing token and constructor; keep them consistent."
        },
        {
          "task": "2.4",
          "file": "core/reader.go",
          "symbol": "Parser.parseHashMap",
          "anchor": "\t\terr = m.Set(key, val)",
          "change": "supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]"
        },
        {
          "task": "2.4",
          "file": "core/reader.go",
          "symbol": "readerScratch.read (top-level form buffer)",
          "anchor": "\tvar forms []Value",
          "change": "The top-level form buffer grows by bare append with no charge. Charge the logical value-slot growth schedule the sealed helper models \u2014 workBufferBytes in core/reader_budget_admission_test.go:32: an initial 1-slot buffer, then the whole doubled capacity charged before each growth, never discounted by retained physical capacity. flatFormBytes (same file:49) = workBufferBytes(1) + workBufferBytes(children) + ValueSlotsBytes(children), i.e. this buffer and Parser.nodes both follow that schedule."
        },
        {
          "task": "2.4",
          "file": "core/reader.go",
          "symbol": "Parser.nodes (parser node scratch)",
          "anchor": "\tnodes []Value",
          "change": "supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]"
        },
        {
          "task": "2.4",
          "file": "core/reader.go",
          "symbol": "Parser.wrapForm / Parser.addNode",
          "anchor": "func (p *Parser) wrapForm(sym string, form Value) (Value, error) {",
          "change": "supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]"
        },
        {
          "task": "2.4",
          "file": "core/types.go",
          "symbol": "newListChain / NewList / listFlatThreshold",
          "anchor": "func newListChain(items []Value) *listNode {",
          "change": "supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]"
        },
        {
          "task": "2.4",
          "file": "core/types.go",
          "symbol": "HashMap.Set (Go-map promotion branch)",
          "anchor": "\t\tm := make(map[hashKey]entry, len(h.entries)+1)",
          "change": "This is the branch the guarded builder must never reach. Do not change Set, Assoc, hashMapSmallLimit=8, hashOfKey, the entry layout, or the observable ordering/equality contracts; add the private guarded construction beside them. Reader-owned nodes are unpublished until the read succeeds, so the private builder may mutate its own nodes, never a shared one. core/hashmap_test.go:414 already asserts the trie form for the Assoc path \u2014 do not weaken it."
        },
        {
          "task": "2.4",
          "file": "core/types.go",
          "symbol": "hamtNode.assoc / hamtNodeBytes / hashOfKey / HashMap.find",
          "anchor": "func (n *hamtNode) assoc(e entry, h uint32, shift uint) (*hamtNode, int64, bool) {",
          "change": "Geometry to reuse verbatim: vecBits/vecBranch shift math, compacted entries/children, mergeEntries collision fallback past shift 32, and hamtNodeBytes = MeterCollectionHeaderBytes + len(entries)*MeterHashMapEntryBytes + len(children)*MeterTrieChildBytes (`func hamtNodeBytes(n *hamtNode) int64 {`). The guarded builder charges: hashOfKey per key byte (`func hashOfKey(hk hashKey) uint32 {` iterates hk.str), collision-scan and key comparisons (`func (h *HashMap) find(hk hashKey) (int, bool) {` is the small-form linear scan), and hamtNodeBytes for every node it allocates, exact-size copies (core/types.go:827,832), never checked doubling \u2014 checked doubling applies only to variable collision buffers. Sealed floors (reader_budget_accounting_test.go:106): small-map 4 -> 4*MeterHashMapEntryBytes, 8 -> 8*, promoted 9 -> MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes, 12 duplicate keys -> at least one MeterHashMapEntryBytes."
        },
        {
          "task": "2.4",
          "file": "core/metering.go",
          "symbol": "EvalMeter.creditAllocBytes",
          "anchor": "func (m EvalMeter) creditAllocBytes",
          "new": false,
          "change": "DELETE EvalMeter.creditAllocBytes and evalState.creditAllocBytes. They are the only writers that move a meter counter down and sit outside the addCharge/saturateCounter/publishedTotal wrap-safety proof."
        },
        {
          "task": "2.4",
          "file": "core/reader_budget.go",
          "symbol": "readerBudget.creditAlloc",
          "anchor": "func (b *readerBudget) creditAlloc",
          "new": false,
          "change": "DELETE readerBudget.creditAlloc; add readerListCellBytes and the growthPlan doubling helper. addNode skips the payload term when tok.copied is set instead of crediting after the fact."
        }
      ],
      "contract": {
        "states": [
          "unadmitted",
          "plan-reserved",
          "node-admitted",
          "workspace-admitted",
          "construction-admitted",
          "built",
          "refused",
          "sealed-old-model",
          "amended-frozen-model"
        ],
        "transitions": [
          {
            "input": "a sealed want whose terms omit output storage",
            "state": "sealed-old-model -> amended-frozen-model",
            "action": "set",
            "evidence": "chargeModel.sealedReconciliation.amend, 11 rows"
          },
          {
            "input": "a sealed want that is a '>=' floor",
            "state": "sealed-old-model",
            "action": "no-op",
            "evidence": "totals only grow under this model"
          },
          {
            "input": "a sealed want derived from admittedForSource",
            "state": "sealed-old-model",
            "action": "no-op",
            "evidence": "self-referential, model-independent"
          },
          {
            "input": "the long-token case reading 8000 bytes under a 1024 ceiling",
            "state": "sealed-old-model -> amended-frozen-model",
            "action": "forced",
            "evidence": "blocker B2 - restructure, ceiling and shape both change"
          },
          {
            "input": "the credits-the-copy-when-the-node-lands case",
            "state": "sealed-old-model -> amended-frozen-model",
            "action": "forced",
            "evidence": "creditRuling - the credit path is deleted, so the case name is false"
          },
          {
            "input": "counting reaches token n, plan+payload <= headroom",
            "state": "unadmitted",
            "action": "no-op",
            "evidence": "core/reader.go:201, core/reader_budget.go:209-224 - checkPlan charges nothing"
          },
          {
            "input": "counting reaches token n, plan+payload > headroom",
            "state": "unadmitted -> refused",
            "action": "forced",
            "evidence": "core/reader_budget.go:218-223"
          },
          {
            "input": "counting completes",
            "state": "unadmitted -> plan-reserved",
            "action": "set (32*tokens, then payload)",
            "evidence": "core/reader.go:159, core/reader_budget.go:230-248"
          },
          {
            "input": "addNode for a node with an aliased or absent payload",
            "state": "plan-reserved -> node-admitted",
            "action": "set (MeterReaderNodeBytes + payload)",
            "evidence": "core/reader.go:562-568; spec core-engine:120-122"
          },
          {
            "input": "addNode for a String node whose token has copied == true",
            "state": "plan-reserved -> node-admitted",
            "action": "set (MeterReaderNodeBytes only)",
            "evidence": "core/reader.go:642; spec core-engine:120-122 'SHALL NOT be charged again'"
          },
          {
            "input": "addNode for a synthetic quote/quasiquote/unquote/function node",
            "state": "plan-reserved -> node-admitted",
            "action": "set (two nodes: 32+len(sym), then 32+0)",
            "evidence": "core/reader.go:790-798"
          },
          {
            "input": "addNode whose admitAlloc is refused",
            "state": "-> refused",
            "action": "forced",
            "evidence": "core/reader_budget.go:163-175"
          },
          {
            "input": "Parser.nodes append needs capacity beyond the logical capacity",
            "state": "-> workspace-admitted",
            "action": "set (whole new logical capacity, unit 16)",
            "evidence": "design.md:139-145; workBufferBytes at admission_test.go:32-43"
          },
          {
            "input": "Parser.nodes append fits the logical capacity",
            "state": "workspace-admitted",
            "action": "no-op",
            "evidence": "the schedule charges on growth only"
          },
          {
            "input": "top-level forms append needs capacity beyond the logical capacity",
            "state": "-> workspace-admitted",
            "action": "set (unit 16)",
            "evidence": "core/reader.go:546-553"
          },
          {
            "input": "a nested collection appends children while an outer one holds a mark",
            "state": "workspace-admitted",
            "action": "set only if the SHARED high-water grows",
            "evidence": "core/reader.go:471, 691-700 - one schedule per read, not per collection"
          },
          {
            "input": "flat collection copy-out of n children",
            "state": "-> construction-admitted",
            "action": "set (ValueSlotsBytes(n))",
            "evidence": "core/reader.go:707, 739, 827"
          },
          {
            "input": "list finalization with n > 32 children",
            "state": "-> construction-admitted",
            "action": "set (readerListCellBytes*n) then link with a checkpoint every 128 slots",
            "evidence": "core/types.go:222; design.md:91"
          },
          {
            "input": "list finalization with n <= 32 children",
            "state": "construction-admitted",
            "action": "no-op (flat, no cells)",
            "evidence": "core/types.go:214"
          },
          {
            "input": "map insert of a new key needing entry-buffer growth",
            "state": "-> construction-admitted",
            "action": "set (whole new logical capacity, unit 64)",
            "evidence": "core/types.go Set small branch"
          },
          {
            "input": "map insert of a key already present",
            "state": "construction-admitted",
            "action": "no-op",
            "evidence": "core/types.go find -> found branch rewrites in place"
          },
          {
            "input": "map insert making the 9th distinct key (len(entries) >= hashMapSmallLimit)",
            "state": "-> construction-admitted",
            "action": "forced (build directly into the trie, never the Go-map branch)",
            "evidence": "core/types.go:1213, 1050-1062; sealed TestGuardedRead_MapConstructionContracts asserts m.large.root != nil && m.large.m == nil"
          },
          {
            "input": "trie assoc allocating node out",
            "state": "-> construction-admitted",
            "action": "set (hamtNodeBytes(out), admitted BEFORE out is allocated)",
            "evidence": "core/types.go:751-755, 823-869"
          },
          {
            "input": "numeric token",
            "state": "node-admitted -> construction-admitted",
            "action": "set (2n+256)",
            "evidence": "core/reader.go:885, core/reader_budget.go:254-269"
          },
          {
            "input": "a conversion that fails after its node and storage were admitted",
            "state": "construction-admitted",
            "action": "no-op (no credit)",
            "evidence": "worked total for (a b c) 1.2.3 = 829"
          },
          {
            "input": "any admit while b.failed != nil",
            "state": "refused",
            "action": "no-op (returns the sticky error)",
            "evidence": "core/reader_budget.go:167-169, 110-115"
          },
          {
            "input": "a read with a nil budget (the context-free reader)",
            "state": "unadmitted",
            "action": "no-op at every guarded site",
            "evidence": "core/reader_budget.go:34-35 - a nil *readerBudget is the absent budget"
          }
        ],
        "forbidden": [
          "any Go allocation reaching state built before the matching admit returned nil",
          "any decrease of EvalMeterSnapshot().AllocationBytes during a read - this is the invariant that removing creditAllocBytes protects",
          "charging an escaped payload both at reservePlan and at addNode",
          "the guarded map builder reaching h.large.m != nil",
          "mutating any HashMap, List or hamtNode that has been published outside the read",
          "changing ReaderStats.Nodes or ReaderStats.Bytes for any source",
          "a refused budget admitting anything afterwards - b.failed is sticky",
          "leaving EvalMeter.creditAllocBytes, evalState.creditAllocBytes or readerBudget.creditAlloc in the tree"
        ],
        "seeding": "Dialect.ReadWithContextStats(ctx, src, maxDepth) is the only public path. readOwnedScratch drives a caller-owned readerScratch for reuse cases. Reach refused only by lowering the ceiling through allocCeilingContext; never by writing b.failed. Reach the trie form only through a source with 9 or more distinct keys; never by constructing a largeMap directly.",
        "budgets": {
          "checkpoint interval": 128,
          "byte copy batch": 128,
          "slot copy/link batch": 128,
          "list flat threshold": 32,
          "small-map limit": 8,
          "trie fan-out": 32,
          "trie shift bits": 5,
          "node unit": 32,
          "value slot unit": 16,
          "entry unit": 64,
          "trie child unit": 8,
          "collection header": 24,
          "list cell": 32,
          "token unit": 32,
          "conversion slack": 256
        },
        "names": [
          "readerListCellBytes",
          "readerBudget.admitAlloc",
          "readerBudget.admitOutputNode",
          "readerBudget.admitSlots",
          "growthPlan",
          "growthPlan.admit",
          "Parser.addNode",
          "Parser.buildList",
          "Parser.mapSet",
          "hamtNode.assocGuarded",
          "hamtNodeBytes",
          "MeterReaderNodeBytes",
          "MeterValueSlotBytes",
          "MeterHashMapEntryBytes",
          "MeterCollectionHeaderBytes",
          "MeterTrieChildBytes",
          "ValueSlotsBytes",
          "listFlatThreshold",
          "hashMapSmallLimit",
          "NewResourceLimitError",
          "CodeResourceLimit"
        ],
        "refusals": [
          {
            "failure": "storage over the ceiling",
            "refusedBy": "readerBudget.admitAlloc -> EvalMeter.ChargeAllocBytes -> evalState.chargeAllocBytes",
            "when": "before the allocation it covers",
            "surface": "*LispicoError, Code CodeResourceLimit"
          },
          {
            "failure": "overflowing token plan",
            "refusedBy": "readerBudget.reservePlan via checkedTokenPlanBytes",
            "when": "after counting, before tokenizeInto allocates",
            "surface": "*LispicoError, Code CodeResourceLimit"
          },
          {
            "failure": "cancellation or deadline",
            "refusedBy": "readerBudget.checkpoint",
            "when": "every <=128 work units and on every return",
            "surface": "context.Canceled or context.DeadlineExceeded, outranking any pending syntax error via readerBudget.settle"
          }
        ]
      }
    },
    {
      "id": "scratch",
      "taskIds": [
        "2.5"
      ],
      "prev": "construct",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "S2",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "redTasks": [],
      "codeTasks": [
        "2.5"
      ],
      "redTests": [
        "TestGuardedRead_ScratchReleaseDropsOversizedCapacity",
        "TestGuardedRead_FailedReadReleasesScratch",
        "TestGuardedRead_RetainedASTSurvivesScratchReuse",
        "TestGuardedRead_ConcurrentCrossDialectReads"
      ],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core && go test -race -timeout 2m -p 2 -parallel 2 ./core && golangci-lint run ./core/...",
      "coder": "go-coder",
      "sites": [
        {
          "task": "2.5",
          "file": "core/reader.go",
          "symbol": "readerScratch.Reset",
          "anchor": "func (s *readerScratch) Reset() {",
          "change": "Extend to clear every reference-bearing slot used by this or a previous read, in bounded batches: reader.input (currently NOT cleared \u2014 it is only overwritten at the next checkout, so a returned scratch pins the whole source string), the tail of s.tokens beyond the new length (token.val strings), and the tail of parser.nodes and parser.tokens. Add the discard rule the spec requires: drop retained buffers whose modeled logical capacity exceeds the current read's allocation ceiling instead of returning them to the pool, and on terminal failure discard rather than traverse. Note TestReaderScratch_ResetClearsAllFields (core/reader_pool_test.go:36) is hand-written, not reflective \u2014 it will not fail on a field you forget to clear."
        },
        {
          "task": "2.5",
          "file": "core/reader.go",
          "symbol": "readerScratchPool / Dialect.ReadWithContextStats put-back",
          "anchor": "var readerScratchPool = sync.Pool{",
          "change": "The pool is shared across every Dialect and both entry points; the guarded put-back is `defer func() { s.budget = nil; readerScratchPool.Put(s) }()` in core/dialect.go (anchor `func (d Dialect) ReadWithContextStats(ctx context.Context, src string, maxDepth int) ([]Value, ReaderStats, error) {`). Route the put-back through the new clear/discard decision so an oversized buffer is not returned. Preserve TestReaderScratch_RetainedTreeSurvivesReuse and TestReadWithMaxDepthStats_PooledNoEscapeStringSharesBackingArray: returned ASTs keep aliasing the caller's source string, so clearing must not touch storage a returned value owns."
        },
        {
          "task": "2.5",
          "file": "core/reader_scratch_release_test.go",
          "symbol": "TestGuardedRead_FailedReadReleasesScratch",
          "anchor": "",
          "change": "new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "retained",
          "cleared",
          "discarded",
          "independent"
        ],
        "transitions": [
          {
            "input": "successful read returning scratch to the pool",
            "state": "retained -> cleared",
            "action": "set (clear reader.input and every reference-bearing slot in s.tokens and s.parser.nodes, in batches of at most 128 slots)",
            "evidence": "design.md:154-159"
          },
          {
            "input": "terminal failure returning scratch",
            "state": "retained -> discarded",
            "action": "forced (drop the buffer rather than traverse input-sized storage to clear it)",
            "evidence": "design.md:157-159"
          },
          {
            "input": "retained logical capacity exceeds the current read's allocation ceiling",
            "state": "retained -> discarded",
            "action": "forced (must not go back in the pool)",
            "evidence": "spec core-engine:158-163"
          },
          {
            "input": "retained capacity within the ceiling on a successful read",
            "state": "cleared",
            "action": "no-op (capacity kept, charge still paid on the logical schedule)",
            "evidence": "sealed TestGuardedRead_PoolReuseChargesTheSameConstruction"
          },
          {
            "input": "a value tree returned by an earlier read",
            "state": "independent",
            "action": "no-op (never touched)",
            "evidence": "spec core-engine:163-164"
          },
          {
            "input": "clearing or discarding reference-bearing scratch slots on release",
            "state": "cleared",
            "action": "set (one work unit per cleared slot, in batches of at most 128, charged to the read being released)",
            "evidence": "spec core-engine: one unit per visited, linked, compared, cleared or copied slot"
          }
        ],
        "forbidden": [
          "clearing or reslicing storage that backs a returned AST",
          "resetting evaluation charges during release",
          "a low-budget read inheriting either a larger allowance or high-water storage from a previous read"
        ],
        "seeding": "call readerScratch.release(ceiling) directly on a test-owned scratch, or drive Dialect.ReadWithContextStats whose defer calls it. Never null out fields by hand to simulate a release.",
        "budgets": {
          "clear batch": 128,
          "checkpoint interval": 128
        },
        "names": [
          "readerScratch.release",
          "readerScratch.Reset",
          "readerScratchPool",
          "readerBudget.allocHeadroom"
        ],
        "refusals": [
          {
            "failure": "over-ceiling retained capacity",
            "refusedBy": "readerScratch.release",
            "when": "before the scratch returns to readerScratchPool",
            "surface": "none - the buffer is dropped, not an error"
          }
        ]
      }
    },
    {
      "id": "routing",
      "taskIds": [
        "0.2",
        "3.1"
      ],
      "prev": "scratch",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "S3",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./core",
        "./runtime"
      ],
      "redTasks": [
        "0.2"
      ],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [
        "TestEval_ReaderBudget_Characterization"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && golangci-lint run ./core/... ./runtime/...",
      "coder": "go-coder",
      "sites": [
        {
          "task": "0.2",
          "file": "runtime/eval_reader_budget_test.go",
          "symbol": "TestEval_ReaderBudget_Characterization",
          "anchor": "",
          "change": "File does not exist on the branch: it was deleted by commit 4ecc086 (\"test: defer reader budget characterization to routing chunk\") to return with the routing chunk. Restore it verbatim from `git show 4ecc086^:runtime/eval_reader_budget_test.go` (111 lines, package runtime, testify) alongside task 3.1 and let it go green. It defines readerBudgetBytes=1<<10, readerBudgetReductions=1_000_000, wideQuotedList, newReaderBudgetEngine, readerBudgetCtx, admittedBytes(ctx), legacyReaderBytes and four subtests: refuses_before_the_full_parse, unread_suffix_does_not_widen_admission, cancelled_context_rejects_comment_only_source, cancellation_precedes_syntax_error. It reuses meteringLimits + isResourceLimit from runtime/resource_limits_test.go. (new file: no anchor at baseSha)",
          "new": true
        },
        {
          "task": "3.1",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.readForms",
          "anchor": "\tforms, stats, err := e.config.dialect.ReadWithMaxDepthStats(input, e.config.limits.MaxReaderDepth)",
          "change": "Switch to e.config.dialect.ReadWithContextStats(ctx, input, e.config.limits.MaxReaderDepth) and delete the following `if err := core.ChargeEvalReader(ctx, stats); err != nil {` block (the duplicate post-parse output charge). Three call sites, all named below. Note core.ChargeEvalReader itself stays exported for legacy/host callers."
        },
        {
          "task": "3.1",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.Eval (deadline arming order)",
          "anchor": "\tif d := e.evalDeadline(ctx, start); !d.IsZero() && core.EvalDeadlineFrom(ctx).IsZero() {",
          "change": "This block currently runs AFTER `forms, err := e.readForms(ctx, input)`, so a guarded read in Eval sees no engine deadline. Move it above the readForms call, matching evalWithBindingScope, which already arms with `ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))` before reading. Keep StartEval/leased before the read so one ledger spans read, compile and execute."
        },
        {
          "task": "3.1",
          "file": "runtime/watch.go",
          "symbol": "fileWatcher.reloadFile",
          "anchor": "\tctx := w.engine.evalResourceContext(core.DetachEvalState(w.ctx))",
          "change": "The reload context carries a meter and limits but no deadline and no StartEval lease. Arm the engine deadline on this detached context before `forms, err := w.engine.readForms(ctx, string(content))` so background reloads are interruptible; the existing early return on a read error already prevents childEnv.MergeInto, which is the no-replacement-bindings guarantee task 3.2 asserts."
        },
        {
          "task": "0.2",
          "file": "runtime/eval_reader_budget_test.go",
          "symbol": "TestEval_ReaderBudget_Characterization",
          "anchor": "",
          "change": "new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "legacy-read",
          "guarded-read",
          "double-charged",
          "deadline-armed",
          "deadline-absent"
        ],
        "transitions": [
          {
            "input": "engineImpl.readForms",
            "state": "legacy-read -> guarded-read",
            "action": "set (ReadWithMaxDepthStats -> ReadWithContextStats(ctx, ...))",
            "evidence": "runtime/eval.go:653-662; design.md:35-38"
          },
          {
            "input": "the ChargeEvalReader call after a successful parse",
            "state": "double-charged -> guarded-read",
            "action": "clear",
            "evidence": "runtime/eval.go:658; spec runtime-api:14-15 'already-accounted reader output SHALL NOT receive a second post-parse charge'"
          },
          {
            "input": "Eval entering with no armed deadline",
            "state": "deadline-absent -> deadline-armed",
            "action": "forced (move the WithEvalDeadline at runtime/eval.go:744-746 to BEFORE the readForms call at :735)",
            "evidence": "design.md:40-41"
          },
          {
            "input": "evalWithBindingScope (EvalWithBindings, LoadScope)",
            "state": "deadline-armed",
            "action": "no-op (already arms at runtime/eval.go:1091, before readForms at :1122)",
            "evidence": "runtime/eval.go:1090-1122"
          },
          {
            "input": "fileWatcher.reloadFile",
            "state": "deadline-absent -> deadline-armed",
            "action": "set (one detached evaluation context carrying the engine deadline for the whole read/evaluate/merge lifecycle)",
            "evidence": "runtime/watch.go:108-109; design.md:42-43"
          },
          {
            "input": "a read failing under the budget",
            "state": "guarded-read",
            "action": "forced (no form executes, no bindings publish)",
            "evidence": "spec runtime-api:77-80"
          },
          {
            "input": "a direct caller of Dialect.ReadWithContextStats outside runtime",
            "state": "guarded-read",
            "action": "no-op (output storage is admitted by the reader itself, so the public entry point is complete on its own)",
            "evidence": "chargeModel term T4"
          },
          {
            "input": "a legacy context-free caller (Read, ReadOne, ReadWithMaxDepthStats)",
            "state": "legacy-read",
            "action": "no-op (depth-only contract retained)",
            "evidence": "spec core-engine:11-12"
          },
          {
            "input": "an existing runtime metering golden or threshold that included the post-parse reader output charge",
            "state": "legacy-read",
            "action": "forced (re-check and restate it: removing ChargeEvalReader moves every number that included reader output)",
            "evidence": "design risks R2, R7; ADR 0008 gold set"
          }
        ],
        "forbidden": [
          "leaving ChargeEvalReader on the guarded path - that is the double charge",
          "removing ChargeEvalReader without S1 having landed - that is the resource-safety hole this whole plan exists to prevent",
          "resetting the ledger, the meter or the deadline between read and execute",
          "installing a fresh deadline for the second scan pass",
          "arming a deadline that overrides one the caller already supplied"
        ],
        "seeding": "Engine.Eval / EvalWithBindings / LoadScope / the watcher's reload path, with a context built by core.WithEvalResourceLimits and optionally core.WithEvalDeadline. Never call ReadWithContextStats from a runtime test to stand in for an entry point.",
        "budgets": {
          "characterization ceiling": 1024,
          "characterization reductions": 1000000,
          "checkpoint interval": 128
        },
        "names": [
          "engineImpl.readForms",
          "Dialect.ReadWithContextStats",
          "core.ChargeEvalReader",
          "core.WithEvalDeadline",
          "core.EvalDeadlineFrom",
          "core.DetachEvalState",
          "engineImpl.evalDeadline",
          "engineImpl.evalResourceContext",
          "fileWatcher.reloadFile",
          "readerBudgetBytes",
          "readerBudgetReductions",
          "TestEval_ReaderBudget_Characterization"
        ],
        "refusals": [
          {
            "failure": "over-budget source",
            "refusedBy": "the reader, inside readForms",
            "when": "before the first form evaluates",
            "surface": "*LispicoError Code CodeResourceLimit, wrapped by runtime/eval.go:737 as \"read: %w\""
          },
          {
            "failure": "cancelled context, including comment-only and empty source",
            "refusedBy": "readerBudget.checkpoint at read entry",
            "when": "before any source byte is scanned",
            "surface": "context.Canceled, errors.Is-matchable through the read: wrap"
          }
        ]
      }
    },
    {
      "id": "entries",
      "taskIds": [
        "3.2"
      ],
      "prev": "routing",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "S4",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "redTasks": [],
      "codeTasks": [
        "3.2"
      ],
      "redTests": [
        "TestPublicEntries_ReaderBudget"
      ],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && golangci-lint run ./runtime/...",
      "coder": "go-coder",
      "sites": [
        {
          "task": "3.2",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.Eval / EvalWithBindings / LoadScope",
          "anchor": "func (e *engineImpl) evalWithBindingScope(ctx context.Context, source string, bindings map[string]core.Value) (result core.Value, childEnv *core.Env, err error) {",
          "change": "Public entry tests (new runtime _test.go). EvalWithBindings and LoadScope both funnel through evalWithBindingScope (`forms, err := e.readForms(ctx, source)`); Eval reads at `forms, err := e.readForms(ctx, input)`. Cover both evaluator modes (WithBytecode / WithTreeWalker, last-wins), both dialect reader surfaces (cl.Dialect(), clojure.Dialect(), plus WithReaderVector/WithFunctionRef), context meter vs engine meter (WithMeter / engineMeter), and assert an over-budget source executes no forms. Existing builders to reuse: newLimitsEngine, meteringLimits, evalLimits, isResourceLimit, evalModeName (runtime/resource_limits_test.go)."
        },
        {
          "task": "3.2",
          "file": "runtime/watch.go",
          "symbol": "fileWatcher.reloadFile (hot-reload publication)",
          "anchor": "\tif err := childEnv.MergeInto(w.engine.rootEnv); err != nil {",
          "change": "Assert a failed reload publishes no replacement bindings: the read error path returns before MergeInto. Existing precedent tests to extend: TestReloadFile_SyntaxErrorKeepsOldDefinitions (runtime/watch_test.go:115), TestReloadFile_EvalErrorKeepsOldDefinitions (:143), TestReloadFile_PanicSurfacesErrorAndKeepsOldDefinitions (:171) \u2014 all drive reloadFile directly rather than through the ticker, which keeps the test deterministic."
        },
        {
          "task": "3.2",
          "file": "runtime/eval_reader_entry_test.go",
          "symbol": "TestPublicEntries_ReaderBudget",
          "anchor": "",
          "change": "new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "executed",
          "refused-before-execution",
          "published",
          "unpublished"
        ],
        "transitions": [
          {
            "input": "over-budget source through any of the four entry points",
            "state": "-> refused-before-execution",
            "action": "forced",
            "evidence": "spec runtime-api:77-80"
          },
          {
            "input": "a failed hot reload",
            "state": "-> unpublished",
            "action": "forced (no replacement bindings merge into rootEnv)",
            "evidence": "runtime/watch.go:139"
          },
          {
            "input": "in-budget source",
            "state": "-> executed / published",
            "action": "set",
            "evidence": "existing entry-point contracts"
          },
          {
            "input": "bytecode evaluator vs WithTreeWalker",
            "state": "executed",
            "action": "no-op (reading precedes evaluator selection, so both modes see the identical reader charge)",
            "evidence": "runtime/eval.go:748-768"
          },
          {
            "input": "an inherited caller deadline vs an engine-derived one vs a disabled timeout",
            "state": "executed",
            "action": "no-op (arming only ever fills an absent deadline)",
            "evidence": "runtime/eval.go:744-746, 982-988"
          }
        ],
        "forbidden": [
          "a partial form sequence executing after a failed read",
          "a reload publishing bindings from a failed read",
          "a test asserting reader charges through a private core path instead of a public entry point"
        ],
        "seeding": "Engine.Eval / EvalWithBindings / LoadScope, and the watcher driven through its own reload path. Meters via core.WithEvalResourceLimits (context) or WithResourceLimits/engineMeter (engine).",
        "budgets": {
          "test ceiling": 1024,
          "checkpoint interval": 128
        },
        "names": [
          "Engine.Eval",
          "Engine.EvalWithBindings",
          "Engine.LoadScope",
          "WithBytecode",
          "WithTreeWalker",
          "WithDialect",
          "WithResourceLimits",
          "meteringLimits",
          "isResourceLimit"
        ],
        "refusals": [
          {
            "failure": "over-budget source at any entry point",
            "refusedBy": "readForms",
            "when": "before the first form evaluates or any binding publishes",
            "surface": "*LispicoError Code CodeResourceLimit"
          }
        ]
      }
    },
    {
      "id": "settle",
      "taskIds": [
        "3.3"
      ],
      "prev": "entries",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "S5",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "redTasks": [],
      "codeTasks": [
        "3.3"
      ],
      "redTests": [
        "TestGuardedRead_SingleSettledOutcome"
      ],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && golangci-lint run ./runtime/...",
      "coder": "go-coder",
      "sites": [
        {
          "task": "3.3",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.Eval / evalWithBindingScope settlement defer",
          "anchor": "func (e *engineImpl) Eval(ctx context.Context, source, input string) (result core.Value, err error) {",
          "change": "supersedes its pre-freeze note: the frozen charge model in chargeModel governs this site, and the sealed equalities it used to cite are amended by amendments[]"
        },
        {
          "task": "3.3",
          "file": "runtime/settlement_observation_test.go",
          "symbol": "runSettlementOutcomes / assertSettledOnce / settlementProbe",
          "anchor": "func runSettlementOutcomes(t *testing.T, entry string, invoke settlementInvoke, extra func(t *testing.T, tc settlementOutcome, scope *core.Env)) {",
          "change": "The predecessors' (metered-call-deadlines, evaluation-outcome-settlement) lifecycle harness \u2014 extend it with a reader-failure outcome rather than building a second one. settlementOutcomes()/outcomeEntries() enumerate the outcome cases, assertSettledOnce pins one event + one stats increment + one error, and TestEval_TerminalSettlementErrorWinsOverEvalError / TestEval_TerminalSettlementErrorWinsOverRecoveredPanic pin precedence."
        },
        {
          "task": "3.3",
          "file": "runtime/eval_reader_settlement_test.go",
          "symbol": "TestGuardedRead_SingleSettledOutcome",
          "anchor": "",
          "change": "new file (tagged new: no anchor at baseSha) (new file: no anchor at baseSha)",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "settled-once",
          "settled-twice",
          "unsettled"
        ],
        "transitions": [
          {
            "input": "a read failing under the budget",
            "state": "-> settled-once",
            "action": "set (returned error, EvalEvent and error statistics all describe the same failure)",
            "evidence": "spec runtime-api:87-90; runtime/eval.go:710-727"
          },
          {
            "input": "a cancelled empty or comment-only read",
            "state": "-> settled-once",
            "action": "set",
            "evidence": "spec runtime-api:68; core/reader_budget.go:57-80 checks even with zero pending work"
          },
          {
            "input": "a meter denying the reader charge",
            "state": "-> settled-once",
            "action": "set",
            "evidence": "runtime/eval.go:715-720 FinishEval is the single settlement point"
          },
          {
            "input": "a host meter panicking during settlement",
            "state": "-> settled-once",
            "action": "forced (recovered, lease returned, reported as the settlement error)",
            "evidence": "core/metering.go:352-370"
          },
          {
            "input": "the watcher's reload path",
            "state": "settled-once",
            "action": "no-op (it logs; EvalEvent is not part of its contract)",
            "evidence": "runtime/watch.go:110-113 - 'where those events are part of the entry point's contract', spec runtime-api:70-71"
          }
        ],
        "forbidden": [
          "two settlements for one read failure",
          "a reader failure bypassing FinishEval",
          "regressing the panic containment landed by evaluation-outcome-settlement (commits df156e7, ec03ad5)"
        ],
        "seeding": "the public entry points under a ceiling low enough to refuse the read, plus a host meter stub that denies or panics.",
        "budgets": {
          "settlement count": 1
        },
        "names": [
          "core.StartEval",
          "core.FinishEval",
          "core.FlushEvalState",
          "core.IsTerminalEvalError",
          "core.NewPanicError",
          "EvalEvent",
          "engineImpl.fireEvalCallbacks",
          "engineImpl.stats.recordEval"
        ],
        "refusals": [
          {
            "failure": "reader meter denial",
            "refusedBy": "evalState.chargeAllocBytes then finishEval",
            "when": "once, at the single settlement point",
            "surface": "*LispicoError Code CodeResourceLimit, reported identically to caller, event and stats"
          }
        ]
      }
    },
    {
      "id": "docs",
      "taskIds": [
        "4.1"
      ],
      "prev": "construct",
      "sharedPkg": null,
      "parallel": true,
      "seam": "S6",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "redTasks": [],
      "codeTasks": [
        "4.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go build ./...",
      "coder": "zdocs",
      "sites": [
        {
          "task": "4.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Fixed size table / Charge sites",
          "anchor": "- reader output, charged immediately after `Read` and before the first form runs;",
          "change": "This bullet is the statement task 3.1 invalidates for the guarded path. Amend the charge-site list and the fixed size table (anchor `| Reader node | 32 bytes per parsed node |`) with the guarded-reader rows the change introduces: 32-byte token plan unit, decoded-payload reservation and the skip-at-source rule (addNode omits the payload term when tok.copied is set), 2*tokenBytes+256 numeric-conversion storage, 32 bytes per linked list cell, the value-slot workspace growth schedule, and the no-duplicate-charge rule. Also state the opaque numeric-conversion bound (token <= MaxReductions/3) and the 128-byte bounded diagnostic. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns."
        },
        {
          "task": "4.1",
          "file": "docs/adr/0007-resource-limits.md",
          "symbol": "Consequences",
          "anchor": "## Consequences",
          "change": "Record that the reader is no longer depth-only: the guarded entry point observes ctx cancellation, the engine deadline and the allocation ledger, while the legacy context-free reader (core.Read / ReadOne / Dialect.Read / ReadWithMaxDepth[Stats]) keeps its depth-only contract, and filesystem source acquisition stays outside the boundary. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns."
        },
        {
          "task": "4.1",
          "file": "ARCHITECTURE.md",
          "symbol": "Resource Limits / Reader",
          "anchor": "Reader output is also charged into the evaluation's allocation ledger before the",
          "change": "Rewrite this sentence for the guarded boundary (the reader now admits its own storage during the read, no separate post-parse charge on the runtime path) and add the guarded-reader description to the `#### Reader` section: two scan passes, both charged per byte; token plan admitted during counting; guarded collection construction; bounded diagnostics. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns."
        },
        {
          "task": "4.1",
          "file": "README.md",
          "symbol": "Resource limits",
          "anchor": "the fixed deterministic size table in ADR 0011. Reader output is charged",
          "change": "Same correction in the user-facing table section (anchor `## Resource limits` for the section itself): the deterministic work/storage table, the numeric-conversion exception, and that some previously accepted sources are refused under tight budgets. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns."
        },
        {
          "task": "4.1",
          "file": "CHANGELOG.md",
          "symbol": "Unreleased",
          "anchor": "## [Unreleased]",
          "change": "Add the entry under the existing `### Added` / `### Changed` subsections: new Dialect.ReadWithContextStats; runtime source paths now read under the evaluation ledger and deadline; reader charges changed, so tightly budgeted sources can now be refused during reading. | The credit path is DELETED by chunk construct: document skip-at-source, never a credit term. No CHANGELOG line may name a unit no ADR table row owns."
        }
      ],
      "contract": {
        "states": [
          "documented",
          "undocumented"
        ],
        "transitions": [
          {
            "input": "each of the six charge terms T1-T6",
            "state": "undocumented -> documented",
            "action": "set (one table row with its unit, its admission moment and its owner)",
            "evidence": "chargeModel.terms"
          },
          {
            "input": "the legacy context-free reader boundary",
            "state": "undocumented -> documented",
            "action": "set",
            "evidence": "spec core-engine:11-12; design.md:184-186"
          },
          {
            "input": "the post-parse-only safety claim in ADR 0007/0011",
            "state": "documented -> documented",
            "action": "forced (it is now false and must be replaced, not appended to)",
            "evidence": "proposal.md:43-47"
          },
          {
            "input": "the readerListCellBytes 32 vs List.Cons ListShallowBytes(1) 40 divergence",
            "state": "undocumented -> documented",
            "action": "set (two units for one listNode, with the reason each owner uses its own)",
            "evidence": "risk R3"
          },
          {
            "input": "a stated guarantee with no scenario behind it",
            "state": "undocumented -> documented",
            "action": "forced (drop the guarantee or add the scenario)",
            "evidence": "task 4.1"
          }
        ],
        "forbidden": [
          "publishing a unit in CHANGELOG.md that no ADR table row owns - the standing MeterFusedOpBytes defect",
          "weakening the ADR 0008 gate thresholds"
        ],
        "seeding": "n/a - prose",
        "budgets": {
          "documented charge terms": 6,
          "diagnostic render limit": 128,
          "checkpoint interval": 128
        },
        "names": [
          "docs/adr/0007",
          "docs/adr/0011",
          "ARCHITECTURE.md",
          "README.md",
          "CHANGELOG.md"
        ],
        "refusals": []
      }
    },
    {
      "id": "gate",
      "taskIds": [
        "4.2",
        "4.3"
      ],
      "prev": "settle",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "S7",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "redTasks": [],
      "codeTasks": [
        "4.2",
        "4.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 2m -p 2 -parallel 2 ./core ./runtime",
      "coder": "zpatcher",
      "sites": [],
      "contract": {
        "states": [
          "unrun",
          "green",
          "red"
        ],
        "transitions": [
          {
            "input": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime",
            "state": "unrun -> green",
            "action": "set (reader, lifecycle, statistics and pool regressions pass)",
            "evidence": "task 4.2"
          },
          {
            "input": "make build, make lint, make test, then the targeted -race run",
            "state": "unrun -> green",
            "action": "set",
            "evidence": "task 4.3"
          },
          {
            "input": "an untracked bin/ tree in the checkout",
            "state": "unrun",
            "action": "no-op (the floor runs in a worktree, which has no bin/; go list errors 7x in the primary and 0x in the worktree)",
            "evidence": "orchestrator verification at 75399eb"
          }
        ],
        "forbidden": [
          "narrowing the wrapper scope to make it pass"
        ],
        "seeding": {
          "worktree": "a clean worktree checkout"
        },
        "budgets": {
          "unit": "2m",
          "integration": "10m"
        },
        "names": [
          "GOTESTFLAGS"
        ],
        "refusals": []
      }
    },
    {
      "id": "perfgate",
      "taskIds": [
        "4.4",
        "4.5"
      ],
      "prev": "gate",
      "sharedPkg": "runtime",
      "parallel": false,
      "seam": "S7b",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "redTasks": [],
      "codeTasks": [
        "4.4",
        "4.5"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make profile && go test -timeout 10m -run '^$' -bench . -benchmem ./internal/goldset/ && openspec validate reader-budget-enforcement --strict --json",
      "coder": "zpatcher",
      "sites": [],
      "contract": {
        "states": [
          "unmeasured",
          "measured",
          "validated"
        ],
        "transitions": [
          {
            "input": "bounded parsing benchmarks and gold-set checks vs the prerequisite baseline",
            "state": "unmeasured -> measured",
            "action": "set (both evaluators, unchanged gate thresholds, allocation evidence separate from timing noise)",
            "evidence": "task 4.4; ADR 0008"
          },
          {
            "input": "openspec validate --strict plus final code/spec/doc consistency",
            "state": "measured -> validated",
            "action": "set",
            "evidence": "task 4.5"
          }
        ],
        "forbidden": [
          "weakening a gate threshold to make a comparison pass",
          "reporting latency as decisive on a developer machine"
        ],
        "seeding": {
          "baseline": "the prerequisite baseline recorded before this change"
        },
        "budgets": {
          "thresholds": "unchanged from ADR 0008"
        },
        "names": [
          "GOLDSET_MODE",
          "internal/goldset"
        ],
        "refusals": []
      }
    }
  ],
  "seams": [
    {
      "id": "S1",
      "tasks": [
        "1.3",
        "2.4"
      ],
      "summary": "Amend the two sealed reader budget test files to the frozen charge model, add the four output-storage red tests, then admit output-node, parser-workspace and construction storage in the guarded reader and delete the credit path.",
      "contract": {
        "states": [
          "unadmitted",
          "plan-reserved",
          "node-admitted",
          "workspace-admitted",
          "construction-admitted",
          "built",
          "refused",
          "sealed-old-model",
          "amended-frozen-model"
        ],
        "transitions": [
          {
            "input": "a sealed want whose terms omit output storage",
            "state": "sealed-old-model -> amended-frozen-model",
            "action": "set",
            "evidence": "chargeModel.sealedReconciliation.amend, 11 rows"
          },
          {
            "input": "a sealed want that is a '>=' floor",
            "state": "sealed-old-model",
            "action": "no-op",
            "evidence": "totals only grow under this model"
          },
          {
            "input": "a sealed want derived from admittedForSource",
            "state": "sealed-old-model",
            "action": "no-op",
            "evidence": "self-referential, model-independent"
          },
          {
            "input": "the long-token case reading 8000 bytes under a 1024 ceiling",
            "state": "sealed-old-model -> amended-frozen-model",
            "action": "forced",
            "evidence": "blocker B2 - restructure, ceiling and shape both change"
          },
          {
            "input": "the credits-the-copy-when-the-node-lands case",
            "state": "sealed-old-model -> amended-frozen-model",
            "action": "forced",
            "evidence": "creditRuling - the credit path is deleted, so the case name is false"
          },
          {
            "input": "counting reaches token n, plan+payload <= headroom",
            "state": "unadmitted",
            "action": "no-op",
            "evidence": "core/reader.go:201, core/reader_budget.go:209-224 - checkPlan charges nothing"
          },
          {
            "input": "counting reaches token n, plan+payload > headroom",
            "state": "unadmitted -> refused",
            "action": "forced",
            "evidence": "core/reader_budget.go:218-223"
          },
          {
            "input": "counting completes",
            "state": "unadmitted -> plan-reserved",
            "action": "set (32*tokens, then payload)",
            "evidence": "core/reader.go:159, core/reader_budget.go:230-248"
          },
          {
            "input": "addNode for a node with an aliased or absent payload",
            "state": "plan-reserved -> node-admitted",
            "action": "set (MeterReaderNodeBytes + payload)",
            "evidence": "core/reader.go:562-568; spec core-engine:120-122"
          },
          {
            "input": "addNode for a String node whose token has copied == true",
            "state": "plan-reserved -> node-admitted",
            "action": "set (MeterReaderNodeBytes only)",
            "evidence": "core/reader.go:642; spec core-engine:120-122 'SHALL NOT be charged again'"
          },
          {
            "input": "addNode for a synthetic quote/quasiquote/unquote/function node",
            "state": "plan-reserved -> node-admitted",
            "action": "set (two nodes: 32+len(sym), then 32+0)",
            "evidence": "core/reader.go:790-798"
          },
          {
            "input": "addNode whose admitAlloc is refused",
            "state": "-> refused",
            "action": "forced",
            "evidence": "core/reader_budget.go:163-175"
          },
          {
            "input": "Parser.nodes append needs capacity beyond the logical capacity",
            "state": "-> workspace-admitted",
            "action": "set (whole new logical capacity, unit 16)",
            "evidence": "design.md:139-145; workBufferBytes at admission_test.go:32-43"
          },
          {
            "input": "Parser.nodes append fits the logical capacity",
            "state": "workspace-admitted",
            "action": "no-op",
            "evidence": "the schedule charges on growth only"
          },
          {
            "input": "top-level forms append needs capacity beyond the logical capacity",
            "state": "-> workspace-admitted",
            "action": "set (unit 16)",
            "evidence": "core/reader.go:546-553"
          },
          {
            "input": "a nested collection appends children while an outer one holds a mark",
            "state": "workspace-admitted",
            "action": "set only if the SHARED high-water grows",
            "evidence": "core/reader.go:471, 691-700 - one schedule per read, not per collection"
          },
          {
            "input": "flat collection copy-out of n children",
            "state": "-> construction-admitted",
            "action": "set (ValueSlotsBytes(n))",
            "evidence": "core/reader.go:707, 739, 827"
          },
          {
            "input": "list finalization with n > 32 children",
            "state": "-> construction-admitted",
            "action": "set (readerListCellBytes*n) then link with a checkpoint every 128 slots",
            "evidence": "core/types.go:222; design.md:91"
          },
          {
            "input": "list finalization with n <= 32 children",
            "state": "construction-admitted",
            "action": "no-op (flat, no cells)",
            "evidence": "core/types.go:214"
          },
          {
            "input": "map insert of a new key needing entry-buffer growth",
            "state": "-> construction-admitted",
            "action": "set (whole new logical capacity, unit 64)",
            "evidence": "core/types.go Set small branch"
          },
          {
            "input": "map insert of a key already present",
            "state": "construction-admitted",
            "action": "no-op",
            "evidence": "core/types.go find -> found branch rewrites in place"
          },
          {
            "input": "map insert making the 9th distinct key (len(entries) >= hashMapSmallLimit)",
            "state": "-> construction-admitted",
            "action": "forced (build directly into the trie, never the Go-map branch)",
            "evidence": "core/types.go:1213, 1050-1062; sealed TestGuardedRead_MapConstructionContracts asserts m.large.root != nil && m.large.m == nil"
          },
          {
            "input": "trie assoc allocating node out",
            "state": "-> construction-admitted",
            "action": "set (hamtNodeBytes(out), admitted BEFORE out is allocated)",
            "evidence": "core/types.go:751-755, 823-869"
          },
          {
            "input": "numeric token",
            "state": "node-admitted -> construction-admitted",
            "action": "set (2n+256)",
            "evidence": "core/reader.go:885, core/reader_budget.go:254-269"
          },
          {
            "input": "a conversion that fails after its node and storage were admitted",
            "state": "construction-admitted",
            "action": "no-op (no credit)",
            "evidence": "worked total for (a b c) 1.2.3 = 829"
          },
          {
            "input": "any admit while b.failed != nil",
            "state": "refused",
            "action": "no-op (returns the sticky error)",
            "evidence": "core/reader_budget.go:167-169, 110-115"
          },
          {
            "input": "a read with a nil budget (the context-free reader)",
            "state": "unadmitted",
            "action": "no-op at every guarded site",
            "evidence": "core/reader_budget.go:34-35 - a nil *readerBudget is the absent budget"
          }
        ],
        "forbidden": [
          "any Go allocation reaching state built before the matching admit returned nil",
          "any decrease of EvalMeterSnapshot().AllocationBytes during a read - this is the invariant that removing creditAllocBytes protects",
          "charging an escaped payload both at reservePlan and at addNode",
          "the guarded map builder reaching h.large.m != nil",
          "mutating any HashMap, List or hamtNode that has been published outside the read",
          "changing ReaderStats.Nodes or ReaderStats.Bytes for any source",
          "a refused budget admitting anything afterwards - b.failed is sticky",
          "leaving EvalMeter.creditAllocBytes, evalState.creditAllocBytes or readerBudget.creditAlloc in the tree"
        ],
        "seeding": "Dialect.ReadWithContextStats(ctx, src, maxDepth) is the only public path. readOwnedScratch drives a caller-owned readerScratch for reuse cases. Reach refused only by lowering the ceiling through allocCeilingContext; never by writing b.failed. Reach the trie form only through a source with 9 or more distinct keys; never by constructing a largeMap directly.",
        "budgets": {
          "checkpoint interval": 128,
          "byte copy batch": 128,
          "slot copy/link batch": 128,
          "list flat threshold": 32,
          "small-map limit": 8,
          "trie fan-out": 32,
          "trie shift bits": 5,
          "node unit": 32,
          "value slot unit": 16,
          "entry unit": 64,
          "trie child unit": 8,
          "collection header": 24,
          "list cell": 32,
          "token unit": 32,
          "conversion slack": 256
        },
        "names": [
          "readerListCellBytes",
          "readerBudget.admitAlloc",
          "readerBudget.admitOutputNode",
          "readerBudget.admitSlots",
          "growthPlan",
          "growthPlan.admit",
          "Parser.addNode",
          "Parser.buildList",
          "Parser.mapSet",
          "hamtNode.assocGuarded",
          "hamtNodeBytes",
          "MeterReaderNodeBytes",
          "MeterValueSlotBytes",
          "MeterHashMapEntryBytes",
          "MeterCollectionHeaderBytes",
          "MeterTrieChildBytes",
          "ValueSlotsBytes",
          "listFlatThreshold",
          "hashMapSmallLimit",
          "NewResourceLimitError",
          "CodeResourceLimit"
        ],
        "refusals": [
          {
            "failure": "storage over the ceiling",
            "refusedBy": "readerBudget.admitAlloc -> EvalMeter.ChargeAllocBytes -> evalState.chargeAllocBytes",
            "when": "before the allocation it covers",
            "surface": "*LispicoError, Code CodeResourceLimit"
          },
          {
            "failure": "overflowing token plan",
            "refusedBy": "readerBudget.reservePlan via checkedTokenPlanBytes",
            "when": "after counting, before tokenizeInto allocates",
            "surface": "*LispicoError, Code CodeResourceLimit"
          },
          {
            "failure": "cancellation or deadline",
            "refusedBy": "readerBudget.checkpoint",
            "when": "every <=128 work units and on every return",
            "surface": "context.Canceled or context.DeadlineExceeded, outranking any pending syntax error via readerBudget.settle"
          }
        ]
      }
    },
    {
      "id": "S2",
      "tasks": [
        "2.5"
      ],
      "summary": "NO-RED-WAIVER: 2.5 is a single self-contained lifecycle task whose own text names its verification clauses; splitting it would invent tasks the change does not have, so the coder writes its four named tests and the -race floor gates them. Clear scratch references on every exit and discard retained buffers above the current ceiling. The chunk sites are the guard allowlist: they are the only test files this coder may add or change, and every other test in its pkgDirs is pre-sealed before it runs and guarded after.",
      "contract": {
        "states": [
          "retained",
          "cleared",
          "discarded",
          "independent"
        ],
        "transitions": [
          {
            "input": "successful read returning scratch to the pool",
            "state": "retained -> cleared",
            "action": "set (clear reader.input and every reference-bearing slot in s.tokens and s.parser.nodes, in batches of at most 128 slots)",
            "evidence": "design.md:154-159"
          },
          {
            "input": "terminal failure returning scratch",
            "state": "retained -> discarded",
            "action": "forced (drop the buffer rather than traverse input-sized storage to clear it)",
            "evidence": "design.md:157-159"
          },
          {
            "input": "retained logical capacity exceeds the current read's allocation ceiling",
            "state": "retained -> discarded",
            "action": "forced (must not go back in the pool)",
            "evidence": "spec core-engine:158-163"
          },
          {
            "input": "retained capacity within the ceiling on a successful read",
            "state": "cleared",
            "action": "no-op (capacity kept, charge still paid on the logical schedule)",
            "evidence": "sealed TestGuardedRead_PoolReuseChargesTheSameConstruction"
          },
          {
            "input": "a value tree returned by an earlier read",
            "state": "independent",
            "action": "no-op (never touched)",
            "evidence": "spec core-engine:163-164"
          },
          {
            "input": "clearing or discarding reference-bearing scratch slots on release",
            "state": "cleared",
            "action": "set (one work unit per cleared slot, in batches of at most 128, charged to the read being released)",
            "evidence": "spec core-engine: one unit per visited, linked, compared, cleared or copied slot"
          }
        ],
        "forbidden": [
          "clearing or reslicing storage that backs a returned AST",
          "resetting evaluation charges during release",
          "a low-budget read inheriting either a larger allowance or high-water storage from a previous read"
        ],
        "seeding": "call readerScratch.release(ceiling) directly on a test-owned scratch, or drive Dialect.ReadWithContextStats whose defer calls it. Never null out fields by hand to simulate a release.",
        "budgets": {
          "clear batch": 128,
          "checkpoint interval": 128
        },
        "names": [
          "readerScratch.release",
          "readerScratch.Reset",
          "readerScratchPool",
          "readerBudget.allocHeadroom"
        ],
        "refusals": [
          {
            "failure": "over-ceiling retained capacity",
            "refusedBy": "readerScratch.release",
            "when": "before the scratch returns to readerScratchPool",
            "surface": "none - the buffer is dropped, not an error"
          }
        ]
      }
    },
    {
      "id": "S3",
      "tasks": [
        "3.1",
        "0.2"
      ],
      "summary": "Route readForms through the guarded reader, remove the post-parse ChargeEvalReader, arm the deadline before parsing on every runtime-owned path, and restore task 0.2's characterization test.",
      "contract": {
        "states": [
          "legacy-read",
          "guarded-read",
          "double-charged",
          "deadline-armed",
          "deadline-absent"
        ],
        "transitions": [
          {
            "input": "engineImpl.readForms",
            "state": "legacy-read -> guarded-read",
            "action": "set (ReadWithMaxDepthStats -> ReadWithContextStats(ctx, ...))",
            "evidence": "runtime/eval.go:653-662; design.md:35-38"
          },
          {
            "input": "the ChargeEvalReader call after a successful parse",
            "state": "double-charged -> guarded-read",
            "action": "clear",
            "evidence": "runtime/eval.go:658; spec runtime-api:14-15 'already-accounted reader output SHALL NOT receive a second post-parse charge'"
          },
          {
            "input": "Eval entering with no armed deadline",
            "state": "deadline-absent -> deadline-armed",
            "action": "forced (move the WithEvalDeadline at runtime/eval.go:744-746 to BEFORE the readForms call at :735)",
            "evidence": "design.md:40-41"
          },
          {
            "input": "evalWithBindingScope (EvalWithBindings, LoadScope)",
            "state": "deadline-armed",
            "action": "no-op (already arms at runtime/eval.go:1091, before readForms at :1122)",
            "evidence": "runtime/eval.go:1090-1122"
          },
          {
            "input": "fileWatcher.reloadFile",
            "state": "deadline-absent -> deadline-armed",
            "action": "set (one detached evaluation context carrying the engine deadline for the whole read/evaluate/merge lifecycle)",
            "evidence": "runtime/watch.go:108-109; design.md:42-43"
          },
          {
            "input": "a read failing under the budget",
            "state": "guarded-read",
            "action": "forced (no form executes, no bindings publish)",
            "evidence": "spec runtime-api:77-80"
          },
          {
            "input": "a direct caller of Dialect.ReadWithContextStats outside runtime",
            "state": "guarded-read",
            "action": "no-op (output storage is admitted by the reader itself, so the public entry point is complete on its own)",
            "evidence": "chargeModel term T4"
          },
          {
            "input": "a legacy context-free caller (Read, ReadOne, ReadWithMaxDepthStats)",
            "state": "legacy-read",
            "action": "no-op (depth-only contract retained)",
            "evidence": "spec core-engine:11-12"
          },
          {
            "input": "an existing runtime metering golden or threshold that included the post-parse reader output charge",
            "state": "legacy-read",
            "action": "forced (re-check and restate it: removing ChargeEvalReader moves every number that included reader output)",
            "evidence": "design risks R2, R7; ADR 0008 gold set"
          }
        ],
        "forbidden": [
          "leaving ChargeEvalReader on the guarded path - that is the double charge",
          "removing ChargeEvalReader without S1 having landed - that is the resource-safety hole this whole plan exists to prevent",
          "resetting the ledger, the meter or the deadline between read and execute",
          "installing a fresh deadline for the second scan pass",
          "arming a deadline that overrides one the caller already supplied"
        ],
        "seeding": "Engine.Eval / EvalWithBindings / LoadScope / the watcher's reload path, with a context built by core.WithEvalResourceLimits and optionally core.WithEvalDeadline. Never call ReadWithContextStats from a runtime test to stand in for an entry point.",
        "budgets": {
          "characterization ceiling": 1024,
          "characterization reductions": 1000000,
          "checkpoint interval": 128
        },
        "names": [
          "engineImpl.readForms",
          "Dialect.ReadWithContextStats",
          "core.ChargeEvalReader",
          "core.WithEvalDeadline",
          "core.EvalDeadlineFrom",
          "core.DetachEvalState",
          "engineImpl.evalDeadline",
          "engineImpl.evalResourceContext",
          "fileWatcher.reloadFile",
          "readerBudgetBytes",
          "readerBudgetReductions",
          "TestEval_ReaderBudget_Characterization"
        ],
        "refusals": [
          {
            "failure": "over-budget source",
            "refusedBy": "the reader, inside readForms",
            "when": "before the first form evaluates",
            "surface": "*LispicoError Code CodeResourceLimit, wrapped by runtime/eval.go:737 as \"read: %w\""
          },
          {
            "failure": "cancelled context, including comment-only and empty source",
            "refusedBy": "readerBudget.checkpoint at read entry",
            "when": "before any source byte is scanned",
            "surface": "context.Canceled, errors.Is-matchable through the read: wrap"
          }
        ]
      }
    },
    {
      "id": "S4",
      "tasks": [
        "3.2"
      ],
      "summary": "NO-RED-WAIVER: 3.2 is a test-only task with no paired code task in tasks.md; the routing it exercises lands in the routing chunk, so the coder writes these public-entry tests over an already-routed reader. Public entry tests for Eval, EvalWithBindings, LoadScope and background hot reload, across both evaluator modes, both dialect reader surfaces, and context vs engine meters. The chunk sites are the guard allowlist: they are the only test files this coder may add or change, and every other test in its pkgDirs is pre-sealed before it runs and guarded after.",
      "contract": {
        "states": [
          "executed",
          "refused-before-execution",
          "published",
          "unpublished"
        ],
        "transitions": [
          {
            "input": "over-budget source through any of the four entry points",
            "state": "-> refused-before-execution",
            "action": "forced",
            "evidence": "spec runtime-api:77-80"
          },
          {
            "input": "a failed hot reload",
            "state": "-> unpublished",
            "action": "forced (no replacement bindings merge into rootEnv)",
            "evidence": "runtime/watch.go:139"
          },
          {
            "input": "in-budget source",
            "state": "-> executed / published",
            "action": "set",
            "evidence": "existing entry-point contracts"
          },
          {
            "input": "bytecode evaluator vs WithTreeWalker",
            "state": "executed",
            "action": "no-op (reading precedes evaluator selection, so both modes see the identical reader charge)",
            "evidence": "runtime/eval.go:748-768"
          },
          {
            "input": "an inherited caller deadline vs an engine-derived one vs a disabled timeout",
            "state": "executed",
            "action": "no-op (arming only ever fills an absent deadline)",
            "evidence": "runtime/eval.go:744-746, 982-988"
          }
        ],
        "forbidden": [
          "a partial form sequence executing after a failed read",
          "a reload publishing bindings from a failed read",
          "a test asserting reader charges through a private core path instead of a public entry point"
        ],
        "seeding": "Engine.Eval / EvalWithBindings / LoadScope, and the watcher driven through its own reload path. Meters via core.WithEvalResourceLimits (context) or WithResourceLimits/engineMeter (engine).",
        "budgets": {
          "test ceiling": 1024,
          "checkpoint interval": 128
        },
        "names": [
          "Engine.Eval",
          "Engine.EvalWithBindings",
          "Engine.LoadScope",
          "WithBytecode",
          "WithTreeWalker",
          "WithDialect",
          "WithResourceLimits",
          "meteringLimits",
          "isResourceLimit"
        ],
        "refusals": [
          {
            "failure": "over-budget source at any entry point",
            "refusedBy": "readForms",
            "when": "before the first form evaluates or any binding publishes",
            "surface": "*LispicoError Code CodeResourceLimit"
          }
        ]
      }
    },
    {
      "id": "S5",
      "tasks": [
        "3.3"
      ],
      "summary": "NO-RED-WAIVER: 3.3 is a verification task over the predecessors' settled lifecycle with no paired code task in tasks.md; the coder writes its outcome tests. Exactly one settled outcome for source errors, meter denial and cancelled empty reads, preserving the predecessors' lifecycle and panic behavior. The chunk sites are the guard allowlist: they are the only test files this coder may add or change, and every other test in its pkgDirs is pre-sealed before it runs and guarded after.",
      "contract": {
        "states": [
          "settled-once",
          "settled-twice",
          "unsettled"
        ],
        "transitions": [
          {
            "input": "a read failing under the budget",
            "state": "-> settled-once",
            "action": "set (returned error, EvalEvent and error statistics all describe the same failure)",
            "evidence": "spec runtime-api:87-90; runtime/eval.go:710-727"
          },
          {
            "input": "a cancelled empty or comment-only read",
            "state": "-> settled-once",
            "action": "set",
            "evidence": "spec runtime-api:68; core/reader_budget.go:57-80 checks even with zero pending work"
          },
          {
            "input": "a meter denying the reader charge",
            "state": "-> settled-once",
            "action": "set",
            "evidence": "runtime/eval.go:715-720 FinishEval is the single settlement point"
          },
          {
            "input": "a host meter panicking during settlement",
            "state": "-> settled-once",
            "action": "forced (recovered, lease returned, reported as the settlement error)",
            "evidence": "core/metering.go:352-370"
          },
          {
            "input": "the watcher's reload path",
            "state": "settled-once",
            "action": "no-op (it logs; EvalEvent is not part of its contract)",
            "evidence": "runtime/watch.go:110-113 - 'where those events are part of the entry point's contract', spec runtime-api:70-71"
          }
        ],
        "forbidden": [
          "two settlements for one read failure",
          "a reader failure bypassing FinishEval",
          "regressing the panic containment landed by evaluation-outcome-settlement (commits df156e7, ec03ad5)"
        ],
        "seeding": "the public entry points under a ceiling low enough to refuse the read, plus a host meter stub that denies or panics.",
        "budgets": {
          "settlement count": 1
        },
        "names": [
          "core.StartEval",
          "core.FinishEval",
          "core.FlushEvalState",
          "core.IsTerminalEvalError",
          "core.NewPanicError",
          "EvalEvent",
          "engineImpl.fireEvalCallbacks",
          "engineImpl.stats.recordEval"
        ],
        "refusals": [
          {
            "failure": "reader meter denial",
            "refusedBy": "evalState.chargeAllocBytes then finishEval",
            "when": "once, at the single settlement point",
            "surface": "*LispicoError Code CodeResourceLimit, reported identically to caller, event and stats"
          }
        ]
      }
    },
    {
      "id": "S6",
      "tasks": [
        "4.1"
      ],
      "summary": "NO-RED-WAIVER: documentation. Amend ADR 0007 and ADR 0011, ARCHITECTURE.md, README.md and the unreleased CHANGELOG.md with the frozen charge table, the guarded-reader boundary, the opaque numeric-conversion bound, bounded diagnostics, guarded collection construction and the no-duplicate-charge rule. Prose deliverables carry no behavior contract.",
      "contract": {
        "states": [
          "documented",
          "undocumented"
        ],
        "transitions": [
          {
            "input": "each of the six charge terms T1-T6",
            "state": "undocumented -> documented",
            "action": "set (one table row with its unit, its admission moment and its owner)",
            "evidence": "chargeModel.terms"
          },
          {
            "input": "the legacy context-free reader boundary",
            "state": "undocumented -> documented",
            "action": "set",
            "evidence": "spec core-engine:11-12; design.md:184-186"
          },
          {
            "input": "the post-parse-only safety claim in ADR 0007/0011",
            "state": "documented -> documented",
            "action": "forced (it is now false and must be replaced, not appended to)",
            "evidence": "proposal.md:43-47"
          },
          {
            "input": "the readerListCellBytes 32 vs List.Cons ListShallowBytes(1) 40 divergence",
            "state": "undocumented -> documented",
            "action": "set (two units for one listNode, with the reason each owner uses its own)",
            "evidence": "risk R3"
          },
          {
            "input": "a stated guarantee with no scenario behind it",
            "state": "undocumented -> documented",
            "action": "forced (drop the guarantee or add the scenario)",
            "evidence": "task 4.1"
          }
        ],
        "forbidden": [
          "publishing a unit in CHANGELOG.md that no ADR table row owns - the standing MeterFusedOpBytes defect",
          "weakening the ADR 0008 gate thresholds"
        ],
        "seeding": "n/a - prose",
        "budgets": {
          "documented charge terms": 6,
          "diagnostic render limit": 128,
          "checkpoint interval": 128
        },
        "names": [
          "docs/adr/0007",
          "docs/adr/0011",
          "ARCHITECTURE.md",
          "README.md",
          "CHANGELOG.md"
        ],
        "refusals": []
      }
    },
    {
      "id": "S7",
      "tasks": [
        "4.2",
        "4.3"
      ],
      "summary": "NO-TESTER-WAIVER: verification and archive readiness. Not code seams - these run commands and record evidence.",
      "contract": {
        "states": [
          "unrun",
          "green",
          "red"
        ],
        "transitions": [
          {
            "input": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime",
            "state": "unrun -> green",
            "action": "set (reader, lifecycle, statistics and pool regressions pass)",
            "evidence": "task 4.2"
          },
          {
            "input": "make build, make lint, make test, then the targeted -race run",
            "state": "unrun -> green",
            "action": "set",
            "evidence": "task 4.3"
          },
          {
            "input": "an untracked bin/ tree in the checkout",
            "state": "unrun",
            "action": "no-op (the floor runs in a worktree, which has no bin/; go list errors 7x in the primary and 0x in the worktree)",
            "evidence": "orchestrator verification at 75399eb"
          }
        ],
        "forbidden": [
          "narrowing the wrapper scope to make it pass"
        ],
        "seeding": {
          "worktree": "a clean worktree checkout"
        },
        "budgets": {
          "unit": "2m",
          "integration": "10m"
        },
        "names": [
          "GOTESTFLAGS"
        ],
        "refusals": []
      }
    },
    {
      "id": "S7b",
      "tasks": [
        "4.4",
        "4.5"
      ],
      "summary": "NO-TESTER-WAIVER: performance comparison and archive readiness; these run commands and record evidence.",
      "contract": {
        "states": [
          "unmeasured",
          "measured",
          "validated"
        ],
        "transitions": [
          {
            "input": "bounded parsing benchmarks and gold-set checks vs the prerequisite baseline",
            "state": "unmeasured -> measured",
            "action": "set (both evaluators, unchanged gate thresholds, allocation evidence separate from timing noise)",
            "evidence": "task 4.4; ADR 0008"
          },
          {
            "input": "openspec validate --strict plus final code/spec/doc consistency",
            "state": "measured -> validated",
            "action": "set",
            "evidence": "task 4.5"
          }
        ],
        "forbidden": [
          "weakening a gate threshold to make a comparison pass",
          "reporting latency as decisive on a developer machine"
        ],
        "seeding": {
          "baseline": "the prerequisite baseline recorded before this change"
        },
        "budgets": {
          "thresholds": "unchanged from ADR 0008"
        },
        "names": [
          "GOLDSET_MODE",
          "internal/goldset"
        ],
        "refusals": []
      }
    }
  ],
  "requirements": [
    {
      "shall": "### Requirement: Structural recursion is bounded The reader and the evaluator SHALL bound structural recursion so that no input can exhaust the Go stack.",
      "tests": [
        "TestReader_DeepParensReturnsResourceLimit",
        "TestValueWalksBoundOverDeepValues"
      ]
    },
    {
      "shall": "The reader SHALL enforce a nesting-depth ceiling while parsing lists, vectors, and maps; the evaluator SHALL enforce a structural-depth ceiling while descending `Vector` and `HashMap` literals and expanding quasiquote.",
      "tests": [
        "TestReader_NestedVectorJustOverDefaultLimitFails",
        "TestReader_NestedVectorUnderDefaultLimitOK",
        "TestLimits_NestedCallsDoNotTripStructural"
      ]
    },
    {
      "shall": "The reader ceiling SHALL be fixed for each read.",
      "tests": [
        "TestReader_ReadWithMaxDepthLowCeiling",
        "TestReadWithMaxDepthStats_ZeroMaxDepthUsesDefault"
      ]
    },
    {
      "shall": "Legacy reader calls without a context SHALL retain their depth-only contract; runtime-owned reads SHALL additionally carry evaluation budgets, cancellation, and the already-resolved deadline.",
      "tests": [
        "TestReadWithContextStats_LegacyParity"
      ]
    },
    {
      "shall": "The evaluator ceiling SHALL be tracked per evaluation, not on a shared engine field, consistent with the concurrent-evaluation contract.",
      "tests": [
        "TestLimits_MeteringCounterIsolationRace"
      ]
    },
    {
      "shall": "WHEN** two goroutines evaluate deeply nested literals concurrently on one engine - **THEN** each SHALL be bounded by its own per-evaluation structural-depth counter and `go test -race` SHALL report no data race ## ADDED Requirements ### Requirement: Metered reader work is interruptible before materialization A runtime-owned read SHALL consume the current evaluation's reduction budget while scanning and parsing.",
      "tests": [
        "TestReader_BareDeepParensProcessSurvives",
        "TestLimits_MeteringCounterIsolationRace"
      ]
    },
    {
      "shall": "The deterministic work model SHALL charge one unit per source byte advanced on each scanning pass, one per parsed node including reader-generated nodes, one per copied, hashed, or compared byte, and one per visited, linked, compared, cleared, or copied collection/workspace slot.",
      "tests": [
        "TestGuardedRead_ChargesEveryScannedByte",
        "TestGuardedRead_ExactCharges"
      ]
    },
    {
      "shall": "Comment, whitespace, number, symbol, and string scans SHALL participate even when they produce no AST node until the scan ends.",
      "tests": [
        "TestGuardedRead_ChargesEveryScannedByte"
      ]
    },
    {
      "shall": "Cancellation and the armed engine deadline SHALL be observed before work begins, at least every 128 local work units, and before every return.",
      "tests": [
        "TestGuardedRead_SynchronizesWithinBound",
        "TestGuardedRead_CancellationAndDeadline"
      ]
    },
    {
      "shall": "Pending reduction charges SHALL settle exactly once at each checkpoint and before returning, without hidden checkpoint charges, a second batching layer, or resetting the outer evaluation's counters or deadline.",
      "tests": [
        "TestGuardedRead_ExactCharges",
        "TestGuardedRead_SynchronizesWithinBound"
      ]
    },
    {
      "shall": "Numeric conversion is the sole opaque reader phase: its token-length work SHALL be admitted and charged before conversion, with cancellation/deadline checks immediately before and after it.",
      "tests": [
        "TestGuardedRead_NumericConversionAdmission"
      ]
    },
    {
      "shall": "That phase SHALL be bounded by the remaining reduction allowance; its admitted token length is the explicit exception to the 128-unit observation bound.",
      "tests": [
        "TestGuardedRead_NumericConversionAdmission",
        "TestGuardedRead_NumericConversionStorage"
      ]
    },
    {
      "shall": "It SHALL NOT perform input-independent exponent expansion or unbounded retries.",
      "tests": [
        "TestGuardedRead_InvalidNumberDiagnosticIsBounded"
      ]
    },
    {
      "shall": "Collection construction SHALL obey the checkpoint bound, including final list linking, string copies, map hashing, key comparisons, and collision handling.",
      "tests": [
        "TestGuardedRead_ConstructionStorage",
        "TestGuardedRead_MapConstructionContracts"
      ]
    },
    {
      "shall": "Terminal failures SHALL take precedence over a pending nonterminal read error.",
      "tests": [
        "TestGuardedRead_TerminalStateOutranksSyntaxError"
      ]
    },
    {
      "shall": "A read SHALL NOT return a partial form sequence on failure.",
      "tests": [
        "TestEval_ReaderBudget_Characterization"
      ]
    },
    {
      "shall": "No form SHALL execute until the complete source has been admitted and parsed successfully.",
      "tests": [
        "TestEval_ReaderBudget_Characterization",
        "TestPublicEntries_ReaderBudget"
      ]
    },
    {
      "shall": "only read fails before scanning - **WHEN** a runtime-owned read receives an already-cancelled context and a long comment-only source - **THEN** it SHALL return the cancellation error before scanning source bytes or allocating input-sized token storage, including when the source contains no executable form #### Scenario: Long token and trivia scans consume bounded work - **WHEN** a long comment, whitespace run, symbol, number, or string is read with a reduction budget smaller than the required scanning work - **THEN** the read SHALL fail with terminal `ResourceLimitError` without traversing the complete source, within at most one 128-unit synchronization interval #### Scenario: An existing deadline covers both scanning passes - **WHEN** the evaluation deadline expires during token counting or token production - **THEN** reading SHALL stop within the checkpoint bound and SHALL NOT install a fresh deadline for the later pass #### Scenario: Cancellation wins over a pending syntax error - **WHEN** cancellation is observed while settling a malformed read - **THEN** the cancellation error SHALL be returned instead of the pending syntax error, and already-incurred charges SHALL remain accounted #### Scenario: Numeric conversion is admitted as bounded opaque work - **WHEN** converting a numeric token would consume more work than the remaining reduction allowance - **THEN** reading SHALL reject it before conversion; an admitted conversion SHALL consume its token-length charge once and observe cancellation before publishing a result #### Scenario: Checkpoints neither batch again nor charge extra work - **WHEN** a read crosses several 128-unit work checkpoints and a final partial checkpoint - **THEN** each checkpoint SHALL observe cancellation/deadline directly and total reductions SHALL equal only the documented work units, independently of evaluator polling state #### Scenario: Final list construction remains interruptible - **WHEN** cancellation or deadline expiry occurs while linking a large list after its children have been parsed and copied - **THEN** the read SHALL stop within 128 local work units without completing the remaining chain or publishing a partial value #### Scenario: Map construction cannot hide long key or collision work - **WHEN** a read hashes or compares long keys, promotes a map beyond the small-map threshold, or scans colliding keys - **THEN** construction SHALL charge those bytes and slots, admit storage before allocation, and observe cancellation/deadline within 128 local work units while preserving existing key identity and duplicate-key behavior ### Requirement: Metered reader storage is admitted before allocation A runtime-owned read SHALL reserve deterministic allocation charges before allocating token storage, decoded payloads, parser workspace, numeric-conversion and diagnostic storage, or AST nodes and containers.",
      "tests": [
        "TestGuardedRead_CancellationAndDeadline"
      ]
    },
    {
      "shall": "Token counting SHALL reject a token-storage plan exceeding the remaining allocation allowance before allocating that plan.",
      "tests": [
        "TestGuardedRead_TokenPlanAdmission"
      ]
    },
    {
      "shall": "Token-count arithmetic, payload lengths, workspace growth, and node charges SHALL reject overflow rather than wrap.",
      "tests": [
        "TestReaderPlanArithmetic_RefusesOverflow"
      ]
    },
    {
      "shall": "The allocation model SHALL add 32 bytes per admitted token, including its end marker, and 16 bytes per logical reader workspace value slot to the existing reader node and payload model.",
      "tests": [
        "TestGuardedRead_TokenPlanAdmission",
        "TestGuardedRead_AdmitsOutputStorage"
      ]
    },
    {
      "shall": "Numeric conversion SHALL additionally reserve `2 * tokenBytes + 256` bytes before conversion, including successful conversions; invalid-number diagnostics SHALL render at most 128 source bytes plus a truncation marker while retaining error kind and source position.",
      "tests": [
        "TestGuardedRead_NumericConversionStorage",
        "TestGuardedRead_InvalidNumberDiagnosticIsBounded"
      ]
    },
    {
      "shall": "Linked-list construction SHALL add 32 bytes per cell.",
      "tests": [
        "TestGuardedRead_ConstructionStorage"
      ]
    },
    {
      "shall": "Map construction SHALL add the existing collection header, map entry, and trie child-slot units for its allocated node/buffer storage.",
      "tests": [
        "TestGuardedRead_MapConstructionContracts"
      ]
    },
    {
      "shall": "Workspace and construction-buffer growth SHALL use a deterministic allocation schedule, charged on the same logical schedule for cold and pooled reads.",
      "tests": [
        "TestGuardedRead_PoolReuseChargesTheSameConstruction",
        "TestGuardedRead_WorkspaceHighWaterSpansNesting"
      ]
    },
    {
      "shall": "Reader output SHALL retain its existing node and payload charges; a payload charged before decoding SHALL NOT be charged again when its AST node is created.",
      "tests": [
        "TestGuardedRead_AdmitsOutputStorage",
        "TestGuardedRead_PrepaidPayloadKeepsOutputTotals",
        "TestGuardedRead_EscapedPayloadAdmission"
      ]
    },
    {
      "shall": "The whole reader result SHALL NOT receive another post-parse charge.",
      "tests": [
        "TestGuardedRead_AdmittedBytesNeverDecrease",
        "TestEval_ReaderBudget_Characterization"
      ]
    },
    {
      "shall": "Bytes` SHALL remain unchanged for the same source.",
      "tests": [
        "TestGuardedRead_StatsUnchanged"
      ]
    },
    {
      "shall": "Increasing the unconsumed suffix after a fixed resource rejection point SHALL NOT increase the storage admitted before that rejection.",
      "tests": [
        "TestGuardedRead_UnreadSuffixDoesNotRaiseAdmission"
      ]
    },
    {
      "shall": "WHEN** a quoted list containing 250,000 integer elements is read with `MaxAllocationBytes` set to 1024 - **THEN** reading SHALL fail with terminal `ResourceLimitError` before allocating the full token array or constructing the full list #### Scenario: A decoded string reserves its payload first - **WHEN** an escaped string's decoded payload exceeds the remaining allocation allowance - **THEN** reading SHALL reject the payload before materializing an oversized decoded buffer, and any already-accounted prefix SHALL NOT be charged twice #### Scenario: Invalid numeric tokens cannot allocate an unbounded diagnostic - **WHEN** a long overflowing numeric token fits the reduction allowance but its conversion-storage charge exceeds a 1 KB allocation ceiling - **THEN** reading SHALL return terminal `ResourceLimitError` before conversion or formatting the token; with sufficient allocation, an invalid token SHALL produce a position-preserving read error with a bounded excerpt #### Scenario: Exact admitted allocation succeeds once - **WHEN** a valid source is read with an allocation ceiling exactly equal to its deterministic output, workspace, conversion, and construction charges and sufficient reductions - **THEN** it SHALL succeed with that total charge, and the same read with a ceiling one byte lower SHALL fail before the disallowed allocation #### Scenario: Successful output stats retain their meaning - **WHEN** the same in-budget source is parsed through legacy and metered readers - **THEN** their values and `ReaderStats` SHALL be equal even though only the metered read charges workspace and scan work to its evaluation ### Requirement: Reader reuse cannot bypass current resource policy Every pooled read SHALL use only its own context, limits, deadline, charges, and logical workspace growth schedule.",
      "tests": [
        "TestGuardedRead_TokenPlanAdmission"
      ]
    },
    {
      "shall": "A larger buffer retained from an earlier read SHALL NOT permit the next read to bypass a lower limit.",
      "tests": [
        "TestGuardedRead_ScratchReleaseDropsOversizedCapacity"
      ]
    },
    {
      "shall": "Returning reader scratch SHALL clear source references and used reference-bearing slots or discard the whole buffer without retaining it; storage whose logical retained capacity exceeds the current read's allocation ceiling SHALL NOT remain in the shared pool.",
      "tests": [
        "TestGuardedRead_FailedReadReleasesScratch",
        "TestReaderScratch_ResetClearsAllFields"
      ]
    },
    {
      "shall": "Clearing and discarding scratch SHALL preserve previously returned value trees and SHALL NOT reset evaluation charges.",
      "tests": [
        "TestGuardedRead_RetainedASTSurvivesScratchReuse",
        "TestReaderScratch_RetainedTreeSurvivesReuse"
      ]
    },
    {
      "shall": "### Requirement: Evaluation reductions and cumulative allocation are metered The runtime SHALL extend `ResourceLimits` with `MaxReductions` and `MaxAllocationBytes` fields.",
      "tests": [
        "TestLimits_MeteringFieldsExist"
      ]
    },
    {
      "shall": "Each SHALL default to a conservative value (10,000,000 reductions and 64 MiB per evaluation) when left at zero \u2014 never unlimited.",
      "tests": [
        "TestLimits_NegativeNormalize",
        "TestLimits_RetainedDefaultsNormalize"
      ]
    },
    {
      "shall": "Runtime-owned reads SHALL charge scan work, workspace, and reader output while reading and before allocating the storage each charge covers.",
      "tests": [
        "TestEval_ReaderBudget_Characterization",
        "TestGuardedRead_AdmitsOutputStorage"
      ]
    },
    {
      "shall": "All source SHALL be admitted before the first form evaluates; already-accounted reader output SHALL NOT receive a second post-parse charge.",
      "tests": [
        "TestLimits_FlatHugeReaderLiteralChargedBeforeFirstEval",
        "TestEval_ReaderBudget_Characterization"
      ]
    },
    {
      "shall": "The evaluator SHALL observe the caller's context cancellation at least every 1,024 reductions.",
      "tests": [
        "TestLimits_RangeCancelledContext"
      ]
    },
    {
      "shall": "Reader cancellation and deadline observation SHALL obey the tighter reader checkpoint contract.",
      "tests": [
        "TestGuardedRead_CancellationAndDeadline"
      ]
    },
    {
      "shall": "Counters SHALL NOT be shared across concurrent evaluations on the same engine.",
      "tests": [
        "TestLimits_MeteringCounterIsolationRace"
      ]
    },
    {
      "shall": "Allocation charges SHALL use a fixed, architecture-independent, documented per-type size table.",
      "tests": [
        "TestVectorLedgerBytesIndependentOfLayout",
        "TestLimits_MeteringFieldsExist"
      ]
    },
    {
      "shall": "WHEN** an Engine runs a loop that allocates faster than it reduces, configured with `MaxAllocationBytes: 1<<20` - **THEN** evaluation SHALL fail with `Code: \"ResourceLimitError\"` before the host is exhausted, and `try`/`catch` SHALL NOT intercept the error #### Scenario: Reduction-amplified macro recursion fails closed - **WHEN** an Engine runs a macro-amplified recursion that exceeds `MaxReductions` before tripping `MaxDepth` - **THEN** evaluation SHALL fail with `Code: \"ResourceLimitError\"` #### Scenario: GoFunc-built values are charged - **WHEN** a loop concatenates strings through a stdlib GoFunc until shallow result sizes exceed `MaxAllocationBytes` - **THEN** evaluation SHALL fail with `Code: \"ResourceLimitError\"` without per-plugin instrumentation #### Scenario: Reader output is charged before evaluation - **WHEN** source containing a flat literal whose parsed size exceeds `MaxAllocationBytes` is evaluated - **THEN** the call SHALL fail with `Code: \"ResourceLimitError\"` during reading, before the disallowed reader allocation and before the first form's evaluation begins #### Scenario: Context observed within the reduction budget - **WHEN** the caller's context is cancelled mid-evaluation - **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation #### Scenario: Per-evaluation counters are isolated - **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race` - **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race #### Scenario: Defaults match the embedder contract - **WHEN** an Engine is constructed with no `MaxReductions` / `MaxAllocationBytes` and adversarial input runs - **THEN** the defaults (10M reductions / 64 MiB allocation per evaluation) SHALL apply, never \"unlimited\" ## ADDED Requirements ### Requirement: Runtime source reading shares the evaluation lifecycle `Eval`, `EvalWithBindings`, `LoadScope`, and background hot-reload source parsing SHALL install the effective absolute evaluation deadline and active meter before reading.",
      "tests": [
        "TestLimits_MeteringAdversariesTripTightLimits"
      ]
    },
    {
      "shall": "The reader, compiler, and evaluator SHALL consume one continuous ledger; neither a second reader pass nor a cached compilation SHALL reset it.",
      "tests": [
        "TestEval_ReaderBudget_Characterization",
        "TestPublicEntries_ReaderBudget"
      ]
    },
    {
      "shall": "Caller cancellation SHALL be checked even for empty or comment-only source.",
      "tests": [
        "TestGuardedRead_CancellationAndDeadline",
        "TestGuardedRead_SingleSettledOutcome"
      ]
    },
    {
      "shall": "A failed read SHALL prevent form execution or hot-reload publication.",
      "tests": [
        "TestPublicEntries_ReaderBudget"
      ]
    },
    {
      "shall": "Public evaluation events and statistics SHALL describe the final settled failure where those events are part of the entry point's contract.",
      "tests": [
        "TestGuardedRead_SingleSettledOutcome"
      ]
    },
    {
      "shall": "- **THEN** `Read` SHALL return a `*core.LispicoError` reporting the depth limit, and the process SHALL NOT abort with a fatal stack overflow",
      "tests": [
        "TestReader_DeepParensReturnsResourceLimit",
        "TestReader_BareDeepParensProcessSurvives"
      ]
    },
    {
      "shall": "- **THEN** evaluation SHALL return a `*core.LispicoError` reporting the depth limit, not a panic or a fatal stack overflow",
      "tests": [
        "TestValueWalksBoundOverDeepValues",
        "TestLimits_NestedCallsDoNotTripStructural"
      ]
    },
    {
      "shall": "- **THEN** the second read SHALL fail at the same logical admission point as a cold read and SHALL NOT retain the oversized scratch capacity afterward",
      "tests": [
        "TestGuardedRead_ScratchReleaseDropsOversizedCapacity",
        "TestGuardedRead_PoolReuseChargesTheSameConstruction"
      ]
    },
    {
      "shall": "- **THEN** each SHALL observe its own policy, previously returned values SHALL remain unchanged, and race checks SHALL report no shared-state race",
      "tests": [
        "TestGuardedRead_ConcurrentCrossDialectReads",
        "TestGuardedRead_RetainedASTSurvivesScratchReuse",
        "TestLimits_MeteringCounterIsolationRace"
      ]
    },
    {
      "shall": "- **THEN** reading SHALL fail under that invocation's budget without executing source forms or publishing replacement bindings",
      "tests": [
        "TestPublicEntries_ReaderBudget"
      ]
    },
    {
      "shall": "- **THEN** execution SHALL fail with terminal `ResourceLimitError`, with no reset between phases and no duplicate reader charge",
      "tests": [
        "TestEval_ReaderBudget_Characterization",
        "TestGuardedRead_AdmittedBytesNeverDecrease"
      ]
    },
    {
      "shall": "- **THEN** its returned error, supported evaluation event, and error statistics SHALL describe the same final failure exactly once",
      "tests": [
        "TestGuardedRead_SingleSettledOutcome"
      ]
    }
  ],
  "testHarness": [
    "readerTokenPlanBytes \u2014 core/reader_budget_admission_test.go:14 \u2014 test-local const 32, the token workspace unit; declared independently of the implementation's readerTokenUnitBytes (core/reader_budget.go:13), so changing one does not move the other",
    "invalidNumberSourceBytes \u2014 core/reader_budget_admission_test.go:18 \u2014 test-local const 128, the bounded invalid-number render limit",
    "planBytes \u2014 core/reader_budget_admission_test.go:20 \u2014 tokens * 32, the admitted token plan for a token count",
    "conversionBytes \u2014 core/reader_budget_admission_test.go:25 \u2014 2*n + 256, the numeric-conversion temporary storage charge",
    "workBufferBytes \u2014 core/reader_budget_admission_test.go:32 \u2014 the logical value-slot charge of a reader work buffer reaching capacity n: 1 slot, then every doubled capacity charged whole",
    "flatFormBytes \u2014 core/reader_budget_admission_test.go:49 \u2014 workBufferBytes(1) + workBufferBytes(children) + ValueSlotsBytes(children): a top-level flat collection's construction storage beyond its plan",
    "allocCeilingContext \u2014 core/reader_budget_admission_test.go:56 \u2014 context with DefaultMaxReductions and exactly maxAllocBytes of allowance, so a read fails on storage and never on work",
    "admittedBytes \u2014 core/reader_budget_admission_test.go:61 \u2014 m.Snapshot().AllocationBytes",
    "readOwnedScratch \u2014 core/reader_budget_admission_test.go:66 \u2014 drives a test-owned *readerScratch (Reset + newReaderBudget + checkpoint + read) instead of a pooled one, so reuse cases hit known retained buffers",
    "budgetContext \u2014 core/reader_budget_cancel_test.go:13 \u2014 context with a reduction ceiling and default allocation allowance",
    "chargedReductions \u2014 core/reader_budget_cancel_test.go:18 \u2014 m.Snapshot().Reductions",
    "cancelAtReductions \u2014 core/reader_budget_cancel_test.go:30 \u2014 context wrapper whose Err() turns Canceled once the meter has charged `at` reductions: deterministic mid-read cancellation",
    "reductionProbe \u2014 core/reader_budget_cancel_test.go:45 \u2014 context wrapper recording the reduction total at every Err() call, used to prove the 128-unit synchronization bound",
    "stepClock \u2014 core/reader_budget_cancel_test.go:53 \u2014 swaps core's package-private nowFunc (core/eval.go:307) for a scripted clock advancing n readings, restored via t.Cleanup",
    "readErrorCode \u2014 core/reader_budget_cancel_test.go:67 \u2014 extracts LispicoError.Code from a read failure",
    "mixedSource / longKeySource / collidingKeySource \u2014 core/reader_budget_cancel_test.go:75,79,92 \u2014 fixtures for mixed forms, long map keys, and colliding map keys",
    "listCellBytes \u2014 core/reader_budget_accounting_test.go:11 \u2014 test-local const 32, the construction storage of one shared-tail list cell",
    "listSource / vectorSource \u2014 core/reader_budget_accounting_test.go:13,14 \u2014 n-element list and vector literals sharing every term but the collection form, so a diff isolates the cell charge",
    "pairSource / sameKeySource \u2014 core/reader_budget_accounting_test.go:18,32 \u2014 map literals of distinct keyword keys, and one key repeated n times",
    "admittedForSource \u2014 core/reader_budget_accounting_test.go:36 \u2014 reads src under a default ceiling and returns total admitted bytes; the basis of every exact-charge diff",
    "readContextStats \u2014 core/reader_context_parity_test.go:14 \u2014 calls Dialect.ReadWithContextStats and converts a panic into an error, because core must answer every failure through its error return",
    "assertReadErrorParity \u2014 core/reader_context_parity_test.go:80 \u2014 compares guarded vs legacy errors by Code, Message, Line and Col",
    "readNonPooled \u2014 core/reader_pool_test.go:15 \u2014 reads through NewReaderWithFlags + NewParserWithDepth, bypassing the scratch pool, as the pooled path's control",
    "TestReaderScratch_ResetClearsAllFields \u2014 core/reader_pool_test.go:36 \u2014 hand-written field-by-field assertion, NOT reflective: a newly added scratch field is not caught automatically",
    "TestReaderScratch_RetainedTreeSurvivesReuse / _NodeScratchIsolatedAcrossReuse / _StatsIdenticalAcrossReuse \u2014 core/reader_pool_test.go:84,109,153 \u2014 the reuse contracts task 2.5 must not break",
    "TestReadWithMaxDepthStats_PooledNoEscapeStringSharesBackingArray \u2014 core/reader_pool_test.go:297 \u2014 pins that an unescaped string token still aliases the source, so clearing must not touch returned storage",
    "TestReaderStats_Goldset / TestReaderStats_Bench \u2014 core/reader_stats_test.go:13,56 \u2014 ReaderStats totals over the gold set and bench fixtures",
    "tokenizeCountCases \u2014 core/reader_tokenize_count_test.go:21 \u2014 shared fixture set proving countTokens matches the second pass exactly (also under flags and unterminated input)",
    "TestVectorLedgerBytesIndependentOfLayout \u2014 core/metering_test.go:13 \u2014 the ledger-vs-Go-layout independence rule the new charges must also obey",
    "newLimitsEngine / newMeteringStdlibEngine \u2014 runtime/resource_limits_test.go:22,29 \u2014 engines built with explicit ResourceLimits, with and without stdlib",
    "evalLimits / isResourceLimit / resourceLimitErrorCode \u2014 runtime/resource_limits_test.go:44,50,56 \u2014 eval under limits and classify the resulting error",
    "meteringLimits / requireMeteringField / skipUntilMeteringFields \u2014 runtime/resource_limits_test.go:90,78,71 \u2014 build a ResourceLimits with reduction and allocation ceilings set by name",
    "evalModeName / vectorLiteral / allocationLoopSource / reductionLoopSource \u2014 runtime/resource_limits_test.go:107,114,128,132 \u2014 mode labels and budget-burning source generators",
    "settlementProbe (watch/seen) \u2014 runtime/settlement_observation_test.go:120,131 \u2014 captures every EvalEvent plus the stats snapshot at settlement time",
    "runSettlementOutcomes / assertSettledOnce / settlementOutcomes / settlementFixtures / outcomeEntries \u2014 runtime/settlement_observation_test.go:180,215,148,158,757 \u2014 the one-settled-outcome table harness for Eval, EvalWithBindings and LoadScope",
    "newSettlementEngine / newPrecedenceEngine / collectEvents \u2014 runtime/settlement_observation_test.go:789,650,800 \u2014 engines wired to an event collector",
    "retainedDenialCases / newRetainedDenialEngine / assertRetainedDenialPublished / denyLeaseMeter \u2014 runtime/settlement_observation_test.go:307,318,339,93 \u2014 meter-denial outcomes",
    "runPrecedence / runPrecedenceSource / forEachPublishedError \u2014 runtime/settlement_observation_test.go:512,519,585 \u2014 terminal-vs-eval error precedence across both evaluators",
    "catchPanic / panicSettlementMeter / burningSettlementMeter \u2014 runtime/settlement_observation_test.go:733,745,948 \u2014 panic containment and duration accounting during settlement",
    "TestReloadFile_* \u2014 runtime/watch_test.go:115,143,171,212 \u2014 drive fileWatcher.reloadFile directly (no ticker) for syntax error, eval error, panic and success",
    "TestDialect_Reader_* \u2014 runtime/dialect_reader_test.go:17,36,56 \u2014 reader-flag gating through the engine, the dialect reader surfaces task 3.2 must cover"
  ],
  "floor": "make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -race -timeout 2m -p 2 -parallel 2 ./core ./runtime && openspec validate reader-budget-enforcement --strict --json",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 3
  },
  "amendments": [
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_TokenPlanAdmission",
      "case": "long-trivia/costs-no-token-storage",
      "line": 105,
      "was": "planBytes(4)+flatFormBytes(1) = 176",
      "now": "planBytes(4)+flatFormBytes(1)+outputBytes(t, src) = 241",
      "outputTerm": "2 nodes * 32 + 1 = 65"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_TokenPlanAdmission",
      "case": "long-token/costs-one-workspace-unit",
      "line": "110-124",
      "was": "planBytes(2)+workBufferBytes(1) = 80 under a 1024-byte ceiling, read expected to SUCCEED",
      "now": "RESTRUCTURE, not renumber. Ceiling 1024 -> DefaultMaxAllocationBytes; want = planBytes(2)+workBufferBytes(1)+MeterReaderNodeBytes+8000 = 8112. Preserve the case's actual point ('token storage is per token, not per byte') by adding a paired read of a 16000-byte symbol and asserting the delta is exactly 8000 - payload only, with no additional plan bytes.",
      "severity": "blocker B2 - the old case cannot pass under any spec-conformant model"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_TokenPlanAdmission",
      "case": "exact-ceiling/admits-the-plan AND one-byte-below/rejects-the-plan",
      "line": "127, 143",
      "was": "want := planBytes(6) + flatFormBytes(3) = 368",
      "now": "want := planBytes(6) + flatFormBytes(3) + outputBytes(t, \"(a b c)\") = 499",
      "outputTerm": "4 nodes * 32 + 3 = 131"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_PoolReuseKeepsPayingThePlan",
      "case": "-",
      "line": 167,
      "was": "read*(planBytes(6)+flatFormBytes(3)) = read*368",
      "now": "read*499"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_MalformedSuffixAdmission",
      "case": "-",
      "line": 210,
      "was": "planBytes(7)+flatFormBytes(3)+conversionBytes(5) = 666",
      "now": "+ outputBytes for 5 nodes and 3 payload bytes = 829",
      "note": "the failure message must say the number token's node charge is admitted and NOT refunded when its conversion fails"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_EscapedPayloadAdmission",
      "case": "credits-the-copy-when-the-node-lands",
      "line": "231-244",
      "was": "planBytes(2)+workBufferBytes(1) = 80, named for a credit",
      "now": "RENAME to charges-the-prepaid-payload-once; want = planBytes(2)+workBufferBytes(1)+MeterReaderNodeBytes = 115 (64 plan + 3 reserved payload + 32 node + 16 buffer). Nothing is credited any more.",
      "severity": "the case name is now false, not just its number"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_EscapedPayloadAdmission",
      "case": "zero-copy-payload-gains-no-copy-charge",
      "line": 256,
      "was": "80",
      "now": "planBytes(2)+workBufferBytes(1)+MeterReaderNodeBytes+2 = 114"
    },
    {
      "file": "core/reader_budget_admission_test.go",
      "test": "TestGuardedRead_NumericConversionStorage",
      "case": "all four sub-cases derive from want",
      "line": 264,
      "was": "planBytes(2)+workBufferBytes(1)+conversionBytes(5) = 346",
      "now": "+ MeterReaderNodeBytes = 378"
    },
    {
      "file": "core/reader_budget_accounting_test.go",
      "test": "TestGuardedRead_ExactCharges",
      "case": "generated-quote-node",
      "line": 76,
      "was": "planBytes(1) + 2*MeterValueSlotBytes = 64",
      "now": "planBytes(1) + 2*MeterValueSlotBytes + 2*MeterReaderNodeBytes + int64(len(\"quote\")) = 133",
      "why": "wrapForm calls addNode twice - the generated Symbol carries a 5-byte payload, the generated List carries none"
    },
    {
      "file": "core/reader_budget_accounting_test.go",
      "test": "TestGuardedRead_ExactCharges",
      "case": "parser-workspace-doubling",
      "line": 87,
      "was": "planBytes(1) + MeterValueSlotBytes + 8*MeterValueSlotBytes = 176",
      "now": "+ MeterReaderNodeBytes + int64(len(\"a\")) = 209",
      "why": "the 5th child is one more output node with a 1-byte symbol payload"
    },
    {
      "file": "core/reader_budget_accounting_test.go",
      "test": "TestGuardedRead_PrepaidPayloadKeepsOutputTotals",
      "case": "-",
      "line": "300-330",
      "was": "assertion is CORRECT and stays - both (\"a\\nb\") and (\"axb\") admit 243",
      "now": "comment only: 'the copy is credited when its node lands' and 'only the unpaid delta' describe a credit that no longer exists. Restate as: the payload is charged once, at reservation, and the node it becomes admits only its node unit.",
      "severity": "comment-only, no arithmetic change"
    }
  ],
  "chargeModel": {
    "decision": "The guarded reader admits output-node storage itself: MeterReaderNodeBytes (32) per parsed node plus that node's own payload bytes, admitted at Parser.addNode before the node value is constructed. The running total the reader admits for output equals ReaderAllocationBytes(stats) exactly, so task 3.1's removal of core.ChargeEvalReader from runtime.readForms moves the same total earlier and makes it incremental, rather than deleting it.",
    "authority": "specs/core-engine/spec.md:110-122 (ADDED 'Metered reader storage is admitted before allocation') and specs/runtime-api/spec.md:12-15 (MODIFIED 'Runtime-owned reads SHALL charge scan work, workspace, and reader output while reading'). design.md:130-135 says the same. The two sealed test files agree with each other and disagree with all three. The accepted spec wins; the sealed tests are AMENDed.",
    "independentProof": "Without the output term, core/reader.go:642-644's creditAlloc makes an escaped payload's net admitted storage ZERO, not 'charged once': reservePlan admits the decoded length, and the credit returns all of it against a node charge that never happens. The sealed case reader_budget_admission_test.go:241 pins exactly that (planBytes(2)+workBufferBytes(1), no payload term). The spec sentence 'SHALL NOT be charged AGAIN when its AST node is created' presupposes a charge at node creation. The credit is only coherent if the output term exists.",
    "units": {
      "readerTokenUnitBytes": {
        "value": 32,
        "site": "core/reader_budget.go:13",
        "testMirror": "readerTokenPlanBytes / planBytes(tokens)"
      },
      "MeterReaderNodeBytes": {
        "value": 32,
        "site": "core/metering.go:29"
      },
      "MeterValueSlotBytes": {
        "value": 16,
        "site": "core/metering.go:14",
        "helper": "ValueSlotsBytes(n)"
      },
      "MeterHashMapEntryBytes": {
        "value": 64,
        "site": "core/metering.go:19"
      },
      "MeterCollectionHeaderBytes": {
        "value": 24,
        "site": "core/metering.go:17"
      },
      "MeterTrieChildBytes": {
        "value": 8,
        "site": "core/metering.go:27"
      },
      "readerConversionSlackBytes": {
        "value": 256,
        "site": "core/reader_budget.go:17"
      },
      "readerListCellBytes": {
        "value": 32,
        "site": "NEW constant in core/reader_budget.go",
        "testMirror": "listCellBytes, reader_budget_accounting_test.go:11"
      },
      "checkInterval": {
        "value": 128,
        "site": "core/eval.go:303"
      },
      "readerSourceRenderLimit": {
        "value": 128,
        "site": "core/reader_budget.go:22"
      },
      "listFlatThreshold": {
        "value": 32,
        "site": "core/types.go:175"
      },
      "hashMapSmallLimit": {
        "value": 8,
        "site": "core/types.go:707"
      },
      "vecBits/vecBranch": {
        "value": "5 / 32",
        "site": "core/types.go:384-387"
      }
    },
    "terms": [
      {
        "id": "T1",
        "name": "token plan",
        "formula": "32 * tokens, EOF token included",
        "admittedWhen": "once, by readerBudget.reservePlan, after countTokens completes and before tokenizeInto obtains storage",
        "site": "core/reader.go:155-161, core/reader_budget.go:230-248",
        "notes": "checkPlan (reader_budget.go:209-224) runs per counted token and charges NOTHING; it only refuses when plan+payload exceeds the headroom read once at reader.go:193. Pool reuse never waives the charge."
      },
      {
        "id": "T2",
        "name": "escaped-string decoded payload",
        "formula": "sum over escaped string tokens of len(decoded); counted into Reader.copiedPayload on pass 1 (reader.go:375-378)",
        "admittedWhen": "in the same reservePlan call, second admitAlloc, before pass 2 decodes anything",
        "site": "core/reader.go:159, core/reader_budget.go:247",
        "creditRule": "NONE. See creditRuling below. The payload is charged once, at reservation. The node that the copy becomes admits only MeterReaderNodeBytes, skipping its payload term, because tok.copied is true at that exact site (reader.go:642).",
        "zeroCopy": "a token that aliases r.input (reader.go:323) never reserves and never skips: its node admits 32 + len(tok.val)."
      },
      {
        "id": "T3",
        "name": "numeric-conversion temporary storage",
        "formula": "2*len(tok.val) + 256, checked arithmetic (checkedConversionBytes)",
        "admittedWhen": "per numeric token, in parseNumberToken, before strconv is entered, on success and failure alike",
        "site": "core/reader.go:881-900, core/reader_budget.go:145-150, 254-269",
        "notes": "never credited on a failed conversion. Bounded separately in work units: admitConversion refuses a token longer than MaxReductions/3 (reader_budget.go:284)."
      },
      {
        "id": "T4",
        "name": "output-node storage",
        "formula": "per node: MeterReaderNodeBytes (32) + that node's payload bytes; payload term SKIPPED when the node comes from a token with copied == true",
        "admittedWhen": "in Parser.addNode, BEFORE the node value is constructed. Every call site already precedes its allocation: reader.go:715-718 (list), 747-750 (vector), 782-785 (map), 791-797 (wrapForm), and each atom arm at 639-680.",
        "site": "core/reader.go:562-568 (to change)",
        "ordering": "admitAlloc first, then work(1), then stats.Nodes++/stats.Bytes+=. A refusal then never leaves ReaderStats describing a node that was never built. Stats are discarded on failure anyway, so this is unobservable but is the rule.",
        "syntheticNodes": "included, with no special case: wrapForm calls addNode(len(sym)) for the generated Symbol and addNode(0) for the generated List, so 'a admits 2 extra nodes and 5 extra payload bytes over a.",
        "relationshipToReaderStats": "the admitted output total for a read is exactly ReaderAllocationBytes(stats) = stats.Nodes*MeterReaderNodeBytes + stats.Bytes (core/metering.go:726-728), because addNode maintains both counters. ReaderStats values themselves are UNCHANGED - they still describe output only, never workspace, conversion, construction or scan work.",
        "relationshipTo31": "core.ChargeEvalReader(ctx, stats) at runtime/eval.go:658 charges that identical total post-parse. 3.1 deletes it. Net charge across the read is unchanged; what changes is that it is admitted before allocation, incrementally, and is now also charged for direct callers of the public Dialect.ReadWithContextStats."
      },
      {
        "id": "T5",
        "name": "parser workspace",
        "formula": "MeterValueSlotBytes (16) per logical slot, on a fresh-per-read logical doubling schedule: capacity 1, then doubling; the WHOLE new logical capacity is charged before each growth, because old and new buffers coexist during the copy",
        "closedForm": "workBufferBytes(n) as already written at reader_budget_admission_test.go:32-43",
        "buffers": [
          "Parser.nodes - the shared mark/truncate child scratch (reader.go:471). ONE schedule for the whole read, high-water across all nesting, NOT per collection. A nested form's children stack on top of its parent's, so ({...3 pairs...} [a a a a]) reaches high-water 5, not 4.",
          "the top-level forms slice in readerScratch.read (reader.go:546-553). Reaches len(forms)."
        ],
        "values": {
          "1": 16,
          "2": 48,
          "3": 112,
          "4": 112,
          "5": 240,
          "40": 2032
        },
        "poolRule": "logical, not physical. Retained capacity avoids the Go allocation but never the charge."
      },
      {
        "id": "T6",
        "name": "construction storage",
        "subterms": [
          {
            "name": "flat collection copy-out",
            "formula": "ValueSlotsBytes(n) for the items := make([]Value, n) at reader.go:707, 739, 827",
            "note": "the container header is folded into T4's 32-byte node unit; no separate MeterCollectionHeaderBytes for List/Vector."
          },
          {
            "name": "linked-list cells",
            "formula": "readerListCellBytes (32) * n, only when n > listFlatThreshold (32)",
            "note": "NewList keeps a flat slice at or below 32 (types.go:213-218), so a 32-child list admits no cell bytes and a 33-child list admits 33*32."
          },
          {
            "name": "small-map entry buffer",
            "formula": "MeterHashMapEntryBytes (64) per logical slot on the SAME doubling schedule as T5 - helper entryBufferBytes(n)",
            "values": {
              "1": 64,
              "3": 448,
              "4": 448,
              "8": 960
            },
            "note": "charged only when an insert of a NEW key grows the buffer; rewriting an existing key is a no-op (types.go find->found branch)."
          },
          {
            "name": "HAMT node",
            "formula": "hamtNodeBytes(out) = MeterCollectionHeaderBytes + 64*len(out.entries) + 8*len(out.children), per node the guarded builder allocates, admitted before allocating it",
            "site": "core/types.go:751-755",
            "note": "exact occupied size, per design.md:150-151. Collision-node entry storage is inside this exact term; it gets no separate doubling schedule because assoc allocates exact-size copies (types.go:827,832) that are never doubled."
          }
        ]
      }
    ],
    "creditRuling": {
      "question": "Does EvalMeter.creditAllocBytes (core/metering.go:205-232) ship, or does the reader charge only the unpaid delta and never decrement?",
      "ruling": "The credit path is REMOVED. EvalMeter.creditAllocBytes, evalState.creditAllocBytes and readerBudget.creditAlloc (reader_budget.go:181-186) are deleted, along with the call at reader.go:642-644. Instead, addNode skips the payload term for a token with copied == true. Totals are byte-for-byte identical either way; the ledger stops moving backwards.",
      "reasons": [
        "core/metering.go builds its whole correctness argument on counters that only climb: addCharge:530-556 proves wrap-safety from 'every add here is at most max', saturateCounter:558-577 is documented as 'the only writer that may run with the counter in an unusable state', and publishedTotal:579-598 returns math.MaxInt64 for a wrapped total SPECIFICALLY to stay monotone. creditAllocBytes is the only writer that moves a counter down and it is outside every one of those proofs - a concurrent subtract is not an add of at most max.",
        "It is already not exact: the 'if used < n { return }' branch (metering.go:225-227) silently drops the credit whenever the counter has saturated negative, so a read near the ceiling keeps the reservation regardless.",
        "It is asymmetric under a session meter: the metered branch (metering.go:219-222) returns bytes to st.leasedAllocBytes, i.e. to the LEASE, not to the host meter. Bytes drawn across a lease boundary from the host meter are never returned to it. Charge and credit are not symmetric operations there.",
        "The reader has the information at the credit site: tok.copied is in hand at reader.go:642, which is the same place the credit would fire. Not charging is strictly simpler than charging and refunding.",
        "creditAllocBytes was ADDED by this change (git diff master...HEAD on core/metering.go). Deleting it removes nothing that predates the change and needs no deprecation."
      ],
      "invariantProtected": "EvalMeterSnapshot().AllocationBytes is monotonically non-decreasing for the lifetime of one evalState.",
      "provingTest": "TestGuardedRead_AdmittedBytesNeverDecrease (new, core/reader_budget_accounting_test.go): probe AllocationBytes at every terminal-state check across a read of a source mixing escaped strings, zero-copy strings and collections, and fail on any decrease. TestGuardedRead_PrepaidPayloadKeepsOutputTotals (existing, accounting_test.go:303) keeps proving the totals agree: (\"a\\nb\") and (\"axb\") both admit 243.",
      "docChange": "the phrase 'the ledger records only the unpaid delta' in design.md:135 stays true, but its mechanism changes from credit-after to skip-at-source. Task 4.1 must state it that way."
    },
    "workedTotals": [
      {
        "source": "(a b c)",
        "total": 499,
        "terms": {
          "T1 plan": "6 tokens * 32 = 192",
          "T4 output": "4 nodes * 32 + 3 payload bytes = 131",
          "T5 node scratch": "workBufferBytes(3) = 112",
          "T5 form buffer": "workBufferBytes(1) = 16",
          "T6 items copy": "ValueSlotsBytes(3) = 48"
        },
        "sealedValueToday": 368,
        "delta": "+131, exactly the output term"
      },
      {
        "source": "'a",
        "total": 246,
        "terms": {
          "T1 plan": "3 tokens * 32 = 96",
          "T4 output": "3 nodes * 32 + 6 payload bytes (1 for a, 5 for quote) = 102",
          "T5 node scratch": "0 - a quote wraps a form, it never appends to Parser.nodes",
          "T5 form buffer": "workBufferBytes(1) = 16",
          "T6 wrapForm items": "ValueSlotsBytes(2) = 32"
        },
        "companion": "bare a = 64 + 33 + 16 = 113",
        "delta": "246 - 113 = 133, which is the amended generated-quote-node want"
      },
      {
        "source": "pairSource(4) = {:k0 v :k1 v :k2 v :k3 v }",
        "total": 1116,
        "terms": {
          "T1 plan": "11 tokens * 32 = 352",
          "T4 output": "9 nodes * 32 + 12 payload bytes = 300",
          "T5 node scratch": "0 - parseHashMap inserts pairs as it parses them and never uses Parser.nodes",
          "T5 form buffer": "16",
          "T6 entry buffer": "entryBufferBytes(4) = (1+2+4)*64 = 448"
        }
      },
      {
        "source": "pairSource(9) - past the small-map threshold",
        "total": ">= 2883 (floor; the exact total is derived, not literal)",
        "terms": {
          "T1 plan": "21 tokens * 32 = 672",
          "T4 output": "19 nodes * 32 + 27 payload bytes = 635",
          "T5 form buffer": "16",
          "T6 entry buffer": "entryBufferBytes(8) = (1+2+4+8)*64 = 960 - inserts 1..8 grow the small buffer before the 9th promotes",
          "T6 trie": ">= MeterCollectionHeaderBytes + 9*MeterHashMapEntryBytes = 600"
        },
        "whyDerived": "the trie's node count is a deterministic function of the fixed FNV hash over the 9 keys, but it is not hand-derivable. Assert it the way the sealed files already do: a floor in TestGuardedRead_ConstructionStorage (>=) plus the self-referential exactness of TestGuardedRead_AllowanceBoundary (read at exactly admittedForSource(src) succeeds, one byte below returns CodeResourceLimit)."
      },
      {
        "source": "listSource(40) - past listFlatThreshold",
        "total": 6696,
        "terms": {
          "T1 plan": "43 * 32 = 1376",
          "T4 output": "41 nodes * 32 + 40 payload bytes = 1352",
          "T5 node scratch": "workBufferBytes(40) = 127 slots * 16 = 2032",
          "T5 form buffer": "16",
          "T6 items copy": "ValueSlotsBytes(40) = 640",
          "T6 cells": "40 * 32 = 1280"
        }
      },
      {
        "source": "({:k0 v :k1 v :k2 v } [a a a a ]) - the sealed nested-collections case",
        "total": 1773,
        "terms": {
          "T1 plan": "17 * 32 = 544",
          "T4 output": "13 nodes * 32 + 13 payload bytes = 429",
          "T5 node scratch": "workBufferBytes(5) = 240 - HIGH-WATER 5, not 4: the map sits at index 0 while the vector's 4 children stack on top",
          "T5 form buffer": "16",
          "T6 entry buffer": "entryBufferBytes(3) = 448",
          "T6 vector items": "64",
          "T6 list items": "32"
        }
      },
      {
        "source": "\"a\\nb\" (escaped)",
        "total": 115,
        "terms": {
          "T1 plan": "64",
          "T2 payload": "3, reserved once",
          "T4 output": "32 only - payload prepaid, tok.copied is true",
          "T5 form buffer": "16"
        },
        "sealedValueToday": 80
      },
      {
        "source": "\"ab\" (zero-copy)",
        "total": 114,
        "terms": {
          "T1 plan": "64",
          "T4 output": "32 + 2 = 34",
          "T5 form buffer": "16"
        },
        "sealedValueToday": 80
      },
      {
        "source": "12345",
        "total": 378,
        "terms": {
          "T1 plan": "64",
          "T4 output": "32 + 0 - an Int node calls addNode(0)",
          "T3 conversion": "2*5 + 256 = 266",
          "T5 form buffer": "16"
        },
        "sealedValueToday": 346
      },
      {
        "source": "3000 comment lines then (a)",
        "total": 241,
        "terms": {
          "T1 plan": "4 * 32 = 128",
          "T4 output": "2 nodes * 32 + 1 = 65",
          "T5 node scratch": "16",
          "T5 form buffer": "16",
          "T6 items copy": "16"
        },
        "sealedValueToday": 176,
        "note": "still fits the 1024 ceiling the sealed case uses"
      },
      {
        "source": "one 8000-byte symbol",
        "total": 8112,
        "terms": {
          "T1 plan": "64",
          "T4 output": "32 + 8000 = 8032",
          "T5 form buffer": "16"
        },
        "sealedValueToday": 80,
        "note": "BREAKS the sealed case outright. 8112 cannot fit the 1024 ceiling that case expects the read to succeed under."
      },
      {
        "source": "(a b c) 1.2.3 - the malformed-suffix case",
        "total": 829,
        "terms": {
          "T1 plan": "7 * 32 = 224",
          "T4 output": "5 nodes * 32 + 3 = 163 - the list's 4 nodes plus the addNode(0) at reader.go:648 for a number whose conversion then fails",
          "T5 node scratch": "112",
          "T5 form buffer": "16",
          "T6 items copy": "48",
          "T3 conversion": "266"
        },
        "sealedValueToday": 666,
        "note": "the failed conversion's 32-byte node charge is never credited back. That is the model, not an accident."
      }
    ],
    "sealedReconciliation": {
      "rule": "an AMEND of a sealed test is the sanctioned path here and every one is named below. Totals only ever GROW under this model, so every '>=' floor in the sealed files survives untouched.",
      "newHelper": [
        {
          "name": "outputBytes",
          "file": "core/reader_budget_admission_test.go",
          "signature": "func outputBytes(t *testing.T, src string) int64",
          "body": "read src through FullDialect().ReadWithMaxDepthStats(src, 0) and return ReaderAllocationBytes(stats)",
          "why": "states the output term as what it is - the legacy reader's own node+payload total - instead of re-deriving node counts by hand in nine places. The coupling to the legacy reader IS the invariant (ReaderStats unchanged, output charges retained)."
        },
        {
          "name": "entryBufferBytes",
          "file": "core/reader_budget_accounting_test.go",
          "signature": "func entryBufferBytes(entries int64) int64",
          "body": "the small-map entry buffer on the checked-doubling schedule: MeterHashMapEntryBytes * the doubled logical capacity that holds `entries`",
          "why": "states the small-map buffer term once instead of re-deriving the doubling in each map case"
        }
      ],
      "unchanged": [
        "core/reader_budget_accounting_test.go TestGuardedRead_ExactCharges/linked-list-cells - both arms gain identical output charges, delta stays 40*listCellBytes = 1280",
        "core/reader_budget_accounting_test.go TestGuardedRead_ExactCharges/flat-list-under-the-threshold - delta stays 0",
        "core/reader_budget_accounting_test.go TestGuardedRead_ConstructionStorage - every want is a '>=' floor and totals only grow",
        "core/reader_budget_accounting_test.go TestGuardedRead_AllowanceBoundary - totals are self-derived from admittedForSource",
        "core/reader_budget_accounting_test.go TestGuardedRead_PoolReuseChargesTheSameConstruction - self-derived from read 1",
        "core/reader_budget_accounting_test.go TestGuardedRead_MapConstructionContracts - shape assertions, no arithmetic",
        "core/reader_budget_accounting_test.go TestGuardedRead_StatsUnchanged - ReaderStats is unchanged by this model",
        "core/reader_budget_admission_test.go TestGuardedRead_TokenPlanAdmission/wide-flat-form - 4003 tokens * 32 = 128096 > 1024, refused during counting before any output charge",
        "core/reader_budget_admission_test.go TestGuardedRead_UnreadSuffixDoesNotRaiseAdmission - refused during counting",
        "core/reader_budget_admission_test.go TestGuardedRead_EscapedPayloadAdmission/reserves-the-payload-before-decoding - refused by checkPlan during counting",
        "core/reader_budget_admission_test.go TestGuardedRead_NumericConversionStorage/overflowing-token-refused-before-conversion - 112 admitted + 1056 conversion storage > 1024, still refused",
        "core/reader_budget_admission_test.go TestReaderPlanArithmetic_RefusesOverflow - drives checkedTokenPlanBytes/checkedConversionBytes directly",
        "core/reader_budget_admission_test.go TestGuardedRead_InvalidNumberDiagnosticIsBounded - runs under DefaultMaxAllocationBytes",
        "core/reader_budget_cancel_test.go, all tests - reduction assertions with '>=' floors, all running under DefaultMaxAllocationBytes",
        "core/reader_context_parity_test.go - legacy parity, no charge arithmetic"
      ],
      "newRedTests": [
        {
          "name": "TestGuardedRead_AdmitsOutputStorage",
          "file": "core/reader_budget_accounting_test.go",
          "asserts": "for each of (a b c), 'a, \"ab\", listSource(40), pairSource(4): admittedForSource(src) equals the full frozen sum T1..T6, and the sum's output component equals ReaderAllocationBytes of the legacy read of the same source"
        },
        {
          "name": "TestGuardedRead_AdmittedBytesNeverDecrease",
          "file": "core/reader_budget_accounting_test.go",
          "asserts": "AllocationBytes probed at every terminal-state check across a read mixing escaped strings, zero-copy strings, a promoted map and a linked list never decreases"
        },
        {
          "name": "TestGuardedRead_FailedConversionKeepsItsNodeCharge",
          "file": "core/reader_budget_admission_test.go",
          "asserts": "a malformed numeric token's addNode charge stays on the ledger after the conversion fails - no credit on failure"
        },
        {
          "name": "TestGuardedRead_WorkspaceHighWaterSpansNesting",
          "file": "core/reader_budget_accounting_test.go",
          "asserts": "({...3 pairs...} [a a a a]) admits workBufferBytes(5), not workBufferBytes(4) - Parser.nodes is one shared schedule across nesting"
        }
      ],
      "amendRowsLiveIn": "plan.amendments (11 rows) \u2014 the single copy"
    },
    "deferredCharacterization": {
      "file": "/var/tmp/zapply/reader-budget-enforcement/deferred-eval_reader_budget_test.go.txt",
      "restoresTo": "runtime/eval_reader_budget_test.go",
      "verdict": "RETURNS AS WRITTEN. All four sub-tests hold under the frozen model.",
      "redProof": "refuses_before_the_full_parse is genuinely red on master: ChargeEvalReader is handed ~640KB against a 1024 ceiling, addCharge refuses it as oversized (n > max), and saturateCounter records the full 640KB, so admitted == full and assert.Less FAILS. After 3.1 the read is refused during counting (20004 tokens * 32 = 640128 > 1024) with admitted == 0.",
      "recommendedAddition": "one assertion, since this is task 0.2's own file and is not sealed: assert.LessOrEqual(admitted, int64(readerBudgetBytes)). 'admitted < full' passes even if nothing were metered at all; 'admitted never exceeds the ceiling it was refused under' is the property that actually distinguishes the fixed reader.",
      "nameCheck": "its package-level admittedBytes(ctx context.Context) does not collide - runtime has no such name today, and core's admittedBytes(m EvalMeter) is a different package. isResourceLimit and meteringLimits exist at runtime/resource_limits_test.go:50 and :90."
    }
  }
}
```
