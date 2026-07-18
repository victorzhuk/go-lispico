# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Gold-ref consumer checkout

go-lispico release CI SHALL check out YAGEL's `gold` ref — the blessed-release
pointer YAGEL advances to the revision it stands behind — and replace that
revision's go-lispico dependency with the release candidate. No revision pin
SHALL be recorded in this repo; YAGEL owns when the pointer moves. YAGEL owns the
corpus, goldens, benchmark cells, envelopes, and tiers; go-lispico SHALL NOT copy
those fixtures.

#### Scenario: Candidate runs against the blessed consumer

- **WHEN** the release job runs for a candidate
- **THEN** YAGEL's suites SHALL execute at the `gold` ref with the candidate substituted as its go-lispico dependency

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
