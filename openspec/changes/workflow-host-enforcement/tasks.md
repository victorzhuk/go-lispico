OVERSIZE: 23 task rows exceed twelve; the assessment indivisibility approval (grilled, 12 scenarios) applies because the 23 rows form one change-delivery acceptance boundary across two spec deltas, one ADR, and the AWL closure; splitting would publish untraceable requirements.
## 1. Host enforcement clauses

- [ ] 1.1 Add the permission-layer declaration requirement to `specs/host-contracts/spec.md`: every world-touching capability declares its enforced control set, and a capability registered without a declaration is rejected before any workflow using it starts.
- [ ] 1.2 Add the filesystem confinement requirement: the root is canonicalized before the first operation, a path that resolves outside it through a symlink, hard link, or intermediate component is rejected, and the home directory is contained.
- [ ] 1.3 Add the network allowlist requirement: a capability declares its allowed hosts, and a host outside the allowlist is refused without an interactive prompt.
- [ ] 1.4 Add the three-capability requirement: a stage declares untrusted input, secret access, and outbound state change, and a stage declaring all three is rejected at validation.
- [ ] 1.5 Add the taint persistence requirement: a tainted value keeps its label across checkpoint, restart, and replay, labels combine so aggregation cannot launder taint, and clearing a label requires a declared decontamination gate.
- [ ] 1.6 Add the budget requirement: every workflow declares maximum steps, tokens, wallclock, and cost plus a concurrency cap, and a workflow without them is rejected.
- [ ] 1.7 Add the breach landing-state requirement: a breach emits its declared event, leaves the run in a declared state, and never leaves a superstep partially applied.
- [ ] 1.8 Add the bounded-history requirement: a long-lived workflow rotates execution history at a declared snapshot frequency and preserves the state it declares as carried forward.
- [ ] 1.9 Add the partial-failure join requirement: a failed branch fills its slot as failed, and the join releases or fails per its declared policy without discarding completed siblings.
- [ ] 1.10 Add the correlation requirement: a correlation declares its cardinality, its deadline, and which outcome wins when the deadline fires first; an absent cardinality means one event.

## 2. Adapter digest carry

- [ ] 2.1 Add the approval-digest carry requirement to `specs/temporal-adapter/spec.md`: the approval Signal carries the digest the graph layer recorded, and a Signal whose digest does not match is rejected.
- [ ] 2.2 Add the digest-absence and replayed-stale-approval scenarios: a Signal carrying no digest authorizes nothing, and a replayed approval cannot authorize a later patch.

## 3. Enforcement boundary ADR

- [ ] 3.1 Write `docs/adr/0018-host-enforcement-boundary.md` recording that the permission layer's controls are host-contract requirements, that `agent` stays frozen, that `fsm` stays consumer-gated, and that the approval digest is carried by the adapter.

## 4. AWL research closure

- [ ] 4.1 In `docs/research/awl.md`, replace the unsupported dedup-window assertion in the durable-receipt section with a stated retention horizon and its source.
- [ ] 4.2 Add the history-rotation rule to the execution-journal section of the reference manifest.
- [ ] 4.3 Define fan-in partial-failure semantics for the `NamedBarrierValue` channel in the state-graph section. Deliverable: in the `stateGraphChannels.channels` entry `syncGate` (`docs/research/awl.md:291`) and the channel-table row for `NamedBarrierValue` / `LastValueAfterFinish` (`docs/research/awl.md:37`), state which required name a failed branch fills, that the barrier opens once every `requiredNames` slot carries a value or a failure marker, that the failure marker is persisted with the barrier's fill state so a restart reopens the same barrier, and that the declared release policy then decides whether the gate releases with the filled results or fails the run.
- [ ] 4.4 Define correlation cardinality and deadline precedence in the routing and manifest sections. Deliverable: in the routing expression that carries `correlate.approvalRequestId` (`docs/research/awl.md:417-420`) and in the manifest schema, state the cardinality each correlation awaits, that an absent cardinality means exactly one event, and which outcome wins when the deadline expires before the awaited count is reached.
- [ ] 4.5 Define the landing state for a mid-superstep budget breach in the state-graph and manifest sections. Deliverable: alongside the superstep phase list and the global superstep barrier (`docs/research/awl.md:231`), state that a breach detected between the plan phase and the update phase resolves before the run leaves that superstep, that the run leaves it with no partial set of channel updates applied, and that the run then takes the declared breach action and emits the declared breach event.
- [ ] 4.6 Define taint propagation across checkpoint and replay, and the decontamination predicate, in the security and schema sections. Deliverable: beside the `::taint-level` enum (`docs/research/awl.md:108`) and the `securityAndInformationFlowControl` block (`docs/research/awl.md:358`), state that a taint label is written into the checkpoint alongside the value and restored on reconstruction, that labels combine so a merged value keeps the strongest label of its inputs, and that a label is cleared only by an explicitly declared decontamination gate.
- [ ] 4.7 Define `terminate-cleanly` and `completionBoundary`. Deliverable: name the state a run reaches under `operationalBudgets.breachPolicy.action: "terminate-cleanly"` and the terminal state named by `completionBoundary`, state which channel updates and which lifecycle transitions `terminate-cleanly` allows and forbids, and state that `completionBoundary` is reached only when every declared stage reached a terminal state.

