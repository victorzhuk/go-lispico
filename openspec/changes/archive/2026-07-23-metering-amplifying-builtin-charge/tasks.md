## 1. Behavior contracts

- [x] 1.1 Red test: `json/decode` of a compact payload whose decoded structure
  exceeds a low `MaxAllocationBytes` fails with a `ResourceLimitError`; a
  payload under the limit decodes and is charged proportionally to
  `ValueDeepBytes` of the result (assert the ledger moved by ~the deep size, not
  the shallow size).
- [x] 1.2 Red test: `format` with many wide verbs whose estimated output
  exceeds a low `MaxAllocationBytes` fails closed BEFORE `fmt.Sprintf` builds
  the string (assert no multi-hundred-MB transient — e.g. the call returns the
  limit error quickly and total allocation stays bounded).
- [x] 1.3 Characterization: an ordinary `format`/`json/decode` under budget
  still returns the correct value; round-trip and integer-detection guarantees
  for `json/decode` are unchanged.
- [x] 1.4 Audit tests: `repeat`/`make-string`-style and count-driven builders
  identified in the design either charge their output eagerly or are shown
  already bounded.

## 2. Implementation

- [x] 2.1 `json/decode`: `core.ChargeEvalAllocBytes(ctx, core.ValueDeepBytes(result))`
  after `fromJSONValue`, before return.
- [x] 2.2 `format`: parse width/precision specifiers to an upper-bound output
  estimate; `ChargeEvalAllocBytes` the estimate before `fmt.Sprintf`; return the
  ledger error without building on overflow.
- [x] 2.3 Apply the same eager-charge to the amplifying builders found in the
  audit; leave non-amplifying builtins on the shallow generic charge.

## 3. Integration

- [x] 3.1 `go test ./... -race` green.
- [x] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — the added charges are
  size computations (no Go allocation) on the amplifying builtins only; the
  scalar-returning hot path is untouched. Verify allocs/op unchanged.
- [x] 3.3 Confirm charges are deterministic (fixed size table) across a second
  run.

## 4. Verification

- [x] 4.1 `openspec validate --strict metering-amplifying-builtin-charge`.
- [x] 4.2 CHANGELOG `[Unreleased]` note under Fixed/Changed: amplifying builtins
  now charge the allocation ledger for their constructed output.
