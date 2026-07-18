## Why

The host→script boundary is the worst article-benchmark ratio: `Engine.Call` of a two-argument script function costs 1520 ns / 13 allocs / 960 B, vs GopherLua's `CallByParam` at ~90 ns (and 0 allocs/op steady-state on a current gopher-lua checkout) — a 14–17x gap. `BenchmarkCallback_Lispico` (script→host round trip) is 1365 ns, nearly identical to plain `Call`, proving the cost is engine plumbing, not user code.

Where the 13 allocs go (alloc_objects profile): ~27% `context.WithDeadlineCause` + ~7.5% `time.newTimer` + ~7.5% `Done`-channel creation (removed by `eval-batched-cancellation`); ~18% `ensureEvalState` + ~8% `context.WithValue` (per-call eval-state context wrapping); ~23% `ChildVariadic`→`NewEnv`+`Set` (per-call env map on the tree-walker); ~17% `evalList` arg slices; plus two `time.Now` calls, a stats mutex, and a callback-slice RLock on every call. On the VM path, `vm.apply` additionally builds a throwaway wrapper `Chunk` (struct + code slice + constants slice) per call.

Prior art: GopherLua pushes fn+args onto a persistent register stack and reuses a preallocated call-frame array — zero per-call setup; goja's `AssertFunction` call writes args into the runtime's live stack and appends a by-value context struct to a reusable call stack; tengo reuses fixed frame slots; expr pools scopes and reuses a single args buffer sized by bytecode scan. None of them wraps a context, snapshots a clock twice, or allocates an environment per call.

## What Changes

- **VM apply without a wrapper chunk**: `vm.apply` seeds the (pooled) VM's stack with the closure and arguments and enters the existing call protocol directly, instead of synthesizing a `<apply>` chunk per call. `Apply`'s fresh-isolation contract and `ApplyPooled`'s pooled contract are both preserved.
- **Eval-state without per-call context values**: the `Call`/`Apply` entry no longer allocates `ensureEvalState` + `context.WithValue` per call; per-invocation state (ADR 0003) rides the pooled VM / an explicit state argument on internal entry points. Nested `GoFunc → Evaluator.Eval` re-entry keeps working.
- **Lazy observability**: when no `OnEval`/`OnPluginCall` callbacks are registered, `Call` skips event construction and duration timing; `Stats()` counters stay accurate via atomic counters. With callbacks registered, behavior is unchanged.
- Target: `Engine.Call` of a compiled two-arg function on a `WithBytecode()` engine ≤ ~500 ns and ≤ 4 allocs/op on the benchmark machine — from 14x down to ~5x of GopherLua, with the remaining gap owned by value boxing and name lookup, each measured and documented.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: modified requirement — pooled execution extends to the apply path running without per-call chunk synthesis; result isolation unchanged.
- `runtime-api`: new requirement — boundary-call efficiency contract (no per-call context/timer/eval-state/wrapper allocations on the steady-state `Call` path; stats stay correct; callbacks fire when registered).

## Impact

- Code: `core/vm/vm.go` (`apply`), `runtime/eval.go` (`Call`, `bytecodeEvaluator.Apply`), `runtime/stats.go` (atomic counters), `core/eval.go` (eval-state threading).
- Depends on: `eval-batched-cancellation` (removes the context/timer share of the allocs).
- Invariants: ADR 0003 concurrency (pooled state reset between uses, `-race` suites), VM/tree-walker parity on `Call` results, `Stats()`/callback API behavior.
- Gate: ADR 0008 goldset boundary-shaped cells must improve; others non-regressing.
