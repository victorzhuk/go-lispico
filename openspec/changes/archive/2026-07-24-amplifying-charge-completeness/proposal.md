## Why

The amplifying-builtin metering pass (archived
`metering-amplifying-builtin-charge`) closed the ledger gap for `format` and
`json/decode`, but two sibling paths that construct output disproportionate to
their input still bypass the deep-allocation charge:

- `assoc` (`plugins/stdlib/collections.go`) does the structurally identical
  "add key(s) to a map" operation as `conj`'s map branch and `merge`, both of
  which call `chargeCollectionResult` (deep-bytes charge + length check) — but
  `assoc` does not. Its result only gets the generic shallow post-return charge
  (`ValueShallowBytes` = header + entry-count, no descent), so a script chaining
  `assoc` with large nested values grows retained memory past what the ledger
  reports. `assoc`'s first argument is an existing, arbitrarily large runtime
  value, so its output is not arity-bounded the way `list`/`vector`/`hash-map`
  are.
- The VM fused native ops (`execNativeFastFused`, `core/vm/vm.go:1084`) push
  their result with no `chargeValue` call, unlike the GoFunc dispatch path which
  charges. A heap-boxed `Float` or out-of-preboxed-range `Int` result of a fused
  `+ - * / < > = …` is invisible to `MaxAllocationBytes`. It is bounded in
  practice by the reduction ceiling, but it is a silent zero-count — the
  opposite of the metering design's over-count principle.

## What Changes

- `assoc` SHALL route its result through the same deep-bytes charge + length
  check (`chargeCollectionResult`) that `conj`'s map branch and `merge` use.
- The VM fused native ops SHALL charge a fixed scalar allocation for their
  result at the fused dispatch site, matching the charge the GoFunc path already
  applies, so fused arithmetic results are not invisible to the allocation
  ledger.
- No API change; scalar-returning non-amplifying builtins keep the cheap shallow
  charge.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: extend the amplifying-builtin charge requirement to cover
  `assoc`.
- `bytecode-vm`: new requirement that fused native-op results charge the
  allocation ledger consistently with the GoFunc dispatch path.

## Impact

- Code: `plugins/stdlib/collections.go` (`assoc`), `core/vm/vm.go`
  (`execNativeFastFused`).
- Behavior: `assoc` chains and fused-arithmetic results now count against
  `MaxAllocationBytes`, closing the remaining silent under-counts in the
  allocation governor.
- Goldset: the fused-op charge is a fixed size computation on an already-boxed
  result — verify the VM cells stay non-increasing.
