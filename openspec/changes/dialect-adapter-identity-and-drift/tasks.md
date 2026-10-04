## 1. Runtime adapter identity

- [ ] 1.1 Add the failing test: two dialects with equal fingerprints and different adapter values each install their own adapter value.
- [ ] 1.2 Resolve adapter values from the dialect in hand; stop retaining adapter values in `vocabShapeOf`'s process-wide cache.

## 2. Dialect base validation

- [ ] 2.1 Add the failing test: `NewDialect` refuses a `DialectBase` that is neither `BaseFull` nor `BaseEmpty`.
- [ ] 2.2 Reject an unknown base in `validateSpec`.

## 3. JSON decode scaling test

- [ ] 3.1 Replace the wall-clock ratio assertion in `TestDecodeHashMap_Scaling` with a deterministic allocation-ratio assertion.

## 4. Document counts and file trees

- [ ] 4.1 `AGENTS.md`: 21 special forms in the Status bullet; add `core/dialect.go` and `core/callable.go` to the core tree.
- [ ] 4.2 `ARCHITECTURE.md`: 21 special forms in the table header; add `core/dialect.go`, `core/callable.go` and `core/registration.go` to the core tree.

## 5. ADR and PRD truthiness drift

- [ ] 5.1 `docs/adr/0005-dialect-layer.md` and `docs/adr/0006-vm-first-staged.md`: no truthiness axis, `TruthyFunc` removed, fixed `core.IsTruthy`.
- [ ] 5.2 `docs/prd/dialect-layer.md`: one configurable axis; the VM is the default and complete path.

## 6. Dependency record and release notes

- [ ] 6.1 `openspec/changes/DEPENDENCIES.md`: empty active set, the seven dialect changes listed as archived with archive links.
- [ ] 6.2 `CHANGELOG.md` `[Unreleased]`: entries for the `CanonicalName` arity change and `NormalizeCond` returning `[]CondClause`.
