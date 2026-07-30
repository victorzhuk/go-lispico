# Tasks — reader-allocation-floor

## 0. Decisions (before touching code)

- [ ] 0.1 `token` layout: confirm 32 bytes (`typ uint8`, `line`/`col`
      `int32`) as the target, or explicitly choose the 24-byte variant
      (`line`/`col` `uint16`, capped at 65,535) after checking whether any
      realistic embedder source could exceed that line/column count.
      Record the choice and why.
- [ ] 0.2 `readString` fast-path retention: accept that an escape-free string
      literal aliases the reader's input string indefinitely (documented,
      consistent with symbols/keywords), or add a length threshold above
      which the fast path copies instead of aliasing. Record the choice.

## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (one session, `-count=10`): `BenchmarkRead_*`
      (`core/bench_test.go`) and `BenchmarkGoldsetParse` (`internal/goldset`)
      with `-benchmem`; untouched-row control. `ReaderStats` values recorded
      for every fixture (must stay identical after implementation).

## 2. Implement

- [ ] 2.1 Prealloc `Tokenize`'s token slice from `len(input)`; size the
      divisor by measuring token-count-per-source-byte across the goldset
      corpus, not by guessing.
- [ ] 2.2 Shrink `token` per 0.1's decision. Update every field access;
      confirm no reader error test loses line/col precision it currently
      reports (`core/reader_test.go`, `core/error_test.go` if applicable).
- [ ] 2.3 Zero-copy fast path in `readString`: scan for the closing `"` with
      no intervening unescaped backslash; return the substring directly.
      Fall into the existing `strings.Builder` path unchanged when an escape
      is found. Wire in 0.2's threshold if that path was chosen.
- [ ] 2.4 Add benchmarks: non-default reader flags
      (`WithFunctionRef`/`WithReaderVector`/`WithoutBracketLiterals`), a
      deeply-nested input, a large flat list/vector literal, and a
      `Tokenize`-only benchmark distinct from `Parse`.

## 3. Verify

- [ ] 3.1 `ReaderStats` (nodes, bytes) bit-identical to 1.1's baseline for
      every fixture — this is the ADR 0011 evaluator-independence invariant,
      not negotiable.
- [ ] 3.2 Full floor: `go build ./...`, `go vet ./...`, `gofmt -l .`,
      `golangci-lint run`, `go test ./... -count=1`, `go test ./... -race
      -count=1`, `TestVMVsTreeWalker` crossval, `GOLDSET_MODE=eval` and
      `GOLDSET_MODE=vm` gold-set correctness.
- [ ] 3.3 Interleaved benchstat vs. 1.1 (same session, untouched control
      row): `Read_SmallListLiteral` ≤ 400 B / ≤ 4 allocs; `GoldsetParse/*`
      geomean B/op ≤ −40%; no cell regresses on allocs or bytes.
- [ ] 3.4 Reader-flag combination tests (function-ref, reader-vector,
      no-bracket-literals) green under the new `readString`/`Tokenize` paths
      — these flags don't change tokenization of strings, but confirm no
      interaction was missed.
- [ ] 3.5 Record `cmd/perfgate` as DEFERRED locally (`ns/op` on this
      workstation exceeds the gate's 5% tolerance); rely on `B/op`/`allocs/op`
      as the trustworthy local signal, real verdict from the hosted runner
      once `release-gate-activation` lands.
