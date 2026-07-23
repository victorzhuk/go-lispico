# data-plugin — delta

## ADDED Requirements

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
