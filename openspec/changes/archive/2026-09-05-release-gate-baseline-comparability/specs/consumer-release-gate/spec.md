## ADDED Requirements

### Requirement: A baseline comparison states the hardware it was measured on

The stored VM baseline SHALL carry the identity of the runner that produced it,
and the gate SHALL compare that identity against the candidate run's before
drawing a latency conclusion from it. Where the two differ, the gate SHALL report
every latency cell judged against that baseline as inconclusive, naming the two
runners, and SHALL NOT convert the difference into a pass or a failure.

Allocation counts and allocated bytes SHALL stay enforced across differing
runners: they are properties of the program, not of the machine.

The gate SHALL NOT strip or normalise the configuration lines that make two
benchmark runs incomparable in order to obtain a comparison. A tool that refuses
to pair samples across configurations is reporting a real limit on the evidence,
and the gate SHALL treat that refusal as a verdict input rather than an obstacle.

#### Scenario: The candidate runs on different hardware from the baseline

- **WHEN** a post-authorization release runs on a machine whose CPU identity differs from the one recorded with the stored baseline
- **THEN** the latency cells judged against that baseline SHALL be reported inconclusive with both runner identities named, and the release SHALL still be gated on correctness, the race suite, allocation counts and allocated bytes

#### Scenario: An incomparable pair is never forced into a comparison

- **WHEN** the comparison tool declines to pair the baseline and candidate samples because their recorded configurations differ
- **THEN** the gate SHALL fail with that reason stated, and SHALL NOT retry by removing the configuration lines that caused the refusal

### Requirement: Every gate cell states its bytes allowance

Allocated bytes per operation are reported as a total divided by an iteration
count, so repeated runs of identical code differ by a few bytes. Every cell SHALL
therefore state a bytes allowance, and the gate SHALL fail with a missing-config
error when a cell it is asked to judge has none, rather than reading the absence
as an allowance of zero.

An allowance SHALL be justified by observed sampling spread on that cell, and
SHALL NOT be widened to admit a measured regression. Allocation counts are exact
in the same output and SHALL keep a zero allowance.

#### Scenario: A cell without a stated allowance

- **WHEN** the gate judges a cell for which `tiers.json` states no bytes allowance
- **THEN** the gate SHALL fail naming that cell and the missing allowance, and SHALL NOT judge it against an implicit zero

#### Scenario: Sampling spread does not fail a release

- **WHEN** a cell's allocated bytes differ from the baseline by less than its stated allowance, and its allocation count is unchanged
- **THEN** the cell SHALL pass on the bytes axis

### Requirement: A pre-flight resolves the mode its release will use

The gate SHALL resolve its comparison mode from the repository's stored
baselines, not from the event that triggered the run, so that a manually
dispatched run and the release it precedes judge the candidate by the same rule.
A dispatched run SHALL NOT publish a baseline or upload a release asset.

#### Scenario: Dispatch and release agree on the rule

- **WHEN** the gate is dispatched manually against the same tree a release will be cut from, and a stored baseline exists
- **THEN** the dispatched run SHALL judge the candidate as non-regression against that baseline, reaching the same per-cell verdicts the release run reaches

### Requirement: Baseline resolution distinguishes absence from failure

The gate SHALL treat a failure to enumerate or download release assets as a
failure, not as the absence of a baseline. Falling back to first-authorization
thresholds SHALL happen only when the repository is known to hold no baseline.

#### Scenario: The release API is unavailable

- **WHEN** enumerating or downloading the stored baseline fails for any reason other than the baseline not existing
- **THEN** the gate SHALL fail naming that failure, and SHALL NOT judge the candidate against first-authorization improvement thresholds
