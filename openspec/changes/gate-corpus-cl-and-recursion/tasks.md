# Tasks — gate-corpus-cl-and-recursion

## 0. Decide widen vs. delete

- [ ] 0.1 Decide (a) widen the gold-set corpus, or (b) delete
      `BenchmarkEngine_FibonacciCL` and its design-doc claim. Record the
      reasoning here. The rest of this file assumes (a); if (b) is chosen,
      skip to task 3.

## 1. Baseline profile (blocking — a new cell needs one before its tier is committed)

- [ ] 1.1 Using `release-gate-activation`'s armed workflow, profile a
      candidate CL-dialect fixture and a candidate recursion fixture on the
      hosted runner's fixed parameters (`GOMAXPROCS=2`, `BENCHTIME=200ms`).
      Record the profile that justifies each fixture's tier assignment, per
      `internal/perfgate/tiers.json`'s own rule ("Reclassification requires a
      committed baseline profile justifying it" — the same rule applies to a
      new cell's first classification).

## 2. Add the fixtures

- [ ] 2.1 Add a CL-dialect fixture (or CL variant of an existing one) to
      `internal/goldset/testdata/`, hand-derived golden per the corpus's own
      rule (never captured from either engine).
- [ ] 2.2 Add a fib-shaped recursion fixture. Confirm it exercises
      `OpFusedNativeOp` and both `GET_GLOBAL`/`GET_FUNC` global-read paths.
- [ ] 2.3 Wire both into `internal/goldset/goldset.go`'s engine construction
      (a CL-dialect variant alongside the existing Clojure one) and add tier
      entries to `internal/perfgate/tiers.json`, citing 1.1's profile.
- [ ] 2.4 Record the func-cell site-cache gap's measured magnitude on the new
      CL cell (`buildSites` skipping `Func:true`, `core/vm/chunk.go:203`) —
      this change does not fix it; note it as a candidate follow-up if the
      number is material.
- [ ] 2.5 Remove `BenchmarkEngine_FibonacciCL` (`runtime/bench_test.go:388`)
      and its "covers the CL path" language from
      `archive/2026-07-28-compiler-branch-arith-fusion/design.md`'s
      cross-reference, now that the gated equivalent exists — annotate,
      don't rewrite, the archived doc's original claim.

## 3. If deleted instead (path (b))

- [ ] 3.1 Remove `BenchmarkEngine_FibonacciCL`; add an explicit note to the
      `bytecode-vm` or `consumer-release-gate` spec stating the gate is
      Clojure-dialect-only by decision, with the CL/Lisp-2 path covered by
      dialect test suites (per ADR 0013's existing consequence 4) rather than
      the performance gate.

## 4. Verify

- [ ] 4.1 `openspec validate --strict` on this change.
- [ ] 4.2 `GOLDSET_MODE=eval` and `GOLDSET_MODE=vm` gold-set correctness green
      with the new fixtures (if widened).
- [ ] 4.3 `cmd/perfgate` runs clean against the new tier entries (no
      "no committed tier" failure) on the next hosted run.
