# Tasks — gate-corpus-cl-and-recursion

## 0. Decide widen vs. delete

- [x] 0.1 Decide (a) widen the gold-set corpus, or (b) delete
      `BenchmarkEngine_FibonacciCL` and its design-doc claim. Record the
      reasoning here. The rest of this file assumes (a); if (b) is chosen,
      skip to task 3.

      **Decided: (b), state the scope boundary instead of widening.** Three
      findings drove it, two of which were not visible from this change's own
      text.

      1. **Widening is blocked by a loop, not by a queue.** Task 1.1 makes a
         hosted profile the precondition for committing any new cell's tier.
         `release-gate-activation` ran the gate (run `30561584997`) and its
         verdict failed on tier misclassification, so `release.yml`'s store
         step was skipped and `v0.11.0` carries no asset. The gate needs
         correct tiers to pass, passing is what stores a baseline asset, and a
         stored baseline is what ADR 0008 accepts as the committed profile
         that licenses a tier. The run's numbers survive only in the expiring
         `consumer-gate-evidence` workflow artifact, which
         `release-gate-activation` task 1.2 explicitly forbids hand-promoting.
         Nothing inside either change breaks that loop; breaking it needs a
         decision that a `workflow_dispatch` artifact is the profile of
         record, or an ADR 0008 amendment. Neither is in scope here.
      2. **A locally-assigned tier is exactly what the first run refuted.**
         Eight of twenty-six committed cells came back misclassified, five of
         them close to inverted — cells assigned `data-dominated` a priori
         turned out to be the corpus's most engine-sensitive. Adding two more
         cells with tiers derived from a developer box would repeat that
         mistake while the evidence against it is a day old.
      3. **The technical motive is gone.** The Why's central claim — that the
         CL path carries a known, un-gated cost because `buildSites` skips
         `Func:true` — was closed by `archive/2026-07-29-vm-func-site-cache`.
         See the proposal's Amendment. What remains is coverage symmetry,
         which is real but is housekeeping, and housekeeping does not justify
         widening the gate's pass/fail surface on an unbacked tier.

      Path (b) is not a smaller version of (a). It closes the actual defect
      the spec delta names — a benchmark cited as covering a gap it cannot
      cover — and converts an implied scope into a stated one.

## 1. Baseline profile (blocking — a new cell needs one before its tier is committed)

- [ ] 1.1 Using `release-gate-activation`'s armed workflow, profile a
      candidate CL-dialect fixture and a candidate recursion fixture on the
      hosted runner's fixed parameters (`GOMAXPROCS=2`, `BENCHTIME=200ms`).
      Record the profile that justifies each fixture's tier assignment, per
      `internal/perfgate/tiers.json`'s own rule ("Reclassification requires a
      committed baseline profile justifying it" — the same rule applies to a
      new cell's first classification).

      **N/A — path (b) chosen in 0.1.** Void by decision, not unrun. Its
      precondition is the loop described in 0.1.

## 2. Add the fixtures

- [ ] 2.1 Add a CL-dialect fixture (or CL variant of an existing one) to
      `internal/goldset/testdata/`, hand-derived golden per the corpus's own
      rule (never captured from either engine).

      **N/A — path (b) chosen in 0.1.**
- [ ] 2.2 Add a fib-shaped recursion fixture. Confirm it exercises
      `OpFusedNativeOp` and both `GET_GLOBAL`/`GET_FUNC` global-read paths.

      **N/A — path (b) chosen in 0.1.**
- [ ] 2.3 Wire both into `internal/goldset/goldset.go`'s engine construction
      (a CL-dialect variant alongside the existing Clojure one) and add tier
      entries to `internal/perfgate/tiers.json`, citing 1.1's profile.

      **N/A — path (b) chosen in 0.1.** `internal/perfgate/tiers.json` is
      untouched by this change.
- [ ] 2.4 Record the func-cell site-cache gap's measured magnitude on the new
      CL cell (`buildSites` skipping `Func:true`, `core/vm/chunk.go:203`) —
      this change does not fix it; note it as a candidate follow-up if the
      number is material.

      **N/A — the gap it measures no longer exists.**
      `archive/2026-07-29-vm-func-site-cache` closed it: `buildSites` now
      indexes `OpGetFunc` and `OpFreezeNativeFunc` and keys sites by
      `siteSym{constIdx, isFunc}` so a Lisp-2 symbol's value and function
      cells never share one. Its acceptance gate was
      `BenchmarkEngine_FibonacciCL` at −30.43% (p=0.000).
