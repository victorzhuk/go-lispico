# Design

## Job shape

One hosted release-CI job, triggered by the release workflow (trigger mechanics are an open PRD gap). The gate is self-contained: it runs the committed gold set (`internal/goldset`) — no consumer checkout, no cross-repo secret.

1. Check out the candidate. The gold set — rule-shaped `.lisp` fixtures with hand-derived `.golden` expected results, plus the benchmark cells over them — is owned by this repo (`internal/goldset`), modeled on embedder rule workloads, independent of any consumer.
2. Run every gold-set fixture under both execution modes against its golden, and the race-enabled suite — untimed.
3. Run the Paired release run: Evaluator and VM samples interleaved per iteration (eval, then vm, ten times) in the same job, fixed `GOMAXPROCS` and benchtime, at least ten samples per cell per mode; identical cell names across the two output files so benchstat pairs them.
4. Evaluate each cell against its committed Hot-cell tier per ADR 0008's thresholds; emit the benchstat report and per-cell verdict as release evidence.

## Decision rules

- Inconclusive benchstat: one whole-suite rerun at doubled benchtime, every cell re-judged from the rerun data (no first-attempt verdict is frozen); still inconclusive → improvement cells fail, non-regression cells pass.
- First authorization uses ADR 0008's improvement thresholds; subsequent releases compare candidate VM against the previous release's stored VM baseline as non-regression.
- Any cell timed under the race detector is a job bug: race runs are separate and untimed.
- The gate binary is built once and run directly: `go run` collapses exit codes to 1, which would hide the needs-rerun signal (exit 2).
- Fixed run parameters: `GOMAXPROCS=1`, `BENCHTIME=1s`, `BENCH_COUNT=10`. All committed cells are single-threaded, so `GOMAXPROCS=1` minimizes hosted-runner scheduler jitter; revisit when concurrent cells land.
- Cross-release VM baseline storage: the passing gate uploads its `bench-vm.txt` as an asset on the GitHub release it authorized; the next release's gate downloads the previous release's asset and runs perfgate in non-regression mode against it. Durable, tied to release identity, no repo churn; the release job needs `contents: write`. GH Actions cache was rejected (LRU-evicted); a committed baseline file was rejected (bot push + branch-protection exception).

## Open inputs (blocking an authoritative verdict)

Scale-envelope cells (baseline/knee/boundary per data dimension) and concurrent cells. The corpus (12 cells), goldens, and tier assignments are committed; the harness is dry-run verified end-to-end, including the inconclusive-rerun-resolve path.

Trigger: `workflow_dispatch` only until the first authorization attempt — in first-authorization mode the engine-sensitive cells are red until the VM demonstrates its win, so arming `release: published` earlier would block every release. The PR that attempts first authorization wires the release trigger.
