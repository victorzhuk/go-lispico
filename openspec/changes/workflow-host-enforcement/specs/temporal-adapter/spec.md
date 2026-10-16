## ADDED Requirements

### Requirement: An approval Signal carries the digest the graph layer recorded

The approval Signal SHALL carry the digest of the reviewed patch that the graph-layer approval binding recorded. The adapter SHALL reject a Signal whose digest does not match the digest the waiting approval recorded, and SHALL treat a Signal that carries no digest as authorizing nothing.

#### Scenario: Matching digest authorizes the patch

- **WHEN** an approval Signal arrives for the waiting approval the adapter holds a record of, carrying that record's digest and that record's approved outcome
- **THEN** the adapter SHALL record the approved outcome and authorize that patch

#### Scenario: Mismatched digest is rejected

- **WHEN** an approval Signal arrives carrying a digest different from the one the waiting approval recorded
- **THEN** the adapter SHALL reject the approval and SHALL NOT authorize any patch

#### Scenario: Digest-less approval authorizes nothing

- **WHEN** an approval Signal arrives carrying no digest
- **THEN** the adapter SHALL record no approval and SHALL NOT authorize a patch

#### Scenario: Replayed stale approval authorizes nothing

- **WHEN** an approval Signal is delivered to a waiting approval whose recorded approval identity is not the identity that Signal carries, so the Signal matches no approval record the receiving run holds
- **THEN** the adapter SHALL record no approval for that waiting approval and SHALL NOT authorize any patch, whether or not the digest it carries matches a recorded digest

_Informative note (non-normative): the digest binds an approval to patch content and to the recorded outcome, as `openspec/specs/workflow-graph/spec.md` states for `Approval binds to the digest of the reviewed patch`. Two runs reviewing identical patch content therefore produce identical digests, so the digest alone cannot distinguish an approval belonging to another run. Scenarios above and here differ in their input, not only in their outcome: the scenarios above take a Signal delivered to the approval that holds the matching recorded digest, while this scenario takes a Signal that matches no approval record of the receiving run._