# temporal-adapter Specification

## Purpose
Define the engine-neutral durable-workflow contract and Temporal as its first
adapter target. These requirements are forward design: no package in this
repository implements a Temporal adapter or any workflow host. The boundary is
proposed in `docs/adr/0017-temporal-first-adapter.md`, which is
`status: proposed`. Read a requirement here as the adapter contract a future
host owes, not as a capability the shipped kernel provides.

## Requirements

### Requirement: Temporal is the first adapter target behind an engine-neutral contract

Temporal SHALL be the first approved adapter target. The graph contracts the adapter implements SHALL stay engine-neutral: a graph definition SHALL NOT name a Temporal type, workflow ID, task queue, or Temporal-specific field. The adapter SHALL live in its own module with its own `go.mod`, never inside `core/`, `runtime/`, or `internal/`, and the root module baseline SHALL remain unchanged.

#### Scenario: Graph definition stays engine-neutral

- **WHEN** a Lisp-produced workflow graph is defined
- **THEN** the definition SHALL contain no Temporal-specific field, workflow ID, or task queue

#### Scenario: Second engine is admissible

- **WHEN** a different durable engine is later targeted
- **THEN** the engine-neutral graph contracts SHALL admit it without changing the graph definition

### Requirement: Model and tool calls run as Activities

Every model call and every tool call SHALL run as an Activity. Workflow code, graph interpretation, and Lisp graph construction SHALL be deterministic and free of side effects. A workflow function SHALL NOT call a model, a tool, a clock, a random source, or a host-registered primitive directly, and graph definition SHALL NOT read a clock, generate randomness, or call a model.

#### Scenario: Model call runs as an Activity

- **WHEN** a workflow node performs a model call
- **THEN** the call SHALL execute as an Activity and the workflow code SHALL return only its declared result

#### Scenario: Non-deterministic workflow code is refused

- **WHEN** workflow code reads a clock or a random source
- **THEN** the adapter SHALL refuse that workflow code

#### Scenario: Prohibited effect during graph definition is refused

- **WHEN** Lisp graph definition reads a clock, generates randomness, or calls a model
- **THEN** the adapter SHALL reject the graph definition before any node executes

### Requirement: Human approval is a durable Signal with an explicit deadline outcome

A human approval step SHALL be implemented as a durable Signal wait with an explicit deadline. The approval outcome SHALL be one of: approved, rejected, or deadline-expired. A deadline expiry SHALL be a recorded outcome and SHALL NOT be reported as a pending wait.

#### Scenario: Approval arrives before the deadline

- **WHEN** a Signal carrying an approval arrives before the declared deadline
- **THEN** the workflow SHALL record the approved outcome and continue

#### Scenario: Deadline expiry is an outcome

- **WHEN** no Signal arrives before the declared deadline
- **THEN** the workflow SHALL record the deadline-expired outcome and SHALL NOT remain pending

#### Scenario: Rejection is recorded

- **WHEN** a Signal carrying a rejection arrives
- **THEN** the workflow SHALL record the rejected outcome and SHALL NOT apply the patch

### Requirement: Restart during the approval wait preserves pending state and completed work

A restart of the host or the worker while the workflow waits for approval SHALL preserve the pending approval state and every completed Activity result. After recovery, the workflow SHALL resume at the approval wait and SHALL NOT re-run completed Activities with a different result.

#### Scenario: Restart during approval wait

- **WHEN** the process restarts while the workflow waits for an approval Signal
- **THEN** the recovered workflow SHALL resume the same pending approval wait

#### Scenario: Completed work survives restart

- **WHEN** Activities completed before the restart are replayed after recovery
- **THEN** each SHALL return its recorded result and SHALL NOT re-execute its effect

### Requirement: Ambiguous external effects require host idempotency or reconciliation

An external effect whose outcome is unknown SHALL NOT be covered by an engine promise. The host SHALL provide idempotency or reconciliation for such an effect. An exactly-once delivery SHALL NOT be claimed from the engine.

#### Scenario: Unknown patch outcome is not blind-retried

- **WHEN** patch execution fails and the host cannot determine whether the patch was applied
- **THEN** the host SHALL reconcile or apply its idempotency rule before any retry, and SHALL NOT retry the patch blindly

#### Scenario: Ambiguous effect surfaces as unknown outcome plus reconciliation

- **WHEN** an external effect has an unknown outcome
- **THEN** the effect SHALL surface as an unknown outcome plus host idempotency or reconciliation, never as a silent retry that may double-apply

### Requirement: No engine exactly-once guarantee is assumed or promised

The system SHALL NOT assume or promise an engine exactly-once guarantee. Exactly-once SHALL NOT be claimed as derived from the engine; a claim of exactly-once grounded in host-provided idempotency is not an engine claim. Documentation, host code, and host-facing messages SHALL state that ambiguous outcomes are resolved by host idempotency or reconciliation, not by an engine claim.

#### Scenario: No engine-derived exactly-once claim in host-facing text

- **WHEN** host documentation describes effect delivery
- **THEN** it SHALL NOT claim exactly-once as a guarantee derived from the engine

#### Scenario: Unknown outcome is reconciled

- **WHEN** the engine reports an unknown effect outcome
- **THEN** the host SHALL reconcile rather than assume success or assume failure

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
