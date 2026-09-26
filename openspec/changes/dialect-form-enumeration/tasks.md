## 1. Contract

- [ ] 1.1 Red tests for every scenario in the spec delta.

## 2. Implement

- [ ] 2.1 Add `Forms()` over the frozen resolved table (sorted names computed once, copied per call); doc comment names the Lisp-2 intrinsics.

## 3. Validate

- [ ] 3.1 CHANGELOG `[Unreleased]` Added entry; `go test -timeout 2m ./core/... ./cl/... ./clojure/...`, `make lint`, `openspec validate dialect-form-enumeration --strict`.
