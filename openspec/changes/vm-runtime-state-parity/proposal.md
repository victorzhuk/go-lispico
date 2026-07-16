## Why

The remaining VM correctness-floor defects live at runtime, not compile time (PRD stories 8, 10–12): keyword calls — Clojure-style `(:key value)` map lookups — fail under VM application; a VM error path can leak structural-depth state and poison a later evaluation, including through pooled VM reuse; and shared-Engine concurrency across distinct Rule closures needs pinned race evidence. Macro-epoch chunk invalidation is already implemented and tested; this change pins the remaining unproven edge (redefinition interleaved with pooled `Apply`) rather than re-implementing it.

## What changes

- VM application supports Keyword values with the same arity, map-lookup, missing-key, and non-map behavior as the Evaluator.
- VM structural-depth accounting is restored on every return and error path, including pooled VM reuse, so one failed evaluation cannot affect a later call.
- Pooled sequential isolation and shared-Engine concurrency across distinct closures are pinned under the race detector, covering both `Eval` and `Apply`/`Call` paths.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: adds a Keyword application requirement and a state-hygiene requirement (structural-depth restoration across all exit paths and pooled reuse); the concurrency-safety requirement extends to distinct closures on the `Apply`/`Call` path.

## Impact

- ADRs: part of the VM correctness floor required by ADR 0008; consistent with ADR 0003 (per-evaluation state) and ADR 0007 (structural-depth ceiling fails closed).
- Invariants preserved: VM/tree-walker result agreement; no state leaks between evaluations; race-clean concurrent evaluation.
- Out of scope: compile-time defects tracked by `vm-compile-shape-and-scope`; any performance work — this is correctness only.