- [ ] 2.5 Remove `BenchmarkEngine_FibonacciCL` (`runtime/bench_test.go:388`)
      and its "covers the CL path" language from
      `archive/2026-07-28-compiler-branch-arith-fusion/design.md`'s
      cross-reference, now that the gated equivalent exists — annotate,
      don't rewrite, the archived doc's original claim.

      **N/A as written** — its premise ("now that the gated equivalent
      exists") is false under (b); no gated equivalent was built. The archived
      design doc's claim still needed correcting either way, so that half was
      carried into 3.1.

## 3. If deleted instead (path (b))

- [x] 3.1 Remove `BenchmarkEngine_FibonacciCL`; add an explicit note to the
      `bytecode-vm` or `consumer-release-gate` spec stating the gate is
      Clojure-dialect-only by decision, with the CL/Lisp-2 path covered by
      dialect test suites (per ADR 0013's existing consequence 4) rather than
      the performance gate.

      **Done, with one deliberate deviation: the benchmark is kept, not
      removed.** Everything else landed as written — the `consumer-release-gate`
      delta now states the scope boundary, and ADR 0013's consequence carries
      the exact wording this task cites ("the gold set runs the Clojure
      dialect, so dialect-specific default behavior (Lisp-2 function cells, CL
      list bindings) is covered by the dialect test suites rather than the
      gate").

      Two reasons for keeping it. **The spec forbids citing, not keeping** —
      the delta's rule is that an out-of-workflow benchmark "SHALL NOT be
      cited as closing that gap", so deleting the citation satisfies it in
      full and deleting the benchmark is surplus. **And it acquired a second
      purpose after this task was written**: it is the recorded hard
      acceptance gate for `archive/2026-07-29-vm-func-site-cache`
      (FibonacciCL 282.5µs → 196.5µs, −30.43%, p=0.000) and a recorded row in
      `archive/2026-07-29-engine-lean-call-boundary`. Deleting it would make a
      landed win on the Lisp-2 path permanently unmeasurable — destroying live
      regression evidence to satisfy wording written before that evidence
      existed. It is the repo's only timed Lisp-2 cell.

      What changed instead: its doc comment now states that it is not a gate
      cell, must not be cited as one, and says what it is for. The archived
      `compiler-branch-arith-fusion` design doc's "task 1.2 adds that
      coverage" claim is annotated in place, not rewritten.

## 4. Verify

- [x] 4.1 `openspec validate --strict` on this change.
- [x] 4.2 `GOLDSET_MODE=eval` and `GOLDSET_MODE=vm` gold-set correctness green
      with the new fixtures (if widened).

      No new fixtures — the corpus is unchanged at thirteen. Run anyway as a
      no-drift check: `go test ./internal/goldset/ -count=1 -run TestGoldset`
      is green, 26 subtests (13 fixtures × 2 modes).

      Note for whoever writes the next such task: `GOLDSET_MODE` does not
      gate correctness. `TestGoldset` loops `Modes` itself and runs both in
      one process; the env var selects the mode for `BenchmarkGoldset` only.
- [x] 4.3 `cmd/perfgate` runs clean against the new tier entries (no
      "no committed tier" failure) on the next hosted run.

      **Satisfied vacuously and deliberately.** No tier entries were added,
      `internal/perfgate/tiers.json` is byte-unchanged, and no fixture was
      added to `internal/goldset/testdata/` — so this change cannot introduce
      a "no committed tier" failure on the next hosted run. That is the whole
      point of (b): the gate's pass/fail surface is untouched.

---

## Status at apply time

Complete: 0.1, 3.1, 4.1, 4.2, 4.3 — the decision, the (b) branch, and
verification. Void by decision: 1.1, 2.1, 2.2, 2.3, 2.4, 2.5 — the (a)
branch, which this file's own 0.1 authorizes skipping. Their boxes are left
unchecked on purpose: a checked box on a task nobody ran is the artifact this
program exists to prevent, even when the skip is authorized. 5/11 is the
honest count.

**Carried forward, owned by nobody.** `release-gate-activation` task 2.3
assigns a `Call`-boundary gate cell to this change. This change does not add
one — its own fixtures were CL-dialect and recursion, never Call — and under
(b) it adds no cell at all. The `Engine.Call ≤110ns` bar therefore stays
unsettleable, and the standing prohibition holds: no harness-facing document
quotes a `Call` figure as a settled bar. This is now stated in the
`consumer-release-gate` delta rather than left implied, but stating it is not
the same as owning it. A future change must either add the cell or restate
the bar against something the repo measures.

Also unowned: reclassifying the eight misclassified cells the first hosted
run exposed. `release-gate-activation`'s Impact assigns `tiers.json` to this
change, but reclassification needs the same committed baseline profile that
0.1's loop makes unobtainable — so it cannot be done here, and this change
leaves `tiers.json` untouched rather than pretending otherwise.
