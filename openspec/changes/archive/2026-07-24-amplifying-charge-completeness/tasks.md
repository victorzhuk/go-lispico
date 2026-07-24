## 1. Behavior contracts

- [x] 1.1 Red test: chained `assoc` with large nested values under a low
  `MaxAllocationBytes` fails with a `ResourceLimitError`; under budget it decodes
  and the ledger moves by ~the deep size, not the shallow entry count.
- [x] 1.2 Red test: a fused-arithmetic loop producing heap-boxed results under a
  low `MaxAllocationBytes` charges the ledger (assert the ledger advances vs the
  pre-change zero-count).
- [x] 1.3 Characterization: ordinary `assoc` and ordinary fused arithmetic under
  budget return identical values to today.

## 2. Implementation

- [x] 2.1 `assoc`: route the result through `chargeCollectionResult` like
  `conj`'s map branch and `merge`.
- [x] 2.2 `execNativeFastFused` (`core/vm/vm.go`): charge a fixed scalar size for
  the result at the dispatch site, mirroring the GoFunc-path charge.

## 3. Integration

- [x] 3.1 `go test ./... -race` green.
- [x] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — both charges are size
  computations, no Go allocation added; verify allocs/op unchanged on the fused
  arithmetic cells especially.
- [x] 3.3 Charges deterministic (fixed size table) across a second run.

## 4. Verification

- [x] 4.1 `openspec validate --strict amplifying-charge-completeness`.
- [x] 4.2 CHANGELOG `[Unreleased]` under Fixed: `assoc` and fused native-op
  results now charge the allocation ledger.
