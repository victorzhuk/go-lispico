## Context

After `engine-startup-template-sharing`, `Use(stdlib)` costs 6.81µs / 42 allocs
against a < 1µs target. The plugin's `Init` no longer runs and no builtin
closure is rebuilt, so what remains is attachment: `snapshotEntries` copies the
completed layer's entry map under `RLock`, `populateTemplateBindings` merges
that copy into `e.bindings` and calls `activate`, which rebuilds the engine's
active list.

The copy has no justification left. `putEntry` is reachable only from
`RegisterValue`/`RegisterSource`, which run only during a plugin's `Init`, which
runs only inside `ensureLayer`'s single-flighted build closure. `markComplete`
runs after that closure returns, and `ensureLayer` short-circuits on `complete`
both before entering the flight and again inside it. So from the moment a layer
is complete, its entry map is never written again.

Sizing, measured rather than estimated (`GODEBUG=memprofilerate=1`): the
`snapshotEntries` map allocation is exactly 4 of the benchmark's 42 allocs/op.
The whole attach path is 13; `runtime.New` is 63% cumulative. This change is
worth ~4 allocs plus an enforced invariant — not the residual. Task 5.2 exists
to state that plainly rather than let the framing drift.

## Decision 1: publish the entry set at completion, via atomic.Pointer

Add `published atomic.Pointer[map[string]*stdlibTemplateEntry]` to
`stdlibTemplateLayer`. `markComplete` stores the entries map at the moment
`complete` flips true, inside the existing `r.mu.Lock()`. The existing
`complete` flag and every `r.mu`-guarded path keep deciding *whether to build*;
the pointer serves only the attach-read path, which loads it and returns the map
directly — no lock, no copy.

Lock-free is safe here, not merely cheaper. Inside `ensureLayer`'s singleflight
closure, `build()` runs to completion, then `markComplete` stores, then the
closure returns and unblocks every waiter. The `Store` is ordered after every
`putEntry` write on that goroutine, and `atomic.Pointer` Load/Store are
sequentially consistent, so any goroutine observing the pointer observes a fully
built map. `singleflight.Do` returns to no caller — builder or waiter — until
that closure returns, and `populateTemplateBindings`/`ForceAll` only ever run
for a key this engine's own `ensureLayer` already returned successfully for, so
`Load` cannot observe nil on any path that exists. The `disabled` fallback does
not complicate it either: `layerFor` returns `nil, false` outright while
disabled, so the read path is never entered regardless of whether a layer object
from an earlier enabled run still sits in the registry.

A layer *can* exist while unpublished — a build that failed leaves one behind,
and the registry is never pruned. The read accessor returns nil for it and the
caller's empty-map check returns early, so an unpublished layer contributes no
bindings rather than the partial ones the old copying accessor would have
handed back. That path is unreachable today, because a failed build propagates
out of `Use` before attachment runs; it is worth stating only because the
accessor's nil return is what makes it safe rather than the layer's absence.

Chosen over widening `entryFor`'s existing `RLock` to iterate in place, which
would also be correct: `r.mu` is one registry-wide lock across every
{dialect, plugin, version} key, so even a pure `RLock` bounces the same reader
count process-wide on unrelated attaches. A per-layer pointer confines each
layer's readers to that layer's own memory. The repo already uses this
publish-once idiom in `core/env.go`, `core/eval.go`, `core/vm/chunk.go`, and
`runtime/call_cache.go`; match that style, including the invariant comment.

## Decision 2: enforce the invariant, do not merely document it

`putEntry` gains a fail-closed guard: if the layer is already published, refuse
the write rather than mutate a map every attached engine is reading. The project
forbids panics, so this returns an error or drops with a log in keeping with the
surrounding code.

Enumerating today's call sites (task 2.1) is documentation and belongs in the
change, but it proves "no bug today", not "cannot regress tomorrow". The guard
plus a white-box test that calls `putEntry` on a completed layer is what makes
the invariant hold.

## Decision 3: keep `e.bindings` per-engine — for the real reason

`owned` stays a genuine per-engine map. The reason is not that shadowing,
deletion, or unload write it — they do not: `removePluginBindings` reads the
inner set and deletes the outer key, `TombstoneForDelete` writes a separate
per-engine map, and `activate`'s tombstone loop only reads.

The real reason is `applyVocabulary` (`runtime/engine.go`): under a dialect using
`WithAdapter`, it can bind into `e.rootEnv` *before* `populateTemplateBindings`
runs, so the before/after diff is already non-empty for stdlib and
`e.bindings[pluginName]` exists and gets *mutated* when template names are
merged in. Aliasing the shared set in that branch would let one engine's
adapter bookkeeping corrupt every sibling. `runtime/dialect_vocab_test.go`
exercises this path today.

That reason belongs in a code comment at the merge site, not only here — the
boundary should defend itself against a later contributor "finishing the job".

## Decision 4: how "copies nothing" is tested

A direct zero-allocation assertion on the read accessor for a complete layer,
via `testing.AllocsPerRun`. Any copy costs at least one allocation regardless of
layer size, so this discriminates at N=1.

Explicitly *not* the earlier idea of asserting flat allocation across two layer
sizes: `make(map[K]V, N)` has an N-independent allocation *count* from roughly
N=10 through N=500–1000, and stdlib's layer sits in the low hundreds. That test
would report "flat" identically whether the copy was removed or silently
reintroduced.

## Decision 5: rename the accessor

`snapshotEntries`' contract flips from "always returns a private copy" to
"returns the shared, read-only map". Keeping the name would mislead every future
reader, including `ForceAll`, which also calls it. Rename it and state in the
doc comment that the result must not be mutated.

## Risks

- **Aliasing.** One shared map read by every engine means a stray write corrupts
  all of them at once. Mitigated by the `putEntry` guard and its test, not by
  the call-site enumeration alone.
- **Attach racing unload.** Low as the code stands — `UnloadPlugin` touches only
  the calling engine and never enters the registry — but this change makes that
  guarantee load-bearing for every engine simultaneously, so it needs an
  explicit identity test rather than an assumption.
- **A later contributor sharing `owned` too**, reasoning from the wrong
  justification. Mitigated by Decision 3's code comment.
- **Diminishing returns.** ~4 allocs of 42. Recorded up front so the change is
  judged on what it is.

## Alternatives rejected

- **Copy-on-write of the entry map per engine:** the map is never written after
  completion, so copy-on-write degenerates to the copy being removed.
- **Sharing `e.bindings` too:** refuted by the `applyVocabulary` adapter path
  above.
- **Widening `entryFor`'s RLock to iterate in place:** correct, but keeps every
  attach coupled to one registry-wide lock.
