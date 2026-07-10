---
status: accepted
---

# Dialects: a semantics-pluggable kernel with one immutable dialect per Engine

go-lispico grows a dialect layer so one kernel can present Common Lisp by default, the current Clojure-style surface as an opt-in dialect, and restricted rule subsets (yagel) as fail-closed dialects. A dialect is a delta — renames, additions, removals — over a declared base: the full kernel table for language dialects, or empty for restricted ones. The delta covers special forms and vocabulary alike; vocabulary is a name map onto one shared pure builtin core, with thin adapters only where semantics genuinely differ. Two semantic axes are configurable in v1: symbol namespaces (Lisp-1 vs Lisp-2) and truthiness (nil-only vs nil+false falsy). The data model is not an axis: values stay the immutable List/Vector/HashMap set; cons cells, dotted pairs, and `nil == '()` are deferred. The reader gains per-dialect feature flags (`[..]`/`{..}` literals, `#'`, `#(...)`) rather than a readtable. A dialect is selected on the Go side at Engine construction and is immutable for the Engine's lifetime; `runtime.New()` without options runs the Common Lisp dialect.

## Consequences

- The package-global `specialForms` map becomes per-Engine dispatch state expressed as canonical kernel forms under neutral names; this is the enabling refactor and lands first.
- The default flips from the current Clojure-ish flavor to Common Lisp — breaking for existing embedders, tests, and examples; the Clojure dialect must exist and yagel must pin its dialect explicitly before the flip lands.
- Lisp-2 requires a function cell in `Env` (`funcall`, `#'` in the CL dialect); truthiness becomes a single hook consulted by `if`/`when`/`and`/`or`.
- Dialect renames normalize to canonical kernel forms before compilation, so the compiler and VM stay dialect-agnostic for special-form dispatch. The VM now supports all three dialect axes (rename normalization, truthiness predicate, Lisp-2 function cell) so non-identity dialects compile and run on the bytecode VM.
- Restriction is a security boundary: a policy dialect built from the empty base can never silently inherit a future kernel form. Evaluated code cannot change the running dialect, so rule code cannot lift its own restrictions.
- One shared builtin core stays the stdlib-completeness workstream from ADR 0004; dialects add names, not implementations.

## Considered options

- Surface-only aliasing over the existing kernel: rejected — cannot express Lisp-2 or CL truthiness, so "supports Common Lisp" would be cosmetic.
- Full ANSI CL semantics as the one language: rejected — cons cells, packages, and the condition system rewrite the 13-type immutable model and contradict the kernel-first mission (ADR 0004).
- Complete per-dialect form tables and stdlibs: rejected — CL, Clojure, and yagel would duplicate ~20 shared forms and every collection builtin; drift between copies becomes a standing bug class.
- Lisp-side dialect switching (`use-dialect`): rejected — syntax changes race concurrent `Eval` on one Engine (ADR 0003) and let policy code escalate its own dialect.
- Readtable/reader macros in v1: deferred — user-extensible reading is unwarranted surface in an embedded policy context until a consumer needs it.
