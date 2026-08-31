---
status: accepted
---

# Reduction and allocation metering use a fixed deterministic ledger

Per-evaluation metering now complements ADR 0007's structural-depth, reader-depth, collection-length, and cache-entry ceilings. Every evaluation carries two more hard ceilings: reductions and cumulative allocation bytes. The ledger is deterministic by construction: the same source under the same engine configuration charges the same units regardless of Go version, allocator behavior, or host architecture.

## Reduction model

Reductions are evaluator-local work units, not wall-clock time and not cross-evaluator comparable counters.

- Tree-walker: one reduction per form dispatch, plus one per apply-trampoline `GoFunc` dispatch.
- Macro expansion: one reduction per expansion step.
- Bytecode VM: one reduction per decoded instruction, plus one per `GoFunc` dispatch.
- Compiler: one reduction per emitted instruction.
- Builtin Go function (`GoFunc`): logical work accrues locally via `core.NewBuiltinWorkBudget(ctx)`, with `Step()` recording one unit and synchronizing with the shared eval state (reductions, engine deadline, caller cancellation) every 128 units. Max unobserved work is 127 units; the first sync error is latched and replayed by every subsequent `Step`/`Flush` call.

The hot loops do not increment a shared atomic on every step. Both evaluators already keep a 128-step cancellation budget, so metering piggybacks that countdown and flushes consumed work at the existing sync points. This keeps the context-observation bound comfortably inside the required 1,024-reduction window while avoiding a new per-step branch or atomic write.

The Builtin work budget flushes on `Flush()` and at return from `GoFunc.Fn`, ensuring host-visible totals are exact at every observation point. The VM installs the run's resolved absolute deadline (`vm.deadline` via `reentrantCtx` armDeadline + `InstallReentrantDeadline`) before the first `GoFunc` dispatch — an earlier non-zero outer deadline wins; no fresh `now+timeout` is derived. This means a builtin observing its deadline at any point during execution sees the same pre-resolved instant the VM committed at dispatch, not a recomputed bound. On rearm of a retained re-entrant context across run boundaries, the new run's deadline is installed **before** the new generation is published (core/vm/vm.go `installReentrantDeadline` precedes the `RearmReentrantEvalState` generation store): a retained-context reader that materializes state in between must never govern the new run by the prior run's deadline. `TestCallReentrancy_RearmInstallsNewRunDeadlineBeforeGeneration` pins this ordering.

Reduction counts are compilation dependent, not just source dependent: because the VM charges per decoded instruction, a change to lispico's own bytecode compiler that alters how many instructions a form compiles to changes the reductions the same source charges, even though the ledger itself is unchanged. A `MaxReductions` boundary that a given source used to trip at some iteration count can move to a different count after such a change. This is expected — the determinism requirement below binds one lispico build's charges across hosts and Go versions; it does not bind one source's charges across lispico's own compiler revisions.

## Allocation model

Allocation charging is shallow and deterministic. It counts the produced value's own container cost, not a recursive deep walk of already-existing children. Values built incrementally in Lisp are therefore charged incrementally at each construction site; values materialized inside a Go builtin are charged once by their shallow result size.

### Fixed size table

| Unit | Charge |
| --- | ---: |
| Scalar value (`nil`, `bool`, `int`, `float`) | 16 bytes |
| String / symbol / keyword header | 16 bytes |
| String / symbol / keyword payload | `len(utf8 bytes)` |
| List header | 24 bytes |
| Vector header | 24 bytes |
| Collection element slot | 16 bytes per element |
| Hash map header | 32 bytes |
| Hash map entry | 64 bytes per key/value pair |
| Closure header | 64 bytes |
| Closure capture slot | 8 bytes per capture |
| Bytecode instruction | 4 bytes |
| Reader node | 32 bytes per parsed node |
| Reader byte payload | `len(source bytes copied into values)` |

### Why these values are conservative

