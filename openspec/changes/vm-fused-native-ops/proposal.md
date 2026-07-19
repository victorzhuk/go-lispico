## Why

Disassembly of the compiled fib body (22 instructions, 20 on the hot path) shows six `OpGetGlobal`s per recursive call — two for `fib` and **four for the operators `<`, `+`, `-`, `-` themselves**. Each operator read materializes the operator value onto the stack purely so the native opcode can freeze its canonical decision and later ignore the value on the fast path (`execNativeFast` drops it). Lua's equivalent body is ~10 register instructions with zero operator reads — arithmetic is not a binding there. Per canonical arithmetic op lispico pays: one full `OpGetGlobal` dispatch, a site check plus cell-value materialization, a stack push, the `nativeOp` freeze store, and a pop-and-discard at execution.

The head-position resolution itself cannot go away — rebind visibility and freeze-before-args parity with the tree-walker are spec'd — but the stack traffic and the value materialization on the canonical path can.

## What Changes

- New `OpFreezeNative` opcode: site-carrying, **no stack effect**. Emitted where the operator's `OpGetGlobal` sits today; resolves the head through the same cell/site machinery at the same point in evaluation order, records the frozen decision keyed by the current stack depth (the existing `nativeOp` mechanism, minus the push): canonical → opcode key only; non-canonical → the resolved operator value goes to a parallel fallback slot, not the stack.
- Fused native opcodes (`OpAdd` … `OpEq`) consume exactly `argc` argument slots. Canonical path: compute in place, push result — no operator slot to drop. Fallback path (frozen non-canonical): splice the saved operator value under the arguments (one `copy` of ≤argc values, rare path only) and enter `vm.call` exactly as today.
- Evaluation-order parity is exact: the head is resolved before arguments in both paths, the frozen decision cannot flip mid-arguments, and the fallback calls the head-time-resolved value — byte-identical semantics to today, verified by the existing rebind crossval scenarios.
- Measurement-gated: the win is instruction-count and stack-traffic (estimated fib −5–15% at post-program baseline); the change lands only if benchstat confirms a gain and the goldset gate holds.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: `Native arithmetic and comparison opcodes` gains an efficiency bound — resolving a canonical operator head SHALL NOT materialize the operator value onto the stack; all existing semantics scenarios unchanged.

## Impact

- Code: `core/vm/opcode.go` (+`OpFreezeNative`), `core/compiler/compiler.go` (emission), `core/vm/vm.go` (freeze bookkeeping, fused dispatch, fallback splice), `Chunk.Validate` case for the new opcode (validation-completeness lesson from vm-dispatch-loop-tightening applies).
- Expected: fib hot path 20→16 instructions; −4 dispatches −4 push/pop pairs per body; boundary `add` body 5→4 instructions.
- Sequencing: after `vm-budget-only-polls` and `env-cell-versioned-reads` (baseline for the measurement gate); independent of the rest.
