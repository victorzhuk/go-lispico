---
status: proposed
---

# Three persistence concerns, three narrow contracts, no generic storage interface

Storage is split by interface segregation, not collected into one interface. The workflow path has three distinct persistence concerns and each gets a contract owned by the consumer that needs it: application-data operations, execution-history operations, and optional effect receipts. A combined generic storage interface is rejected — one interface covering unrelated guarantees forces every implementation to promise all of them, and callers lose the ability to say which guarantee they actually rely on.

Effect receipts are optional. A workflow that makes no ambiguous external effect declares no receipt contract; one that does must declare it.

## Guarantees each contract declares

Every persistence contract states its guarantees before any backend is chosen:

- Idempotency-key reservation atomicity: reserving a key is atomic, so two concurrent activations of the same logical operation cannot both proceed.
- Concurrency and ownership: a single owner at a time, held by a lease; in-flight markers are swept by a scanner so a crashed owner's work is not stranded.
- Ordering scope: the contract states which operations it orders and which it does not, rather than implying a total order.
- Partition/shard key: the key that routes an operation, so a sharded deployment is a stated fact rather than an accident.
- Retention horizon: how long records live, so history-backed guarantees have an end.

## Substituting a backend

A sharded backend satisfies the declared guarantees or it rejects the configuration. Interface sameness alone proves nothing: a backend that matches the method signatures but cannot reserve idempotency keys atomically is not a conforming implementation, and the adapter must refuse the configuration rather than run and lose work. Observable behavior is preserved across substitution; anything a backend cannot honor is a startup failure, not a silent downgrade.

Host storage substitution never implies replacing the engine's own history backend. The engine keeps its history; these contracts cover application data, execution history the host chooses to retain, and receipts.

## Consequences

- Three contracts, three guarantee sets, three retention horizons. A caller names the guarantee it depends on.
- An implementation that cannot meet a declared guarantee is rejected at configuration time.
- Adding a fourth concern requires a fourth contract, not a wider interface.
- The host owns its own durable records; the engine owns its history, and neither borrows the other's.

## Considered options

- One `Storage` interface with generic key/value or CRUD methods: rejected — a single interface hides which guarantees hold and forces every backend to over-promise.
- Typed collections over one generic store: rejected — it moves the guarantee statement into call-site comments where nothing checks it.
- Leaving guarantees implicit in the adapter implementation: rejected — an implicit guarantee cannot be tested, and substitution then depends on trust.