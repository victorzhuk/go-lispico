---
status: accepted
---

# Kernel-first: the embedding consumer drives the roadmap; world-touching plugins are frozen

go-lispico is a language kernel for a host Go application first, a general-purpose scripting environment second. Its one real consumer (yagel) loads only the pure-computation plugins — stdlib and data — and registers its own gated primitives for every world-touching surface, because an IO plugin loaded into a host bypasses the host's permission layer by construction. The roadmap therefore follows what embedded rule and policy code needs: a complete pure-computation stdlib and a solid concurrency contract for concurrent `Eval` on one Engine. The world-touching plugins — llm, agent, lio, net, exec — are frozen: security and correctness fixes only, no new features. The first real policy written against the kernel could not express `(< budget 500)` — comparison and equality builtins do not exist outside test fixtures — which is the concrete evidence that stdlib completeness, not plugin breadth, is where the missing value is.

## Consequences

- Stdlib completeness is the active workstream: `=`, `<`, `>`, `<=`, `>=` first, then the collection basics rule authors reach for next (`contains?`, `merge`, `dissoc`, `sort`, `range`).
- README positioning must stop advertising the plugin breadth as live surface; frozen plugins are marked frozen so nobody builds on llm or agent expecting evolution.
- fsm is pure computation (no world-touching imports) and is not frozen by this decision, but it has no consumer either; it earns investment only when one appears.
- Freezing is reversible; extracting or deleting the frozen plugins is a separate later decision if the freeze proves permanent.

## Considered options

- General-purpose embeddable Lisp with a growing plugin ecosystem: rejected — no user pulls on llm, agent, net, or exec; polishing them is work without a consumer.
- Deleting the world-touching plugins now: not chosen — the freeze keeps working code (file sandbox, net response caps) at zero feature cost while the mission settles.
