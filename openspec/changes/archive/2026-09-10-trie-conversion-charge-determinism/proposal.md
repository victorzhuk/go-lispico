## Why

The allocation charge for converting a builder-form map to trie form is not
reproducible. Measured at `d185d86` with an unchanged benchmark, three consecutive
runs of one `Assoc` against a 100-key `Set`-built receiver charged **74 224, 79 200
and 94 112 bytes** — a ~25% spread on identical input. At n=1000 the same benchmark
moved between 1 071 440 and 1 084 640.

The cause is one line. `trieFromBuildMap` (`core/types.go:1094`) iterates
`h.large.m`, a Go map, and Go randomises map iteration order on every range. The
finished trie is identical whatever the order — position is a function of the hash —
but the path copies made getting there are not, and `trieFromBuildMap` sums exactly
those intermediate allocations as its charge.

This matters beyond tidiness, because metering is a contract this project sells:

- `CLAUDE.md` states evaluation is deterministic for the same input and environment,
  and ADR 0011 describes its charge terms as deterministic. Neither holds here.
- ADR 0008's consumer gate compares allocation figures between releases. A term that
  moves 25% on its own makes a threshold on that axis unreadable — a real regression
  and a lucky run are indistinguishable.
- An embedder sizing `DefaultMaxAllocationBytes` against observed usage cannot, since
  the same workload bills differently each process.

It was found while baselining `hashmap-builder-trie-conversion`, which rewrites this
exact function. That change is paused on it: its whole argument is a before/after on
allocation, and neither side of the comparison currently reproduces.

The scope really is one function. `eachRaw` (`core/types.go:1040`) also walks the Go
map, but its callers are membership tests (`equals_bounded.go:69`), a depth walk
(`depth.go:230`) and `sortedEntries`, which sorts — none of them charge by traversal
order.

## What Changes

- Make the conversion's charge a function of the map's contents alone, not of the
  order Go happened to iterate it in.
- Pin it with a test that converts one value repeatedly and requires every charge to
  be identical — the defect is observable in a single process, because the order is
  re-randomised per range, not per process.
- State the invariant as a requirement. It is assumed by ADR 0008's gate, by ADR
  0011's tables and by the kernel invariants, and asserted by none of them, which is
  why a 25% swing survived in a metered path.
- No change to what the conversion produces: the resulting trie, `Len`, iteration
  order, equality and printing are untouched. Only the number charged for building it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: an allocation charge is reproducible for the same input, and the
  builder-to-trie conversion meets that.

## Impact

Affected code: `core/types.go` (`trieFromBuildMap`, and whatever it needs from
`hamtNode.assoc`), plus a test in `core`.

The charge's *value* will move for every caller of `Assoc`/`Dissoc` on a builder-form
map, because a reproducible number cannot also be the current unstable one. That is an
ADR 0011 note and a `CHANGELOG.md` entry, sized once the mechanism is chosen.

Mechanism is deliberately not fixed here — it is measurement-gated, and there are at
least three shapes with different costs: insert in a sorted order; build the trie in
one order-independent pass, which would also cut the ~6 480 allocations at n=1000 by
roughly two orders of magnitude; or charge the finished structure rather than the
intermediates, which is the smallest change but under-bills the garbage the build
actually produces. The design stage picks one on evidence.

Blocks `hashmap-builder-trie-conversion`, which is paused after its first chunk and
resumes once this lands, with a re-baseline. Testing mode: existing-service-strict.
