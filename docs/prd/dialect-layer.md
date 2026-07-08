# Dialect layer — PRD

## Problem statement

go-lispico embedders get exactly one language surface, and it is a Clojure-flavored one baked into the kernel: a Lisp-1 with `nil`/`false` both falsy, immutable data, and a fixed set of special-form names. An embedder who wants their host to speak Common Lisp — `defun`, `setq`, `funcall`, `#'fn`, `nil`-only truthiness — cannot get it without forking the evaluator. An embedder who wants the opposite, a *smaller* language than the kernel ships (a locked-down rule subset like yagel, where only an approved handful of forms are callable and no future kernel form can leak in), has no boundary to express that against either. Today the special-form table is a package-global `map`, so even if two Engines wanted different surfaces in one process they would share one. The language the kernel speaks is a compile-time constant, and the mission calls for it to be a per-embedder choice.

## Solution

Introduce a **Dialect**: a named language configuration an Engine runs, fixed at construction. A Dialect is a **Delta** — renames, additions, removals — over a declared base, plus a small set of **Semantic axes**, reader feature flags, and a vocabulary name map. The base is either the full **Kernel table** (for language dialects that extend the kernel) or empty (for fail-closed restricted dialects that allowlist up from nothing).

Three dialects ship: Common Lisp (the new default of `runtime.New()`), Clojure (the current flavor, extracted and named so nothing is lost), and a restricted-subset base that yagel-style policy dialects build on. An embedder picks one on the Go side — `runtime.New(log, WithDialect(cl.Dialect()))` — and it is immutable for that Engine's lifetime. Evaluated code cannot change the dialect it runs under, so a restricted dialect is a real boundary, not a convention.

Two semantic axes are configurable in v1: symbol namespaces (Lisp-1 vs Lisp-2, the latter adding a function cell to the environment plus `funcall`/`#'`) and truthiness (`nil`-only falsy vs `nil`+`false` falsy). The data model stays fixed — immutable `List`/`Vector`/`HashMap`, no cons cells — so "Common Lisp" here means CL vocabulary, syntax, and these two semantics, not the full ANSI object and condition system.

## User stories

1. As an embedder, I want `runtime.New(log)` to give me a Common Lisp surface by default, so that lispico "speaks Common Lisp" without extra wiring.
2. As an embedder, I want `runtime.New(log, WithDialect(clojure.Dialect()))` to give me exactly today's flavor, so that existing scripts and my existing embedding keep working after the default flips.
3. As an embedder writing CL, I want `defun`, `setq`, `progn`, `lambda`, and CL truthiness where only `nil` is falsy, so that textbook CL forms evaluate as written.
4. As an embedder writing CL, I want a separate function cell — `funcall`, `#'name` — so that a variable and a function may share a name the way CL programs assume (Lisp-2).
5. As an embedder writing Clojure-style code, I want `nil` and `false` both falsy and a single namespace, so that the Lisp-1 behavior I rely on today is preserved under the named Clojure dialect.
6. As a host author embedding rule/policy code (yagel), I want to define a dialect that starts from an empty base and allowlists only the forms I approve, so that a future kernel form can never silently become callable inside a policy.
7. As a host author, I want the running dialect fixed at Engine construction and unchangeable from evaluated code, so that rule code cannot lift its own restrictions at runtime.
8. As an embedder, I want CL reader affordances — `#'fn`, `#(...)`, and no `[..]`/`{..}` literals — toggled per dialect, so that CL snippets parse and Clojure snippets keep their literals.
9. As an operator running many Engines in one process, I want each Engine's dialect independent, so that a CL Engine and a Clojure Engine never share a special-form table.
10. As a dialect author, I want to express my dialect as a delta (rename/add/remove) over a base rather than a full table, so that the ~22 shared forms are defined once and my dialect stays small.
11. As a maintainer, I want dialects to add *names*, not reimplement builtins, so that one shared pure-builtin core remains the single place `map`/`filter`/`reduce` live.
12. As an embedder on the default CL dialect and default semantic axes, I want the bytecode VM still available, so that hot loops keep their optimization where the dialect maps cleanly to canonical forms.

## Implementation decisions

