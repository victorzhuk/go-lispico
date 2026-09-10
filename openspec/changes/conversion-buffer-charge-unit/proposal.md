## Why

`trie-conversion-charge-determinism` made `(*HashMap).trieFromBuildMap` charge the entry
buffer its deterministic insertion order obtains, as `HashMapShallowBytes(n)` —
`MeterHashMapHeaderBytes` (32) plus 64 per pair, at the line `bytes := HashMapShallowBytes(len(entries))`. That closed the
determinism defect. It also introduced the ledger's first use of `HashMapShallowBytes` to
price storage that is not a `HashMap` value: the other non-test sites all price a map being
constructed or returned (three sites in `core/types.go`'s small-map paths, two in `core/eval.go`, one each in
`core/vm/vm.go` and `core/compiler/compiler.go`).

The storage it prices is a `[]entry` slice, and the sharpest evidence sits inside
`trieFromBuildMap` itself. That one function obtains two pieces of storage three lines
apart and heads them with two different constants: the entry buffer through
`HashMapShallowBytes`, whose header term is `MeterHashMapHeaderBytes` (32), and then every
trie node its loop builds through `hamtSizeBytes`, whose header term is
`MeterCollectionHeaderBytes` (24). The two differ *only* in the header — both price their
per-entry term with `MeterHashMapEntryBytes` (64). The evaluator already has a
construction-header constant on this exact path, and the buffer does not use it.

The two other sites charging `MeterCollectionHeaderBytes` for construction both price storage
that is **retained**: `core/reader.go` heads the promoted `map[hashKey]entry` that becomes
`large.m`, and `core/eval.go` heads the `[]Value` returned as `NewList(result)`, where header
plus per-element slots reassemble `ListShallowBytes(n)` for the returned value. Both are
already owned by existing table rows.

What does price storage discarded inside one call is `hamtSizeBytes`, on this very path:
every branch of `(*hamtNode).assoc` is billed a fresh copy of the root-to-leaf path, the next
insertion supersedes those copies, and the finished trie does not hold them. That charge heads
them with 24. The reader's own work buffers are discarded at return too, but they carry no
header at all — a `growthPlan` admits capacity times unit and nothing more — so they neither
settle the question nor contradict it. The entry buffer is the only *header-bearing* charge
for storage discarded at return that the published table does not name, which is what makes
this a decision rather than an oversight.

The published table carries no row for it. Worse, the paragraph below the table now closes
"The conversion publishes no unit of its own ... and the table gains no row for it" — true
of the trie nodes it describes, and false of the entry buffer, which is a separate unit on
the same call. That sentence is this change's to correct whichever unit is chosen.

Nothing is under-billed: 32 exceeds 24, so the current charge fails closed and no ceiling
is wrong. This is a defect in the record, not in the bound.

Found by the spec and quality review lenses of `trie-conversion-charge-determinism`, which
recorded it as a warning. That change's design had chosen `HashMapShallowBytes` deliberately
and the decision was not reopened mid-run.

## What Changes

- Decide which unit prices an evaluator-side construction buffer, and apply that decision at
  the line `bytes := HashMapShallowBytes(len(entries))` in `(*HashMap).trieFromBuildMap`.
- Give the decision a row in ADR 0011's fixed size table, so no charged term is unnamed and
  a reader can derive from the table which unit prices a given piece of storage.
- Re-record the conversion's charge figures if the unit moves. The current figures are
  stated under task 2.2 of
  `openspec/changes/archive/2026-09-10-trie-conversion-charge-determinism/tasks.md`.

Mechanism is not fixed here. Two shapes exist. Converge on `MeterCollectionHeaderBytes`
(24) — the header `hamtSizeBytes` already uses for the nodes this very function builds —
which makes the conversion charge 8 bytes less per conversion, at every n, and lets ADR 0011
state one evaluator construction header rather than two. Or keep `MeterHashMapHeaderBytes`
(32) and state in the ADR why a `[]entry` buffer sized from a hash map is priced as a hash
map while the nodes built from it are not. They differ in whether a future reader can derive
the unit from what the storage is, and in which convention the next construction site
inherits. The design stage picks one; either way ADR 0011's table gains a row and the
paragraph below it is corrected.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: every allocation unit the ledger charges is named by the published table,
  and construction storage of one role and lifetime is priced by one unit.

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

Depends on `trie-conversion-charge-determinism` and `hashmap-builder-trie-conversion`, both
archived on 2026-09-10. The second added a per-value memo, so the conversion is now charged
once per value rather than once per update: the charge this change moves is observable only
on a receiver that has not yet been converted, and `BenchmarkHashMapFanOutAssoc`'s
`builder/first` sub-arm is the shape that measures it.
