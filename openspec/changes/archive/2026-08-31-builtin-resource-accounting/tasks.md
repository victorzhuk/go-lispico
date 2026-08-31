- [x] 1.1 Add synthetic GoFunc tests for a long uninterrupted loop under caller cancellation, an expired Engine-owned deadline with a live parent context, and a low Reduction limit; add a VM case that performs substantial bytecode work before entering the GoFunc and verify the absolute deadline is not restarted.
- [x] 1.2 Add synthetic result tests for a wholly borrowed argument/default, a fresh result, and a mixed incrementally charged result; verify the centralized Evaluator, VM, re-entry, and direct Apply sites skip only callee-accounted bytes.
- [x] 1.3 Add error-precedence tests where a pending validation/callback error competes with final-flush success or a Terminal failure; verify the original non-Terminal error survives unless a Terminal error must win.
- [x] 2.1 Add `core.NewBuiltinWorkBudget(ctx)`, single-call `BuiltinWorkBudget.Step() error`, and idempotent `Flush() error` with 128-unit synchronization and a latched first sync error; verify no per-unit atomic/ledger operation or clock read occurs.
- [x] 2.2 Propagate the VM run's already resolved absolute deadline into re-entry evaluation state before GoFunc dispatch, retaining the earlier outer deadline; verify nested callbacks and late Builtin entry cannot reset the timeout.
- [x] 2.3 Keep per-unit `PollEvalState`, direct Reduction charging, and caller-only `ctx.Err()` out of the Builtin budget implementation; verify the maximum unobserved core-owned work is 127 units.
- [x] 2.4 Make zero bytes an explicit wholly borrowed result disposition while preserving fresh fallback and mixed incremental/deep charging behavior.

## 3. Record the metering contract

- [x] 3.1 Amend ADR 0011 and the `CONTEXT.md` Reduction definition with local Builtin work accrual, 128-unit synchronization, absolute VM deadline propagation, trusted-host boundaries, and borrowed-result accounting.
- [x] 3.2 Update public resource-limit documentation with the additional normative charge site, link the successor stdlib migration, and verify no text promises cross-engine counter equality.

## 4. Verify the foundation
- [x] 4.1 Run focused core metering, runtime deadline, VM re-entry, and GoFunc result-charge tests; verify all pass without requiring a production stdlib migration in this change.
- [x] 4.2 Run `go test ./...`, `go test -race ./core/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.
