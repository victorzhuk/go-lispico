## ADDED Requirements

### Requirement: The gate rejects candidate evidence it did not collect

Before any cell verdict is evaluated, the gate SHALL confirm that every cell declared in the tiers configuration has candidate samples and every metric that cell's tier requires. Empty candidate evidence, a missing configured cell, or a missing tier-required metric SHALL be a non-pass exit whose diagnostic names the missing cells. The gate SHALL NOT reach its pass exit when no comparison was performed.

#### Scenario: An empty candidate file is a non-pass

- **WHEN** the gate is given a candidate evidence file containing no samples
- **THEN** the gate SHALL exit non-pass and SHALL name the configured cells it received no evidence for

#### Scenario: A candidate missing one configured cell is a non-pass

- **WHEN** the gate is given a candidate evidence set that covers every configured cell except one
- **THEN** the gate SHALL exit non-pass and SHALL name the missing cell

#### Scenario: A cell missing a tier-required metric is a non-pass

- **WHEN** a candidate cell carries a time measurement but its tier also requires an allocation measurement, and no allocation measurement is present
- **THEN** the gate SHALL exit non-pass and SHALL name that cell and the missing metric

#### Scenario: A missing runner identity does not become a pass

- **WHEN** candidate evidence carries no runner identity and therefore cannot be paired with the baseline
- **THEN** the gate SHALL exit with the documented unconfigurable or rerun-needed code and SHALL NOT exit pass

#### Scenario: Complete paired evidence still passes

- **WHEN** the gate is given candidate evidence covering every configured cell with every metric its tier requires, paired with a stored baseline from the same runner
- **THEN** the gate SHALL evaluate cell verdicts and exit according to them

### Requirement: Incomplete-evidence rejection preserves the gate exit contract

Rejecting incomplete candidate evidence SHALL NOT introduce an exit code outside 0 pass, 1 fail, 2 rerun needed, and 3 unconfigurable. A configured cell whose absence is a baseline condition rather than a candidate-evidence condition SHALL remain eligible for the first-authorization path.

#### Scenario: An empty candidate set exits fail or unconfigurable

- **WHEN** the gate is given candidate evidence containing no samples
- **THEN** it SHALL exit with the fail or unconfigurable code and SHALL NOT exit pass or rerun needed

#### Scenario: A new baseline cell still takes the first-authorization path

- **WHEN** a configured cell exists in the tiers configuration and has no stored baseline, and the candidate evidence for that cell is otherwise complete
- **THEN** the gate SHALL record the cell through the first-authorization path rather than failing it as incomplete candidate evidence