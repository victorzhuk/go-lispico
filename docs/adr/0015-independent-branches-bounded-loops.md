---
status: proposed
---

# Branches advance independently; cycles are explicit bounded loops

A workflow graph's parallelism is declared, not implied. Branch nodes advance as soon as their own declared dependencies are satisfied, without waiting on unrelated branches. Synchronization happens only where the graph declares a join, and a join waits for exactly the dependencies it names. There is no global barrier between "rounds" of work: a fast branch is not held back by a slow one.

Global Pregel-style superstep barriers are rejected. A barrier converts every branch's latency into the slowest branch's latency, and it forces the graph author to reason about a global clock the host does not control. The research input `docs/research/awl.md` proposes such a global barrier (sections 2 and 5.4); this record rejects that global-barrier semantics. Fork/join itself is not what is rejected — declared joins stay. What is rejected is the implicit global round boundary, so that correction is stated here so the rejection is not later misread as a rejection of structured concurrency.

Cycles are allowed, but only as explicit bounded loops. A cyclic graph is valid only when the loop declares its iteration limit or its step limit up front, and each activation of the loop body carries a distinct iteration identity. Recovery must never confuse two activations of the same loop: an identifier that repeats across iterations is a defect, not a limitation, because the durable engine replays activations and a repeated identity would resolve the second activation onto the first one's record.

## Consequences

- Validation rejects a graph with an unbounded cycle, and rejects a loop whose bodies cannot be given distinct iteration identities.
- A branch that depends on nothing runs immediately; latency is per-branch, not per-graph.
- A join that names fewer dependencies than the graph implies is a graph error, caught before execution.
- Loop identity is part of the graph contract, not an adapter detail, so an adapter cannot quietly drop it.

## Considered options

- Global superstep barrier between branch rounds: rejected — it makes the slowest branch the latency of every branch, and it is the semantics proposed in `docs/research/awl.md` sections 2 and 5.4.
- Arbitrary cycles with recovery-time cycle detection: rejected — unbounded work and ambiguous replay outcomes are worse than a graph that fails validation.
- Bounded loops with a shared iteration identity: rejected — recovery would conflate repeated activations of the same loop body.