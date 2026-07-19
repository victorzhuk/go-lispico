# Design — batched cancellation and timer-free deadlines

## Context

- VM `Run` calls `ctx.Err()` at the top of every dispatch iteration; with the Engine's default 30 s timeout every eval context is a `timerCtx`, so the check is a real virtual call into `cancelCtx.Err` — ~10% of fib cycles.
- `withEvalTimeout` allocates `context.WithTimeout` per `Eval`/`Call` (timer + timerCtx + cause), and the tree-walker's `select` forces lazy `Done` channel creation — ~4 allocs per boundary call before any user code runs.
- Cancellation sources today: embedder ctx (may be Background), engine default deadline, `Watch` reload does not cancel evals.

## Decision

### 1. Check points and budget

One helper, shared shape in both evaluators:

- countdown counter initialized to `checkInterval` (start at 128; tune with benchstat), decremented per instruction (VM) / per `Eval` node (tree-walker);
- forced check at every `OpLoop` back-jump and every closure/GoFunc call boundary in the VM, and at every `apply` trampoline iteration in the tree-walker — so tight loops and deep recursion observe cancellation independent of the countdown;
- a check does: `if deadline != 0 && nanotime() >= deadline → deadline error; if ctx.Err() != nil → ctx error`.

Latency bound: ≤ `checkInterval` instructions of straight-line code, ≤ 1 iteration of any loop or call. Both are spec scenarios.

Clock: `time.Now()` per check is acceptable at 1/128 frequency (two orders of magnitude cheaper than today's per-instruction `ctx.Err`); no `runtime.nanotime` linkname hacks.

### 2. Deadline as value, not context

`engineImpl` computes `deadline = start.Add(cfg.timeout)` when it owns the bound (per ADR 0010 rules: caller has no earlier deadline, `timeout > 0`) and threads it to the evaluators — VM field set on the pooled instance next to `SetGlobals`; tree-walker via `evalState`. No `context.WithTimeout`, no timer, no `Done` channel.

Caller-supplied contexts: still honored — `ctx.Err()` polled at the same check points. A caller deadline earlier than the Engine's makes the Engine skip its own bound exactly as today.

Error shape: the deadline error keeps the current `context.DeadlineExceeded` wrapping (`vm: context deadline exceeded` / `eval:` prefix) so embedder error handling does not change. Verify against existing deadline tests.

### 3. GoFunc context semantics

Today a GoFunc receives the engine-wrapped deadline ctx; after this change it receives the caller's ctx. Consequences:

- Pure stdlib GoFuncs: no observable difference.
- I/O plugins (`llm`, `net`, `exec`, `lio` — frozen/idle per ADR 0004): an engine-deadline interrupt mid-I/O no longer happens; the embedder's own ctx still bounds I/O. Documented in the ADR 0010 amendment; if an active I/O consumer appears, a lazy `WithDeadline` wrap on first non-canonical GoFunc dispatch can be added behind the same check-point plumbing without touching the spec.

Rejected alternative — keep `WithTimeout` but only when a plugin registry contains I/O plugins: couples the hot path to plugin metadata and still allocates for the common embedded-rules case.

### 4. Prior-art notes

- GopherLua: two compiled loop variants (`mainLoop` / `mainLoopWithContext`); the context variant does a `select` per instruction and the README calls out the degradation. We keep one loop — a countdown compare is cheap enough not to need loop duplication.
- goja: `atomic.LoadUint32(&vm.interrupted)` per instruction + profiler batched per 100. starlark-go likewise checks an `atomic.Pointer[string]` cancel reason per instruction alongside its step counter — an atomic load is ~1–2 ns vs ~30–80 ns for a channel-select `ctx` check, which is why none of these engines touch `context` in the loop.
- Atomic-flag conversion via `context.AfterFunc(ctx, ...)` (Go ≥1.21) was considered: it turns cancellation into one flag store and would allow per-instruction checks. Rejected for the same reason as a watcher goroutine — `AfterFunc` registers (and must stop) a callback on the context **per evaluation**, a per-call setup cost the 1.5 µs boundary path cannot afford. Batched `ctx.Err()` polling at 1/128 frequency costs less than the registration it replaces; if a future profile shows the residual poll mattering on long evals, `AfterFunc` can be added behind the same check points without spec changes.
- tengo: `atomic.LoadInt64(&v.aborting)` loop condition, `ctx.Done()` select outside the VM in `RunContext`.
- starlark-go: `ExecutionSteps` counter budget — same countdown structure as chosen here.

## Verification

- Existing deadline tests (ADR 0010 scenarios) must pass unchanged.
- New tests: straight-line cancellation within budget; `loop` cancellation within one iteration; recursive call cancellation within one call; deadline fires without caller ctx; `WithTimeout(0)` leaves caller ctx as the only source.
- Bench: fib bytecode delta; `Engine.Call` alloc count −4 expected (assert via `-benchmem` before/after in goldset cells).
