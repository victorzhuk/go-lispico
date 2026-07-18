# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Gold-set gate corpus

go-lispico release CI SHALL run a committed gold set — rule-shaped fixtures with
independent golden expected results, plus benchmark cells over them — under both
execution modes, with no consumer checkout, no revision pin, and no cross-repo
secret. YAGEL SHALL own the corpus content: it exports the gold set from its
shipped Rules and refreshes it deliberately, so corpus drift is bounded by
explicit refresh rather than a checkout pin. A fixture without a golden SHALL be
an error.

#### Scenario: Candidate runs against the gold set

- **WHEN** the release job runs for a candidate
- **THEN** every gold-set fixture SHALL execute under both execution modes against its golden, self-contained in the candidate checkout

### Requirement: Correctness precedes timing

The release job SHALL run YAGEL's Behavior-golden suites under both execution
modes and both repositories' race-enabled test suites before any benchmark result
is considered. Race runs SHALL be separate from timed runs; no timing threshold
SHALL be evaluated under the race detector. Any correctness or race failure SHALL
fail the release regardless of benchmark outcomes.

#### Scenario: Golden failure blocks the release

- **WHEN** any Behavior golden fails under either execution mode
- **THEN** the release SHALL fail before threshold evaluation

### Requirement: Paired release run

The authoritative performance evidence SHALL be one hosted CI job interleaving
Evaluator and VM benchmark variants with fixed concurrency and benchtime and at
least ten samples per cell, compared per cell with benchstat against the cell's
committed Hot-cell tier and ADR 0008's thresholds. When benchstat is inconclusive,
the cell SHALL rerun once at doubled benchtime; if still inconclusive, improvement
cells fail and non-regression cells pass. Ordinary pull requests SHALL carry no
percentage gates.

#### Scenario: Inconclusive improvement cell fails

- **WHEN** an engine-sensitive cell remains benchstat-inconclusive after its doubled-benchtime rerun
- **THEN** the cell SHALL fail — the win was not demonstrated

#### Scenario: Inconclusive non-regression cell passes

- **WHEN** a non-regression cell remains benchstat-inconclusive after its doubled-benchtime rerun
- **THEN** the cell SHALL pass — no regression was demonstrated

### Requirement: One-shot authorization with a standing VM baseline

Passing the complete gate SHALL authorize YAGEL to enable `WithBytecode()`
directly — no user-facing execution flag, no shadow run; rollback is a normal code
or dependency revert. The improvement thresholds SHALL apply only to that first
authorization: subsequent releases SHALL compare the candidate VM against the
previous release's stored VM baseline as a non-regression check, so an Evaluator
improvement cannot fail the gate. Passing SHALL NOT change go-lispico's global
Engine default and SHALL end VM-specific optimization until a gate cell fails or
another consumer need is measured.

#### Scenario: Post-authorization release

- **WHEN** a release runs after the first authorization
- **THEN** each cell SHALL be judged against the stored VM baseline as non-regression, not against the same-release Evaluator improvement thresholds
