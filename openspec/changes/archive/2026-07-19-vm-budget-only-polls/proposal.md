## Why

The VM's dispatch loop already carries a per-instruction countdown budget (128 instructions) for batched cancellation, but it additionally forces a `pollCancel` at every `OpCall`, `OpTailCall`, and `OpLoop` back-jump. Each poll reads the wall clock to compare the engine deadline. On fib(20) bytecode this is ~44k forced `time.Now` calls per evaluation: the CPU profile attributes **26% of total cycles to `time.runtimeNow`** under `pollCancel` (cum 27%). The forced polls are redundant — the budget decrements on every executed instruction, calls and back-jumps included, so the interval bound holds without them.

The run-entry poll (`budget = 0` at loop start) forces one clock read per `run()` invocation, which also taxes short boundary calls; the boundary APIs already check `ctx.Done()` on entry.

## What Changes

- Remove the unconditional `pollCancel` calls at `OpCall`, `OpTailCall`, and `OpLoop`; cancellation observation relies solely on the instruction budget.
- Start `run()` with a full budget (`checkInterval`) instead of 0: the first in-loop poll fires after the budget, not at instruction one. `Engine.Eval`/`Engine.Call` keep their entry `ctx` check, so an already-cancelled context is still rejected before any instruction runs.
- Observation-latency contract becomes: a cancellation or deadline expiry is observed within `checkInterval` executed VM instructions; a host `GoFunc` extends the wall-clock window by its own execution time (unchanged in practice — the VM never preempted host code, and the old forced poll also ran only after the `GoFunc` returned).
- ADR 0010 amendment note: checkpoint placement is budget-only.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: `Batched cancellation observation` requirement relaxed from "unconditionally at every loop back-jump and call boundary" to a uniform instruction-budget bound.

## Impact

- Code: `core/vm/vm.go` (`run` entry budget, three forced poll sites), latency tests, ADR 0010 note.
- Expected: fib(20) bytecode −20–25% (measured 26% of cycles in the polls' clock reads); every VM workload with calls or loops gains; short boundary calls drop one clock read.
- Tree-walker untouched: its checks are already budget-throttled and it is not the production path.
- Sequencing: independent; `engine-func-handle` builds on the full-entry-budget behavior for clock-free short calls.
