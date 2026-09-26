## 1. Contract

- [x] 1.1 Red tests: Lisp-2 without vocabulary (eager and lazy, VM and tree-walker); host `Bind` then `Use` under an empty base; eager-vs-lazy binding parity for `cl.Dialect()` (regression row) and for a Lisp-2 dialect with an identity vocabulary entry on a canonical operator (`+` -> `+`); json load journals no stdlib names; the existing defun-revert regression stays green.
- [x] 1.2 Add CL rows to the startup/`Use` benchmarks; record bytes and allocs for `Use(json)` under CL and Clojure, eager and lazy, as the baseline.

## 2. Implement

- [x] 2.1 Core registration decision function and result type; `Registration.Names` for operation scoping.
- [x] 2.2 Eager path: scope to the operation's names, execute the plan, drop the whole-root passes; adapters ensured only when absent.
- [x] 2.3 Lazy path: execute the same plan in `RegisterValue` and at materialization.

## 3. Validate

- [x] 3.1 Compare the 1.2 baseline (bytes/allocs only); CHANGELOG `[Unreleased]` Fixed entries; `go test -timeout 2m ./core/... ./runtime/... ./cl/... ./clojure/...` and `-race`, `make lint`, `openspec validate dialect-registration-rules --strict`.
