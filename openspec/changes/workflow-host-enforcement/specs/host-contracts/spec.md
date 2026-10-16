## ADDED Requirements

### Requirement: The permission layer declares an enforced control set per capability

Every host capability that reaches the outside world SHALL declare the controls its registration enforces. The host SHALL reject a capability registered without a declaration rather than admit it, and the declaration SHALL be evaluated by the host, never by workflow code.

#### Scenario: Capability without a declaration is rejected

- **WHEN** a host registers a capability that reaches the outside world and declares no control set
- **THEN** the host SHALL reject the registration rather than admit the capability

#### Scenario: Workflow using a rejected capability does not start

- **WHEN** a workflow references a capability whose control set was rejected
- **THEN** validation SHALL reject the workflow before the first node executes
