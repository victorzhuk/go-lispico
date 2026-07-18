## 1. Batched checks in the VM

- [ ] 1.1 Add countdown-budget cancellation check to `VM.Run` (interval constant, forced check at `OpLoop` back-jump and in `vm.call` for closures and GoFuncs); remove the per-instruction `ctx.Err()`.
- [ ] 1.2 Add the deadline-instant field to `VM` (set alongside `SetGlobals` on the pooled instance) and fold the deadline compare into the same check.
- [ ] 1.3 Tests: cancelled ctx observed within the budget on straight-line code; within one iteration inside `loop`; within one call in deep recursion; error text/wrapping unchanged.

## 2. Batched checks in the tree-walker

- [ ] 2.1 Carry the countdown budget and deadline instant in `evalState`; replace per-node `select` in `Eval` and per-iteration `select` in `apply` with the shared check.
- [ ] 2.2 Tests: same latency-bound scenarios as 1.3 on the tree-walker; `recur`/loop cancellation.

## 3. Engine deadline without a timer

- [ ] 3.1 Replace `withEvalTimeout`'s `context.WithTimeout` with deadline-instant computation per ADR 0010 rules (`timeout<=0` → none; earlier caller deadline → caller governs); thread the instant to both evaluators; GoFuncs receive the caller's ctx.
- [ ] 3.2 Amend ADR 0010 with the enforcement mechanism and the GoFunc context consequence.
- [ ] 3.3 Tests: all four existing deadline-ownership scenarios pass; no `time.newTimer`/`WithDeadlineCause` allocation on `Eval`/`Call` (AllocsPerRun or alloc profile assertion).

## 4. Verify

- [ ] 4.1 `go test ./...` and `-race` suites green.
- [ ] 4.2 Goldset gate non-regressing; boundary-call cells show the −4 allocs.
- [ ] 4.3 Bench evidence recorded: fib bytecode ns/op delta (target ~10%), `pprof` confirms `cancelCtx.Err` off the profile.
