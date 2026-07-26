## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (≥10 counts, `GOMAXPROCS=2`) of
      `BenchmarkEngine_Creation`, `BenchmarkEngine_UseStdlibBytecode/lazy`, and
      `BenchmarkEngine_StartupStdlibBytecode/cache-warm`. Starting point at
      f4c7b57: 24 allocs / ~765ns, 38 allocs, 140 allocs. Allocation counts are
      the trustworthy signal on developer hardware; ns/op carries ~20% spread.
- [ ] 1.2 Confirm the construction attribution with `GODEBUG=memprofilerate=1`
      so the per-site allocation counts are exact rather than extrapolated.

## 2. Shared discard logger

- [ ] 2.1 Resolve a nil logger to a process-wide discard logger built once,
      instead of `slog.New(slog.NewTextHandler(io.Discard, nil))` per engine.
- [ ] 2.2 Confirm sharing is sound rather than assuming it: `slog.Logger` is
      safe for concurrent use and the discard handler holds no per-engine
      state. Verify no code path mutates `engineImpl.logger` after
      construction, and record the write sites checked.
- [ ] 2.3 An explicitly passed logger keeps its exact current behavior —
      per-engine, untouched, never replaced by the shared one. Test both
      branches, including that two engines given distinct loggers stay
      distinct.

## 3. Lazy per-engine materialization state

- [ ] 3.1 `newStdlibLazyEngineState`'s maps allocate on first write rather than
      at construction, matching the `nameLocks` treatment already in that type.
      The `activeList` store stays as-is if removing it changes the miss path's
      read.
- [ ] 3.2 Enumerate every write site of `active`, `installed`, and
      `tombstoned`, with file:line, and guard each. A missed one panics on
      assignment to a nil map, which the no-panics invariant forbids. Reads
      from a nil map are legal and need no guard — say which sites are reads.
- [ ] 3.3 Lazy initialization happens inside the state's existing mutex, never
      before it. These maps are written from concurrent first-touch paths.

## 4. Semantics unchanged

- [ ] 4.1 The full existing suite passes unmodified — in particular every
      `TestLazyMaterialize_*`, the plugin lifecycle tests, and the dialect
      tests. An engine that loads no plugin, one that loads and unloads, and
      one that hot-reloads must all behave exactly as before.
- [ ] 4.2 Concurrent first-touch under `-race` on an engine whose state maps
      start nil, including several goroutines racing the first write.
- [ ] 4.3 Logging behavior is unchanged for both nil and explicit loggers —
      an engine given a real logger still emits what it emitted before.

## 5. Measure and decide the remainder

- [ ] 5.1 Re-run 1.1 interleaved. Report the construction figure and how much
      of the predicted ~7.5 allocations actually came out.
- [ ] 5.2 Re-profile construction and report what the remaining sites cost —
      `newBytecodeEvaluator`, `NewEnv`, `NewEvaluatorWithDialect`,
      `NewRegistry`, `newStats`. State plainly whether any is worth a further
      change or whether construction is now near its floor. Implement nothing
      here that the profile does not justify; a "this is done" answer is a
      legitimate outcome.
- [ ] 5.3 Goldset non-regressing in both `GOLDSET_MODE=eval` and
      `GOLDSET_MODE=vm`, interleaved.

## 6. Verify

- [ ] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [ ] 6.2 Full suite + `-race`; crossval; goldset both modes. `cmd/perfgate`
      is release-runner only — its local verdict is not evidence, so perf
      claims rest on interleaved A/B benchmarks.
