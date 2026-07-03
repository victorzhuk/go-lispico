---
status: accepted
---

# One Engine is safe for concurrent evaluation; per-evaluation state is not shared

Multiple goroutines may evaluate on a single Engine concurrently — this is a supported contract, not just an aspiration. Consequently, state that belongs to one evaluation — macro-expansion depth, call depth, and the `recur`/loop counter — must live per invocation, not as fields on the shared evaluator. A future reader will see this state threaded through the call path and should know it is deliberate: sharing it as engine fields lets one goroutine's loop satisfy another goroutine's stray `recur`, and races the plain-int macro depth.

## Consequences

- The evaluation entry points carry per-call depth/loop state rather than reading it from the shared `engine` struct.
- Environments remain individually synchronized; this ADR only governs the per-evaluation counters, which environment locking does not cover.
