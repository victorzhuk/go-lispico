## 1. ResourceLimits config + wiring

- [ ] 1.1 Define `ResourceLimits` in `runtime` (fields: `MaxReaderDepth`, `MaxStructuralDepth`, `MaxCollectionLen`, `MaxCacheEntries`) with documented conservative defaults; zero field → default, resolved at `New`.
- [ ] 1.2 Add `runtime.WithResourceLimits(...)` construction option; store resolved limits on the engine, immutable after `New`.
- [ ] 1.3 Thread the resolved limits into the reader (via `dialect.Read`), the evaluator (into `evalState`), and the stdlib `range` registration.
- [ ] 1.4 Add (or reuse) a single `*core.LispicoError` code classifying "resource limit exceeded" so embedders can `errors.As` uniformly.

## 2. Reader nesting-depth guard (SEC-1)

- [ ] 2.1 Give the parser a construction-time depth ceiling; keep a defaulted package-level `core.Read` so existing callers compile.
- [ ] 2.2 Increment/decrement a depth counter across `parseForm`/`parseList`/`parseVector`/`parseHashMap`/quote forms; return a `LispicoError` (with token position) past the ceiling.
- [ ] 2.3 Regression test: source at a depth that previously produced `fatal error: stack overflow` now returns a typed read error and the process survives.

## 3. Evaluator structural-depth guard (SEC-4)

- [ ] 3.1 Add a structural-depth counter to per-call `evalState`; increment in `Eval`'s `Vector` case, `evalMap`, and `expandQuasiquote`, decrement on unwind.
- [ ] 3.2 Return a `LispicoError` past `MaxStructuralDepth`; never panic, never overflow.
- [ ] 3.3 Test: deeply nested vector/map/quasiquote literal fails closed; a `-race` test proves two concurrent deep evaluations do not share the counter (ADR 0003).

## 4. range bound + cancellation (SEC-3)

- [ ] 4.1 In `plugins/stdlib/collections.go`, cap `range` result length at `MaxCollectionLen`, returning a `LispicoError` before allocating an oversized slice.
- [ ] 4.2 Check `ctx` cooperatively inside the build loop so `WithTimeout`/cancellation stops it promptly.
- [ ] 4.3 Test: oversized `range` fails closed with no huge allocation; cancelled-context `range` returns the context error.

## 5. Bounded chunk cache (PERF)

- [ ] 5.1 Enforce `MaxCacheEntries` on `bytecodeEvaluator.cache`; drop stale-`macroEpoch` entries on lookup miss and evict (size cap / LRU) when over the ceiling, under the existing cache mutex.
- [ ] 5.2 Test: repeated distinct-source + macro-redefinition evaluation keeps entry count at or below the ceiling while results stay correct.

## 6. Verify invariants

- [ ] 6.1 `go test ./...` green, including `core/vm/crossval_test.go` (VM/tree-walker agreement).
- [ ] 6.2 `go test -race ./...` clean for the concurrent structural-depth scenario.
- [ ] 6.3 Confirm `core/` still has zero external imports and no new panic path; update `README`/docs if a limits option needs surfacing.
