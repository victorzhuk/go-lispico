---
status: accepted
---

# Dialects: a semantics-pluggable kernel with one immutable dialect per Engine

go-lispico grows a dialect layer so one kernel can present Common Lisp by default, the current Clojure-style surface as an opt-in dialect, and restricted rule subsets (yagel) as fail-closed dialects. A dialect is a delta — renames, additions, removals — over a declared base: the full kernel table for language dialects, or empty for restricted ones. The delta covers special forms and vocabulary alike; vocabulary is a name map onto one shared pure builtin core, with thin adapters only where semantics genuinely differ. One semantic axis is configurable in v1: symbol namespaces (Lisp-1 vs Lisp-2). Truthiness is not an axis: the fixed package-level `core.IsTruthy` rule applies under every dialect — `nil` and `false` are falsy, everything else truthy. The data model is not an axis: values stay the immutable List/Vector/HashMap set; cons cells, dotted pairs, and `nil == '()` are deferred. The reader gains per-dialect feature flags (`[..]`/`{..}` literals, `#'`, `#(...)`) rather than a readtable. A dialect is selected on the Go side at Engine construction and is immutable for the Engine's lifetime; `runtime.New()` without options runs the Common Lisp dialect.

## Amendment

Truthiness is not an axis at all. `core.Dialect.NilOnlyFalsy()` and
`Dialect.TruthyFunc()` were removed; truthiness is the fixed package-level
`core.IsTruthy` rule for every dialect: `nil` and `false` are falsy,
everything else truthy, including under Common Lisp. The one
configurable semantic axis is symbol namespaces (Lisp-1 vs Lisp-2);
`if`/`when`/`and`/`or` consult `core.IsTruthy` directly.

Construction moved from the builder chain to one call: `core.NewDialect(core.DialectSpec)`
validates, resolves, and fingerprints a plain-data spec in one step and returns
a frozen `core.Dialect` — one pointer to immutable state — or an error and the
zero `Dialect`. The zero `Dialect` is the identity dialect: the full kernel
table under canonical names, Lisp-1, default reader axes, no vocabulary. An
invalid spec (unknown kernel form, a name both hidden and mapped, a hidden
name absent from the base — any `Hide` on an empty base — a Lisp-2 spec
mapping `funcall`/`function`, an adapter without an ID or a value, a name in
both `Vocab` and `Adapters`) is refused at `NewDialect`, not discovered at
`runtime.New`. The fingerprint hashes the resolved configuration — base,
axes, form table, vocabulary presence, vocab entries, adapter IDs — not how
the spec built it, so two specs that resolve alike fingerprint alike; `cl` and
`clojure` build their stock dialects from static `DialectSpec` values.
`FullDialect`, `EmptyDialect`, `Memoized`, and the builder methods (`Add`,
`Rename`, `Remove`, `Lisp2`, `FlatCond`, `WithoutBracketLiterals`,
`WithFunctionRef`, `WithReaderVector`, `Vocabulary`, `WithAdapter`) are
removed.

## Consequences

- The package-global `specialForms` map becomes per-Engine dispatch state expressed as canonical kernel forms under neutral names; this is the enabling refactor and lands first.
- The default flips from the current Clojure-ish flavor to Common Lisp — breaking for existing embedders, tests, and examples; the Clojure dialect must exist and yagel must pin its dialect explicitly before the flip lands.
- Lisp-2 requires a function cell in `Env` (`funcall`, `#'` in the CL dialect).
- Dialect renames normalize to canonical kernel forms before compilation, so the compiler and VM stay dialect-agnostic for special-form dispatch. The VM now supports the dialect axes (rename normalization, Lisp-2 function cell) so non-identity dialects compile and run on the bytecode VM.
- Restriction is a security boundary: a policy dialect built from the empty base can never silently inherit a future kernel form. Evaluated code cannot change the running dialect, so rule code cannot lift its own restrictions.
- One shared builtin core stays the stdlib-completeness workstream from ADR 0004; dialects add names, not implementations.
- The isolation unit is the Engine, not the individual `Eval` call. The dialect boundary above confines which *forms* rule code can reach; it does not isolate ordinary top-level bindings between calls on a shared Engine. `Engine.Eval` mutates the shared root env, so a top-level `def`/`set!` in one call persists and is visible to every later or concurrent call on that Engine — this is intended REPL state, not a cross-call isolation break. Consequently, code from different trust levels must run on separate Engines; a fresh Engine is cheap (~124B, ~123µs boot with stdlib). An embedder must never share one Engine across a trust boundary and rely on `Eval`-call isolation, because there is none.

## Considered options

- Surface-only aliasing over the existing kernel: rejected — cannot express Lisp-2 or CL truthiness, so "supports Common Lisp" would be cosmetic.
- Full ANSI CL semantics as the one language: rejected — cons cells, packages, and the condition system rewrite the 13-type immutable model and contradict the kernel-first mission (ADR 0004).
- Complete per-dialect form tables and stdlibs: rejected — CL, Clojure, and yagel would duplicate ~20 shared forms and every collection builtin; drift between copies becomes a standing bug class.
- Lisp-side dialect switching (`use-dialect`): rejected — syntax changes race concurrent `Eval` on one Engine (ADR 0003) and let policy code escalate its own dialect.
- Readtable/reader macros in v1: deferred — user-extensible reading is unwarranted surface in an embedded policy context until a consumer needs it.
