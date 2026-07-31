# release-gate-activation

## Why

ADR 0008's release consumer gate has never executed as a hosted CI job.
`gh run list --workflow=release.yml` returns no runs for this repository; no
release from v0.6.0 through v0.10.0 carries a `bench-vm.txt` asset; and the
workflow's own trigger is `workflow_dispatch` only, with `release: published`
commented out (`.github/workflows/release.yml:16-27`). Every verdict this
project has recorded as "deferred to the release runner" — most recently
527f03c's null wall-clock result for native-op fusion — is therefore deferred
to a process that does not run. The gate exists in full: gold-set fixtures,
tier classifications, benchstat thresholds, first-authorization and
non-regression modes. It has simply never been fired.

This is more than an oversight in one workflow file. ADR 0013 records that
`runtime.New()`'s bytecode-VM default was authorized by "the perfgate tiers
[deciding] the performance cells. Passing it was the condition; it passed."
The archived `engine-bytecode-default` change's own verification task (4.3)
cites real benchstat evidence — but gathered ad hoc against a separate bench
repository, not through this workflow, with no stored baseline asset and no
tier-threshold judgment of the kind ADR 0008 describes. The gate that
"passed" and the gate ADR 0008 specifies are not the same artifact. That is
a governance gap independent of any single perf commit: nothing in this
repository can currently produce the authoritative verdict its own release
process claims to require.

Two release-hygiene defects compound this while the gate stays unarmed:
`guard-nil.lisp` (`internal/goldset/testdata/`) was rewritten in 3d253e2
(`(unless true :b)` → `(when (not true) :b)`) without any baseline-invalidation
note, and `release.yml`'s documented invalidation rule
(`.github/workflows/release.yml:38-44`) covers only `GOMAXPROCS`/`BENCHTIME`
changes, not fixture-source edits. Both cells (`Goldset/guard-nil` and
`GoldsetParse/guard-nil`) would compare a future candidate against a baseline
measuring different source the moment a real baseline exists.

## What Changes

- Trigger the release consumer gate workflow against the current `v0.10.0`
  tag (or the next tag cut for this purpose) and store its `bench-vm.txt` as
  a release asset — the repo's first real non-regression baseline.
- Decide the standing trigger: keep `workflow_dispatch`-only with a documented
  manual cadence, or re-enable `release: published` with the post-hoc
  semantics the workflow header already describes (it cannot block a release,
  only record evidence against one already published).
- Record, against that first real run, the two v0.10.0 verdicts that were
  deferred to a runner that until now did not exist: 527f03c's fusion result
  (already recorded as a local near-miss) and 631b2ee's ledger-batching claim,
  whose own change (`archive/2026-07-27-vm-batched-ledger-charging`) left
  tasks 1.1 (baseline), 3.3 (verification floor), and 3.4 (benchstat) unchecked
  before being archived. This change does not edit that archived change's
  `tasks.md` — an archive is the historical record that it shipped unmeasured
  — it carries the obligation forward as a task here instead.
- Add a fixture-source-edit rule to `release.yml`'s baseline-invalidation
  documentation, alongside the existing `GOMAXPROCS`/`BENCHTIME` rule, and
  correct `Goldset/guard-nil` and `GoldsetParse/guard-nil`'s stored expectation
  once a baseline exists to reflect 3d253e2's rewritten fixture.
- Correct ADR 0013's account: it should record what actually authorized the
  bytecode default (the local goldset correctness run plus the ad hoc bench
  evidence in `engine-bytecode-default/tasks.md`), not imply the hosted
  ADR-0008 gate produced that verdict.

## Impact

- Affected specs: `consumer-release-gate` (new requirement: the gate SHALL
  actually run and its result SHALL be stored, not merely be runnable).
- Affected code/docs: `.github/workflows/release.yml` (trigger decision,
  invalidation-rule documentation), `internal/perfgate/tiers.json` (no
  change expected — corpus reclassification is `gate-corpus-cl-and-recursion`'s
  scope), ADR 0013 (correction note), CHANGELOG `[Unreleased]`.
- Expected: a stored `bench-vm.txt` release asset exists for the first time;
  every future release runs as a non-regression comparison against a baseline
  that is real, not assumed.
- Risk: low — this is process activation, not code behavior. The only
  functional risk is discovering, on the first real run, that a currently
  "passing" cell actually regresses once measured for real; that is the
  gate doing its job, not a defect in this change.
- Sequencing: `reader-allocation-floor` and `reader-state-reuse` (the
  reader-allocation axis) will drop every `Goldset/*` cell's `B/op` by a
  large, evaluator-unrelated amount once they land. This change's own tasks
  include re-running and re-storing the baseline immediately after those two
  land, so the stored baseline stays informative for evaluator-focused
  non-regression judgments.
