# reader-allocation-floor

## Why

The reader is the single largest allocation source in the engine and has
never been optimized. Measured this session (`GOMAXPROCS=2`, `benchtime=200ms`,
pairing `BenchmarkGoldset` against `BenchmarkGoldsetParse` in one run):
`core.Read` accounts for 81–92% of `B/op` in 11 of the 13 gold-set fixtures,
and 76–89% of allocation count in most of them. `docs/profiling-baseline.md`'s
single pprof capture put the corpus-wide figure at ~75%; the direct per-fixture
pairing shows it is higher, not lower, in most cells. Micro benchmarks
(`core/bench_test.go`) show the shape starkly at small scale:
`Read_SmallListLiteral` on `(1 2 3)` (7 source bytes) costs 720 B / 7 allocs —
103 bytes of heap per source byte.

Three root causes, all in `core/reader.go`:

1. `Tokenize` (`:118`) starts from `var tokens []token` with no
   preallocation, across 18 `append` call sites. `token` (`:40-45`) is 40
   bytes (`tokenType(8) + string(16) + int(8) + int(8)`). Doubling-growth on
   an unsized slice costs ~2×capacity×40 bytes plus log₂(N) copies per
   `Read` — for `(1 2 3)`, roughly 600 of the measured 720 bytes.
2. `token`'s own layout wastes space: `tokenType` and the two position ints
   are all full machine words for values that need far less range.
3. `readString` (`:204-238`) always builds through a `strings.Builder`, even
   for the overwhelmingly common case of a string literal with no escape
   sequence — while `readNumber`, `readSymbol`, and `readKeyword`
   (`:251,:259,:268`) already return zero-copy substrings of the source.

This change attacks all three without touching object lifetime — no pooling,
no retained-state change beyond what already exists. That is
`reader-state-reuse`'s scope, sequenced after this one so the two review
surfaces (allocation-count reduction vs. reused-buffer retention safety)
stay separable.

## What Changes

- Prealloc the token slice in `Tokenize` from a cheap function of
  `len(input)` (a free upper bound; the exact divisor set by measurement
  against the fixture corpus, not guessed).
- Shrink `token` from 40 to 32 bytes: `typ` narrows to `uint8`, `line`/`col`
  narrow to `int32`. (A 24-byte layout exists — `line`/`col` as `uint16` — but
  caps source positions at 65,535; this change does NOT take that by default.
  The task list requires an explicit decision recorded before implementation,
  not a default.)
- Add a zero-copy fast path to `readString`: scan for the closing quote first;
  if no backslash appears before it, return `r.input[start:end]` directly,
  falling into the existing `strings.Builder` path only when an escape is
  present.
- This fast path extends how long the *source string* stays reachable: today
  an escape-free string literal is an independent copy; after this change it
  aliases the reader's input. Symbols and keywords already alias the source
  this way, so this is not a new *class* of retention — but the allocation
  ledger accounts strings by length (`StringShallowBytes`), so accounted
  retained bytes stay correct while *real* retained memory grows silently
  against `MaxRetainedBytesPerEnv` (ADR 0012) for any embedder holding parsed
  forms long-term (`Engine.LoadScope` retaining rule source for a process
  lifetime is the concrete case). The task list requires an explicit decision
  — accept it with a documented note, or add a length threshold above which
  the fast path copies instead of aliasing — not a silent default.
- Add missing benchmark coverage: no existing benchmark exercises non-default
  reader flags (`WithFunctionRef`, `WithReaderVector`, `WithoutBracketLiterals`),
  deeply-nested or large input, or measures `Tokenize` separately from
  `Parse`.

## Impact

- Affected specs: `core-engine` (new requirement: reader allocation cost
  scales with input size, not with a hidden constant-factor penalty from
  unsized growth).
- Affected code: `core/reader.go` only — `Tokenize`, `token`, `readString`.
  No change to `Parser`, `NewList`/`NewVector` retention semantics, or any
  caller (`core/dialect.go`, `runtime/eval.go`).
- Invariant that must not move: `ReaderStats` (nodes/bytes), which feeds
  `ChargeEvalReader` (`runtime/eval.go:482`) under ADR 0011's
  evaluator-independent ledger — ledger accounting stays byte-identical even
  though the underlying reader allocates less.
- Expected: `Read_SmallListLiteral` 720 B / 7 allocs → ≤ 400 B / ≤ 4 allocs;
  `GoldsetParse/*` geomean B/op −40% or better.
- Risk: low for the prealloc and `token` shrink (pure internal representation).
  The `readString` aliasing decision is the one place this change can leak a
  real (not just accounted) memory-retention change to an embedder — must be
  a stated decision, verified by a test, not an assumption.
