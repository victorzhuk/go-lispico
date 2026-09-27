## 1. Vocabulary

- [x] 1.1 Test: under `cl.Dialect()`, `cons`, `list`, `reverse`, `apply`, `type` stay callable with identical results on the tree-walker and the VM, and their canonical flag equals the stdlib registration. Baseline before removal: `applyVocabulary` short-circuits the identical write (`runtime/engine.go:541`), so the cell's canonical flag and value are already unchanged by the identity entry today — removal must keep that property.
- [x] 1.2 Remove the five identity entries from the CL vocabulary.

## 2. Single stock definition

- [x] 2.1 Hold the CL definition in one unexported `clSpec core.DialectSpec`; point `stockDialect` and the three duplicated-spec tests at it. The three sites are `TestCL_Dialect_StockFingerprint` (`cl/cl_test.go:251`), `TestCL_StockMatchesSpecFingerprint` (`cl/dialect_spec_fingerprint_test.go:22`), and the `vocab` subtest of `TestCL_StockFormTable` (`cl/dialect_parity_test.go:67`). Add `cl/export_test.go` exposing `clSpec`.

## 3. Small fixes

- [x] 3.1 Correct the package doc's VM sentence; replace the empty-case `switch` in `clNth` with a type assertion; hoist the `sort` arity message used twice (`cl/cl.go:109,120`) into one const.

## 4. clSort comparison allocations (measure first)

- [x] 4.1 Record `(sort xs #'<)` bytes/allocs at n=1000 (baseline: about 26.3k allocs, 1.1 MB; half are the per-comparison `[]core.Value{a, b}` args slice).
- [x] 4.2 Verify no callee keeps `args` past return (GoFunc, Lambda binding, VM closure); only then reuse one two-slot buffer across comparisons and the `:key` calls. If any callee keeps it, drop this section and record why.

## 5. Validate

- [ ] 5.1 `go test -timeout 2m ./cl/... ./runtime/...`, `make lint`, `openspec validate cl-dialect-surface-cleanup --strict`.
