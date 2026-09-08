## MODIFIED Requirements

### Requirement: json-plugin implementation

The system SHALL implement JSON encoding and decoding with its existing
container and key conversions. `json/decode` SHALL return an exact `Int` for
every mathematically integral JSON number in the inclusive range
`-9223372036854775808` through `9223372036854775807`, including decimal and
exponent spellings. Classification SHALL use the number's exact decimal value
before any rounding to a float.

A fractional number or a number outside that integer range SHALL return a
`Float` when the existing float conversion yields a finite value. Conversion
overflow SHALL remain an error. This fallback does not promise exact integer
or fractional preservation outside the exact `Int` case. Numeric-token work
and storage SHALL be bounded by token length rather than the magnitude of its
exponent. Input SHALL contain exactly one JSON value followed only by optional
whitespace; malformed input, a second value, and trailing garbage SHALL fail.

#### Scenario: JSON encoding works

- **WHEN** a supported Lisp value is passed to `json/encode`
- **THEN** a valid JSON string SHALL be returned

#### Scenario: JSON decoding works

- **WHEN** one valid JSON value within the supported numeric conversion domain is passed to `json/decode`
- **THEN** the corresponding Lisp value SHALL be returned

#### Scenario: Round-trip preserves structure

- **WHEN** a JSON-compatible value containing `Int` elements is encoded then decoded
- **THEN** the result SHALL preserve the existing container/key conversion semantics and every integer's exact value

#### Scenario: Integer detection

- **WHEN** a JSON number has an exact value with no fractional part within the signed 64-bit integer range
- **THEN** it SHALL be returned as the exact `Int`, not `Float`, regardless of integer, decimal, or exponent spelling

#### Scenario: Integer endpoints and adjacent large values survive decoding

- **WHEN** either signed 64-bit endpoint or either of `9007199254740992` and `9007199254740993` is decoded at the root or inside an array or object
- **THEN** the corresponding value SHALL be an exact `Int`, with distinct adjacent values remaining distinct

#### Scenario: Whole decimal and exponent spellings remain exact

- **WHEN** `9223372036854775807.0`, `-9.223372036854775808e18`, or `9007199254740993.0` is decoded
- **THEN** each SHALL produce its mathematically exact signed 64-bit `Int`

#### Scenario: Finite float fallback remains supported

- **WHEN** a fractional number or an out-of-range whole number such as `9223372036854775808` converts to a finite float
- **THEN** decoding SHALL return that `Float` rather than reject it or narrow it into `Int`

#### Scenario: Float rounding does not change numeric classification

- **WHEN** a fractional JSON number rounds to an integral finite float, including underflow to zero
- **THEN** the result SHALL remain a `Float`

#### Scenario: Nonfinite conversion overflow remains an error

- **WHEN** a JSON number exceeds the supported finite float conversion range and is not an exact in-range integer
- **THEN** decoding SHALL return an error rather than publish a nonfinite numeric value

#### Scenario: Exponent magnitude does not amplify parsing work

- **WHEN** a valid numeric token has a large exponent or a long exponent digit string
- **THEN** classification SHALL use work and storage bounded by token length, while retaining the specified integer, finite-float, or overflow result

#### Scenario: Trailing input is checked

- **WHEN** a JSON value is followed by whitespace, a second value, or non-whitespace garbage
- **THEN** only the whitespace-only suffix SHALL be accepted

#### Scenario: Execution modes share numeric behavior

- **WHEN** the same numeric decode or integer round trip is invoked through public dispatch under the Evaluator and VM
- **THEN** both SHALL return the same value and numeric type or equivalent conversion error

### Requirement: Bulk JSON object decode is linear

Decoding a JSON object SHALL scale linearly in the number of keys — it SHALL NOT
rebuild-and-copy the accumulating map once per key. `json/decode` SHALL construct
the resulting `HashMap` with a single-copy builder, so an n-key object decodes in
O(n) rather than O(n²), while preserving the structure conversions and numeric
rules in `json-plugin implementation`. Immutability SHALL hold: the in-place
builder is used only before the finished map is returned to the caller, never
to mutate a map that has already been exposed.

#### Scenario: Large object decodes without quadratic blowup

- **WHEN** a JSON object with several thousand keys is passed to `json/decode`
- **THEN** it SHALL return the corresponding `HashMap` in time proportional to the key count, not its square

#### Scenario: Round-trip still preserves structure

- **WHEN** a JSON-compatible value containing `Int` elements is encoded then decoded
- **THEN** the existing container/key conversions SHALL be preserved, integral values within `int64` SHALL decode as exact `Int`, and other numbers SHALL follow the specified float fallback

### Requirement: JSON decode charges constructed allocation

`json/decode` SHALL charge the evaluation allocation ledger for the full
constructed value it returns, measured deeply so nested `Vector`/`HashMap`
structure counts — not by the outer container's shallow slot count. A payload
whose decoded structure exceeds the remaining allocation budget SHALL fail with
a `ResourceLimitError` rather than returning an uncharged large structure. The
existing linear-decode and structure-conversion guarantees SHALL hold, with
integer detection and finite float fallback governed by the numeric conversion
rules in this capability.

#### Scenario: Deeply nested decode is charged for its real size

- **WHEN** a compact JSON payload decodes into a large nested structure under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the decoded value, not the outer container's shallow size

#### Scenario: Over-budget decode fails closed

- **WHEN** a JSON payload decodes into a structure exceeding the remaining `MaxAllocationBytes`
- **THEN** `json/decode` SHALL return a `ResourceLimitError`

#### Scenario: Ordinary decode is unchanged

- **WHEN** a payload decodes within the allocation budget
- **THEN** it SHALL return the corresponding value, with exact in-range integral numbers as `Int` and other supported numbers following the finite float fallback
