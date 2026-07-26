## Why

`engine-startup-template-sharing` removed the per-engine *construction* of the
stdlib template: a second engine in a process no longer runs the plugin's
`Init` and no longer rebuilds a single `GoFunc`. It did not remove the
per-engine *attachment* cost, and that is what now dominates the `Use` band.

Measured on the article harness after that change (interleaved A/B vs 01c96ca,
n=10, p=0.000):

| Probe | before | after | target |
| --- | --- | --- | --- |
| `Engine_Creation` | 5.49µs / 75 allocs | 741ns / 24 allocs | — |
| `Use(stdlib)` | 18.1µs / 244 allocs | 6.81µs / 42 allocs | < 1µs |
| full startup | 37.8µs / 356 allocs | 24.6µs / 144 allocs | < 10µs |

Both cost targets in that change's task 5.1 were missed, and the reason is
specific rather than diffuse. `Use` still calls `snapshotEntries`, which copies
the completed layer's whole entry map, and `populateTemplateBindings`, which
merges that copy into per-engine state and rebuilds the active list. The copy
is made from a layer that is provably immutable at that point: `putEntry` runs
only inside `ensureLayer`'s single-flighted build closure, and the layer is not
marked complete until that closure returns. A completed layer is written once
and read forever, so copying it per engine buys nothing.

The remaining startup residue outside `Use` is attributed and mostly inherent —
executing the cached bootstrap artifact (18.0% of startup allocations) and
writing the env bindings themselves (5.6%) are work an eager load also pays.
The one addressable non-inherent block left is the compile of user source
(21.8%), which needs a plugin-set-keyed chunk tier; see Out of scope.

## What Changes

- **A completed layer's entry set is read through a shared immutable snapshot
  rather than copied per engine.** `snapshotEntries` stops allocating a fresh
  map per `Use` for a complete layer; the layer publishes its entry set once,
  at completion, and every attaching engine reads that value. The published
  snapshot is never written after completion, so sharing it needs no lock on
  the read path.
- **Per-engine state keeps only what is genuinely per-engine.** Which plugins
  an engine has attached, which names it has materialized, and its own
  bindings stay per-engine and mutable. Only the immutable entry set is
  shared.
- Engine-visible behavior is unchanged: enumeration, shadowing, deletion,
  `UnloadPlugin`, and hot-reload keep operating on per-engine state exactly as
  they do today.

## Capabilities

### Modified Capabilities

- `runtime-api`: "Deferred plugin binding materialization" gains a requirement
  that attaching a completed template layer does not copy its entry set, and
  that the shared entry set is immutable after completion.

## Impact

- Code: `runtime/lazy_template.go` (`snapshotEntries`, layer publication at
  completion), `runtime/plugin.go` (`populateTemplateBindings`), tests.
- Risk — aliasing: sharing one entry map across engines is sound only while
  nothing writes it post-completion. `putEntry` is already confined to the
  build closure; this change must pin that with a test, not an assumption,
  because a later write would corrupt every attached engine at once rather
  than one.
- Risk — unload: `UnloadPlugin` must keep detaching only the calling engine.
  Sharing the entry set makes an accidental write during unload worse, so the
  existing per-engine-only guarantee needs an explicit test against a shared
  snapshot.
- Interaction: builds directly on `engine-startup-template-sharing`
  (archived 2026-07-26). Independent of `vm-boundary-state-reuse` and
  `compiler-constant-literal-folding`.

## Out of scope

Process-level reuse of compiled *user* source. The existing per-engine chunk
cache key carries a macro epoch, but `BumpMacroEpoch` fires only on plugin
unload, reload, and `defmacro` — never on a `Use` that adds bindings. Two
engines can therefore sit at the same `{dialect fingerprint, epoch 0}` with
different plugin sets loaded, and a process-level tier keyed that way would
serve one engine a chunk compiled without the other's plugin macros expanded.
Making it safe requires a plugin-set dimension in the cache key, which is its
own change.
