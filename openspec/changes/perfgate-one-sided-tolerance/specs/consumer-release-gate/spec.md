# consumer-release-gate — delta

## MODIFIED Requirements

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

