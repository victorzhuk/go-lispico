# data-plugin Specification

## Purpose

The data-plugin capability implements JSON encoding and decoding between Lisp values and JSON strings, preserving structure across a round trip and detecting whole-number JSON numbers as Int rather than Float.

## Requirements

### Requirement: data-plugin implementation
The system SHALL implement the data-plugin functionality as described in the proposal.

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
