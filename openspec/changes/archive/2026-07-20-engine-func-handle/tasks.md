## 1. Handle API

- [x] 1.1 `Fn` type + `Engine.Func(name)` in `runtime`: resolve root-env cell + stats counter once; error on undefined name; godoc for both.
- [x] 1.2 `Fn.Call`: entry ctx check → cell read (tombstone → `undefined function: <name>` error) → shared boundary invoke → counter bump; callback event only when registered.
- [x] 1.3 Refactor `Engine.Call` onto the shared boundary helper; behavior byte-identical for existing callers.

## 2. Lazy deadline

- [x] 2.1 VM: store timeout duration + caller-deadline instant; arm the engine bound at the first `pollCancel` with the ADR 0010 suppression rule; remove eager `time.Now` from the bytecode boundary entries (`Call`, `Fn.Call`, `Eval` via `runVM`).
- [x] 2.2 Tests: deadline still fires (short timeout, long loop); earlier caller deadline still governs alone; `WithTimeout(0)` still unbounded; short calls make zero `time.Now` calls when no callbacks are registered (alloc/profile assertion or clock-injection seam).

## 3. Handle semantics tests

- [x] 3.1 Rebind visible: `Func("f")`, rebind `f`, `Fn.Call` observes the new binding.
- [x] 3.2 Delete after resolution: `Fn.Call` errors with the same shape as `Engine.Call` on an undefined name.
- [x] 3.3 Concurrent `Fn.Call` from N goroutines: correct results, `-race` clean.
- [x] 3.4 `Stats()` counts handle calls under the function's name, identical to named calls; callbacks fire with durations when registered.
- [x] 3.5 Lisp-2 dialect: `Func` resolves through the function cell (parity with `Bind`'s mirroring), covered for CL.

## 4. Docs and bench

- [x] 4.1 README embedding section: handle usage; CHANGELOG Added entry.
- [x] 4.2 Benchmarks: `BenchmarkEngine_FuncCall` (+ callback variant) in `runtime/bench_test.go`; goldset boundary cell for the handle path if the gate covers boundaries.
- [ ] 4.3 Bench repo follow-up (separate repo, after release): Call/Callback/Rule move to the handle.

## 5. Verify

- [x] 5.1 `go test ./...`, `-race`, crossval green.
- [x] 5.2 `GOLDSET_MODE=vm` gate non-increasing; boundary cells reflect the removed clock reads.
- [x] 5.3 Bench evidence (benchstat ≥6): handle call ns/op and allocs recorded against `Engine.Call` and the competitors' numbers.
