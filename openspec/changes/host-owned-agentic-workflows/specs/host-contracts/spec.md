## ADDED Requirements

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

### Requirement: Each contract declares its guarantees

Each persistence contract SHALL declare: atomicity of idempotency-key reservation, concurrency and ownership with a single owner at a time held by a lease plus scanner cleanup of in-flight markers, ordering scope, partition or shard key, and retention horizon. Each contract SHALL declare exclusive ownership within its ownership scope: a second owner SHALL NOT hold the same scope while a lease on it remains valid. A contract that omits one of these declarations SHALL NOT be accepted as a host contract.

#### Scenario: Undeclared guarantee is rejected

- **WHEN** a proposed contract omits its retention horizon
- **THEN** the contract SHALL NOT be accepted as a host contract

#### Scenario: Idempotency reservation is atomic

- **WHEN** two concurrent requests reserve the same idempotency key
- **THEN** exactly one reservation SHALL succeed and the other SHALL observe the existing reservation

#### Scenario: In-flight markers are reclaimed

- **WHEN** a marker left in flight by a failed holder outlives its lease
- **THEN** the scanner SHALL reclaim that marker

#### Scenario: Concurrent holder is refused while a lease is valid

- **WHEN** a holder owns a scope under a valid lease and a second owner attempts to claim the same scope
- **THEN** the second owner SHALL be refused until the lease expires or is released

#### Scenario: Ordering scope is bounded by the declaration

- **WHEN** a contract declares ordering within a partition key only
- **THEN** the host SHALL NOT rely on ordering across partition keys

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