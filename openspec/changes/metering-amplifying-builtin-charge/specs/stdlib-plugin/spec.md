# stdlib-plugin — delta

## ADDED Requirements

### Requirement: Amplifying builtins charge output allocation eagerly

A builtin that can construct output disproportionate to its input SHALL charge
the evaluation allocation ledger for its constructed output before performing
the amplifying allocation, rather than relying on the shallow post-return
`GoFunc` result charge. `format` SHALL estimate its output size from the parsed
width/precision specifiers and charge that estimate before calling into Go's
formatter; when the estimate exceeds the remaining allocation budget it SHALL
return a `ResourceLimitError` without building the string. Builtins whose output
is proportional to their input (for example string concatenation, whose result
length equals the sum of inputs) MAY continue to rely on the shallow result
charge. The single governing knob remains `MaxAllocationBytes`.

#### Scenario: format fails closed before amplifying allocation

- **WHEN** `format` is called with specifiers whose estimated output exceeds the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` and SHALL NOT build the oversized string

#### Scenario: Ordinary format still succeeds

- **WHEN** `format` produces output within the allocation budget
- **THEN** it SHALL return the formatted string and charge the ledger for it

#### Scenario: Non-amplifying builtins are unaffected

- **WHEN** a builtin whose output is proportional to its input runs under the ledger
- **THEN** it SHALL behave as before, charged by its shallow result size
