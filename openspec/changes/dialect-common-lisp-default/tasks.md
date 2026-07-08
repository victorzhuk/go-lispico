# Tasks — Common Lisp dialect and default flip

## 1. Red

- [ ] 1.1 At the `runtime` Engine seam, add failing characterization tests for `runtime.New()` behaving as Common Lisp: `defun` defines a function, `(if false :y :n)` is `:y` (nil-only), `funcall`/`#'` work, `#'f` and `#(...)` parse, `[1 2]` does not read as a vector literal. Acceptance: red — default is still the old flavor.
- [ ] 1.2 Add a test that `WithDialect(clojure.Dialect())` reproduces the pre-flip behavior exactly. Acceptance: red until the Clojure dialect exists.

## 2. Assemble dialects

- [ ] 2.1 Define the Clojure dialect: full base, current vocabulary, `nil`+`false` truthiness, Lisp-1, bracket literals on. Acceptance: 1.2 green.
- [ ] 2.2 Define the Common Lisp dialect: full base, CL vocabulary map + adapters over the shared core, `nil`-only truthiness, Lisp-2, CL reader flags. Acceptance: each 1.1 assertion is satisfiable when the CL dialect is selected explicitly.
- [ ] 2.3 Add CL adapters where CL semantics differ from the shared core (argument order, multi-list variants). Acceptance: covered CL functions behave per CL, over one shared implementation.

## 3. Flip the default

- [ ] 3.1 Make `runtime.New()` resolve to the Common Lisp dialect when no `WithDialect` is given. Acceptance: 1.1 goes green.
- [ ] 3.2 Enumerate and pin every existing test, example, and the yagel consumer that relied on the old default to `WithDialect(clojure.Dialect())`. Acceptance: full suite green under the new default.

## 4. Verify

- [ ] 4.1 Full suite green; CL Engines confirmed to evaluate on the tree-walker, VM unchanged. Acceptance: `go test ./...` passes.
- [ ] 4.2 Update the changelog `[Unreleased]` with the breaking default change and the `WithDialect(clojure.Dialect())` migration note. Acceptance: entry present.
- [ ] 4.3 `openspec validate dialect-common-lisp-default --strict` passes.
