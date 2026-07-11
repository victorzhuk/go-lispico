## 1. Pool the Apply/Call path (ARCH-1)

- [x] 1.1 Add public `ApplyPooled` method on `*vm.VM` in `core/vm/vm.go` that calls `vm.apply(...)` directly on receiver without `vm.New`, preserving `VM.Apply` fresh-isolation contract.
- [x] 1.2 Rewrite `bytecodeEvaluator.Apply` in `runtime/eval.go` to get/reset/set-globals/set-structural-counter/`ApplyPooled`/put through `be.vmPool` (mirroring `runVM`) instead of `vm.New(...)` per call.
- [x] 1.3 Test: `Engine.Call` returns correct results (sequential isolation, concurrent correctness). `core/vm`: `ApplyPooled` with reset receiver allocates fewer than fresh `Apply` per `testing.AllocsPerRun`.
- [x] 1.4 Test/verify result isolation: two sequential `Call`s on one engine do not leak stack/frame state (reset before reuse).

## 2. Doc alignment

- [x] 2.1 Rewrite the `cl/cl.go` package doc: CL runs on the bytecode VM via rename-normalization (ADR 0006); keep only the true parts (`IsIdentity()` still false). Remove the `IsIdentity()`-gate / "always tree-walker" claim.
- [x] 2.2 Add `fsm` (idle, no consumer) to the `CLAUDE.md` plugin status line, matching `README` and ADR 0004.

## 3. Verify

- [x] 3.1 `go test ./...` green including `core/vm/crossval_test.go` and `runtime/dialect_default_test.go`.
- [x] 3.2 `go test -race ./runtime/...` clean for concurrent `Call`.
