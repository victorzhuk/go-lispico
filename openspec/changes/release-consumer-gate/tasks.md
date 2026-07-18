## 1. Gold set

- [x] 1.1 Gate runs a committed gold set (tests + benchmark cells) instead of checking out the consumer repo; no revision pin, no cross-repo secret. YAGEL exports and refreshes the corpus deliberately.
- [x] 1.2 Gold-set harness in `internal/goldset`: fixture loader (`.lisp` + `.golden` pairs), engine constructor for both execution modes (Clojure dialect + stdlib), example fixtures pending the YAGEL-exported corpus.

## 2. Correctness leg

- [x] 2.1 Release job runs every gold-set fixture under both execution modes against its golden, plus the race-enabled suite, untimed; any failure fails the release.

## 3. Paired release run

- [x] 3.1 Release job runs the gold-set benchmark cells interleaved per sample (Evaluator, then VM, ten times) with fixed `GOMAXPROCS` and benchtime in one hosted job; identical cell names across the two output files so benchstat pairs them.
- [x] 3.2 Benchstat comparison per cell against its committed Hot-cell tier, applying ADR 0008 thresholds and the burden-of-proof inconclusive rule (one rerun at doubled benchtime).
- [ ] 3.3 Store the passing VM baseline as the non-regression reference for the next release; post-authorization runs compare against it instead of the improvement thresholds. -- OPEN: design.md names no storage backend for the cross-release baseline; GH Actions cache is unsuitable (LRU-evicted). Needs a design decision before scaffolding.
- [x] 3.4 Publish the benchstat report as release evidence.

## 4. Guardrails

- [x] 4.1 Ordinary PR CI carries no percentage gates -- correctness and race checks only; assert the perf job is release-triggered.
- [x] 4.2 Verify no timed cell runs under the race detector in the job definition.

## 5. Verify

- [x] 5.1 Dry-run the full job with the current gold set: correctness leg green under both modes, paired 10-sample interleaved run produced a benchstat-backed per-cell verdict, and the inconclusive path exercised end-to-end (first attempt exit 2, doubled-benchtime rerun, burden-of-proof resolve failing the improvement cells -- the correct verdict for example cells with no VM win). Authoritative verdicts remain pending the YAGEL-exported gold set and its committed tier assignments.
