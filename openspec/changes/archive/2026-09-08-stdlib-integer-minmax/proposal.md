## Why

Review of commit `3bcf9c1` reproduced `(max 9223372036854775807)` returning `-9223372036854775808`: integer extrema pass through `float64` even when every operand is an integer. Policy code can therefore receive a rounded value or a reversed sign from an operation that should return an exact operand.

## What Changes

- Preserve exact `int64` extrema when every argument to `min` or `max` is `core.Int`.
- Preserve current float promotion, result types, argument validation, and work accounting when a `core.Float` occurs.
- Add endpoint and adjacent-integer regressions through both execution modes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: add an exact integer extrema requirement without replacing existing numeric accounting or error requirements.

## Impact

Implementation is confined to `minMaxFunc` in `plugins/stdlib/arithmetic.go`, its regression coverage, and any executable inventory entries affected by branch changes. No new numeric type, dependency, or VM opcode is required. This change has no predecessor; JSON decoding and result metering are separate changes.
