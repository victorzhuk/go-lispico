## Why

The tree-walker and the VM disagree about names a dialect has renamed away or removed, and `cond` depends on a surface name it should not need. All three cases below were reproduced on the current tree.

- **Renamed-away names.** Under the default CL dialect (`do` renamed to `progn`), `(defun do (x) (* x 2))` then `(do 5)` returns `10` on the tree-walker and fails on the default VM with `CompileError: undefined form "do"`. There is no fallback. The compiler reads the frozen `CanonicalName` map, whose hidden-name sentinel it treats as an error; the tree-walker looks the head up in the resolved table and treats a missing name as an ordinary call.
- **`cond` bodies.** A multi-expression clause body is wrapped in the dialect's visible name for `do` (`st.doName`, computed by `doNameOf` during freeze). With a `DialectSpec` that hides `do` without renaming it, or an empty base that exposes `cond` without `do`, `(cond (true 1 2))` fails with "undefined: do", although the body should run as a kernel sequence and the program never wrote `do`.
- **Error types.** The compiler reports `function`/`funcall` shape errors as plain `fmt.Errorf` (`core/compiler/compiler.go:262-273`); the tree-walker returns typed `EvalError` for the same input.

## What Changes

- One source for special-form dispatch: the compiler, macro expansion (`expandDeepList`) and the tree-walker all consult the resolved table. A name that is not in the table is an ordinary symbol on every path.
- `cond` normalization produces clause bodies that the evaluator and the compiler run as a sequence directly, with no surface symbol involved. This also removes the per-evaluation clause lists the tree-walker builds today.
- Compiler shape errors for `function`/`funcall` return the same typed errors as the tree-walker.

Decision (resolved 2026-09-26):

- [x] A renamed-away or removed name is an ordinary symbol: the user may bind and call it, and an unbound call fails as `UndefinedError` on both paths. The binding is a user function, not the form, so a policy dialect gains no kernel capability.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: modify **Dialect renames normalize to canonical kernel forms** and **Form-shape rules are Dialect-owned**.

## Impact

`core/dialect.go` (`CanonicalName`, `NormalizeCond`, `doNameOf`), `core/eval.go` (`evalCond`, `expandDeepList`), `core/compiler/compiler.go`, their tests, CHANGELOG. The tree-walker `cond` change also removes about 9 allocs / 320 B per `cond` evaluation measured on a 4-pair flat `cond` (tree-walker only; the VM normalizes once at compile time). Builds on `dialect-declarative-spec`: the reverse dispatch map already lives in its frozen resolved state.
