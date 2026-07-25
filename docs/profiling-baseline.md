# Profiling baseline

Captured 2026-07-25 at commit `5255b25`, via `make profile` / `make profile-report`
(`Makefile`). Fixed parameters: `GOMAXPROCS=2`, `BENCHTIME=200ms` — the same values
the release gate uses (`.github/workflows/release.yml`), so this baseline is
comparable to what the gate measures. Vehicle: `internal/goldset`'s
`BenchmarkGoldset`, all 13 fixtures, both execution modes.

**Read this profile for what it is before trusting the percentages below.**
`BenchmarkGoldset` calls `eng.Eval(ctx, fx.Name, fx.Source)` inside the timed
loop — every `b.N` iteration re-tokenizes and re-parses the fixture source, then
evaluates it once. Parsing therefore owns roughly a third of CPU time and three
quarters of allocations in both modes (see cumulative tables below). A real
embedder that parses a rule once and evaluates it repeatedly — the shape
`BenchmarkFibonacci_VM` in `core/vm/bench_test.go` uses, and the shape the old
fib(20) profile in the archived `vm-dispatch-loop-tightening` design doc used —
would not pay that cost per call. Treat the goldset numbers as "cost of this
gate's own workload," not as "cost of pure evaluation."

**Consequence for the gate itself, not just for reading this doc:** parsing
cost is identical between eval and VM mode — it happens the same way in both
before either evaluator sees the form. `internal/perfgate/tiers.json` marks
seven of these thirteen cells `engine-sensitive`, meaning the gate exists to
catch regressions *in the evaluator*. A cost that both modes pay equally
dilutes exactly the signal those cells are supposed to isolate: a real
evaluator regression has to move a benchmark's total time enough to clear the
gate's non-regression tolerance despite ~35-38% of that time being unrelated,
identical-between-modes parse work. As currently shaped, the gate
systematically under-detects the class of regression it was built for. This is
filed as its own change (hoist parsing out of `BenchmarkGoldset`'s `b.N` loop)
rather than fixed here, because it changes what the gate measures and
invalidates every stored `bench-vm.txt` baseline — the same one-way door as
the fixed run parameters above.

## Eval mode (tree-walker)

Top CPU, flat:

| flat% | cum% | function |
|---|---|---|
| 6.92% | 6.92% | `sync/atomic.StorePointer` |
| 5.14% | 17.19% | `core.(*Reader).Tokenize` |
| 4.94% | 4.94% | `time.runtimeNow` |
| 4.55% | 4.55% | `sync/atomic.(*Int64).Add` |
| 3.56% | 3.56% | `sync/atomic.(*Int32).Add` |
| 3.36% | 18.77% | `core.(*Parser).parseForm` |
| 2.77% | 2.77% | `core.(*Reader).peek` |
| 2.17% | 10.87% | `runtime.mallocgcSmallScanNoHeader` |

`StorePointer`/`Int64.Add`/`Int32.Add`/`runtimeNow` are the cancellation/budget
bookkeeping (atomic counters + clock checks) charged on every eval step —
non-trivial but not a single dominant offender.

Top CPU, cumulative (internal frames only; `testing`/harness wrappers at ~90%
omitted):

| cum% | function |
|---|---|
| 50.59% | `core.(*engine).Eval` |
| 43.87% | `core.(*engine).evalList` |
| 36.76% | `runtime.(*engineImpl).readForms` (parse) |
| 36.17% | `core.Dialect.ReadWithMaxDepthStats` (parse) |
| 24.70% | `core.(*engine).evalBody` |
| 22.13% | `core.(*engine).apply` |
| 18.77% | `core.(*Parser).Parse` / `parseForm` |
| 17.39% | `core.(*Parser).parseList` |
| 17.19% | `core.(*Reader).Tokenize` |
| 15.42% | `runtime.mallocgc` |

Reading: parsing (`readForms` → `Parse` → `Tokenize`/`parseForm`/`parseList`) is
~37% of CPU on its own; the rest is the tree-walker's `Eval`/`evalList`/
`evalBody`/`apply` chain, which is the expected shape for a recursive evaluator.

Top allocation sites (`-alloc_space`, total 4486.54MB over the run):

| flat% | cum% | site |
|---|---|---|
| 56.79% | 56.86% | `core.(*Reader).Tokenize` |
| 7.90% | 16.57% | `core.(*Parser).parseList` |
| 4.94% | 4.94% | `core.NewEnv` |
| 4.61% | 4.61% | `core.(*HashMap).Set` |
| 3.91% | 17.48% | `core.(*Parser).parseForm` |
| 3.39% | 3.39% | `core.(*Env).localCell` |
| 2.88% | 5.87% | `core.(*Parser).parseVector` |

`Tokenize` alone is 57% of all bytes allocated by this benchmark — cumulative
parse cost (`Dialect.ReadWithMaxDepthStats`) is 75.34% of total allocation.
Evaluation-side allocation (`NewEnv`, `localCell`, `HashMap.Set`) is small by
comparison in this harness.

## VM mode

Top CPU, flat:

| flat% | cum% | function |
|---|---|---|
| 4.87% | 21.66% | `core.(*Parser).parseForm` |
| 4.51% | 4.51% | `sync/atomic.StorePointer` |
| 4.33% | 25.09% | `core/vm.(*VM).run` |
| 3.43% | 12.82% | `runtime.mallocgcSmallScanNoHeader` |
| 3.25% | 3.25% | `sync/atomic.(*Int64).Add` |
| 2.89% | 14.80% | `core.(*Reader).Tokenize` |
| 2.35% | 2.35% | `context.value` |
| 2.17% | 2.17% | `runtime.memclrNoHeapPointers` |

Top CPU, cumulative (internal frames):

| cum% | function |
|---|---|
| 45.67% | `runtime.(*bytecodeEvaluator).EvalCached` |
| 37.73% | `runtime.(*engineImpl).readForms` (parse) |
| 37.36% | `core.Dialect.ReadWithMaxDepthStats` (parse) |
| 27.62% | `runtime.(*bytecodeEvaluator).runVM` |
| 25.63% | `core/vm.(*VM).Run` |
| 25.09% | `core/vm.(*VM).run` |
| 21.84% | `core.(*Parser).Parse` |
| 21.66% | `core.(*Parser).parseForm` |
| 20.04% | `core.(*Parser).parseList` |
| 18.77% | `runtime.mallocgc` |
| 14.80% | `core.(*Reader).Tokenize` |

Reading: parsing is ~38% of CPU, essentially tied with `EvalCached`'s own
compile+run cost (~46%, of which `VM.run` proper is ~25%). Compared to eval
mode, the VM's own dispatch loop (`VM.run`) is a smaller, flatter cost than the
tree-walker's `Eval`/`evalList`/`evalBody`/`apply` chain — consistent with the
VM being the leaner execution path once you're past parsing.

Top allocation sites (`-alloc_space`, total 5062.68MB over the run):

| flat% | cum% | site |
|---|---|---|
| 58.60% | 58.65% | `core.(*Reader).Tokenize` |
| 8.14% | 16.22% | `core.(*Parser).parseList` |
| 5.39% | 5.39% | `core.(*HashMap).Set` |
| 3.49% | 16.92% | `core.(*Parser).parseForm` |
| 3.11% | 3.11% | `runtime.sha256Hash` |
| 2.57% | 6.10% | `core.(*Parser).parseVector` |
| 2.42% | 2.42% | `newEvalStateWithLimits` |
| 2.23% | 2.60% | `core.NewUndefinedError` |

Same shape as eval mode: `Tokenize` is 59% of bytes, parsing overall 76.42% of
total allocation. `sha256Hash` (chunk-cache keying) and `NewUndefinedError` are
VM-mode-specific and each under 3.5% — not worth chasing from this baseline.

## Measurement discipline

- Run on a quiet machine: check `uptime` and `pgrep -af 'go run|corpus'` first;
  an unrelated `go run ./cmd/corpus api` process runs on this box and bursts.
  Both captures here ran at 1-minute load averages of 0.23–0.35 with no other
  CPU-heavy work in flight.
- Timing (ns/op, flat/cum ms, flat/cum %) varies run to run — this baseline was
  captured twice and individual node *rankings* among entries within a few
  points of each other reordered between runs (e.g. `Tokenize` was CPU-flat #1
  in one VM-mode capture and #6 in the other). Treat timing-based rankings as
  noisy below ~5 percentage points at this `-benchtime`; re-measure rather than
  conclude on a single capture. The structural picture — parsing ~35-38% of
  CPU and ~75% of allocation in both modes, global-lookup cost near-zero — held
  in both captures.
- `allocs/op` and `B/op` from `go test -benchmem` are deterministic and are the
  trustworthy signal when timings disagree between runs.

## 3a. Do global lookups still dominate?

**No.** The old fib(20) profile (archived
`openspec/changes/archive/2026-07-18-vm-dispatch-loop-tightening/design.md`)
attributed 33% of CPU to global lookups. In the current `internal/goldset` VM
profile, `core/vm.(*VM).resolveGlobalValue` is 0.72–0.84% flat / 1.18–1.26% cum
(both captures) — a rounding error, not a dominant cost. `LookupAndMaterialize`
(lazy stdlib resolution) adds another ~1% cum. `Chunk.site`, the per-chunk site
cache accessor, is 2.43% flat/cum in one capture and does not surface separately
in the other.

That comparison mixes workload shapes, though (goldset re-parses every
iteration; the old fib(20) profile didn't), so as a cleaner apples-to-apples
check I also profiled `BenchmarkFibonacci_VM` (`core/vm/bench_test.go`) directly
— same methodology as the old study: parse once outside the timed loop, then
run `fib(15)` (deep recursion, many `fib`/`+`/`-`/`<` global lookups) repeatedly
inside it. Two captures at `-benchtime=3s`:

| | capture 1 | capture 2 |
|---|---|---|
| `resolveGlobalValue` flat | 6.16% | 6.17% |
| `resolveGlobalValue` cum | 6.90% | 8.40% |
| `(*VM).run` cum | 98.51% | 97.94% |

Global lookups went from 33% to ~6-8% of CPU on the same kind of workload. The
per-chunk global site cache (`core/vm/chunk.go` `siteTable`/`siteCache`/
`siteEntry`, `resolveGlobalValue` in `core/vm/vm.go`) did what it was built to
do. Global lookups no longer dominate by either measure.

## 3b. Do nativeAdd and siblings still fail to inline?

**Yes, unchanged in kind.** `go build -gcflags='-m -m' ./core/vm/...` (the
first `-m` alone only prints positive inlining decisions; the negative ones
with cost/budget need the second `-m`):

```
core/vm/vm.go:1249:6: cannot inline nativeAdd: function too complex: cost 174 exceeds budget 80
core/vm/vm.go:1277:6: cannot inline nativeSub: function too complex: cost 418 exceeds budget 80
core/vm/vm.go:1323:6: cannot inline nativeMul: function too complex: cost 176 exceeds budget 80
core/vm/vm.go:1351:6: cannot inline nativeDiv: function too complex: cost 513 exceeds budget 80
core/vm/vm.go:1385:6: cannot inline nativeOrder: function too complex: cost 307 exceeds budget 80
core/vm/vm.go:1404:6: cannot inline nativeEq: function too complex: cost 185 exceeds budget 80
```

Old finding: cost 156-513 vs budget 80. Current: cost 174-513 vs budget 80 —
same range, `nativeAdd` even slightly higher (156→174). All six still fail by a
wide margin (smallest, `nativeAdd` at 174, is more than double the budget).
Their own dispatch callers don't inline either: `execNative` (cost 791),
`dispatchNativeOp` (cost 671), `(*VM).execNativeFastFused` (cost 239) all fail
the same 80 budget — expected, since these are per-instruction dispatch, not
tight-loop leaves, so their own non-inlining matters less than whether the
arithmetic leaves they call do.

## Which later stages this supports

- **Native-arithmetic inlining work (Stage G): justified.** 3b confirms the
  original motivating cost is still there, unchanged in kind, in the exact
  functions the old design doc named.
- **Tree-walker modernization — slot frames, flat closures, dropping the
  per-lookup `RWMutex` in `core/env.go` (Stage F): not justified by this
  baseline alone.** This profile's own numbers point elsewhere: in eval mode,
  parsing is ~37% of CPU and the tree-walker's `Eval`/`evalList`/`evalBody`/
  `apply` chain is the rest, with no single function standing out as an
  environment/lookup hot spot the way the old 33% global-lookup figure did for
  the VM: no `core.(*Env)` method appears in either mode's cumulative top-15,
  and in the flat top-20 the only one that shows up at all is
  `(*Env).SetWithContext` at 1.98%/5.53% cum — in one of the two captures,
  not both. Before committing to that stage, profile a tree-walker workload that
  isolates evaluation from parsing (`BenchmarkFibonacci_TreeWalker` in
  `core/vm/bench_test.go` is already shaped that way) — this baseline didn't
  do that because the mandated vehicle was the goldset harness, and the goldset
  harness reparses every iteration.

## Reproducing

```sh
make profile
make profile-report
```

`profile` captures both modes in one invocation — it sets `GOLDSET_MODE`
internally per mode, so nothing in the shell environment needs to. Its
`PROFILE_GOMAXPROCS`/`PROFILE_BENCHTIME` variables must match the release
gate's `env:` block (`.github/workflows/release.yml`) for the numbers to stay
comparable.

On this machine, `/tmp` sits at 80% (13G of 16G) with a per-user quota on top.
The full suite, and any `-v` or `-race` run, can fail packages at the link step
with `mapping output file failed: disk quota exceeded` (or `compile: writing
output: ... disk quota exceeded`) — that reads like a test failure but isn't
one. Point `TMPDIR` outside `/tmp` before a full-suite or race run, e.g.
`TMPDIR=/home/zhuk/.cache/go-lispico-tmp go test ./... -race -count=1`. Not
wired into the Makefile: it's a condition of this machine, not the project —
CI runners have ample `/tmp`, so hardcoding it there would be wrong.
