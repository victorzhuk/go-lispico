## 1. Pool the Apply/Call path (ARCH-1)

- [ ] 1.1 Rewrite `bytecodeEvaluator.Apply` in `runtime/eval.go` to get/reset/put a `*vm.VM` through `be.vmPool` (mirroring `runVM`) instead of `vm.New(...)` per call.
- [ ] 1.2 Test: `Engine.Call` invoked repeatedly allocates no fresh VM per call (allocs/op assertion or pool-hit check) and returns results equal to the tree-walker.
- [ ] 1.3 Test/verify result isolation: two sequential `Call`s on one engine do not leak stack/frame state (reset before reuse).

## 2. Doc alignment

- [ ] 2.1 Rewrite the `cl/cl.go` package doc: CL runs on the bytecode VM via rename-normalization (ADR 0006); keep only the true parts (`IsIdentity()` still false). Remove the `IsIdentity()`-gate / "always tree-walker" claim.
- [ ] 2.2 Add `fsm` (idle, no consumer) to the `CLAUDE.md` plugin status line, matching `README` and ADR 0004.

## 3. Verify

- [ ] 3.1 `go test ./...` green including `core/vm/crossval_test.go` and `runtime/dialect_default_test.go`.
- [ ] 3.2 `go test -race ./runtime/...` clean for concurrent `Call`.
