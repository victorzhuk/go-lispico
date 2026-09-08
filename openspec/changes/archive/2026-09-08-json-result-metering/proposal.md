## Why

Review of commit `3bcf9c1` measured decoding `"42"` at 16 allocation bytes through the plugin function and 32 bytes through `core.Evaluator.Apply`. The plugin charges the decoded value deeply without marking its result accounted, so dispatch charges the root again and can reject a payload that fits its allocation budget.

## What Changes

- Charge the full decoded result, including its root, exactly once per successful dispatch.
- Use the existing result-charge contract so neither execution mode adds a second shallow root charge.
- Pin exact-budget success and one-byte-below failure through public dispatch for scalar and nested results.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `json-plugin`: add a distinct exactly-once result-accounting requirement; do not replace `JSON decode charges constructed allocation`.

## Impact

Affected code is the return-charge site in `plugins/json/plugin.go`, plugin tests, and runtime dispatch regressions. Existing core accounting APIs and numeric policy remain unchanged. Start only after `json-int64-decoding` is implemented and archived; its updated numeric guarantees form this change's accepted spec baseline.
