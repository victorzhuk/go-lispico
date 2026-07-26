# consumer-release-gate Specification

## Purpose
TBD - created by archiving change release-consumer-gate. Update Purpose after archive.
## Requirements
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

### Requirement: Correctness precedes timing

The release job SHALL run every gold-set fixture under both execution modes
against its golden, and this repo's race-enabled test suite, before any benchmark
result is considered. Race runs SHALL be separate from timed runs; no timing
threshold SHALL be evaluated under the race detector. Any correctness or race
failure SHALL fail the release regardless of benchmark outcomes.

#### Scenario: Golden failure blocks the release

- **WHEN** any gold-set fixture fails its golden under either execution mode
- **THEN** the release SHALL fail before threshold evaluation

### Requirement: Paired release run

The authoritative performance evidence SHALL be one hosted CI job interleaving
Evaluator and VM benchmark variants with fixed concurrency and benchtime and at
least ten samples per cell, compared per cell with benchstat against the cell's
committed Hot-cell tier and ADR 0008's thresholds. The fixed values SHALL be
committed in the workflow rather than left to the runner: concurrency
`GOMAXPROCS=2`, chosen so the run behaves identically on a two- or four-vCPU
hosted runner and a runner-spec change cannot silently shift stored baselines,
and benchtime `200ms`, which yields enough iterations per sample on a
microsecond-scale fixture that per-sample variance sits well below the
non-regression tolerance. Changing either value SHALL be treated as
invalidating every stored baseline for non-regression comparison, since
candidate and baseline are then no longer measured under the same conditions.
When any cell is benchstat-inconclusive, the whole paired run SHALL rerun once
at doubled benchtime and every cell SHALL be re-judged from the rerun data —
doubled benchtime is stronger evidence for every cell, so no first-attempt
verdict is frozen; a cell still inconclusive after the rerun fails if it is an
improvement cell and passes if it is a non-regression cell. Ordinary pull
requests SHALL carry no percentage gates.

A tier's percentage bound SHALL be applied as a bound on regression when the
comparison is against a stored baseline from a previous release: a candidate
faster than its baseline SHALL pass at any margin, because failing a release for
making the engine faster is the outcome ADR 0008 rejects when it declines a
standing improvement gate. Byte and allocation-count checks are unaffected —
they were already non-increasing, and a faster candidate that allocates more
SHALL still fail. The bound SHALL remain two-sided in the two cases where the
paired runs are expected to measure the same cost rather than two releases: a
data/output-dominated cell under first authorization, where the runs are the two
evaluators of one commit and the cost is mode-invariant by classification, and a
concurrent cell, whose timed figure may be a throughput measure whose sign
convention is not yet stated.

#### Scenario: A faster candidate passes a non-regression cell

- **WHEN** a non-regression cell's latency improves beyond its tier's percentage bound, with bytes and allocation count not increasing
- **THEN** the cell SHALL pass, at any improvement margin

#### Scenario: A faster candidate that allocates more still fails

- **WHEN** a non-regression cell's latency improves but its allocated bytes or allocation count increase
- **THEN** the cell SHALL fail on the byte or allocation check, which the one-sided latency bound does not relax

#### Scenario: Mode-invariant cost moving either way is a finding

- **WHEN** a data/output-dominated cell is judged under first authorization, where the two runs are the Evaluator and VM variants of one commit, and its latency moves beyond the bound in either direction
- **THEN** the cell SHALL fail, because a cost classified as mode-invariant is not expected to depend on the evaluator

#### Scenario: Inconclusive improvement cell fails

- **WHEN** an engine-sensitive cell remains benchstat-inconclusive after its doubled-benchtime rerun
- **THEN** the cell SHALL fail — the win was not demonstrated

#### Scenario: Inconclusive non-regression cell passes

- **WHEN** a non-regression cell remains benchstat-inconclusive after its doubled-benchtime rerun
- **THEN** the cell SHALL pass — no regression was demonstrated

#### Scenario: Changed run parameters invalidate the stored baseline

- **WHEN** the committed concurrency or benchtime differs from the value under which the stored baseline was captured
- **THEN** that baseline SHALL NOT be used for a non-regression verdict, because candidate and baseline were not measured under the same conditions

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

