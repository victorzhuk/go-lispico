# Design — resolved global bindings

## Context

Hot-path facts (fib(20) bytecode, CPU profile, v0.7.0):

- `Env.GetCanonical` cum 33% — `mapaccess2_faststr` 0.23s + RWMutex atomics 0.09s of 1.08s total.
- `sync/atomic.Int32.Add` flat 13% — RWMutex reader counters.
- stdlib `GoFunc` bodies visible under `vm.call` (~7%) — the `canonicalAt` marker machinery loses markers on some execution shapes and falls back to full dispatch even though `+`/`<` are canonical (verified: `GetCanonical` returns `canonical=true` for all operators on a live clojure-dialect engine).

Constraints:

- ADR 0003: concurrent `Eval`/`Call` on one Engine is a supported contract; envs are individually synchronized. Any cache must be race-free under concurrent chunk execution.
- Chunk cache (ADR 0006): one compiled chunk is shared across evaluations and across engines with different root envs; a per-chunk cache must not pin one env's bindings into another env's run.
- Rebind semantics: `(def + my-fn)` must be visible to subsequent reads and must divert native ops to the call path (spec: "Rebound operator falls back").

## Decision

### 1. Binding cells in `core.Env`

```go
type cell struct {
    v         atomic.Value // core.Value
    canonical atomic.Bool
}
```

- `Env.vars` becomes `map[string]*cell` (locked for map mutation only); `Set` publishes through `cell.v.Store` and clears `canonical`; `SetCanonical` sets both. Deleting a name tombstones the cell (nil value) rather than removing it, so a cached cell pointer stays valid and observes the deletion.
- `Env.Cell(name) (*cell, bool)` walks the chain once and returns the owning cell. `Get` keeps its current signature on top of cells.
- Cost model: read = one atomic load; bind = map access under lock + atomic store. Global binds are rare (top-level `def`), reads are the hot path — same trade tengo/starlark make by construction. Independent benchmarks put an atomic-pointer read at ~0.5 ns vs ~49 ns for an RWMutex read path — two orders of magnitude on exactly the operation `OpGetGlobal` repeats.

### 2. Per-chunk call-site cache, env-identity guarded

Chunk gains `sites []siteCache` parallel to global-reading instructions (index assigned at compile time, operand B or a side table):

```go
type siteCache struct {
    env  atomic.Pointer[core.Env] // globals env this resolution is valid for
    cell atomic.Pointer[cell]
}
```

`OpGetGlobal` fast path: load `site.env`; if it equals the frame's resolution env, load `site.cell`, read value. Miss → resolve via `Env.Cell`, publish both (last-writer-wins is safe: any published pair is internally consistent because a cell is permanent for its env chain).

Guard choice — env identity, not epoch: chunks run against `vm.globals` (Eval) or a closure's captured env (call). Identity equality is one pointer compare and is exact; an epoch scheme would invalidate all sites on any bind anywhere. Frames whose env is a fresh per-call child (`needsCallEnv`) resolve through the chain as today — the site cache applies when the frame env is a stable env (globals or a top-level closure's captured env), which covers the benchmark and typical rule workloads.

Shadowing correctness: a cached cell is the *owning* cell for that name in that chain at resolution time. A later bind in a child env between the chunk's env and the owner would change ownership — but a chunk's frame env is fixed for the frame's lifetime, and binds into stable envs create the cell in that env, changing what `Env.Cell` returns. Therefore the guard must also cover the resolution env's local-bind generation: bump a per-env `atomic.Uint64` generation on any first-bind of a *new* name; sites store and compare it. Rebinds of an existing name go through the same cell — no invalidation needed.

### 3. Native-op dispatch through the cell

`OpAdd`..`OpEq` currently trust `canonicalAt[fnIdx]`, a stack-slot marker zeroed by unrelated pushes. Replace: compileNativeOp emits the site index with the opcode; dispatch loads the site's cell and checks `canonical` — one atomic load, no stack bookkeeping, no fallback flakiness. `canonicalAt`, `push`-time zeroing, and the lookup-time capture protocol are deleted.

## Alternatives considered

