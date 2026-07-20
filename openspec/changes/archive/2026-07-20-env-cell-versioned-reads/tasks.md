## 1. Cell version

- [x] 1.1 Add `version atomic.Uint64` to `core.Cell`; bump inside the existing `env.mu` critical sections on every value/canonical/tombstone mutation (`Set`, `SetFunc`, `Delete`, canonical marking, tombstone revival). Audit ALL cell-mutation sites in `core/env.go` — a missed bump is a stale-read bug.
- [x] 1.2 Unit tests on `Env` directly: version bumps on each mutation kind; unchanged on reads.

## 2. Snapshot in siteEntry

- [x] 2.1 Extend `siteEntry` with `val core.Value`, `canonical bool`, `ver uint64`; capture coherently (under the env read lock) at both publication sites in `resolveGlobalValue`.
- [x] 2.2 Hit path: serve the snapshot when `env`+`gen`+`version` all match; version mismatch → locked `ReadCell`, no republication.
- [x] 2.3 Guard: tombstoned cell (`v == nil`) at publication publishes no snapshot / reports unbound, matching today.

## 3. Tests

- [x] 3.1 Rebind visibility through a warmed site: read → `set!`/`def` rebind → read again observes the new value (same engine, cached chunk; both dialects).
- [x] 3.2 Delete visibility: warmed site, then `Delete`/tombstone → next read reports undefined, not the snapshot.
- [x] 3.3 Canonical-flag transition: warmed native-op site, then `Bind` over the operator → fast-path decision follows the tree-walker (existing crossval cell must stay green).
- [x] 3.4 Concurrency: hammer test — one goroutine rebinding, N goroutines executing a chunk reading the global; every observed value is old-or-new; `-race` clean.

## 4. Verify

- [x] 4.1 `go test ./...`, `-race`, crossval parity suite green.
- [x] 4.2 `GOLDSET_MODE=vm` goldset gate non-increasing (allocs and bytes) — pay attention to `set!`-heavy and loop cells.
- [x] 4.3 Bench evidence (benchstat ≥6): fib bytecode delta; `atomic.(*Int32).Add` / `ReadCell` off the fib profile top; `BenchmarkEngine_ParallelCallBytecode` recorded.
