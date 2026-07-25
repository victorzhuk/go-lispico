# Design

## What the split actually separates

Today one cell per fixture measures tokenize + read + compile + execute. Two of those four
are mode-invariant: both execution modes share the same reader, so parse cost enters every
cell identically. Summing a mode-invariant constant into a cell whose tier exists to detect
mode differences is what suppresses the signal.

The split is therefore not "make the benchmark faster" — it is "stop adding two unrelated
measurements together."

- `BenchmarkGoldset/<fixture>` keeps its name and becomes evaluation-only: parse once
  before `b.ResetTimer()`, evaluate the already-read forms inside the loop.
- `BenchmarkGoldsetParse/<fixture>` is new and measures the reader alone over the same
  source.

Keeping the existing cell's *name* matters: `internal/perfgate/tiers.json` keys on
benchmark names with the GOMAXPROCS suffix stripped, so every existing tier assignment
stays meaningful — the cell still measures the thing its tier describes, only more purely.

## Evaluating already-read forms

The benchmark cannot keep calling `eng.Eval(ctx, name, source)`, since that is the call
that parses. It needs an entry point taking forms rather than source. Read
`runtime/engine.go` and `runtime/eval.go` to find what the public API actually offers — do
not assume a method exists. If the only public path from source to result parses
internally, say so and stop, rather than reaching into unexported internals or adding a
public API purely to serve a benchmark; that would be a change to the embedding surface
disguised as a test change.

`core.Read` is the reader entry point used elsewhere (`core/vm/bench_test.go` calls it
directly), so the parse cell has a clear target.

## Tier assignments

Both sets need committed tiers before candidate results exist. The evaluation cells keep
theirs unchanged.

The parse cells need a fresh assignment, argued from `tiers.json`'s own stated rule rather
than by analogy: "Control-flow and dispatch-dominated cells are engine-sensitive;
collection/string-building cells are data-dominated; rule-load is startup-shaped." Parsing
is neither evaluator control flow nor collection building — it is a single-pass scan whose
cost tracks source length. Propose a tier, justify it against that sentence, and say
plainly if none of the three fits well; inventing a fourth tier is a larger decision than
this change should make alone.

Note that a parse cell is mode-invariant by construction: `GOLDSET_MODE` cannot change
reader cost. Whatever tier they get, the paired eval/vm comparison for those cells should
show no difference — and a difference would itself be a finding worth reporting.

## What must not change

- `TestGoldset` is untouched. Correctness still evaluates each fixture from source against
  its hand-derived golden, and goldens remain derived from the language contract rather
  than captured from either engine.
- `Fixtures()` file I/O stays outside every timed loop.
- The evaluation cells must exercise the same work per iteration as before minus parsing —
  not a smaller program, not a cached result. If the chunk cache means later iterations
  skip compilation, that was already true before this change; note it rather than fight it.

## Verification

The split is proved by the numbers separating, not by the code compiling:

- Eval cells should drop substantially, roughly in line with the 36-38% parse share the
  profiling baseline measured. A drop far from that is worth explaining before accepting.
- Parse cells should account for the difference.
- Eval-cell eval/vm deltas should become *larger* in relative terms than today, since the
  shared constant is gone. That is the point of the change, and the check that it achieved
  its purpose rather than merely moving numbers around.

Every stored baseline is already invalid — the profiling-harness change fixed `GOMAXPROCS`
and `BENCHTIME` — so there is nothing to compare for non-regression. Capture a fresh paired
baseline after the split and record it as the new starting point.

## Rejected alternatives

- **Hoist the parse and add no parse cells.** Restores evaluator sensitivity but leaves the
  reader with no gate coverage at all, on a project where every load and hot reload pays
  reader cost.
- **Leave it and document the dilution.** Keeps baselines valid and avoids a second one-way
  door, but permanently leaves seven of thirteen engine-sensitive cells less sensitive than
  their tier claims. The baseline already documents it; documentation has not made the gate
  work.
- **Change the fixtures to be parse-light.** Would shrink the constant without separating
  the signals, and would make the corpus less representative of embedder rule sources.
