# vm-batched-ledger-charging

## Why

Every fused native arithmetic/comparison op charges the allocation ledger at
its dispatch site (`dispatchNativeOp` → `vm.chargeAllocBytes(core.MeterScalarBytes)`,
core/vm/vm.go:1216). Under an `Eval`/`Call` that carries evaluation state,
that charge lands on the shared `*evalState` as an `atomic.Int64.Add` — one
atomic RMW on shared memory per arithmetic instruction.

Profile evidence (fib(20) bytecode, HEAD, 2026-07-27): `atomic.Int64.Add` is
8.33% flat CPU, 93.5% of it called from `evalState.chargeAllocBytes`;
`evalState.chargeAllocBytes` is 10.22% cumulative. A spike that removed only
the scalar charge moved fib(20) from 2.77ms to 2.53ms (−9%), allocations
unchanged.

The VM already batches its *reduction* accounting: `vm.budget` counts down in
a plain field and `flushConsumedReductions` settles into the shared ledger at
every poll boundary (`pollCancel`, ≤128 instructions apart, core/vm/vm.go:695)
and at run exits. Alloc-byte charges are the one ledger stream still written
per instruction. The meter contract already amortizes observation: leases are
drawn in grants of up to 1,024 reductions and 64 KiB (runtime-api "Meter
interface with engine-side lease amortization"), so a ≤128-instruction
settlement window is strictly finer than the slack the lease design already
accepts. Wasmtime and wasmer both meter fuel/gas at basic-block granularity
computed at translation time, not per instruction — the same posture. ADR
0011 already states the intent outright: metering must piggyback the existing
128-step budget rather than add per-step atomic writes to the hot loop —
this change makes the alloc-byte stream comply.

The charge reaches the shared atomics because `Eval`/`Call` under evaluation
state attach the eval-state meter to the VM (`SetEvalMeter`), making
`vm.chargeAllocBytes` route to `evalState.chargeAllocBytes` per op — the
scalar site is the tail of `dispatchNativeOp`, i.e. every fused arithmetic
instruction, which is why fib pays it ~½M times per evaluation.

## What Changes

- Ledger charges issued by VM opcodes (scalar results, collection
  construction, closure creation, charged constants) accumulate in a VM-local
  plain field — the same pattern as `vm.budget` — instead of writing to the
  shared `evalState`/meter per instruction.
- The accumulated bytes settle into the shared ledger at exactly the points
  where the ledger is externally observable: every poll boundary
  (`pollCancel`), run exit (normal and error unwind), and immediately before
  any `GoFunc` dispatch or re-entrant adoption — a host function or nested
  evaluation sees totals identical to per-instruction charging.
- Limit enforcement (`MaxAllocationBytes`, meter lease exhaustion) moves to
  the settlement points; enforcement slack is bounded by one batch window
  (≤128 instructions × the per-op fixed scalar size), within the lease
  amortization the meter spec already grants.
- No public API change. Tree-walker charging is untouched.

## Impact

- Affected specs: `bytecode-vm` (Fused native-op results charge the
  allocation ledger — charge timing; Batched cancellation observation —
  settlement points).
- Affected code: `core/vm/vm.go` charge sites, `pollCancel`, run exits,
  GoFunc dispatch path. No compiler change.
- Expected: fib(20) −8-10%; VM goldset cells non-increasing time,
  bytes/allocs unchanged; Rule/Call small win.
- Risk: a limit error can fire up to one batch window later than today —
  bounded, inside lease slack; crossval must show identical terminal errors
  on limit-exceeding programs.
