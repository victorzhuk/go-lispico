## Context

`let`/`let*`/`loop` are kernel special forms dispatched before macro lookup, so
no macro can paper over the missing syntax. Their bindings are name/value
**pairs**, structurally unlike `defn`/`fn` parameter lists (a flat list of
symbols), so `paramsAsVector` cannot be reused directly. Two evaluators
(tree-walker in `core/eval.go`, compiler in `core/compiler/compiler.go`) parse
these bindings independently and must stay in crossval parity.

## Goals / Non-Goals

- Goal: `let`/`let*`/`loop` writable under every dialect, including bracket-less
  `cl`, with each dialect's idiomatic shape.
- Goal: identical binding semantics and identical crossval behavior across both
  evaluators.
- Non-Goal: changing scoping, evaluation order, or `recur` semantics.
- Non-Goal: adding bracket literals to the CL reader (rejected — that would
  change CL's reader identity; the fix is on the form, not the reader).

## Decisions

### Two accepted binding shapes, one normalizer

Introduce a single binding-list normalizer used by both evaluators that maps
either surface shape to the same internal `[](name, valueForm)` sequence:

- **Vector (Clojure form):** flat, alternating `[name0 val0 name1 val1 …]`.
  Odd length is an error. Unchanged from today.
- **List (Common Lisp form):** a list of two-element lists
  `((name0 val0) (name1 val1) …)`. Each element must be a 2-element `List` whose
  head is a binding symbol. A non-pair element is an error.

The two shapes are disambiguated by the concrete type of `args[0]` (`Vector` vs
`List`), so a dialect that has both syntaxes can use either; a bracket-less
dialect uses the List form exclusively.

### Why not the flat-list form `(let (a 1) a)`

Rejected. Classic Common Lisp binds with pair lists `((a 1))`, and a flat list
`(a 1 b 2)` is ambiguous against a single pair `(a 1)`. Matching real CL avoids
surprising CL users and keeps the pair boundary explicit. `(let (a 1) a)`
therefore remains an error (with a message pointing at the pair form).

### Error messages

Replace "bindings must be vector" with a dialect-neutral message naming both
accepted shapes, e.g. "let bindings must be a vector [n v …] or a list of
(name value) pairs".

## Risks / Trade-offs

- Parity risk: the normalizer is the single source of truth; both evaluators
  call it, and crossval cases cover the List form under `cl` and the Vector form
  under `clojure`. A divergent hand-rolled parse in either evaluator is the
  failure mode to guard against — hence the shared function.
- Empty bindings (`(let () body)` / `(let [] body)`) must remain valid (no
  bindings), consistent across both shapes.

## Migration

Purely additive. Existing `clojure` Vector-form code is unchanged; `cl` code
gains the ability to use `let`/`let*`/`loop`.
