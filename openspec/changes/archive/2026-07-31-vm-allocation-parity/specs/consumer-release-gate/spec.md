# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Every tier states its allocation bound explicitly

A tier SHALL state whether it bounds allocated bytes and allocation count, and
the gate's implementation SHALL apply exactly what the tier states. A tier
whose stated rule is silent on allocation SHALL be read as not bounding it, and
that silence SHALL be deliberate and written down rather than inherited from
the code.

This exists because the `startup` tier's implementation checks neither bytes
nor allocation count, while every other tier applies a non-increase bound, and
nothing in the tier's own description says which is intended. Combined with an
absolute overhead bound that every cell in the current corpus clears, a cell
classified `startup` passes the gate whatever it allocates. No cell is
mis-gated by this today, but a silent unconditional pass is not a property a
release gate may hold by accident.

#### Scenario: A tier's allocation bound is discoverable without reading the gate

- **WHEN** a cell is assigned a tier
- **THEN** whether that tier bounds allocated bytes and allocation count SHALL be answerable from ADR 0008 and `internal/perfgate/tiers.json` alone, without reading `internal/perfgate/perfgate.go`

#### Scenario: An absolute-overhead escape does not become an unconditional pass

- **WHEN** a tier offers an absolute overhead bound as an alternative to a percentage bound
- **THEN** the tier SHALL state what that alternative does and does not excuse, and the gate SHALL NOT pass a cell on an absolute bound that every cell in the corpus clears by construction unless the tier says that is the intended outcome

### Requirement: A failing cell is resolved by measurement, not by relabelling

A cell that fails its tier SHALL be resolved by changing what the engine
measures or by amending the threshold with recorded measured justification.
Reassigning the cell to a tier that passes it, where that tier does not
describe the cell's measured shape, SHALL NOT be a resolution.

A threshold amendment SHALL name the profile and the figures that justify it,
in the same way a tier assignment does, and SHALL be recorded in ADR 0008,
which owns the numbers.

#### Scenario: A cell fails on an axis its tier bounds

- **WHEN** a cell's measured figure fails the bound its tier states
- **THEN** the resolution SHALL be an engine change, or a recorded threshold amendment carrying the profile and figures behind it; reassignment to a better-fitting tier SHALL require that the new tier describe the cell's measured shape independently of the verdict it produces
