# Tasks — reader-state-reuse

## 0. Pin the baseline

- [ ] 0.1 After `reader-allocation-floor` lands: interleaved baseline
      (`-count=10`) of `BenchmarkRead_*` and `BenchmarkGoldsetParse`,
      `-benchmem`, untouched-row control. This is the comparison point for
      this change, not v0.10.0's numbers.

## 1. Pooled scratch object

- [ ] 1.1 Define a combined scratch struct `{Reader, Parser, tokens []token}`
      (or equivalent) with a `Reset` that clears every field a subsequent
      `Read` must not observe (`pos`, `line`, `col`, parser `depth`,
      `stats`, and the token slice length, retaining capacity).
- [ ] 1.2 Package-level `sync.Pool` in `core/`; checkout/return wired through
      `Dialect.ReadWithMaxDepthStats` (`core/dialect.go:408-424`) — the sole
      call site every `Eval` reaches (`runtime/eval.go:541`).
- [ ] 1.3 Test the scratch object directly, not through `sync.Pool` (a pool
      does not deterministically hand back the same slot — `runtime.GC()`
      clears pools rather than forcing reuse). Acquire one scratch object,
      parse source A, retain the returned tree, call `Reset()`, parse source
      B into the *same* object, then assert source A's retained tree is
      unchanged. This is the retention-safety pin the proposal requires, not
      optional.
- [ ] 1.4 `-race` test: concurrent `Read` calls across goroutines sharing the
      pool — no data race, no cross-call state leakage.

## 2. Right-sized node slices

- [ ] 2.1 `parseList`/`parseVector`/`parseReaderVector`: build element slices
      into pooled scratch storage during parsing (growth cost paid against
      reusable capacity, not fresh heap growth), then copy once into an
      exactly `len`-sized slice before constructing `List`/`Vector`.
- [ ] 2.2 Confirm `NewList`/`NewVector`'s reference-retention contract is
      preserved — the slice each receives is the final, right-sized,
      independently-owned one, never aliasing pooled scratch storage.
- [ ] 2.3 Test directly against the scratch object (same method as 1.3, not
      through `sync.Pool`): parse a collection literal, retain its elements,
      `Reset()` and reuse the same scratch object's node-slice storage for
      an unrelated `Read`, confirm the first literal's elements are
      unaffected.

## 3. Verify

- [ ] 3.1 Full floor: build/vet/gofmt/lint, full suite, `-race`, crossval,
      goldset both modes.
- [ ] 3.2 Interleaved benchstat vs. task 0's baseline: fixed per-`Read`
      allocations at or near zero amortized; `GoldsetParse/*` geomean B/op
      further down 55-70%; no regression on any cell.
- [ ] 3.3 Determinism check: repeated `Read` of the same source under the
      pooled path returns results indistinguishable (by `Equals` and by
      `String()`) from the non-pooled `reader-allocation-floor` baseline —
      pooling must not change observable output, only allocation shape.
- [ ] 3.4 Record `cmd/perfgate` as DEFERRED locally; real verdict from the
      hosted runner via `release-gate-activation`.
