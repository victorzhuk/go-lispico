## MODIFIED Requirements

### Requirement: A baseline comparison states the hardware it was measured on

The stored VM baseline SHALL carry the identity of the runner that produced it,
and the gate SHALL compare that identity against the candidate run's before
drawing a latency conclusion from it. Where the two differ, the gate SHALL report
every latency cell judged against that baseline as inconclusive, naming the two
runners, and SHALL NOT convert the difference into a pass or a failure.

Allocation counts SHALL stay enforced across differing runners. They are exact
per-operation integers in the benchmark output, carrying no iteration-count term,
so a change of machine cannot move them.

Allocated bytes SHALL NOT be judged across differing runner identities. `B/op` is
total bytes divided by the iteration count the benchmark reached, and that count
is a property of machine speed: the same code measured on a slower machine
amortises a fixed per-run allocation over fewer iterations and reports a higher
per-operation figure. Where the identities differ, the gate SHALL report the bytes
axis inconclusive, naming both runners, and SHALL NOT convert the difference into
a pass or a failure. Where the identities match, allocated bytes SHALL be enforced
against the cell's stated allowance exactly as before.

The gate SHALL NOT strip or normalise the configuration lines that make two
benchmark runs incomparable in order to obtain a comparison. A tool that refuses
to pair samples across configurations is reporting a real limit on the evidence,
and the gate SHALL treat that refusal as a verdict input rather than an obstacle.

#### Scenario: The candidate runs on different hardware from the baseline

- **WHEN** the stored baseline records one runner identity and the candidate run records another
- **THEN** the latency cells judged against that baseline SHALL be reported inconclusive with both runner identities named, and the release SHALL still be gated on correctness, the race suite and allocation counts

#### Scenario: Allocated bytes move across a change of runner with the allocation count unchanged

- **WHEN** the stored baseline and the candidate record different runner identities, and a cell's allocation count is identical on both arms while its `B/op` differs
- **THEN** that cell's bytes verdict SHALL be reported inconclusive naming both runners, and SHALL NOT fail the release

#### Scenario: Allocated bytes are enforced when the runners match

- **WHEN** the stored baseline and the candidate record the same runner identity, and a cell's allocated bytes exceed its stated allowance
- **THEN** that cell SHALL fail, exactly as it does today

#### Scenario: An allocation count regresses across a change of runner

- **WHEN** the stored baseline and the candidate record different runner identities, and a cell's allocation count increases
- **THEN** that cell SHALL fail, because the allocation count carries no iteration-count term and the change of machine cannot explain it

#### Scenario: An incomparable pair is never forced into a comparison

- **WHEN** the comparison tool declines to pair the baseline and candidate samples because their recorded configurations differ
- **THEN** the gate SHALL fail with that reason stated, and SHALL NOT retry by removing the configuration lines that caused the refusal
