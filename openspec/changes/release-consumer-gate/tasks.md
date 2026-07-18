## 1. Consumer ref and dependency plumbing

- [x] 1.1 Track YAGEL's `gold` ref as the blessed-release pointer instead of recording a pinned revision in this repo; YAGEL owns when the pointer moves. Done on the go-lispico side (release.yml checks out `YAGEL_REF: gold`); the ref does not exist in yagel-labs/yagel yet and must be created there before the job can run.
- [x] 1.2 Script the checkout + `go.mod` replace step and verify YAGEL builds against a candidate working tree.

## 2. Correctness leg

- [x] 2.1 Release job runs YAGEL's Behavior-golden suites under both execution modes plus both repositories' race-enabled suites, untimed; any failure fails the release.

## 3. Paired release run

- [x] 3.1 Release job runs the interleaved Evaluator/VM benchmark cells with fixed `GOMAXPROCS`, fixed benchtime, and at least ten samples per cell, in one hosted job.
- [x] 3.2 Benchstat comparison per cell against its committed Hot-cell tier, applying ADR 0008 thresholds and the burden-of-proof inconclusive rule (one rerun at doubled benchtime).
- [ ] 3.3 Store the passing VM baseline as the non-regression reference for the next release; post-authorization runs compare against it instead of the improvement thresholds. -- OPEN: design.md names no storage backend for the cross-release baseline; GH Actions cache is unsuitable (LRU-evicted). Needs a design decision before scaffolding.
- [x] 3.4 Publish the benchstat report as release evidence.

## 4. Guardrails

- [x] 4.1 Ordinary PR CI carries no percentage gates -- correctness and race checks only; assert the perf job is release-triggered.
- [x] 4.2 Verify no timed cell runs under the race detector in the job definition.

## 5. Verify (blocked on YAGEL harness)

- [ ] 5.1 Dry-run the full job against YAGEL's `gold` ref with the current go-lispico release; correctness leg green, paired run produces a benchstat report. -- BLOCKED: YAGEL has not published its Behavior-golden suites, benchmark cells, or Scale envelopes yet (verified zero hits for expected harness terms in the YAGEL repo), and the `gold` ref does not exist there yet. Cannot be attempted until YAGEL publishes.
