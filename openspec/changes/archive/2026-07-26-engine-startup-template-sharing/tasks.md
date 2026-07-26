## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (≥10 counts): article Startup row plus split
      probes (`New`-only; `New`+`Use`; full) with `-benchmem`. Record the
      dcbdf62 figures: 39.6µs/286 allocs full, 3.5µs/42 New-only,
      20.7µs/244 New+Use.
- [x] 1.2 Alloc-profile the full startup and record the
      `RegisterValue`/`register*`/dialect shares as the before-picture.

Baseline at 01c96ca, `GOMAXPROCS=2`, `-count=10 -benchtime=200ms`. Allocation
counts are the stable signal on developer hardware; ns/op carries ~20% spread.

| Probe | ns/op (median) | allocs/op |
| --- | --- | --- |
| `BenchmarkEngine_Creation` (`New`+`Close`) | 5.2µs | 75 |
| `BenchmarkEngine_UseStdlibBytecode/lazy` | 19.4µs | 244 |
| `BenchmarkEngine_StartupStdlibBytecode/cache-warm` | 40.0µs | 356 |
| `BenchmarkEngine_StartupStdlibBytecode/cache-disabled` | 40.5µs | 447 |
| `BenchmarkEngine_UseStdlibBytecode/eager` | 69µs | 605 |

Alloc profile of the cache-warm startup (`-alloc_objects`, 2000x): template
construction is ~56% of startup allocations — `RegisterValue` 18.9% flat, the
`register*` functions plus `unaryMathFunc` ~37%. Dialect construction
(`Dialect.with`, `copyKernel`, `NewEvaluatorWithDialect`, `sha256Hash`) is
~7.4%. The first-eval tail is VM-side, not compile-side: `CopyTreeFreshSites`
6.6% cum, `resolveGlobalValue` 7.2% cum, `vm.New` 5.7% cum, `Chunk.buildSites`
5.5% flat, against `Tokenize`+`parseList` at 2.6% combined.

## 2. Completed-layer short-circuit

- [x] 2.1 Add layer completion to the template registry: the first
      successful `Init` for a `{dialectFP, plugin name+version}` key marks
      the layer complete; failure leaves it incomplete for retry.
      Single-flight concurrent first `Use` of one key.
- [x] 2.2 `Use` attaches a completed layer without running `Init`. Scope
      fail-closed: only plugins whose registration routes through the
      template registry qualify; direct-env plugins keep per-engine `Init`.
- [x] 2.3 Extend the layer key with plugin identity/version from
      `Metadata()` so two builds of one plugin name get distinct layers.
- [x] 2.4 Semantics tests: materialization, enumeration, shadowing,
      deletion, `UnloadPlugin` (removes attachment + materialized bindings,
      not the shared layer), hot-reload — all behave identically on a
      first-engine (built the layer) and a second-engine (attached it).
      Concurrent engines under `-race`.

The fail-closed scope guard needs an explicit call-site gate, not just the
structural fact that a direct-env plugin's key never gets a layer.
`singleflight` collapses any two concurrent calls sharing a key, including
calls whose key will never complete: two engines loading the same non-template
plugin name+version under one dialect fingerprint would have one engine's
`Init` — and therefore its own `env` writes — silently skipped. Sharing is
sound for the deferred path only because its registrations never touch the
`env` argument at all. `initPlugin` (`runtime/plugin.go:67-84`) therefore
routes through `ensureLayer` only when `Name()` is `""`.

`ensureLayer` (`runtime/lazy_template.go:185-204`) never holds `r.mu` across
`build()`, which reenters `putEntry` and takes `r.mu` itself; the
`singleflight.Group` lock is disjoint. `markComplete` is the only writer of
`complete`, reached only from the build callback — `UnloadPlugin`,
`ReloadPlugin` rollback, `rollbackPluginUse`, and `deactivate` touch per-engine
state only.

## 3. Memoized stock dialects

- [x] 3.1 `cl.Dialect()`/`clojure.Dialect()` return process-memoized values;
      the memoized constructor forces `resolve()` and `Fingerprint()` so the
      shared value is complete before it escapes.
- [x] 3.2 Immutability audit: enumerate every write site of the resolved
      form table and vocabulary map; prove none runs post-construction; add
      a test that engine-level operator redefinition on one engine does not
      alter another engine's dialect behavior (pins "Per-Engine dispatch
      isolation" over the shared value).
- [x] 3.3 Custom (non-stock) dialect construction stays byte-identical in
      behavior; `Fingerprint()` caching inside a `Dialect` value must not
      change its output for any dialect (compare against the uncached hash
      across the test dialect corpus).

Memoization is a `cache *dialectCache` field on `Dialect`, populated only by
`Memoized()` and cleared by every builder on the copy it returns. All fourteen
`Dialect`-returning functions were audited: `Add`/`Rename`/`Remove` delegate to
`with()`; `FlatCond`, `Vocabulary`, `WithAdapter`, `Lisp2`,
`WithoutBracketLiterals`, `WithFunctionRef`, `WithReaderVector` clear it
inline; `FullDialect`/`EmptyDialect` build fresh literals.

