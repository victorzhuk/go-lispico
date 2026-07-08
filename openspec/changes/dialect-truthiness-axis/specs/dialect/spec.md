# dialect Specification

## ADDED Requirements

### Requirement: Truthiness is a Dialect axis
A Dialect SHALL set the truthiness axis to one of two values: only `nil` is falsy, or both `nil` and `false` are falsy. All conditional special forms — `if`, `when`, `unless`, `cond`, `and`, `or`, `not` — SHALL determine truthiness from the running Dialect's setting.

#### Scenario: nil-only truthiness treats false as true
- **WHEN** an Engine runs a Dialect with `nil`-only truthiness
- **THEN** `(if false :yes :no)` SHALL evaluate to `:yes`

#### Scenario: nil-plus-false truthiness treats false as falsy
- **WHEN** an Engine runs a Dialect with `nil`+`false` truthiness
- **THEN** `(if false :yes :no)` SHALL evaluate to `:no`

#### Scenario: Axis applies across all conditional forms
- **WHEN** an Engine runs a Dialect with `nil`-only truthiness
- **THEN** `when`, `unless`, `cond`, `and`, `or`, and `not` SHALL each treat `false` as a true value consistently with `if`

### Requirement: Identity Dialect truthiness is unchanged
The identity Dialect SHALL keep `nil`+`false` truthiness, so an Engine created without selecting the axis behaves as before this change.

#### Scenario: Default Engine keeps prior truthiness
- **WHEN** an Engine is created without changing the truthiness axis
- **THEN** `(if false :yes :no)` SHALL evaluate to `:no`
