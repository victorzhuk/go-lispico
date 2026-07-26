## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (≥10 counts, `GOMAXPROCS=2`) of
      `BenchmarkEngine_Creation`, `BenchmarkEngine_UseStdlibBytecode/lazy`, and
      `BenchmarkEngine_StartupStdlibBytecode/cache-warm`. Starting point at
      f4c7b57: 24 allocs / ~765ns, 38 allocs, 140 allocs. Allocation counts are
      the trustworthy signal on developer hardware; ns/op carries ~20% spread.
- [x] 1.2 Confirm the construction attribution with `GODEBUG=memprofilerate=1`
      so the per-site allocation counts are exact rather than extrapolated.

## 2. Shared discard logger

- [x] 2.1 Resolve a nil logger to a process-wide discard logger built once,
      instead of `slog.New(slog.NewTextHandler(io.Discard, nil))` per engine.
      Use `slog.DiscardHandler` (stdlib, Go 1.24; go.mod already requires
      1.24.0) rather than a text handler over `io.Discard`: its `Enabled`
      always reports false, so every later `Info`/`Warn`/`Error` on a
      default-logger engine skips attribute formatting instead of running the
      full handler pipeline and throwing the bytes away.
- [x] 2.2 Confirm sharing is sound rather than assuming it: `slog.Logger` is
      safe for concurrent use and the discard handler holds no per-engine
      state. Verify no code path mutates `engineImpl.logger` after
      construction, and record the write sites checked.
- [x] 2.3 An explicitly passed logger keeps its exact current behavior —
      per-engine, untouched, never replaced by the shared one. Test both
      branches, including that two engines given distinct loggers stay
      distinct.

## 3. Lazy per-engine materialization state

- [x] 3.1 `newStdlibLazyEngineState`'s maps allocate on first write rather than
      at construction, matching the `nameLocks` treatment already in that type.
      The `activeList` store stays — resolved, not left open: `activeKeys`
      type-asserts `Load().([]stdlibTemplateKey)` without comma-ok, so a
      never-stored `atomic.Value` panics, and `installLazyLayer` runs on every
      engine, so a zero-plugin engine reaches that read. It also costs nothing.
      Leave a comment at the store saying so, or a later cleanup will remove it
      for no gain and reintroduce the panic.
- [x] 3.2 Enumerate every write site of `active`, `installed`, and
      `tombstoned`, with file:line, and guard each. A missed one panics on
      assignment to a nil map, which the no-panics invariant forbids. Reads
      from a nil map are legal and need no guard — say which sites are reads.
- [x] 3.3 Lazy initialization happens inside the state's existing mutex, never
      before it. These maps are written from concurrent first-touch paths.

## 4. Semantics unchanged

- [x] 4.1 The full existing suite passes unmodified — in particular every
      `TestLazyMaterialize_*`, the plugin lifecycle tests, and the dialect
      tests. An engine that loads no plugin, one that loads and unloads, and
      one that hot-reloads must all behave exactly as before.
- [x] 4.2 Concurrent first-touch under `-race` on an engine whose state maps
      start nil, including several goroutines racing the first write.
- [x] 4.3 Logging behavior is unchanged for both nil and explicit loggers —
      an engine given a real logger still emits what it emitted before.

## 5. Measure and decide the remainder

- [x] 5.1 Re-run 1.1 interleaved. The expected landing is ~17.5 allocations
      from 24 — 3.5 from the logger and 3 from the maps. Report what actually
      came out; landing at 17-18 is the target, not a shortfall against the
      16.5 an earlier four-map reading implied.
- [x] 5.2 Re-profile construction and report what the remaining sites cost —
      `newBytecodeEvaluator`, `NewEnv`, `NewEvaluatorWithDialect`,
      `NewRegistry`, `newStats`, and `core.Env.SetLazyLayer` (1 allocation
      boxing its argument for the `atomic.Pointer` store; in `core/`, so out of
      scope here, but name it rather than dropping it). State plainly whether
      any is worth a further change or whether construction is near its floor.
      Implement nothing the profile does not justify; "this is done" is a
      legitimate outcome.
- [x] 5.3 Goldset non-regressing in both `GOLDSET_MODE=eval` and
      `GOLDSET_MODE=vm`, interleaved.

Interleaved A/B against 4921bbe, `GOMAXPROCS=2 -count=10 -benchtime=200ms`,
exact on every run both sides:

| Probe | allocs/op | bytes/op |
| --- | --- | --- |
| `Engine_Creation` | 24 → **17** | 2224 → 1904 |
| `Use/lazy` | 38 → **32** | 6977 → 6704 |
| `Startup/cache-warm` | 140 → **135** | 36433 → 36197 |

Seven allocations came out of construction against the six and a half
predicted. Goldset is byte-identical to base in both modes, geomean +0.00%.

**Construction is at its floor.** The post-change profile sums to exactly 17
and every remaining site is state a fresh engine genuinely needs:
`newBytecodeEvaluator` 3, `New` itself 3, `NewEnv` 2,
`NewEvaluatorWithDialect` 2, `NewRegistry` 2, `newStats` 1, the lazy-state
struct 1, the materializer struct 1, and `core.Env.SetLazyLayer` 1 — the last
boxing its argument for an `atomic.Pointer` store, in `core/` and out of scope
here. The discard-logger site and the three maps no longer appear in the
profile at all. Nothing further was implemented, because nothing in the profile
justifies it.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [x] 6.2 Full suite + `-race`; crossval; goldset both modes. `cmd/perfgate`
      is release-runner only — its local verdict is not evidence, so perf
      claims rest on interleaved A/B benchmarks.
