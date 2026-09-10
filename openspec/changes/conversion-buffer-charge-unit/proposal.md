## Why

`trie-conversion-charge-determinism` made `(*HashMap).trieFromBuildMap` charge the entry
buffer its deterministic insertion order obtains, as `HashMapShallowBytes(n)` —
`MeterHashMapHeaderBytes` (32) plus 64 per pair, at `core/types.go:1097`. That closed the
determinism defect. It also introduced the ledger's first use of `HashMapShallowBytes` to
price storage that is not a `HashMap` value: the other non-test sites all price a map being
constructed or returned (`core/types.go:1139`, `:1149`, `:1177`, `core/eval.go:826`,
`:1166`, `core/vm/vm.go:1294`, `core/compiler/compiler.go:1350`).

The storage it prices is a `[]entry` slice. ADR 0011's own rationale gives 24 bytes as the
price of one slice header — "`24` bytes for list/vector headers corresponds to one slice
header" — and two existing sites charge exactly that for construction storage:
`core/reader.go:986` for the reader's map builder, and `core/eval.go:1085` for the
`[]Value` a list walk fills. The ADR then says, of the reader's builder, that its 24-byte
header "is not the `MeterHashMapHeaderBytes` (32) output term of `HashMapShallowBytes`".

So the ledger now prices two construction buffers of the same role with two different
headers, and says nothing about the second. The published table names the first — "Reader
promoted-map construction header | 24 bytes, once per promoted map literal" — and carries
no row for the evaluator's. The `CHANGELOG.md` entry for the conversion fix sends readers
to ADR 0011 for the charge terms; ADR 0011 was edited only in its determinism section.

Nothing is under-billed: 32 exceeds 24, so the current charge fails closed and no ceiling
is wrong. This is a defect in the record, not in the bound.

Found by the spec and quality review lenses of `trie-conversion-charge-determinism`, which
recorded it as a warning. That change's design had chosen `HashMapShallowBytes` deliberately
and the decision was not reopened mid-run.

## What Changes

- Decide which unit prices an evaluator-side construction buffer, and apply that decision at
  `core/types.go:1097`.
- Give the decision a row in ADR 0011's fixed size table, so no charged term is unnamed and
  a reader can derive from the table which unit prices a given piece of storage.
- Re-record the conversion's charge figures if the unit moves. The current figures are
  stated under task 2.2 of
  `openspec/changes/archive/2026-09-10-trie-conversion-charge-determinism/tasks.md`.

Mechanism is not fixed here. Two shapes exist. Converge on the 24-byte slice-header unit
that `core/reader.go:986` and `core/eval.go:1085` already charge for construction storage,
which makes the conversion charge 8 bytes less per conversion and requires ADR 0011 to say
the reader's and the evaluator's builder buffers are one unit rather than two. Or keep 32
and state in the ADR why a `[]entry` buffer sized from a hash map is priced as a hash map.
They differ in whether a future reader can derive the unit from the storage's shape, and in
whether a third construction site added later has one precedent to follow or two. The design
stage picks one.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: every allocation unit the ledger charges is named by the published table,
  and storage of one shape is priced by one unit.

## Impact

Affected code: `core/types.go` (`trieFromBuildMap`'s buffer term),
`docs/adr/0011-reduction-and-allocation-metering.md` (the fixed size table and the paragraph
below it), and `CHANGELOG.md` if the charged value moves.

If the unit converges on 24, the builder-to-trie conversion charges 8 bytes less per
conversion — a fixed shift, not a per-entry one, so it does not scale with the map. No
gold-set fixture builds a map above `hashMapSmallLimit`, so no cell reaches
`trieFromBuildMap` in either evaluator mode and ADR 0008's gate is unaffected whichever way
the decision goes. The exact-byte expectations in `core`, `plugins/stdlib` and `runtime`
that cross a conversion decide the rest; the floor names them.

Depends on `trie-conversion-charge-determinism`, archived. Overlaps
`hashmap-builder-trie-conversion`, which rewrites the same function to add a per-value memo:
this change fixes which unit the buffer is charged, that one fixes how often it is charged.
Either order works, and whichever lands second inherits the other's figures.
