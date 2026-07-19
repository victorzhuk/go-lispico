## 1. Poll policy

- [x] 1.1 Remove forced `pollCancel` at `OpCall`, `OpTailCall`, and `OpLoop` in `core/vm/vm.go`; keep the budget check at the loop head.
- [x] 1.2 Start `run()` with `vm.budget = checkInterval`; update the `checkInterval` doc comment (first check no longer fires at instruction one).
- [x] 1.3 Confirm every boundary entry (`Engine.Eval`, `Engine.Call`, `applyBoundary`, `runVM`) still rejects an already-cancelled ctx before instruction one; add the check where missing.
- [x] 1.4 Share the evaluator's batched cancellation budget across reentrant bytecode `Eval` calls so short VM runs cannot indefinitely defer an engine deadline.

## 2. Tests

- [x] 2.1 Retarget latency tests: cancelled ctx observed within the instruction budget on straight-line code, inside `loop`/`recur`, and in deep recursion (replace "within one call/back-jump" assertions).
- [x] 2.2 Deadline-expiry tests keep their error shape (`errors.Is(err, context.DeadlineExceeded)` through the `vm:` wrap).
- [x] 2.3 Already-cancelled ctx at boundary entry returns immediately with no instruction executed.

## 3. Docs

- [x] 3.1 ADR 0010 amendment note: checkpoints are budget-only; a GoFunc extends the wall-clock window by its own duration.

## 4. Verify

- [x] 4.1 `go test ./...` and `-race` green; crossval parity suite green.
- [x] 4.2 `GOLDSET_MODE=vm` goldset gate non-increasing.
- [x] 4.3 Bench evidence (bench repo, benchstat ≥6 counts): fib bytecode delta recorded; `time.runtimeNow` off the fib profile top.
