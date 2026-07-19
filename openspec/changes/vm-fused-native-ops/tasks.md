## 1. Opcode and compiler

- [ ] 1.1 Add `OpFreezeNative` (site-carrying constant operand, no stack effect); `Chunk.Validate` case (constant index in range; every opcode with a jump/slot/const operand gets a case — dispatch-loop-tightening lesson).
- [ ] 1.2 Compiler: replace the operator `OpGetGlobal`/`OpGetFunc` emission in `compileNativeOp` with `OpFreezeNative` at the same code position; fused ops keep their argc operand; `MaxStack` keeps charging the operator slot.

## 2. VM

- [ ] 2.1 `FREEZE_NATIVE` handler: resolve head through the dialect-correct cell via the site path; store `nativeOp[depth]` (canonical) or `opFallback[depth]` (head-time value, non-canonical); keep the existing stale-marker zeroing discipline.
- [ ] 2.2 Fused dispatch: canonical → in-place native over `argc` slots; fallback → splice `opFallback` under the args and enter `vm.call`; neither → resolve via the op's constant operand (hand-built-chunk recovery).
- [ ] 2.3 Delete the now-unreachable operator-slot handling (`fnIdx` drop) from `execNativeFast`.

## 3. Parity tests

- [ ] 3.1 Existing rebind scenarios green: rebound `+` falls back (both dialects), Lisp-2 function-cell rebind, mid-argument rebind does not flip the frozen decision.
- [ ] 3.2 Nested operators (`(+ (* a b) c)`, deeper) — freeze keying by depth composes; crossval parity suite green.
- [ ] 3.3 Keyword/GoFunc/closure operands under fused ops unchanged (promotion parity, division-by-zero error shape).

## 4. Measure and verify

- [ ] 4.1 `go test ./...`, `-race`, crossval green; `GOLDSET_MODE=vm` gate non-increasing.
- [ ] 4.2 Benchstat ≥6 at post-poll/post-versioned-reads baseline: fib bytecode, arithmetic-loop goldset cells, boundary `add`. Adopt on a confirmed win; otherwise archive as measured-and-rejected with numbers.
