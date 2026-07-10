# data-plugin — delta

## ADDED Requirements

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
