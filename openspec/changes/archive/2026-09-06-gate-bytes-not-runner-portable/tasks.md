## 1. Pin the defect

- [x] 1.1 Author the cross-runner gate tests in one pass, before the narrowing
  lands: rewrite `TestCrossRunner_LatencyInconclusiveBytesEnforced` as
  `TestCrossRunner_AllocsEnforcedBytesInconclusive`, asserting the bytes verdict
  inconclusive naming both runners on the cells whose allocation counts are
  identical and the surviving allocation-count failure on the cells whose counts
  increase; add `TestCrossRunner_CleanRunExitsZero` for a cross-runner run whose
  cells are otherwise clean; and add `TestSameRunner_BytesStillEnforced` pinning
  that a same-runner pair still fails on allocated bytes past its stated
  allowance.
- [x] 1.2 Verify every other existing gate test passes unchanged.

## 2. Narrow the bytes axis to matching runners

- [x] 2.1 Report a cell’s bytes verdict as inconclusive rather than enforced
  whenever its comparison is marked not comparable, and keep the allocation-count
  verdict decided on that path. Verify the reported reason distinguishes an
  undecided bytes axis from a passing one.
- [x] 2.2 Verify the per-cell bytes allowances in `internal/perfgate/tiers.json`
  are unchanged in value and still enforced whenever the identities match.
- [x] 2.3 Verify a cross-runner run whose cells are otherwise clean now exits 0,
  so the release it authorizes stores a baseline.
- [x] 2.4 Mark a comparison not comparable on the bytes axis when the two runs’
  runner identities differ, naming both runners in the reported reason, and leave
  it enforced when they match.

## 3. Record the decision

- [x] 3.1 Amend ADR 0008 with which measurement axis survives a change of runner and why: allocation counts are exact per-operation integers, while `B/op` divides by an iteration count that is a property of machine speed.
- [x] 3.2 Record under `[Unreleased]` in `CHANGELOG.md` that a release measured against a baseline from a different runner is gated on allocation counts rather than allocated bytes. Verify the entry names the observable change, not the implementation.

## 4. Verify

- [x] 4.1 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.
- [ ] 4.2 Dispatch the gate against the release candidate tree and verify it reports non-regression, states the runner comparison, reports the bytes axis inconclusive on the cross-runner pair, and reaches exit 0 so a baseline would be stored.
