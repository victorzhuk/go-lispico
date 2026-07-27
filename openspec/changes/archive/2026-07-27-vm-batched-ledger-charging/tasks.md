# Tasks — vm-batched-ledger-charging

## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (one session, alternate old/new rounds,
      `-count=10`): external harness fib/Call/Callback/Rule rows with
      `-benchmem`; in-repo goldset both modes. Record the
      `evalState.chargeAllocBytes` and `atomic.Int64.Add` CPU shares on fib
      as the before-picture (10.22% cum / 8.33% flat at HEAD 2026-07-27).
      Treat one untouched row as a control; if the control moves, rerun.

## 2. Accumulate locally, settle at observation points

- [x] 2.1 Add a VM-local pending-bytes field; convert every opcode-issued
      `chargeAllocBytes`/`chargeValue` call in the run loop to accumulate
      into it. Inventory first: vm.go:1216 (scalar), :1024 (list), :1040
      (vector), :1062 (hashmap), :1085 (closure), :768 (charged const),
      :656/:1676 (chargeValue) — re-grep at implementation time.
- [x] 2.2 Settle pending bytes into the existing charge path at: every
      `pollCancel`, both `run` exits (return and error unwind, including
      terminal-error `Reset`), and before `reentrantCtx` adoption /
      `GoFunc` dispatch, so host-visible totals are exact at every
      observation point.
- [x] 2.3 Enforcement at settlement: when the settled total exceeds
      `MaxAllocationBytes` or the meter lease is exhausted, raise the same
      terminal `ResourceLimitError` as today. Document the ≤1-batch-window
      slack in the requirement text.
- [x] 2.4 Audit every early exit out of `run`/`apply` (throw, handler
      dispatch, terminal Reset) for a missed settlement — an unsettled
      batch must never be dropped when the run's charges are observable
      afterward (meter `ReturnEval` accounting must still balance).

## 3. Verify

- [x] 3.1 Unit: a program crossing `MaxAllocationBytes` mid-batch fails
      with the identical terminal error under VM and tree-walker (crossval
      parity on the ledger boundary cells).
- [x] 3.2 Meter accounting: lease draw/return totals for a metered run are
      byte-identical before/after this change (extend the existing meter
      accounting test with a scalar-heavy body).
- [ ] 3.3 Full floor: build, vet, lint, full suite, `-race`, crossval
      `TestVMVsTreeWalker`, goldset both modes non-increasing.
- [ ] 3.4 Interleaved benchstat vs 1.1 baseline: fib −8% or better;
      Call/Callback/Rule non-regressing. Perfgate verdict is only valid on
      the release runner — do not chase local perfgate FAILs.
