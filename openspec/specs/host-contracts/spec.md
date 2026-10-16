# host-contracts Specification

## Purpose
TBD - created by archiving change host-owned-agentic-workflows. Update Purpose after archive.

## Requirements

### Requirement: Persistence contracts are separated by consumer concern

The host SHALL define separate narrow persistence contracts per consumer: one for application-data operations, one for execution-history operations, and one for optional effect receipts.

#### Scenario: Each consumer implements only its own contract

- **WHEN** a host registers an application-data store that provides no execution-history operations
- **THEN** the store SHALL satisfy the application-data contract and SHALL NOT be required to implement the execution-history contract

### Requirement: No combined storage interface is imposed

A combined generic storage interface SHALL NOT be required, and no consumer SHALL be required to implement a method it does not use. Adding a persistence concern SHALL add a new contract, not widen an existing interface.

#### Scenario: No generic storage interface is imposed

- **WHEN** host contracts are declared
- **THEN** each SHALL expose only the operations its own consumer needs

### Requirement: A receipt contract is declared only for ambiguous external effects

A workflow that makes an ambiguous external effect SHALL declare a receipt contract, and one that makes no ambiguous external effect SHALL NOT be required to declare one.

#### Scenario: Receipt contract is declared only for ambiguous effects

- **WHEN** a workflow makes no ambiguous external effect and declares no receipt contract
- **THEN** the host SHALL accept the workflow without a receipt contract

### Requirement: Each contract declares its declared guarantee set

Each persistence contract SHALL declare: atomicity of idempotency-key reservation, concurrency and ownership, ordering scope, partition or shard key, and retention horizon.

#### Scenario: Idempotency reservation is atomic

- **WHEN** two concurrent requests reserve the same idempotency key
- **THEN** exactly one reservation SHALL succeed and the other SHALL observe the existing reservation

#### Scenario: Ordering scope is bounded by the declaration

- **WHEN** a contract declares ordering within a partition key only
- **THEN** the host SHALL NOT rely on ordering across partition keys

### Requirement: A contract declares exclusive ownership of its scope

Each contract SHALL declare exclusive ownership within its ownership scope: a single owner at a time held by a lease plus scanner cleanup of in-flight markers. A second owner SHALL NOT hold the same scope while a lease on it remains valid.

#### Scenario: Concurrent holder is refused while a lease is valid

- **WHEN** a holder owns a scope under a valid lease and a second owner attempts to claim the same scope
- **THEN** the second owner SHALL be refused until the lease expires or is released

#### Scenario: Ownership is exclusive while a lease is valid

- **WHEN** a lease on an ownership scope remains valid
- **THEN** no second owner SHALL hold that scope

#### Scenario: Sweeper reclaims in-flight markers of a crashed owner

- **WHEN** a marker left in flight by a failed holder outlives its lease
- **THEN** the scanner SHALL reclaim that marker

### Requirement: A contract that omits a declaration is rejected

A contract that omits a declaration required by "Each contract declares its declared guarantee set" or "A contract declares exclusive ownership of its scope" SHALL NOT be accepted as a host contract.

#### Scenario: Omitted declaration is rejected

- **WHEN** a proposed contract omits its retention horizon
- **THEN** the contract SHALL NOT be accepted as a host contract

### Requirement: A backend satisfies the declared guarantees or rejects the configuration

A sharded or distributed backend SHALL either satisfy every guarantee its contract declares or reject the configuration. Interface sameness alone SHALL NOT be treated as conformance. An unsupported guarantee SHALL NOT silently degrade into a weaker behavior.

#### Scenario: Unsupported guarantee rejects the configuration

- **WHEN** a configured backend cannot guarantee atomic idempotency-key reservation
- **THEN** startup SHALL reject the configuration rather than continue without the guarantee

#### Scenario: Silent degradation is refused

- **WHEN** a backend supports an operation only with weaker semantics than the contract declares
- **THEN** the configuration SHALL be rejected

#### Scenario: Matching interface names do not establish conformance

- **WHEN** a backend declares the same method set as the contract but differs in the declared guarantees
- **THEN** the host SHALL NOT accept it as conforming on interface sameness alone

### Requirement: Local-to-distributed substitution preserves observable behavior

Substituting a distributed backend for a local one SHALL preserve the observable workflow behavior under the declared guarantees. Observable behavior covers: which nodes run, which joins release, which loops stop at their bound, and which approvals authorize which patch digest.

#### Scenario: Substitution preserves branch and join behavior

- **WHEN** a workflow validated against a local backend runs against a distributed backend that satisfies the declared guarantees
- **THEN** branch advance, join release, loop bounds, and approval outcomes SHALL match the local run

#### Scenario: Violated guarantee is visible

- **WHEN** the distributed backend fails a declared guarantee during execution
- **THEN** the host SHALL surface the failure rather than continue with different observable behavior

### Requirement: Workflow definitions are independent of storage clients and local paths

A workflow definition SHALL NOT reference a concrete storage client, a concrete storage backend type, or a local filesystem path. Definitions address host contracts only, so the same definition is admissible against any host backend that satisfies the declared guarantees, and host storage substitution SHALL NOT be treated as replacing the durable engine's own history backend.

#### Scenario: Definition naming a local path is rejected

- **WHEN** a workflow definition references a local filesystem path
- **THEN** validation SHALL reject the definition before execution

#### Scenario: Definition naming a storage client is rejected

- **WHEN** a workflow definition references a concrete storage client or backend type
- **THEN** validation SHALL reject the definition before execution

#### Scenario: Contract-addressed definition admits backend substitution

- **WHEN** a workflow validated against one host backend runs against a substituted backend that satisfies the declared guarantees
- **THEN** the workflow's observable behavior SHALL be unchanged

### Requirement: Host storage substitution does not replace engine history

Host storage substitution SHALL NOT be represented as replacing the durable engine's own history backend. The engine retains its history backend; the host contracts cover application data, execution history exposed by the host, and effect receipts only.

#### Scenario: Engine history stays engine-owned

- **WHEN** the host swaps its application-data backend
- **THEN** the durable engine's own history backend SHALL be unaffected

#### Scenario: Substituting a host backend is not engine migration

- **WHEN** a host backend is replaced with a distributed one
- **THEN** the system SHALL NOT claim the engine's history backend moved with it

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

### Requirement: Correlation declares cardinality and deadline precedence

A correlation SHALL declare its cardinality, its deadline, and which outcome takes precedence when the deadline expires before the correlation completes. A correlation that declares no cardinality SHALL be treated as awaiting exactly one event.

#### Scenario: Deadline fires before cardinality is met

- **WHEN** a correlation declared to await three events receives two before its deadline
- **THEN** the correlation SHALL take the outcome its declaration gives the deadline

#### Scenario: Cardinality is met before the deadline

- **WHEN** a correlation declared to await three events receives three before its deadline
- **THEN** the correlation SHALL complete and its deadline SHALL NOT fire

#### Scenario: Absent cardinality means one event

- **WHEN** a correlation declares no cardinality
- **THEN** the host SHALL treat it as awaiting exactly one event
