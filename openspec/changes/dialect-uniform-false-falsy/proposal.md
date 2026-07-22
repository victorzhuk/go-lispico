## Why

The default engine runs the CL dialect, whose truthiness axis is "only `nil` is
falsy" (`NilOnlyFalsy()`, `core/dialect.go:310`), so `isTruthy` is `!isNil`
(`core/dialect.go:317-322`). stdlib comparison and predicate builtins are
dialect-agnostic and return a concrete `Bool{false}` for "no"
(`plugins/stdlib/comparison.go:19,59`), which is not `Nil` — so under CL it is
truthy. Every conditional built on a predicate inverts:

```
(if (= 1 2) "WRONG" "CORRECT")  => "WRONG"   ; default engine + stdlib
(if false   "WRONG" "CORRECT")  => "WRONG"
(when (= 1 2) "fires")          => "fires"
```

A silent wrong branch, no error, on the out-of-box dialect.

The axis was meant to mimic CL nil-punning (nil the sole false). But lispico has
a concrete `Bool` type and a `false` reader literal, and — verified — its empty
list `'()` is a distinct truthy `:list`, not `nil` (`(nil? '())` → `false`).
Across all 13 value types the nil-only axis makes exactly one value truthy that
the default treats as falsy: the concrete `Bool{false}`. It serves no
nil-punning the value model actually supports; it only produces the inversion.

## What Changes

- Truthiness becomes uniform across all dialects: `nil` and the concrete
  `false` are falsy, everything else truthy. A concrete boolean `false` is
  falsy in every dialect — because it is a boolean false.
- Remove the now-purposeless nil-only truthiness axis: drop
  `Dialect.NilOnlyFalsy()` and the `truth` field/branch; `isTruthy` is
  uniformly the existing `IsTruthy` (`nil`+`false` falsy).
- The CL dialect keeps its vocabulary, Lisp-2 namespace, and reader flags
  unchanged — only its truthiness stops inverting.
- No stdlib change: predicates keep returning `Bool{false}`, and the shared
  builtins stay dialect-agnostic ("single shared implementation").

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: remove `Truthiness is a Dialect axis` and `Identity Dialect
  truthiness is unchanged`; add `Truthiness is uniform: nil and false are
  falsy`; modify `Common Lisp dialect` (drop nil-only truthiness from its
  composition).

## Impact

- Code: `core/dialect.go` (remove the axis, `isTruthy` → uniform `IsTruthy`),
  `cl/cl.go` (drop the `.NilOnlyFalsy()` call), plus any base-dialect wiring
  that set the axis.
- Tests: existing tests that assert `(if false …)` → `:yes` under CL (they
  encoded the bug) flip to `:no`; VM/tree-walker crossval extended for
  predicate-driven conditionals under CL.
- Breaking (alpha): a custom `Dialect` built with `.NilOnlyFalsy()` no longer
  compiles — the removal surfaces every caller. In-repo CL/Clojure/identity
  updated.
- Fixes silent branch inversion on the default dialect.
