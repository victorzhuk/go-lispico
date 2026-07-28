# compiler-branch-arith-fusion

## Why

The fib(20) body executes 20 VM instructions per recursive call (disassembly
at HEAD, 2026-07-27), and after removing the governance tax (batched ledger +
clock cadence spikes) the residual profile is pure interpretation: `vm.run`
dispatch 38.7% flat, `execNativeFastFused` 17.7% cum, push/pop/`reloadFrame`
proportional to instruction count. Of those 20 instructions per recursion:

- 4 are `FREEZE_NATIVE` — canonicality pushes for `<`, `+`, and two `-`,
  pure bookkeeping with no Lua/JS equivalent;
- 4 form the unfused compare-branch `GET_LOCAL, CONST, LT, JUMP_IF_FALSE`;
- 6 form two `FREEZE_NATIVE, GET_LOCAL, CONST, SUB` argument sequences.

Lua's compiler always pairs relational opcodes with the following jump — the
comparison handler performs the branch in the same dispatch. CPython 3.11+
gained "10-60%, typically ~25%" from specializing/fusing exactly these
families. Dispatch count is the multiplier on every per-instruction cost the
VM has, so fusing these shapes attacks the largest residual block.

Caveat, held honestly: Go offers no control over switch lowering, so classic
threaded-code superinstruction figures do not transfer 1:1 — each fusion must
prove itself by interleaved measurement, and the tasks are ordered so the
cheapest, highest-confidence fusion lands first and later ones are gated on
measured wins.

## What Changes

- **Compare-branch fusion**: the compiler emits a single fused
  compare-and-branch instruction for a two-operand native comparison whose
  result feeds `if`/`when`/`unless` — operand descriptors cover
  local×local, local×const, and stack×stack shapes. The fused op carries the
  native-op site index so canonicality freezing keeps its exact semantics
  (frozen before argument evaluation, falling back to the generic path when
  the operator binding is non-canonical).
- **Fused local/const argument arithmetic**: `FREEZE_NATIVE, GET_LOCAL,
  CONST, <arith>` collapses into one three-address-style fused instruction
  that reads a local slot and a constant and pushes the result, with the
  same site-canonicality guard and ledger charge as the existing fused
  native ops.
- Both fusions are compiler-local rewrites over the existing opcode stream;
  unfused opcodes remain valid and are still emitted for every shape the
  fusion does not cover, and whenever the operator site is non-canonical at
  freeze time the fused instruction falls back to the exact current generic
  behavior.
- Chunk validation gains cases for every new opcode (operand bounds, jump
  targets) — the validated-chunk/no-per-instruction-bounds-check invariant
  is load-bearing (two prior escapes; see 2026-07-18-vm-dispatch-loop-
  tightening lesson).

## Impact

- Affected specs: `bytecode-vm` (Native arithmetic and comparison opcodes —
  fused shapes; Bytecode VM robustness — validation of new opcodes).
- Affected code: `core/compiler/compiler.go` (emission), `core/vm/opcode.go`,
  `core/vm/vm.go` (dispatch cases), `core/vm/chunk.go` (Validate).
- Expected: fib hot path 20 → ~12-14 instructions per recursion; fib(20)
  −12-20% beyond the ledger/clock changes; Rule/Call small wins (`add`
  body shrinks too). Bytecode size grows slightly (register-VM literature:
  ~25% — acceptable, chunks are cached).
- Risk: dispatch-switch density changes could perturb Go's switch lowering —
  measured per fusion, with the untouched-row control; tree-walker parity
  unchanged (fusion is evaluator-internal); crossval pins semantics
  including the non-canonical fallback and division-by-zero edge cases.
