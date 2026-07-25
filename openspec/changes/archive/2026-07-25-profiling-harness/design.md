# Design

## Constraints that shape the choice

- The repository already has the measurement substrate: `internal/goldset` switches
  execution mode via `GOLDSET_MODE=eval|vm` (`internal/goldset/bench_test.go`), and
  `go test -bench` accepts `-cpuprofile`/`-memprofile` natively. Nothing needs building —
  only wiring and a place to put the output.
- The project's posture is deliberately low-tooling: no CI dashboard, no metrics service,
  and performance findings recorded as prose in design documents rather than committed
  binaries. A harness that departs from that would not be maintained.
- `.github/workflows/release.yml` cannot fire automatically until `GOMAXPROCS` and
  `BENCHTIME` are fixed (both are `""` today, and an empty `BENCHTIME` fails the bench
  step). Profiling needs the same determinism the gate needs, so one choice serves both.

## Fixed run parameters

`GOMAXPROCS=2` and `BENCHTIME=200ms`, with `BENCH_COUNT` staying at 10.

`GOMAXPROCS=2` is chosen for robustness rather than realism. `runs-on: ubuntu-latest` has
had 2 vCPU historically and 4 today; pinning to 2 behaves identically on either, so a
runner-spec change cannot silently shift stored baselines. It still yields real
parallelism for the concurrent tier, whose `RaceClean` requirement is independent of
core count. The gate's job is detecting regression against its own history, not
predicting absolute embedder throughput, so reproducibility outranks fidelity.

`BENCHTIME=200ms` gives roughly 10^5 iterations per sample on a microsecond-scale
fixture, which puts per-sample variance well below the gate's ±5% non-regression
tolerance. Total pure benchmark time is about 48s per mode-pass — affordable even when
ADR 0008's inconclusive-verdict rule doubles it. Time-based rather than iteration-based
so a fixture that grows more expensive yields fewer iterations instead of silently
consuming more wall clock.

These values are a one-way door: changing either invalidates every stored
`bench-vm.txt` baseline for non-regression comparison. Record them as such.

## Harness shape

Two Makefile targets alongside the existing five:

- `profile` — runs `./internal/goldset/` once per mode with the fixed parameters,
  writing `profiles/{eval,vm}.{cpu,mem}.prof`. Creates `profiles/` if absent, the same
  way `build` creates `bin/`.
- `profile-report` — `go tool pprof -top -nodecount=20` over each captured profile.

`profiles/` is gitignored. Profiles are large, machine-specific binaries with no value in
history; the committed artifact is the analysis, not the data.

`-benchmem` is included so the memory profile and the allocation columns come from the
same run and cannot disagree.

## The committed baseline

`docs/profiling-baseline.md`, dated and tied to a commit hash, recording per mode: the
top CPU entries by flat and cumulative time, the top allocation sites, and a short
reading of what each implies. It exists to be superseded — later stages diff against it,
and a stale one is worse than none, so it carries its own date and commit prominently.

The two claims from the 2026-07-18 profile that later stages depend on are re-checked
explicitly and called out by name, whatever the answer:

- whether global lookups still dominate, given the per-chunk site cache landed since;
- whether `nativeAdd` and its siblings still exceed Go's inline budget.

A finding that either has already been fixed is as valuable as a finding that it has not,
because Stage G's priority ordering rests on them.

## Measurement discipline, recorded in the doc

A profile from a contended machine is misleading, and this must be stated where whoever
runs it next will read it:

- Capture on a quiet machine. Nothing else CPU-heavy in parallel — no concurrent test
  run, no build, no second benchmark.
- Timing variance above roughly 15% means re-measure, not conclude.
- `allocs/op` and `B/op` are deterministic; when timings and allocation disagree,
  allocation is the trustworthy signal.

This is not generic advice. During the change that preceded this one, uncontrolled machine
load produced three separate false conclusions — a phantom +42%/+96% regression on `let`
cells, a fake ~100% accumulation regression, and a uniform +10% across cells whose
allocation was byte-identical — each caught only because allocation counts contradicted
the timing story.

## Rejected alternatives

- **A dashboard or metrics service.** Nothing in the repository maintains one, and a
  profiling capability that needs a service to read it will rot. `pprof -top` plus prose
  is sufficient to prioritize.
- **Committing profile binaries.** Machine-specific, large, and superseded by the next
  capture. The analysis is the durable artifact.
- **New instrumentation inside the interpreter.** `go test -bench` already captures what
  is needed; adding hooks to `core/` would risk the zero-dependency invariant and change
  the thing being measured.
- **A separate profiling corpus.** The gold set is already the agreed model of embedder
  workloads and is what the gate judges. Profiling something else would prioritize
  against a workload nobody gates on.

## Verification

The deliverable is a capability plus a measurement, so verification is that both exist
and reproduce: the targets run clean from a fresh checkout, `profiles/` stays untracked,
the committed baseline names its commit and date, and a second run on a quiet machine
reproduces the top entries in the same order. No production code changes, so the existing
floor (`build`, `vet`, `lint`, full suite, `-race`, crossval, gold set) must be unchanged
rather than merely green.
