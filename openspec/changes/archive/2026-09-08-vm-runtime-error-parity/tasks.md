## 0. Predecessor and scope

- [x] 0.1 Verify `vm-local-stack-scope` has been implemented and archived before implementation here; read the resulting frame/operand contract and confirm its scope/capture regressions pass.
- [x] 0.2 Confirm existing-service-strict coverage of ordinary runtime errors, thrown values, terminal identity, metering settlement and pooled reuse; verify compile-time syntax and bytecode-validation failures remain outside the new catch-routing requirement.

## 1. Regressions before implementation

- [x] 1.1 Add new VM/runtime test `TestVMOrdinaryErrorCatchParity` covering `(try missing (catch e 42))`, undefined mutation, nested closure errors and runtime collection construction errors; assert exact expected handler values through both evaluators and confirm the current VM skips handlers.
- [x] 1.2 Add new test `TestVMUncaughtThrowParity` for `(throw "boom")`, a non-String throw and rethrow; assert `errors.As`, `ThrowError`, equal rendering and caught-value runtime type, then verify current VM uncaught throws fail those assertions.
- [x] 1.3 Add new test `TestVMErrorUnwindReuse` covering an error during native argument evaluation, nested handlers and a successful subsequent `Eval`/`Apply`; assert no stale operands, callee freeze records or reduced structural-depth allowance. Extend existing terminal-error tests with handler side effects that must remain absent for wrapped cancellation/deadline and resource-limit errors.

No project wrapper selects these packages/tests; use:

```sh
timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$'
```

## 2. Runtime routing and throw identity

- [x] 2.1 Route errors raised by valid runtime opcodes through a common policy using `core.IsTerminalEvalError` and existing handler unwinding; verify lookup, assignment, construction, call and native-operation cases reach the expected handler while terminal tests still bypass it.
- [x] 2.2 Return the original ordinary error when unhandled and preserve `ThrowError` plus rendering for an uncaught explicit throw; verify `TestVMUncaughtThrowParity`, existing structured-throw cases and supported evaluator re-entry paths retain value and error identity.
- [x] 2.3 Preserve resume positions, frame reloads, pending-charge settlement and all handler state restoration; verify `TestVMErrorUnwindReuse` and existing terminal/meter settlement tests pass without changing terminal-error precedence.

## 3. Integration and documentation

- [x] 3.1 Run `timeout 10m go test -timeout 2m -p 2 -parallel 2 ./core/... ./runtime ./internal/goldset`; record test names, commands and results for both shipped dialects, wrapped errors, ordinary and structured throws, and pooled application.
- [x] 3.2 Run `timeout 10m go test -race -timeout 2m -p 2 -parallel 2 ./core/vm ./runtime`; verify shared counters, handlers and reused VM state remain race-free.
- [x] 3.3 Update existing `ARCHITECTURE.md` error semantics and `[Unreleased]` in existing `CHANGELOG.md`; verify ordinary opcode errors are described as catchable, terminal classes remain excluded and uncaught throws retain their rendering and classification.
- [x] 3.4 Run `timeout 10m make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`, `timeout 5m make lint`, and `openspec validate vm-runtime-error-parity --strict --json`; record results and archive only after implementation validation passes.
