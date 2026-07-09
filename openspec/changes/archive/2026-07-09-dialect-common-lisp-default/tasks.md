# Tasks — Common Lisp dialect and default flip

## 1. Red

- [ ] 1.1 At the `runtime` Engine seam, add failing characterization tests for `runtime.New()` behaving as Common Lisp: `defun` defines a function, `(if false :y :n)` is `:y` (nil-only), `funcall`/`#'` work, `#'f` and `#(...)` parse, `[1 2]` does not read as a vector literal. Acceptance: red — default is still the old flavor.
- [ ] 1.2 Add a test that `WithDialect(clojure.Dialect())` reproduces the pre-flip behavior exactly. Acceptance: red until the Clojure dialect exists.
- [ ] 1.3 Add a failing test that `New(nil, WithBytecode())` (no `WithDialect`) errors at construction. The default flips to Common Lisp (Lisp-2 + nil-only + CL reader flags), which is non-identity, so the bytecode VM guard at `runtime/engine.go` rejects it. Acceptance: red — default is still identity today, so the call succeeds and the test fails; goes green after 3.1.
- [ ] 1.4 Add a passing test that `New(nil, WithBytecode(), WithDialect(clojure.Dialect()))` succeeds at construction. This pins the Clojure-identity invariant end-to-end at the runtime seam. Acceptance: green only if `clojure.Dialect().IsIdentity()` is true; the test fails if a future change makes Clojure non-identity.

## 2. Assemble dialects

- [ ] 2.1 Define the Clojure dialect: full base, current vocabulary, `nil`+`false` truthiness, Lisp-1, bracket literals on. Acceptance: 1.2 green.
- [ ] 2.2 Define the Common Lisp dialect: full base, CL vocabulary map + adapters over the shared core, `nil`-only truthiness, Lisp-2, CL reader flags. Acceptance: each 1.1 assertion is satisfiable when the CL dialect is selected explicitly.
- [ ] 2.3 Add CL adapters where CL semantics differ from the shared core (argument order, multi-list variants). Acceptance: covered CL functions behave per CL, over one shared implementation.
- [ ] 2.4 Pin the Clojure identity invariant: assert `clojure.Dialect().IsIdentity()` is `true` as a unit test against the `clojure` package. The implementation in 2.1 must be a bare `FullDialect()` with no vocabulary map and no axis changes — adding a vocab map "for naming clarity" would silently break the bytecode VM. Acceptance: 1.4 green; the test fails if Clojure is ever given a non-identity feature.

## 3. Flip the default

- [ ] 3.1 Make `runtime.New()` resolve to the Common Lisp dialect when no `WithDialect` is given. Acceptance: 1.1 goes green.
- [ ] 3.2 Enumerate and pin every existing test, example, and the yagel consumer that relied on the old default to `WithDialect(clojure.Dialect())`. The bytecode-VM sites in `runtime/bytecode_test.go` (13 calls to `New(nil, WithBytecode())` at lines 18, 36, 70, 84, 101, 117, 130, 144, 159, 174, 187, 200, 212) and `runtime/fallback_test.go` (1 call at line 18) are pinned to `WithDialect(clojure.Dialect())` because the bytecode VM rejects non-identity dialects at construction. The other test files (`runtime/engine_test.go`, `runtime/eval_test.go`, `runtime/integration_test.go`, `runtime/plugin_test.go`, `runtime/repl_test.go`, `runtime/stats_test.go`, `runtime/watch_test.go`, `runtime/bench_test.go`) and any examples are enumerated and pinned in the same pass. Acceptance: full suite green under the new default; `go test ./...` passes with the flip in place.

## 4. Verify

- [ ] 4.1 Full suite green; CL Engines confirmed to evaluate on the tree-walker, VM unchanged. Acceptance: `go test ./...` passes; the bytecode-compatibility test for `WithDialect(clojure.Dialect())` (1.4) is green; the construction-error test for `New(nil, WithBytecode())` (1.3) is green.
- [ ] 4.2 Update the changelog `[Unreleased]` with the breaking default change and the `WithDialect(clojure.Dialect())` migration note. Acceptance: entry present.
- [ ] 4.3 `openspec validate dialect-common-lisp-default --strict` passes.
