# dialect Specification

## ADDED Requirements

### Requirement: Reader syntax varies by Dialect flags
A Dialect SHALL carry reader feature flags controlling: `[..]` and `{..}` literal syntax, `#'` function-reference syntax, and `#(...)` vector syntax. The reader SHALL honor the running Dialect's flags when tokenizing and parsing source.

#### Scenario: Function-reference syntax gated by flag
- **WHEN** an Engine runs a Dialect with `#'` enabled
- **THEN** `#'foo` SHALL read as a function-reference form
- **AND** under a Dialect with `#'` disabled, `#'foo` SHALL NOT read as a function-reference form

#### Scenario: Reader-vector syntax gated by flag
- **WHEN** an Engine runs a Dialect with `#(...)` enabled
- **THEN** `#(...)` SHALL read as a vector form

#### Scenario: Bracket literals gated by flag
- **WHEN** an Engine runs a Dialect with `[..]` literals disabled
- **THEN** `[1 2]` SHALL NOT read as a vector literal
- **AND** under a Dialect with `[..]` literals enabled, `[1 2]` SHALL read as a vector literal

### Requirement: Identity Dialect reader flags are unchanged
The identity Dialect SHALL enable `[..]`/`{..}` literals and disable `#'` and `#(...)`, so an Engine created without changing reader flags parses source exactly as before this change.

#### Scenario: Default Engine parsing is preserved
- **WHEN** an Engine is created without changing reader flags
- **THEN** `[..]` and `{..}` literals SHALL parse as before, and `#'`/`#(...)` SHALL NOT be special
