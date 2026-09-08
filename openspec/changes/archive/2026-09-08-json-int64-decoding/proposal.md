## Why

Review of commit `3bcf9c1` reproduced `(json/decode (json/encode 9007199254740993))` returning `core.Float` with value `9007199254740992`. Decoding through `float64` loses integer information before the plugin chooses the Lisp numeric type, contradicting the JSON integer and round-trip requirements.

## What Changes

- Decode every mathematically integral JSON number within `int64` as exact `core.Int`, including decimal and exponent spellings.
- **BREAKING**: integer values from `9007199254740992` upward, and their negative counterparts within `int64`, change from `core.Float` to `core.Int`; the existing safe-range test must change.
- Fractional tokens that round to a whole float remain `core.Float`, including underflow to zero, rather than being misclassified as integers.
- Preserve finite `core.Float` fallback for fractional or out-of-`int64` numbers and existing errors when conversion overflows the finite float range.
- Bound numeric-token processing by token length, retain one-value JSON parsing, and reconcile the three existing JSON requirements with the explicit numeric domain.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `json-plugin`: refine numeric decoding and round-trip guarantees while preserving object construction and deep allocation requirements.

## Impact

Affected code is `plugins/json/plugin.go`, numeric and structural JSON tests, and public runtime parity tests. No bigint Lisp type or external dependency is added. This change has no predecessor; `json-result-metering` must start after this change is implemented and archived so it adds to the accepted JSON spec baseline.
