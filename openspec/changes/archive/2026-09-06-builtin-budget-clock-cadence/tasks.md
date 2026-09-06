## 1. Pin the defect

- [x] 1.1 Pin the synchronization contract in one pass, before the cadence lands: that a
  sequence of short Builtin calls under an armed deadline reads the wall clock a bounded
  fraction of the times it synchronizes rather than once per call; that an expired
  deadline still terminates Builtin work within the documented number of synchronizations
  with the error shape unchanged; and that caller cancellation and the Reduction charge
  still occur at every synchronization, including mid-cadence. The last two hold today and
  must still hold after, so they are regression pins rather than failing tests — only the
  first is red.
- [x] 1.2 Pin that installing a deadline makes the next synchronization read the clock, so
  no evaluation inherits a cadence position from an earlier one. Cover both install paths:
  the direct one and the lazily materialized one.
- [x] 1.3 Pin forced deadline checking while settling a pending ordinary error, including
  an empty remainder; preserve normal cadence, terminal precedence, cancellation,
  latching, and reduction charging exactly once.
- [x] 1.4 Reproduce the existing runtime deadline-precedence regression before migrating
  consumers, then retain it unchanged as the integration proof.
- [x] 1.5 Pin zero allocations for standard terminal-error classification and budget
  settlement. Preserve first typed match, joined-error order, custom hooks, target
  identity, and the existing deadline/error-precedence contract.

## 2. Amortize the clock read

- [x] 2.1 Introduce a clock seam in package `core` — it has none today — and route
  `flushPending`'s existing unconditional read through it. No behavior change, no gating:
  this exists so the tests of 1.1 can count reads.
- [x] 2.2 Carry the cadence on the evaluation state rather than on the budget, which is
  confined to one GoFunc call, and gate only the clock read in `flushPending`. Verify by
  test that the Reduction charge and the caller-cancellation check stay unconditional.
  The counter SHALL NOT grow `evalState` into a larger size class: the struct is 192 bytes
  and every `Eval` allocates one, so a wider struct moves `B/op` on every gold-set cell
  against per-cell allowances of 0-8 bytes. Verify the size with `unsafe.Sizeof` before and
  after.
- [x] 2.3 Reset the cadence at both sites that install a deadline rather than relying on
  caller discipline, matching how the VM does it at its own choke point.
- [x] 2.4 Introduce the budget error-settlement API with the current flush-and-select
  behavior, ready for the deterministic contract tests.
- [x] 2.5 Force deadline and caller-cancellation checks when settling a pending ordinary
  error, preserving the shared cadence for ordinary synchronization.
- [x] 2.6 Migrate collection, adapter, and standard-library error selectors to the new
  settlement API; preserve result charging and terminal-error precedence.
- [x] 2.7 Remove the standard-error classification allocation in `IsTerminalEvalError`.
  Preserve `errors.Is` ordering and `errors.As` semantics, including custom hooks;
  retain Go 1.24 support and leave budget synchronization unchanged.

## 3. Record the decision

- [x] 3.1 Record under `## [0.13.0]` in `CHANGELOG.md` that a Builtin now observes an
  expired evaluation deadline within a bounded number of synchronizations rather than at
  the very next one, and why. The entry names the observable change, not the mechanism.
  It belongs to `[0.13.0]`, not `[Unreleased]`: that release is cut but untagged, and this
  change is part of what unblocks it.
- [x] 3.2 Update the existing metering ADR and release entry to describe forced checking
  during error settlement alongside the ordinary eight-synchronization bound.

## 4. Verify the cost is gone

- [x] 4.1 Verify exact clock counts with the existing deterministic tests: first read,
  then every eighth ordinary synchronization, plus forced ordinary-error settlement.
  Preserve unconditional reduction/cancellation checks and the eight-sync expiry bound.
  Retain existing profiles as diagnostics; local CPU percentages and latency A/B are
  not acceptance gates. The unchanged hosted gate remains required by 5.1.
- [x] 4.2 Run the repository test suite, the race suite over `core`, `plugins` and
  `runtime`, `go vet`, and the linter; verify every command exits successfully.
- [x] 4.3 Verify the gold set under both modes, unchanged committed VM allocation pins,
  and a 192-byte `evalState`. Compare all 52 evaluation/parser cells against the base
  in two predetermined single-worker runs: 32,768 warmups, 10,000 measured calls, GC off,
  zero reader/VM pool misses and zero runtime interface-cache builds. A candidate overlay restoring the baseline classifier
  must match baseline allocation counts, byte totals and size-class counts exactly.
  The final classifier may only remove its target allocations; no positive or
  unexplained delta is accepted. Every variant must repeat identically.
  Standard-error classification and settlement must allocate zero with precreated
  inputs; custom-hook traversal may retain one shared target cell when required by
  `errors.As` semantics. Rounded raw `B/op` remains diagnostic only.

## 5. Verify on the runner

- [ ] 5.1 Dispatch the release gate against the pushed candidate and verify it reports
  non-regression, no longer fails `Goldset/queue-promote`, and reaches exit 0 so a baseline
  would be stored.
