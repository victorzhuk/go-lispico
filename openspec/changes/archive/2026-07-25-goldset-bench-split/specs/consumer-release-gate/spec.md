# consumer-release-gate — delta

## MODIFIED Requirements

### Requirement: Gold-set gate corpus

go-lispico release CI SHALL run a committed gold set — rule-shaped fixtures with
hand-derived golden expected results, plus benchmark cells over them — under both
execution modes, with no consumer checkout, no revision pin, and no cross-repo
secret. The corpus, goldens, and tier assignments SHALL be owned by this repo,
independent of any consumer; goldens SHALL be derived from the language
contract, never captured from either engine. A fixture without a golden SHALL be
an error.

Parsing SHALL carry its own benchmark cells over the same fixtures, with their own
committed tier assignments. Reader cost is real — embedders pay it on every load and
hot reload — and without dedicated cells a reader regression is invisible to the gate.
Dedicated cells additionally quantify the parse component of the evaluation cells,
which measure parsing and evaluation together because the public embedding API accepts
source rather than pre-parsed forms. That component is identical under both execution
modes, so it compresses the mode difference an evaluation cell can express; a tier
assignment SHALL be read with that in mind rather than as a pure evaluator measurement.

#### Scenario: Candidate runs against the gold set

- **WHEN** the release job runs for a candidate
- **THEN** every gold-set fixture SHALL execute under both execution modes against its golden, self-contained in the candidate checkout

#### Scenario: Parsing is measured on its own

- **WHEN** the gold-set benchmark cells are run
- **THEN** a parse cell SHALL measure the reader alone over each fixture's source, carrying its own committed tier, so a reader regression is detectable and the parse component of the evaluation cells is quantifiable

#### Scenario: Parse cells do not vary by execution mode

- **WHEN** a parse cell is run under both execution modes
- **THEN** the results SHALL show no mode difference, since the reader is shared; a difference SHALL be treated as a finding about the corpus rather than a performance verdict
