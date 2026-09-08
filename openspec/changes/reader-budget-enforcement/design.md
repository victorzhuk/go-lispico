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
