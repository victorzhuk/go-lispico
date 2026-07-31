# Tasks — gate-deferred-measurements

## 0. Decide the attribution mechanism

- [ ] 0.1 Choose, and record the reasoning: build a two-ref paired benchmark
      capability, or record 527f03c's and 631b2ee's attribution as declined.

      The cost is real and its only consumer is two already-merged commits: a
      new workflow, two hosted runs per attribution, and a maintained second
      benchmark path. The case for building it is that two changes shipped
      claims nothing ever checked, and `consumer-release-gate` now requires a
      deferred verdict to name a real run — a requirement this repository
      cannot satisfy for these two without the mechanism. The case for
      declining is that both commits are long merged, neither is a rollback
      candidate, and a null result changes nothing that ships.

      Declining is a valid outcome only if it is written down as a decision
      with reasoning, the way `release-gate-activation` task 2.4 dropped the
      cross-engine bar. Silence is what this change exists to end.

## 1. Attribution runs (only if 0.1 chose to build)

- [ ] 1.1 Add the two-ref capability. Do not widen `.github/workflows/
      release.yml` — its run is the release verdict and should not grow modes;
      a separate `workflow_dispatch` workflow taking two refs keeps the gate's
      surface fixed. It SHALL use the gate's committed fixed parameters
      (`GOMAXPROCS=2`, `BENCHTIME=200ms`, `BENCH_COUNT=10`), or state why not.
- [ ] 1.2 Run `527f03c^` vs `527f03c` (native-op fusion) and record the
      verdict, naming both refs. The local evidence predicts a near-miss;
      record the number either way. Reference
      `archive/2026-07-28-compiler-branch-arith-fusion` by path; do not edit it.
- [ ] 1.3 Run `631b2ee^` vs `631b2ee` (ledger batching) and record the verdict,
      naming both refs. Reference
      `archive/2026-07-27-vm-batched-ledger-charging` by path; do not edit it —
      an archive is the historical record that it shipped unmeasured.
- [ ] 1.4 Record both results against `release-gate-activation` tasks 2.1 and
      2.2 by name, so the obligation those tasks carried is visibly discharged
      here rather than silently dropped.

## 2. Call-boundary gate cell

- [ ] 2.1 Design the fixture. The cell must exercise the `Engine.Call`
      boundary the way `BenchmarkEngine_Call*` (`runtime/bench_test.go:307-366`)
      does, but live where the gate runs — `internal/goldset/`, which
      `release.yml` benches. Decide what the fixture calls and how many times,
      so the cell measures boundary cost rather than callee work, and give it a
      hand-derived golden like every other fixture.
- [ ] 2.2 Produce the hosted classification profile for the new cell and commit
      it, per `gate-tier-reclassification`'s mechanism. A tier from a developer
      box does not license the cell — the boundary's own dev-box spread
      (137.0-137.4 ns against 119.7-122.8 ns at one HEAD on one day) is wider
      than the margin any tier would assert.
- [ ] 2.3 Commit the cell's tier in `internal/perfgate/tiers.json` against that
      profile.
- [ ] 2.4 Adjudicate `engine-lean-call-boundary`'s absolute bar against the
      hosted figure: the `Call ≤110ns` target and the composed ≤95ns target.
      See `archive/2026-07-29-engine-lean-call-boundary/tasks.md` tasks 1.1 and
      5.4 for the pinned local table; do not edit that archive. A hosted miss
      is a finding about the target, not a regression — the boundary cut's
      relative deltas (−21..−37%) are independently confirmed. If the target is
      wrong, restate it against what the gate measures rather than carrying it
      forward unmet.
- [ ] 2.5 Lift the prohibition on quoting a `Call` figure — only now, and only
      for the hosted figure. Update any harness-facing document that has been
      silent because of it.

## 3. Record

- [ ] 3.1 CHANGELOG `[Unreleased]`: the new gate cell and the attribution
      verdicts, or the recorded decision to decline attribution.

## 4. Verify

- [ ] 4.1 `openspec validate --strict` on this change.
- [ ] 4.2 `go build ./... && go test ./internal/goldset/ ./internal/perfgate/... -count=1`.
- [ ] 4.3 Confirm the new cell appears in a hosted run's verdict output before
      this change is archived. A cell committed but never seen in a hosted
      verdict is the same defect this change's own Why indicts.
