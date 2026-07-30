# Tasks — metering-semantics-disclosure

## 1. core-engine spec

- [ ] 1.1 Amend "Per-evaluation reduction and allocation counters": add that
      reduction counts are evaluator-*and*-compilation-dependent — a
      compiler change that alters instruction count for the same source
      under the same evaluator (e.g. a superinstruction) changes the
      reductions charged, and a resource-limit boundary previously crossed
      may no longer be crossed. Add a scenario pinning this.

## 2. bytecode-vm spec

- [ ] 2.1 Amend "Fused native-op results charge the allocation ledger":
      replace "one checkpoint interval of fixed-size scalar charges" with
      language covering every charge class `pendingAllocBytes` accumulates
      (scalar, list/vector/map/closure shallow bytes, constant charges) —
      the bound is one checkpoint interval of whatever charges were issued
      in it, not bounded to scalar size.
- [ ] 2.2 Add a scenario/paragraph disclosing that a fused site's accounted
      `chunk.DeepBytes` grows by `MeterFusedOpBytes(40) −
      instructionsRemoved × MeterInstructionBytes(4)` even when real
      instruction count shrinks, and that this feeds `MaxCacheBytes`
      admission.

## 3. Docs

- [ ] 3.1 CHANGELOG `[Unreleased]`: disclose both semantics for the readers
      of v0.10.0's release notes, which carried neither.
- [ ] 3.2 Cross-reference note in `docs/adr/0011-reduction-and-allocation-metering.md`
      if, on reading it during implementation, its text implies a stronger
      slack bound than what 2.1 documents.

## 4. Verify

- [ ] 4.1 `openspec validate --strict` on this change.
- [ ] 4.2 No code changes in this change — confirm `git diff` touches only
      `openspec/`, `CHANGELOG.md`, and `docs/adr/0011-*.md`.
