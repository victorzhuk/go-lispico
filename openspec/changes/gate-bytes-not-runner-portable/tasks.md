## 1. Pin the defect

- [ ] 1.1 Add a perfgate test that a cross-runner pair whose allocation counts are identical reports its bytes verdict as inconclusive naming both runners, rather than failing, seeded from the two committed cross-runner corpora.
- [ ] 1.2 Add a perfgate test that a cross-runner pair whose allocation count increases still fails, so narrowing the bytes axis does not narrow the allocs axis with it.
- [ ] 1.3 Add a perfgate test that a same-runner pair still fails on allocated bytes past its stated allowance, unchanged.
- [ ] 1.4 Verify the existing gate tests still pass unchanged, so the narrowing is additive.

## 2. Narrow the bytes axis to matching runners

- [ ] 2.1 Report the bytes verdict as inconclusive when the runner identities differ, naming both, and keep the allocation-count verdict decided on that path. Verify the reported reason distinguishes an undecided bytes axis from a passing one.
- [ ] 2.2 Verify the per-cell bytes allowances in `internal/perfgate/tiers.json` are unchanged in value and still enforced whenever the identities match.
- [ ] 2.3 Verify a cross-runner run whose cells are otherwise clean now exits 0, so the release it authorizes stores a baseline.

## 3. Record the decision

- [ ] 3.1 Amend ADR 0008 with which measurement axis survives a change of runner and why: allocation counts are exact per-operation integers, while `B/op` divides by an iteration count that is a property of machine speed.
- [ ] 3.2 Record under `[Unreleased]` in `CHANGELOG.md` that a release measured against a baseline from a different runner is gated on allocation counts rather than allocated bytes. Verify the entry names the observable change, not the implementation.

## 4. Verify

- [ ] 4.1 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.
- [ ] 4.2 Dispatch the gate against the release candidate tree and verify it reports non-regression, states the runner comparison, reports the bytes axis inconclusive on the cross-runner pair, and reaches exit 0 so a baseline would be stored.
