## Context

`(*HashMap).trieFromBuildMap` converts `Set`-built staging storage into trie form. It obtains
two pieces of storage on one call and heads them with two different constants, three lines
apart:

```go
func (h *HashMap) trieFromBuildMap() (*hamtNode, int64) {
	entries := h.sortedEntries()
	root := &hamtNode{}
	bytes := HashMapShallowBytes(len(entries))
	for _, e := range entries {
		next, b, _ := root.assoc(e, hashOfKey(e.hk), 0)
		root, bytes = next, bytes+b
	}
	return root, bytes
}
```

The entry buffer goes through `HashMapShallowBytes`, whose header term is
`MeterHashMapHeaderBytes` (32). Every trie node the loop builds goes through `hamtSizeBytes`,
whose header term is `MeterCollectionHeaderBytes` (24). The two differ only in the header —
both price their per-entry term with `MeterHashMapEntryBytes` (64).

The 32-byte term has no row in ADR 0011's fixed size table, and the paragraph below that table
closes with a sentence that is false at this sha: *"The conversion publishes no unit of its own:
it obtains persistent-map nodes, priced by this row at their construction site, and the table
gains no row for it."* True of the nodes it describes; false of the entry buffer, which is a
separate unit charged on the same call.

Nothing is under-billed. 32 exceeds 24, so the charge fails closed. This is a defect in the
record, not in the bound.

## Decision

The evaluator's construction buffer charges `MeterCollectionHeaderBytes` (24), and ADR 0011's
table gains a row naming it.

Four reasons, in the order they carry weight:

1. **Derivability**, which is the requirement's own test. The added requirement asks that a
   reader be able to determine from the table alone which unit prices a given piece of storage.
   Under 24 the published rule is "construction headers are 24", which the promoted-map row and
   the persistent-map node row already state. Under 32 the table would have to publish "a
   `[]entry` slice is priced with the hash-map header" — a rule a reader cannot derive from the
   storage, and one that runs opposite to the Go shapes: the reader obtains a real Go map and is
   charged 24, while the evaluator obtains a plain slice and would keep being charged 32.
2. **Shape.** A Go slice header is 24 bytes. `MeterHashMapHeaderBytes` (32) prices a `*HashMap`
   value's own header, and no `*HashMap` is constructed here.
3. **It does not under-count.** `entry` is exactly 64 bytes on the 64-bit basis ADR 0011 uses
   — `hashKey` is 32 (a `uint8` padded to 8, a `uint64`, a 16-byte string header) plus two
   16-byte interface values — and `sortedEntries` allocates `make([]entry, 0, h.Len())`, so
   capacity equals n and the nominal footprint is 24 + 64n exactly. The charged term equals it
   rather than exceeding it. That is not a weakening: ADR 0011's own list/vector bullet already
   reads 24 as *one slice header*, an exact term rather than a conservative one, and the
   allocator rounds the 64n payload up to a size class above the charge. The 8 bytes this change
   removes were the whole margin, and they were margin against the wrong storage — a hash map's
   header, not a slice's.
4. **One evaluator construction header rather than two.** The ADR states a single rule instead
   of a rule plus an exception.

Rejected: keep `MeterHashMapHeaderBytes` (32). Its only argument is that the buffer is sized
from a hash map's contents — which prices provenance rather than storage. Recorded so, and cited
by the table row.

The decision is ruled here rather than left to the run because a plan cannot carry two
executable branches: the seam's red status, its assertion, its budgets and the CHANGELOG's
existence all turn on it. The evidence it turns on — the classification of every non-test call
site of `HashMapShallowBytes` and `MeterCollectionHeaderBytes`, and the conversion charge
re-measured at n = 9, 100 and 1000 — was collected before the ruling and is restated in seam
S1's budgets. Task 1.1 records the ruling against what task 0.1 measures. A survey result that
contradicts the ruling's premises stops the run; it does not license a fresh decision mid-flight.

## What the evidence showed

Both sites originally offered as precedent price **retained** storage instead. `core/reader.go`'s
promotion header sits above `make(map[hashKey]entry, ...)`, which becomes the map's `large.m`;
`core/eval.go`'s quasiquote List-arm header sits above the `[]Value` returned as `NewList(result)`,
where header plus slots reassemble `ListShallowBytes`. Both are already owned by existing rows.

What *does* price storage discarded inside one call is `hamtSizeBytes`, on this very path. Every
branch of `(*hamtNode).assoc` is billed a fresh copy of the root-to-leaf path; the next insertion
supersedes those copies and the finished trie does not hold them. They are headed with 24, in the
same loop, three lines below the buffer that is headed with 32.

The reader's own work buffers — `nodePlan`, `formPlan`, `entryPlan` — are discarded at return too,
but they carry no header at all: `growthPlan.admit` charges capacity times unit and nothing more.
They are priced per slot by the *Reader workspace slot* and *Reader map entry slot* rows, and they
neither settle the header question nor contradict it. That distinction goes into the ADR beside the
new row, so a reader of the requirement is not left asking why those buffers charge no header.

The precise claim, then: **exactly one header-bearing charge for storage discarded at return has no
row in the published table.** The spec's same-role requirement binds few sites today and its
scenario is evaluator-scoped, which keeps the reader's header-free buffers outside enforcement.
`TestHashMap_ConversionBufferUnit` enforces it the only way one site can be enforced — by pinning
that site to the shared header constant rather than to a private literal, so a future divergence
fails a test instead of passing quietly.

## Consequences

A conversion charges 8 bytes less, at every size — a fixed shift, not a per-entry one. Measured
at this sha, an `Assoc` that converts a 1000-entry builder-form map charges 1145936 bytes; after
the change, 1145928.

No test constant moves. `fanOutBaselineCharge` is checked under a ±10% tolerance that absorbs the
shift at every size by three orders of magnitude, and `fanOutFirstLedgerFloor`,
`fanOutMaxChargeAfterFirst`, `fanOutLaterLedgerCeiling` and `fanOutCeilingTotal` all hold unchanged.

Two things do move, and both **pass stale** rather than failing, so no command catches either: the
honesty sub-arm's `floor := retainedTrieBytes(root) + HashMapShallowBytes(size.n)`, which only
widens a bound the charge already clears, and the figure `1145936` quoted in
`fanOutLaterLedgerCeiling`'s doc comment, which no test reads. Both are named sites of the chunk
that makes the change.

No gold-set fixture builds a map above `hashMapSmallLimit`, so no fixture reaches
`trieFromBuildMap` in either evaluator mode and ADR 0008's gate is unaffected. The expected delta
on both the bytes and the allocation-count axes is zero; a non-zero one names a fixture that
reached the conversion and stops the run.

Two identifiers a test might reach for are already taken in package `core` with unrelated
meanings: `entryBufferBytes` (`core/reader_budget_accounting_test.go`) and `conversionBytes`
(`core/reader_budget_admission_test.go`). Redeclaring either does not compile, and `go build ./...`
skips `_test.go`, so it would surface only at test time.

## Plan appendix

