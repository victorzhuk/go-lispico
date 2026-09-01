## Why

The Common Lisp Dialect directly aliases `nth`, `mapcar`, and `sort` to shared
stdlib Builtins whose argument order or arity differs from the visible CL name.
That violates the canonical thin-adapter rule and makes ordinary CL-shaped calls
fail before collection semantics are exercised.

Blocked by: `builtin-resource-accounting`.

## What Changes

- Route CL `nth` through an adapter accepting index before list and returning
  `nil` when the index is beyond the list.
- Route CL `mapcar` through an adapter accepting one or more lists, applying the
  function to aligned elements, and stopping at the shortest list.
- Route CL `sort` through an adapter accepting a predicate and optional `:key`
  function while preserving Lispico's immutable value model.
- Extract parameterized collection kernels shared by canonical stdlib Builtins
  and CL adapters; do not duplicate mapping, indexing, or sorting algorithms.
- Give every Dialect adapter a stable semantic ID/version that participates in
  the Dialect fingerprint; never fingerprint a function pointer or only its Go
  concrete type.
- Account uninterrupted adapter/kernel copying, traversal, and sort scheduling
  with `core.BuiltinWorkBudget`, leaving evaluator re-entry as the owner of
  callback execution.
- Preserve canonical Clojure-style `nth`, `map`, and natural `sort` call shapes.
- **BREAKING** CL calls that relied on the previous non-CL argument shapes must
  migrate to the visible names' documented CL shapes. Go callers of
  `Dialect.WithAdapter` must also supply a stable semantic adapter ID.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: make semantics-differing Common Lisp collection names use thin
  adapters with explicit call and result contracts.

## Impact

- Affects Common Lisp Dialect construction, shared stdlib collection kernels,
  Lisp-2 callback application, Builtin resource budgets, the `WithAdapter` Go
  API, Dialect fingerprints, runtime tests, README examples, and the Unreleased
  changelog.
- Unblocks `stdlib-nil-sequence-semantics`; nil boundary behavior can then flow
  through CL adapters without pretending the visible call shapes are identical.
- Does not add cons cells, dotted lists, destructive sequence mutation, or full
  ANSI Common Lisp compatibility.
