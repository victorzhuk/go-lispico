## Why

Under the shipped default dialect (`cl`), the kernel binding forms `let`,
`let*`, and `loop` cannot be written at all. They hard-require a `Vector` for
their bindings (`compileLet`/`compileLetStar`/`compileLoop` and the tree-walker
`evalLet`/`evalLetStar`/`evalLoop`), but `cl.Dialect()` calls
`WithoutBracketLiterals()` and the CL reader rejects `[`/`]`, so there is no
source syntax that produces a `Vector`. Reproduced on `runtime.New(nil)`:

```
(let [a 1] a)   => ReadError: unexpected character: [
(let ((a 1)) a) => CompileError: compile let: bindings must be vector
(loop [i 0] i)  => ReadError: unexpected character: [
```

`defn`/`fn` avoid this because their parameter lists already accept List or
Vector (`paramsAsVector`); `let`/`let*`/`loop` were never given the same
dual-acceptance. The whole test suite missed it because every `let`/`loop` test
pins `clojure.Dialect()`. This is pre-existing (predates the current release
window) but leaves the default dialect unable to express local bindings or
iteration.

## What Changes

- `let`, `let*`, and `loop` SHALL accept their bindings as either a `Vector`
  (flat, alternating name/value — the existing Clojure form) or a `List` of
  two-element `(name value)` binding pairs (the classic Common Lisp form), in
  both the tree-walker and the bytecode compiler.
- The accepted List shape is dialect-idiomatic: under `cl`, `(let ((a 1) (b 2))
  body)` becomes writable; under `clojure`, `(let [a 1 b 2] body)` keeps working
  unchanged.
- No change to binding semantics (scoping, `let` parallel vs `let*` sequential,
  `loop` recur targets) — only the accepted surface syntax of the binding list.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: new requirement that `let`/`let*`/`loop` accept List-form
  binding pairs in addition to the Vector form, so the forms are usable under
  every dialect including the bracket-less Common Lisp default. Honored
  identically by the tree-walker and the bytecode compiler.

## Impact

- Code: `core/eval.go` (`evalLet`/`evalLetStar`/`evalLoop` binding parse),
  `core/compiler/compiler.go` (`compileLet`/`compileLetStar`/`compileLoop`
  binding parse). A shared binding-list normalizer keeps the two evaluators in
  parity.
- Behavior: `let`/`let*`/`loop` become writable under the default `cl` dialect;
  no change under `clojure`.
- Crossval: the shared normalizer must produce identical bindings for both
  evaluators; add crossval cases for the List form.
