# Design

## Canonical clause representation

One canonical clause is `(test body)` — exactly one test and one body expression. Normalization happens where special-form dispatch resolves `cond`, after macro expansion and before either execution path parses operands, so the Evaluator and Compiler consume identical structure and cannot drift.

## Dialect ownership

The normalizer is part of the Dialect value (alongside renames, truthiness, namespace axis), fixed at Engine construction per ADR 0005:

- Clojure dialect: flat pairs `(cond t1 e1 t2 e2 ...)` → clauses `(t1 e1) (t2 e2) ...`; odd trailing form is a shape error.
- Common Lisp dialect: nested `(cond (t1 e1a e1b) ...)` → multi-expression bodies wrap in kernel `do`: `(t1 (do e1a e1b))`.
- Identity/kernel behavior stays whatever the kernel table defines today; dialects without a `cond` rule are unaffected.

## Non-rewriting constraint

Normalization operates on the form being dispatched, not on Reader output or stored data: `(quote (cond ...))` and `cond` forms inside quasiquoted data pass through byte-identical. Shape errors are typed evaluation errors, never panics.
