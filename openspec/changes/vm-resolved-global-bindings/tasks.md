## 1. Binding cells in core

- [ ] 1.1 Introduce the internal `cell` type (atomic value + canonical flag) and rebase `Env.vars` on `map[string]*cell`; `Set`/`SetCanonical`/`Delete`/`Get`/`GetCanonical`/`Find`/`MergeInto` keep their signatures and semantics (delete = tombstone).
- [ ] 1.2 Add `Env.Cell(name)` chain resolution and a per-env new-name generation counter (`atomic.Uint64`).
- [ ] 1.3 Tests: cell write-through visibility, tombstoned delete, canonical flag cleared on rebind, concurrent Set/Get under `-race`, `AllocsPerRun` for read path (0 allocs).

## 2. Chunk call-site cache

- [ ] 2.1 Compiler assigns a site index to every `OpGetGlobal` and native-op emission; chunk carries the `sites` table.
- [ ] 2.2 VM `OpGetGlobal`: guarded site fast path (env identity + generation), fallback chain resolution that publishes the site.
- [ ] 2.3 Tests: cached-site hit returns rebound value after `Set`; new-name bind in the resolution env invalidates via generation; chunk shared across two engines with different root envs resolves per-env.

## 3. Native-op dispatch via cells

- [ ] 3.1 Replace `canonicalAt` protocol with site-cell canonical check in `dispatchNativeOp`; delete `canonicalAt`, its `push` zeroing, and lookup-time capture.
- [ ] 3.2 Tests: rebound `+` falls back to the custom function (existing spec scenario), canonical ops never route through stdlib `GoFunc` on the hot path (assert via counter hook or profile-guided benchmark), crossval parity suite green.

## 4. Verify

- [ ] 4.1 `go test ./...` and `go test -race ./core/... ./runtime/...` green.
- [ ] 4.2 Goldset gate: engine-sensitive hot cells non-regressing per ADR 0008 thresholds.
- [ ] 4.3 Bench evidence recorded: fib bytecode ns/op before/after (target ≥25% improvement), `pprof` top confirms `Env.GetCanonical` / `mapaccess2_faststr` off the top-10.
