# Tasks — reader-allocation-floor

## 0. Decisions (before touching code)

- [x] 0.1 `token` layout: confirm 32 bytes (`typ uint8`, `line`/`col`
      `int32`) as the target, or explicitly choose the 24-byte variant
      (`line`/`col` `uint16`, capped at 65,535) after checking whether any
      realistic embedder source could exceed that line/column count.
      Record the choice and why.

      **Decided: 32 bytes (`typ uint8`, `val string`, `line`/`col` `int32`).
      The 24-byte variant is ruled out empirically, not on judgment.**
      `core/reader_limits_test.go:11` reads
      `strings.Repeat("(", 100000) + "1" + strings.Repeat(")", 100000)` —
      100,001 columns on a single line, and `Tokenize` carries no depth
      guard (the depth check lives in `parseForm`, which runs after
      tokenization completes), so every one of those columns is computed.
      A `uint16` `col` wraps mod 65,536 on input the shipped suite already
      produces. `int32` has no reachable overflow path.

      32 is also the true floor for this field set, not an estimate: `val
      string` forces 8-byte alignment, so `1 + 16 + 4 + 4 = 25` data bytes
      round to 32 under every field ordering. The 24-byte layout is only
      reachable by taking the `uint16` truncation above.

      Public surface is unaffected: `LispicoError.Line`/`Col`
      (`core/error.go:16-17`) and `NewReadError(msg string, line, col int)`
      stay `int`. The narrowing is internal to token construction and
      widens back at `core/reader.go:180,184,189,193` plus the depth-limit
      literal at `:358`; the compiler catches any missed conversion.
- [x] 0.2 `readString` fast-path retention: accept that an escape-free string
      literal aliases the reader's input string indefinitely (documented,
      consistent with symbols/keywords), or add a length threshold above
      which the fast path copies instead of aliasing. Record the choice.

      **Decided: accept the aliasing and document it. No length threshold.**
      This is not a new risk class. `readSymbol` already returns
      `r.input[start:r.pos]` (`core/reader.go:254-260`), and `evalDefn`
      (`core/eval.go:1150-1168`) stores that substring straight into
      `Lambda{Name: name.V}`. There is no `strings.Clone` anywhere in
      `core/` or `runtime/`, and no interning on the bind path — so every
      `defn` in every source passed through `Engine.LoadScope` already pins
      the whole source array for as long as the returned env or a closure
      captured from it survives.

      A string-only threshold would be asymmetric: it guards one `Value`
      type while every captured function or macro name still pins the same
      array, so it narrows one path without closing the hole, and it puts a
      magic number and a branch on the hot path this change exists to
      speed up.

      What an embedder observes: a `LoadScope`d env retained for the
      process lifetime keeps the full source byte array alive as long as
      any symbol name or escape-free string literal from it survives —
      identical in shape to today's `Symbol` exposure, sized by source
      length rather than by charged bytes. ADR 0012's ledger still bounds
      the *accounted* total per env at `MaxRetainedBytesPerEnv`; it cannot
      distinguish an aliased substring from a fresh copy, which is the
      deviation ADR 0012 already recorded and deferred pending yagel
      measurement. That deferral, not a new one, governs here. A test
      asserts the fast path actually aliases, so the accepted behavior is
      regression-locked rather than implicit.

## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (one session, `-count=10`): `BenchmarkRead_*`
      (`core/bench_test.go`) and `BenchmarkGoldsetParse` (`internal/goldset`)
      with `-benchmem`; untouched-row control. `ReaderStats` values recorded
      for every fixture (must stay identical after implementation).

      Pinned at the pristine worktree, `GOMAXPROCS=2`, `-count=10`.
      Interleaving is moot for these metrics: `B/op` and `allocs/op` came
      back identical across all ten counts for every cell, so drift cannot
      confound them. `ns/op` is not used as a signal here (see 3.5).

      | cell | B/op | allocs/op |
      | --- | --- | --- |
      | Read_SmallListLiteral | 720 | 7 |
      | Read_SmallVectorLiteral | 768 | 7 |
      | Read_Simple | 1552 | 12 |
      | Read_Representative | 14480 | 69 |
      | GoldsetParse/guard-nil | 3520 | 30 |
      | GoldsetParse/kw-lookup | 3552 | 29 |
      | GoldsetParse/loop-sum | 3664 | 34 |
      | GoldsetParse/text-render | 3864 | 42 |
      | GoldsetParse/twice-macro | 3872 | 42 |
      | GoldsetParse/route-decision | 7192 | 50 |
      | GoldsetParse/queue-promote | 7200 | 47 |
      | GoldsetParse/counter-closure | 7504 | 55 |
      | GoldsetParse/pipeline | 7520 | 46 |
      | GoldsetParse/safe-parse | 7720 | 68 |
      | GoldsetParse/registry-fold | 7736 | 61 |
      | GoldsetParse/merge-config | 7760 | 55 |
      | GoldsetParse/rule-load | 17072 | 154 |

      `Read_SmallListLiteral` at 720 B / 7 allocs reproduces the figure the
      proposal cites, so the proposal's measurement is confirmed
      independently rather than taken on trust.

      `ReaderStats` is pinned as executable expectations rather than prose:
      `TestReaderStats_Goldset` and `TestReaderStats_Bench`
      (`core/reader_stats_test.go`) hardcode {Nodes, Bytes} for all 13
      fixtures and the four benchmark sources, captured from the untouched
      tree. Any drift fails the suite instead of needing a manual diff.

