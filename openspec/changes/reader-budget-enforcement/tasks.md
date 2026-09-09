## 0. Prerequisites and baseline

- [x] 0.1 Confirm `metered-call-deadlines` and `evaluation-outcome-settlement` are implemented, validated, and archived; verify their accepted requirements are present before changing reader lifecycle wiring.
- [x] 0.2 Use existing-service-strict testing. Capture the guarded-read failure through current `Eval`: wide source exceeds a 1 KB budget only after full allocation, and already-cancelled comment-only source succeeds; verify the characterization fails once asserted against the new contract.
- [x] 0.3 Check all source callers of `readForms`, reader statistics tests, pool tests, and ADR 0007/0011; record the current charge formula and the explicit filesystem/legacy-reader boundary in the design baseline.

## 1. Failing resource contracts

- [x] 1.1 Add new bounded reader admission tests for wide flat forms, escaped strings, long trivia/tokens, malformed suffixes, conversion/error storage, and overflow-safe arithmetic; verify a long overflowing number with ample reductions fails before conversion under a 1 KB allocation ceiling, and extending an unread suffix does not increase admitted storage.
- [x] 1.2 Add new cancellation/deadline tests covering both scan passes, parsing, final list linking, escaped-prefix copying, long map keys/collisions, numeric conversion admission, empty input, comment-only input, and terminal precedence over syntax errors; use deterministic contexts/clocks and verify the 128-unit bound plus the documented numeric-conversion exception.
- [x] 1.3 Add new successful-reader accounting tests for exact total allowance and one byte below, prepaid payloads, generated quote nodes, parser workspace growth, conversion/construction storage, and unchanged `ReaderStats`; verify no charge depends on pooled capacity and multiple checkpoints plus a partial flush introduce no undocumented reductions.

## 2. Guarded reader and ownership

- [x] 2.1 Add the new `Dialect.ReadWithContextStats` entry point and private per-read budget state described in the design; verify legacy reader signatures, results, positions, and statistics remain unchanged on all existing fixtures.
- [x] 2.2 Budget byte advances on both scanning passes, output nodes, and construction/copy work; flush through `EvalMeter.ChargeReductions` and directly observe context/deadline without nesting evaluator polling. Pre-admit numeric conversion work and storage, bound diagnostics, and verify the work/cancellation tests pass without per-byte atomic ledger operations or hidden charges.
- [x] 2.3 Reject oversized token plans during counting, then reserve deterministic token storage and decoded payloads before materialization; verify the wide-source and escaped-payload tests reject before oversized allocation.
- [x] 2.4 Add private guarded collection constructors within existing representations, including chunked list linking and small-map/HAMT construction with budgeted hashing, comparisons, collision scans, and buffer growth. Reserve output/workspace/construction storage first, track prepaid payload ownership, and verify promotion, duplicate keys, exact charges, and unchanged `ReaderStats` and small-map allocation contracts.
- [x] 2.5 Clear scratch references on all exits and discard retained buffers above the current ceiling; verify same-object low-budget reuse, failed-read reuse, retained AST independence, and concurrent cross-dialect reads under race detection.

## 3. Runtime integration

- [x] 3.1 Route `readForms` through the guarded reader and remove its duplicate post-parse output charge; arm the effective deadline before parsing in every runtime-owned source path and verify one ledger spans read, compile, and execution.
- [x] 3.2 Add public entry tests for `Eval`, `EvalWithBindings`, `LoadScope`, and background hot reload with both supported evaluator modes, dialect reader surfaces, and context/engine meters; verify over-budget source executes no forms and a failed reload publishes no replacement bindings.
- [x] 3.3 Verify source errors, meter denial, and cancelled empty reads produce exactly one settled outcome where events/stats apply; retain existing lifecycle settlement and panic behavior from the predecessors.

## 4. Documentation and verification

- [x] 4.1 Amend existing ADR 0007/0011, `ARCHITECTURE.md`, and `README.md` with the guarded-reader boundary, deterministic work/storage table, opaque numeric-conversion bound, bounded diagnostics, guarded collection construction, and no-duplicate-charge rule; verify every stated guarantee has a scenario and add an unreleased `CHANGELOG.md` entry.
- [x] 4.2 Run targeted tests with `go test -timeout 2m -p 2 -parallel 2 ./core ./runtime`; no Makefile target selects these packages. Verify reader, lifecycle, statistics, and pool regressions pass.
- [x] 4.3 Run `make build`, `make lint`, and `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`, then targeted race checks with `go test -race -timeout 2m -p 2 -parallel 2 ./core ./runtime`; record pass/failure evidence. Resolve any pre-existing ignored `bin/` package contamination before using the full wrapper as a gate; do not silently weaken its scope.
- [x] 4.4 Compare bounded parsing benchmarks and existing gold-set checks against the prerequisite baseline using the established performance workflow; report both evaluators and unchanged gate thresholds, with deterministic allocation evidence separate from timing noise.
- [x] 4.5 Run `openspec validate reader-budget-enforcement --strict --json` and inspect final code/spec/doc consistency; verify all accepted requirements and task results are present before archive.
