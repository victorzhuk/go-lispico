## 1. Contract

- [ ] 1.1 Parity tests on both evaluators: renamed-away name bound by the user (CL `do`, `set!`, and `defmacro do`), unbound removed name, cond without exposed `do` (Remove and empty base), `function`/`funcall` shape errors.

## 2. Implement

- [ ] 2.1 Reverse map memoized with the resolved table; `CanonicalName` answers from it; compiler and `expandDeepList` treat absent names as ordinary symbols.
- [ ] 2.2 `NormalizeCond` returns structured clauses; `evalCond` and the compiler consume them; drop `visibleName`.
- [ ] 2.3 Typed errors for compiler `function`/`funcall` shapes.

## 3. Validate

- [ ] 3.1 Record tree-walker `cond` allocs before/after (`-benchmem`, bytes and allocs only); CHANGELOG `[Unreleased]` Fixed entries; `go test -timeout 2m ./core/... ./cl/... ./clojure/... ./runtime/...`, `make lint`, `openspec validate dialect-surface-name-resolution --strict`.
