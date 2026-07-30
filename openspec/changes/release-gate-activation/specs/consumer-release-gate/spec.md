# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Gate execution is not merely possible

The release consumer gate SHALL execute against at least one real release and
publish a stored non-regression baseline before any future release's verdict
is treated as authoritative. A gate that exists as workflow definitions, gold
fixtures, and committed tiers but has never run SHALL NOT be cited as having
produced a verdict — "deferred to the release runner" SHALL mean deferred to
an identified future run, not deferred indefinitely because no run occurs.

Editing a gold-set fixture's source SHALL be treated as invalidating that
fixture's stored non-regression baseline, on the same footing as a
`GOMAXPROCS` or benchtime change: the next release's comparison for that cell
runs in first-authorization mode (or is explicitly noted as measuring a
changed fixture) rather than silently comparing against a baseline that
measured different source.

#### Scenario: A deferred verdict names a real future run

- **WHEN** a change's benchmark verdict is recorded as deferred to the release gate
- **THEN** the record SHALL identify the specific release or workflow run the verdict will be checked against, not merely name the gate mechanism

#### Scenario: A fixture-source edit invalidates its baseline

- **WHEN** a gold-set fixture's `.lisp` source changes between two releases
- **THEN** that fixture's cells SHALL NOT be compared against the prior release's stored baseline as an ordinary non-regression check; the comparison SHALL either run in first-authorization mode for that cell or be explicitly noted as measuring changed source
