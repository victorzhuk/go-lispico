## 1. Contract

- [ ] 1.1 Red tests for all four scenarios; the singleton test must restore the entry it changes.

## 2. Implement

- [ ] 2.1 `Vocab()` returns `maps.Clone`; add `VocabEntry(name)`; move runtime lookups to it where they run per name.
- [ ] 2.2 Length-prefix every field in `fingerprintUncached`; doc comment states the process-local contract.

## 3. Validate

- [ ] 3.1 CHANGELOG `[Unreleased]` (Fixed: vocabulary exposure; Changed: fingerprint values); `go test -timeout 2m ./core/... ./cl/... ./clojure/... ./runtime/...`, `make lint`, `openspec validate dialect-value-integrity --strict`.
