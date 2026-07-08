# Tasks — Dialect namespace axis (Lisp-1/Lisp-2)

## 1. Red

- [x] 1.1 At the `runtime` Engine seam, add a failing test: under a Lisp-2 Dialect, a symbol bound as both a value and a function resolves to the value in argument position and to the function in head position. Acceptance: red.
- [x] 1.2 Add a failing test that `(funcall f args...)` and `#'f` apply the function-cell binding under Lisp-2, and that both are undefined under the default Lisp-1 Dialect. Acceptance: red.

## 2. Implement

- [x] 2.1 Add a namespace setting to the Dialect (Lisp-1 | Lisp-2). Acceptance: resolvable from the Engine's Dialect.
- [x] 2.2 Add a function cell to `Env`, unused under Lisp-1. Acceptance: Lisp-1 environment behavior unchanged; existing `env_test.go` green.
- [x] 2.3 Make head-symbol resolution consult the function cell under Lisp-2 and the single cell under Lisp-1. Acceptance: 1.1 green.
- [x] 2.4 Supply `funcall` and `#'` evaluation as Lisp-2-only forms via the Dialect Delta. Acceptance: 1.2 green.

## 3. Verify

- [x] 3.1 Full suite green; confirm an Engine on the Lisp-2 axis falls back to the tree-walker rather than miscompiling in the VM. Acceptance: `go test ./...` passes.
- [x] 3.2 `openspec validate dialect-namespace-axis --strict` passes.