## 2. Implement

- [x] 2.1 Give `Tokenize`'s token slice a fixed initial capacity instead of
      starting from `nil`. Amended from "prealloc from `len(input)`,
      divisor sized by measurement" — the measurement the task asked for
      is what rules that shape out.

      Bytes-per-token is ~1.17 for `Read_SmallListLiteral` (`"(1 2 3)"`)
      but 3.0-7.3 across the 13 goldset fixtures, mean ~4.5. No single
      `len(input)` divisor satisfies both of 3.3's criteria: `len(input)+1`
      over-allocates 3-7× on real sources and regresses `rule-load`
      (~19.4KB vs ~16.4KB today), violating "no cell regresses";
      `len(input)/4` misses `Read_SmallListLiteral`'s ≤4 allocs.

      **Shipped: an exact count, not a fixed constant.** A fixed initial
      capacity of 8 was tried first — it dominates `nil`-start doubling, so
      nothing regressed — but it only reached −21% geomean, missing 3.3.
      Probing `tokenizeInitialCap` at 4096 (one 131072-byte allocation) and
      subtracting that constant isolated the residual per cell: 800 bytes
      for `guard-nil` up to 5136 for `rule-load`. Token-slice doubling was
      26-52% of every cell's remaining bytes, and an exactly-sized single
      allocation projected −52.5%.

      So `Tokenize` counts first and allocates once: `countTokens` runs the
      scan to completion, rewinds `pos`/`line`/`col`, and `Tokenize`
      allocates `make([]token, 0, n)`. The per-character switch moved out of
      `Tokenize` into `nextToken`, which **both** passes drive — the count
      cannot disagree with what the emitting pass produces, because it is
      the same function. The constant is gone entirely; nothing needs a
      floor once the size is exact.

      Measured: −50.5% geomean, every individual cell past −40% on its own
      (worst `twice-macro` −40.5%), no cell regressed on bytes or allocs.

      Cost this introduced, and its fix: the counting pass originally ran
      the full `nextToken`, so escaped literals decoded through
      `strings.Builder` twice. No gold-set fixture contains an escaped
      string, so the corpus could not see it — measured directly against
      master on a 40-escaped-literal source, allocations regressed 216 → 330
      while bytes improved. A `countOnly` flag now suppresses the decode
      buffer writes and nothing else, leaving the boundary scan identical on
      both passes; the counting pass is zero-alloc and the same source reads
      210. `BenchmarkRead_EscapeHeavyStrings` exists so this gap cannot
      reopen unseen.
- [x] 2.2 Shrink `token` per 0.1's decision. Update every field access;
      confirm no reader error test loses line/col precision it currently
      reports (`core/reader_test.go`, `core/error_test.go` if applicable).

      `tokenType` itself narrowed to `uint8` rather than only the field,
      which avoids conversions at ~30 sites and costs nothing — the type has
      no `String()` method. `Tokenize`'s per-iteration locals are captured as
      `int32` once, so the inline token literals need no per-site cast; six
      error and report sites widen back with `int(...)`.
      `TestToken_Sizeof` asserts `unsafe.Sizeof(token{}) == 32` so the layout
      cannot regrow silently. `TestTokenize_ColBeyond65535` reads 70,000
      spaces then `foo` and asserts col 70001 — under `uint16` that reads
      4465, which is the 0.1 decision regression-locked rather than merely
      argued. `core/reader_positions_test.go` and
      `core/reader_limits_test.go` pass unchanged.
- [x] 2.3 Zero-copy fast path in `readString`: scan for the closing `"` with
      no intervening unescaped backslash; return the substring directly.
      Fall into the existing `strings.Builder` path unchanged when an escape
      is found. Wire in 0.2's threshold if that path was chosen.

      No threshold, per 0.2. The scan advances through `r.next()` rather than
      raw indexing so `r.line`/`r.col` track exactly as the byte-by-byte loop
      did, including newlines inside a literal;
      `TestReadString_EmbeddedNewlineTracksPosition` locks that down, since a
      naive raw-slice fast path would silently misreport positions after any
      multi-line string. `TestReadString_NoEscapeSharesBackingArray` compares
      `unsafe.StringData` to prove the fast path actually engages — without
      it the optimization could silently stop firing — and doubles as the
      regression lock 0.2 requires. The escaped path keeps its
      `strings.Builder` and shares its decode switch with `appendEscape`.
