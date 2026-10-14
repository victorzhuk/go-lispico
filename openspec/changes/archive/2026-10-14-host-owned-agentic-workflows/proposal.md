## Why

Agents built over go-lispico need workflows that survive process restarts and long human waits, but the kernel must not own infrastructure. Durable execution, retries, and history belong to an external engine; the Lisp layer defines the workflow, and the Go host owns models, tools, storage, and permissions.

The existing plugin paths show the gap. `plugins/agent/plugin.go:206-233` fans out bounded parallel work in a single process pass and loses completed branches when that pass is interrupted. `plugins/fsm/machine.go:204-233` serializes current state only, so a restart resumes from a snapshot with no execution history to replay. Neither path survives a host crash without an engine that owns durability.

## What Changes

- Lisp produces a validated workflow graph: structure, loop bounds, and required adapter capabilities are checked before any node executes.
- The Go host owns a workflow boundary. The host adapter maps the validated graph onto an external durable execution engine. No interpreter stack is persisted across a wait or a restart.
- Branches advance independently and synchronize only at declared joins. Graph cycles are allowed only as explicit bounded loops with declared iteration limits.
- Persistence splits by interface segregation into consumer-owned narrow contracts: application-data operations, execution-history operations, and optional effect receipts.
- Temporal is the first approved adapter target, implemented in its own module behind engine-neutral graph contracts.
- Human approval is a durable wait bound to the digest of the reviewed patch, so a stale or replayed approval cannot authorize a different patch.

First acceptance workflow: parallel code analysis produces joined findings, the joined result waits on a durable human approval, and the host applies the approved patch under host-controlled permissions.

Parent decisions:

- `docs/adr/0014-host-owned-workflow-boundary.md`
- `docs/adr/0015-independent-branches-bounded-loops.md`
- `docs/adr/0016-segregated-persistence-contracts.md`
- `docs/adr/0017-temporal-first-adapter.md`

This change implements the decisions recorded in those four ADRs; the spec deltas express them as `## ADDED Requirements` because all three capabilities are new.

Deviation from the default template: this change introduces three NEW capability specs (`workflow-graph`, `host-contracts`, `temporal-adapter`). The deltas therefore use `## ADDED Requirements` for the whole delta rather than mirroring `openspec/specs/agent-plugin/spec.md`, whose placeholder scenario cannot define workflow acceptance.

Research corrections carried into the specs: MCP standard transports are stdio and Streamable HTTP only ([transports specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)); no blanket CEL linear-time guarantee exists ([CEL language definition](https://github.com/google/cel-spec/blob/master/doc/langdef.md)); `BinaryOperatorAggregate` checkpoints folded values ([langgraph binop channel](https://github.com/langchain-ai/langgraph/blob/main/libs/langgraph/langgraph/channels/binop.py)); network-management draft error codes are not base MCP codes ([draft-zw-opsawg-mcp-network-mgmt-00](https://www.ietf.org/archive/id/draft-zw-opsawg-mcp-network-mgmt-00.html)).

## Capabilities

### New Capabilities

- `workflow-graph`: validation, independent branches, declared joins, bounded loops, patch-bound approval.
- `host-contracts`: segregated persistence contracts with declared guarantees and substitutable backends.
- `temporal-adapter`: mapping the engine-neutral graph onto the first durable adapter target.

### Modified Capabilities

None.

## Impact

Specs and documents only. No production code change in this change; the adapter module and its `go.mod` are later work. Root module `go 1.24.0` in `go.mod:1-3` stays untouched.