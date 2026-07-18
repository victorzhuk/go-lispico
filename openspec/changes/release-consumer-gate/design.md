# Design

## Job shape

One hosted release-CI job, triggered by the release workflow (trigger mechanics are an open PRD gap):

1. Check out the candidate and YAGEL's `gold` ref — a ref YAGEL advances to its blessed release. No revision pin is recorded in this repo; YAGEL owns when the pointer moves.
2. Point YAGEL's go-lispico dependency at the candidate via a `go.mod` replace directive.
3. Run YAGEL's correctness suites (Behavior goldens under both execution modes) and both repositories' race-enabled suites — untimed.
4. Run the Paired release run: Evaluator and VM benchmark variants interleaved in the same job, fixed `GOMAXPROCS` and benchtime, at least ten samples per cell, compared with benchstat.
5. Evaluate each cell against its committed Hot-cell tier per ADR 0008's thresholds; emit the benchstat report as release evidence.

## Decision rules

- Inconclusive benchstat: one rerun at doubled benchtime; still inconclusive → improvement cells fail, non-regression cells pass.
- First authorization uses ADR 0008's improvement thresholds; subsequent releases compare candidate VM against the previous release's stored VM baseline as non-regression.
- Any cell timed under the race detector is a job bug: race runs are separate and untimed.

## Open inputs (blocking full verification)

Creation of the `gold` ref in the YAGEL repo; envelope values (baseline/knee/boundary per data dimension); tier assignments from baseline profiles; fixed `GOMAXPROCS`/benchtime/interleaving order; release workflow trigger. All are named PRD gaps — the job lands wired but cannot produce an authoritative verdict until YAGEL publishes its harness.
