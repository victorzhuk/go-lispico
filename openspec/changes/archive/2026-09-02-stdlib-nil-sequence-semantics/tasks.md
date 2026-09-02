## 0. Enforce prerequisites

- [x] 0.1 Verify `stdlib-typed-error-compliance` and its transitive lookup/CL/resource/ownership prerequisites are implemented, validated, and archived into the canonical specs; verify no active delta overlaps this change's two `stdlib-plugin` requirements.

## 1. Characterize the boundary

- [x] 1.1 Add table tests that pin each affected operation's current empty-list result, output type, multi-argument order, non-`nil` scalar outcome (value or typed error), and function-invocation count; verify the characterization table passes before nil behavior changes and includes `(empty? 1) => false`.
- [x] 1.2 Add the corresponding closed nil-input table with every operation and exact argument position from the design matrix; verify already-supported rows pass, newly specified rows fail for the expected reason, and at least one excluded operation/position remains rejected.
- [x] 1.3 Add value-model regressions proving nil remains distinct from an empty list in type predicates, equality, printing, and truthiness; verify those invariants pass unchanged.

## 2. Implement nil sequence adaptation

- [x] 2.1 Add an internal List/Vector/Nil sequence-input adapter that receives the active evaluator while leaving callers in control of error text and output construction; verify adapter-focused tests cover all accepted and rejected value types.
- [x] 2.2 Route `reverse` and `nth` through nil-aware empty-list behavior without changing non-nil bounds semantics; verify focused collection tests cover both `nth` arities.
- [x] 2.3 Route `map`, `filter`, `reduce`, and the expanded tail of `apply` through the adapter; verify nil inputs produce the specified values and never invoke callbacks except the final `apply` target.
- [x] 2.4 Route `string/join` through the adapter; verify a nil collection returns an empty string while separator and scalar-collection errors remain unchanged.
- [x] 2.5 Route nil `cons`/`conj` through the existing persistent empty-list extension paths; verify element order, immutability, length/depth limits, and incremental allocation-ledger tests remain green.
- [x] 2.6 Change collection-length and construction-depth helpers/kernels to query the active GoFunc evaluator, never `env.Evaluator()`; verify low limits still fail Terminal inside a nested Lambda child environment under both Evaluator and VM.

## 3. Verify shared semantics

- [x] 3.1 Add behavior-golden runtime coverage for Evaluator and VM paths plus exposed CL/Clojure names such as `mapcar`, `map`, and `length`; verify each adapter-backed name reaches the shared kernel and satisfies its own dialect call shape.
- [x] 3.2 Document the closed nil sequence-boundary matrix and its explicit non-goals, including `nil != '()`, unchanged map-only operations, and deliberate differences from Clojure's empty two-argument `reduce`; add an Unreleased breaking-change entry and verify every affected operation/position is represented.
- [x] 3.3 Run focused allocation/resource tests, `go test ./...`, `go test -race ./core/... ./plugins/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.
