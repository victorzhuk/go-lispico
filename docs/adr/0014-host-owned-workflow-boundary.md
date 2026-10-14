---
status: proposed
---

# Host-owned workflows: Lisp defines a validated graph, the host maps it to a durable engine

Agentic work is host territory, not language territory. A Lisp program here defines a workflow graph — nodes, declared joins, declared bounds, the capabilities each node needs — and that graph is validated before a single node runs. It is not executed by an interpreter that lives across the run. The Go host owns models, tools, application storage, security policy, and sandboxing; a host adapter maps the validated graph onto an external durable engine. Recovery is the engine's responsibility, and the engine owns it: no interpreter stack is persisted across a wait, a retry, or a process restart. ADR 0004 remains the parent decision and is preserved as written.

## What the boundary is

- The Lisp side owns graph definition and validation. Structure, declared bounds, and capability requests are checked before execution; an invalid graph never starts.
- The host side owns every world-touching surface. Models, tools, storage, security, and sandboxing are host-registered primitives — `runtime/engine.go:21-65` already carries the `Engine` interface where a host registers capabilities. The workflow module adds a graph-shaped way to name those capabilities, not a new capability surface.
- The engine side owns durable recovery. After a wait, a crash, or a redeploy, the engine replays from its own record. The host receives the same node activations again and must tolerate that.

## Relationship to the frozen plugins

This module revives nothing. `llm`, `agent`, `lio`, `net`, and `exec` stay frozen per ADR 0004 — security and correctness fixes only, no new features — and workflow nodes reach the world exclusively through host-registered primitives behind the host permission layer. A workflow that needs a model call is given a host primitive that makes that call under host policy; it is not given a plugin that makes the call on its own.

`fsm` is unfrozen by ADR 0004 but consumer-gated: it has no consumer and earns investment only when one appears. A workflow that needs explicit state also declares its state through host contracts rather than growing `fsm` for the purpose.

The existing plugins show what is missing and confirm nothing has to be un-frozen to fix it. `plugins/agent/plugin.go:206-233` (`runParallel`) fans out with a bounded pattern and no durable replay; `plugins/fsm/machine.go:204-233` (`Serialize`) persists current state only, with no history and no replay. Durable recovery lives in the engine, not in either plugin.

## Consequences

- The Lisp contract is engine-neutral and stays small: a validated graph plus the host capabilities it needs.
- A durable engine is a dependency of the workflow path, but the root module keeps its current baseline. Adapter modules carry their own `go.mod` (ADR 0017).
- Anything that must survive a wait survives in the engine or in host storage, never in the interpreter stack.
- Nobody may add a workflow feature by editing a frozen plugin; host primitive registration is the only extension seam.

## Considered options

- Run the workflow inside the Lisp interpreter with checkpointed state: rejected — persisting an interpreter stack across waits makes recovery a language problem and couples every host policy change to a replay bug.
- Persist the interpreter stack as a serialized continuation: rejected — the continuation is opaque to the host, so host permission, cancellation, and sandbox decisions cannot be re-checked on replay.
- Generalize `plugins/agent` into the workflow runtime: rejected — ADR 0004 freezes it, and its bounded fan-out has no durable replay to build on.