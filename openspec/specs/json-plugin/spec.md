# json-plugin Specification

## Purpose

The json-plugin capability implements JSON encoding and decoding between Lisp values and JSON strings, preserving structure across a round trip and detecting whole-number JSON numbers as Int rather than Float.
## Requirements
### Requirement: json-plugin implementation
The system SHALL implement the json-plugin functionality as described in the proposal.

#### Scenario: JSON encoding works
- **WHEN** a Lisp value is passed to `json/encode`
- **THEN** a valid JSON string SHALL be returned

#### Scenario: JSON decoding works
- **WHEN** a valid JSON string is passed to `json/decode`
- **THEN** the corresponding Lisp value SHALL be returned

#### Scenario: Round-trip preserves structure
- **WHEN** a value is encoded then decoded
- **THEN** the result SHALL equal the original value

#### Scenario: Integer detection
- **WHEN** a JSON number with no fractional part is decoded
- **THEN** it SHALL be returned as Int, not Float

### Requirement: Bulk JSON object decode is linear

Decoding a JSON object SHALL scale linearly in the number of keys — it SHALL NOT
rebuild-and-copy the accumulating map once per key. `json/decode` SHALL construct
the resulting `HashMap` with a single-copy builder, so an n-key object decodes in
O(n) rather than O(n²), while preserving the existing round-trip and
integer-detection guarantees. Immutability SHALL hold: the in-place builder is used
only before the finished map is returned to the caller, never to mutate a map that
has already been exposed.

#### Scenario: Large object decodes without quadratic blowup

- **WHEN** a JSON object with several thousand keys is passed to `json/decode`
- **THEN** it SHALL return the corresponding `HashMap` in time proportional to the key count, not its square

#### Scenario: Round-trip still preserves structure

- **WHEN** a value is encoded then decoded
- **THEN** the result SHALL equal the original value, and whole-number JSON values SHALL decode as `Int`, not `Float`

### Requirement: JSON decode charges constructed allocation

`json/decode` SHALL charge the evaluation allocation ledger for the full
constructed value it returns, measured deeply so nested `Vector`/`HashMap`
structure counts — not by the outer container's shallow slot count. A payload
whose decoded structure exceeds the remaining allocation budget SHALL fail with
a `ResourceLimitError` rather than returning an uncharged large structure. The
existing linear-decode, round-trip, and integer-detection guarantees SHALL hold
unchanged.

#### Scenario: Deeply nested decode is charged for its real size

- **WHEN** a compact JSON payload decodes into a large nested structure under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the decoded value, not the outer container's shallow size

#### Scenario: Over-budget decode fails closed

- **WHEN** a JSON payload decodes into a structure exceeding the remaining `MaxAllocationBytes`
- **THEN** `json/decode` SHALL return a `ResourceLimitError`

#### Scenario: Ordinary decode is unchanged

- **WHEN** a payload decodes within the allocation budget
- **THEN** it SHALL return the corresponding value, and whole-number JSON values SHALL still decode as `Int`, not `Float`

