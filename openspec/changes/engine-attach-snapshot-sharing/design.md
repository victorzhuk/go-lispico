## Context

After `engine-startup-template-sharing`, `Use(stdlib)` costs 6.81µs / 42 allocs
against a < 1µs target. The plugin's `Init` no longer runs and no builtin
closure is rebuilt, so what remains is attachment: `snapshotEntries` copies the
completed layer's entry map under `RLock`, `populateTemplateBindings` merges
that copy into `e.bindings` and calls `activate`, which rebuilds the engine's
active list.

The copy is the part that has no justification left. `putEntry` is reachable
only from `RegisterValue`/`RegisterSource`, which run only during a plugin's
`Init`, which runs only inside `ensureLayer`'s single-flighted build closure.
`markComplete` runs after that closure returns. So from the moment a layer is
complete, its entry map is never written again — and every engine that attaches
it makes a private copy of a map that will outlive them all unchanged.

## Decision 1: publish the entry set at completion

Rather than leaving `entries` mutable and copying on read, the layer publishes
an immutable entry set when it is marked complete, and readers take that value.
Two shapes are viable and the implementation should pick one on evidence:

- Publish the existing map behind an `atomic.Pointer`, set once by
  `markComplete`. Readers load the pointer and read the map directly with no
  lock. Cheapest; correctness depends entirely on the no-write-after-complete
  invariant, which must be pinned by test.
- Keep a `sync.RWMutex` read for the pointer load. Marginally more expensive,
  strictly safer if any future path ever writes post-completion.

Prefer the first, but only with the invariant test in place. The incomplete
path is unchanged: a layer still under construction keeps its current
lock-guarded map, and nothing attaches to it.

## Decision 2: keep per-engine state per-engine

Only the entry set is shared. `stdlibLazyEngineState` — installed names,
tombstones, the active list, per-name locks — stays per-engine and mutable, as
does `e.bindings`. `UnloadPlugin` continues to touch only the calling engine.
This is the same boundary the previous change established; the point here is
that widening what is shared must not widen what is *mutable* and shared.

## Decision 3: what "copies nothing" means as a test

An allocation assertion, not an inspection. `testing.AllocsPerRun` over a
second engine's `Use` of an already-complete layer, asserting the count does
not scale with the layer's entry count — build layers of two sizes and require
the attach cost to be flat between them. This catches a reintroduced copy in a
way a pointer-identity check on one entry would not.

## Risks

- **Aliasing.** One shared map read by every engine means a stray write
  corrupts all of them at once. Mitigated by confining writes to the build
  closure (already true) and pinning it: a test that fails if `putEntry` is
  reachable after `markComplete`.
- **The invariant is load-bearing but implicit.** It currently holds because of
  how `ensureLayer` is written, not because the type prevents it. If the chosen
  shape is the lock-free one, the comment and test have to carry that weight
  explicitly.
- **Diminishing returns.** Removing the copy addresses the `Use` band; full
  startup also carries bootstrap-artifact execution and env-binding writes that
  this change does not touch. Reaching < 10µs startup is not promised here —
  see the success criteria in tasks.

## Alternatives rejected

- **Copy-on-write of the entry map per engine:** the map is never written after
  completion, so copy-on-write degenerates to the copy we are removing.
- **Sharing `e.bindings` too:** that map is genuinely per-engine — shadowing,
  deletion, and unload all write it.
