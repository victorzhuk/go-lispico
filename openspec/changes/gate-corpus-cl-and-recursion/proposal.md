# gate-corpus-cl-and-recursion

## Why

`compiler-branch-arith-fusion` (527f03c) added `BenchmarkEngine_FibonacciCL`
(`runtime/bench_test.go:388`) explicitly to cover the shipped engine's actual
default: `runtime.New()` builds a Common Lisp dialect engine (Lisp-2,
`GetFuncCanonical` function-cell resolution), while every existing gold-set
cell runs `clojure.Dialect()` (`internal/goldset/goldset.go:36`). The design
doc's own reasoning for adding it was "the gate can go green on a path the
shipped engine never takes." But `release.yml:105-112` runs only
`go test ./internal/goldset/`, and `BenchmarkEngine_FibonacciCL` lives in
`runtime/`. The gate still cannot see the CL path. The mitigation was built
and then left somewhere the gate never looks.

This matters beyond housekeeping because the CL path has a known,
un-gated gap: `Chunk`'s per-instruction global-read site cache
(`core/vm/chunk.go:203`, `buildSites`) explicitly skips `Func:true` entries,
so every fused or unfused native-op dispatch under CL re-walks
`GetFuncCanonical` from scratch — a real cost with zero regression coverage,
left as a deliberate follow-up in `compiler-branch-arith-fusion`'s own design
doc. Separately, no gold-set cell exercises deep recursion (the shape fusion,
ledger batching, and every one of the six pending round-6 VM changes target);
the closest cells (`counter-closure`, `loop-sum`) are closure-state and
bounded-loop shapes, not call-stack recursion.

## What Changes

Per ADR 0008 and `internal/perfgate/tiers.json`, a new gate cell needs a
committed tier backed by a baseline profile — this change's own first task is
producing that profile using `release-gate-activation`'s now-armed gate, which
is why this change is sequenced after it.

- Decide, and do not leave half-built: either (a) widen `internal/goldset`
  with a CL-dialect variant of an existing fixture (or a new one) plus a
  fib-shaped recursion fixture, each committed with a tier and a baseline
  profile justifying it, and delete `BenchmarkEngine_FibonacciCL` in favor of
  the gated equivalent; or (b) delete `BenchmarkEngine_FibonacciCL` and its
  associated "covers the CL path" claim from the fusion change's design doc,
  and record explicitly that the gate remains Clojure-dialect-only by
  decision, not oversight.
- If widened: profile the new CL cell before classifying it. The func-cell
  site-cache gap above will be directly visible for the first time — record
  its magnitude even if this change does not fix it; that fix, if warranted,
  is its own follow-up change.

## Impact

- Affected specs: `consumer-release-gate` (new requirement, if widened:
  gold-set corpus coverage extends beyond one dialect and one recursion-free
  fixture set; or, if not widened, a requirement documenting that scope
  boundary explicitly).
- Affected code: `internal/goldset/` (new fixture(s), new goldens,
  `internal/perfgate/tiers.json` entries) if widened; `runtime/bench_test.go`
  (removal) either way.
- Risk: a new gate cell changes the gate's pass/fail surface for every future
  release — must not land without its own baseline profile per ADR 0008's
  tiers.json rule.
- Sequencing: after `release-gate-activation` (needs its baseline run to
  exist before a new cell's tier can be justified against one).
