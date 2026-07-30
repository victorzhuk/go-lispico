# metering-semantics-disclosure

## Why

Two v0.10.0 metering changes shipped real code with undocumented or
understated consequences.

`compiler-branch-arith-fusion` (527f03c) collapses fib's recursive body from
22 instructions to 13 (−41%). Reductions are charged per executed instruction
(`core/vm/vm.go:786`, `if vm.budget--; vm.budget <= 0`, settled by
`flushConsumedReductions` at `:376`), and the requirement that codifies this
(`core-engine` spec, "Per-evaluation reduction and allocation counters")
already says counter values need not agree *between evaluators*. It says
nothing about the same evaluator charging fewer reductions for identical
source after a compiler change. A program that previously tripped
`MaxReductions` at some iteration count may no longer trip it post-fusion —
a real change in observable resource-limit behavior, absent from the fusion
proposal's Impact section, the shipped spec delta, and the CHANGELOG.

`vm-batched-ledger-charging` (631b2ee) replaced per-opcode atomic allocation
charges with a VM-local accumulator settled at bounded checkpoints. The
shipped requirement text ("Fused native-op results charge the allocation
ledger", `bytecode-vm` spec) bounds the resulting enforcement slack at "one
checkpoint interval of fixed-size scalar charges" — `checkInterval = 128` ×
`MeterScalarBytes = 16` = 2048 bytes. But the same accumulator
(`vm.pendingAllocBytes`, `core/vm/vm.go:385`) also absorbs
`ListShallowBytes` (`:1071`), `VectorShallowBytes` (`:1085`),
`HashMapShallowBytes` (`:1105`), `ClosureShallowBytes` (`:1126`), and
`chunk.ConstCharges[idx]` (`:811`) — none of them fixed-size scalars. A
128-instruction window that constructs collections can overshoot
`MaxAllocationBytes` by far more than the documented 2048-byte bound.

A related, smaller gap: fusion's compile-time accounting is asymmetric with
its runtime effect. `MeterFusedOpBytes = 40` charges one `FusedOp` descriptor
per fused site, while removing 3 net instructions saves `3 ×
MeterInstructionBytes(4) = 12` bytes — a net +28 bytes of accounted
`chunk.DeepBytes` per fused site despite fewer real instructions executing.
`DeepBytes` feeds `MaxCacheBytes` admission (`runtime/eval.go:133`), so this
is not cosmetic: post-0.10.0, a chunk near the cache-byte ceiling is more
likely to be rejected than the same source compiled pre-fusion, and nothing
documents that trade-off.

## What Changes

This is a spec-and-docs-only change. No runtime behavior changes.

- `core-engine` spec: state explicitly that reduction counts are
  evaluator-*and*-compilation-dependent — a superinstruction or any future
  fusion reduces the reductions charged for identical source under the same
  evaluator, not only across evaluators.
- `bytecode-vm` spec: correct the enforcement-slack bound on the existing
  "Fused native-op results charge the allocation ledger" requirement to cover
  every charge class `pendingAlloc` absorbs (scalar, list/vector/map/closure
  shallow, constant charges), not only fixed-size scalars.
- `bytecode-vm` spec: add the FusedOp accounted-size disclosure — fusion
  grows a chunk's accounted `DeepBytes` even where it shrinks real
  instruction count — and its interaction with `MaxCacheBytes` admission.
- CHANGELOG `[Unreleased]`: an entry disclosing the reduction-count and
  ledger-slack semantics for v0.10.0's two metering changes, since the
  original release notes carried none.

## Impact

- Affected specs: `core-engine` ("Per-evaluation reduction and allocation
  counters"), `bytecode-vm` ("Fused native-op results charge the allocation
  ledger").
- Affected docs: CHANGELOG.md, `docs/adr/0011-reduction-and-allocation-metering.md`
  (a cross-reference note, if the ADR's own text needs one — judged during
  implementation).
- Code: none. This is documentation of existing, shipped behavior.
- Risk: none functionally; the risk this change addresses is an embedder
  relying on an incorrect enforcement-slack bound or an unaware regression in
  resource-limit sensitivity across a lispico upgrade.