- **Global slot table (tengo-style `[]Value` + compile-time index)**: fastest reads, but requires a closed world at compile time; lispico chunks are cached across engines and envs, and plugins bind names after compilation. Rejected for now; cells + site cache get most of the win without freezing the namespace.
- **Symbol interning (`unique` package)**: reduces string-hash cost but keeps the map+lock walk. Subsumed by cells.
- **Striped/sharded env locks**: treats the symptom (contention) not the cost (per-read map+lock). Rejected.

## Risks

- Shadowing is the known failure mode of index-style resolution: starlark-go shipped exactly this class of optimization (PR #576, −22% on a call benchmark) and reverted it (a079b1f) because it broke on duplicate local names its scoping rules allow. The generation guard plus the shadowing tests in tasks exist specifically to close that hole before merge.
- Cell tombstones keep deleted names' cells alive — bounded by namespace size, acceptable.
- `atomic.Value` boxing of interface values: `atomic.Value` requires consistent concrete types; store `core.Value` via a single wrapper type or use `atomic.Pointer[core.Value]` with one extra indirection. Decide at implementation with `AllocsPerRun` evidence.
- Site cache adds 3 words per global-reading instruction per chunk — negligible against chunk cache limits (4096 entries).

## Verification

- Crossval + dialect suites (parity), `-race` concurrency suites (ADR 0003 scenarios).
- New tests: rebind visibility through a cached site; delete-then-read through a cached cell; concurrent `Set` + chunk execution under `-race`.
- Bench evidence: fib bytecode in go-lispico-bench and `internal/goldset` hot cells; profile confirms `GetCanonical` gone from top-10.

## Implementation notes (as built)

The lock-free cell (section 1) was implemented and measured, then revised. Four
findings from adversarial review + the ADR 0008 gate reshaped the final design:

1. **Locked reads, not lock-free (value stored inline).** `atomic.Value`
   panics on inconsistent concrete types and on `Store(nil)` tombstones, so the
   first fix used `atomic.Pointer[cellState]`. But any lock-free atomic read of
   a multi-word interface value requires publishing a heap snapshot **per
   write**, which regressed the ADR 0008 allocation gate on write-heavy loops
   (`loop`/`recur` mirrors captured locals into the env every iteration:
   loop-sum 141→343 allocs). The shipped `Cell` stores `{v, canonical}` inline,
   guarded by the owning `Env`'s `RWMutex`; reads take a short `RLock` but skip
   the scope-chain map walk (the dominant cost). This keeps the map-walk win
   with zero per-write allocation. The core-engine spec's "lock-free reads"
   requirement was amended to "reads under a short read lock" accordingly.
2. **Canonical frozen at head-resolution, not re-read at dispatch.** Reading the
   cell's canonical flag in `dispatchNativeOp` (after arguments ran) diverges
   from the tree-walker when an argument flips the operator canonical
   mid-evaluation. `OpGetGlobal` freezes the native-op decision per operand-stack
   slot (a minimal scratch, cleared on push and stack unwind); dispatch consumes
   it. `canonicalAt` is still gone; the frozen scratch is its correct, robust
   replacement.
3. **Depth-0-only, generation-guarded, root-env-scoped caching.** Only a name
   owned by the frame's env is cached (closing the starlark-revert shadowing
   hazard), guarded by env identity + a per-env new-name generation counter, and
   only when the frame env is the stable root — a per-call child env is fresh
   every invocation and would only churn cache entries.
4. **Lazy, single-atomic-pointer site table + capture analysis in the engine
   path.** The site table is built from `Code` on the first cache **hit** (proven
   reuse) and published via one `atomic.Pointer` (a two-field publish would allow
   a torn read `-race` cannot catch). A form that recompiles every eval
   (`defmacro`) never builds it. The engine's compile path now runs
   `markCaptures`, without which a top-level chunk stayed conservatively
   `FullEnv` and mirrored every local — the source of the loop-sum regression.
   The first cell in each `Env` is stored inline to avoid a per-scope heap
   allocation.

Outcome: VM/tree-walker parity and ADR 0003 race-freedom hold; fib bytecode
~10.6% faster (the locked read keeps the RWMutex cost the lock-free version
removed); ADR 0008 goldset non-regressing on 11/12 cells, with `twice-macro`
+0.21% B/op (the 8-byte site-cache pointer on the `Chunk`, within CI noise).
