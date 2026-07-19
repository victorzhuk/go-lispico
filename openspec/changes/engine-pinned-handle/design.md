# Design — engine-pinned-handle

## Cost accounting

Shared `Fn.Call` after the program lands: ctx select + cell read + counter +
pool Get (~10) + `Reset` (~10) + `SetGlobals`/deadline plumbing + apply/run +
pool Put (~10). The pool round-trip and full reset are the only parts a
single-goroutine contract removes; everything else is contract-bearing
(cancellation, deadline, stats) or the actual work.

## API

```go
func (f *Fn) Pin() *PinnedFn
func (p *PinnedFn) Call(ctx context.Context, args ...core.Value) (core.Value, error)
```

`Pin()` on an `Fn`, not a constructor on Engine — the resolution work (cell,
counter) is already done; pinning only changes execution ownership. Multiple
`Pin()` calls give independent `PinnedFn`s (one per goroutine is the intended
idiom). The private VM is created at `Pin()` time, not lazily — a pinned
handle exists to be called.

## Incremental reset

A full `Reset` zeroes stacks, frames, handlers, scratch. Between calls on a
private VM, only what the previous call dirtied needs clearing, and the
run/return path already restores most of it (frames pop to empty, stack
truncates to base). The private VM asserts the post-call invariant (empty
frames, base-length stack) and clears only the deviation — on the happy path
that is a no-op check. An errored call falls back to a full `Reset` before the
next use; error paths are not the 84 ns race.

## Misuse detection

`atomic.CompareAndSwap` in-use flag around `Call`. Contended entry returns a
typed `LispicoError` (concurrent use of a pinned handle) — never a panic,
never silent corruption. Cost on the happy path: one uncontended CAS (~5 ns),
kept because the no-panics/race-clean invariants outrank the last nanometers;
`-race` tests hammer the misuse path deliberately.

The engine stays fully concurrent; a pinned VM never enters the shared pool,
so no shared state gains a single-goroutine assumption. `Engine.Close`
semantics: pinned handles outliving the engine follow whatever lifecycle
`Fn` has (same answer, decided in engine-func-handle's implementation).

## Rejected

- Per-engine pinning (`WithSingleGoroutine()`): changes the whole engine's
  contract for one call site's benefit; per-handle scope keeps the blast
  radius minimal.
- Lock-based mutual exclusion instead of CAS-error: a silently serializing
  handle hides the misuse instead of surfacing it, and the lock costs as much
  as the pool round-trip it replaced.
