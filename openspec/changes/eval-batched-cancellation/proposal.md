## Why

Two independent costs come from checking cancellation too eagerly.

First, the VM calls `ctx.Err()` before **every instruction**: ~10% of all fib(20) bytecode cycles land in `context.(*cancelCtx).Err`/`timerCtx.Err`. The tree-walker does the equivalent `select` on `ctx.Done()` per `Eval` node and per `apply` iteration.

Second, because the Engine's 30-second default deadline (ADR 0010) is implemented as `context.WithTimeout` per `Eval`/`Call`, every boundary call pays the timer machinery: the alloc profile of `Engine.Call` attributes ~27% of allocated objects to `context.WithDeadlineCause`, ~7.5% to `time.newTimer`, and ~7.5% to `cancelCtx.Done` channel creation (forced by the tree-walker's `select`) — roughly 4 of the 13 allocs per 1.5 µs call.

Prior art is unanimous that neither cost is necessary: GopherLua's default main loop performs **zero** per-instruction cancellation checks and ships a separate `mainLoopWithContext` variant whose README warns "using a context causes performance degradation"; goja checks one `atomic.LoadUint32` interrupt flag per instruction and batches its profiler check per 100 instructions; tengo's loop condition is a plain `atomic.LoadInt64` abort flag with the `ctx.Done()` select moved outside the loop; starlark-go budgets execution steps with a counter. ADR 0008 explicitly deferred "batched cancellation checks plus a cross-engine step budget" until a measured consumer need — the article benchmark is that need.

## What Changes

- **VM**: replace per-instruction `ctx.Err()` with a batched check — a countdown counter (order of 128 instructions) plus an unconditional check at every `OpLoop` back-jump and every closure call, bounding cancellation latency for straight-line and looping code alike.
- **Tree-walker**: replace the per-node `select` with the same countdown budget carried in `evalState`.
- **Engine deadline without a timer**: when the Engine owns the deadline (caller context has none earlier), the deadline is enforced by comparing a precomputed deadline instant at the batched check points, instead of allocating `context.WithTimeout` + timer + Done channel per call. A caller-supplied context is still polled (`ctx.Err()`) at the same batch points, so embedder cancellation keeps working. The externally observable contract of ADR 0010 — default 30 s bound, earlier caller deadline governs, `WithTimeout(0)` disables — is unchanged.
- **Documented semantics change**: a `GoFunc` now receives the caller's context rather than an engine-wrapped deadline context, so a plugin blocking on external I/O is no longer interrupted mid-call by the Engine deadline (it is still bounded by the embedder's own context). With all I/O plugins frozen per ADR 0004 and YAGEL owning its deadlines per ADR 0010, no active consumer relies on mid-GoFunc engine cancellation. ADR 0010 gets an amendment note recording the enforcement mechanism.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: new requirement — batched cancellation with bounded observation latency (loop back-edges, call boundaries, N-instruction budget).
- `core-engine`: new requirement — the tree-walking evaluator observes cancellation on the same bounded budget.
- `runtime-api`: modified requirement — evaluation deadline ownership is enforced without per-call timer allocation; scenarios cover deadline firing, caller-context cancellation, and the GoFunc context semantics.

## Impact

- Code: `core/vm/vm.go` (Run loop), `core/eval.go` (Eval/apply checks, evalState budget), `runtime/eval.go` (`withEvalTimeout` replaced by deadline-instant plumbing), ADR 0010 amendment.
- Expected effect: ~10% fib bytecode cycles recovered; 4 allocs and the timer syscall path removed from every `Eval`/`Call` — prerequisite for `engine-call-fast-path`.
- Invariants: cancellation still observed within a bounded window (spec scenarios); `WithTimeout` semantics per ADR 0010 preserved at the API level.
- Sequencing: independent of `vm-resolved-global-bindings`; `engine-call-fast-path` depends on this change.
