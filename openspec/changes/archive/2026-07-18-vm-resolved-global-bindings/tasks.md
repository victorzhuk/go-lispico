## 1. Binding cells in core

- [x] 1.1 Introduce the exported `core.Cell` type (value + canonical flag, both guarded by the owning `Env`'s lock) and rebase `Env.vars` on `map[string]*Cell`, with the first cell per scope stored inline in the `Env` to avoid a heap alloc; `Set`/`SetCanonical`/`Delete`/`Get`/`GetCanonical`/`Find`/`MergeInto` keep their signatures and semantics (delete = tombstone). (Deviation from lock-free: value stored inline, read under a short `RLock` — see design.md "as built".)
- [x] 1.2 Add `Env.Cell(name)`/`CellLocal(name)` chain/local resolution, `Env.ReadCell` for a coherent value+canonical read, and a per-env new-name generation counter (`atomic.Uint64`).
- [x] 1.3 Tests: cell write-through visibility, tombstoned delete, canonical flag cleared on rebind, concurrent Set/Get under `-race`, value+canonical coherent under concurrent writes, `AllocsPerRun` for the read path (0 allocs).

## 2. Chunk call-site cache

- [x] 2.1 The chunk builds its site table lazily from `Code` (`Chunk.EnsureSites`/`buildSites`, deduped by symbol), published via one `atomic.Pointer`; the engine builds it on the first cache hit (proven reuse) so a run-once form never pays for it. No per-instruction compiler bookkeeping.
- [x] 2.2 VM `OpGetGlobal`: guarded site fast path (env identity + generation), depth-0-only publish scoped to the root env, fallback chain resolution.
- [x] 2.3 Tests: cached-site hit returns rebound value after `Set`; new-name bind in the resolution env invalidates via generation; chunk shared across two engines with different root envs resolves per-env.

## 3. Native-op dispatch via cells

- [x] 3.1 Replace `canonicalAt` with a per-operand-slot frozen-op scratch: `OpGetGlobal` freezes the operator's canonical-native decision at head-resolution time, `dispatchNativeOp` consumes it; delete `canonicalAt`, its `push` zeroing rebuilt for the scratch, and the lookup-time capture protocol. (Frozen at lookup, not re-read at dispatch — preserves tree-walker parity under a mid-argument canonical flip.)
- [x] 3.2 Tests: rebound `+` falls back to the custom function; canonical restored during argument evaluation still matches the tree-walker; ancestor-owned canonical ops keep the native path (GoFunc never called); recursive native path; crossval parity suite green.

## 4. Verify

- [x] 4.1 `go test ./...` and `go test -race ./core/... ./runtime/...` green.
- [x] 4.2 Goldset gate: non-regressing on 11/12 cells (allocs + bytes); `twice-macro` +0.21% B/op (8-byte site-cache pointer on the recompile-every-op chunk, within CI noise) per ADR 0008.
- [x] 4.3 Bench evidence recorded: fib bytecode ~10.6% faster (locked reads keep the RWMutex cost the lock-free design removed); `pprof` confirms the `GetCanonical` scope-chain map walk off the hot path.
