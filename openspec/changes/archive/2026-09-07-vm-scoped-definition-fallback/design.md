## Context

See `proposal.md` for the lexical-definition failure at commit `3bcf9c1`. `compileDef` always emits `OpSetGlobal`. The Lisp-2 branch of `compileDefn` emits `OpSetFunc`; called closures use their captured globals as the frame environment. The tree-walker instead defines names in the current child environment.

The archived `vm-flat-closures` design acknowledges this divergence and requires forms needing name-addressable lexical environments to fall back before execution. `runtime/eval.go` already handles `compiler.CodeUnsupported` in both `Eval` and `EvalCached`.

## Goals / Non-Goals

**Goals:** Preserve lexical definition effects through existing whole-form fallback; keep unaffected compiled functions on flat local/capture storage.

**Non-Goals:** Add an environment to every compiled call, invent a runtime scope API, implement native dynamic lexical definitions, or change ordinary top-level definition behavior.

## Decisions

1. Use lexical scope, not syntax depth or the number of current bindings, to decide support. Functions, `let`, `let*`, `loop` and catch bodies create lexical scopes even when they bind nothing. Top-level `do`, `if` and other expressions that introduce no scope retain the top-level definition target. Build on the scope bookkeeping from `vm-local-stack-scope`.
2. Refuse scoped `def` and scoped `defn` with existing `unsupportedErr`/`CodeUnsupported`, after ordinary shape validation. Apply the rule to Lisp-2 `compileDefn` as well as the Lisp-1 route through `compileDef`. Propagate the refusal to the enclosing top-level compilation; never execute a partially compiled prefix or resume fallback inside a VM frame.
3. Keep `Eval` and `EvalCached` on their existing whole-form fallback path. Any language-level side effects from evaluating the form occur once, against the original environment. The fallback must not retry the form after bytecode execution has begun. Existing macro-expansion policy is unchanged.
4. Keep ordinary top-level definitions compiled, including top-level `do` definitions. This limits the performance change to forms whose lexical definition semantics cannot be represented safely. Retain current restrictions on nested `defmacro`; this change does not broaden macro support.
5. Document compiled-subset narrowing in the existing architecture and disposition/default ADRs. No replacement environment architecture or standalone ADR is needed.

## Risks / Trade-offs

- A fallback-created function is a tree-walker `Lambda` → verify `Eval`, named calls and returned function application under the VM engine, including repeated evaluation.
- Empty scopes and Lisp-2 function cells can evade a naive guard → test both binding shapes, both shipped dialects and direct function-cell lookup after a scoped definition.
- Fallback can change performance and reduction counts → preserve terminal resource contracts and retain unaffected compiled functions; disclose the fallback surface.
- Compiling before fallback could appear to duplicate side effects → pin execution-side counters and verify no partial bytecode prefix executes.

## Migration Plan

Implement and archive `vm-local-stack-scope` first. Add red scope/fallback tests, add targeted compiler refusal, verify runtime dispatch and once-only side effects, then update existing docs and changelog. No persistent data migration. `WithTreeWalker()` provides the same language semantics throughout.
