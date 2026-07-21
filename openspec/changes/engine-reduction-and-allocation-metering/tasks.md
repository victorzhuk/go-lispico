## 1. Behavior contracts

- [ ] 1.1 Red adversarial tests: tight allocation loop; macro-amplified recursion; deep compiler emit; GoFunc-dispatch loop — each trips `CodeResourceLimit` under tight limits, green under defaults. New-behavior coverage fails against pre-change behavior; characterization coverage records the unchanged baseline.
- [ ] 1.2 Race tests: per-evaluation reduction + allocation counters isolated across goroutines (extend the existing `MaxStructuralDepth` race pattern in `eval_hardening_test.go`).

## 2. Implementation

- [ ] 2.1 Add `MaxReductions`, `MaxAllocationBytes` to `runtime.ResourceLimits`; defaults 10M / 64 MiB; immutable; resolve at `New`.
- [ ] 2.2 Extend per-call `evalState` with reduction + allocation counters; thread limits through the same context key as `structDepth`.
- [ ] 2.3 Charge reductions in tree-walker: every apply-trampoline iteration and every form dispatch.
- [ ] 2.4 Charge reductions in VM: every instruction decode in the run loop.
- [ ] 2.5 Charge reductions in macro expansion, bytecode compiler emit, and plugin `GoFunc` dispatch.
- [ ] 2.6 Charge allocation bytes in Vector / HashMap / List / String constructors, Chunk emit, and `evalState` allocation; use a documented approximate per-type size table.
- [ ] 2.7 `ctx.Err()` check every 1,024 reductions, integrated with the v0.8.0 batched-cancellation countdown (no new clock read).
- [ ] 2.8 Verify the VM `try`/`catch` opcode handler passes `CodeResourceLimit` through; extend the tree-walker pass-through (`core/eval.go:971-975`) to the VM if missing.

## 3. Integration

- [ ] 3.1 Crossval test: VM and tree-walker agree on counter values for shared forms under the same limits.
- [ ] 3.2 `go test ./... -race`; `GOLDSET_MODE=vm` goldset gate non-increasing vs the previous release.

## 4. Verification

- [ ] 4.1 `openspec validate --strict engine-reduction-and-allocation-metering`.
