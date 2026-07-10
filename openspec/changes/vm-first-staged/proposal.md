# VM-First, Staged

## Why

ADR 0006 commits the bytecode VM to becoming the performance path for every dialect. Today it cannot be: it recompiles and allocates a fresh machine per `Eval` (13x slower than the tree-walker on repeated file loads), dispatches `+`/`-`/`<` through full `GoFunc` calls in its best-case hot loops, mirrors every local write into a heap map, and hard-errors for any non-identity dialect — so the default Common Lisp engine and fail-closed restricted dialects can never use it.

## What Changes

- Native arithmetic/comparison opcodes (`+ - * / < > <= >= =`) with exact stdlib promotion semantics and a rebinding guard that falls back to the builtin call.
- Slot-only locals: capture analysis in the compiler; only closure-captured variables materialize in an `Env`, the mirror map for every local write goes away.
- Per-Engine compiled-chunk cache keyed by source, dialect, and macro epoch; macro redefinition invalidates. Replaces the spec's "no cache, compile per call" posture.
- Dialect-axis execution: rename normalization to canonical kernel forms before compilation, truthiness consulted through the dialect hook, Lisp-2 head-position resolution via the function cell. The `IsIdentity()` construction gate is removed — `WithBytecode()` composes with any resolvable dialect. **BREAKING** for code relying on that construction error.
- Fresh end-to-end benchmarks (one-shot eval, file load, hot loops) recorded per stage; the default flip to VM is explicitly deferred to a later change gated on the VM beating the tree-walker end-to-end (ADR 0006 staging).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: execution requirement loses "no cache / no carried state" wording (results stay isolated, chunks may be cached); gains native opcodes, slot-resident locals, chunk cache, and dialect-axis execution requirements.
- `runtime-api`: `WithDialect` no longer restricted to the identity dialect when combined with `WithBytecode()`.
- `dialect`: resolved dialects expose canonical-name normalization for compilation (delivers ADR 0005 consequence 4).

## Impact

- Code: `core/vm/` (opcodes, dispatch, frames), `core/compiler/` (capture analysis, normalization, opcode emission), `core/dialect.go` (normalization surface), `runtime/eval.go` (chunk cache, VM reuse), `runtime/engine.go` (gate removal), benchmarks throughout.
- ADRs: implements ADR 0006; delivers ADR 0005 consequence 4; ADR 0002 already marked superseded.
- Out of scope: flipping the default evaluator (separate change once benchmarks prove), tree-walker Env redesign, REPL binary.
