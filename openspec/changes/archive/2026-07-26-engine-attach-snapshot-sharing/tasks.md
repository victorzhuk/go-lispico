## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (≥10 counts) of `BenchmarkEngine_Creation`,
      `BenchmarkEngine_UseStdlibBytecode/lazy`, and
      `BenchmarkEngine_StartupStdlibBytecode/cache-warm` with `-benchmem`.
- [x] 1.2 Alloc-profile `Use` on an already-complete layer and record what
      `snapshotEntries` and `populateTemplateBindings` each contribute, so the
      claimed win has a before-picture rather than an estimate.

Baseline at 41bbdb5, `GOMAXPROCS=2`, `-count=10 -benchtime=200ms`:
`Engine_Creation` 765ns / 24 allocs, `Use/lazy` 6.587µs / 42 allocs,
`Startup/cache-warm` 23.15µs / 144 allocs.

Exact attribution (`GODEBUG=memprofilerate=1`, per-allocation sampling) of the
42 allocs/op in `Use/lazy`: `snapshotEntries`' map allocation 4;
`owned := make(...)` in `populateTemplateBindings` 4; the whole attach path
including `activate` and `rebuildActiveList` 13; `runtime.New` 63% cumulative.
The ceiling for this change is therefore about 42 → 38 allocs.

## 2. Prove and enforce the invariant

- [x] 2.1 Enumerate every write site of a layer's entry map and show each is
      reachable only from inside `ensureLayer`'s build closure, before
      `markComplete`. Record the list with file:line.
- [x] 2.2 Add a fail-closed guard in `putEntry`: once the layer is published,
      refuse the write instead of mutating a map other engines are reading.
      No panics — return an error or drop with a log, matching the surrounding
      code.
- [x] 2.3 White-box test calling `putEntry` directly on a completed layer,
      asserting the guard fires. The enumeration in 2.1 documents that no bug
      exists today; this test is what stops one appearing later. Both land
      before the sharing does.

