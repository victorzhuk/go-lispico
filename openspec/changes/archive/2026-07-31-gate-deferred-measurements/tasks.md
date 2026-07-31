# Tasks — gate-deferred-measurements

## 0. Decide the attribution mechanism

- [x] 0.1 Choose, and record the reasoning: build a two-ref paired benchmark
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

      **Decided 2026-07-31: declined.** The attribution of 527f03c and
      631b2ee is recorded as declined, and the two-ref capability is not
      built. Four things carried the decision:

      1. No open change carries a claim that needs a `<ref>^`-vs-`<ref>` run.
         `vm-allocation-parity`'s only open task (3.2) amends an ADR 0008
         threshold against the committed profile, and `release-gate-activation`
         tasks 2.1 and 2.2 are these two commits themselves. The instrument's
         only consumers would be two merged commits, neither a rollback
         candidate.
      2. `527f03c`'s own local evidence predicts a null result. A null result
         is a verdict under this change's spec delta, but it changes nothing
         that ships.
      3. The cost is a second maintained measurement path, not a workflow
         file. Two refs are two source trees, so `-count` cannot interleave
         them — Go interleaves samples within one binary, never across trees —
         and an honest paired run means building both test binaries with
         `go test -c` and alternating them, machinery whose own correctness
         has to be maintained against the gate's fixed parameters.
      4. The spec delta lands either way. Its ADDED requirement governs *how*
         attribution must be done and explicitly licenses recording a claim as
         declined; declining here does not weaken the rule this change ships.

      What follows from the decline: neither commit's performance claim is
      confirmed or refuted, and no run — including the passing gate run
      30630796967 — may be cited as settling either, since both commits are
      ancestors of every tree those runs measured.

## 1. Attribution runs (only if 0.1 chose to build)

Not applicable — 0.1 declined. Each task below is closed by that decision,
marked the way `release-gate-activation` marked 2.1-2.3 when their obligation
moved rather than completed.

- [x] 1.1 Add the two-ref capability. Do not widen `.github/workflows/
      release.yml` — its run is the release verdict and should not grow modes;
      a separate `workflow_dispatch` workflow taking two refs keeps the gate's
      surface fixed. It SHALL use the gate's committed fixed parameters
      (`GOMAXPROCS=2`, `BENCHTIME=200ms`, `BENCH_COUNT=10`), or state why not.

      Not built, per 0.1. `release.yml` is unchanged.
