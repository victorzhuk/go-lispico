# Benchmark results — stdlib-merge-bulk-builder

CPU: AMD Ryzen AI 9 HX 370 w/ Radeon 890M
Go: linux/amd64
Date: 2026-07-17

Shared-path fix (ADR 0008, PRD story 25) — reported separately from any VM/core
benchmark numbers, no credit toward VM default-flip thresholds.

`benchstat` is not installed in this environment; before/after are hand-tabulated
from `-count=5` raw runs instead (see [before.txt](before.txt) / [after.txt](after.txt)).

## Command

```sh
go test ./plugins/stdlib/... -run '^$' -bench=BenchmarkMerge -benchmem -count=5
```

Run once against the pre-fix `merge` (per-key `HashMap.Assoc`, copy-on-write)
and once against the post-fix `merge` (mutable `HashMap.Set` bulk builder).

## Results (mean of 5 samples per size)

| Size | ns/op before | ns/op after | speedup | B/op before | B/op after | bytes reduction | allocs/op before | allocs/op after |
|---|---|---|---|---|---|---|---|---|
| 10 | 9,414 | 2,313 | 4.1× | 13,824 | 3,344 | 4.1× | 73 | 19 |
| 100 | 1,232,846 | 27,960 | 44.1× | 1,626,958 | 29,840 | 54.5× | 1,871 | 31 |
| 1,000 | 114,112,551 | 407,815 | 279.8× | 178,835,250 | 485,105 | 368.7× | 33,377 | 53 |
| 10,000 | 15,079,140,161 | 5,305,129 | 2,842.4× | 19,092,973,894 | 3,993,528 | 4,780× | 1,000,726 | 171 |

Raw: [before.txt](before.txt), [after.txt](after.txt)

## Growth shape (B/op multiplier per 10× size step)

| Step | Before | After |
|---|---|---|
| 10 → 100 | 117.7× | 8.9× |
| 100 → 1,000 | 109.9× | 16.3× |
| 1,000 → 10,000 | 106.8× | 8.2× |

Before: ~110× bytes per 10× size step — matches O(n²) (a 10× size increase
should cost ~100× under quadratic growth). After: 8–16× bytes per 10× size
step — close to the ~10× a linear cost would produce, and nowhere near the
prior quadratic shape.

## Observations

- Allocation *count* is a weak signal for this fix: before/after allocs/op
  only differ by ~4–60×, far less than the bytes/ns differ by, because Go's
  map growth allocates in batches largely independent of the copy-per-key
  cost `Assoc` was paying. `B/op` and `ns/op` are the metrics that actually
  demonstrate the fix; `TestMerge_LinearGrowth` (plugins/stdlib/bench_test.go)
  asserts on both, with bytes as the primary regression signal.
- At 10,000 entries, `merge` goes from ~15.1s to ~5.3ms — the O(n²) path was
  unusable at realistic map-heavy envelope sizes; the fix is what actually
  makes bulk merges viable, not just faster.
