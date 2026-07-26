## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (≥10 counts): article Startup row plus split
      probes (`New`-only; `New`+`Use`; full) with `-benchmem`. Record the
      dcbdf62 figures: 39.6µs/286 allocs full, 3.5µs/42 New-only,
      20.7µs/244 New+Use.
- [ ] 1.2 Alloc-profile the full startup and record the
      `RegisterValue`/`register*`/dialect shares as the before-picture.

## 2. Completed-layer short-circuit

- [ ] 2.1 Add layer completion to the template registry: the first
      successful `Init` for a `{dialectFP, plugin name+version}` key marks
      the layer complete; failure leaves it incomplete for retry.
      Single-flight concurrent first `Use` of one key.
- [ ] 2.2 `Use` attaches a completed layer without running `Init`. Scope
      fail-closed: only plugins whose registration routes through the
      template registry qualify; direct-env plugins keep per-engine `Init`.
- [ ] 2.3 Extend the layer key with plugin identity/version from
      `Metadata()` so two builds of one plugin name get distinct layers.
- [ ] 2.4 Semantics tests: materialization, enumeration, shadowing,
      deletion, `UnloadPlugin` (removes attachment + materialized bindings,
      not the shared layer), hot-reload — all behave identically on a
      first-engine (built the layer) and a second-engine (attached it).
      Concurrent engines under `-race`.

## 3. Memoized stock dialects

- [ ] 3.1 `cl.Dialect()`/`clojure.Dialect()` return process-memoized values;
      the memoized constructor forces `resolve()` and `Fingerprint()` so the
      shared value is complete before it escapes.
- [ ] 3.2 Immutability audit: enumerate every write site of the resolved
      form table and vocabulary map; prove none runs post-construction; add
      a test that engine-level operator redefinition on one engine does not
      alter another engine's dialect behavior (pins "Per-Engine dispatch
      isolation" over the shared value).
- [ ] 3.3 Custom (non-stock) dialect construction stays byte-identical in
      behavior; `Fingerprint()` caching inside a `Dialect` value must not
      change its output for any dialect (compare against the uncached hash
      across the test dialect corpus).

## 4. First-eval tail

- [ ] 4.1 Profile the `Use`→`Eval`→`Close` tail (~19µs) and attribute it
      across: first-touch materialization, tokenize+compile, cache
      admission, cold `vm.New`, `Close`.
- [ ] 4.2 Apply the dominant fix per the design's decision table
      (macro-epoch-gated user-chunk reuse in the existing process tier /
      batched materialization / pool pre-size). Do not implement branches
      the profile does not justify.

## 5. Measure

- [ ] 5.1 Re-run 1.1 interleaved. Success criteria: second-and-later engine
      full startup < 10µs; `Use` band < 1µs; no change to first-engine
      correctness; goldset both modes non-regressing (steady-state rows must
      not pay for startup sharing).
- [ ] 5.2 Cross-engine correctness: N engines constructed concurrently, each
      evaluating the dialect-vocab test corpus — results identical to
      today's per-engine construction.

## 6. Verify

- [ ] 6.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean.
- [ ] 6.2 Full suite + `-race` (the concurrency surface is the point of this
      change — treat any race as a design fault, not a test flake);
      crossval; goldset both modes; `cmd/perfgate` one-sided non-regression.
