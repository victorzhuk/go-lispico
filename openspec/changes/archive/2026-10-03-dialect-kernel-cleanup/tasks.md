## 1. Contract

- [x] 1.1 Tests: zero-value dialect compiler; CL `mapcar`/`sort` with a VM closure and with a non-callable; `IsCallable` table over every value type.

## 2. Implement

- [x] 2.1 Remove the truthiness hook end to end; conditionals call `IsTruthy`.
- [x] 2.2 Compiler takes `core.Dialect` by value; delete the nil branches.
- [x] 2.3 `core.IsCallable` + marker on `*vm.Closure`; `cl` drops the `core/vm` import.
- [x] 2.4 Drop `dialectFP` from `cacheKey`; fix the `IsIdentity` doc.
- [x] 2.5 `slices.Backward` in the two list-chain builders (`core/types.go:225`, `:236`).

## 3. Validate

- [x] 3.1 CHANGELOG `[Unreleased]` with the breaking removals; `go test -timeout 2m ./...`, `make lint`, `openspec validate dialect-kernel-cleanup --strict`.
