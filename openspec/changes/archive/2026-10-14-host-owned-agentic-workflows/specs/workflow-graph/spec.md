## ADDED Requirements

### Requirement: A workflow graph is validated before execution

The system SHALL validate a Lisp-produced workflow graph and reject an invalid graph before any node executes.

#### Scenario: Invalid structure is rejected before any node runs

- **WHEN** a graph declares an edge whose target node does not exist
- **THEN** validation SHALL return an error and no node SHALL execute

#### Scenario: Valid graph passes validation

- **WHEN** a graph declares an entry node, reachable nodes, joins that declare every dependency the graph implies between their incoming branches, a cycle declared as a bounded loop with a declared iteration limit whose activations carry distinct iteration identities, and node capabilities that the selected adapter provides
- **THEN** validation SHALL succeed

### Requirement: A join declares every dependency it waits on

Validation SHALL reject a join declaring no dependencies and a join that names fewer dependencies than the graph implies.

#### Scenario: Under-declared join is rejected before execution

- **WHEN** a join names fewer dependencies than the graph implies between its incoming branches
- **THEN** validation SHALL return an error and no node SHALL execute

### Requirement: A cycle is declared as a bounded loop

Validation SHALL reject a cycle that is not declared as a bounded loop, a bounded loop without a declared iteration or step limit, and a loop whose activations cannot be given distinct iteration identities.

#### Scenario: Missing loop bound is rejected

- **WHEN** a graph declares a cycle that carries no iteration limit and no step limit
- **THEN** validation SHALL return an error and no node SHALL execute

### Requirement: A required capability is provided by the selected adapter

Validation SHALL reject a node requiring a capability the selected adapter does not provide.

#### Scenario: Unsupported adapter capability is rejected

- **WHEN** a graph declares a node that requires a capability the selected adapter does not provide
- **THEN** validation SHALL return an error naming the missing capability

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

### Requirement: A replayed activation is tolerated after recovery

The host SHALL tolerate a node activation the durable engine delivers more than once from its own record after a restart, and SHALL NOT persist an interpreter stack across the wait.

#### Scenario: Replayed activation is tolerated after recovery

- **WHEN** the durable engine replays its own record after a restart and delivers the same node activation to the host a second time
- **THEN** the host SHALL tolerate the repeated activation and SHALL NOT persist an interpreter stack across the wait

_Informative note (non-normative): research observation, not a host obligation. A channel-style aggregate that checkpoints folded values, as `BinaryOperatorAggregate` does in [langgraph](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/langgraph/channels/binop.py), carries that folded state into the recovered execution rather than restarting from an empty value._

### Requirement: Approval binds to the digest of the reviewed patch

An approval SHALL record the digest of the patch it reviews together with its declared deadline outcome. An approval SHALL authorize only the patch whose digest matches the recorded digest; a patch whose digest differs SHALL be rejected and SHALL NOT be applied. No approval outcome is assumed beyond the recorded approved, rejected, or deadline-expired result. The approval contract stays engine-neutral; the engine-specific signal mapping lives in the temporal-adapter delta.

#### Scenario: Approval records the reviewed digest

- **WHEN** an approval arrives before its declared deadline
- **THEN** the workflow SHALL record the approved outcome together with the digest of the reviewed patch

#### Scenario: Matching digest authorizes the patch

- **WHEN** the patch presented for application carries the digest recorded with the approval, and the recorded approval outcome is approved rather than rejected or deadline-expired
- **THEN** the workflow SHALL apply the patch and continue

#### Scenario: Changed digest is rejected

- **WHEN** the patch presented for application carries a digest that differs from the recorded approval digest
- **THEN** the workflow SHALL reject the approval for that patch and SHALL NOT apply it

#### Scenario: Approval wait survives a restart

- **WHEN** the process restarts while the workflow waits for an approval
- **THEN** the recovered workflow SHALL resume the same pending approval wait with its declared deadline intact

#### Scenario: Duplicate approval does not authorize a different patch

- **WHEN** the durable engine replays its record and delivers the same approval again
- **THEN** the workflow SHALL keep the recorded outcome and digest and SHALL NOT run the approval wait a second time

### Requirement: Workflow capabilities stay within host-registered primitives

A workflow node SHALL reach the outside world only through a primitive the host registered behind the host permission layer.

#### Scenario: Unregistered primitive is refused

- **WHEN** a workflow node requests a capability the host did not register
- **THEN** the host SHALL refuse the call and the workflow SHALL NOT perform the effect

_Informative note (non-normative): research correction, not a host obligation. The MCP transports specification defines stdio and Streamable HTTP as the standard transports ([transports specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports))._

_Informative note (non-normative): research correction, not a host obligation. The CEL language definition states no blanket linear-time guarantee for expression evaluation ([CEL language definition](https://github.com/google/cel-spec/blob/master/doc/langdef.md))._

_Informative note (non-normative): research correction, not a host obligation. Error codes defined by the network-management extension draft are not base MCP codes ([draft-zw-opsawg-mcp-network-mgmt-00](https://www.ietf.org/archive/id/draft-zw-opsawg-mcp-network-mgmt-00.html))._