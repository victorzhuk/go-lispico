## ADDED Requirements

### Requirement: The permission layer declares an enforced control set per capability

Every host capability that reaches the outside world SHALL declare the controls its registration enforces. The host SHALL reject a capability registered without a declaration rather than admit it, and the declaration SHALL be evaluated by the host, never by workflow code.

#### Scenario: Capability without a declaration is rejected

- **WHEN** a host registers a capability that reaches the outside world and declares no control set
- **THEN** the host SHALL reject the registration rather than admit the capability

#### Scenario: Workflow using a rejected capability does not start

- **WHEN** a workflow references a capability whose control set was rejected
- **THEN** validation SHALL reject the workflow before the first node executes

### Requirement: Filesystem capability is confined to a canonical root

A filesystem capability SHALL canonicalize its root before the first operation, SHALL reject a path that resolves outside the root through a symbolic link, a hard link, or an intermediate directory component, and SHALL contain the home directory. Containment SHALL be decided on the resolved path and SHALL NOT be decided on a lexical prefix of the requested path.

#### Scenario: Symlink escape is refused

- **WHEN** a path inside the root resolves through a symlink to a target outside the root
- **THEN** the capability SHALL refuse the operation

#### Scenario: Containment is decided after resolution

- **WHEN** a requested path is lexically inside the root but resolves outside it
- **THEN** the capability SHALL refuse the operation

#### Scenario: Home directory is contained

- **WHEN** a workflow addresses a path under the host home directory and the root does not contain it
- **THEN** the capability SHALL refuse the operation

### Requirement: Network capability declares a strict host allowlist

A network capability SHALL declare its allowed hosts. A request to a host outside the allowlist SHALL be refused without an interactive prompt: for an unlisted host the default answer is denial, not a question.

#### Scenario: Unlisted host is refused without prompting

- **WHEN** a workflow requests a host outside the declared allowlist
- **THEN** the capability SHALL refuse the request and SHALL NOT prompt for approval

#### Scenario: Listed host is permitted

- **WHEN** a workflow requests a host inside the declared allowlist
- **THEN** the capability SHALL permit the request under its declared controls

### Requirement: Untrusted input does not combine with secrets and outbound mutation

A stage SHALL declare whether it accepts untrusted input, whether it has secret access, and whether it allows outbound state change. Validation SHALL reject a stage that declares all three at once.

#### Scenario: Three-capability combination is rejected

- **WHEN** a stage declares untrusted input, secret access, and outbound state change together
- **THEN** validation SHALL reject the stage before execution

#### Scenario: Untrusted stage without the other two is admitted

- **WHEN** a stage declares untrusted input and neither secret access nor outbound state change
- **THEN** validation SHALL admit the stage

#### Scenario: Secret-holding stage is admitted

- **WHEN** a stage declares secret access, accepts no untrusted input, and allows no outbound state change
- **THEN** validation SHALL admit the stage

### Requirement: A tainted value stays tainted across persistence and replay

A value derived from untrusted input SHALL carry a taint label in the state channel that holds it. The label SHALL survive checkpoint, restart, and replay. Labels of merged values SHALL combine so that aggregation cannot launder a tainted input into a trusted value. Clearing a label SHALL require an explicit declared decontamination gate and SHALL NOT happen implicitly.

#### Scenario: Taint survives a restart

- **WHEN** a workflow checkpoints a state channel holding a tainted value and the host restarts
- **THEN** the value SHALL still carry its taint label after recovery

#### Scenario: Aggregation does not launder taint

- **WHEN** a tainted value and a trusted value are merged into one channel
- **THEN** the merged value SHALL be tainted

#### Scenario: Taint is cleared only through the declared gate

- **WHEN** a value is tainted and no decontamination gate has run for it
- **THEN** the host SHALL keep the value tainted
