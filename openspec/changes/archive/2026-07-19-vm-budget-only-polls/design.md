# Design — vm-budget-only-polls

## Evidence

CPU profile of `BenchmarkFib_Lispico_Bytecode` (bench repo, v0.8.0, 3s run):

```
0.91s 26.22%  time.runtimeNow
0.95s 27.38%  (cum) vm.(*VM).pollCancel
```

`pollCancel` executes `time.Now().Before(vm.deadline)` whenever a deadline is
set (the engine's 30s default makes that always). Call sites today:

1. loop head, when `vm.budget` hits 0 — every 128 instructions (correct);
2. after every `OpCall` (vm.go:477) — forced, redundant;
3. after every `OpTailCall` (vm.go:489) — forced, redundant;
4. at every `OpLoop` back-jump (vm.go:577) — forced, redundant;
5. `run()` entry via `vm.budget = 0` — forces a poll at instruction one.

fib(20) makes ~22k closure calls → ~44k forced polls beyond the ~4k budgeted
ones. Removing 2–4 keeps the interval bound because the budget decrements on
every executed instruction, including `OpCall`/`OpTailCall`/`OpLoop`
themselves.

Local deadline-armed `fib(15)` VM validation (six samples per revision) measured
`377.2µs` → `300.2µs` per operation (`-20.41%`, `p=0.002`); allocations were
unchanged. CPU profiles moved `time.runtimeNow` from the top cost (`26.44%`) to
`3.77%`, below the top six entries. The temporary harness set the same
30-second engine-style deadline before each VM run and was removed after capture.

## Decisions

**Budget-only observation.** The latency contract weakens from "no later than
the next call boundary / back-jump" to "within `checkInterval` executed
instructions". At ~ns/instruction that is a sub-microsecond window — far below
any embedder-visible granularity. The spec scenarios are reworded to the
uniform budget bound.

**Full entry budget.** `run()` starts with `vm.budget = checkInterval` instead
of 0. Rationale: `Engine.Eval`/`Engine.Call`/`applyBoundary` already reject an
already-cancelled ctx before entering the VM, so the instruction-one poll only
duplicated that check and forced a clock read onto every short call. This is
what lets a later boundary fast path run short calls entirely clock-free
(deadline arming moves into the first poll — `engine-func-handle`).

**Shared reentrant budget.** A bytecode `GoFunc` can re-enter the evaluator
through many short VM runs. Those runs must consume the context's shared
evaluation budget before dispatch, matching the tree-walker; otherwise each
fresh VM run resets its local budget and an engine deadline can remain
unobserved indefinitely. This boundary poll is budget-throttled, not a
per-run clock read, so standalone short VM runs retain their full initial
budget.

**GoFunc window.** A host `GoFunc` runs to completion regardless of polls; the
old forced poll fired after it returned. Now observation happens at most 127
instructions later. Both before and after, the wall-clock observation window
is `GoFunc duration + O(budget)` — the contract text now says so explicitly.

**Deadline precision.** The engine deadline is compared at checkpoints only;
expiry is observed at the first checkpoint after the instant passes. Unchanged
semantics, same error shape (`vm: context deadline exceeded`).

## Rejected

- Keeping a forced poll only after `GoFunc` returns: the budget check lands
  within 127 instructions anyway; not worth a branch in `vm.call`.
- Making `checkInterval` configurable: no consumer need; ADR 0007/0010 keep
  limits engine-owned.

## Validation invariant (from vm-dispatch-loop-tightening lesson)

No opcode gains or loses a jump/slot operand here, so `Chunk.Validate()` needs
no new cases. The only control-flow-adjacent edit is deleting poll calls; the
`reloadFrame` synchronization points are untouched.
