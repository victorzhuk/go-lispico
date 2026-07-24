## 1. Behavior contracts

- [ ] 1.1 Red test: a bound `GoFunc` that panics, called via `EvalWithBindings`,
  returns a `PanicError` (not a propagated panic). Mirror the existing
  `panic_boundary_test.go` shape.
- [ ] 1.2 Red test: same panicking `GoFunc` via `LoadScope` returns a nil scope
  and a `PanicError`.
- [ ] 1.3 Red test: a watched file whose reload evaluation panics does not crash
  the process — the watcher reports a reload error and stays active for a
  subsequent successful reload (assert the process survives and the next reload
  applies).
- [ ] 1.4 Characterization: a normal `EvalWithBindings`/`LoadScope`/reload with
  no panic behaves exactly as before (result, child scope, applied bindings
  unchanged).

## 2. Implementation

- [ ] 2.1 Wrap `evalWithBindingScope` (`runtime/eval.go`) in the same
  deferred-recover → `core.NewPanicError` pattern as `Engine.Eval`, preserving
  the result/scope/err reset semantics; ensure any pooled VM is reset before Put
  on the recovered branch.
- [ ] 2.2 Wrap `fileWatcher.reloadFile` (`runtime/watch.go`) so a recovered
  panic becomes an error routed through the existing reload-error surface; log
  via the engine logger when no sink is wired; keep the watch loop running.

## 3. Integration

- [ ] 3.1 `go test ./... -race` green, including a `-race` run of the new watch
  test (background goroutine).
- [ ] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — recovery adds a defer
  only on the binding-scope and reload paths, not the `Call`/`Eval` hot paths;
  verify allocs/op on the goldset cells unchanged.
- [ ] 3.3 Crossval parity unaffected (no evaluator-semantics change).

## 4. Verification

- [ ] 4.1 `openspec validate --strict recover-public-eval-boundaries`.
- [ ] 4.2 CHANGELOG `[Unreleased]` under Fixed: `EvalWithBindings`/`LoadScope`
  and background hot-reload now recover `GoFunc` panics instead of escaping or
  crashing the host.
