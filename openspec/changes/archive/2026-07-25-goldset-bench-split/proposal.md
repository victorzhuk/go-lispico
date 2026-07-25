## Why

`BenchmarkGoldset` calls `eng.Eval(ctx, fx.Name, fx.Source)` inside `b.N`
(`internal/goldset/bench_test.go:44`), so every iteration re-tokenizes and re-reads the
fixture source. The chunk cache does not prevent this: `runtime/eval.go:551` hashes the
source only *after* parsing it into forms, so the cache avoids re-compilation, never
re-parsing.

The profiling baseline measured the consequence — parsing owns roughly 36-38% of CPU and
75% of allocation in both execution modes (`docs/profiling-baseline.md`). That cost is
**identical across modes**, because both run the same reader. It is therefore a large
constant added to every cell, including the seven of thirteen that
`internal/perfgate/tiers.json` marks `engine-sensitive` precisely to isolate evaluator
differences.

The effect is that the gate is least sensitive exactly where it is meant to be most
sensitive. A real evaluator regression has to overcome a mode-invariant 36% before it can
move a cell past the ±5% non-regression tolerance, so the gate systematically
under-detects the class of regression it exists to catch.

Deleting the parse is not the fix either. Reader cost is real — embedders pay it on every
load and every hot reload — and a gate that measures only evaluation would never notice a
parser regression at all. The current shape cannot detect either cleanly because it mixes
them.

## What Changes

- New parse-only cells over the same fixtures measure the reader alone, so a reader
  regression is detectable and the parse component of each evaluation cell is quantified.
- They get committed tier assignments in `internal/perfgate/tiers.json`, before any
  candidate results exist, as ADR 0008 requires.

The evaluation cells are **not** split, and that is a deliberate narrowing. Doing so needs
an entry point that evaluates pre-parsed forms, and none exists: every `runtime.Engine`
method takes a source string and parses internally, `EvalCached` is an unexported method
on an unexported type, and `internal/goldset` is a separate package. The two ways around
that are both worse than the problem. Adding a public `EvalForms` would widen the
embedding surface to serve a benchmark, and is weakly justified on merit — an embedder
with a hot path uses `LoadScope` plus `Call`, not repeated `Eval` of the same source.
Bypassing the Engine to hand-assemble reader, compiler, and VM would stop measuring the
chunk cache, dialect vocabulary, stdlib materialization, metering, and the mode switch,
which is precisely what the gold set exists to measure, and `core/vm/bench_test.go`
already covers raw execution separately.

So the dilution is quantified rather than removed. That is a smaller result than intended,
and the honest one available without changing what the corpus measures or what the library
exposes.

## Capabilities

### Modified Capabilities

- `consumer-release-gate`: `Gold-set gate corpus` requires "benchmark cells over" the
  fixtures without saying what a cell measures. It is amended to require dedicated parse
  cells, and to record that the evaluation cells measure parsing and evaluation together
  because the public API accepts source rather than forms — so a tier assignment on those
  cells is read as a mixed measurement rather than a pure evaluator one.

## Impact

- Code: `internal/goldset/bench_test.go`, `internal/perfgate/tiers.json`. No production
  code, no change to `TestGoldset` — correctness still evaluates each fixture from source
  against its golden.
- **One-way door**: this changes what the gate measures, so every stored `bench-vm.txt`
  non-regression baseline becomes incomparable. Doing it now is materially cheaper than
  later, because fixing `GOMAXPROCS` and `BENCHTIME` in the profiling-harness change
  already invalidated those baselines — the cost is being paid once rather than twice.
- Risk: the split doubles the cell count and therefore the paired-run wall time. The
  control is that fixtures are microsecond-scale and `BENCHTIME` is 200ms, so the addition
  is bounded and measurable before committing to it.
- Risk: a parse cell measured through `Fixtures()` could accidentally include file I/O.
  Fixtures must be read once, outside the timed loop.
- Sequencing: before the remaining optimization stages, so their verdicts are judged by an
  instrument that can actually see them.
