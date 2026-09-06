## 1. Pin the defect

- [ ] 1.1 Pin the synchronization contract in one pass, before the cadence lands: that a
  sequence of short Builtin calls under an armed deadline reads the wall clock a bounded
  fraction of the times it synchronizes rather than once per call; that an expired
  deadline still terminates Builtin work within the documented number of synchronizations
  with the error shape unchanged; and that caller cancellation and the Reduction charge
  still occur at every synchronization, including mid-cadence. The last two hold today and
  must still hold after, so they are regression pins rather than failing tests — only the
  first is red.
- [ ] 1.2 Pin that installing a deadline makes the next synchronization read the clock, so
  no evaluation inherits a cadence position from an earlier one. Cover both install paths:
  the direct one and the lazily materialized one.

## 2. Amortize the clock read

- [ ] 2.1 Introduce a clock seam in package `core` — it has none today — and route
  `flushPending`'s existing unconditional read through it. No behavior change, no gating:
  this exists so the tests of 1.1 can count reads.
- [ ] 2.2 Carry the cadence on the evaluation state rather than on the budget, which is
  confined to one GoFunc call, and gate only the clock read in `flushPending`. Verify by
  test that the Reduction charge and the caller-cancellation check stay unconditional.
  The counter SHALL NOT grow `evalState` into a larger size class: the struct is 192 bytes
  and every `Eval` allocates one, so a wider struct moves `B/op` on every gold-set cell
  against per-cell allowances of 0-8 bytes. Verify the size with `unsafe.Sizeof` before and
  after.
- [ ] 2.3 Reset the cadence at both sites that install a deadline rather than relying on
  caller discipline, matching how the VM does it at its own choke point.

## 3. Record the decision

- [ ] 3.1 Record under `## [0.13.0]` in `CHANGELOG.md` that a Builtin now observes an
  expired evaluation deadline within a bounded number of synchronizations rather than at
  the very next one, and why. The entry names the observable change, not the mechanism.
  It belongs to `[0.13.0]`, not `[Unreleased]`: that release is cut but untagged, and this
  change is part of what unblocks it.

## 4. Verify the cost is gone

- [ ] 4.1 Profile `Goldset/queue-promote` under the VM against a `v0.12.0` build and verify
  `time.runtimeNow` falls from ~11% of the profile toward the ~0.5% `v0.12.0` carried.
  Anchor the benchmark pattern: an unanchored `Goldset/...` also matches `GoldsetParse` and `GoldsetCall`.
  Do not use wall-clock A/B or `git bisect` for this verdict — this workstation's noise band
  swallows the signal, and a bisect over this range has already returned a test-only commit.
- [ ] 4.2 Run the repository test suite, the race suite over `core`, `plugins` and
  `runtime`, `go vet`, and the linter; verify every command exits successfully.
- [ ] 4.3 Verify the gold set passes under both execution modes, that the committed VM
  allocation pins in `internal/goldset/alloc_test.go` are unchanged, and that `B/op` is
  unchanged on every `Goldset/*` and `GoldsetParse/*` cell against a pre-change build.
  Allocation counts and bytes are exact locally, unlike latency, so this is measurable here
  and is the last check before the gate.

## 5. Verify on the runner

- [ ] 5.1 Dispatch the release gate against the pushed candidate and verify it reports
  non-regression, no longer fails `Goldset/queue-promote`, and reaches exit 0 so a baseline
  would be stored.
