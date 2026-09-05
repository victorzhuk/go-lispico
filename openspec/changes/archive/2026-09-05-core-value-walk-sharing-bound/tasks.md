## 1. Pin the defect

- [x] 1.1 Add regressions that fail today: a shared structure of bounded ledger cost drives each of `ValueDeepBytes`, `String`, `ValueNodeCount` and the construction-depth check past a stated work ceiling, and each walk ignores a cancelled context, an expired evaluation deadline, and an exhausted reduction budget.
- [x] 1.2 Record the measured before/after quantities for the shared case so the charge movement in task 3.2 is evidence rather than assertion.

## 2. Bound the walks

- [x] 2.1 Choose and implement the bound per walk — sharing-aware traversal, an interruptible node budget, or both — and verify a wide, shallow, heavily shared structure terminates within the stated ceiling.
- [x] 2.2 Thread a context through the walks and their callers so the three Terminal classes are observable mid-walk; verify Terminal precedence matches the rest of the engine.
- [x] 2.3 Verify `core` keeps its zero external imports and that ordinary values within the depth limit are walked exactly as before.

## 3. Settle the consequences

- [x] 3.1 Retire the `unbounded-tracked` disposition in `internal/inventory`; verify each phase that carried it now has a budgeted owner or a bounded-exception proof that holds under sharing.
- [x] 3.2 Re-measure the gold set and any allocation charge that moves; verify each movement against the task 1.2 baseline and update the recorded cells deliberately.
- [x] 3.3 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.
