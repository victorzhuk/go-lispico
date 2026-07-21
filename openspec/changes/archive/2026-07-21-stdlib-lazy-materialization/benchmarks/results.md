# stdlib-lazy-materialization — benchmark evidence

Machine: AMD Ryzen AI 9 HX 370, linux/amd64, count=6, benchtime=1s, `-benchmem`.
Master baseline captured at 696a72e (identical code to d08fef7, docs-only delta).
Benchstat: `go run golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d`.

## Gate (task 4.1): goldset non-increasing

`GOLDSET_MODE=vm`, master vs branch:

- time: geomean −10.58%
- B/op +0.00%; allocs/op +0.00% (byte-identical)

`GOLDSET_MODE=eval`, master vs branch:

- time: geomean −3.43%
- B/op +0.00%, allocs/op +0.00% (byte-identical)

## Targets (task 4.2)

| Benchmark | lazy | eager (pre-change behavior) | delta |
|---|---|---|---|
| Engine construction (`BenchmarkEngine_Creation`) | 6.6–7.4 µs / 75 allocs | same | ≤ 10 µs target MET |
| Use-only floor (`BenchmarkEngine_UseStdlibBytecode`) | 24–28 µs / 242 allocs | 92–100 µs / 697 allocs | −73% time, −65% allocs |
| Startup + first eval, warm (`cache-warm` vs `eager-warm`) | 51–60 µs / 359 allocs | 109–118 µs / 759 allocs | −52% time, −53% allocs |
| Startup + first eval, cold (`cache-disabled`) | 60–65 µs / 454 allocs | (master: 113 µs / 849 allocs) | −45% |
| Full-surface script (46 forms, all names) | 294–314 µs / 2469 allocs | 279–285 µs / 2245 allocs | ~+6% (converged) |

GopherLua's 70 µs startup row: beaten (51–60 µs warm; 42 µs on an unloaded
machine). Small-script working set pays only what it touches; full-surface
scripts pay deferred work plus ~6% per-name sync overhead.

Files: master-{eval,vm,startup}.txt, branch-{eval,vm,startup}.txt.
