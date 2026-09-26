## 1. Contract

- [x] 1.1 Parity tests on both evaluators: renamed-away name bound by the user (CL `do`, `set!`, and `defmacro do`), unbound hidden name, cond without exposed `do` (hidden `do` and empty base), `function`/`funcall` shape errors; migrate existing old-contract assertions in `cl/dialect_parity_test.go`, `clojure/dialect_parity_test.go`, `core/dialect_spec_test.go`, and `runtime/dialect_zero_value_test.go`.

## 2. Implement

- [x] 2.1 Remove the hidden-name sentinel from the existing frozen reverse map; `CanonicalName`, the compiler and `expandDeepList` treat absent names as ordinary symbols.
- [x] 2.2 `NormalizeCond` returns structured clauses; `evalCond` and the compiler consume them; drop `st.doName` and `doNameOf`.
- [x] 2.3 Typed errors for compiler `function`/`funcall` shapes.

## 3. Validate

- [x] 3.1 Record tree-walker `cond` allocs before/after (`-benchmem`, bytes and allocs only); CHANGELOG `[Unreleased]` Fixed entries; `go test -timeout 2m ./core/... ./cl/... ./clojure/... ./runtime/...`, `make lint`, `openspec validate dialect-surface-name-resolution --strict`.
