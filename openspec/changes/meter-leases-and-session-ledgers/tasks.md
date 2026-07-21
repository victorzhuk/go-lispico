## 1. Behavior contracts

- [ ] 1.1 Red tests per scenarios above; characterization coverage of unchanged no-meter behavior.
- [ ] 1.2 Concurrency tests: `go test -race` over multi-goroutine charge + lease/return patterns.

## 2. Implementation

- [ ] 2.1 Define `runtime.Meter` interface, `Lease` type, atomic-counter implementation, and a no-op default.
- [ ] 2.2 Add `runtime.WithMeter(ctx, m) context.Context` helper.
- [ ] 2.3 Read the meter at every engine entry point (`Eval`, `EvalWithBindings`, `Call`, `(*Fn).Call`); fall through to `evalState` when absent.
- [ ] 2.4 Route reduction / allocation / retained charges through the meter when present (uses Change 1 + Change 3 charge primitives).
- [ ] 2.5 Implement `runtime.NewChildMeter(parent, rank, limits)` with ranked reservation; exhaustion fails closed.
- [ ] 2.6 Implement `Lease.Return` releasing unused credit to the parent.

## 3. Integration

- [ ] 3.1 `go test ./... -race`; existing suite unchanged when no meter is present.
- [ ] 3.2 Verify cross-evaluator parity under a meter (crossval test).

## 4. Verification

- [ ] 4.1 `openspec validate --strict meter-leases-and-session-ledgers`.
