## 1. Contract

- [ ] 1.1 Tests: zero-value dialect compiler; CL `mapcar`/`sort` with a VM closure and with a non-callable; `IsCallable` table over every value type.

## 2. Implement

- [ ] 2.1 Remove the truthiness hook end to end; conditionals call `IsTruthy`.
- [ ] 2.2 Compiler takes `core.Dialect` by value; delete the nil branches.
- [ ] 2.3 `core.IsCallable` + marker on `*vm.Closure`; `cl` drops the `core/vm` import.
- [ ] 2.4 Drop `dialectFP` from `cacheKey`; fix the `IsIdentity` doc.
- [ ] 2.5 `maps.Copy` in `copyVocab`, `slices.Backward` at `core/dialect.go:550`.

## 3. Validate

- [ ] 3.1 CHANGELOG `[Unreleased]` with the breaking removals; `go test -timeout 2m ./...`, `make lint`, `openspec validate dialect-kernel-cleanup --strict`.