- `16` bytes per value slot matches the project baseline that a boxed Lisp value occupies one interface slot on supported 64-bit targets; keeping it fixed avoids platform drift.
- `24` bytes for list/vector headers corresponds to one slice header; element storage is charged separately through slots.
- `64` bytes per hash-map pair intentionally over-counts both the small sorted form and the promoted Go-map form. The exact in-memory shape differs by path; the ledger must stay simple and fail closed.
- `64 + 8*caps` for closures over-counts small closures a little, but it captures the closure object plus capture-array growth without consulting runtime layout.
- `32` bytes per reader node intentionally prices parse-tree shape higher than the minimum object footprint. Reader metering must reject wide flat literals before evaluation starts, even though the exact token/value mix varies.

## Determinism requirement

The ledger MUST NOT depend on `unsafe.Sizeof`, allocator classes, pointer width, map bucket layout, or any other runtime-specific measurement. Those values vary across architectures and Go releases; a metering ceiling tied to them would make the same source pass on one host and fail on another. The published table is therefore normative even when the real heap footprint is smaller.

## Charge sites

The fixed table is applied only at evaluator-owned construction boundaries:

- reader output, charged immediately after `Read` and before the first form runs;
- tree-walker collection literals and quasiquote construction;
- VM `OpMakeList`, `OpMakeVector`, `OpMakeMap`, and `OpClosure`;
- compiler-emitted bytecode and constant pools, charged before a compiled chunk is cached;
- shallow `GoFunc` results at the centralized apply sites, unless the callee already charged the ledger for that same value.

This keeps the meter complete without trying to instrument every composite literal or every Go allocation in the process.

### The apply-site fallback charge and its opt-out

The apply site's shallow `GoFunc` result charge (`ValueShallowBytes(result)`,
in both `core/eval.go`'s tree-walker apply loop and `core/vm/vm.go`'s
`OpCall` `GoFunc` case) exists to catch results the fixed table has no other
charge site for. Left unconditional, it double-charges any builtin whose
result derives structurally from one of its own arguments — `cons`, `conj`,
`concat`, and friends on a shared `List`/`Vector` allocate O(1) new storage
but their *result* is still the whole accumulated structure, so a
shallow-size charge on every call turns an O(1) structural update into an
O(n) charge, and repeated calls into the same quadratic-charging defect this
change removed from accumulation.

`core.ChargeGoFuncResultBytes(ctx, n)` is the opt-out: a builtin that already
knows its own incremental cost calls it with that cost immediately before
returning the value it describes, and the apply site's fallback charge is
skipped for that call. Contract:

- Call exactly once per `GoFunc.Fn` invocation, immediately before returning
  the value `n` describes — the value returned must be the same value `n`
  was computed for.
- `BeginGoFuncDispatch`/`EndGoFuncDispatch` bracket each dispatch so the
  callee-charged marker is visible only to that call's own apply-site
  fallback check, not to an outer frame's — required because a `GoFunc` like
  `map` re-enters `apply`/VM `call` once per element on the same
  `evalState`, and a naive marker would mistake an inner element-lambda's
  charge for `map`'s own result already being billed.
- Both evaluators enforce the same rule off the same marker, so a builtin
  that opts out charges identically under the tree-walker and the VM.

A zero-byte charge (`n == 0`) marks a wholly borrowed result — an existing
argument, stored member, or caller-supplied default returned as-is. The
apply site skips the fallback shallow charge without adding any bytes. A
non-zero `n` charges exactly that many bytes; mixed results combining fresh
and borrowed components must pass only the fresh delta. The normative charge
site for `GoFunc` results is now the centralized apply site *unless* the
callee opted out via `ChargeGoFuncResultBytes`; builtins that return
structurally derived or wholly borrowed results are the primary opt-out
consumers.

### Trusted-host boundary

Host-provided `GoFunc` implementations (registered by plugins) are
trusted-host boundaries: the core-owned interruption guarantee — reduction
budgets, allocation ceilings, cooperative cancellation — applies at the
*call boundary* and at *charge sites*, but the code inside a `GoFunc.Fn`
body runs on the host's trust. The `BuiltinWorkBudget` lets well-behaved
builtins participate in cooperative metering, but core cannot enforce
metering correctness on untrusted Go code that ignores the budget API.
