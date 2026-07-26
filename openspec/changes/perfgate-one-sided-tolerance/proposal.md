## Why

The release gate fails a release for making the engine faster.

`evaluateWithinTolerance` tests `math.Abs(cell.Latency.DeltaPct) > tolerancePct`,
so a non-regression cell that improves beyond 5% fails exactly as one that
regresses beyond 5% does. Observed on real candidate runs today:

- `cache-hit-skips-expansion`: 20 PASS / 6 FAIL, where **all six failures were
  negative deltas** (−5.9% to −20%), with allocations down or flat on every cell
  and up on none.
- `idempotent-macro-rebind`: failed on its own headline result,
  `Goldset/twice-macro-2: latency delta -28.36% exceeds 5% tolerance`.

ADR 0008 does not ask for this. Its governing sentence is one-sided — "No cell
may **regress** beyond its tier's budget" — and it explicitly rejects a standing
improvement gate because "after authorization it punishes Evaluator
improvements, failing a release for making the fallback path faster". The
per-tier shorthand "within 5% latency" was read as a two-sided band, which
reintroduces the very failure mode the ADR declined. The same tier lines say
"bytes and allocation count non-increasing" — one-sided, and implemented as
such. Only latency was symmetric.

Two places where two-sided *is* correct, and both survive:

- A data/output-dominated cell under first authorization. There the two runs are
  the Evaluator and VM variants of one commit, and the cost is mode-invariant by
  classification — which is exactly the argument `tiers.json` records for
  putting the `GoldsetParse/*` cells in that tier. A mode-invariant cost that
  moves either way is a finding.
- The concurrent tier, whose timed figure may be a throughput measure where
  larger is better. A one-sided check cannot be written until that sign
  convention is stated, and no cell is currently classified concurrent.

## What Changes

- `evaluateNonRegression` bounds regression only, and is used for
  engine-sensitive and data/output-dominated cells in non-regression mode, and
  for the startup tier's percentage arm in that mode.
- `evaluateWithinTolerance` keeps its two-sided meaning and is now used only
  where that is intended: first-authorization data/output-dominated cells, and
  the concurrent tier.
- Byte and allocation-count checks are untouched. A faster candidate that
  allocates more still fails.
- ADR 0008 states the direction explicitly instead of leaving "within 5%" to be
  inferred, and `tiers.json`'s rationale for the parse cells is narrowed to the
  mode where it holds.

## Capabilities

### Modified Capabilities

- `consumer-release-gate`: `Paired release run` fixes the run parameters and the
  inconclusive-rerun rule but never says which direction a percentage bound
  applies in, which is how a symmetric reading survived. It gains that
  direction, the two exceptions, and three scenarios — a faster candidate
  passes; a faster candidate that allocates more still fails; a mode-invariant
  cost moving either way under first authorization still fails.

## Impact

- Code: `internal/perfgate/perfgate.go`, its tests, `tiers.json`'s comment, and
  ADR 0008.
- Effect on today's runs, re-judged with the fix: `cache-hit-skips-expansion`
  goes from 20 PASS / 6 FAIL to **26 PASS / 0 FAIL**; `idempotent-macro-rebind`
  and `compile-toplevel-defmacro` drop from 4 failures to 2.
- **This is a gate weakening in one direction, deliberately.** A cell that
  improves can no longer fail on latency. The check that would otherwise have
  caught "the cell stopped doing its work" is not this one — the gold set's
  correctness tests assert each fixture's expected result independently, and
  they run before timing per `Correctness precedes timing`. Bytes and allocation
  count also remain one-sided against increase, so a cell cannot get faster by
  allocating more without failing.
- Not fixed here, and previously misattributed: the residual failures are
  positive-delta noise, and every one investigated today cleared under a paired
  re-measure at `-count=12`. The cause is comparing two separately captured
  files, not cell size — `Paired release run` already requires one interleaved
  job, which CI does and ad-hoc local comparison does not. An earlier claim that
  the `GoldsetParse/*` cells are too noisy for the gate is withdrawn; no
  absolute-delta floor is added, since that would mask real regressions on fast
  cells to paper over a measurement practice.
