## 0. Decide

- [x] 0.1 Accept the change and choose removal without a deprecation window.
- [x] 0.2 Order audit: list builder chains whose intermediate order matters and map each to a spec; record the table in design.md.

## 1. Contract

- [x] 1.1 Red tests for every scenario in the spec delta.
- [x] 1.2 Parity test: for each stock dialect, the spec-built value resolves the same table and passes the existing dialect corpus on both evaluators.

## 2. Implement

- [x] 2.1 `DialectSpec`, `Adapter`, `NewDialect` with validation, defensive copies and a frozen resolved state.
- [x] 2.2 Semantic fingerprint over the resolved configuration.
- [x] 2.3 Rebuild `cl` and `clojure` stock dialects from specs.
- [x] 2.4 Remove the builders, `FullDialect`, `EmptyDialect` and `Memoized`; migrate every test and doc example to `NewDialect`.

## 3. Validate and document

- [x] 3.1 ADR 0005 amendment, README Dialects section (`NewDialect` example), `docs/dialect-layer.md` and `ARCHITECTURE.md` builder text, CHANGELOG with a migration note; `go test -timeout 2m ./...`, `make lint`, `openspec validate dialect-declarative-spec --strict`.
- [x] 3.2 Swap `dialect-registration-rules` and `dialect-declarative-spec` in the merge-order table of `openspec/changes/DEPENDENCIES.md` (this change is applied first), and note that registration-rules tests build dialects with `NewDialect`.
