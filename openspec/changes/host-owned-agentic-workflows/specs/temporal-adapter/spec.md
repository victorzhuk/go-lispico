## ADDED Requirements

### Requirement: Temporal is the first adapter target behind an engine-neutral contract

Temporal SHALL be the first approved adapter target. The graph contracts the adapter implements SHALL stay engine-neutral: a graph definition SHALL NOT name a Temporal type, workflow ID, task queue, or Temporal-specific field. The adapter SHALL live in its own module with its own `go.mod`, never inside `core/`, `runtime/`, or `internal/`, and the root module baseline SHALL remain unchanged.

#### Scenario: Graph definition stays engine-neutral

- **WHEN** a Lisp-produced workflow graph is defined
- **THEN** the definition SHALL contain no Temporal-specific field, workflow ID, or task queue

#### Scenario: Second engine is admissible

- **WHEN** a different durable engine is later targeted
- **THEN** the engine-neutral graph contracts SHALL admit it without changing the graph definition

### Requirement: Model and tool calls run as Activities

Every model call and every tool call SHALL run as an Activity. Workflow code and the graph interpretation SHALL be deterministic and free of side effects. A workflow function SHALL NOT call a model, a tool, a clock, a random source, or a host-registered primitive directly.

#### Scenario: Model call runs as an Activity

- **WHEN** a workflow node performs a model call
- **THEN** the call SHALL execute as an Activity and the workflow code SHALL return only its declared result

#### Scenario: Non-deterministic workflow code is refused

- **WHEN** workflow code reads a clock or a random source
- **THEN** the adapter SHALL refuse that workflow code

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

The system SHALL NOT assume or promise an engine exactly-once guarantee. Documentation, host code, and host-facing messages SHALL state that ambiguous outcomes are resolved by host idempotency or reconciliation, not by an engine claim.

#### Scenario: No exactly-once claim in host-facing text

- **WHEN** host documentation describes effect delivery
- **THEN** it SHALL NOT state that effects happen exactly once

#### Scenario: Unknown outcome is reconciled

- **WHEN** the engine reports an unknown effect outcome
- **THEN** the host SHALL reconcile rather than assume success or assume failure