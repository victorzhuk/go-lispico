# Design — boundary call fast path

## Context

Measured `Engine.Call` cost structure (default tree-walker engine, bench machine): 1520 ns, 13 allocs. `Callback` bench ≈ `Call` bench → plumbing-bound. Alloc shares: context/timer ~42% (handled by `eval-batched-cancellation`), eval-state ctx ~26%, child env ~23%, arg slices ~17% (shares overlap across cum edges; total 13). VM-path `Apply` additionally builds a wrapper `Chunk` per call today.

GopherLua reference on the same machine: ~90 ns, and 0 allocs/op steady state (current upstream checkout) — persistent register stack, preallocated `fixedCallFrameStack`, no locks anywhere in the package.

## Decisions

### 1. Direct apply protocol in the VM

`vm.apply` for `*Closure` becomes: reset check → push closure → push args → run an internal entry that executes `vm.call(argc)` and loops the dispatch until the seeded frame returns. No `<apply>` chunk, no per-call `Constants`/`Code` slices. `GoFunc` and `Keyword` branches stay direct calls (already allocation-light).

The existing `Run` loop terminates when `len(vm.frames) == 0`; seeding via the call protocol reuses that invariant — the seeded call's `OpReturn` pops the last frame and yields the result from the stack. Guard: arity errors surface before any frame is pushed (unchanged).

### 2. Eval-state off the per-call context

`ensureEvalState`/`EnsureEvalState` currently allocate a state struct + `context.WithValue` per boundary call. Replace on the `Call` path: the pooled VM owns a per-use state (reset with the VM); the tree-walker's `Apply` entry accepts state explicitly and only falls back to context-carried state for re-entrant `GoFunc → Evaluator.Eval` calls (which today already carry the value). ADR 0003's requirement — per-invocation counters, never shared engine fields — is met because pool discipline guarantees single-use ownership during a call.

Re-entrancy: a `GoFunc` that calls back into the evaluator must observe the same depth counters. The state handle is placed in the ctx **only when a non-canonical GoFunc is actually dispatched** (lazy wrap), keeping the common arithmetic path clean. If measurement shows the lazy wrap costs more than it saves, fall back to one `WithValue` per call and keep the other wins — decide with `AllocsPerRun`.

### 3. Lazy observability

`Call` today: two `time.Now`, `stats.recordPluginCall` (mutex + map write), `firePluginCallbacks` (RLock + event struct) — unconditionally. Change:

- callbacks empty (the common embedded case): skip timing and event construction entirely; count calls via `atomic.Int64`.
- callbacks registered: current behavior, including durations.
- `Stats()` keeps returning call counts either way; per-function duration aggregation is documented as available when callbacks/stats consumers require it (flag flipped on first `OnPluginCall`/`OnEval` registration).

`env.Get(name)` per `Call` stays: one map read on a stable root env is ~20 ns and correct under rebinds; a cached-handle API (`Engine.Func(name)`) is deliberately out of scope until a consumer needs it (YAGNI, and the article benchmark measures the named-call shape).

### 4. What is *not* changed

- Tree-walker `ChildVariadic` env-per-call stays: ADR 0006 designates the VM as the performance path and explicitly skips deep tree-walker env work. The boundary story lands on `WithBytecode()`; the article's boundary bench should measure the VM engine (fib already does).
- Args are copied into the VM stack (tengo's PR #259→#260 lesson: passing live interpreter storage into host code corrupts state; the copy is the safety boundary). For GoFunc dispatch inside the VM the existing zero-copy stack-view convention stays — the same shape wazero (`GoFunction.Call(ctx, stack []uint64)` on a live sub-slice) and gojq (fixed `args [32]any` array, sliced per call, zero allocation at any arity) ship in production.

## Verification

- `AllocsPerRun` assertions: `Call` on `WithBytecode()` engine ≤ 4 allocs steady state; no wrapper-chunk allocation in `apply` (bench in `core/vm`).
- Parity: `Call` results identical tree-walker vs VM (existing crossval + `runtime` suites).
- `-race`: concurrent `Call` on one engine (existing scenario) plus concurrent `Call` + `OnPluginCall` registration.
- Goldset boundary cells + article bench `BenchmarkCall_Lispico`/`BenchmarkCallback_Lispico` before/after.
