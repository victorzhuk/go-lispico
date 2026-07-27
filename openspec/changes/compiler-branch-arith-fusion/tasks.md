# Tasks — compiler-branch-arith-fusion

## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (one session, `-count=10`): fib, Call,
      Callback, Rule, goldset both modes, `-benchmem`. Record the fib
      disassembly (20 instructions/recursion at HEAD) and the `vm.run`
      flat share as the before-picture. Untouched-row control.

## 2. Compare-branch fusion (land first — highest confidence)

- [ ] 2.1 Opcode(s) for fused compare-and-branch with operand descriptors
      (local×local, local×const, stack×stack) carrying: comparison op,
      operand refs, native-op site index, jump target. Packing must fit the
      existing `Instruction` encoding or extend it uniformly.
- [ ] 2.2 Compiler: pattern-match a native comparison feeding a conditional
      jump; emit fused form only when the head resolves to a canonical
      native comparison site (same criteria as `FREEZE_NATIVE` emission
      today). All other shapes keep the current sequence.
- [ ] 2.3 VM dispatch case: freeze site canonicality exactly where the old
      `FREEZE_NATIVE` did (before operand reads); non-canonical → fall back
      to generic operator application with identical semantics; Int/Int
      fast path first, general comparison fallback after.
- [ ] 2.4 `Chunk.Validate`: operand bounds + jump-target cases for every new
      opcode, including the fall-off-end rule (fused branch is not a
      terminator). Adversarial review of the validate/hot-loop invariant —
      every operand the hot loop indexes must be validated.
- [ ] 2.5 Interleaved benchstat: fib and goldset. Gate: fib improves ≥5%
      with no other row regressing, else revert the emission (keep opcodes
      dark) and record the numbers in design.md.

## 3. Fused local/const argument arithmetic (gated on 2's measured win)

- [ ] 3.1 Opcode(s) for `<arith> local, const → push` and
      `<arith> local, local → push`, carrying the native-op site index;
      ledger charge identical to the existing fused native-op charge.
- [ ] 3.2 Compiler pattern for `FREEZE_NATIVE, GET_LOCAL, CONST, <arith>`
      argument sequences (and the local×local variant); emission gated on
      canonical site as in 2.2.
- [ ] 3.3 Dispatch + Validate cases as in 2.3/2.4; division-by-zero and
      float-promotion edges fall back to the generic path unchanged.
- [ ] 3.4 Interleaved benchstat: same gate as 2.5.

## 4. Verify

- [ ] 4.1 Crossval `TestVMVsTreeWalker` green — including rebind tests: a
      user rebinding `<`/`-`/`+` mid-program must see identical results
      under fused and unfused paths (canonicality freeze parity).
- [ ] 4.2 Full floor: build, vet, lint, full suite, `-race`, goldset both
      modes non-increasing bytes/allocs.
- [ ] 4.3 Record the final fib per-recursion instruction count in
      design.md next to the 20-instruction baseline.
