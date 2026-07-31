# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Classification profile and stored baseline are distinct

The gate SHALL keep two measurement artifacts apart, because they answer
different questions and follow different rules.

The **classification profile** is a hosted paired Evaluator/VM run at the
gate's committed fixed parameters, checked into this repository alongside the
run id, ref, and date that produced it. It is what ADR 0008's Thresholds
section means by "a checked-in baseline profile", and it is the only artifact
that licenses a cell's tier in `internal/perfgate/tiers.json`. A
`workflow_dispatch` run SHALL be a valid source: it exercises the same gold
set, race suite, paired bench, and first-authorization `perfgate` evaluation as
a release-triggered run, and it publishes the same evidence artifact. A tier
assigned from a developer box SHALL NOT satisfy this requirement.

The **stored non-regression baseline** is the `bench-vm.txt` asset a passing
release stores on itself, and it answers only the next release's
non-regression comparison. It SHALL be produced by the workflow on a release
whose verdict passed, never hand-uploaded, and SHALL NOT be treated as the
artifact that licenses a tier. The absence of a stored baseline SHALL NOT be
read as the absence of a classification profile.

A classification profile SHALL be checked in before the release whose
candidate results its tiers judge, per ADR 0008's ordering rule. A profile
taken from the same tree the next release is cut from satisfies that rule; what
it SHALL NOT be used for is adjusting a tier after that release's own verdict
is known.

#### Scenario: A dispatch run licenses a tier

- **WHEN** a tier in `internal/perfgate/tiers.json` is assigned or changed
- **THEN** a hosted run at the gate's committed fixed parameters SHALL have produced the profile justifying it, and that profile SHALL be checked in with its run id, ref, and date; the run MAY be `workflow_dispatch`-triggered

#### Scenario: A failed verdict does not invalidate its measurements

- **WHEN** a hosted run's verdict fails because a cell's committed tier does not match what the run measured
- **THEN** the run's paired benchmark output SHALL remain usable as a classification profile, since a verdict is a judgment applied to the measurements rather than a property of them

#### Scenario: A missing baseline asset is not a missing profile

- **WHEN** no release carries a stored `bench-vm.txt`
- **THEN** the next release SHALL run as a first authorization, and the absence SHALL NOT block committing or correcting a tier that a checked-in classification profile justifies

#### Scenario: Tiers are not fitted to a judged release

- **WHEN** a release's own verdict is known and a cell of that release failed
- **THEN** that release's candidate results SHALL NOT be used to re-fit the failing cell's tier; a fresh profile SHALL be produced and checked in before the next release's candidate results exist
