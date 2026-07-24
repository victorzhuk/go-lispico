## 1. Behavior contracts

- [x] 1.1 Red test: `(let ((a 1) (b 2)) (+ a b))` under `cl` (default) returns
  `3` — currently a CompileError. Same for `let*` and `loop`.
- [x] 1.2 Red test: `(loop ((i 0)) (if (< i 3) (recur ((i (+ i 1)))) i))`-shaped
  CL loop parses and iterates (adjust to the real recur binding shape) under
  `cl`.
- [x] 1.3 Characterization: `(let [a 1 b 2] (+ a b))` under `clojure` still
  returns `3`; empty bindings `(let () 1)` / `(let [] 1)` return `1`.
- [x] 1.4 Crossval: the List-form `let`/`let*`/`loop` cases produce identical
  results in tree-walker and bytecode modes.
- [x] 1.5 Red test: a malformed binding list (odd-length Vector; a non-pair List
  element) fails with an error naming both accepted shapes.

## 2. Implementation

- [x] 2.1 Add a shared binding-list normalizer (Vector flat form → pairs; List
  `(name value)` form → pairs) with the odd-length / non-pair / non-symbol-head
  validation, returning a dialect-neutral error.
- [x] 2.2 Route `evalLet`/`evalLetStar`/`evalLoop` (`core/eval.go`) through the
  normalizer instead of the `args[0].(Vector)` assertion.
- [x] 2.3 Route `compileLet`/`compileLetStar`/`compileLoop`
  (`core/compiler/compiler.go`) through the same normalizer.
- [x] 2.4 Update the error string from "bindings must be vector" to name both
  shapes.

## 3. Integration

- [x] 3.1 `go test ./... -race` green.
- [x] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — binding parse is
  compile-time only; runtime hot path unchanged.
- [x] 3.3 Crossval suite green including the new List-form cases.

## 4. Verification

- [x] 4.1 `openspec validate --strict dialect-binding-form-list-syntax`.
- [x] 4.2 CHANGELOG `[Unreleased]` under Fixed: `let`/`let*`/`loop` are now
  usable under the default Common Lisp dialect via `(name value)` list bindings.
- [x] 4.3 Update CLAUDE.md / README if either shows a `let` example, so the
  documented default-dialect syntax is correct.