- [x] 1.2 Run `527f03c^` vs `527f03c` (native-op fusion) and record the
      verdict, naming both refs. The local evidence predicts a near-miss;
      record the number either way. Reference
      `archive/2026-07-28-compiler-branch-arith-fusion` by path; do not edit it.

      Not run. `527f03c` ("perf(vm): fuse native-op canonicality freeze with
      local/const operands") carries its claim as declined; the local
      near-miss recorded in
      `openspec/changes/archive/2026-07-28-compiler-branch-arith-fusion`
      remains the only evidence, and it is not a verdict.
- [x] 1.3 Run `631b2ee^` vs `631b2ee` (ledger batching) and record the verdict,
      naming both refs. Reference
      `archive/2026-07-27-vm-batched-ledger-charging` by path; do not edit it —
      an archive is the historical record that it shipped unmeasured.

      Not run. `631b2ee` ("perf(vm): batch opcode-issued allocation ledger
      charges") carries its claim as declined;
      `openspec/changes/archive/2026-07-27-vm-batched-ledger-charging` stands
      as the record that it shipped with its own tasks 1.1, 3.3, and 3.4
      unchecked.
- [x] 1.4 Record both results against `release-gate-activation` tasks 2.1 and
      2.2 by name, so the obligation those tasks carried is visibly discharged
      here rather than silently dropped.

      `release-gate-activation` task 2.1 (527f03c) and task 2.2 (631b2ee) are
      both discharged as declined by 0.1 above. Neither is reopened, and
      neither obligation moves anywhere else: the decline is the terminal
      state, recorded here where those tasks point.

## 2. Call-boundary gate cell

- [x] 2.1 Design the fixture. The cell must exercise the `Engine.Call`
      boundary the way `BenchmarkEngine_Call*` (`runtime/bench_test.go:307-366`)
      does, but live where the gate runs — `internal/goldset/`, which
      `release.yml` benches. Decide what the fixture calls and how many times,
      so the cell measures boundary cost rather than callee work, and give it a
      hand-derived golden like every other fixture.

      Landed as `GoldsetCall/call-boundary` (`internal/goldset/bench_test.go`,
      `BenchmarkGoldsetCall`). Three design calls, each forced by how the gate
      reads this package:

      - **The callee is GoFunc-free.** `(defn call-boundary [a b] a)` compiles
        to a local read and a return, so the timed cost is argument
        marshalling, function-cell lookup, and frame setup — the boundary —
        rather than the callee's work. The setup `Eval` runs once, untimed.
      - **Exactly one `Call` per iteration.** Amortizing several calls per
        iteration would lower variance but make ns/op a per-batch figure, and
        2.4 has to adjudicate an absolute per-call target against it.
      - **The source lives in `testdata/call/`, not `testdata/`.**
        `Fixtures()` globs `testdata/*.lisp`, and both `BenchmarkGoldset` and
        `BenchmarkGoldsetParse` iterate it, so a `.lisp` file there would have
        silently added two further cells that measure `Eval` and parsing
        instead of the boundary. One cell was the intent; one cell is what
        landed.

      The golden is hand-derived from the language contract, like every other
      fixture: the body returns its first parameter, the call passes `7, 11`,
      so the golden is `7` — and distinct arguments make a wrong-slot read
      visible rather than passing on a symmetric pair. `TestGoldsetCall`
      asserts it through `Call` under both execution modes, since an
      unasserted fixture must not silently pass the gate.

      The five `BenchmarkEngine_Call*` benchmarks in `runtime/bench_test.go`
      now carry the disclaimer this spec already requires of a gap-covering
      benchmark outside the gate's package: they are not gate cells and must
      not be cited as ones.
- [x] 2.2 Produce the hosted classification profile for the new cell and commit
      it, per `gate-tier-reclassification`'s mechanism. A tier from a developer
      box does not license the cell — the boundary's own dev-box spread
      (137.0-137.4 ns against 119.7-122.8 ns at one HEAD on one day) is wider
      than the margin any tier would assert.

      `internal/perfgate/testdata/profile-30637802780`, from `workflow_dispatch`
      run 30637802780 at head `0a36275` on the branch carrying the cell,
      `GOMAXPROCS=2`, `BENCHTIME=200ms`, `BENCH_COUNT=10`, `ubuntu-24.04` /
      AMD EPYC 7763. The run failed at "Enforce performance gate verdict" on
      one line — `GoldsetCall/call-boundary-2: FAIL no committed tier for this
      cell` — which is the only outcome a first measurement of a new cell can
      produce: `cmd/perfgate` fails any untiered cell, and `TestPinnedProfile`
      requires `benchstat.csv`, `tiers.json`, and `verdict.txt` to agree on the
      cell set, so the tier cannot precede the profile that licenses it. Every
      other leg passed — gold set under both modes, race suite, paired
      benchmark — and no existing cell changed tier or verdict.

      The profile also re-licenses the twenty-six cells already committed, and
      surfaced a provenance defect in the profile it supersedes: run
      30630796967 exited inconclusive, so its rerun step deleted both bench
      files and regenerated them at doubled benchtime, making the files
      committed under `profile-30630796967/` a 400ms measurement under a README
      that records 200ms (57092 against 28188 iterations on
      `Goldset/counter-closure-2`, same runner CPU, within 2% on ns/op). No
      tier it licensed is invalidated — a percentage delta at 400ms is as sound
      as one at 200ms, and every tier holds on the new 200ms figures — but the
      stated parameters were not the parameters that produced the files. A
      correction note now heads that profile's README; its raw files are left
      untouched, since correcting them would destroy the evidence rather than
      the error.
- [x] 2.3 Commit the cell's tier in `internal/perfgate/tiers.json` against that
      profile.

      `"GoldsetCall/call-boundary": "engine-sensitive"`, by measurement rather
      than assumption: latency -80.66% and bytes -100.00% (768 B/op under the
      Evaluator against 0 under the VM), both p=0.000 at n=10, clearing ADR
      0008's -15%/-20% floors by the widest margin in the corpus, with
      allocation count non-increasing. Regenerated against the committed tier,
      the cell reads PASS on merit. `perfgate_test.go`'s `pinnedProfileRunID`
      and both SHA-256 digest constants move to the new profile, and
      `tiers.json`'s provenance sentence names it.
- [x] 2.4 Adjudicate `engine-lean-call-boundary`'s absolute bar against the
      hosted figure: the `Call ≤110ns` target and the composed ≤95ns target.
      See `archive/2026-07-29-engine-lean-call-boundary/tasks.md` tasks 1.1 and
      5.4 for the pinned local table; do not edit that archive. A hosted miss
      is a finding about the target, not a regression — the boundary cut's
      relative deltas (−21..−37%) are independently confirmed. If the target is
      wrong, restate it against what the gate measures rather than carrying it
      forward unmet.

      **Both targets are retired as not adjudicable, and restated.** The
      hosted figure is 188.50 ns/op (CI 1%, n=10), which misses ≤110ns by 71%
      and the composed ≤95ns by 98%. That miss is not reported as a finding
      about the boundary, because the two figures are not measurements of the
      same thing. Three independent reasons, any one sufficient:

      1. **Machine.** Both targets were set from dev-box numbers — the pinned
         table in `archive/2026-07-29-engine-lean-call-boundary/tasks.md` task
         1.1, and the ≤95ns target's stated purpose of beating "GopherLua's
         97ns local". The gate runs on a shared hosted vCPU. The new cell
         itself reads 89.57 ns on the dev box against 188.50 ns hosted: a 2.1x
         gap on identical code, which alone spans the entire distance between
         the target and the miss.
      2. **Engine configuration.** The archive's `Call` rows measured an
         engine with no stdlib. The gate cell measures the gold set's engine —
         Clojure dialect plus stdlib — which is the configuration a consumer
         embeds.
      3. **What is inside the timed region.** The archive's benchmarks build
         the variadic argument slice inside the timed loop; the gate cell
         hoists it out. That slice is still measurable today at 32 B/op and
         1 alloc/op on `BenchmarkEngine_CallBytecodePlain` — two 16-byte
         interface headers, and the whole of the difference between that
         benchmark's non-zero allocation figure and this cell's zero.

      An absolute nanosecond bar that names no machine, no engine
      configuration, and no timed region cannot be met or missed; it can only
      be restated. What replaces it is what the gate can enforce on every
      release: `GoldsetCall/call-boundary` at the engine-sensitive tier,
      currently -80.66% latency and -100.00% bytes against the Evaluator at
      p=0.000. The GopherLua comparison the ≤95ns target was aimed at was
      always a same-box exercise against a harness that lives outside this
      repository; no gate cell can settle it, and none is claimed to. The
      boundary cut's relative deltas (−21..−37%) remain independently
      confirmed and are untouched by this.
- [x] 2.5 Lift the prohibition on quoting a `Call` figure — only now, and only
      for the hosted figure. Update any harness-facing document that has been
      silent because of it.

      Lifted in the spec delta, with the limits the measurement actually
      carries: a quoted figure must come from a hosted run of the cell, name
      that run, and state the runner, the engine configuration, and that the
      caller's argument slice is excluded. No document needed correcting —
      none had ever quoted a `Call` figure, which is the prohibition having
      worked. `docs/profiling-baseline.md` gains the pointer to where the
      hosted figure now lives, since that document's own discipline section is
      what warns readers off its dev-box timings.

## 3. Record

- [x] 3.1 CHANGELOG `[Unreleased]`: the new gate cell and the attribution
      verdicts, or the recorded decision to decline attribution.

      Three entries: the new cell under Added, with its hosted figure and the
      qualifiers that travel with it; the declined attribution of both v0.10.0
      commits under Changed; and the superseded profile's benchtime correction,
      which is the sort of thing a reader of the tiers would otherwise trust
      silently.

## 4. Verify

- [x] 4.1 `openspec validate --strict` on this change.

      `openspec validate gate-deferred-measurements --strict --json` — 1 item,
      passed, no issues.
- [x] 4.2 `go build ./... && go test ./internal/goldset/ ./internal/perfgate/... -count=1`.

      Both clean, 71 tests across the two packages, plus `gofmt -l .` empty,
      `go vet ./...` clean, and `golangci-lint run ./...` at 0 issues. The full
      `go test ./...` is green; one run of it failed
      `plugins/json.TestDecodeHashMap_Scaling`, a wall-clock timing test that
      compares decode durations at two input sizes. It passes three times in a
      row in isolation on this branch and on `master`, and a full-suite rerun
      is clean — a load-sensitive test in a package this change does not
      touch.
- [x] 4.3 Confirm the new cell appears in a hosted run's verdict output before
      this change is archived. A cell committed but never seen in a hosted
      verdict is the same defect this change's own Why indicts.

      Two hosted runs, both on this change's branch. Run 30637802780 is the
      classification profile: it measured the cell untiered, so the cell
      appears there as `FAIL no committed tier for this cell` — seen, but not
      yet judged. Run 30639778105 then ran the tree with the tier committed
      and reported the cell judged against it:

      ```
      GoldsetCall/call-boundary-2: PASS
      ```

      at -83.32% latency and -100.00% on both bytes and allocations on that
      run's own data, against -80.66%/-100.00%/-100.00% on the profile. The
      cell is measured, tiered, and green in a hosted verdict.

      **Run 30639778105 failed as a whole, on a different cell.**
      `Goldset/guard-nil-2: FAIL (bytes increased by 0.09%)`. That is the
      finding both this profile's README and `profile-30630796967`'s record,
      firing for the first time: guard-nil's latency delta read +2.46% at
      p=0.000 — significant, where three earlier runs read `~` — so
      `evaluateWithinTolerance` cleared the significance gate and the +-5%
      tolerance, then reached `nonIncreasing` and failed on the same
      1128-against-1129 B/op every profile has recorded.

      It is not attributable to this change. Runs 30637802780 and 30639778105
      measure trees that differ only in testdata, documentation, `tiers.json`,
      and `perfgate_test.go` — no engine code — and guard-nil's byte figure is
      identical across both. The cell's green was never a property of the
      code; it was a property of whether a given run's latency delta cleared
      significance, and it fails whenever it does. `vm-allocation-parity` task
      3.2 owns the threshold question and already records why the byte is not
      a removable allocation site. What this run settles is that the question
      is load-bearing for every release rather than a documentation nicety.
