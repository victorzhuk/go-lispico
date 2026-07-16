## 1. Keyword application (red → green)

- [ ] 1.1 Failing tests: `(:key m)` hit, miss (returns `nil`), default arg, wrong arity (typed error), and non-map argument behave identically under Evaluator and `WithBytecode()`, through `Eval` and `Call`.
- [ ] 1.2 Support Keyword callables in VM application with Evaluator-equal semantics.

## 2. Structural-depth restoration (red → green)

- [ ] 2.1 Failing test: after a VM evaluation fails (thrown error, depth ceiling, malformed input), a subsequent successful evaluation on the same Engine sees a fully restored structural-depth budget — including when the VM instance came from the pool.
- [ ] 2.2 Restore structural-depth accounting on every return and error path, including pooled reuse.

## 3. Pin state isolation and concurrency

- [ ] 3.1 Regression: macro redefinition interleaved with pooled `Apply`/`Call` uses the current macro definition (extends the existing epoch suite to the Apply path).
- [ ] 3.2 Regression: sequential pooled calls leak no stack, frame, or depth state between evaluations.
- [ ] 3.3 Race test: distinct Rule-style closures with separate environments run concurrently on one shared `WithBytecode()` Engine through `Call`; results correct, `go test -race` clean.

## 4. Verify

- [ ] 4.1 `go test ./...` and `go test -race ./runtime/... ./core/vm/...` green; `golangci-lint run` clean.