Isolation rests on dispatch order rather than on the dialect table being
private: `core/eval.go:465-473` checks `e.forms[sym.V]` before any environment
lookup, so a special-form name can never be shadowed by an env binding and
per-engine env mutation structurally cannot reach a shared `forms` map.

## 4. First-eval tail

- [x] 4.1 Profile the `Use`→`Eval`→`Close` tail (~19µs) and attribute it
      across: first-touch materialization, tokenize+compile, cache
      admission, cold `vm.New`, `Close`.
- [x] 4.2 Apply the dominant fix per the design's decision table
      (macro-epoch-gated user-chunk reuse in the existing process tier /
      batched materialization / pool pre-size). Do not implement branches
      the profile does not justify.

Tail attribution after streams 1 and 2, `-alloc_objects` on the cache-warm
startup:

| Bucket | share |
| --- | --- |
| First-touch materialization | 30.1% |
| — executing the cached bootstrap artifact | 18.0% |
| — `getNameMutex` `sync.Map` node allocation | 6.5% |
| — `installValue`→`Env.localCell` binding writes | 5.6% |
| Engine + dialect construction (`runtime.New`) | 18.3% |
| Tokenize + compile of the benchmark's own source | 21.8% |
| Cold `vm.New` on first pool Get | 9.1% |
| Cache admission | 2.0% |
| `Close` | ~0% |

Only the `getNameMutex` component is deferred-machinery overhead rather than
work an eager load would also pay, so it is the only thing 4.2 changed. The
bootstrap-execution and env-binding components are inherent; the compile
component is the branch ruled out below; `vm.New` is real but relocating it to
construction time cannot reduce a benchmark that times construction and first
eval as one unit.

Macro-epoch-gated user-chunk reuse was rejected on correctness, not on the
profile. `BumpMacroEpoch` fires only on plugin unload, reload, and `defmacro`,
never on a `Use` that adds bindings, so two engines can sit at the same
`{dialectFP, epoch 0}` with different plugin sets loaded. A process-level
user-source chunk cache keyed that way would serve one engine a chunk compiled
without the other's plugin macros expanded. The existing bootstrap-artifact
tier escapes this only because bootstrap source expands through a throwaway
evaluator with no plugin macros visible, which user source does not. Making it
safe needs a plugin-set dimension in the cache key — a larger change than this
one, and a candidate for a follow-up.

## 5. Measure

- [x] 5.1 Re-run 1.1 interleaved. Success criteria: second-and-later engine
      full startup < 10µs; `Use` band < 1µs; no change to first-engine
      correctness; goldset both modes non-regressing (steady-state rows must
      not pay for startup sharing).
- [x] 5.2 Cross-engine correctness: N engines constructed concurrently, each
      evaluating the dialect-vocab test corpus — results identical to
      today's per-engine construction.

Interleaved A/B against 01c96ca, prebuilt test binaries alternated over five
rounds, `GOMAXPROCS=2 -benchtime=200ms -count=2` per round, n=10 per side:

| Probe | sec/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `Engine_Creation` | 5.49µs → 741ns (−86.5%) | 5.92Ki → 2.17Ki | 75 → 24 |
| `Startup/cache-warm` | 37.8µs → 24.6µs (−35.0%) | 52.3Ki → 39.1Ki | 356 → 144 |
| `Use/lazy` | 18.1µs → 6.81µs (−62.4%) | 23.2Ki → 10.3Ki | 244 → 42 |
| geomean | −67.9% | −50.5% | −71.9% |

All deltas at p=0.000. Correctness criteria are met: `go test -race ./...`
passes 2500 tests across 18 packages, and the goldset passes in both
`GOLDSET_MODE=eval` and `GOLDSET_MODE=vm`.

**The two cost criteria are not met.** Startup lands at 24.6µs against a
< 10µs target, and the `Use` band at 6.81µs against < 1µs. The proposal
modelled `Use` as collapsing to a fingerprint lookup, but attaching a
completed layer is not free: `populateTemplateBindings` and `snapshotEntries`
still copy the layer's entry list into per-engine state, and `Use` still runs
plugin registration, vocabulary application, and binding snapshots. Those 42
allocations are per-engine attach cost that sharing the template does not
remove. Reaching the stated targets needs attach-side work that this change
did not scope — sharing the snapshot rather than copying it, and the
plugin-set-keyed chunk tier described under 4.2.

## 6. Verify

- [x] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [x] 6.2 Full suite + `-race` (the concurrency surface is the point of this
      change — treat any race as a design fault, not a test flake);
      crossval; goldset both modes; `cmd/perfgate` one-sided non-regression.

`go test -race -count=1 ./...` passes 2500 tests across 18 packages, crossval
included. Goldset benchmarks were compared interleaved against 01c96ca in both
modes: every row identical, all samples equal, geomean +0.00%.

`cmd/perfgate` was not run. Its absolute thresholds are calibrated for the
release runner and report false failures on developer hardware, so a local
verdict would be evidence of nothing. The non-regression claim above rests on
interleaved A/B benchmarks instead. The gate still applies on the release
runner.