A layer's entry map has exactly one write site: `layer.entries[entry.name] =
entry` in `putEntry` (`runtime/lazy_template.go:187`), now guarded by the
`layer.complete` check immediately above it. Its call chain:

`putEntry` ← `stdlibLazyMaterializer.RegisterValue` (two sites, around
`lazy_template.go:490`) and `RegisterSource` (`:542`) ← `core.Env.RegisterValue`
/ `RegisterSource` (`core/env.go:118,132`) ← a plugin's own `Init` (for example
`plugins/stdlib/arithmetic.go:12`, `plugins/stdlib/bootstrap.go:90`) ←
`p.Init(e.rootEnv)` inside `initPlugin`'s build closure (`runtime/plugin.go:74`,
closure at `:90`) ← `ensureLayer` (`lazy_template.go:227`), single-flighted.

Both materializer methods gate on `m.engine.loadingPlugin != ""`, so only the
plugin whose `Name()` is `""` reaches `putEntry` at all. `markComplete`
(`:211`) runs strictly after `build()` returns without error, under the same
`r.mu.Lock()`, and `ensureLayer` short-circuits on `complete` both before
entering the flight and again inside it. No other `.entries[...] =` site exists
in the tree; `core/types.go`'s `hamtNode.entries` and `runtime/call_cache.go`'s
`callCache.entries` are unrelated types.

## 3. Publish the entry set

- [x] 3.1 Add `published atomic.Pointer[...]` to the layer; `markComplete`
      stores the entries map as `complete` flips true. Match the publish-once
      idiom already used in `core/env.go`, `core/vm/chunk.go`, and
      `runtime/call_cache.go`, including an explicit invariant comment.
- [x] 3.2 Replace the copy in the attach read path with a load of the published
      pointer. Rename the accessor — its contract changes from "returns a
      private copy" to "returns the shared, read-only map" — and say so in its
      doc comment. `ForceAll` consumes the same accessor; verify it never
      mutates the result.
- [x] 3.3 Leave `owned`/`e.bindings` construction exactly as it is, and comment
      the real reason at the merge site: `applyVocabulary` can bind through a
      dialect adapter before this runs, so the map may already exist and be
      mutated here, and aliasing the shared set would corrupt sibling engines.
- [x] 3.4 The incomplete-layer path is untouched: a layer under construction
      keeps its current lock-guarded map and is never attached.

`snapshotEntries` became `publishedEntries`, hanging off the layer that owns the
pointer rather than the registry, matching `callCache.snapshot()`.
`RegisterValue` propagates the guard's error directly; `RegisterSource` returns
`bool` per the `core.LazyLayer` interface, so it logs and returns `false`, which
is the interface's existing "not deferred, evaluate eagerly" contract that
`plugins/stdlib/bootstrap.go` already handles.

## 4. Semantics unchanged

- [x] 4.1 Enumeration, shadowing, deletion, `UnloadPlugin`, and hot-reload
      behave identically on a first engine (built the layer) and a second
      (attached the published set). The existing `TestLazyMaterialize_*` suite
      must pass unmodified.
- [x] 4.2 `UnloadPlugin` on one engine leaves the published entry set
      unchanged and every sibling unaffected — assert map and per-entry pointer
      identity across a sibling's unload and re-`Use`, not merely that
      evaluation still works.
- [x] 4.3 Concurrent engines attaching one completed layer under `-race`,
      including attach racing another engine's unload.
- [x] 4.4 The `WithAdapter` vocabulary path (`runtime/dialect_vocab_test.go`)
      passes unmodified — it is the regression net for 3.3.

Two coverage gaps surfaced in review and were closed rather than argued away.
`TestUnloadPlugin_PublishedLayerIdentityUnaffected` now materializes names on
the unloading engine first, because the scenario reads "after materializing
some of its names" and the test previously unloaded an engine that had resolved
nothing. `TestPublishedLayer_ConcurrentAttachRacesUnload` builds its layer
before the goroutines start, so `markComplete`'s store never ran concurrently
with anything; `TestPublishedLayer_ConcurrentFirstBuildRacesAttach` was added
for that window — sixteen engines race one unbuilt key, so the single publisher's
store races the loads that singleflight releases, and every engine must observe
the same published map.

## 5. Measure

- [x] 5.1 Re-run 1.1 interleaved. Success criteria: the read accessor allocates
      exactly zero for a complete layer (`testing.AllocsPerRun`, size-independent
      — do not use a flat-across-two-layer-sizes assertion, which cannot
      discriminate at stdlib's scale); `Use/lazy` strictly below 42 allocs;
      goldset non-regressing in both modes.
- [x] 5.2 Report the resulting figures against the < 10µs / < 1µs targets from
      `engine-startup-template-sharing` and state plainly that this change does
      not reach them, with the measured split showing where the remainder sits.

Interleaved A/B against 41bbdb5, prebuilt binaries, `GOMAXPROCS=2`,
`-count=10 -benchtime=200ms`:

| Probe | allocs/op | delta |
| --- | --- | --- |
| `Engine_UseStdlibBytecode/lazy` | 42 → 38 | −9.52%, p=0.000 |
| `Engine_StartupStdlibBytecode/cache-warm` | 144 → 140 | −2.78%, p=0.000 |
| `Engine_Creation` | 24 → 24 | unchanged, as expected |

Both sides at ±0% variance — allocation counts here are exact, not sampled.
`TestPublishedEntries_AllocsZeroOnCompleteLayer` reports 0.00 allocations for
the read accessor. Goldset benchmarks are identical to base in both
`GOLDSET_MODE=eval` and `GOLDSET_MODE=vm`, geomean +0.00%. ns/op is not claimed
as a win: it stays inside this hardware's ~20% spread.

**The startup targets remain unmet and this change does not approach them.**
`Use` is 38 allocs against a < 1µs band, and full startup 140 allocs / ~23µs
against < 10µs. The measured remainder sits where this change never reached:
`runtime.New` is 63% cumulative of the `Use/lazy` benchmark — engine and
evaluator construction, not template attachment. Of the attach path itself, the
`owned`/`e.bindings` merge is another 4 allocs and must stay per-engine (see
3.3). Closing the targets means attacking engine construction, which is a
separate change.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [x] 6.2 Full suite + `-race`; crossval; goldset both modes. `cmd/perfgate`
      is release-runner only — its local verdict is not evidence, so perf
      claims rest on interleaved A/B benchmarks.

`go test -race -count=1 ./...` passes 2504 tests across 18 packages, crossval
included; `golangci-lint run` reports 0 issues; `gofmt -l .` and `go vet ./...`
are clean. `cmd/perfgate` was not run, for the reason stated above.
