## Why

`openspec/specs/workflow-graph/spec.md:146` requires a workflow node to reach the outside world "only through a primitive the host registered behind the host permission layer", but no specification states what that layer enforces. A grilling of `docs/research/awl.md` against twelve published real-world agent scenarios found the enforcement controls absent from every spec: filesystem and network confinement, information-flow control, operational budgets, history rotation, partial-failure joins, and correlation cardinality.

The existing plugins cannot answer it. `plugins/fsm` persists current state only (`plugins/fsm/machine.go:248-297`), drops events silently when its channel is full (`plugins/fsm/plugin.go:471-475`), and runs transition callbacks with no timeout (`plugins/fsm/machine.go:227-246`). `plugins/agent/run-parallel` cancels every sibling on the first error and discards the results of branches that completed (`plugins/agent/plugin.go:209,:234`), and no agent call path accepts an idempotency key. ADR 0014 keeps `agent` frozen and `fsm` consumer-gated, so the missing rules have to become host contracts.

The approval control is also incomplete. `workflow-graph` binds an approval to the digest of the reviewed patch (`openspec/specs/workflow-graph/spec.md:115-142`), but the adapter implements approval as a Signal plus a deadline with no digest (`openspec/specs/temporal-adapter/spec.md:41-49`). The control exists in one spec and is unimplemented by the layer that runs approvals, so a replayed approval has nothing to match against.

Scenario sources: durable human waits https://docs.restate.dev/develop/ts/external-events; idempotent effects https://www.inngest.com/docs/durable-execution/guides-and-advanced/idempotency; versioned long-running instances https://docs.temporal.io/workflow-definition; fan-out partial failure https://docs.langchain.com/oss/python/langgraph/graph-api; correlation with a deadline https://sonataflow.org/serverlessworkflow/latest/core/cncf-serverless-workflow-specification-support.html; budget ceilings https://openai.github.io/openai-agents-python/running_agents; indirect prompt injection https://genai.owasp.org/llmrisk/llm06-sensitive-information-disclosure; agentic memory hijack https://genai.owasp.org/2025/12/09/owasp-top-10-for-agentic-applications-the-benchmark-for-agentic-security-in-the-age-of-autonomous-ai.

## What Changes

- `host-contracts` gains the clauses the permission layer must enforce: a declared control set per capability, canonical filesystem confinement, a strict network allowlist, the three-capability rule, taint carried through persistence and replay, mandatory operational budgets, a declared landing state for a budget breach, bounded execution history, partial-failure join policy, and correlation cardinality with deadline precedence.
- `temporal-adapter` gains the digest carry: an approval Signal carries the digest the graph layer recorded, and a mismatched or digest-less Signal authorizes nothing.
- ADR 0018 records that enforcement lives in host contracts, that `agent` stays frozen, and that `fsm` stays consumer-gated.
- `docs/research/awl.md` closes its six unwritten rules (dedup horizon, history rotation, fan-in partial failure, correlate cardinality, breach landing state, taint propagation) and defines `terminate-cleanly` and `completionBoundary`.

Parent decisions: `docs/adr/0014-host-owned-workflow-boundary.md`, `docs/adr/0016-segregated-persistence-contracts.md`, `docs/adr/0017-temporal-first-adapter.md`, and the new `docs/adr/0018-host-enforcement-boundary.md`.

Deviation from the default template: this change modifies two capabilities that already exist (`host-contracts`, `temporal-adapter`), so both deltas use `## ADDED Requirements` for new requirements only and carry no `## MODIFIED Requirements`. Existing requirements are left as archived.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `host-contracts`: adds the permission-layer enforcement clauses, the budget and breach requirements, bounded history, partial-failure join release, and correlation cardinality.
- `temporal-adapter`: adds the approval-digest carry requirement.

## Impact

Specifications and documents only. No production code change: ADR 0014 keeps `plugins/agent` frozen and `plugins/fsm` consumer-gated, and enforcement lands in host contracts. The root module baseline in `go.mod` stays untouched.

Deliverable boundary: this change writes its requirements as deltas inside the change directory, at `specs/host-contracts/spec.md` and `specs/temporal-adapter/spec.md` under `openspec/changes/workflow-host-enforcement/`, and writes nothing under `openspec/specs/`. The canonical capability specs `openspec/specs/host-contracts/spec.md` and `openspec/specs/temporal-adapter/spec.md` receive these requirements at archive, when the deltas are applied to them.