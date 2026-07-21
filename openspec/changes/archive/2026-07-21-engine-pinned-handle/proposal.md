## Why

Both boundary leaders run under a single-goroutine contract: GopherLua's `LState` and goja's `Runtime` are documented not-goroutine-safe, which is precisely what lets their call paths carry zero synchronization — no pool round-trip, no reset of shared machinery, no locks. Lispico's `Fn` handle (engine-func-handle) stays concurrency-safe and therefore pays the `sync.Pool` Get/Put plus a VM `Reset` on every call — ~20–40 ns of the remaining gap to GopherLua's 84 ns floor after the rest of the program lands.

Round-4 research (verified against both codebases) shows this is a *contract* difference, not an engineering one. Offering the same contract as an explicit opt-in closes the floor gap for embedders with a hot single-goroutine call site — the exact shape both competitors' benchmarks (and the article) measure.

## What Changes

- `(*Fn).Pin() *PinnedFn`: returns a handle variant that owns a private, pre-reset VM instead of borrowing from the pool. `PinnedFn.Call(ctx, args...)` is the same boundary minus pool Get/Put and per-call Reset (the private VM resets incrementally — only the state the previous call dirtied).
- Contract, documented loudly: a `PinnedFn` is NOT safe for concurrent use — one per goroutine, exactly the `LState`/`goja.Runtime` model. Concurrent misuse is detected best-effort (an atomic in-use flag flips a typed error, mirroring the no-panics invariant) rather than silently corrupting.
- Semantics otherwise identical to `Fn.Call`: current-binding resolution through the cell, undefined-after-delete error, stats attribution, callback events, lazy deadline, re-entrancy budget sharing. The engine and all other handles remain fully concurrent — pinning is per-handle, not per-engine.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `Pinned function handles` — single-goroutine contract, misuse detection, semantic equivalence to shared handles.

## Impact

- Code: `runtime` (`PinnedFn`, incremental reset), small; builds entirely on engine-func-handle's shared boundary helper.
- Expected: pinned call ~100–120 ns (vs ~130–150 shared) — GopherLua-class floor while keeping ctx check, deadline, and stats; Callback proportionally.
- Sequencing: after `engine-func-handle`; bench repo adds a pinned variant row alongside the shared-handle row (like-for-like with `CallByParam`).
