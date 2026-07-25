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

#### Scenario: Inconclusive improvement cell fails

- **WHEN** an engine-sensitive cell remains benchstat-inconclusive after its doubled-benchtime rerun
- **THEN** the cell SHALL fail — the win was not demonstrated

#### Scenario: Inconclusive non-regression cell passes

- **WHEN** a non-regression cell remains benchstat-inconclusive after its doubled-benchtime rerun
- **THEN** the cell SHALL pass — no regression was demonstrated

#### Scenario: Changed run parameters invalidate the stored baseline

- **WHEN** the committed concurrency or benchtime differs from the value under which the stored baseline was captured
- **THEN** that baseline SHALL NOT be used for a non-regression verdict, because candidate and baseline were not measured under the same conditions
