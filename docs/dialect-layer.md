# Dialect layer

A dialect is a delta over the kernel form table plus reader flags, a
vocabulary renaming, and adapters — named `core.Value` bindings with a
semantic ID. Resolving the delta yields the effective name-to-form table an
engine dispatches through; the dialect itself is an immutable value, and
every builder method returns a new `core.Dialect`.

## Adapters

`WithAdapter(name, semanticID, value)` binds `value` under `name` and folds
`semanticID` into the dialect fingerprint. The ID makes adapters with the
same name but different semantics distinguishable and gives the surface a
stable hash across processes. The Common Lisp dialect registers its
collection adapters under fixed IDs:

```go
d := core.FullDialect().
    Lisp2().
    WithFunctionRef().
    WithoutBracketLiterals().
    WithAdapter("nth", "cl/nth@1", clNth).
    WithAdapter("mapcar", "cl/mapcar@1", clMapcar).
    WithAdapter("sort", "cl/sort@1", clSort)
```

`Memoized()` caches the fingerprint of a fully built dialect; repeated
`cl.Dialect()` calls share one fingerprint.

## CL collection shapes

- `nth (index list)`: index-first. Out-of-range access and indexing `nil`
  return `nil`, not an error. Wrong arity fails with `ArityError`, a
  negative index or unknown option with `EvalError`, a non-integer index or
  a sequence that is neither list nor `nil` with `TypeError`.
- `mapcar (fn &rest lists)`: one function, any number of sequences; the
  shortest sequence terminates the traversal. Wrong arity fails with
  `ArityError`; a non-sequence argument fails with `TypeError`.
- `sort (sequence predicate &key key)`: returns a new sorted sequence of the
  input type and leaves the input untouched — a deliberate deviation from
  the Common Lisp standard, which permits `sort` to destroy its argument.
  The sort is stable; key functions run exactly once per element, in
  original order, before any comparison; keywords are truthy for the
  predicate. An unknown or repeated keyword fails with `EvalError`, a
  leftover option without a value with `ArityError`.

Callback errors — from the `mapcar` function or the `sort` predicate or key
— stop the traversal immediately: the first error propagates unchanged and
no callback runs after it. Terminal errors (resource limits, deadlines) keep
their terminal precedence through the adapters.
