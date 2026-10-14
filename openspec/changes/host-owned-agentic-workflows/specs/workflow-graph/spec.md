## ADDED Requirements

### Requirement: A workflow graph is validated before execution

The system SHALL validate a Lisp-produced workflow graph and reject it before any node executes. Validation SHALL reject: a graph with no entry node, a node with no reachable path from the entry node, an edge referencing an unknown node, a join declaring no dependencies, a cycle that is not declared as a bounded loop, a bounded loop without a declared iteration or step limit, and a node requiring a capability the selected adapter does not provide.

#### Scenario: Invalid structure is rejected before any node runs

- **WHEN** a graph declares an edge whose target node does not exist
- **THEN** validation SHALL return an error and no node SHALL execute

#### Scenario: Missing loop bound is rejected

- **WHEN** a graph declares a cycle that carries no iteration limit and no step limit
- **THEN** validation SHALL return an error and no node SHALL execute

#### Scenario: Unsupported adapter capability is rejected

- **WHEN** a graph declares a node that requires a capability the selected adapter does not provide
- **THEN** validation SHALL return an error naming the missing capability

#### Scenario: Valid graph passes validation

- **WHEN** a graph declares an entry node, reachable nodes, a join with at least one declared dependency, and a cycle with a declared iteration limit
- **THEN** validation SHALL succeed

### Requirement: Branches advance independently

A fast branch SHALL advance while an unrelated slow branch is still active. The system SHALL NOT require a global superstep barrier in which every branch completes before any branch continues. The rejected semantics are the global-barrier execution model described in `docs/research/awl.md`, not fork and join itself.

#### Scenario: Fast branch continues while a slow branch runs

- **WHEN** two sibling branches start and one branch completes while the other is still executing
- **THEN** the completed branch SHALL be free to proceed to its downstream node without waiting for the slow branch

#### Scenario: No global barrier is imposed

- **WHEN** any branch becomes runnable while another branch remains active
- **THEN** the system SHALL NOT hold the runnable branch until every branch completes

### Requirement: A join waits only on its declared dependencies

A join node SHALL wait until every branch it declares as a dependency has delivered its result, and SHALL NOT wait on a branch it does not declare.

#### Scenario: Undeclared branch does not block a join

- **WHEN** a join declares two dependencies and a third branch continues past the join
- **THEN** the join SHALL proceed when the two declared dependencies have delivered, regardless of the third branch

#### Scenario: Declared dependency still blocks

- **WHEN** a join declares a dependency whose branch has not delivered
- **THEN** the join SHALL remain blocked

### Requirement: Loops stop at their declared bound

A declared loop SHALL stop at its configured iteration or step bound. A loop with no declared bound SHALL NOT be executable.

#### Scenario: Loop stops at its bound

- **WHEN** a loop declares an iteration limit of three and its body would otherwise continue
- **THEN** execution SHALL stop after three iterations

### Requirement: The loop bound holds after recovery

The declared iteration or step bound SHALL hold after recovery from a restart or a fault as it does during uninterrupted execution.

#### Scenario: Bound holds after recovery

- **WHEN** a loop is interrupted before its declared iteration limit and the workflow recovers
- **THEN** the recovered execution SHALL stop at the same declared iteration limit rather than restarting the count

### Requirement: Repeated loop activations keep distinct iteration identities

Repeated activations of the same node SHALL carry distinct iteration identities so a recovered execution never confuses one activation with another.

#### Scenario: Repeated activations keep distinct identities

- **WHEN** the same node activates more than once within one execution
- **THEN** each activation SHALL carry a distinct iteration identity

### Requirement: A checkpointing loop aggregate carries folded state into recovery

A channel-style aggregate that checkpoints folded values, as `BinaryOperatorAggregate` does in [langgraph](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/langgraph/channels/binop.py), SHALL carry that folded state into the recovered execution rather than restarting from an empty value.

#### Scenario: Folded state survives recovery

- **WHEN** a loop aggregates values with a checkpointing aggregate and the workflow recovers mid-loop
- **THEN** the recovered execution SHALL continue from the checkpointed folded value

### Requirement: Approval binds to the digest of the reviewed patch

An approval SHALL record the digest of the patch it reviewed. An approval SHALL NOT authorize a patch whose digest differs from the recorded digest. A duplicate or stale approval SHALL NOT authorize a different patch.

#### Scenario: Approval applies to the reviewed patch

- **WHEN** an approval is recorded for a patch digest and the patch is applied with that digest
- **THEN** the host SHALL accept the approval as valid for that patch

#### Scenario: Stale approval does not authorize a changed patch

- **WHEN** an approval is recorded for one patch digest and a later patch carries a different digest
- **THEN** the host SHALL reject the approval and SHALL NOT apply the later patch

#### Scenario: Duplicate approval does not authorize a different patch

- **WHEN** the same approval is replayed against a different patch digest
- **THEN** the host SHALL reject it as a digest mismatch

### Requirement: Workflow capabilities stay within host-registered primitives

A workflow node SHALL reach the outside world only through a primitive the host registered behind the host permission layer.

#### Scenario: Unregistered primitive is refused

- **WHEN** a workflow node requests a capability the host did not register
- **THEN** the host SHALL refuse the call and the workflow SHALL NOT perform the effect

### Requirement: A workflow capability adds no MCP transport

The workflow capability SHALL NOT add a new transport. MCP standard transports are stdio and Streamable HTTP only ([transports specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)), so a workflow capability SHALL NOT require another MCP transport.

#### Scenario: Non-standard transport is refused

- **WHEN** a workflow declares an MCP transport other than stdio or Streamable HTTP
- **THEN** the host SHALL reject the declaration

### Requirement: A workflow promises no evaluation-time bound

A workflow SHALL NOT promise linear-time evaluation of its expressions; the CEL language definition states no blanket linear-time guarantee ([CEL language definition](https://github.com/google/cel-spec/blob/master/doc/langdef.md)).

#### Scenario: No linear-time promise is claimed

- **WHEN** a workflow capability documents the evaluation cost of its expressions
- **THEN** the documentation SHALL NOT claim a linear-time guarantee

### Requirement: Network-management draft error codes are not base MCP codes

Error codes defined by a network-management extension draft SHALL NOT be treated as base MCP codes ([draft-zw-opsawg-mcp-network-mgmt-00](https://www.ietf.org/archive/id/draft-zw-opsawg-mcp-network-mgmt-00.html)).

#### Scenario: Draft error code is not a base MCP code

- **WHEN** a workflow capability reports an error defined only by the network-management extension draft
- **THEN** the host SHALL NOT present it as a base MCP error code