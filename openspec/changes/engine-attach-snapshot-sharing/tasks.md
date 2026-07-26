## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (≥10 counts) of `BenchmarkEngine_Creation`,
      `BenchmarkEngine_UseStdlibBytecode/lazy`, and
      `BenchmarkEngine_StartupStdlibBytecode/cache-warm` with `-benchmem`.
      Starting point at e4b7ce8: 741ns/24 allocs, 6.81µs/42 allocs,
      24.6µs/144 allocs. Allocation counts are the trustworthy signal on
      developer hardware; ns/op carries ~20% spread.
- [ ] 1.2 Alloc-profile `Use` on an already-complete layer and record what
      `snapshotEntries` and `populateTemplateBindings` each contribute, so the
      claimed win has a before-picture rather than an estimate.

## 2. Prove the invariant before relying on it

- [ ] 2.1 Enumerate every write site of a layer's entry map and show each is
      reachable only from inside `ensureLayer`'s build closure, before
      `markComplete`. Record the list with file:line.
- [ ] 2.2 Add a test that fails if a completed layer's entry map is written —
      the invariant the shared read depends on. This lands before the sharing
      does, not after.

## 3. Publish the entry set

- [ ] 3.1 Mark completion by publishing the entry set once, and have attaching
      engines read the published value instead of copying it. Pick the
      lock-free or the lock-guarded shape per the design and say which and why.
- [ ] 3.2 `populateTemplateBindings` consumes the published set without
      materialising a per-engine copy. Per-engine state — installed names,
      tombstones, active list, `e.bindings` — stays per-engine.
- [ ] 3.3 The incomplete-layer path is untouched: a layer under construction
      keeps its current lock-guarded map and is never attached.

## 4. Semantics unchanged

- [ ] 4.1 Enumeration, shadowing, deletion, `UnloadPlugin`, and hot-reload
      behave identically on a first engine (built the layer) and a second
      (attached the published set).
- [ ] 4.2 `UnloadPlugin` on one engine leaves the published entry set
      unchanged and every sibling engine unaffected — the spec's
      "Unload does not disturb a shared entry set" scenario.
- [ ] 4.3 Concurrent engines attaching one completed layer under `-race`,
      including attach racing against another engine's unload.

## 5. Measure

- [ ] 5.1 Re-run 1.1 interleaved. Success criteria: attach cost does not scale
      with the layer's entry count (flat between two layer sizes per the
      design's test); `Use` band strictly below the 6.81µs/42-alloc starting
      point; goldset non-regressing in both modes.
- [ ] 5.2 Report the resulting full-startup figure against the < 10µs target
      from `engine-startup-template-sharing` and state plainly whether it is
      reached. If it is not, attribute what still stands between — do not
      leave the target silently unmet a second time.

## 6. Verify

- [ ] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [ ] 6.2 Full suite + `-race`; crossval; goldset both modes. `cmd/perfgate`
      is release-runner only — its local verdict is not evidence, so perf
      claims rest on interleaved A/B benchmarks.
