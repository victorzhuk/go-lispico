---
status: accepted
---

# Resource limits are a per-Engine config struct, not hardcoded constants

Adversarial or accidental input can exhaust the host through paths that the existing `MaxDepth` (function-call/macro depth) and `WithTimeout` (cooperative cancellation) guards do not cover: the reader recurses on nesting with no counter and no `ctx`, so `2M` unmatched `(` triggers a Go `fatal error: stack overflow` — uncatchable by `recover`, killing the whole process; the evaluator's `Vector`/`HashMap`/quasiquote descent has the same unbounded recursion with no counter; `range` builds an unbounded slice with no length cap or `ctx` check; and the bytecode chunk cache never evicts. We add a `ResourceLimits` struct — reader nesting depth, evaluator structural depth, collection length, chunk-cache size — set at Engine construction via `New()` options and threaded into the reader, evaluator, and stdlib, each ceiling failing closed with a clean error rather than a fatal crash. It is a public config struct now (not per-site constants) because these limits are the safety contract an embedder tunes per deployment, and because ADR 0004's kernel-first consumer runs semi-trusted rule code where a wrong default is a host-availability incident.

## Consequences

- The reader takes its depth ceiling at construction (it has no `ctx` and runs before any eval guard); the evaluator's structural-depth counter lives in the per-call `evalState` threaded through `context`, consistent with ADR 0003 — never as a shared engine field.
- `ResourceLimits` is immutable for the Engine's lifetime, like `Dialect` and `MaxDepth`; evaluated code cannot raise its own ceilings.
- Distinct from `MaxDepth`: that bounds function-call and macro-expansion depth per evaluation; `ResourceLimits` bounds structural recursion, collection size, and cache growth. The two are not merged.
- Fail-closed is mandatory: every ceiling returns a `ReadError`/eval error; none may panic or let the process reach a fatal stack overflow.

## Considered options

- Hardcoded constants at each site: rejected — the values are a security contract an embedder needs to set per deployment, and burying them makes the fail-closed guarantee invisible at the API surface.
- `ctx`-cancellation only, no hard caps: rejected — a tight recursive descent overflows the Go stack before any `ctx.Err()` check can fire, so cancellation alone does not fix the reader crash.
- Rely on the OS/cgroup memory limit: rejected — an OOM kill is the same host-availability failure this ADR exists to prevent, just one layer down, and gives no clean error to the embedder.
