## Context

`vm.reentryCtx` is a run-scoped memo of the context wrapper handed to
dispatched GoFuncs. Reset zeroes it; every `Engine.Call` resets. The wrapper
allocation (one `*lazyEvalStateCtx`) and the eager `armDeadline` clock read
are therefore per-top-level-call costs, dominating the callback-heavy rows'
non-construction overhead (Rule: 20.3% of bytes, 13.6% of time).

## Decision: VM-owned wrapper, generation-guarded, observation-lazy deadline

### Reuse keyed by outer-context identity

The wrapper embeds the outer `context.Context`. Reuse is legal only when the
next run's outer context is the same interface value (pointer-equal
comparison of both interface words). yagel's shape — one `context.Context`
per agent task, thousands of rule calls under it — hits this cache
essentially always. A different outer ctx allocates a fresh wrapper exactly
as today, so the change is monotone: never slower, never semantically
different on miss.

Reset paths stop clearing the wrapper; instead `Reset`/`ResetIncremental`
bump the VM's run generation and leave the wrapper for the next run to
re-arm (zero budget counters, clear the computed deadline, adopt the new
generation) — cheap field writes, no allocation.

### Generation guard

The current spec pins snapshot consistency: "retaining the context past the
call SHALL NOT expose a recycled VM's internals." A reused wrapper would
violate that raw, so the wrapper carries `gen uint64` and the VM's live
generation is compared on every state access path (`Value(evalStateKey)`,
deadline observation, budget charge). Stale generation → the access behaves
as if the wrapper carried no eval state (fresh-budget fallback on re-entry,
outer-ctx delegation for `Deadline`/`Done`/`Err`), which is exactly the
fail-safe the requirement wants: no corruption, no access to another run's
counters. The comparison is one predicted branch on paths that already do an
atomic load.

Rejected alternative — allocation per call with a smaller struct: shrinking
`lazyEvalStateCtx` saves bytes but keeps one alloc + cache-miss latency per
call and still reads the clock eagerly. Reuse removes both.

Rejected alternative — handing GoFuncs the raw outer ctx and detecting
re-entry via an engine-side lookup: breaks the documented contract that the
received ctx carries the budget (`HasEvalState`), and engine-side goroutine
correlation is exactly the kind of magic the codebase has avoided.

### Observation-lazy deadline

`armDeadline` currently runs at wrapper construction. Move the `time.Now`
behind the first of: `Deadline()` call on the wrapper, poll checkpoint that
needs a deadline comparison, or re-entrant adoption. Implementation: store
`timeout` and an unset sentinel; compute and cache the absolute deadline on
first need under the wrapper's existing atomics. The engine-level lazy-arm
contract text ("armed lazily at the first in-evaluation checkpoint") already
permits this; the change makes the code match the most lazy reading of the
words. Calls that never observe: zero clock reads — measured 8.2% of Rule
CPU returned.

### What stays eager

`SetTimeout`/`SetResourceLimits` boundary field writes are already cheap and
stay per-call: budgets are per-evaluation semantics and must re-arm. Stats
counter increments stay: `Stats()` accuracy is a separate requirement.

## Measurement plan

Interleaved benchstat (≥10 counts, one session): article Rule and Callback
rows (primary — wrapper alloc and clock read both live there), Call row
(should be flat — no GoFunc dispatch in its body), goldset both modes,
`-race` full suite, plus adversarial retention tests. Success: Rule/Callback
each lose the wrapper alloc and the `time.runtimeNow` profile line; nothing
regresses.
