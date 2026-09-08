## ADDED Requirements

### Requirement: JSON decode accounts its full result exactly once per dispatch

Every successful `json/decode` invocation SHALL charge the full constructed
result's deep allocation size exactly once, including the root value. Public
dispatch SHALL NOT add another shallow charge for the same result. The existing
deep-allocation, numeric-conversion, and terminal-refusal requirements SHALL
remain in force.

When otherwise sufficient evaluation resources leave exactly the decoded
result's deep allocation charge available, decoding SHALL succeed. One byte less
SHALL fail with a terminal `ResourceLimitError` and publish no value. Result
accounting for one dispatch SHALL NOT suppress the charges of later dispatches.

#### Scenario: Scalar root is charged once

- **WHEN** `json/decode` decodes `42` through public dispatch with an otherwise sufficient budget
- **THEN** its result allocation charge SHALL be 16 bytes, not 32 bytes

#### Scenario: Nested result includes its root once

- **WHEN** `json/decode` returns a nested array or object through public dispatch
- **THEN** its charge SHALL equal the full deep result size without an additional shallow root charge

#### Scenario: Exact result budget succeeds

- **WHEN** the remaining allocation budget equals the deep charge of a scalar, string, empty container, or nested decoded result
- **THEN** decoding SHALL succeed and consume exactly that result charge

#### Scenario: One byte below the result budget fails closed

- **WHEN** the remaining allocation budget is one byte below the decoded result's deep charge
- **THEN** decoding SHALL fail with a terminal `ResourceLimitError` and SHALL return no value

#### Scenario: Later dispatches keep their own charges

- **WHEN** several decodes or a decode followed by another builtin execute within one evaluation ledger
- **THEN** each result SHALL retain its own required charge without duplicate root charges or suppression by an earlier call

#### Scenario: Both execution modes honor exact thresholds

- **WHEN** identical direct named-call threshold cases run under the Evaluator and VM
- **THEN** both SHALL accept the exact result budget, reject one byte less, and report the specified result charge
