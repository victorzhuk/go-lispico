## 1. Workflow graph spec

- [x] 1.1 Draft `specs/workflow-graph/spec.md`: validation rejects invalid structure, missing loop bounds, and unsupported adapter capabilities before execution.
- [x] 1.2 Draft independent branch advance and declared-join semantics; state that no global superstep barrier is required.
- [x] 1.3 Draft bounded loop requirements: declared iteration and step bounds hold after recovery, and repeated activations carry distinct iteration identities.
- [x] 1.4 Draft patch-bound approval requirements: approval binds to the digest of the reviewed patch, and duplicate or stale approval authorizes nothing.
- [ ] 1.5 Check every scenario against `docs/adr/0014-host-owned-workflow-boundary.md` and `docs/adr/0015-independent-branches-bounded-loops.md`.

## 2. Host contracts spec

- [x] 2.1 Draft `specs/host-contracts/spec.md`: narrow consumer-owned contracts for application data, execution history, and optional effect receipts.
- [x] 2.2 Draft the declared guarantees per contract: idempotency-key reservation atomicity, concurrency and ownership with lease plus scanner cleanup, ordering scope, partition key, retention horizon.
- [x] 2.3 Draft backend conformance: a sharded or distributed backend satisfies the declared guarantees or rejects the configuration; unsupported guarantees never degrade silently.
- [x] 2.4 Draft substitution requirements: local-to-distributed preserves observable behavior, definitions stay free of storage clients and local paths, and engine history stays engine-owned.
- [ ] 2.5 Check every scenario against `docs/adr/0016-segregated-persistence-contracts.md`.

## 3. Temporal adapter spec

- [x] 3.1 Draft `specs/temporal-adapter/spec.md`: Temporal as the first adapter target behind an engine-neutral graph contract.
- [x] 3.2 Draft activity mapping and determinism: model and tool calls run as Activities; the graph and workflow code are deterministic and side-effect-free.
- [x] 3.3 Draft durable approval signals with an explicit deadline outcome.
- [x] 3.4 Draft restart-during-approval and unknown-outcome scenarios: no unsafe blind retry of a patch.
- [x] 3.5 State that no engine exactly-once guarantee is assumed or promised.
- [ ] 3.6 Check every scenario against `docs/adr/0017-temporal-first-adapter.md`.

## 4. Cross-check deltas against ADR 0014-0017

- [ ] 4.1 Verify every normative claim in the three specs traces to a decision in ADRs 0014, 0015, 0016, or 0017, and drop or weaken any claim that does not.
- [ ] 4.2 Verify the research corrections carry their primary-source URLs wherever the corrected claim would otherwise read as a requirement.
- [ ] 4.3 Verify `proposal.md` lists the four parent ADRs and records the `## ADDED Requirements` deviation.
- [ ] 4.4 Confirm the change adds no production code and leaves the root `go.mod` baseline untouched.