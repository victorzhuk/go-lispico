## 1. Vocabulary

- [ ] 1.1 Test: under `cl.Dialect()`, `cons`, `list`, `reverse`, `apply`, `type` stay callable with identical results on the tree-walker and the VM, and their canonical flag matches the stdlib registration.
- [ ] 1.2 Remove the five identity entries from the CL vocabulary.

## 2. Single stock definition

- [ ] 2.1 Hold the CL definition in one unexported `DialectSpec`; point `stockDialect` and the memoization test at it.

## 3. Small fixes

- [ ] 3.1 Correct the package doc's VM sentence; replace the empty-case `switch` in `clNth` with a type assertion; hoist the `sort` arity message used twice (`cl/cl.go:109,120`) into one const.

## 4. clSort comparison allocations (measure first)

- [ ] 4.1 Record `(sort xs #'<)` bytes/allocs at n=1000 (baseline: about 26.3k allocs, 1.1 MB; half are the per-comparison `[]core.Value{a, b}` args slice).
- [ ] 4.2 Verify no callee keeps `args` past return (GoFunc, Lambda binding, VM closure); only then reuse one two-slot buffer across comparisons and the `:key` calls. If any callee keeps it, drop this section and record why.

## 5. Validate

- [ ] 5.1 `go test -timeout 2m ./cl/... ./runtime/...`, `make lint`, `openspec validate cl-dialect-surface-cleanup --strict`.
