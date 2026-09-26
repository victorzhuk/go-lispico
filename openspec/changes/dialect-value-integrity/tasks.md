## 1. Contract

- [ ] 1.1 Red tests for the two accessor scenarios: mutating the map `Vocab()` returns leaves the Dialect, its fingerprint and the `cl.Dialect()` singleton unchanged (nil stays nil, empty stays non-nil empty); an entry added to an empty-base Dialect's returned map does not become callable in an engine (`UndefinedError`).
- [ ] 1.2 Red test for the separator scenario: dialects on the same base that differ only in where `:` and `|` sit inside names (vocabulary and ops) fingerprint differently.

## 2. Implement

- [ ] 2.1 `Vocab()` returns `maps.Clone`; add `VocabEntry(name) (VocabEntry, bool)` with its own unit test. Runtime callers stay on `Vocab()`.
- [ ] 2.2 Length-prefix every string field in `fingerprintUncached`; doc comment and `docs/dialect-layer.md` state the process-local contract.

## 3. Validate

- [ ] 3.1 CHANGELOG `[Unreleased]` (Fixed: vocabulary exposure; Changed: fingerprint values); `go test -timeout 2m ./core/... ./cl/... ./clojure/... ./runtime/...`, `make lint`, `openspec validate dialect-value-integrity --strict`.
