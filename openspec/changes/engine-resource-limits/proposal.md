## Why

Adversarial or accidental input can exhaust the host through paths the existing `MaxDepth` (function-call/macro depth) and `WithTimeout` (cooperative cancellation) guards do not cover. Verified during a kernel-first security review, grounded in ADR 0007 (resource limits) and ADR 0005's Engine-is-the-trust-domain note:

- **Reader stack overflow (CRITICAL, reproduced).** `core/reader.go` `parseForm`↔`parseList`/`parseVector`/`parseHashMap` recurse with no depth counter and take no `ctx`. `strings.Repeat("(", 2_000_000)` triggers a Go `fatal error: stack overflow` — uncatchable by `recover`, killing the whole host process. Parsing runs before any eval guard (`dialect.Read` is called unconditionally), so `WithMaxEvalDepth`/`WithTimeout` give zero coverage.
- **Evaluator structural recursion (HIGH).** `Eval`'s `Vector` case, `evalMap`, and `expandQuasiquote` recurse on nested literals/quasiquote with no counter — `MaxDepth` only increments on function calls and macro expansion, so it never engages. Same fatal-overflow class as the reader.
- **`range` memory bomb (HIGH).** The `range` builtin builds an unbounded `[]Value` with no length cap and no `ctx.Done()` check in the loop — a single expression exhausts host memory, un-cancellable by `WithTimeout`.
- **Unbounded chunk cache (MEDIUM-HIGH).** The bytecode chunk cache never evicts; every top-level `defmacro` bumps the macro epoch, orphaning entries that accumulate forever — unbounded heap growth that threatens the <10MB/Engine target.

These are host-availability defects that hit trusted rule code by accident as readily as adversarial input, so they are must-fix regardless of the trust model.

## What Changes

- Add a `ResourceLimits` struct — reader nesting depth, evaluator structural depth, collection length, chunk-cache size — set once at Engine construction via a `runtime.New()` option, immutable for the Engine's lifetime so evaluated code cannot raise its own ceilings.
- Reader enforces its nesting-depth ceiling, baked into the parser at construction (the reader has no `ctx`), failing closed with a `*core.LispicoError` read error instead of a fatal stack overflow.
- Evaluator enforces a structural-depth ceiling on `Vector`/`HashMap` literal descent and quasiquote expansion, via a counter in the per-call `evalState` threaded through `context` (ADR 0003), never a shared engine field.
- `range` caps its result length and checks `ctx` cooperatively, returning a clean error past the ceiling.
- The chunk cache is bounded (size cap; stale-macro-epoch entries dropped on lookup miss), so a long-lived VM Engine stays within its memory budget.
- Conservative defaults ship so an Engine built with no limits option is safe; the zero value of unset per-field limits means "use the default," never "unlimited".

## Capabilities

### New Capabilities

None. The limits attach to existing capabilities rather than forming a standalone surface.

### Modified Capabilities

- `core-engine`: gains a resource-ceiling requirement — reader nesting depth and evaluator structural depth are bounded, failing closed with a typed error and never a fatal stack overflow.
- `runtime-api`: gains a `ResourceLimits` construction option, immutable after `New`, that carries the ceilings into reader, evaluator, and stdlib.
- `stdlib-plugin`: `range` gains a bounded, cancellable requirement.
- `bytecode-vm`: the compiled-chunk-cache requirement gains a bounded-size clause so it cannot grow without limit.

## Impact

- Code: `core/reader.go` (parser depth guard + construction-time limit), `core/eval.go` (structural-depth counter in `evalState`, `Vector`/`evalMap`/`expandQuasiquote`), `core/error.go` (limit-exceeded error code if a new one is warranted), `plugins/stdlib/collections.go` (`range` cap + `ctx`), `runtime/engine.go` + `runtime/eval.go` (`WithResourceLimits` option, wiring, bounded cache), `core/dialect.go` (`Read` carries the reader limit).
- ADRs: implements ADR 0007; honors ADR 0003 (per-call state) and ADR 0005 (Engine trust boundary).
- Invariants preserved: `core/` zero external imports; no panics (the fatal stack overflow is removed, not converted to a panic); VM and tree-walker keep agreeing on results.
- Out of scope: tree-walker Env optimization (ADR 0006 defers it), and the backlog items (sha256 swap, fsm error swallow, `cons`/nil, `1e10` tokenize, net SSRF, lio TOCTOU, benchmark regression gate, Engine interface split).
