---
status: proposed
---

# Host enforcement: the permission layer's controls are contract requirements

ADR 0014 put every world-touching surface behind host-registered primitives and named a host permission layer as the boundary. It did not say what that layer enforces. A grilling of `docs/research/awl.md` against twelve published agent scenarios found the enforcement controls specified nowhere: filesystem and network confinement, information-flow control, operational budgets, history rotation, partial-failure joins, and correlation cardinality.

Neither plugin can host those rules. `plugins/fsm` persists current state only (`plugins/fsm/machine.go:248-297`) and drops events silently when its channel is full (`plugins/fsm/plugin.go:471-475`). `plugins/agent/run-parallel` cancels siblings on the first error and discards completed results (`plugins/agent/plugin.go:209,:234`). Growing either would put infrastructure into plugins that ADR 0004 and ADR 0014 keep out of the workflow path: `agent` is frozen, and `fsm` is consumer-gated with no consumer.

## What this decides

- The permission layer's controls are host-contract requirements. `host-contracts` declares what a registered capability must enforce: a declared control set per capability, canonical filesystem confinement, a strict network allowlist, the three-capability rule, taint carried through persistence and replay, mandatory budgets with a declared breach landing state, bounded history, partial-failure join policy, and correlation cardinality with deadline precedence.
- The approval digest is carried by the adapter. `workflow-graph` binds an approval to the reviewed patch's digest; the adapter's approval Signal SHALL carry that digest and SHALL reject a mismatch or its absence.
- `agent` stays frozen and `fsm` stays consumer-gated. Their durability and fan-out limits are inputs to the host contracts, not defects to repair in the plugin.

## Consequences

- Enforcement is declarative and testable per capability, independent of any workflow definition.
- The adapter module gains a digest-carry obligation but no new dependency, and the root module baseline is unchanged.
- A host that cannot satisfy a declared control rejects the capability rather than degrading silently.

## Considered options

- Grow `fsm` into a durable state machine with history and replay: rejected — ADR 0014 keeps durability in the engine, and `fsm` has no consumer to justify the surface.
- Leave enforcement implicit in the host application and the specs silent: rejected — a control that is not declared cannot be re-checked on replay, which is the failure the grill found.
- Require taint separation in place of the patch-digest approval binding: rejected — the digest blocks unauthorized mutation and taint blocks exfiltration. The two cover different threats and both are needed.