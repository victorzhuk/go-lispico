## 0. Verify the required predecessor

- [ ] 0.1 Confirm `json-int64-decoding` is implemented, verified, and archived; inspect the accepted `json-plugin` spec and numeric tests to verify its exact-`int64` and finite-float policy is the baseline. Do not implement this change while that predecessor remains unarchived; testing mode is existing-service-strict.

## 1. Pin dispatch accounting before the fix

- [ ] 1.1 Extend the direct-function coverage around `TestDecodeChargesDeepResultBytes` with `core.Evaluator.Apply`; verify decoding `"42"` reproduces the 32-byte dispatch charge against a 16-byte deep result.
- [ ] 1.2 Add real `Engine.Call` regressions under `WithTreeWalker()` and `WithBytecode()` for scalar, string, empty-container, and nested results; verify expected full deep bytes are derived from independently constructed expected values, not from the invocation under test.
- [ ] 1.3 Add exact-deep-budget success and one-byte-below terminal failure assertions using fresh contexts, plus cumulative decode and later-builtin cases; verify the current duplicate root charge makes the exact-budget success regression fail.

## 2. Mark the full result charge at return

- [ ] 2.1 Replace the generic successful-result charge with `core.ChargeGoFuncResultBytes(ctx, deep)` after the existing depth and deep-size checks; verify the full result is charged once and over-budget calls return no value.
- [ ] 2.2 Verify both public execution modes and direct `core.Evaluator.Apply` pass the exact-threshold matrix, sequential calls keep their charges, and predecessor numeric behavior and terminal publication refusals remain unchanged.

## 3. Verify and document the correction

- [ ] 3.1 Run `make test GOTESTFLAGS="-timeout 2m -p 2 -parallel 2"` and `make lint`; record commands, results, and regression names for the complete dispatch path.
- [ ] 3.2 Add a concise `[Unreleased]` fix entry in `CHANGELOG.md`; verify it describes removal of the duplicate root allocation charge while preserving full deep accounting.
