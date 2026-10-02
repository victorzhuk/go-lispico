## 1. Contract

- [x] 1.1 Scenario tests adopted from 4613eec, with the Clojure correction from b8b474f. Historical red evidence is not replayed at this base.

## 2. Implement

- [x] 2.1 Frozen-table `Forms()` implementation adopted from a4dc645.

## 3. Validate

- [x] 3.1 CHANGELOG `[Unreleased]` Added entry; `go test -timeout 2m ./core/... ./cl/... ./clojure/...`, `make lint`, `openspec validate dialect-form-enumeration --strict`.
