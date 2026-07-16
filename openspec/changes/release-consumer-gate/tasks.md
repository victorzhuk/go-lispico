## 1. Pin and dependency plumbing

- [ ] 1.1 Record the pinned YAGEL revision in this repo with the re-pin-at-release-cut rule documented next to it.
- [ ] 1.2 Script the checkout + `go.mod` replace step and verify YAGEL builds against a candidate working tree.

## 2. Correctness leg

- [ ] 2.1 Release job runs YAGEL's Behavior-golden suites under both execution modes plus both repositories' race-enabled suites, untimed; any failure fails the release.

## 3. Paired release run

- [ ] 3.1 Release job runs the interleaved Evaluator/VM benchmark cells with fixed `GOMAXPROCS`, fixed benchtime, and at least ten samples per cell, in one hosted job.
- [ ] 3.2 Benchstat comparison per cell against its committed Hot-cell tier, applying ADR 0008 thresholds and the burden-of-proof inconclusive rule (one rerun at doubled benchtime).
- [ ] 3.3 Store the passing VM baseline as the non-regression reference for the next release; post-authorization runs compare against it instead of the improvement thresholds.
- [ ] 3.4 Publish the benchstat report as release evidence.

## 4. Guardrails

- [ ] 4.1 Ordinary PR CI carries no percentage gates — correctness and race checks only; assert the perf job is release-triggered.
- [ ] 4.2 Verify no timed cell runs under the race detector in the job definition.

## 5. Verify (blocked on YAGEL harness)

- [ ] 5.1 Dry-run the full job against the pinned YAGEL revision with the current go-lispico release; correctness leg green, paired run produces a benchstat report.
