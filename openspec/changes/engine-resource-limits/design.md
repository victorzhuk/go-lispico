## Context

A kernel-first security review (grill session, 2026-07-10) found four host-availability defects where a single input escapes every existing guard. `MaxDepth` only counts function-call and macro-expansion frames; `WithTimeout` only stops cooperative `ctx` checks that the code actually performs. Neither covers structural recursion in the reader/evaluator, an unbounded `range`, or a chunk cache that never evicts. ADR 0007 records the decision to express the missing ceilings as a per-Engine `ResourceLimits` config; ADR 0005 records that the Engine is the trust domain, and ADR 0003 requires per-evaluation state to be per-call, not shared engine fields.

## Goals / Non-Goals

**Goals:**

- No input can crash the host with a Go `fatal error: stack overflow` or an unbounded allocation.
- Ceilings are set once at `New`, immutable for the Engine's lifetime, so evaluated code cannot raise them.
- Conservative defaults make an Engine safe with no explicit limits option.
- VM and tree-walker keep agreeing on results; `core/` stays zero-import; no path panics.

**Non-Goals:**

- Tree-walker `Env` optimization (ADR 0006 defers it; the VM is the performance path).
- Changing the trust model (SEC-2 env poisoning stays a documented Engine-boundary constraint per ADR 0005).
- The backlog items (sha256 swap, fsm error swallow, `cons`/nil, `1e10` tokenize, net SSRF, lio TOCTOU, benchmark regression gate, Engine interface split).

## Decisions

- **`ResourceLimits` struct, config not constants.** A struct with `MaxReaderDepth`, `MaxStructuralDepth`, `MaxCollectionLen`, `MaxCacheEntries`, set via a `runtime.WithResourceLimits(...)` option. Zero value of a field means "use default", resolved at `New` — never "unlimited". Immutable after construction, matching `Dialect`/`MaxDepth`.
- **Reader ceiling at construction.** The reader takes no `ctx`, so the parser holds the depth limit as a field set when the parser is built; `dialect.Read` threads the Engine's `MaxReaderDepth` into the parser. `parseForm`/`parseList`/`parseVector`/`parseHashMap` increment/decrement a depth counter and return a `LispicoError` past the ceiling.
- **Evaluator ceiling per-call.** The structural-depth counter lives in the existing per-call `evalState` (already threaded through `context` for ADR 0003), incremented in `Eval`'s `Vector` case, `evalMap`, and `expandQuasiquote`. Never a shared engine field — a `-race` scenario proves isolation.
- **`range` bounded + cancellable.** Cap result length at `MaxCollectionLen` before/while building; check `ctx` periodically in the build loop, matching the pattern lio/net/exec already use.
- **Bounded cache.** Enforce `MaxCacheEntries` on the chunk cache; drop entries whose `macroEpoch` no longer matches on a lookup miss, and evict (simple size cap / LRU) when over the ceiling. Correctness is unaffected because any dropped chunk recompiles on demand.
- **One limit-exceeded error code.** Reuse or add a single `LispicoError` code for all four ceilings so embedders can classify "resource limit hit" uniformly via `errors.As`.

## Risks / Trade-offs

- **Default values.** Too low breaks legitimate deep-but-valid programs; too high leaves headroom for abuse. Pick conservative defaults (depth ~1024, collection len ~10M, cache ~4096 entries) and expose them so embedders tune per deployment. Defaults are documented, not silent.
- **Cache eviction correctness.** Dropping a live chunk mid-run must never corrupt an in-flight evaluation; eviction happens on the cache map under its existing mutex, and a `Chunk` already in a running VM is referenced by the VM, not the map, so eviction only removes the future-lookup entry. Cross-val tests guard result equality.
- **Reader signature.** Threading a limit into the parser touches `dialect.Read` and any direct `core.Read` callers (tests, bootstrap). Keep a defaulted package-level `core.Read` so existing callers compile; the Engine path passes the configured limit.
