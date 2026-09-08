## Context

See `proposal.md` for the verified round-trip failure at review commit `3bcf9c1`. `json/decode` unmarshals into `any`, then `fromJSONValue` sees only rounded `float64` numbers. `TestDecodeLargeIntegers` explicitly expects `Float` at `9007199254740992`, while the accepted spec promises whole-number `Int` detection without defining its domain.

## Goals / Non-Goals

**Goals:** classify the exact decimal value before float conversion; preserve every integral value within `int64`; retain finite float fallback and existing parsing behavior.

**Non-Goals:** bigint Lisp values, external dependencies, lossless arbitrary-precision fractions, changes to container/key conversion, and the separate exactly-once result-metering fix.

## Decisions

### Preserve numeric tokens through decoding

Retain each validated JSON number's original decimal spelling until numeric classification finishes. Plain integer-token parsing alone is insufficient: `42.0`, `4.2e1`, and `9007199254740993.0` represent integers too. Do not use a rounded float to decide whether the exact number has a fractional part or fits in `int64`.

Classify sign, significant digits, fractional scale, exponent, and trailing zeroes without expanding powers of ten. Compare the normalized integer magnitude against the signed endpoint before accumulation or conversion. Exponent accumulation must saturate at a bound derived from token length and the fixed integer domain; storage and work remain proportional to token length, not exponent magnitude. Zero remains exact regardless of a valid exponent spelling.

Numbers outside the exact integer case use the existing float conversion semantics: a finite value, including underflow to zero, returns `core.Float`; conversion overflow still returns an error. A fractional token that rounds to an integer float remains a `Float`. Rejecting every out-of-`int64` whole number was considered and not selected; finite fallback remains supported.

### Preserve the parsing boundary

Continue requiring exactly one JSON value, followed only by whitespace. If decoding becomes stream-based, explicitly check that no second value or trailing garbage remains. Preserve malformed-input errors, key conversion, recursive array/object conversion, depth checks, and the linear `HashMap` construction path.

### Own the numeric requirement updates

This change modifies all three existing JSON requirements to replace unconditional integer promises with the agreed `int64` domain and finite float fallback. The existing deep-allocation requirement stays in force; `json-result-metering` adds its exactly-once refinement after this change is implemented and archived.

## Verification

Testing mode: existing-service-strict. Add failing regressions before implementation and replace the contradictory outside-safe-range expectation in `TestDecodeLargeIntegers`.

Assert exact `core.Int` values for both `int64` endpoints and adjacent integers around positive and negative `2^53`, at the root and inside arrays/maps. Cover decimal and exponent equivalents, including `9223372036854775807.0`, `-9.223372036854775808e18`, and `9007199254740993.0`. Assert finite `core.Float` for fractional inputs, near-integer fractions, and `9223372036854775808` / `-9223372036854775809`; the latter cases are fallback checks, not exact integer guarantees.

Cover positive exponent overflow, negative exponent underflow, zero with a large exponent, long exponent digit strings, malformed JSON, trailing whitespace, a second value, and trailing garbage. Include true integer encode/decode round trips through `Engine.Call` and `Engine.Eval` under both explicit execution modes. Keep `TestDecodeHashMap_Scaling`, round-trip, immutability, construction-depth, and allocation tests valid; update any test-only decoded intermediate representation to match the production decoder.

## Risks / Trade-offs

- Existing callers may branch on `core.Float` for large whole numbers → mark the type change in the existing changelog and numeric documentation.
- Arbitrary exponent expansion can turn a short token into huge work → use bounded digit/scale classification with no power expansion.
- A stream decoder can accept a valid prefix → retain the single-value end-of-input check.
- Fractional or out-of-range values remain approximate → make finite float fallback explicit in every affected guarantee.

## Migration Plan

No predecessor or stored-data migration. Update existing JSON documentation and the capability purpose to state the integer domain when implementing this change; do not create a new architecture document. Archive this change before starting `json-result-metering`. Rollback restores the former numeric type and precision behavior.
