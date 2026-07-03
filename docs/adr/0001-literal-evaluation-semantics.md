---
status: accepted
---

# Vector and map literals evaluate their elements

Evaluating a vector `[a b]` or map `{:k v}` evaluates each element, so `(let [x 99] [1 x])` yields `[1 99]`, matching Clojure and every mainstream Lisp. Previously vector and map literals self-evaluated with their elements left unresolved, which contradicted quasiquote (which already evaluated `~x` inside a vector) and surprised anyone expecting Lisp semantics.

## Consequences

- A departure from the prior behavior: any script that relied on `[...]`/`{...}` holding unevaluated symbols as data must switch to `quote` or build the collection explicitly.
- Quasiquote gains the previously missing map case, so `` `{:k ~x} `` expands consistently with `` `[~x] ``.
