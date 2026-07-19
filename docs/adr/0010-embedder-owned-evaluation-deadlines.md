---
status: accepted
---

# A fully covering embedder may own evaluation deadlines

The Engine retains its safe 30-second default, but it does not create a redundant timer when the caller already has an earlier deadline. An embedder may select `WithTimeout(0)` only after it applies a deadline to every evaluation lifecycle; YAGEL does so once the separate Rule-load and handler-dispatch deadlines from YAGEL ADR 0042 cover all paths. Keeping both timers was rejected because the hidden Engine limit cannot express YAGEL's distinct lifecycles, while removing the Engine default globally was rejected because ordinary embedders still need a safe default.

## Amendment

The Engine deadline is now enforced by bounded-interval in-evaluation checks that compare a precomputed instant, not by a per-call `context.WithTimeout`. Both evaluators carry the instant alongside the caller's context and compare against it at their batched cancellation checkpoints, instead of racing a timer goroutine against every call.

Consequence: a `GoFunc` now receives the caller's own context, unwrapped — it no longer observes the Engine's deadline as a context deadline, only as the eventual error the evaluator returns once the GoFunc completes. A GoFunc blocking on external work (a network call, a file read) is bounded by the caller's context, not interrupted mid-call by the Engine deadline. All I/O plugins are frozen or idle (ADR 0004), so no active consumer relies on mid-GoFunc engine cancellation.