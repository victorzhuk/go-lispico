## ADDED Requirements

### Requirement: Integer min and max preserve exact extrema

When every argument is an `Int`, `min` and `max` SHALL return the exact minimum
or maximum as an `Int` throughout the signed 64-bit integer range. A singleton
call SHALL return that integer unchanged in value. If any argument is a `Float`,
the operation SHALL retain its existing float promotion, comparison, and result
type behavior. Existing arity, type-error, cancellation, and resource-accounting
contracts SHALL remain unchanged.

#### Scenario: Singleton endpoint retains its value

- **WHEN** `min` or `max` receives only `-9223372036854775808` or only `9223372036854775807`
- **THEN** it SHALL return an `Int` with exactly the supplied value

#### Scenario: Adjacent large integers remain distinguishable

- **WHEN** `min` and `max` receive `9007199254740992` and `9007199254740993` in either order
- **THEN** they SHALL return `9007199254740992` and `9007199254740993`, respectively, as `Int` values

#### Scenario: Negative and mixed-sign integer extrema remain exact

- **WHEN** `min` and `max` receive adjacent negative integers beyond the exact float range or both signed 64-bit endpoints
- **THEN** each SHALL select the exact integer extremum without rounding or changing its sign

#### Scenario: A float operand preserves float promotion

- **WHEN** `(max 2 1.5)` and `(min 1 1.5)` are evaluated
- **THEN** they SHALL return `Float` values `2` and `1`, respectively

#### Scenario: Public dispatch agrees across execution modes

- **WHEN** identical integer-extrema calls run through the public named-call or source-evaluation boundary under the Evaluator and VM
- **THEN** both modes SHALL return the same exact integer value and type