- [x] 2.4 Add benchmarks: non-default reader flags
      (`WithFunctionRef`/`WithReaderVector`/`WithoutBracketLiterals`), a
      deeply-nested input, a large flat list/vector literal, and a
      `Tokenize`-only benchmark distinct from `Parse`.

      All present in `core/bench_test.go`, plus
      `BenchmarkRead_EscapeHeavyStrings`, which the task list did not ask
      for and should have: the escaped-string path was the one shape no
      benchmark and no gold-set fixture covered, which is exactly why 2.1's
      regression there went unmeasured until it was probed by hand. Reading
      the corpus for what it cannot exercise belongs in this task, not only
      in review.

## 3. Verify

- [x] 3.1 `ReaderStats` (nodes, bytes) bit-identical to 1.1's baseline for
      every fixture — this is the ADR 0011 evaluator-independence invariant,
      not negotiable.

      Held. `TestReaderStats_Goldset` and `TestReaderStats_Bench` pass
      unchanged through every slice. The pins name all 13 fixtures
      explicitly rather than globbing, so the test cannot pass vacuously on
      an empty match.
- [x] 3.2 Full floor: `go build ./...`, `go vet ./...`, `gofmt -l .`,
      `golangci-lint run`, `go test ./... -count=1`, `go test ./... -race
      -count=1`, `TestVMVsTreeWalker` crossval, `GOLDSET_MODE=eval` and
      `GOLDSET_MODE=vm` gold-set correctness.

      All green. `golangci-lint` reports 0 issues, `gofmt -l` is empty.

      One flake seen, investigated, and cleared:
      `plugins/json` `TestDecodeHashMap_Scaling` failed once under a
      full-suite `-race` run at ratio 3.03 against a threshold of 3.0 — a
      wall-clock scaling assertion with a 1% margin. It passed 3/4
      full-suite `-race` runs here and 5/5 in isolation, and 5/5 on
      untouched master. `plugins/json` production code never calls
      `core.Read` or `Tokenize`, so this change cannot reach the decode path
      it measures. Pre-existing, unrelated; worth its own change to widen
      the threshold or make it load-independent.
- [x] 3.3 Interleaved benchstat vs. 1.1 (same session, untouched control
      row): `Read_SmallListLiteral` ≤ 400 B / ≤ 4 allocs; `GoldsetParse/*`
      geomean B/op ≤ −40%; no cell regresses on allocs or bytes.

      All three met, verified independently of the implementer's report.
      `Read_SmallListLiteral` 720 B/7 allocs → 304/4. `GoldsetParse/*`
      geomean −50.5%, with every individual cell also past −40% (worst
      `twice-macro` −40.5%). No cell regressed on either axis; alloc total
      across the 13 cells fell 713 → 630. Other reader cells: `Read_Simple`
      1552/12 → 560/8, `Read_SmallVectorLiteral` 768/7 → 352/4,
      `Read_Representative` 14480/69 → 5232/62.
- [x] 3.4 Reader-flag combination tests (function-ref, reader-vector,
      no-bracket-literals) green under the new `readString`/`Tokenize` paths
      — these flags don't change tokenization of strings, but confirm no
      interaction was missed.

      Green, and extended past what this task asked for. Review found the
      count/emit parity test only ever ran under `defaultReaderFlags`, which
      left the one branch where a flag changes the token *count* uncovered:
      the `#` sub-dispatch, where `#'x` is one token with `functionRef` on
      and two with it off. A disagreement there would have failed no test —
      `append` grows silently past a wrong capacity, and the dialect tests
      assert parsed values, not token counts — so it would have surfaced
      only as an unexplained `B/op` number. `TestTokenize_CountMatchesLen_Flags`
      now covers `#'x`, `#(...)`, and the flags-off path.
- [x] 3.5 Record `cmd/perfgate` as DEFERRED locally (`ns/op` on this
      workstation exceeds the gate's 5% tolerance); rely on `B/op`/`allocs/op`
      as the trustworthy local signal, real verdict from the hosted runner
      once `release-gate-activation` lands.

      **`cmd/perfgate`: DEFERRED, not run.** `ns/op` on this box drifts far
      wider than the gate's tolerance, which is the standing finding, not a
      new one. Every acceptance judgment above rests on `B/op` and
      `allocs/op`, which came back identical across all ten counts of the
      baseline and so are drift-immune.

      The hosted verdict is further away than this task assumed:
      `release-gate-activation` is blocked on a `v0.11.0` cut that has not
      happened, so no `bench-vm.txt` baseline exists yet. When it does, this
      change's cells will read against a reader that already improved —
      which is exactly the re-baseline obligation that change carries as its
      own task 5.1.
