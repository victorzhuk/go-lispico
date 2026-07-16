## 1. Pin current behavior (red)

- [ ] 1.1 Failing test: Clojure dialect Engine evaluates a flat-pair `cond` correctly under the tree-walker and under `WithBytecode()`.
- [ ] 1.2 Failing test: CL dialect Engine evaluates a nested-clause `cond` with a multi-expression body (implicit progn) identically under both paths.
- [ ] 1.3 Failing test: quoted `cond` data — `(quote (cond ...))` and quasiquoted forms — round-trips unchanged under both dialects and both paths.
- [ ] 1.4 Failing test: malformed shapes (odd flat pair, non-list CL clause, empty clause) return typed errors under both paths, never panic.

## 2. Implement the Form-shape rule (green)

- [ ] 2.1 Add the clause normalizer to the Dialect, with Clojure flat-pair and CL nested-clause implementations; multi-expression CL bodies wrap in kernel `do`.
- [ ] 2.2 Route Evaluator `cond` dispatch through the normalizer; delete its private clause parsing.
- [ ] 2.3 Route Compiler `cond` compilation through the same canonical clauses; delete its private clause parsing.

## 3. Refactor and verify

- [ ] 3.1 Extend the VM cross-validation corpus with both clause shapes and quoted-data cases.
- [ ] 3.2 `go test ./...` and `go test -race ./runtime/...` green; `golangci-lint run` clean.
