## 0. Confirm the implementation baseline

- [x] 0.1 Confirm `minMaxFunc`, its existing numeric tests, and inventory entries still match the failure documented from `3bcf9c1`; record any intervening changes before implementation. This change has no predecessor; testing mode is existing-service-strict.

## 1. Pin exact extrema before the fix

- [x] 1.1 Extend `TestArithmetic_MinMax` with singleton signed endpoints, adjacent integers around positive and negative `2^53`, both argument orders, and mixed signs; verify the current implementation fails exact `core.Int` value assertions.
- [x] 1.2 Add public `Engine.Call` and source-evaluation coverage under `WithTreeWalker()` and `WithBytecode()`; verify both expose the integer defect while existing mixed `core.Float` results and typed errors stay pinned.

## 2. Preserve integer state through selection

- [x] 2.1 Keep exact integer candidates in `minMaxFunc` until a float operand requires promotion; verify the new extrema regressions and existing mixed numeric cases pass without changing argument traversal accounting.
- [x] 2.2 Update executable inventory entries only where the changed phases or return branches require it; verify numeric cancellation, deadline, reduction, and static completeness tests pass without relaxed checks.

## 3. Verify and document the correction

- [ ] 3.1 Run `make test GOTESTFLAGS="-timeout 2m -p 2 -parallel 2"` and `make lint`; record commands, results, and exact regression names, resolving all failures attributable to this change.
- [x] 3.2 Add a concise `[Unreleased]` fix entry to `CHANGELOG.md`; verify it describes exact integer extrema without claiming exact mixed float arithmetic.
