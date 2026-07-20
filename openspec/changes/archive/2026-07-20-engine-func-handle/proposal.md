## Why

Embedders calling one script function millions of times pay per-call boundary work that GopherLua and goja front-load into a handle: both resolve the function once (`L.GetGlobal` / `goja.AssertFunction`) and then call through the resolved reference. `Engine.Call(ctx, name, ...)` instead re-resolves per call: root-env map lookup under `RWMutex`, a `sync.Map` stats lookup keyed by name, and two unconditional `time.Now` reads (deadline start + event duration). On the VM path that is 260 ns/op against GopherLua's 84 and goja's 190; the CPU profile of the boundary bench puts ~19% in `time.runtimeNow` and the rest of the fixed overhead in the lookups.

lispico has no resolve-once API, so embedders cannot amortize any of this — the boundary floor is structural, not incidental.

## What Changes

- New public handle API: `Engine.Func(name) (*Fn, error)` resolves the name once — root-env cell and per-name stats counter — and returns a handle; `(*Fn).Call(ctx, args ...core.Value)` invokes it. `Func` on an undefined name errors immediately.
- Handle semantics: safe for concurrent use; a rebind of the name is visible on the next `Fn.Call` (the handle caches the cell, not the value); deleting the name makes `Fn.Call` return the same "undefined function" error `Engine.Call` produces. Stats attribution via the pre-resolved counter — `Stats()` reports handle calls identically to named calls.
- Lazy deadline arming: the engine stops computing `time.Now().Add(timeout)` at call entry; the VM stores the timeout duration and arms the deadline instant at the first cancellation checkpoint (`vm-budget-only-polls` gives short calls a full first budget, so a call shorter than the budget never reads the clock). The earlier-caller-deadline suppression rule (ADR 0010) is evaluated at arm time with the same outcome. Applies to `Fn.Call`, `Engine.Call`, and `Eval`.
- Event timing only when observed: `PluginCallEvent` durations require wall-clock reads only while an `OnPluginCall` callback is registered (existing `callbacksActive` gate extended to the clock reads themselves). Registered-callback behavior is unchanged.
- Bench repo follow-up (separate repo): boundary benchmarks move to the handle, restoring like-for-like comparison with `CallByParam`/`AssertFunction`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `Function handles` (resolve-once semantics, rebind/delete visibility, concurrency, stats); `Boundary call efficiency` extended — no wall-clock reads on unobserved calls, name resolution once per handle rather than per call.

## Impact

- Code: `runtime/engine.go`/`runtime/eval.go` (`Fn` type, `Func`, `Call` de-clocking), `core/vm/vm.go` (timeout field + arm-at-first-poll), `runtime/stats.go` (counter handle), Engine interface addition (alpha API — additive).
- Expected: handle boundary call ~120–150 ns (beats goja's 190; GopherLua's 84 stays ahead — its path carries no ctx check, deadline, or stats), Callback proportionally; `Engine.Call` itself also drops the two clock reads.
- Sequencing: depends on `vm-budget-only-polls` (entry budget) ; complements `engine-bytecode-default`.
