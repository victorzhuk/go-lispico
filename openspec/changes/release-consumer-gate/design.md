# Design

## Job shape

One hosted release-CI job, triggered by the release workflow (trigger mechanics are an open PRD gap). The gate is self-contained: it runs the committed gold set (`internal/goldset`) — no consumer checkout, no cross-repo secret.

1. Check out the candidate. The gold set — rule-shaped `.lisp` fixtures with independent `.golden` expected results, plus the benchmark cells over them — is committed in this repo; YAGEL exports and refreshes it deliberately from its shipped Rules.
2. Run every gold-set fixture under both execution modes against its golden, and the race-enabled suite — untimed.
3. Run the Paired release run: Evaluator and VM samples interleaved per iteration (eval, then vm, ten times) in the same job, fixed `GOMAXPROCS` and benchtime, at least ten samples per cell per mode; identical cell names across the two output files so benchstat pairs them.
4. Evaluate each cell against its committed Hot-cell tier per ADR 0008's thresholds; emit the benchstat report and per-cell verdict as release evidence.

## Decision rules

- Inconclusive benchstat: one rerun at doubled benchtime; still inconclusive → improvement cells fail, non-regression cells pass.
- First authorization uses ADR 0008's improvement thresholds; subsequent releases compare candidate VM against the previous release's stored VM baseline as non-regression.
- Any cell timed under the race detector is a job bug: race runs are separate and untimed.
- The gate binary is built once and run directly: `go run` collapses exit codes to 1, which would hide the needs-rerun signal (exit 2).

## Open inputs (blocking an authoritative verdict)

The YAGEL-exported gold-set corpus (current fixtures are examples that keep the harness executable); envelope values (baseline/knee/boundary per data dimension); tier assignments from baseline profiles; fixed `GOMAXPROCS`/benchtime; release workflow trigger. The harness itself is dry-run verified end-to-end, including the inconclusive-rerun-resolve path.
