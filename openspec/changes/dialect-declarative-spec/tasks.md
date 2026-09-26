## 0. Decide

- [x] 0.1 Accept the change and choose removal without a deprecation window.
- [ ] 0.2 Order audit: list builder chains whose intermediate order matters and map each to a spec; record the table in design.md.

## 1. Contract

- [ ] 1.1 Red tests for every scenario in the spec delta.
- [ ] 1.2 Parity test: for each stock dialect, the spec-built value resolves the same table and passes the existing dialect corpus on both evaluators.

## 2. Implement

- [ ] 2.1 `DialectSpec`, `Adapter`, `NewDialect` with validation, defensive copies and a frozen resolved state.
- [ ] 2.2 Semantic fingerprint over the resolved configuration.
- [ ] 2.3 Rebuild `cl` and `clojure` stock dialects from specs.
- [ ] 2.4 Remove the builders, `FullDialect`, `EmptyDialect` and `Memoized`; migrate every test and doc example to `NewDialect`.

## 3. Validate and document

- [ ] 3.1 ADR 0005 amendment, README dialect section, CHANGELOG with a migration note; `go test -timeout 2m ./...`, `make lint`, `openspec validate dialect-declarative-spec --strict`.
