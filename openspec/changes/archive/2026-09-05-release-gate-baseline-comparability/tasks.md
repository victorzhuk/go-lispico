## 1. Pin the defects

- [x] 1.1 Add a perfgate test that a baseline and candidate recording different runner identities produce inconclusive latency cells naming both runners, while allocation and bytes verdicts are still decided.
- [x] 1.2 Add a perfgate test that a cell with no stated bytes allowance fails as a missing-config error rather than being judged against zero.
- [x] 1.3 Add a perfgate test that a baseline-resolution failure is distinguishable from a repository holding no baseline, and that only the latter selects first-authorization thresholds.
- [x] 1.4 Verify the existing gate tests still pass unchanged, so the new rules are additive.

## 2. Carry the runner identity

- [x] 2.1 Record the runner identity alongside the stored VM baseline, and read it back when the baseline is used. Verify a baseline stored before this change is handled without crashing and reports its identity as unknown.
- [x] 2.2 Make a latency comparison against a baseline from a different runner inconclusive, naming both identities. Verify allocation counts and bytes are still enforced on that path.
- [x] 2.3 Verify the gate never removes the configuration lines that make two runs incomparable, and fails with the tool's own reason when pairing is declined.

## 3. State the bytes allowances

- [x] 3.1 Give every cell in `internal/perfgate/tiers.json` a bytes allowance justified by observed sampling spread on that cell; record the spread each number came from.
- [x] 3.2 Make a missing allowance a configuration error. Verify allocation counts keep a zero allowance.

## 4. Make the pre-flight predictive

- [x] 4.1 Resolve the gate mode from the stored baselines rather than the triggering event, and verify a dispatched run and a release run reach the same per-cell verdicts on the same tree.
- [x] 4.2 Verify a dispatched run still publishes no baseline and uploads no release asset.
- [x] 4.3 Fail the gate when baseline enumeration or download errors, and verify first-authorization is selected only when no baseline exists.

## 5. Record the decision

- [x] 5.1 Amend ADR 0008 with the runner-comparability rule its non-regression check assumed but never stated, and with the distinction between exact allocation counts and averaged bytes.
- [x] 5.2 Record under `[Unreleased]` → `Changed` in `CHANGELOG.md` that the gate now reports latency as inconclusive across differing runners instead of comparing them. Verify the entry names the observable change, not the implementation.

## 6. Verify

- [x] 6.1 Run the repository test suite, the race suite over `core`, `plugins` and `runtime`, `go vet`, and the linter; verify every command exits successfully.
- [ ] 6.2 Dispatch the gate against the release candidate tree and verify it reports non-regression mode, states the runner comparison, and reaches a verdict that is a defensible prediction of the release run.