## 5. Validation

- [ ] 5.1 Run `openspec validate workflow-host-enforcement --strict` and resolve every finding. What this asserts: the change directory is a well-formed OpenSpec change — each delta carries `## ADDED Requirements`, every requirement header names a capability delta that exists, and every requirement has at least one scenario with WHEN and THEN lines. It asserts document structure, not requirement correctness.
- [ ] 5.2 Confirm every added requirement traces to a scenario in the grill or to ADR 0014, 0016, 0017, or 0018. What this asserts: the requirement-to-source table below names one source per added requirement, and each named source exists in this repository.
- [ ] 5.3 Confirm the change adds no production code and leaves the root `go.mod` baseline untouched. What this asserts: no file under `plugins/`, `internal/`, or any other production path is added or modified by this change, and `go.mod` carries no new requirement relative to the base this change was planned against.

## 6. Requirement-to-source traceability

| # | Requirement name | Carried by | Requirement text in | Scenario text in | Source |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 1.1 | The permission layer declares an enforced control set per capability | host-contracts | `specs/host-contracts/spec.md` | same file | fan-out partial failure, https://docs.langchain.com/oss/python/langgraph/graph-api |
| 1.2 | Filesystem capability is confined to a canonical root | host-contracts | `specs/host-contracts/spec.md` | same file | indirect prompt injection, https://genai.owasp.org/llmrisk/llm06-sensitive-information-disclosure |
| 1.3 | Network capability declares a strict host allowlist | host-contracts | `specs/host-contracts/spec.md` | same file | indirect prompt injection, https://genai.owasp.org/llmrisk/llm06-sensitive-information-disclosure |
| 1.4 | Untrusted input does not combine with secrets and outbound mutation | host-contracts | `specs/host-contracts/spec.md` | same file | agentic memory hijack, https://genai.owasp.org/2025/12/09/owasp-top-10-for-agentic-applications-the-benchmark-for-agentic-security-in-the-age-of-autonomous-ai |
| 1.5 | A tainted value stays tainted across persistence and replay | host-contracts | `specs/host-contracts/spec.md` | same file | agentic memory hijack, https://genai.owasp.org/2025/12/09/owasp-top-10-for-agentic-applications-the-benchmark-for-agentic-security-in-the-age-of-autonomous-ai |
| 1.6 | Every workflow declares operational budgets | host-contracts | `specs/host-contracts/spec.md` | same file | budget ceilings, https://openai.github.io/openai-agents-python/running_agents |
| 1.7 | A budget breach has a declared landing state | host-contracts | `specs/host-contracts/spec.md` | same file | budget ceilings, https://openai.github.io/openai-agents-python/running_agents |
| 1.8 | Execution history is bounded by rotation | host-contracts | `specs/host-contracts/spec.md` | same file | versioned long-running instances, https://docs.temporal.io/workflow-definition |
| 1.9 | A declared join releases on partial failure under its declared policy | host-contracts | `specs/host-contracts/spec.md` | same file | fan-out partial failure, https://docs.langchain.com/oss/python/langgraph/graph-api |
| 1.10 | Correlation declares cardinality and deadline precedence | host-contracts | `specs/host-contracts/spec.md` | same file | correlation with a deadline, https://sonataflow.org/serverlessworkflow/latest/core/cncf-serverless-workflow-specification-support.html |
| 2.1 | An approval Signal carries the digest the graph layer recorded | temporal-adapter | `specs/temporal-adapter/spec.md` | same file | ADR 0017 `docs/adr/0017-temporal-first-adapter.md`; durable human waits, https://docs.restate.dev/develop/ts/external-events |
| 3.1 | Enforcement boundary ADR | — | `docs/adr/0018-host-enforcement-boundary.md` | — | ADR 0014 `docs/adr/0014-host-owned-workflow-boundary.md`, ADR 0016 `docs/adr/0016-segregated-persistence-contracts.md`, ADR 0017 `docs/adr/0017-temporal-first-adapter.md` |
| 4.1-4.7 | AWL research closure | — | `docs/research/awl.md` | — | `openspec/changes/workflow-host-enforcement/design.md`, decision `Research-document closure is a task, not a capability delta` |

Verification floor: this change adds requirement and scenario text and research prose only. Rows 5.1 through 5.3 assert delta structure, scenario presence, requirement-to-source traceability, and that production code and the root `go.mod` stay untouched. They do not assert runtime behaviour: no requirement here has an implementing code path yet, because ADR 0014 keeps `plugins/agent` frozen and `plugins/fsm` consumer-gated.

## 7. Where the requirements land

- Apply writes the requirements into this change directory only: `specs/host-contracts/spec.md` and `specs/temporal-adapter/spec.md` under `openspec/changes/workflow-host-enforcement/`. It writes nothing under `openspec/specs/`.
- The canonical capability specs `openspec/specs/host-contracts/spec.md` and `openspec/specs/temporal-adapter/spec.md` receive these requirements at archive, when the deltas are applied to them. Until archive, the canonical specs stay as they are at the pinned base and these requirements are not yet in force.
- ADR 0018 and `docs/research/awl.md` are documents outside the capability set; they are edited in place and are not deltas.