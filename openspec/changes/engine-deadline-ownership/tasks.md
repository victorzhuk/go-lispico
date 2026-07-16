## 1. Pin deadline behavior (red → green)

- [ ] 1.1 Failing test: with a caller context whose deadline is earlier than the Engine timeout, evaluation is bounded by the caller's deadline and the Engine creates no second timer (observable via the context returned to evaluation carrying the caller's deadline, not a shorter one).
- [ ] 1.2 Test: with a caller deadline later than the Engine timeout, the Engine's tighter bound still applies.
- [ ] 1.3 Test: `WithTimeout(0)` plus an undeadlined caller context runs unbounded by the Engine; `WithTimeout(0)` plus a caller deadline is bounded only by the caller.
- [ ] 1.4 Test: default construction keeps the 30-second bound on `Eval`, `EvalWithBindings`, and `Call`.

## 2. Implement the redundant-timer skip

- [ ] 2.1 In the evaluation entry points, create the Engine timer only when the caller's context has no deadline or a later one.

## 3. Document the contract

- [ ] 3.1 `WithTimeout` doc comment states the default, the `0` disable path and its full-coverage expectation (ADR 0010), and the redundant-timer skip.

## 4. Verify

- [ ] 4.1 `go test ./runtime/...` and full `go test ./...` green; `golangci-lint run` clean.
