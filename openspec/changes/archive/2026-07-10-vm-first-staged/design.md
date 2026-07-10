# Design — vm-first-staged

## Context

ADR 0006. Measured baseline: file load 11.6ms VM vs 0.87ms tree-walker (fresh
machine + recompile per call); arithmetic loop ~2 allocs/iter through `GoFunc`
dispatch; every VM closure call allocates an `Env` map mirroring stack slots;
`IsIdentity()` gate excludes the default CL dialect and all restricted dialects.

## Goals / Non-Goals

- Goals: the four workstreams (opcodes, slots, cache, axes) with parity preserved and benchmarks recorded per stage.
- Non-Goals: flipping the default evaluator (separate change, benchmark-gated); tree-walker Env redesign; new language features; on-disk cache.

## Decisions

- **Staging order: opcodes + slots → axes → cache.** Opcodes and slots are pure
  VM-internal wins with existing parity tests as the safety net. Axes unlock real
  consumers (CL default, restricted dialects). Cache lands last because its
  correctness story (invalidation) depends on a stable compile pipeline.
- **Arithmetic opcode rebinding guard: compile-time shadow check + runtime
  identity check.** The compiler emits a native opcode only when the operator
  symbol is not locally shadowed; the opcode verifies at runtime that the global
  binding is still the canonical stdlib builtin (pointer identity against the
  registered `GoFunc`), else falls back to the call path. Alternative — declaring
  rebinding unsupported under bytecode — rejected: silently diverging from the
  tree-walker breaks the parity invariant, the one rule ADR 0002/0006 both keep.
- **Capture analysis in the compiler, not the VM.** A pre-pass marks locals
  referenced by inner `fn`/`defmacro` bodies; only those compile to env-backed
  storage, the rest are slot-only. `OpSetLocal` stops mirroring into the env.
  Alternative — runtime escape tracking — rejected: pays per-call cost to answer
  a static question.
- **Rename normalization at read/compile boundary.** The resolved dialect already
  owns the visible→canonical name table; a normalization step rewrites head
  symbols before the compiler sees them. Compiler and VM stay dialect-agnostic
  for vocabulary, exactly as ADR 0005 consequence 4 described. Removed forms are
  rejected during resolution, before normalization could resurrect them.
- **Truthiness and Lisp-2 as VM parameters.** The VM takes the dialect's
  truthiness predicate (single function pointer consulted by conditional opcodes)
  and a lisp2 flag switching head-position resolution to the function cell
  (`OpGetFunc`). Mirrors how the tree-walker consumes the same axes — one source
  of truth per axis.
- **Chunk cache: per-Engine `map[cacheKey]*Chunk` behind a mutex, keyed by
  (source hash, dialect fingerprint, macro epoch).** The engine increments a macro
  epoch on every `defmacro` at the root env; an epoch mismatch is a miss. Whole-
  epoch invalidation over per-macro dependency tracking: redefinition is rare
  (REPL/dev), correctness is trivial to reason about, and a miss just recompiles.
  Unbounded map accepted for v1 — embedders evaluate a bounded set of sources;
  revisit if a consumer streams unbounded distinct sources.
- **VM instance reuse.** With chunks cached, `runtime/eval.go` keeps a pooled or
  per-call-but-cheap VM; stacks/frames reset between runs. Concurrency contract
  unchanged: a VM instance is single-use-at-a-time; concurrent `Eval` gets
  separate instances (ADR 0003).

## Risks / Trade-offs

- [Opcode semantics drift from stdlib builtins] → parity corpus extended with promotion/division/comparison edge cases run under both evaluators; builtin stays the reference implementation.
- [Capture analysis misses an escape path (quasiquoted `fn`, macro-generated closures)] → analysis runs post-macro-expansion on canonical forms; anything unanalyzable marks the frame fully env-backed (conservative fallback).
- [Cache returns stale chunk after macro redefinition through a non-root env] → macros are root-defined by the kernel today; epoch bump hooks the single `defmacro` path. If nested `defmacro` ever compiles (currently a documented fallback), it bumps the same epoch.
- [Benchmark gate never met, default never flips] → acceptable by design; ADR 0006 keeps the tree-walker default until evidence says otherwise.

## Migration Plan

Land per stage behind the existing `WithBytecode()` opt-in; each stage keeps
`go test ./...` and the parity corpus green. Gate removal (axes stage) is the only
observable API change — construction that previously errored now succeeds.
Rollback = revert the stage's commits; no data or config migration.

## Open Questions

None — the default-flip decision is explicitly a later change with fresh numbers.
