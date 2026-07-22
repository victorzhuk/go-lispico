## 1. Behavior contracts

- [x] 1.1 Red tests (CL dialect, both evaluators): `(if (= 1 2) a b)`,
  `(if false a b)`, `(when (= 1 2) x)`, `(unless (= 1 2) x)`,
  `(cond ((= 1 2) a) (:else b))`, `(and (= 1 2) x)`, `(or (= 1 2) y)`,
  `(not (= 1 2))` each take the correct branch (false is falsy).
- [x] 1.2 Characterization: Clojure and identity dialects unchanged — `(if
  false a b)` → `b` as before.
- [x] 1.3 Crossval: predicate-driven conditionals under CL agree between VM and
  tree-walker.

## 2. Implementation

- [x] 2.1 `core/dialect.go`: remove the `truth` field, the `truthNilOnly`
  branch in `isTruthy` (uniform `IsTruthy`), the `NilOnlyFalsy()` method, and
  the associated axis constants.
- [x] 2.2 `cl/cl.go`: drop the `.NilOnlyFalsy()` call from `Dialect()`.
- [x] 2.3 Sweep for any other `NilOnlyFalsy`/`truthNilOnly` references
  (base-dialect wiring, dialect fingerprint/`IsIdentity` inputs) and remove.

## 3. Test migration

- [x] 3.1 Flip existing assertions that encoded the bug — `(if false …)` →
  `:yes` under CL, and any CL-dialect test asserting a comparison-guarded true
  branch — to the corrected result. Grep the suite for `NilOnlyFalsy`,
  `:yes`/`:y` under CL truthiness fixtures.

## 4. Integration

- [x] 4.1 `go test ./... -race` green.
- [x] 4.2 `GOLDSET_MODE=vm` goldset gate non-increasing (truthiness resolution
  is unchanged in cost — one fewer branch, if anything).
- [x] 4.3 `golangci-lint run` clean (dead-code removal leaves no unused
  symbols).

## 5. Verification

- [x] 5.1 `openspec validate --strict dialect-uniform-false-falsy`.
- [x] 5.2 CHANGELOG `[Unreleased]` note under Fixed/Changed (breaking:
  `NilOnlyFalsy()` removed; CL `false` is now falsy); update any doc that states
  CL treats `false` as true (ARCHITECTURE.md/README dialect notes,
  `core/dialect.go` doc comments).
