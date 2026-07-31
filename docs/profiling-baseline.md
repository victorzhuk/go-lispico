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
gate's non-regression tolerance despite a material share of that time being
unrelated, identical-between-modes parse work — see "Measured per-fixture
parse share" below for exactly how much, per fixture (it is not the flat
~35-38% this single profile capture suggested).

Hoisting parsing out of `BenchmarkGoldset`'s `b.N` loop was considered and
rejected: the only way to do it without widening the public `runtime.Engine`
surface is to bypass the Engine and hand-assemble `core.Read` +
`compiler.CompileAll` + `vm.Run`, which would stop measuring the chunk cache,
dialect vocabulary, stdlib materialization, and resource metering this gate
exists to cover — a different, narrower benchmark wearing the old name.
Instead, `BenchmarkGoldsetParse` (`internal/goldset/bench_test.go`) measures
the reader's share directly, alongside the unchanged `BenchmarkGoldset`, so
the dilution is quantified per fixture rather than removed.

## Measured per-fixture parse share

Added 2026-07-25 (commit `df7c77e` + `BenchmarkGoldsetParse`,
`internal/goldset/bench_test.go`): a benchmark that parses each fixture's
source alone, via `core.Read`, outside any timed evaluation.
`BenchmarkGoldset` itself is untouched — same name, same body, still
re-parsing every `b.N` iteration — so pairing the two gives a *measured*
per-fixture parse share instead of the profile-derived corpus-wide estimate
above. That estimate was a single pprof capture's CPU-sample average; the
real picture is far less uniform.

`GOMAXPROCS=2`, `benchtime=200ms`, `count=10`, `sec/op`, per fixture (`eval` /
`vm` label the two `BenchmarkGoldset` modes; `tier` is this fixture's
`internal/perfgate/tiers.json` classification):

| fixture | tier | eval mode: eval / parse | share | vm mode: eval / parse | share |
|---|---|---|---|---|---|
| counter-closure | engine-sensitive | 8.480µ / 3.399µ | 40.1% | 6.622µ / 3.271µ | 49.4% |
| guard-nil       | engine-sensitive | 3.004µ / 1.564µ | 52.1% | 4.048µ / 1.641µ | 40.5% |
| kw-lookup       | engine-sensitive | 4.206µ / 1.746µ | 41.5% | 3.591µ / 1.809µ | 50.4% |
| loop-sum        | engine-sensitive | 148.2µ / 1.963µ | 1.3%  | 13.90µ / 2.009µ | 14.5% |
| merge-config    | data-dominated   | 8.128µ / 3.203µ | 39.4% | 7.368µ / 3.198µ | 43.4% |
| pipeline        | data-dominated   | 21.75µ / 3.615µ | 16.6% | 11.22µ / 3.621µ | 32.3% |
| queue-promote   | data-dominated   | 79.04µ / 2.835µ | 3.6%  | 24.81µ / 3.397µ | 13.7% |
| registry-fold   | data-dominated   | 10.46µ / 3.695µ | 35.3% | 8.894µ / 3.735µ | 42.0% |
| route-decision  | engine-sensitive | 5.948µ / 2.871µ | 48.3% | 5.734µ / 2.981µ | 52.0% |
| rule-load       | startup          | 15.39µ / 8.373µ | 54.4% | 20.58µ / 9.242µ | 44.9% |
| safe-parse      | engine-sensitive | 9.592µ / 4.104µ | 42.8% | 6.034µ / 3.972µ | 65.8% |
| text-render     | data-dominated   | 4.810µ / 1.964µ | 40.8% | 4.245µ / 2.066µ | 48.7% |
| twice-macro     | engine-sensitive | 5.187µ / 2.364µ | 45.6% | 9.045µ / 2.151µ | 23.8% |
| geomean         | —                | 11.33µ / 2.897µ | 25.6% | 8.120µ / 2.969µ | 36.6% |

Range: 1.3% (`loop-sum`, eval mode) to 65.8% (`safe-parse`, vm mode) — an
order of magnitude wider than the single flat corpus-wide figure suggested.
The dilution this gate cares about (an evaluator regression getting masked by
shared parse cost) is not evenly distributed:

- `loop-sum` and `queue-promote`, the two costliest cells by raw eval time,
  have the *lowest* parse share (1.3-14.5%, 3.6-13.7%). A real evaluator
  regression in either clears the gate's tolerance easily — parse cost barely
  dilutes them.
- `safe-parse`, `route-decision`, `guard-nil`, `kw-lookup`, `rule-load` sit at
  40-66% parse share in at least one mode — for these, half or more of the
  measured cell is reader cost with nothing to do with the evaluator. A
  regression here has to be roughly 2x as large to move the cell's total by
  the same amount.

`BenchmarkGoldsetParse` cells are mode-invariant by construction — the
benchmark never reads `GOLDSET_MODE` — and two independent captures (labeled
`eval`/`vm` above only for pairing with the corresponding `BenchmarkGoldset`
run) confirm it: `B/op` and `allocs/op` are byte-identical across all 13
fixtures (`benchstat`, `p=1.000`, "all samples are equal"). `sec/op` agrees
within noise for 10 of 13 cells; 3 show a small but statistically significant
difference (`queue-promote` +19.84% p=0.009, `rule-load` +10.37% p=0.023,
`twice-macro` -8.99% p=0.011) that, given the identical allocation profile,
reads as inter-process scheduling jitter rather than a real cost difference —
nothing in the parse-only path branches on which label ran it.

`BenchmarkGoldset`'s own cells remain, and will remain, mixed parse+eval
measurements: splitting them cleanly would need a public `runtime.Engine`
method that evaluates already-parsed forms, judged not worth widening the
embedding surface for a benchmark (no real embedder hot path needs it — one
parses once via `LoadScope` and calls repeatedly via `Call`). This table is
the compensating control: it quantifies the dilution per fixture instead of
removing it.

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
  in both captures. That figure is a corpus-wide CPU-sample average, though;
  "Measured per-fixture parse share" above supersedes it with a direct
  per-fixture `sec/op` measurement, which varies far more widely (1.3-65.8%).
- `allocs/op` and `B/op` from `go test -benchmem` are deterministic and are the
  trustworthy signal when timings disagree between runs.
- Absolute timings in this document are dev-box captures and settle no bar.
  The one boundary that now has a hosted figure is `Engine.Call`, measured by
  the gate cell `GoldsetCall/call-boundary`: 188.50 ns/op on the runner, at
  `internal/perfgate/testdata/profile-30637802780`. Quote that figure with the
  qualifiers recorded alongside it — it is a hosted-runner number for the gold
  set's engine configuration, and it excludes the caller's variadic argument
  slice, which the cell hoists out of its timed loop. The same cell reads
  89.57 ns here, which is the size of the gap between this box and the gate.

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
