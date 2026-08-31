## Why

The resource ledger charges one Reduction per Builtin dispatch and shallow bytes
for every returned value, but some Builtins perform input-sized uninterrupted Go
work or return values they did not allocate. Treating those cases as ordinary
dispatch/results either leaves work outside Evaluation deadlines and Reduction
limits or charges borrowed storage as fresh allocation.

Blocked by: none.

## What Changes

- Add a `core.BuiltinWorkBudget` that accrues logical Builtin work in a local
  counter and synchronizes with evaluation state in bounded batches.
- Require the first Builtin budget in a VM callback to inherit the VM's already
  resolved absolute deadline, so time already spent in bytecode is not reset.
- Require batch synchronization and final flushing to observe caller
  cancellation, the Engine-owned Evaluation deadline, and Reduction exhaustion
  without a ledger update or clock read per logical unit.
- Define a zero-byte callee result charge for borrowed values and defaults so the
  centralized apply site does not charge existing storage again.
- Define how consumers classify opaque scalable helpers and trusted host-provided
  `Value` methods; migrate the whole active stdlib in a separate successor change.
- Amend the Reduction glossary and metering ADR so the new charge site is explicit
  instead of being introduced by one collection operation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: extend normative Reduction and allocation charge sites to cover
  scalable uninterrupted Builtin work and borrowed Builtin results.

## Impact

- Affects VM re-entry deadline propagation, evaluation-state polling, GoFunc
  result accounting, core resource-limit tests, ADR 0011, and the `CONTEXT.md`
  glossary.
- Reduction counts remain evaluator-specific; only terminal behavior is compared.
- Downstream Builtin migrations may reach `MaxReductions` earlier when previously
  unmetered work becomes visible and may reach `MaxAllocationBytes` later when
  false borrowed-result charges are removed.
