---
status: accepted
---

# One Engine is safe for concurrent evaluation; per-evaluation state is not shared

Multiple goroutines may evaluate on a single Engine concurrently — this is a supported contract, not just an aspiration. Consequently, state that belongs to one evaluation — macro-expansion depth, call depth, and the `recur`/loop counter — must live per invocation, not as fields on the shared evaluator. A future reader will see this state threaded through the call path and should know it is deliberate: sharing it as engine fields lets one goroutine's loop satisfy another goroutine's stray `recur`, and races the plain-int macro depth.

## Consequences

- The evaluation entry points carry per-call depth/loop state rather than reading it from the shared `engine` struct.
- Environments remain individually synchronized; this ADR governs the per-evaluation counters, which environment locking does not cover, and the derived state below.
- A registration view holds no bindings of its own: its reads and writes forward to its root and take the root's lock, and the registration journal is updated under that same lock as the mutation it records.
- Derived state a value computes for itself is exempt from the immutability invariant, and the exemption is what keeps that invariant meaningful rather than weakening it. A large hash map converts its builder storage to a trie on the first update and keeps the result: the converted trie is a pure function of `large.m` as it stands when the memo is published, and any write to `large.m` clears the memo, so a live memo always matches the map it was derived from. It is published atomically with a compare-and-swap, and no observable — value, equality, iteration order, printing, `Len` — differs before or after. A goroutine that reads the map concurrently either sees the memo or recomputes an equal trie; neither outcome is distinguishable from the other. Only state with this shape may be shared across evaluations. Per-evaluation state, which is observable and not derivable, may not.
