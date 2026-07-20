# Design — engine-bytecode-default

## Promotion evidence

- Crossval parity: every compiled form cross-validated against the tree-walker;
  unsupported forms return the typed `CodeUnsupported` error and fall back
  per-form (spec: `Bytecode VM execution`).
- Goldset gate (ADR 0008): VM cells non-increasing across the five landed perf
  changes; release job stores and compares the VM baseline.
- Probe measurements (article bench, v0.8.0, `WithBytecode()` only):
  Call 723→260 ns/op (9→2 allocs), Callback 740→365 (9→5), Rule 1884→812
  (24→10), startup 74→120 µs (regression, see below).

## Option semantics

`config.bytecode` flips to `true` by default. Two options mutate it,
last-wins in argument order, matching how the existing options compose:

- `WithBytecode()` — sets true. Kept: removing it breaks every current
  embedder invocation for zero benefit; after the flip it is an explicit
  selection, documented as the default. It is not a silent no-op in the sense
  of the `Configuration options behave as documented` requirement: combined
  with `WithTreeWalker()` it participates in last-wins selection.
- `WithTreeWalker()` — sets false. The documented use cases: debugging a
  suspected VM divergence, and embedders wanting the single-path evaluator.

Rejected: an enum-style `WithEvaluator(kind)` — two boolean options with
last-wins is the smallest surface and matches the existing option idiom.

## Behavioral risk assessment

The VM's observable-behavior obligations are already spec'd and enforced:
result parity per compiled form, typed fallback, error-shape parity, keyword
application parity, `cond` clause shapes per dialect, let-binding parity,
structural-depth hygiene. Known semantic edges (canonical-operator rebind
freezing at head resolution) are cross-validated against the tree-walker's own
behavior.

Residual risk is a program hitting an unknown VM divergence in production.
Containment: `WithTreeWalker()` one-line rollback per embedder, and the
tree-walker path stays fully tested (goldset runs `GOLDSET_MODE=eval` too).

## Startup coupling

Bytecode startup pays per-engine stdlib compilation today (+46 µs). The flip
must not ship a startup regression in a release: `stdlib-startup-cache` lands
in the same release. If that change slips, this one holds.

## Docs and ADR trail

- ADR 0002 amendment: disposition "experimental, opt-in" → "default execution
  path; tree-walker is the complete fallback and the debugging path".
- ADR 0006 amendment: the staged-promotion trigger fired (consumer gate green
  + measured consumer need); record the probe numbers.
- README / ARCHITECTURE / CLAUDE.md / spec Purpose blocks: reword the VM's
  status; document `WithTreeWalker()`.
- CHANGELOG: Changed entry, prominent, with the one-line rollback.

## Verification evidence (task 4.3)

Applied on top of `stdlib-startup-cache` (merged `240e8d7`). Benchstat over 6
counts on the default engine, `BenchmarkEngine_StartupStdlibBytecode`:
cache-disabled 116.5 µs ± 10% / 854 allocs; cache-warm 106.3 µs ± 11% /
754 allocs. No startup regression ships in the release: both changes land
together and the warm process reuses stdlib compilation artifacts. The ≤ ~40 µs
warm target remains with the `stdlib-lazy-materialization` follow-up; the
external article bench (Call/Callback/Rule probe rows) runs at release time
against the published bench repo.
