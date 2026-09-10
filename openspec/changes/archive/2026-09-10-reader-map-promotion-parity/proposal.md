## Why

`reader-budget-enforcement` gave the reader its own hash-map construction path so
each allocation could be admitted before it happened. That path promotes past
`hashMapSmallLimit` differently from the constructor every other caller uses, and
nothing in the repo could see the difference.

`HashMap.Set` promotes into `largeMap.m`, a plain Go map: each later insert is one
store, and `trieFromBuildMap` defers trie conversion to the first `Assoc`/`Dissoc`.
`Parser.mapSet` promotes into `largeMap.root` through `newGuardedTrie`, so each
later insert path-copies the spine through `assocGuarded`. Reading a map literal of
n keys above the limit therefore costs n path-copies of depth `ceil(log32 n)`
instead of n amortized-constant stores, and the trie shape survives the read — a
literal that is only ever read loses the builder form's constant-time
`getByHashKey` and gains nothing, because the deferred conversion it was paying
for never happens.

Three review lenses reached this independently. It is invisible to the gold set,
which builds only the small form, and to `TestReadWithContextStats_LegacyParity`,
whose widest map fixture has two keys against a threshold of eight.
`core/reader_map_parity_test.go` now pins that the two builders agree on content,
but every observable it compares is representation-independent, so the divergence
itself stays unpinned.

The duplication follows from the divergence. `newGuardedTrie`, `assocGuarded`,
`assocCollisionGuarded` and `mergeEntriesGuarded` reproduce `assoc`,
`mergeEntries` and their collision handling line for line, and the only non-test
callers of the first two are the two reader sites this change removes.

## What Changes

- Promote reader-built maps into the builder form, exactly as `HashMap.Set` does,
  so a map literal's representation does not depend on who built it.
- Charge the reader's promoted entry storage on the deterministic growth schedule
  the small-form entry buffer already uses, replacing per-trie-node admission.
- Delete `newGuardedTrie`, `assocGuarded`, `assocCollisionGuarded` and
  `mergeEntriesGuarded` once their two reader call sites are gone.
- Assert representation in `core/reader_map_parity_test.go`, so a future promotion
  or threshold change in one builder cannot diverge silently again.
- **BREAKING (charge model):** admitted totals change for map literals above
  `hashMapSmallLimit`. Trie-node units are replaced by entry-buffer units on the
  doubling schedule. Sealed expectations in `core/reader_budget_accounting_test.go`
  are amended; no limit, default or gate threshold moves.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: reader map construction charges the entry buffer rather than trie
  nodes, and a collection's representation no longer depends on which builder
  produced it.

## Impact

Affected code: `core/reader.go` (`Parser.mapSet`), `core/types.go` (delete the
guarded trie family), `core/reader_budget.go` if the entry-buffer helper moves.
`HashMap.Set`, `Get`, `Each`, `Pairs`, `Assoc`, ordering and equality contracts are
untouched, as are `ReaderStats` and the legacy context-free readers.

Update ADR 0011's T6 construction row to state the entry-buffer term for reader
map construction in place of the per-HAMT-node term, and record the charge change
in `CHANGELOG.md`.

Two follow-ups from `reader-budget-enforcement` close with this change: the
duplicated guarded builders, and the parity test's missing shape assertions. The
remaining ones — unmetered pool retention, the engine's double tokenize scan, and
the stale `ChargeEvalReader` parenthetical — are out of scope here.

Not blocked. Testing mode: existing-service-strict.
