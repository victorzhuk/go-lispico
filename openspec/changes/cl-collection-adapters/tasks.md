## 0. Enforce prerequisites

- [ ] 0.1 Verify `builtin-resource-accounting` is implemented, validated, and archived into the canonical specs; verify no active delta overlaps this change's `Common Lisp dialect` requirement.

## 1. Pin canonical and CL behavior

- [ ] 1.1 Add characterization tests for canonical `nth`, single-sequence `map`, and natural `sort`, including values, collection types, typed errors, callback counts, and allocation behavior; verify they pass before kernel extraction.
- [ ] 1.2 Add failing CL behavior goldens for `nth` argument order/out-of-range nil, one- and multi-list `mapcar` with shortest-list termination, and the complete predicate/`:key` `sort` grammar; verify expected values are independent of Evaluator/VM parity.
- [ ] 1.3 Add Lisp-2 callback, empty/nil input, callback-error, Terminal-error, and empty-base allowlist cases; verify each desired adapter branch is represented.
- [ ] 1.4 Add CL `sort` callback-order/count tests for identity `:key nil`, exactly-once key projection, generalized predicate truthiness, stable equivalent elements, and first-error stop; verify list/vector/nil output types and input immutability.

## 2. Share collection kernels

- [ ] 2.1 Extract the internal indexed-access kernel and route canonical `nth` through it; pass the active evaluator/resource policy and verify all canonical characterization tests remain unchanged.
- [ ] 2.2 Extract a multi-sequence mapping kernel, retain canonical `map`'s one-sequence validation, budget alignment/copy/result phases, and verify unequal inputs stop before an extra callback.
- [ ] 2.3 Extract a parameterized stable sorting kernel with precomputed keys and a first-error latch, retain canonical natural ordering, and budget copying/key storage/comparator scheduling/result construction without double-charging callback evaluation.

## 3. Install Common Lisp adapters

- [ ] 3.1 Extend `VocabEntry` with a required stable `AdapterID`, change `WithAdapter` to accept it, and hash visible/canonical names plus that ID; verify empty IDs fail Dialect resolution/Engine construction, identical IDs are stable, different IDs/config versions differ, and no function pointer or `%T` identity enters the fingerprint.
- [ ] 3.2 Add memoized CL `nth`, `mapcar`, and `sort` adapter GoFuncs with versioned IDs over the shared kernels and bind them with `WithAdapter`; verify direct aliases are gone.
- [ ] 3.3 Verify CL `sort` enforces the exact option grammar, applies predicate and optional `:key` through the active Evaluator, preserves input immutability/result type, and propagates the first typed/Terminal callback error unchanged without later callbacks.
- [ ] 3.4 Add Evaluator/VM and eager/lazy behavior goldens for every CL adapter plus canonical Clojure-style names, including a low Reduction budget and late VM deadline; verify each visible name satisfies its own call shape over one shared kernel.

## 4. Document the migration

- [ ] 4.1 Document CL collection call shapes, `sort` keyword/error/result rules, the immutable deviation, and the `WithAdapter` signature migration; add an Unreleased breaking-change entry and verify examples parse under the CL reader without bracket literals.
- [ ] 4.2 Run focused stdlib/Dialect/runtime tests, `go test ./...`, `go test -race ./core/... ./plugins/stdlib/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.
