## 0. Baseline and prerequisites

- [x] 0.1 Confirm the settlement/event mismatch at commit `3bcf9c1` against the implementation branch; record baseline failures and verify no semantic dependency on the deadline or plugin rollback changes. Coordinate runtime file edits in the documented merge order.
- [x] 0.2 Confirm existing-service-strict, regression-first coverage using `recordingMeter` and existing stats/panic fixtures; verify the Makefile limits and use bounded raw `go test` only for focused coverage absent from its targets.

## 1. Pin observable outcomes

- [x] 1.1 Add failing retained-denial regressions for `Eval`, `EvalWithBindings`, and `LoadScope` under both evaluators and meter attachment modes; verify returned error cause, event failure, error count, and retained binding behavior together.
- [x] 1.2 Add ordering assertions that callbacks observe completed settlement and unused-lease return, with exactly one event/count for success, setup failure, parse error, returned evaluation error, and recovered GoFunc panic; verify current gaps fail before refactoring.
- [x] 1.3 Pin terminal settlement-error precedence over a nonterminal evaluation error and existing scope/error-wrapping behavior; verify tests distinguish selected cause without requiring wrapper pointer identity.

## 2. Publish after finalization

- [x] 2.1 Restructure `Eval` completion into panic recovery, owned lifecycle settlement, final error selection, then one stats/event publication; verify all evaluation and early-return cases from section 1 pass without duplicate lease return.
- [x] 2.2 Apply the same ordering to `evalWithBindingScope`, leaving `EvalWithBindings` delegation and `LoadScope` scope ownership intact; verify each invocation publishes exactly once.
- [x] 2.3 Preserve charge-after-write effects, terminal-error precedence, and callback-panic policy while measuring duration through settlement; verify the existing meter and panic characterization tests remain unchanged in meaning.

## 3. Validate and document

- [x] 3.1 Run `go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(Meter_|Engine_OnEval|PanicBoundary|Eval|LoadScope)'`, ensuring every new regression matches the focused selection; record commands, test names, and results.
- [x] 3.2 Run `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'` and affected runtime coverage with `-race` under the same limits; classify unrelated baseline failures without relaxing checks.
- [x] 3.3 Update existing metering/observation documentation and CHANGELOG `[Unreleased]`; verify settlement-inclusive duration, early-failure counting, and preserved ordinary evaluation writes are explicit.
- [x] 3.4 Run `make lint` and `openspec validate evaluation-outcome-settlement --strict --json`; verify no plugin lifecycle or callback-panic redesign entered the diff.
