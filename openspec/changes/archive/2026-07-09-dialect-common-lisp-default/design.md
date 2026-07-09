# Design — Common Lisp dialect and default flip

## Context

Axes (truthiness, namespace), reader flags, and the vocabulary map exist as independent mechanisms. This slice composes them into two shipped dialects and changes the zero-config default. The risk is not new mechanism — it is the breaking default change and ensuring the Clojure surface is genuinely lossless.

## Decisions

- **Common Lisp dialect composition.** Full Kernel table base; vocabulary map for CL names over the shared core, with adapters where CL semantics differ (argument order, multi-list `mapcar`); truthiness `nil`-only; namespace Lisp-2 (so `funcall`/`#'` are live); reader flags `#'` and `#(...)` on, `[..]`/`{..}` off.
- **Clojure dialect composition.** Full Kernel table base; today's vocabulary names; truthiness `nil`+`false`; namespace Lisp-1; reader flags `[..]`/`{..}` on, `#'`/`#(...)` off. This is behaviorally the pre-flip default, now named.

**Identity invariant.** The Clojure dialect is built as a bare `FullDialect()` with no vocabulary map and no axis changes, so `clojure.Dialect().IsIdentity()` is `true`. The bytecode VM dispatches canonical form names directly and rejects non-identity dialects at construction (`runtime/engine.go`: the `bytecode && !IsIdentity()` guard). This invariant protects the canonical VM construction path `New(nil, WithBytecode(), WithDialect(clojure.Dialect()))`. A future change that adds a vocabulary map to Clojure "for naming clarity" would silently break the VM for every bytecode user. The invariant is pinned by a test (`clojure.Dialect().IsIdentity()`) and by the bytecode-compatibility test.

- **Default resolution.** `runtime.New()` resolves to the Common Lisp dialect when no `WithDialect` is given. The identity dialect from the dispatch slice is retired as the default; it remains available as the composition Clojure builds on.
- **Migration containment.** Every existing test, example, and the yagel consumer that depends on the old default is updated to select `clojure.Dialect()` explicitly. New CL-default characterization tests assert the flipped behavior at the runtime seam.
- **VM disposition.** The CL dialect sets non-default semantic axes, so CL Engines evaluate on the tree-walker; the VM stays a Clojure-axis optimization for now (ADR 0005). No VM changes in this slice.

**Consequence at the runtime seam.** `New(nil, WithBytecode())` (no `WithDialect`) now errors at construction, because the new default is the CL dialect and the guard rejects non-identity. The 14 sites in `runtime/bytecode_test.go` and `runtime/fallback_test.go` that call this exact shape are pinned to `WithDialect(clojure.Dialect())` as part of task 3.2 — not by accident but because the Clojure dialect is the only named dialect the VM accepts. The characterization test for this construction-error is part of the red set (task 1.3).

## Risks

- Under-pinning the flip — a test that silently passed on the old default and now runs CL — is the main hazard. The migration task enumerates every prior-default dependency and pins it; the CL characterization tests catch anything missed.
- **VM/identity coupling** (the trap the migration hides). If the Clojure dialect is defined with any non-identity feature (vocabulary map, axis flag, adapter), the canonical VM construction `New(nil, WithBytecode(), WithDialect(clojure.Dialect()))` starts to fail. The 14 bytecode test sites pinned in task 3.2 only stay green if Clojure remains identity. The design's Clojure composition is therefore an invariant, not a default — enforced by an explicit `IsIdentity()` assertion on the Clojure value.
- CL adapter breadth (the PRD's flagged gap) surfaces here concretely. Where a needed CL function lacks an adapter, this slice adds it over the shared core rather than duplicating an implementation.

## Out of scope

Cons cells, packages, the condition system, `format`, reader macros — all deferred per ADR 0005. yagel's own restricted allowlist lives in the yagel repo; this slice provides the dialects, not that policy.
