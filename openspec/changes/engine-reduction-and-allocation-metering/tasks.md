## 1. Behavior contracts

- [ ] 1.1 Red adversarial tests: tight allocation loop; macro-amplified
  recursion; deep compiler emit; GoFunc building large results in a loop
  (`str` concatenation); flat-but-huge reader literal charged before eval —
  each trips `CodeResourceLimit` under tight limits, green under defaults.
  New-behavior coverage fails against pre-change behavior; characterization
  coverage records the unchanged baseline.
- [ ] 1.2 Race tests: per-evaluation reduction + allocation counters isolated
  across goroutines (extend the `MaxStructuralDepth` race pattern in
  `eval_hardening_test.go`).
- [ ] 1.3 Non-catchability: each ceiling breach passes through `try`/`catch`
  on both evaluators (reuses `eval-noncatchable-terminal-errors` classifier;
  test here proves the metering errors qualify).

## 2. Implementation

- [ ] 2.1 Add `MaxReductions`, `MaxAllocationBytes` to
  `runtime.ResourceLimits`; defaults 10M / 64 MiB; immutable; resolved at
  `New`.
- [ ] 2.2 Extend `evalState` with reduction + allocation counters and
  resolved limits; VM frame-local mirror flushed at reloadFrame/poll/exit
  sync points so tree-walker fallback and GoFunc re-entry share one ledger.
- [ ] 2.3 Reduction flush: tree-walker `pollCancel` and VM `pollCancel`
  transfer consumed 128-step budget to the counter; final flush at eval end;
  ceiling check at each flush. No new interval constant.
- [ ] 2.4 Charge reductions in macro expansion (per step) and compiler emit
  (per instruction), against the same evaluation ledger.
- [ ] 2.5 Charge one reduction per GoFunc dispatch at the two apply sites
  (`core/eval.go` `apply`, `core/vm/vm.go` `apply`); verify no other dispatch
  path exists.
- [ ] 2.6 Allocation charges with the fixed size table: VM
  `OpMakeList`/`OpMakeVector`/`OpMakeMap`/`OpClosure`; tree-walker
  collection-literal + quasiquote construction; compiler emit (code +
  constants, charged to the compiling evaluation before caching); GoFunc
  result shallow size at both apply sites.
- [ ] 2.7 Reader bridge: reader accumulates node/byte counts during parse;
  engine charges the evaluation ledger after `Read`, before the first form,
  in `Eval` / `EvalWithBindings` / `EvalFile` / `LoadDir` / REPL paths.
- [ ] 2.8 Document the size table in ADR 0011 (values + conservativeness
  rationale + determinism requirement).

## 3. Integration

- [ ] 3.1 Crossval: same adversarial programs under the same limits on both
  evaluators → same terminal error class (no counter-value comparison).
- [ ] 3.2 `go test ./... -race`; `GOLDSET_MODE=vm` goldset gate
  non-increasing vs the previous release (verify the gate itself, not only
  fib).

## 4. Verification

- [ ] 4.1 `openspec validate --strict engine-reduction-and-allocation-metering`.
