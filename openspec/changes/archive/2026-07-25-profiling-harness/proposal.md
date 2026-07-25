## Why

The repository has a formal consumer performance gate — `internal/perfgate` with four
tiers and committed thresholds, `internal/goldset` with twelve hand-derived fixtures run
under both execution modes, and roughly seventy benchmarks — but no way to answer *where
time actually goes*. There is no `pprof` invocation anywhere in the source or the
Makefile, whose five targets are `build`, `test`, `test-unit`, `lint`, and `fmt`.

The only profile on record lives in an archived design doc
(`openspec/changes/archive/2026-07-18-vm-dispatch-loop-tightening/design.md`) and reports
fib(20) as global lookups 33%, `VM.Run` 19% flat, cancellation 10%,
`GetConstant`/`GetSymbolConstant` ~9%, push/pop ~6%. Every one of those numbers predates
work that has since landed: the per-chunk global site cache, batched cancellation,
budget-only polling, hashmap compaction, lazy stdlib materialization, and persistent
sequence structures. The figures cannot be assumed to describe the current engine, and
two concrete findings recorded beside them — that `nativeAdd` and its siblings exceed
Go's inline budget (cost 156-513 against ~80), and that global lookups dominated — have
never been re-measured against today's code.

This matters beyond curiosity. Several candidate optimizations differ by an order of
magnitude in payoff depending on which of them is true today, and one of them
(modernizing the tree-walking evaluator toward the VM's slot-based design) is a large
change whose cost/benefit nothing in the repository currently establishes either way.
Choosing among them on the strength of a stale profile risks spending the effort in the
wrong place.

A second, smaller gap compounds it: `.github/workflows/release.yml` is
`workflow_dispatch`-only, with its automatic trigger commented out pending a fixed
`GOMAXPROCS` and `BENCHTIME` that were never chosen. The perf gate needs those values to
produce comparable runs, and profiling needs the same determinism. Picking them once
serves both.

## What Changes

- A `make profile` target runs the gold set under both execution modes with
  `-cpuprofile` and `-memprofile`, writing into a gitignored `profiles/` directory. A
  `make profile-report` target summarizes them through `go tool pprof -top`.
- A fixed `GOMAXPROCS` and `BENCHTIME` are chosen and recorded, unblocking the release
  workflow's automatic trigger as a side effect.
- A dated baseline analysis is committed as prose — top CPU and allocation entries per
  mode, tied to a commit hash — following this repository's existing practice of keeping
  performance findings in design documents rather than committing binary artifacts.
- Optionally a small `cmd/profdiff` wrapping `go tool pprof -diff_base`, mirroring the
  existing `cmd/perfgate` pattern, so later stages can show before/after directly.

No production code changes. No behavior changes. The deliverable is the measurement
capability and the first measurement taken with it.

## Capabilities

### Modified Capabilities

- `consumer-release-gate`: `Paired release run` already requires "fixed concurrency and
  benchtime" but never named the values, which is precisely why the release workflow is
  blocked. The requirement is amended to commit `GOMAXPROCS=2` and `BENCHTIME=200ms`, to
  say why each was chosen, and to state that changing either invalidates every stored
  baseline for non-regression comparison — candidate and baseline are otherwise not
  measured under the same conditions. A scenario pins that last property.

The harness itself (Makefile targets, `profiles/` ignore rule, the committed baseline
analysis) is developer tooling and carries no requirement.

## Impact

- Code: `Makefile`, `.gitignore`, optionally `cmd/profdiff/main.go`. Nothing under
  `core/`, `runtime/`, or `plugins/`.
- Risk: scope creep into building a dashboard or a metrics service. The controlling
  decision is that `go tool pprof` output plus a committed markdown summary is
  sufficient, and matches the project's existing low-tooling posture — there is no CI
  dashboard and no metrics service today.
- Risk: a profile captured on a contended machine is misleading. The fixed
  `GOMAXPROCS`/`BENCHTIME` choice and a documented quiet-machine requirement are the
  control; allocation counts are deterministic and are the trustworthy signal when
  timings disagree.
- Sequencing: first of the staged performance program, because the stages that follow —
  particularly the tree-walker decision and the arithmetic inlining work — are prioritized
  against its output rather than against the stale figures.
