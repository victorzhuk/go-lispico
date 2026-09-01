## 0. Enforce prerequisites

- [x] 0.1 Verify `builtin-resource-accounting` and `stdlib-bootstrap-evaluator-ownership` are completed; verify the ownership delta is archived into the canonical `stdlib-plugin` spec before applying this change and no other active change modifies `Bootstrap macros bind through the engine's evaluator`.

## 1. Pin lookup behavior

- [x] 1.1 Accept `nil` in two- and three-argument `get`, preserve the scalar-subject error, and verify the direct stdlib nil/default/non-map cases pass.
- [x] 1.2 Add direct `get` regressions for a present key storing `nil`, a missing map key, invalid arity, exact `ArityError`/`TypeError` classification, and zero-byte borrowed returns; verify the focused collection and allocation-ledger tests pass.
- [x] 1.3 Add failing `get-in` table tests for list/vector/nil paths, empty paths, missing intermediate keys, terminal versus intermediate `nil`, defaults, invalid path types, scalar intermediates, and arity; verify every specified branch has an explicit expected value or error.
- [x] 1.4 Add failing direct tests for caller cancellation, an expired Engine-owned deadline with a live parent context, a VM that consumes most of its absolute deadline before entering lookup, an exhausted Reduction budget, and tight allocation budgets on borrowed results; verify terminal errors and allocation outcomes are typed under both execution paths.
- [x] 1.5 Add cursor tests over geometric shared-tail list sizes plus vector paths at representative trie depths, assert no path-sized copy allocations, and add non-gating 1K/10K/100K list-path benchmarks; verify list work scales linearly without timing assertions in correctness tests.

## 2. Implement presence-aware nested lookup

- [x] 2.1 Register two-/three-argument Builtin `get-in` beside `get`, traverse with `HashMap.Get` presence bits and representation-aware list/vector cursors, accrue one `BuiltinWorkBudget` step per visited key, flush every return path, and mark all lookup outputs as borrowed; verify semantic, deadline, cancellation, metering, allocation, and cursor tests pass.
- [x] 2.2 Remove the Lisp bootstrap `get-in` entry and update bootstrap artifact/name/cache tests to show the process-level cache has no producer; verify eager stdlib startup still resolves all remaining bootstrap macros through the environment-owned evaluator.
- [x] 2.3 Update lazy-template and materialization-count tests for Builtin `get-in`; verify first use does not publish a `get-in` source template or materialize `reduce`/`get` as transitive dependencies.
- [x] 2.4 Pin the callable migration and Dialect surface: verify `get-in` prints/compares as a Builtin, remains available in full-base Dialects, and is undefined under an empty-base Dialect unless explicitly allowlisted.

## 3. Verify public and engine contracts

- [x] 3.1 Add behavior-golden runtime tests for Evaluator and VM execution plus eager/lazy startup, including successful, missing, stored-`nil`, empty-path, typed-error, and resource cases; use CL-portable forms for default-Engine cases and verify both paths satisfy the independent expected result.
- [x] 3.2 After implementation is complete, document `get-in` arities, default/presence behavior, supported path types, conditional map-only scope, callable migration, and empty-base allowlisting next to `get`; verify every README example evaluates successfully under its stated Dialect.
- [x] 3.3 Run `go test ./...`, `go test -race ./core/... ./plugins/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.
