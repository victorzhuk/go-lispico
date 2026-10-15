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
- [ ] 4.3 Define fan-in partial-failure semantics for the `NamedBarrierValue` channel in the state-graph section.
- [ ] 4.4 Define correlation cardinality and deadline precedence in the routing and manifest sections.
- [ ] 4.5 Define the landing state for a mid-superstep budget breach in the state-graph and manifest sections.
- [ ] 4.6 Define taint propagation across checkpoint and replay, and the decontamination predicate, in the security and schema sections.
- [ ] 4.7 Define `terminate-cleanly` and `completionBoundary`.

## 5. Validation

- [ ] 5.1 Run `openspec validate workflow-host-enforcement --strict` and resolve every finding.
- [ ] 5.2 Confirm every added requirement traces to a scenario in the grill or to ADR 0014, 0016, 0017, or 0018.
- [ ] 5.3 Confirm the change adds no production code and leaves the root `go.mod` baseline untouched.