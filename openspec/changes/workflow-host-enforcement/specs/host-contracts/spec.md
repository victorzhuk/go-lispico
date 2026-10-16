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

### Requirement: Every workflow declares operational budgets

A workflow SHALL declare a maximum step count, a maximum token count, a maximum wallclock, a maximum cost, and a concurrency cap. Validation SHALL reject a workflow that omits any of them.

#### Scenario: Workflow without a step budget is rejected

- **WHEN** a workflow declares no maximum step count
- **THEN** validation SHALL reject the workflow before execution

#### Scenario: Workflow without a concurrency cap is rejected

- **WHEN** a workflow declares its other budgets without a concurrency cap
- **THEN** validation SHALL reject the workflow

### Requirement: A budget breach has a declared landing state

A workflow SHALL declare what happens when a budget is exceeded. A breach SHALL emit its declared event, SHALL leave the run in a declared state, and SHALL NOT leave a superstep partially applied. A breach that falls between the plan phase and the update phase of one superstep SHALL be resolved before the run leaves that superstep.

#### Scenario: Breach emits its event and lands in a declared state

- **WHEN** a workflow exceeds its declared maximum cost
- **THEN** the host SHALL emit the declared breach event and place the run in the state the workflow declared

#### Scenario: No partially applied superstep survives a breach

- **WHEN** a budget is exceeded while a superstep is between its plan and update phases
- **THEN** the run SHALL NOT leave that superstep with a partial set of channel updates applied

### Requirement: Execution history is bounded by rotation

A long-lived workflow SHALL rotate its execution history at a declared snapshot frequency and SHALL carry forward the state it declares as preserved across rotation. The host SHALL refuse unbounded growth of an execution history.

#### Scenario: History rotation preserves carried state

- **WHEN** a workflow's history reaches its declared rotation point
- **THEN** the host SHALL rotate the history and preserve the state the workflow declared as carried forward

#### Scenario: Unbounded history is refused

- **WHEN** a workflow declares no history bound and runs past the host's history ceiling
- **THEN** the host SHALL refuse further growth rather than continue expanding the history

### Requirement: A declared join releases on partial failure under its declared policy

A join SHALL declare how it treats a failed branch. A failed branch SHALL fill its slot as failed, and the join SHALL either release with the collected results or fail, per its declared policy. A failed branch SHALL NOT discard the results of branches that completed.

#### Scenario: Completed siblings survive a failed branch

- **WHEN** one branch of a declared join fails while another branch completes
- **THEN** the join SHALL retain the completed branch's result

#### Scenario: Join releases under its declared policy

- **WHEN** a join declaring release-with-partial-results has one failed slot and one filled slot
- **THEN** the join SHALL release with the filled slot's result and the failed slot marked failed

#### Scenario: Join fails under its declared policy

- **WHEN** a join declaring fail-on-branch-error has a failed slot
- **THEN** the join SHALL fail rather than release