```json
{
  "v": 2,
  "change": "conversion-buffer-charge-unit",
  "baseSha": "cbbb0f34418dd3d17ab7fb8b27f33918d2c688b7",
  "generatedAt": "2026-09-10T20:13:39.313840Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "chunks": [
    {
      "id": "c1",
      "taskIds": [
        "0.1",
        "1.1"
      ],
      "prev": null,
      "parallel": false,
      "sharedPkg": null,
      "seam": "S1-unit-survey-and-decision",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "redTasks": [],
      "codeTasks": [
        "0.1",
        "1.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -run '^$' -bench '^BenchmarkHashMapFanOutAssoc$/builder/first' -benchmem -benchtime=50x ./core && GOLDSET_MODE=vm go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset && GOLDSET_MODE=eval go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset && git diff --quiet -- core plugins runtime docs",
      "coder": "go-coder",
      "sites": [
        {
          "task": "0.1",
          "file": "core/metering.go",
          "symbol": "HashMapShallowBytes",
          "anchor": "func HashMapShallowBytes(n int) int64 {",
          "change": "read only: the 32-byte header term, the 64-byte per-pair term, and the n <= 0 clamp the survey classifies"
        },
        {
          "task": "0.1",
          "file": "core/types.go",
          "symbol": "(*HashMap).trieFromBuildMap",
          "anchor": "bytes := HashMapShallowBytes(len(entries))",
          "change": "read only: the header-bearing charge for storage discarded at return that no table row names"
        },
        {
          "task": "0.1",
          "file": "core/types.go",
          "symbol": "hamtSizeBytes",
          "anchor": "return MeterCollectionHeaderBytes +",
          "change": "read only: the 24-byte header the same loop charges for its path copies, which are discarded at return as well and are already table-owned"
        },
        {
          "task": "0.1",
          "file": "core/reader_budget.go",
          "symbol": "growthPlan.admit",
          "anchor": "func (g *growthPlan) admit(b *readerBudget, n int64) error {",
          "change": "read only: the reader's scratch buffers charge capacity times unit with no header term, so they are neither precedent nor counter-example"
        },
        {
          "task": "0.1",
          "file": "core/reader.go",
          "symbol": "Parser.mapSet promotion branch",
          "anchor": "if err := p.budget.admitAlloc(MeterCollectionHeaderBytes); err != nil {",
          "change": "read only: heads retained promotion storage, already owned by the promoted-map construction header row"
        },
        {
          "task": "0.1",
          "file": "core/eval.go",
          "symbol": "engine.expandQuasiquote, List arm",
          "anchor": "if err := st.chargeAllocBytes(MeterCollectionHeaderBytes); err != nil {",
          "change": "read only: heads the []Value returned as NewList(result), so header plus slots reassemble ListShallowBytes — the List header row, not a construction buffer"
        },
        {
          "task": "0.1",
          "file": "core/bench_test.go",
          "symbol": "BenchmarkHashMapFanOutAssoc, builder/first",
          "anchor": "b.ReportMetric(float64(charge)/float64(b.N), \"charge/op\")",
          "change": "read only: the sub-arm that measures the conversion; it rebuilds the receiver with the timer stopped, so charge/op is the exact per-conversion charge. It reports the first-Assoc charge, not trieFromBuildMap's return"
        },
        {
          "task": "0.1",
          "file": "internal/goldset/bench_test.go",
          "symbol": "BenchmarkGoldsetParse",
          "anchor": "func BenchmarkGoldsetParse(b *testing.B) {",
          "change": "read only: capture its B/op in both evaluator modes at this sha. It has no in-tree pin, so task 3.4's comparison has no base unless it is taken before the code change lands"
        },
        {
          "task": "1.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Why these values are conservative",
          "anchor": "### Why these values are conservative",
          "change": "read only: the fail-closed direction the chosen unit must still satisfy, and the list/vector header bullet that already reads 24 as one slice header"
        }
      ]
    },
    {
      "id": "c2",
      "taskIds": [
        "2.1",
        "2.2"
      ],
      "prev": "c1",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "S2-conversion-buffer-unit",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "redTasks": [
        "2.1"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestHashMap_ConversionBufferUnit"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./core",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./core",
      "coder": "go-coder",
      "sites": [
        {
          "task": "2.1",
          "file": "core/hashmap_test.go",
          "symbol": "TestHashMap_ConversionBufferUnit",
          "anchor": "func TestHashMap_ConversionChargeIsReproducible(t *testing.T) {",
          "change": "adds TestHashMap_ConversionBufferUnit beside the existing conversion suite: at n = 9, 100 and 1000 it takes a fresh setBuiltMap receiver, calls trieFromBuildMap once for the charge C, replays the same insertion over sortedEntries to accumulate the path-copy sum P, and requires C - P == MeterCollectionHeaderBytes + int64(n)*MeterHashMapEntryBytes. C is trieFromBuildMap's return, which is smaller than the first-Assoc figures"
        },
        {
          "task": "2.2",
          "file": "core/types.go",
          "symbol": "(*HashMap).trieFromBuildMap",
          "anchor": "bytes := HashMapShallowBytes(len(entries))",
          "change": "becomes bytes := MeterCollectionHeaderBytes + int64(len(entries))*MeterHashMapEntryBytes"
        },
        {
          "task": "2.2",
          "file": "core/hashmap_test.go",
          "symbol": "TestHashMap_ConversionChargeIsReproducible, honesty sub-arm",
          "anchor": "floor := retainedTrieBytes(root) + HashMapShallowBytes(size.n)",
          "change": "the floor expression moves to the same unit: retainedTrieBytes(root) + MeterCollectionHeaderBytes + int64(size.n)*MeterHashMapEntryBytes. Left stale it passes rather than fails, so nothing else catches it"
        },
        {
          "task": "2.2",
          "file": "core/hashmap_test.go",
          "symbol": "fanOutLaterLedgerCeiling doc comment",
          "anchor": "bytes for the first update against 10216 for the rest, and leaves headroom",
          "change": "the figure quoted one line above becomes 1145928. No test reads a comment, so nothing else catches it"
        }
      ]
    },
    {
      "id": "c3",
      "taskIds": [
        "3.1",
        "3.2"
      ],
      "prev": null,
      "parallel": true,
      "sharedPkg": null,
      "seam": "S3-published-record",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "redTasks": [],
      "codeTasks": [
        "3.1",
        "3.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "openspec validate conversion-buffer-charge-unit --strict --json && git diff --stat -- docs/adr/0011-reduction-and-allocation-metering.md CHANGELOG.md",
      "coder": "zdocs",
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "fixed size table, last row",
          "anchor": "| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |",
          "change": "one row added after it for the evaluator's construction buffer: 24 bytes once per builder-to-trie conversion, plus 64 per key/value pair, following the table's existing phrasing"
        },
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "paragraph below the table, reader-scoping sentence",
          "anchor": "The promoted-map construction header is `MeterCollectionHeaderBytes` (24) for the storage the reader's map builder obtains",
          "change": "widens to one construction header at 24 for both the reader's map builder and the evaluator's conversion buffer, keeping the existing contrast against the 32-byte output term of HashMapShallowBytes, and adds the clause that distinguishes them from the reader's own work buffers: those are read-scoped and priced per slot with no separate header"
        },
        {
          "task": "3.1",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "paragraph below the table, no-row claim",
          "anchor": "The conversion publishes no unit of its own: it obtains persistent-map nodes, priced by this row at their construction site, and the table gains no row for it.",
          "change": "corrected: the conversion obtains two pieces of storage on one call — nodes, owned by the persistent-map node row, and an entry buffer, owned by the added construction-buffer row"
        },
        {
          "task": "3.2",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased] / Changed",
          "anchor": "## [Unreleased]",
          "change": "one entry under the existing ### Changed heading: the conversion's entry buffer now charges the 24-byte construction header the reader's builder and the trie nodes already charge, so a conversion costs 8 bytes less whatever its size. No total, which depends on the key set"
        }
      ]
    },
    {
      "id": "c4",
      "taskIds": [
        "3.3",
        "3.4"
      ],
      "prev": "c2",
      "parallel": false,
      "sharedPkg": "core",
      "seam": "S4-verification-floor",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "redTasks": [],
      "codeTasks": [
        "3.3",
        "3.4"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "test -z \"$(grep -F 'and the table gains no row for it' docs/adr/0011-reduction-and-allocation-metering.md)\" && go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -race -timeout 2m -p 2 -parallel 2 ./core && golangci-lint run ./core/... && make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -timeout 2m -run '^TestGoldsetVMAllocations$' ./internal/goldset && GOLDSET_MODE=vm go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset && GOLDSET_MODE=eval go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset && openspec validate conversion-buffer-charge-unit --strict --json",
      "coder": "go-tester",
      "sites": [
        {
          "task": "3.3",
          "file": "core/hashmap_test.go",
          "symbol": "fanOutBaselineCharge",
          "anchor": "var fanOutBaselineCharge = map[int]int64{9: 4304, 100: 101264, 1000: 1145784}",
          "change": "read only: reconcile against the post-change figures. withinOneTenth absorbs the 8-byte shift at every size, so no constant is edited"
        },
        {
          "task": "3.3",
          "file": "plugins/stdlib/monotonic_test.go",
          "symbol": "TestAssocMonotonic_ChargesPerCallHonestly",
          "anchor": "if maxDelta >= core.HashMapShallowBytes(n) {",
          "change": "read only: classify — it bounds relatively and pins no exact byte count, so it does not move"
        },
        {
          "task": "3.3",
          "file": "runtime/map_conversion_refusal_test.go",
          "symbol": "bulkBuiltMap / TestMetering_RefusedConversionStillSettlesTheCharge",
          "anchor": "func bulkBuiltMap(t *testing.T, n int) *core.HashMap {",
          "change": "read only: classify — this one does cross a conversion, so it is the first place outside core a missed expectation would surface"
        },
        {
          "task": "3.4",
          "file": "internal/goldset/alloc_test.go",
          "symbol": "vmAllocCeilings",
          "anchor": "var vmAllocCeilings = map[string]int{",
          "change": "read only: expected zero delta, self-checking against in-tree pins. A non-zero one names the fixture that reached the conversion and stops the run"
        },
        {
          "task": "3.4",
          "file": "internal/goldset/bench_test.go",
          "symbol": "BenchmarkGoldsetParse",
          "anchor": "func BenchmarkGoldsetParse(b *testing.B) {",
          "change": "read only: re-run in both evaluator modes and compare against the figures c1 captured at cbbb0f3"
        },
        {
          "task": "3.3",
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "the sentence c3 deletes",
          "anchor": "and the table gains no row for it",
          "change": "read only: the verify command greps for it and refuses to run the floor while it is still present, which is what makes the docs shard's merge a precondition rather than a convention"
        }
      ]
    }
  ],
  "seams": [
    {
      "id": "S1-unit-survey-and-decision",
      "tasks": [
        "0.1",
        "1.1"
      ],
      "summary": "NO-RED-WAIVER: this seam produces a survey and a record, and changes no Go. It classifies every non-test call site of HashMapShallowBytes and MeterCollectionHeaderBytes as pricing a value, pricing storage discarded at return, or pricing scratch with no header at all; names which the ADR 0011 table accounts for; re-measures the conversion charge at n = 9, 100 and 1000 off BenchmarkHashMapFanOutAssoc's builder/first sub-arm; and captures the pre-change BenchmarkGoldsetParse figures that task 3.4 compares against, which cannot be captured once S2 has landed. It then writes down the unit decision with the rejected option's reason. The plan already rules the decision (converge on MeterCollectionHeaderBytes, 24) on this same evidence, so 1.1 records that ruling against what 0.1 measures rather than reopening it; a survey that contradicts the ruling's premises is a stop, not a licence to re-decide.",
      "contract": {
        "states": [
          "prices-value",
          "prices-scratch",
          "prices-scratch-headerless",
          "table-owned",
          "table-unowned",
          "decision-recorded"
        ],
        "transitions": [
          {
            "input": "a HashMapShallowBytes call whose result is charged for a *HashMap the call returns or binds",
            "state": "prices-value",
            "effect": "no-op",
            "evidence": "anchors `return &HashMap{entries: entries}, HashMapShallowBytes(len(entries)), nil` in core/types.go (three occurrences: Assoc overwrite, Assoc insert, Dissoc), `st.chargeAllocBytes(HashMapShallowBytes(m.Len()))` and `st.chargeAllocBytes(HashMapShallowBytes(val.Len()))` in core/eval.go, `return HashMapShallowBytes(val.Len())` in core/metering.go, `bytes := HashMapShallowBytes(val.Len())` in core/depth.go, `n := HashMapShallowBytes(val.Len())` in core/value_walk_context.go, `vm.pendingAllocBytes(core.HashMapShallowBytes(pairCount))` in core/vm/vm.go, `charge := core.HashMapShallowBytes(len(pairs))` in core/compiler/compiler.go. Ten of eleven HashMapShallowBytes call sites."
          },
          {
            "input": "the one HashMapShallowBytes call charged for a []entry slice the function discards at return",
            "state": "prices-scratch",
            "effect": "set",
            "evidence": "anchor `bytes := HashMapShallowBytes(len(entries))` in (*HashMap).trieFromBuildMap, core/types.go. The slice comes from h.sortedEntries(), which allocates `make([]entry, 0, h.Len())`; it is unreachable once the function returns, and only the trie root it fed is retained."
          },
          {
            "input": "a hamtSizeBytes charge for a path copy the next insertion supersedes",
            "state": "prices-scratch",
            "effect": "set",
            "evidence": "anchor `return MeterCollectionHeaderBytes +` in core/types.go. Every branch of (*hamtNode).assoc is billed a fresh copy of the root-to-leaf path; the archived trie-conversion-charge-determinism task 2.2 records the gap between the conversion charge and retainedTrieBytes as exactly those discarded copies. The header they carry is MeterCollectionHeaderBytes, inside the very loop the entry buffer heads — storage discarded at return, already charged 24."
          },
          {
            "input": "a reader growthPlan admission for the node, form or map-entry buffer",
            "state": "prices-scratch-headerless",
            "effect": "no-op",
            "evidence": "growthPlan.admit charges `g.capacity * g.unit` and nothing else, core/reader_budget.go:236. nodePlan (core/reader.go:496), formPlan (:636) and entryPlan (:916) are all discarded at return, all priced per slot by the Reader workspace slot and Reader map entry slot rows, and none carries a header term. They do not bear on the header question either way."
          },
          {
            "input": "the reader's promotion header charge",
            "state": "prices-value",
            "effect": "no-op",
            "evidence": "anchor `if err := p.budget.admitAlloc(MeterCollectionHeaderBytes); err != nil {` in core/reader.go heads `make(map[hashKey]entry, ...)`, which becomes the HashMap's large.m — retained, not scratch."
          },
          {
            "input": "the quasiquote List-arm header charge",
            "state": "prices-value",
            "effect": "no-op",
            "evidence": "anchor `if err := st.chargeAllocBytes(MeterCollectionHeaderBytes); err != nil {` in core/eval.go expandQuasiquote is followed by a per-element slot charge and by `return NewList(result), nil` — header plus slots reassemble ListShallowBytes for the returned value, so it is the List header row, not a construction buffer."
          },
          {
            "input": "each classified site checked against the published fixed size table",
            "state": "table-unowned",
            "effect": "set",
            "evidence": "docs/adr/0011-reduction-and-allocation-metering.md's table carries a row for every unit above except the conversion's entry buffer; the paragraph below it asserts of the conversion that `the table gains no row for it`."
          },
          {
            "input": "the ruling written into tasks.md at 1.1 with the rejected option's reason",
            "state": "decision-recorded",
            "effect": "set",
            "evidence": "task 1.1 requires both halves; the ADR row written at 3.1 cites the rejected reason."
          }
        ],
        "forbidden": [
          "Recording the decision without recording why the other option was rejected — task 1.1 requires both halves.",
          "Quoting the archived figures instead of re-measuring. The per-value memo landed between the two changes, so a naive re-measure against a retained receiver reads a path-copy charge, not a conversion.",
          "Measuring off BenchmarkHashMapFanOutAssoc's steady sub-arm or off the post-first updates of TestHashMap_FanOutAssocStaysBounded — both observe memo hits.",
          "Conflating the first-Assoc charge with trieFromBuildMap's return. The first is the second plus the updating call's own path copy; the budgets below label them separately and c2's test asserts on the second.",
          "Re-opening the unit decision. The plan rules it; a survey result that contradicts the ruling's premises stops the run and comes back as a finding.",
          "Treating ns/op or B/op from the measurement run as a verdict. charge/op is an exact int64 mean over identical per-iteration values; the other two axes are observation on this machine.",
          "Deferring the BenchmarkGoldsetParse capture to S4. Its B/op has no in-tree pin, so once S2 lands there is no base left to compare against."
        ],
        "seeding": [
          "prices-value / prices-scratch / prices-scratch-headerless / table-owned / table-unowned: reached only by reading the anchors above out of the working tree at cbbb0f3. No test or process seeds them.",
          "The conversion charge at n: `go test -timeout 2m -run '^$' -bench '^BenchmarkHashMapFanOutAssoc$/builder/first' -benchmem -benchtime=50x ./core`, reading the charge/op metric. builder/first rebuilds a fresh Set-built receiver per iteration with the timer stopped, so charge/op is the exact per-conversion charge.",
          "The gold-set base figures: `GOLDSET_MODE=vm go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset` and `GOLDSET_MODE=eval go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset`, run at cbbb0f3 before S2 lands.",
          "decision-recorded: reached only by writing the conclusion into tasks.md at 1.1. No other artifact carries it."
        ],
        "budgets": [
          "Non-test call sites of the two surveyed symbols: HashMapShallowBytes exactly 11 (core/types.go x4, core/eval.go x2, core/depth.go, core/metering.go, core/value_walk_context.go, core/vm/vm.go, core/compiler/compiler.go); MeterCollectionHeaderBytes exactly 5 (core/reader.go, core/eval.go, core/metering.go x2 inside ListShallowBytes and VectorShallowBytes, core/types.go inside hamtSizeBytes). Sixteen in all.",
          "Among those sixteen, sites whose storage is discarded before the call returns: exactly 2, both in core/types.go and both inside trieFromBuildMap's loop — the entry buffer through HashMapShallowBytes, and the superseded path copies through hamtSizeBytes. Table-unowned among the two: exactly 1, the entry buffer; the path copies are owned by the Evaluator persistent-map node row and are already headed with 24.",
          "Header-bearing charges anywhere in the ledger for storage discarded at return that no table row names: exactly 1. The reader's own scratch buffers are discarded at return too but carry no header term at all, so they are neither precedent nor counter-example.",
          "First-Assoc charge — the conversion plus the updating call's own path copy, which is what BenchmarkHashMapFanOutAssoc/builder/first reports. Assoc: 4400 at n=9, 101328 at n=100, 1145936 at n=1000. Measured at cbbb0f3 on 2026-09-10 at -benchtime=50x and identical to the figures archived under trie-conversion-charge-determinism task 2.2. Dissoc: 4240, 101200, 1145720. Colliding pair at n=11: 4192. The charge is an exact int64 sum with no allocator input, so a re-measure that disagrees by more than 0 at any n is a finding, not noise.",
          "trieFromBuildMap's own return — the quantity c2's test asserts on, smaller than the first-Assoc charge by the updating call's path copy: 100704 at n=100 and 1144336 at n=1000, from the archived task 2.2 record. The difference is 624 at n=100 and 1600 at n=1000.",
          "Buffer term at cbbb0f3: HashMapShallowBytes(n) = 32 + 64n -> 608 at n=9, 6432 at n=100, 64032 at n=1000, 736 at n=11.",
          "Files written by this seam under core, plugins, runtime and docs: 0. The evidence lands in tasks.md, which only the orchestrator writes."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "0.1",
        "1.1"
      ]
    },
    {
      "id": "S2-conversion-buffer-unit",
      "tasks": [
        "2.1",
        "2.2"
      ],
      "summary": "existing-service-strict. A sealed test in core/hashmap_test.go isolates the conversion's buffer term from the path-copy sum and pins it to MeterCollectionHeaderBytes plus MeterHashMapEntryBytes per pair, failing at base by exactly 8 bytes at every n; the coder then edits the one anchored line and moves the honesty sub-arm's floor expression to the same unit. The floor expression is the trap: left stale it passes rather than fails, because it only widens a bound the charge already clears.",
      "contract": {
        "states": [
          "buffer-32",
          "buffer-24",
          "builder-form",
          "trie-form",
          "memo-published",
          "header-term-isolated"
        ],
        "transitions": [
          {
            "input": "trieFromBuildMap runs at base against a builder-form receiver",
            "state": "buffer-32",
            "effect": "no-op",
            "evidence": "anchor `bytes := HashMapShallowBytes(len(entries))`, with HashMapShallowBytes returning `MeterHashMapHeaderBytes + int64(n)*MeterHashMapEntryBytes` at core/metering.go:687."
          },
          {
            "input": "the decision applied at that anchored line",
            "state": "buffer-24",
            "effect": "set",
            "evidence": "the replacement is `bytes := MeterCollectionHeaderBytes + int64(len(entries))*MeterHashMapEntryBytes` — the header hamtSizeBytes already charges three lines below, for the nodes this same loop builds, with the identical per-entry term."
          },
          {
            "input": "a receiver built through Set past hashMapSmallLimit",
            "state": "builder-form",
            "effect": "set",
            "evidence": "setBuiltMap at core/hashmap_test.go:376 followed by assertBuilderForm at :391 — large != nil, large.root == nil, len(large.m) == n."
          },
          {
            "input": "a receiver built through Assoc",
            "state": "trie-form",
            "effect": "set",
            "evidence": "assocBuiltMap at core/hashmap_test.go:907. Control only: this receiver never converts, so it charges no buffer term."
          },
          {
            "input": "one Assoc against a builder-form receiver",
            "state": "memo-published",
            "effect": "set",
            "evidence": "trieRoot publishes the memo through h.large.memo.CompareAndSwap(nil, root) at core/types.go:1123 — not trieFromBuildMap, which leaves the receiver untouched. Never a seed for a charge assertion."
          },
          {
            "input": "trieFromBuildMap called directly on a fresh builder-form receiver, with the path-copy sum replayed beside it",
            "state": "header-term-isolated",
            "effect": "set",
            "evidence": "trieFromBuildMap ignores the memo and returns (root, bytes); replaying `root.assoc(e, hashOfKey(e.hk), 0)` over h.sortedEntries() in the same order reproduces the path-copy sum P alone, so C - P is the buffer's header plus per-entry term and nothing else. The replay reproduces production's own loop, so it pins the header and per-entry term only — it says nothing about whether the path-copy accounting is right, which TestHashMap_ConversionChargeIsReproducible and its honesty sub-arm already cover. It is the only isolation available: nothing else returns the buffer term separately."
          }
        ],
        "forbidden": [
          "buffer-24 together with an unchanged `retainedTrieBytes(root) + HashMapShallowBytes(size.n)` floor expression at core/hashmap_test.go:533: the ledger would charge one unit and the honesty test would measure against another, and the stale form passes rather than fails.",
          "Leaving the fanOutLaterLedgerCeiling comment at core/hashmap_test.go:800-808 quoting 1145936 for the first update. That figure becomes 1145928, and no test reads a comment.",
          "memo-published together with any conversion-charge assertion: the charge is 0 there.",
          "trie-form together with any conversion-charge assertion: that receiver never converts.",
          "Asserting that C equals the first-Assoc figures (4400 / 101328 / 1145936). Those include the updating call's own path copy; trieFromBuildMap's return is smaller.",
          "Seeding builder-form by writing h.large.m, h.large.root or h.large.memo directly. Set is the only legal producer and assertBuilderForm the only legal check.",
          "Naming anything `entryBufferBytes` or `conversionBytes`: both identifiers already exist in package core, at core/reader_budget_accounting_test.go:57 and core/reader_budget_admission_test.go:25, with unrelated meanings. Redeclaring either does not compile, and `go build ./...` will not catch it because it skips _test.go. `trieConversionBufferBytes` and `entrySliceBytes` are free at cbbb0f3.",
          "Changing HashMapShallowBytes itself, or MeterHashMapHeaderBytes: ten value-pricing call sites depend on 32, and core/metering_test.go pins the constants.",
          "Touching newTrieFromEntries at core/types.go:1074. It builds a trie from the small form's own retained entries slice, obtains no buffer, and charges none."
        ],
        "seeding": [
          "builder-form: `m := setBuiltMap(t, n)` with n > hashMapSmallLimit (8); the suite's sizes are 9, 100 and 1000.",
          "header-term-isolated: call `m.trieFromBuildMap()` once on a fresh builder-form receiver for C, and separately replay the insertion loop over `m.sortedEntries()` to accumulate P. Do not route through Assoc twice.",
          "trie-form: `assocBuiltMap(t, n)` — control only.",
          "memo-published: one `m.Assoc(...)` against a builder-form receiver. Never a seed for a charge assertion.",
          "buffer-24: reached only by editing the single line located by the string `bytes := HashMapShallowBytes(len(entries))`, per task 2.2's instruction to locate it by that string and not by a line number."
        ],
        "budgets": [
          "Buffer term after the change: 24 + 64n -> 600 at n=9, 6424 at n=100, 64024 at n=1000, 728 at n=11.",
          "Buffer term at base: 32 + 64n -> 608 / 6432 / 64032 / 736.",
          "Delta: exactly -8 bytes per conversion at every n. Fixed, not per-entry — it does not scale with the map. The replacement expression drops HashMapShallowBytes' `n <= 0` clamp at core/metering.go:684, under which the base charges 32 and the replacement 24; n = 0 is unreachable, since builder form exists only past hashMapSmallLimit (core/types.go:724) and nothing deletes from large.m, which is what makes -8 exact at every reachable n rather than an accident.",
          "First-Assoc charge after the change, assoc: 4392 at n=9, 101320 at n=100, 1145928 at n=1000. Dissoc: 4232, 101192, 1145712. Colliding pair at n=11: 4184.",
          "trieFromBuildMap's return after the change — what c2's test asserts on: 100696 at n=100 and 1144328 at n=1000, each 8 below the archived base figure.",
          "The red assertion fails at base by exactly 8 at every n, since C - P is 32 + 64n at base and the assertion demands 24 + 64n. Any other delta means the test measured something other than the buffer.",
          "fanOutBaselineCharge at core/hashmap_test.go:791 is {9: 4304, 100: 101264, 1000: 1145784}, checked by withinOneTenth: |4392-4304| = 88 <= 430, |101320-101264| = 56 <= 10126, |1145928-1145784| = 144 <= 114578. All three hold, so no baseline constant is edited.",
          "The remaining fan-out bounds, all holding unchanged: fanOutFirstLedgerFloor 500000 against 1145928 at n=1000 (core/hashmap_test.go:1199); fanOutMaxChargeAfterFirst 4096 and fanOutLaterLedgerCeiling 16384, which bound post-conversion updates this change does not touch; fanOutCeilingTotal 2<<20 across fanOutCeilingUpdates = 64 updates at :1046, which clears by roughly 860 KB at n=1000. That is the full enumeration of fan-out constants in the file.",
          "Production lines changed: exactly 1. Test lines changed outside the added test: exactly 2 — the honesty floor expression at :533 and the fanOutLaterLedgerCeiling comment figure at :805.",
          "Test constants edited: 0."
        ]
      },
      "redTasks": [
        "2.1"
      ],
      "codeTasks": [
        "2.2"
      ]
    },
    {
      "id": "S3-published-record",
      "tasks": [
        "3.1",
        "3.2"
      ],
      "summary": "NO-RED-WAIVER: prose deliverables, no assertion of its own. Adds the evaluator construction buffer's row to ADR 0011's fixed size table, corrects the two sentences below the table that the row contradicts, states the distinction between call-scoped evaluator construction buffers and read-scoped reader work buffers, and adds one CHANGELOG entry under [Unreleased]. Both ADR sentences are already inaccurate at cbbb0f3, independently of this change: one scopes the 24-byte construction header to the reader's builder alone, the other says the conversion gains no table row.",
      "contract": {
        "states": [
          "row-absent",
          "row-present",
          "paragraph-scoped-to-reader",
          "paragraph-reconciled",
          "changelog-entry"
        ],
        "transitions": [
          {
            "input": "ADR 0011's fixed size table read at base",
            "state": "row-absent",
            "effect": "no-op",
            "evidence": "the table's last row is the Evaluator persistent-map node row and no row follows it."
          },
          {
            "input": "one row added after it for the evaluator's construction buffer",
            "state": "row-present",
            "effect": "set",
            "evidence": "requirement `Every allocation charge SHALL be composed from units the published fixed size table names`. The row names 24 bytes once per builder-to-trie conversion plus 64 per key/value pair, and follows the table's existing phrasing — evaluator-scoped rows are prefixed `Evaluator `."
          },
          {
            "input": "the sentence scoping MeterCollectionHeaderBytes to the reader's map builder, read at base",
            "state": "paragraph-scoped-to-reader",
            "effect": "no-op",
            "evidence": "`The promoted-map construction header is `MeterCollectionHeaderBytes` (24) for the storage the reader's map builder obtains; it is not the `MeterHashMapHeaderBytes` (32) output term of `HashMapShallowBytes` ...`"
          },
          {
            "input": "both sentences below the table rewritten against the added row",
            "state": "paragraph-reconciled",
            "effect": "set",
            "evidence": "the first widens to one construction header at 24 for both the reader's map builder and the evaluator's conversion buffer; the second stops claiming the conversion gains no row, and splits the conversion's two charges — nodes to the persistent-map node row, entry buffer to the added row."
          },
          {
            "input": "the reader's own work buffers, which are scratch of the same role and carry no header",
            "state": "paragraph-reconciled",
            "effect": "set",
            "evidence": "one clause states the distinction: read-scoped reader buffers are priced per slot with no separate header, as the last `Why these values are conservative` bullet already says; call-scoped evaluator construction buffers carry the 24-byte header. Without it a reader of the added requirement asks why growthPlan buffers charge no header, and a spec lens raises it."
          },
          {
            "input": "the charged value having moved",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "task 3.2. The entry goes under the existing `### Changed` heading of `## [Unreleased]`: the charge was never wrong — 32 exceeded 24 and failed closed — so what changes is which unit prices it, not a repaired defect."
          }
        ],
        "forbidden": [
          "row-present together with paragraph-scoped-to-reader: a row for the evaluator's buffer beside a paragraph asserting the table gains no row for the conversion is a self-contradicting document, which is the class of defect this change exists to remove.",
          "Justifying the row with an over-count claim. `entry` is exactly 64 bytes on ADR 0011's 64-bit basis (hashKey is 32 — uint8 padded to 8, uint64 8, string 16 — plus two 16-byte interface values, core/types.go:641 and :715) and sortedEntries allocates `make([]entry, 0, h.Len())`, so cap equals n and the nominal footprint is 24 + 64n exactly. The charged term equals it rather than exceeding it. The row's justification is that 24 is one slice header — the same reading the table's own list/vector header bullet already takes — and that the allocator rounds the 64n payload up to a size class, so the charge still does not under-count.",
          "A CHANGELOG entry stating a byte count per conversion as if it were universal: the total depends on the key set; only the 8-byte header shift is fixed.",
          "Adding a row for storage no non-test site charges, or adding more than one row: exactly one term is unowned at base.",
          "Touching the `Evaluator persistent-map node` row: it is correct and the conversion's node charges are already owned by it."
        ],
        "seeding": [
          "row-absent / paragraph-scoped-to-reader: the state of docs/adr/0011-reduction-and-allocation-metering.md at cbbb0f3.",
          "row-present / paragraph-reconciled: edits to that one file. No other document publishes the table — the ADR is the only holder across docs/ and README.md.",
          "changelog-entry: one entry under the existing `### Changed` heading inside `## [Unreleased]` in CHANGELOG.md, beside the existing per-value-conversion entry it continues."
        ],
        "budgets": [
          "Exactly 1 table row added.",
          "Exactly 2 sentences below the table corrected — the reader-scoping clause and the `the table gains no row for it` clause — plus exactly 1 clause added for the reader-buffer distinction.",
          "Exactly 2 files edited: docs/adr/0011-reduction-and-allocation-metering.md and CHANGELOG.md.",
          "CHANGELOG entries added: exactly 1, under `### Changed`.",
          "Documents publishing the fixed size table: exactly 1. No README or docs page duplicates it, so there is no second copy to drift.",
          "Go files touched by this seam: 0."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.1",
        "3.2"
      ]
    },
    {
      "id": "S4-verification-floor",
      "tasks": [
        "3.3",
        "3.4"
      ],
      "summary": "NO-RED-WAIVER and NO-TESTER-WAIVER: this seam runs the floor and records evidence; it authors no assertion of its own. It runs the core and runtime suites, the race pass over core, the linter and the make targets, then compares TestGoldsetVMAllocations and BenchmarkGoldsetParse against the base figures c1 captured, in both evaluator modes, expecting a zero delta on both axes because no gold-set fixture builds a map above hashMapSmallLimit. It also names every exact-byte expectation in core, plugins/stdlib and runtime that crossed a conversion and moved, which is expected to be the empty list.",
      "contract": {
        "states": [
          "floor-green",
          "goldset-zero-delta",
          "goldset-nonzero-delta",
          "moved-expectations-named"
        ],
        "transitions": [
          {
            "input": "the full floor chain run from the repository root once c2 and the docs shard carrying c3 have both merged",
            "state": "floor-green",
            "effect": "set",
            "evidence": "task 3.3 names the commands verbatim; the Makefile provides build, lint and test with a GOTESTFLAGS override. Nothing in the chain reads ADR 0011 or CHANGELOG.md, so c3 is not a hard dependency — but the run reports one tree, and a floor taken before the docs shard merges describes a tree that was never merged. The chunk graph cannot express two predecessors, so the verify command holds the ordering itself: its first term fails while the sentence c3 deletes is still in the ADR."
          },
          {
            "input": "TestGoldsetVMAllocations run after the change",
            "state": "goldset-zero-delta",
            "effect": "forced",
            "evidence": "no gold-set fixture builds a map above hashMapSmallLimit (8) — the largest map literal in the corpus is 3 pairs, in merge-config.lisp, and registry-fold folds three assocs onto `{}` — so no fixture reaches trieFromBuildMap in either mode and the buffer term is never charged. This test needs no captured baseline: it pins exact counts in-tree at internal/goldset/alloc_test.go:25 and fails on its own if one moves."
          },
          {
            "input": "BenchmarkGoldsetParse compared against the figures c1 captured",
            "state": "goldset-zero-delta",
            "effect": "set",
            "evidence": "task 3.4. B/op has no in-tree pin, so the comparison is against c1's capture at cbbb0f3 and nothing else; once c2 has landed there is no base left to take."
          },
          {
            "input": "any fixture whose allocation count or parse bytes moved",
            "state": "goldset-nonzero-delta",
            "effect": "set",
            "evidence": "task 3.4: a non-zero delta names the fixture that reached the conversion. It contradicts the corpus survey and is a stop, not a threshold to widen."
          },
          {
            "input": "every exact-byte expectation in core, plugins/stdlib and runtime classified as conversion-crossing or not",
            "state": "moved-expectations-named",
            "effect": "set",
            "evidence": "task 3.3's closing sentence. The inventory matters more than the floor run here, because the one expectation that had to move — the honesty floor at core/hashmap_test.go:533 — passes stale rather than failing if c2 missed it."
          }
        ],
        "forbidden": [
          "goldset-nonzero-delta reported together with floor-green as a pass: a moved fixture means the change reached storage the survey said it could not, and the run stops there.",
          "Widening a vmAllocCeilings pin to absorb a delta. The comment on that map is explicit that a number moves only with a reason, and that a fixture allocating less should tighten rather than leave headroom.",
          "Treating a TestDecodeHashMap_Scaling failure in plugins/json as this change's. It is a ratio threshold carrying roughly 0.1 of headroom that fails under load on unrelated runs; re-run it alone before reporting it, and it is not a stop for this change.",
          "Reporting a latency delta as a regression verdict. Report allocation evidence separately from timing; latency deltas under this machine's measurement floor are not a verdict.",
          "Running the goldset allocation test under -race: internal/goldset/alloc_test.go carries a `!race` build tag, so a race run silently measures nothing.",
          "Running golangci-lint with absolute paths from a foreign working directory: it reports no issues and exits 7. Run it from the repository root, or under `env -C`."
        ],
        "seeding": [
          "floor-green: run the chain from the repository root with the working tree carrying c2's and c3's edits and nothing else. The docs shard merges before this seam starts, and the verify command's first term refuses to run the floor until it has.",
          "goldset-zero-delta: the base figures come from c1's capture at cbbb0f3, on the same machine in the same session. This seam re-runs the same two commands and compares. No stash and no second worktree.",
          "moved-expectations-named: search the test files of core, plugins/stdlib and runtime for HashMapShallowBytes, MeterHashMapHeaderBytes and MeterCollectionHeaderBytes, and classify each hit as conversion-crossing or not."
        ],
        "budgets": [
          "Expected goldset delta: 0 allocations per fixture across every fixture, and 0 bytes on BenchmarkGoldsetParse, in both eval and vm modes. Any non-zero value on either axis is a stop.",
          "Expected count of exact-byte expectations that moved: 0. plugins/stdlib/monotonic_test.go and runtime/map_conversion_refusal_test.go both bound relatively and neither pins an exact byte count across a conversion.",
          "Expected count of test constants edited by this change: 0. fanOutBaselineCharge, fanOutFirstLedgerFloor, fanOutMaxChargeAfterFirst, fanOutLaterLedgerCeiling and fanOutCeilingTotal all hold at the post-change figures, as reconciled in S2's budgets.",
          "Test wall clock: every run carries -timeout 2m, matching the Makefile's GOTESTFLAGS default; the race pass is scoped to ./core only.",
          "Preconditions the verify command enforces before the floor runs: exactly 1 — the ADR sentence c3 deletes must be gone, which is true only once the docs shard has merged."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.3",
        "3.4"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "SHALL be composed from units the published fixed size table names",
      "tests": [
        "TestHashMap_ConversionBufferUnit (core): pins the conversion's buffer term to MeterCollectionHeaderBytes plus MeterHashMapEntryBytes per pair, both of which the table names",
        "prose verification under task 3.1; no executable check exists, because no test can assert a published table's completeness: no unit is published that no table row owns, and no row is added that nothing charges"
      ]
    },
    {
      "shall": "SHALL charge the same header unit for it, whatever the underlying Go type",
      "tests": [
        "TestHashMap_ConversionBufferUnit (core): the evaluator's construction buffer, a []entry slice, charges the same MeterCollectionHeaderBytes the reader's promoted map and hamtSizeBytes charge"
      ]
    },
    {
      "shall": "the table SHALL name the unit that charge is composed from and the storage it prices",
      "tests": [
        "prose verification under task 3.1; no executable check exists, because no test can assert a published table's completeness: the added row names 24 bytes once per conversion plus 64 per pair, and names the storage as the conversion's entry buffer",
        "TestHashMap_ConversionBufferUnit (core): the charged term equals what the row states, so the row and the code cannot drift apart silently"
      ]
    },
    {
      "shall": "both SHALL charge the same header term, unless the published table states the distinction",
      "tests": [
        "TestHashMap_ConversionBufferUnit (core): the pin is to the shared header constant, not a private literal, so a future divergence fails here",
        "prose verification under task 3.1; no executable check exists, because no test can assert a published table's completeness: the paragraph below the table states one construction header rather than two, and states the distinction that keeps the reader's header-free work buffers outside the rule"
      ]
    },
    {
      "shall": "SHALL still be accounted rather than only the storage it keeps",
      "tests": [
        "TestHashMap_ConversionChargeIsReproducible/honesty (core): charge > retainedTrieBytes(root) plus the buffer term, with the floor expression moved to the chosen unit by task 2.2"
      ]
    }
  ],
  "testHarness": [
    "setBuiltMap — core/hashmap_test.go:376 — builds an n-entry map through Set alone and leaves it in builder form; fatals if n <= hashMapSmallLimit.",
    "assertBuilderForm — core/hashmap_test.go:391 — asserts large != nil, large.root == nil, len(large.m) == n.",
    "retainedTrieBytes — core/hashmap_test.go:406 — sums hamtNodeBytes over a finished trie; the floor a conversion charge must exceed.",
    "findCollidingKeys — core/hashmap_test.go:545 — two distinct Int keys whose 32-bit hashes agree in every bit; fixed seed, same pair every run.",
    "assocBuiltMap — core/hashmap_test.go:907 — builds the same contents through Assoc, leaving trie form. Control arm: it never converts.",
    "withinOneTenth — core/hashmap_test.go:811 — the plus or minus 10 percent tolerance applied against fanOutBaselineCharge.",
    "fanOutBaselineCharge — core/hashmap_test.go:791 — {9: 4304, 100: 101264, 1000: 1145784}.",
    "fanOutMaxChargeAfterFirst / fanOutCeilingTotal / fanOutFirstLedgerFloor / fanOutLaterLedgerCeiling — core/hashmap_test.go:794, :798, :799, :808 — 4096, 2<<20, 500000, 16384. The full set of fan-out constants in the file.",
    "BenchmarkHashMapFanOutAssoc builder/first — core/bench_test.go:747 — rebuilds a Set-built receiver per iteration with the timer stopped and reports charge/op, the first-Assoc charge. Not trieFromBuildMap's return.",
    "hamtNodeBytes / hamtSizeBytes — core/types.go:768, :774 — production sizing for one trie node; the 24-byte header the conversion's own path copies already charge.",
    "sortedEntries — core/types.go:1055 — allocates make([]entry, 0, h.Len()) and sorts by hashKey. The storage the buffer term prices, and the deterministic order a replay must follow.",
    "entryBufferBytes — core/reader_budget_accounting_test.go:57 — TAKEN identifier in package core, meaning the reader's doubling schedule in entry units. Do not redeclare.",
    "conversionBytes — core/reader_budget_admission_test.go:25 — TAKEN identifier in package core, meaning a numeric token's temporary storage. Do not redeclare.",
    "bulkBuiltMap — runtime/map_conversion_refusal_test.go:13 — builds a Set-built map through the public runtime API; the only conversion-crossing fixture outside core.",
    "vmAllocCeilings — internal/goldset/alloc_test.go:25 — per-fixture allocation pins; the file carries a !race build tag, so a race run measures nothing."
  ],
  "floor": "go test -timeout 2m -p 2 -parallel 2 ./core ./runtime && go test -race -timeout 2m -p 2 -parallel 2 ./core && golangci-lint run ./core/... && make build && make lint && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -timeout 2m -run '^TestGoldsetVMAllocations$' ./internal/goldset && GOLDSET_MODE=vm go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset && GOLDSET_MODE=eval go test -timeout 2m -run '^$' -bench '^BenchmarkGoldsetParse$' -benchmem ./internal/goldset && openspec validate conversion-buffer-charge-unit --strict --json",
  "lenses": [
    "spec",
    "quality"
  ],
  "estimateHours": 2.4,
  "planRulings": [
    "Unit ruling: converge on MeterCollectionHeaderBytes (24). The evidence the decision turns on was collected at plan time and is restated in S1's budgets, so task 1.1 records the ruling against what 0.1 measures rather than re-deciding it. Four reasons. (1) Derivability, which is the requirement's own test: under 24 the published rule is 'construction headers are 24', which the promoted-map row and the persistent-map node row already state; under 32 the table would have to publish 'a []entry slice is priced with the hash-map header', a rule a reader cannot derive from the storage, and one that runs opposite to the Go shapes — the reader obtains a real Go map and is charged 24 while the evaluator obtains a plain slice and would keep being charged 32. (2) Shape: a Go slice header is 24 bytes, and MeterHashMapHeaderBytes (32) prices a *HashMap value's own header, of which none is constructed here. (3) It does not under-count: entry is exactly 64 bytes on ADR 0011's 64-bit basis and sortedEntries allocates with cap == n, so 24 + 64n equals the nominal footprint rather than exceeding it, and the allocator rounds the 64n payload up to a size class above that. An exact header term is the reading ADR 0011 already takes — its list/vector bullet reads 24 as one slice header — so this is the table's own convention, not a weakening of it. (4) It leaves ADR 0011 with one evaluator construction header instead of two.",
    "Rejected: keep MeterHashMapHeaderBytes (32). Its only argument is that the buffer is sized from a hash map's contents, which prices provenance rather than storage, and it would make the ADR publish a distinction running opposite to the Go shapes. This reason is what task 1.1 records and what the ADR row at 3.1 cites.",
    "Scope of the 'only site' claim, stated precisely because an earlier draft overstated it. Two sites price storage discarded before the call returns, and both are inside trieFromBuildMap's own loop: the entry buffer through HashMapShallowBytes, and the superseded path copies through hamtSizeBytes. The second is already table-owned and already headed with 24 — which is an argument for the ruling, not against it. Separately, the reader's node, form and map-entry buffers are discarded at return too, but growthPlan.admit charges capacity times unit with no header term at all (core/reader_budget.go:236), so they are neither precedent nor counter-example. The exact claim is: exactly one header-bearing charge for storage discarded at return has no row in the published table.",
    "The spec's same-role requirement therefore binds few sites today, and its scenario is evaluator-scoped ('another call in the same evaluator'), which keeps the reader's header-free buffers outside enforcement. TestHashMap_ConversionBufferUnit enforces it the only way it can be enforced — by pinning the one site to the shared header constant rather than to a private literal, so a future divergence fails a test rather than passing silently. Task 3.1 states the distinction in the ADR so the omission is a recorded decision.",
    "Requirement coverage: the delta spec carries seven SHALL clauses folded into five sentence blocks, and the plan maps the five blocks. The two clauses not separately listed — 'a reader SHALL be able to determine from that table alone...' and 'permitted only where the published table states the distinction...' — are each the second half of a mapped block and are covered by that block's entry. Three of the five mappings rest on prose verification under task 3.1 rather than on a test, and are labelled so: no test can assert a published table's completeness.",
    "The honesty sub-arm's floor expression at core/hashmap_test.go:533 and the fanOutLaterLedgerCeiling comment figure at :805 are the two existing places that must move, and both pass stale rather than failing — one widens a bound the charge already clears, the other is a comment. That is why both are named sites of c2 and neither is left to the floor run.",
    "c2's red test pins the header and per-entry term only. Replaying root.assoc over sortedEntries reproduces production's own loop, so C - P would keep passing if the path-copy accounting itself moved. That is not a defect here: it is the only isolation available, since nothing returns the buffer term separately, and the path-copy sum is already covered by TestHashMap_ConversionChargeIsReproducible and its honesty sub-arm. The replay is exact because trieFromBuildMap leaves the receiver untouched and publishes no memo — that happens in trieRoot at core/types.go:1123 — and sortedEntries re-sorts deterministically.",
    "The doc comment above trieFromBuildMap is deliberately left alone. Its sentence 'the entry buffer that ordering obtains is charged alongside the path copies' names no unit and stays true at 24, so there is nothing to correct; the unit is published in the ADR, not in the comment.",
    "The CHANGELOG entry goes under ### Changed, not ### Fixed. The charge was never wrong — 32 exceeded 24 and the ledger failed closed — so what changes is which unit prices the storage, not a repaired defect.",
    "Ordering: c3 runs in the docs shard in parallel with c2, since its content is fixed by this ruling and does not depend on c2's output. c4's prev names c2 because they share package core, but the floor is taken only after the docs shard has merged as well — nothing in the floor chain reads ADR 0011 or CHANGELOG.md, so this is a reporting requirement rather than a dependency. A chunk names one prev, so the graph cannot hold it; c4's verify command does instead, by refusing to start while the sentence c3 deletes is still in the ADR. Re-guard every sealed chunk after the docs shard merges: a shard merge moves the tree and stales guards taken before it.",
    "The pre-change BenchmarkGoldsetParse capture sits in c1 rather than c4, even though task 3.4 owns the comparison, because B/op has no in-tree pin and c4 runs after c2 has landed. TestGoldsetVMAllocations needs no such capture — it pins exact counts in-tree and fails on its own.",
    "Verified at cbbb0f3 and recorded so no stage re-derives it: the 11 non-test HashMapShallowBytes sites and 5 non-test MeterCollectionHeaderBytes sites match the counts above exactly; core/reader.go:989 heads the map that becomes large.m and core/eval.go:1085 heads the slice returned as NewList(result) with per-element slots charged alongside, so both are table-owned; withinOneTenth absorbs every post-change figure against fanOutBaselineCharge (88 <= 430, 56 <= 10126, 144 <= 114578); fanOutFirstLedgerFloor 500000 is met by 1145928 at :1199; fanOutMaxChargeAfterFirst 4096 and fanOutLaterLedgerCeiling 16384 bound only post-conversion updates; fanOutCeilingTotal 2<<20 clears by roughly 860 KB across 64 updates at :1046; the red assertion fails at base by exactly 8 at every n; entryBufferBytes and conversionBytes are taken in package core while trieConversionBufferBytes and entrySliceBytes are free; no goldset fixture exceeds 3 map pairs, so the zero-delta expectation holds; plugins/stdlib/monotonic_test.go and runtime/map_conversion_refusal_test.go both bound relatively; CHANGELOG.md already carries ### Changed under ## [Unreleased].",
    "Out of scope, carried from the trie-conversion-charge-determinism run and not re-opened here: the ledger charges 1.7 to 1.9 times below the allocator's real B/op on the trie path. This change widens that gap by 8 bytes per conversion — 0.001 percent of the gap at n=1000, measured 1145936 charged against 1922851 B/op — and moves neither its direction nor its order."
  ],
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
