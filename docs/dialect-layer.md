# Dialect layer

A dialect is a delta over the kernel form table plus reader flags, a
vocabulary renaming, and adapters — named `core.Value` bindings with a
semantic ID. `core.NewDialect(spec core.DialectSpec)` validates the spec,
resolves the delta once, and returns a frozen `core.Dialect`: an immutable
one-pointer value safe to share across goroutines. An invalid spec reports an
error and the zero `Dialect`; the zero `Dialect` is itself the identity
dialect (full kernel, canonical names, Lisp-1, no vocabulary).

## Adapters

`spec.Adapters[name] = core.Adapter{ID: semanticID, Value: value}` binds
`value` under `name` and folds `semanticID` into the dialect fingerprint. The
ID makes adapters with the same name but different semantics distinguishable
within a process. The fingerprint is not persistent and may change between
releases. The Common Lisp dialect registers its collection adapters under
fixed IDs:

```go
d, err := core.NewDialect(core.DialectSpec{
    Lisp2:        true,
    NoBrackets:   true,
    FunctionRef:  true,
    Adapters: map[string]core.Adapter{
        "nth":    {ID: "cl/nth@1", Value: clNth},
        "mapcar": {ID: "cl/mapcar@1", Value: clMapcar},
        "sort":   {ID: "cl/sort@1", Value: clSort},
    },
})
```

`cl.Dialect()` and `clojure.Dialect()` wrap their spec in `sync.OnceValue`, so
resolution and fingerprinting run once per process and every caller shares
the same `core.Dialect` value.

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
