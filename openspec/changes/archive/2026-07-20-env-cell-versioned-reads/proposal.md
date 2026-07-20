## Why

A site-cache **hit** still pays the owning env's `RWMutex` on every global read: `resolveGlobalValue` finds the cached cell, then `ReadCell` takes `RLock`/`RUnlock` — two atomic read-modify-writes on a shared cache line. The fib(20) bytecode CPU profile attributes **21.6% of total cycles to `sync/atomic.(*Int32).Add`, 100% of it from `RWMutex.RLock`/`RUnlock`** under `Env.ReadCell` (cum 22%). fib reads the `fib` global twice per call, ~44k locked reads per evaluation — for a binding that never changes after `defn`.

The prior lock-free attempt (vm-resolved-global-bindings, round 1) failed the goldset alloc gate because it boxed a heap value per **write**. This design allocates nothing on either path: reads validate a version counter; writes bump an inline atomic.

## What Changes

- `core.Cell` gains a `version atomic.Uint64`, bumped (inside the existing `env.mu` critical section) by every mutation of the cell's value or canonical flag — `Set`, `SetFunc`, `Delete`/tombstone, canonical marking.
- The published `siteEntry` (already immutable, atomically swapped) gains a snapshot: `{val, canonical, ver}` captured under the lock at publication time.
- Site-hit read path: `entry.env == env && entry.gen == env.NameGen() && entry.cell.version.Load() == entry.ver` → return the snapshot value. Two atomic loads, zero locks, zero RMW, zero allocation.
- Version mismatch (the global was written after publication) → fall back to today's locked `ReadCell`. **No republication on mismatch**: a mutated global permanently degrades to today's cost instead of allocating a fresh entry per write — this is what keeps `set!`-heavy goldset cells inside the alloc gate.
- Rebind visibility, shadowing (`gen`) semantics, and the concurrent old-or-new guarantee are unchanged; the snapshot is only served while the version proves no write happened since it was taken.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: `Resolved global bindings` gains an efficiency bound — a cached-site read of a global that has not been written since resolution takes no lock and allocates nothing — while keeping every existing visibility and race-safety scenario.

## Impact

- Code: `core/env.go` (`Cell` version field, bump sites in `Set`/`SetFunc`/`Delete`/canonical marking), `core/vm/chunk.go` (`siteEntry` snapshot), `core/vm/vm.go` (`resolveGlobalValue` hit path).
- Expected: fib(20) bytecode −15–20% on top of vm-budget-only-polls (measured 22% of cycles in the lock traffic); every chunk-reusing workload with global reads gains; contended parallel eval gains more (no reader RMW on a shared line).
- Race model: snapshot lives in an immutable published entry; the version is atomic; `go test -race` clean by construction. Goldset alloc gate: no allocation added to any read or write path.
- Sequencing: independent of vm-budget-only-polls; both land before parity re-measurement.
