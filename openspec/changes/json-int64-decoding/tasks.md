## 0. Confirm the numeric contract

- [ ] 0.1 Recheck `fromJSONValue`, `TestDecodeLargeIntegers`, and the three existing JSON requirements against the `3bcf9c1` reproduction; verify the implementation scope uses exact in-range integral values and finite float fallback outside that case. This change has no predecessor; testing mode is existing-service-strict.

## 1. Pin precision and compatibility before the fix

- [ ] 1.1 Replace the outside-safe-range type expectation and add exact-value regressions for both signed endpoints and adjacent integers around positive and negative `2^53`, at the root and in arrays/maps; verify the old decoder fails these `core.Int` assertions.
- [ ] 1.2 Add whole decimal/exponent endpoint cases, fractional values that round to integral floats, finite out-of-`int64` fallback, overflow, underflow, zero with large exponents, and long exponent tokens; verify the matrix distinguishes exact numeric classification from float rounding.
- [ ] 1.3 Add public `Engine.Call` and `Engine.Eval` round trips under `WithTreeWalker()` and `WithBytecode()`; verify exact integer results and concrete numeric types at both dispatch boundaries.
- [ ] 1.4 Pin parser compatibility for malformed JSON, trailing whitespace, a second top-level value, and trailing garbage; verify only a single value with optional whitespace succeeds before changing the parser.

## 2. Decode without losing the numeric lexeme

- [ ] 2.1 Preserve validated number spellings and classify exact integrality and signed `int64` range before float conversion; verify the numeric regression matrix passes with no bigint Lisp type or new dependency.
- [ ] 2.2 Bound digit/scale/exponent processing by token length without expanding powers of ten; verify large-exponent cases complete within the test timeout and preserve finite fallback or overflow errors as specified.
- [ ] 2.3 Retain single-value parsing, linear object construction, recursive container conversion, depth checks, and current deep charging; verify existing round-trip, immutability, `TestDecodeHashMap_Scaling`, and allocation tests pass, updating test-only intermediate representations when required.

## 3. Verify and disclose the type change

- [ ] 3.1 Run `make test GOTESTFLAGS="-timeout 2m -p 2 -parallel 2"` and `make lint`; record commands, results, and new regression names without folding in the separate exactly-once metering fix.
- [ ] 3.2 Update the existing JSON capability purpose and README JSON description for exact in-range integers and finite float fallback; add a `[Unreleased]` entry in `CHANGELOG.md` explaining the breaking numeric type change, and verify no unconditional arbitrary-range integer guarantee remains.
- [ ] 3.3 After implementation and required verification, archive this change and verify its three updated requirement blocks are the accepted `json-plugin` baseline before `json-result-metering` starts.
