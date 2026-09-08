## 0. Baseline and prerequisites

- [ ] 0.1 Confirm the deadline behavior and reviewed symbols against commit `3bcf9c1` and the implementation branch; record baseline failures and verify there is no semantic prerequisite. Coordinate overlapping edits before the other runtime lifecycle changes.
- [ ] 0.2 Confirm existing-service-strict, regression-first coverage in the existing deadline/boundary test files; inspect Makefile limits and record that targeted tests need raw `go test` because the wrapper has no focused-package target.

## 1. Reproduce the missing deadline

- [ ] 1.1 Add failing integration cases for context meter, engine meter, and existing state without a deadline across `Engine.Call`, `Fn.Call`, and `PinnedFn.Call`; verify a cooperative GoFunc observes the missing bound before the fix.
- [ ] 1.2 Add deterministic caller/inherited-deadline and timeout-disablement cases, including reentry into both evaluators; verify deadline equality, retained state/budget identity, and expiry error propagation without primary reliance on sleeps.

## 2. Compose deadline ownership

- [ ] 2.1 Arm an absent engine bound on the general call boundary without replacing inherited state or creating a second lease lifecycle; verify all cases from 1.1 pass through the common VM dispatch seam.
- [ ] 2.2 Preserve inherited deadlines in the tree-walker boundary and timeout-disablement path; verify the reentry and caller-bound cases from 1.2 pass.
- [ ] 2.3 Keep the lean call path and callback timing behavior intact; verify `TestEngineDeadline_UnobservedBoundaryReadsNoClock` and existing call-boundary flag/parity tests pass.

## 3. Validate and document

- [ ] 3.1 Run bounded focused coverage with `go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)'`; record commands, test names, and results, including every new regression.
- [ ] 3.2 Run `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'` and the affected runtime tests with `-race` under the same limits; classify baseline failures without weakening required checks.
- [ ] 3.3 Update the existing deadline documentation and CHANGELOG `[Unreleased]`; verify they describe meter-independent enforcement, inherited bounds, and unchanged explicit disablement.
- [ ] 3.4 Run `make lint` and `openspec validate metered-call-deadlines --strict --json`; verify the final diff contains only this change's deadline behavior, tests, and documentation.