- **Per-Engine dispatch (enabling refactor, lands first).** The package-global `specialForms` map in `core/eval.go` becomes per-Engine state. The Evaluator dispatches head symbols through the Engine's resolved table rather than a global, so two Engines in one process can run different dialects. This refactor is behavior-preserving on its own and is sequenced before any dialect ships.
- **Kernel table under neutral names.** The kernel defines its canonical special forms once under neutral names. A Dialect is a Delta resolved against a base — the full Kernel table (CL, Clojure) or empty (restricted) — producing that Engine's effective table. Renames normalize to canonical forms.
- **Selection API.** A `WithDialect(...)` Engine option, in the same family as the existing `WithBytecode`/`WithMaxEvalDepth`/`WithTimeout`. The dialect is captured into the Engine at `New` and is immutable thereafter. No Lisp-side dialect switch exists.
- **Default flip.** `runtime.New()` with no dialect option resolves to the Common Lisp dialect. The current Clojure-ish flavor is extracted as an explicit `clojure` dialect. This is breaking; it is acceptable while the project is alpha (v0.3.0), and yagel plus the existing suite pin their dialect explicitly as part of the change.
- **Semantic axes.** Truthiness is a single hook consulted by `if`/`when`/`unless`/`cond`/`and`/`or`/`not` — `nil`-only for CL, `nil`+`false` for Clojure. Lisp-2 adds a function cell to `core/env.go` and changes head-symbol resolution in `evalList` to consult it; `funcall` and `#'` exist only in the CL dialect. The data-representation and immutability axes are explicitly *not* configurable in v1.
- **Vocabulary.** One shared pure-builtin core (the stdlib-completeness workstream from ADR 0004). A Dialect's vocabulary is a name map onto that core — `car`→first-impl, `cdr`→rest-impl — with thin adapters only where semantics genuinely differ (e.g. CL `mapcar` argument order). Dialects add names, never implementations.
- **Reader feature flags.** A per-dialect reader options struct toggles `[..]`/`{..}` literals, `#'` function-quote, and `#(...)` vector sugar. No readtable / user reader macros in v1.
- **VM interaction.** Vocabulary renames normalize to canonical forms before compilation, so the compiler and VM stay dialect-agnostic for names. An Engine running non-default semantic axes (Lisp-2, or CL truthiness) falls back to the tree-walker until the VM learns those axes; the tree-walker remains the default and complete path.

See `docs/adr/0005-dialect-layer.md` for the decision record and `CONTEXT.md` for the vocabulary (Dialect, Semantic axis, Kernel table, Delta).

## Testing decisions

- **Flow: TDD (test-first).** The enabling refactor must preserve current evaluator behavior exactly, and the CL-default flip is breaking — so characterization tests pin today's behavior *before* the global-to-per-Engine refactor moves it, and each new axis (Lisp-2 function cell, CL truthiness) gets a red-green cycle. This matches the repo's already-thorough suite.
- **Seam: the `runtime` Engine, black-box.** One seam anchors the whole feature: `New(log, WithDialect(...))` then `Eval(ctx, source, input)`, asserting on the returned `core.Value`. It exercises selection, semantic axes, vocabulary mapping, and reader flags end-to-end as observable language behavior. Prior art: `runtime/integration_test.go`, `runtime/engine_test.go`, `runtime/fallback_test.go`.
- **What good tests cover.** Per dialect: a form that is renamed (CL `defun` evaluates; the Clojure name for it does *not* resolve in the CL dialect), a truthiness divergence (`(if nil ...)` vs `(if false ...)` differ between CL and Clojure), a Lisp-2 case (`funcall`/`#'` work in CL, absent in Clojure), and a restricted dialect that rejects a kernel form its allowlist omits. Cross-Engine isolation: two Engines with different dialects in one test never bleed into each other. A default-flip characterization test asserts `runtime.New()` now behaves as CL.
- **Lower-level supplements (not the anchor).** Delta resolution and name-map building may get focused unit tests at the dialect-package level, and evaluator-axis behavior may be checked at `core/eval_test.go` level, but the runtime seam is the contract.

## Out of scope

- Cons cells, dotted pairs, and `nil == '()` — the data-representation axis is deferred to a later change; v1 dialects run on the immutable `List` underneath.
- Mutable data structures — the immutability invariant holds; `set!`-style binding mutation already exists and is unchanged.
- CL packages, the condition system, `format`, and the broader ANSI standard library.
- Reader macros / an extensible readtable — v1 is fixed reader feature flags only.
- Lisp-side or REPL-time dialect switching — selection is Go-side and construction-fixed.
- VM support for non-default semantic axes — those Engines fall back to the tree-walker in v1.
- yagel's concrete allowlist and its policy dialect definition — those live in the yagel repo; this feature provides the mechanism.

## Further notes

- **Sequencing is load-bearing.** The per-Engine dispatch refactor is a prerequisite and should be its own vertical slice, merged behavior-preserving, before CL/Clojure/restricted dialects land. Decomposition into changes should reflect that ordering.
- **Open question — CL `mapcar` and friends.** Where CL vocabulary differs from the shared core by more than a name (argument order, arity, multiple-list variants), the adapter surface is unenumerated. Which builtins need a real adapter versus a plain rename is a gap to resolve during the CL-dialect slice, not now — flag back to grilling if it turns out to be large.
- **Open question — reader flag inventory.** The v1 flag set (`[..]`/`{..}` literals, `#'`, `#(...)`) is the minimum for CL/Clojure fidelity; whether CL needs additional dispatch-char handling (e.g. `#\char`, `#| |#` block comments) to parse common snippets is unverified against a real CL corpus.
- **Restricted-dialect base semantics.** A dialect built from the empty base still needs *some* semantic axis settings (a rule subset is presumably Lisp-1, and its truthiness must be pinned). The default axes for an empty-base dialect should be stated explicitly when yagel's dialect is authored, rather than inherited implicitly.
