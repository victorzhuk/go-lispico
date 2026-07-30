# chunk-cache-contention

## Why

A full lock inventory across `core/`, `core/vm/`, and `runtime/` shows the
sharding axis is mostly a non-issue: `Env.mu` (`core/env.go:61`) is per-scope
and naturally sharded by the environment chain; `Engine.Call`'s name-resolution
cache (`runtime/call_cache.go:38`) and the per-chunk global-read site cache
(`core/vm/chunk.go:101,140`) are already lock-free atomic-pointer
copy-on-write; the stdlib template registry (`runtime/lazy_template.go:62`)
and the bootstrap artifact cache are first-build-only, not steady-state
contention; `limitMeter`'s CAS loops (`runtime/meter.go:78-132`) are amortized
to roughly one per 1024 reductions by the existing lease mechanism.

One real global serialization point remains: `bytecodeEvaluator.mu`
(`runtime/eval.go:45`), a single per-engine `sync.Mutex` guarding the compiled
chunk cache map and its intrusive LRU list, taken three times per
`EvalCached` call (probe at `eval.go:356-358`, macro-epoch flush at
`:371-373`, admit at `:395-402`). Given ADR 0003's shared-Engine concurrency
model and the one-engine-process-wide usage pattern this repo's own
consumer facts record, every concurrent evaluation on a shared Engine
serializes through this one mutex.

Nothing in the repository currently measures whether this is load-bearing.
The gold-set corpus has no concurrent benchmark cell, and no mutex or block
profile of `EvalCached` under concurrent load exists anywhere in the repo's
history. This change's first task is producing that measurement before
proposing a fix — striping a cache that carries engine-wide
`MaxCacheBytes`/`MaxCacheNodes` budgets (ADR 0007) is real complexity that a
speculative "shard because mutexes are generically bad" instinct should not
purchase on its own.

## What Changes

- Task 0 is a gate: add a concurrent gold-set benchmark cell (with its own
  committed tier — `cmd/perfgate` fails any cell with no tier) exercising
  multiple goroutines calling `EvalCached` concurrently on one shared Engine,
  and measure `bytecodeEvaluator.mu` contention with `-mutexprofile` and
  `-blockprofile` at `GOMAXPROCS` 2, 8, and 24.
- If contention is not load-bearing at any of those concurrency levels:
  record the measured numbers in `design.md` and close this change
  unimplemented. That is a valid, correctly-evidenced outcome — the repo's
  own convention (Stage-program "closed as not worth doing, with evidence")
  applies here exactly as it did to the constructor-asymmetry and
  AddConstant-dedup findings in the prior perf program.
- If it is load-bearing: stripe the chunk cache by a hash of its cache key
  (source hash, dialect fingerprint, macro epoch, form index), each stripe
  with its own mutex and its own intrusive LRU. The central design decision,
  to be resolved during implementation and recorded in `design.md`: how
  `MaxCacheBytes`/`MaxCacheNodes` stay engine-wide budgets under
  per-stripe locking — either an atomic global running total checked
  against on insert (cross-stripe eviction ordering becomes approximate) or
  fixed per-stripe quotas with a documented tolerance (simpler, less exact).
  Pick by measurement, not preference.
- Explicitly out of scope: `engineImpl.mu` (`runtime/engine.go:65`), which
  wraps a write-once `rootEnv` field in an unnecessary `RWMutex` round-trip
  at five call sites — that is pending `engine-lean-call-boundary`'s "atomic
  rootEnv" item, not this change's.

## Impact

- Affected specs: `bytecode-vm` (new requirement, conditional: concurrent
  chunk-cache access MAY be sharded; every correctness and budget-enforcement
  invariant of the existing "Compiled-chunk cache" requirement SHALL hold
  regardless of internal locking granularity).
- Affected code, if gated in: `runtime/eval.go` (`bytecodeEvaluator.mu`,
  cache map, LRU list), `runtime/meter.go` (if budget accounting needs an
  atomic global total).
- Risk: incorrect cross-stripe budget enforcement is the main hazard — a
  cache that silently exceeds `MaxCacheBytes` across stripes would violate an
  existing, tested invariant. The design decision above exists precisely to
  avoid that.
- Expected: if gated in, reduced tail latency under concurrent Engine use at
  no cost to single-goroutine throughput; if gated out, a documented,
  measured "not currently load-bearing" record replaces speculation.
