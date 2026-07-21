## 1. Behavior contracts

- [x] 1.1 Red tests, tree-walker: `(loop [] (try body (catch e nil)))` under a
  short engine deadline terminates with `context.DeadlineExceeded`; cancelled
  ctx not interceptable by `try`; `CodeResourceLimit` pass-through still holds
  (characterization).
- [x] 1.2 Red tests, VM: resource-limit raised inside a called closure /
  GoFunc is not caught by an enclosing `try`; cancellation surfacing through
  `OpCall` error routing is not caught; freeze stack and handler stack unwind
  cleanly (extend the existing freeze-unwind regression pattern).
- [x] 1.3 Characterization: explicit `(throw ...)` of arbitrary values,
  including error-looking strings, remains catchable on both evaluators.

## 2. Implementation

- [x] 2.1 `core.IsTerminalEvalError(err) bool`: `errors.Is` on
  `context.Canceled` / `context.DeadlineExceeded`; `errors.As` on
  `*LispicoError` with `Code == CodeResourceLimit`.
- [x] 2.2 `evalTry` uses the classifier in place of its inline
  resource-limit-only check.
- [x] 2.3 VM: filter before `vm.throw` at `OpCall`, `OpTailCall`, native-op
  dispatch, and the throw-coercion path; terminal errors take the direct
  return route.

## 3. Integration

- [x] 3.1 Crossval: same adversarial programs, same limits, both evaluators
  → same terminal error class, never caught.
- [x] 3.2 `go test ./... -race`; `GOLDSET_MODE=vm` goldset gate non-increasing.

## 4. Verification

- [x] 4.1 `openspec validate --strict eval-noncatchable-terminal-errors`.
