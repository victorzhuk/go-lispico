# Design — Per-Engine dialect dispatch

## Context

`evalList` resolves a head symbol against the package-global `specialForms` map. Making dialects possible means (a) the table becomes per-Engine, and (b) a Dialect value can describe a table as a delta over a base. This is the foundation slice; correctness here is measured by *no observable change* for existing embedders plus the new selection mechanics working.

## Decisions

- **Kernel table under neutral names.** The kernel owns one canonical set of special-form implementations keyed by neutral names. Today's names that already are neutral stay; where a name is dialect-flavored, the canonical key is the neutral one and the identity dialect maps the current name onto it. This keeps the compiler/VM keyed on canonical forms.
- **Delta over a base.** A Dialect carries a base selector (full Kernel table or empty) and three delta operations: rename (`from` → canonical), add (name → canonical form already in the kernel), remove (name). Resolution produces an effective `name → formFn` table once, at Engine construction.
- **Per-Engine ownership.** The resolved table lives on the evaluator/Engine, not in a package global. Concurrent Engines with different dialects must not share it (ADR 0003). The tree-walker reads the table from Engine-scoped state, not a `var`.
- **Immutable selection.** `WithDialect` captures the Dialect into `engineConfig` at `New`; there is no Lisp-side switch. This makes a restricted dialect a real boundary — evaluated code cannot re-resolve the table.
- **Empty base is fail-closed.** Resolving an empty-base Dialect yields only the forms its Delta explicitly adds. A form added to the Kernel table in a future change is invisible to it unless its Delta adds it.

## Risks

- The refactor from a package global to per-Engine state is the highest-risk step; characterization tests over the current special-form behavior land first and must stay green.
- Neutral-naming must not change what the VM compiles; cross-validation tests (`core/vm/crossval_test.go`) guard parity.

## Out of scope

Semantic axes, vocabulary mapping, reader flags, and the CL default — each is its own later slice. This slice ships only the identity dialect.
