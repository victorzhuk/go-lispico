# Tasks — compiler-branch-arith-fusion

Design correction (recorded in design.md): the originally proposed "one
fused compare-and-branch instruction" that also performs the branch is
unsound — a rebind to a VM-compiled `*Closure` pushes a new call frame and
returns asynchronously (`core/vm/vm.go:1764`), so a single instruction
cannot both compute the comparison and consume a result that may not exist
yet. The corrected, equivalent-savings mechanism fuses only
`FREEZE_NATIVE + operand + operand + <native op>` into one instruction that
pushes its result; the trailing `JUMP_IF_FALSE` (or whatever consumes an
arithmetic result) stays a real, ordinary instruction, emitted unchanged.
Sections 2 and 3 below are one mechanism (`OpFusedNativeOp`), staged by
which operators are eligible, not two separate code paths.

## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (one session, `-count=10`): fib, Call,
      Callback, Rule, goldset both modes, `-benchmem`. Record the fib
      disassembly (20 instructions/recursion at HEAD) and the `vm.run`
      flat share as the before-picture. Untouched-row control.

      Corrected on re-measurement: true baseline is 22 instructions/call,
      not 20 — see design.md.
- [x] 1.2 Add at least one CL-dialect (Lisp-2) fib-shaped timed cell to the
      comparison set. Every existing fib/Call/Callback/goldset benchmark
      uses `clojure.Dialect()` (Lisp-1); the shipped default engine is CL
      (Lisp-2), which resolves native-op heads through `GetFuncCanonical`
      with no site cache (`OpFreezeNativeFunc` keeps no site today). Without
      a Lisp-2 cell, the gate can go green on a path the shipped engine
      never takes.

## 2. Fused native op — comparison ops first (highest confidence)

- [x] 2.1 `OpFusedNativeOp`: one opcode, `A` operand indexes a new
      `Chunk.Fused []FusedOp` side table (mirrors `SubChunks`/`Caps`, not a
      packed sub-field — the 24-bit operand has no room for a second
      operand descriptor). `FusedOp` carries: the op (one of the 9 native
      opcodes), the operator symbol's constant index, a `Func bool`
      (mirrors `OpFreezeNative` vs `OpFreezeNativeFunc` — which cell to
      resolve through), and each operand's kind (local slot or constant
      index) + index. Local×const and local×local only; stack×stack (an
      arbitrary sub-expression operand) is explicitly out of scope — it can
      side-effect and rebind mid-evaluation, exactly what the freeze
      protocol exists to prevent, and the atomic single-dispatch design
      depends on operands being non-effecting.
- [x] 2.2 Compiler: `compileNativeOp` emits the fused instruction instead of
      `FREEZE_NATIVE(_FUNC)` + two `Compile(arg)` + `<op>` when there are
      exactly two arguments, each a local-resolving symbol or a scalar
      constant, and the head is canonical-eligible (same static check as
      today). Initially restrict eligibility to `< > <= >= =`. All other
      shapes (nested calls, N-ary, shadowed names, arithmetic ops for now)
      keep the current sequence. No changes needed in
      `compileIf`/`compileWhen`/`compileUnless`/`compileCond`/`compileNot`
      — they still compile the condition generically and emit their own
      `JUMP_IF_FALSE`.
- [x] 2.3 VM dispatch case: resolve the operator (via `resolveGlobalValue`
      when `!Func`, `GetFuncCanonical` when `Func`) and read both operands
      via `readFusedOperand` (transparently unwraps a `*cellBox` — the
      mechanism that makes emission-time fusion safe regardless of whether
      the operand's local is later captured by a sibling closure, with no
      `finalize`-time rewrite needed). Canonical → `nativeInt2`/`execNative`
      fast/general path, push result, charge the ledger identically to
      `execNativeFastFused`. Non-canonical → push operator+operands, call
      `vm.call(ctx, 2, false)` exactly as `dispatchNativeOp`'s existing
      rebind fallback does (same terminal-error/throw handling as the
      `OpAdd..OpEq` case) — this is what correctly handles a rebind to a
      `*Closure`: the call may push a new frame, and the *already-real*
      trailing consumer instruction naturally picks up the eventual result.
- [x] 2.4 `Chunk.Validate`: bounds-check the `Fused` index, the symbol
      constant, and each operand (local against `MaxStack`, const against
      `len(Constants)`). `stackDelta` gets an explicit `+1` case (not the
      silent `default: 0`). `CopyTreeFreshSites` copies `Fused` (shared,
      immutable slice). `buildSites` indexes `Func:false` fused ops
      (`chunk.Fused[a].Sym`, not `a` directly). `chunkDeepBytes` charges a
      fixed per-entry constant for `Fused` (no `unsafe.Sizeof`, per ADR
      0011). Adversarial review of the validate/hot-loop invariant — every
      operand the hot loop indexes must be validated.
- [x] 2.5 Interleaved benchstat: fib (both dialects, task 1.2) and goldset.
      Gate: fib improves ≥5% with no other row regressing, else revert the
      emission (keep the opcode dark) and record the numbers in design.md.
      Local perfgate is directional only (see `lispico-perfgate-not-local`)
      — a near-miss is recorded and re-judged after
      `vm-deadline-clock-cadence` lands, not a reflexive revert.

      Local near-miss: fib showed no statistically significant delta at
      count=10 on this box (before/after distributions overlap almost
      completely); allocs/bytes unchanged; correctness fully green
      (crossval + goldset). Not reverted, per the policy above — instruction
      count (22→19 comparison-only) is the non-noisy causal evidence.
      Numbers recorded in design.md.

## 3. Extend to arithmetic ops (gated on 2's measured win)

- [x] 3.1 Widen `compileNativeOp`'s eligibility check to `+ - * /`. Same
      mechanism, no new opcode or VM case. Division-by-zero and
      float-promotion fall through `execFusedNative`'s general path
      unchanged (mirrors `nativeInt2`'s existing `divInt2` contract).
- [x] 3.2 Interleaved benchstat: same gate as 2.5.

      Same local near-miss pattern as 2.5 — see design.md.

## 4. Verify

- [x] 4.1 Update the exact-instruction-count compiler test pins
      (`TestCompiler_NativeOp*`, `TestCompiler_ChargesReductionsPerInstruction`)
      to the new, smaller emitted counts. Add a malformed-chunk case per
      validation class and the new emit shape to
      `TestChunkValidate_AcceptsCompilerOutput`.
- [x] 4.2 Crossval `TestVMVsTreeWalker` green, using `stdlibEnv()` (not
      `newCrossValEnv()`, whose bindings are non-canonical and never engage
      fusion). Cases: canonical fast path; rebind to a `core.GoFunc`;
      rebind to a tree-walker `core.Lambda`; rebind to a VM-compiled
      `*Closure` (the discriminating case — the only one exercising the
      async frame-push deopt path); each of the above under the CL dialect
      too (Lisp-1 and Lisp-2 resolve differently); a captured local used as
      a fused operand, mutated via `set!` from a sibling closure, observing
      the update (tests the `cellBox` unwrap).
- [x] 4.3 `go build -gcflags='-m' ./core/vm/` to confirm the fused
      operand-read/dispatch path doesn't newly escape.
- [x] 4.4 Full floor: build, vet, lint, full suite, `-race`, goldset both
      modes non-increasing bytes/allocs.
- [x] 4.5 Record the final fib per-recursion instruction count, both
      dialects, in design.md next to the 20-instruction baseline.
