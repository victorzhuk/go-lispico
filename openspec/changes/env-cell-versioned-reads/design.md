# Design — env-cell-versioned-reads

## Evidence

fib(20) bytecode CPU profile (bench repo, v0.8.0):

```
0.75s 21.61%  sync/atomic.(*Int32).Add   ← 100% from RWMutex.RLock/RUnlock (pprof -peek)
0.77s 22.19%  (cum) core.(*Env).ReadCell
```

`ReadCell` today:

```go
e.mu.RLock()
v, canon := c.v, c.canonical
e.mu.RUnlock()
```

Two atomic RMWs per read on `e.mu.readerCount` — a shared, write-hot cache
line even when uncontended, and a scalability cliff under `ParallelEval`.

## Core idea

A reader may serve a **previously captured snapshot** of the cell as long as it
can prove no write happened since capture. Proof = a per-cell monotonic version:

- Writer (already inside `env.mu.Lock()`): mutate `c.v`/`c.canonical`, then
  `c.version.Add(1)`.
- Publication (today's site-miss path, under the env's read lock): read
  `v, canonical` coherently, read `ver := c.version.Load()`, publish immutable
  `siteEntry{env, gen, cell, val, canonical, ver}` via the existing
  `atomic.Pointer` store.
- Hit: `entry.cell.version.Load() == entry.ver` → return `entry.val`. The
  snapshot was coherent at capture; version equality proves the cell is
  byte-identical now. `uint64` monotonicity rules out ABA.

The shared mutable words (`c.v`, `c.canonical`) are **only ever read under the
lock**, exactly as today — the fast path reads the immutable entry plus one
atomic. No unsynchronized access to multi-word data, so the Go memory model and
the race detector are satisfied without `unsafe`.

## Ordering argument

Go's atomics are sequentially consistent. Writer order: cell words (under mu) →
`version.Add` (release). Reader order: `version.Load` (acquire) → compare
against `entry.ver`. If the loads observe equality, no `Add` intervened between
capture and now, so the captured `val` is the current value. If a write is
mid-flight (cell words written, version not yet bumped — impossible to observe
a torn value since the reader never touches the cell words), the reader may
serve the pre-write snapshot: linearizes as "read before write", identical to
losing the `RLock` race today.

## Why no republication on mismatch

Republishing a fresh snapshot on every version mismatch would restore the fast
path for a rebound global at the cost of **one heap `siteEntry` per write per
site**. A `set!`-in-loop goldset cell writes a global every iteration; that is
precisely the per-write-allocation shape that failed the gate in round 1
(loop-sum 141→343 allocs). Decision: mismatch → locked `ReadCell`, never
allocate. A global written once after warm-up stays on today's cost — not
worse than the status quo. Recorded as a future lever with an explicit
trigger: a gate cell dominated by reads of a rarely-rebound global.

Refreshing the snapshot in place is not an option: `val` is a two-word
interface; atomic in-place update would need a per-write box (rejected above)
or seqlock-style unsynchronized reads (race-model violation, rejected).

## Bump-site audit (correctness-critical)

Every mutation of a cell that a site can cache must bump `version`:

- `Env.Set` — value write and tombstone-revival paths
- `Env.SetFunc` — function-cell writes (sites cache value cells today, but
  the audit covers both cell spaces so a future `OpGetFunc` site cache does
  not inherit a stale-read bug)
- `Env.Delete` — tombstone (`v = nil`) must bump, or a deleted global would
  keep serving the snapshot
- canonical-flag transitions (plugin registration, `Bind` clearing canonical)

`NameGen` continues to guard shape changes (new local bind shadowing an
ancestor); `version` guards value changes. Both checks stay on the hit path.

## Failure containment

Snapshot serving is bypassed whenever `entry.env != env` (shared chunk across
engines) or `gen` mismatches — those fall to the existing paths untouched.
Worst case of a version-check bug is bounded by tests: the rebind-visibility
crossval scenarios read a global immediately after `set!`/`def` in the same
program and across evaluations.

## Expected cost model

Hit path: ~2 atomic loads + 2 compares ≈ 3–4 ns vs ~15–17 ns today; fib saves
~44k × ~12 ns ≈ 0.5 ms/op (~15–20% at post-poll-fix baseline). `ParallelEval*`
benches should improve superlinearly with cores (no reader-side shared-line
RMW).
