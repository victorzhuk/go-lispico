---
status: proposed
---

# Temporal is the first approved integration target, in its own module

Temporal is the first approved durable engine for host-owned workflows. The graph contracts stay engine-neutral: they describe nodes, joins, bounds, and required capabilities, and Temporal is one adapter behind them. A second engine gets a second adapter without a change to the graph contract.

The adapter lives in a new top-level module with its own `go.mod`. It never lives in `core/`, `runtime/`, or `internal/`. It pins `go.temporal.io/sdk v1.49.0`, whose manifest requires `go 1.26.0` — see `https://github.com/temporalio/sdk-go/blob/v1.49.0/go.mod`. The root module stays at `go 1.24.0` (`go.mod:1-3`, module `github.com/victorzhuk/go-lispico`); the adapter's higher language requirement must not raise it. If the pin becomes stale, the rule stands and the new pin is recorded here with its manifest URL.

## Mapping

- Model and tool calls run as Activities. They are host work with host policy, host permissions, and host timeouts, and the host keeps them there.
- Human approval uses durable Signals, and every approval signal declares an explicit deadline outcome. "No answer" is a result with a value, not an indefinite wait.
- Workflow code and the Lisp-produced graph are deterministic and side-effect-free. Graph construction is pure: reading a clock, generating randomness, or calling a model during graph definition is a defect.
- Ambiguous external effects are not covered by an engine promise. Where an outcome is unknown, the host provides idempotency or reconciliation. Exactly-once is never claimed from the engine.

## Not decided here

Production hosting — self-hosted versus Temporal Cloud — store selection, data residency, and cost remain a separate adoption decision. They depend on deployment constraints this record does not have, and no configuration implied here presumes an answer.

## Consequences

- The root module's Go baseline and dependency set are unchanged by the workflow path.
- Every Temporal-specific type stays inside the adapter module; the graph contract and the host contracts see neither.
- A crash during a pending approval resumes as the same signal wait with its declared deadline intact.
- An ambiguous effect surfaces as unknown outcome plus reconciliation, never as a silent retry that may double-apply.

## Considered options

- Add the SDK to the root module: rejected — it would raise the root language baseline for every consumer of the kernel.
- Put the adapter under `internal/`: rejected — the engine boundary is a public extension point, not an implementation detail.
- Express human approval as a polling Activity: rejected — polling loses durability and makes the deadline outcome implicit.
- Let the engine promise exactly-once for ambiguous external effects: rejected — the guarantee does not exist there, and asserting it turns a recoverable unknown into a data bug.