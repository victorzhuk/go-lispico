## Why

Existing microbenchmarks do not represent YAGEL's Rule-loading and handler-application paths and provide no release contract for correctness, latency, allocation, concurrency, or scale — they can reward work that never improves the consumer. ADR 0008 makes YAGEL's consumer workload gate the release contract: go-lispico release CI runs YAGEL's own corpus against the candidate, and passing authorizes YAGEL's direct `WithBytecode()` opt-in (PRD stories 13–22, 26, 27).

Blocked by: `dialect-cond-form-shape`, `vm-compile-shape-and-scope`, `vm-runtime-state-parity`, `stdlib-merge-bulk-builder` — the VM correctness floor and the named Shared-path fix precede any performance authorization.

External dependency: YAGEL owns the live corpus, Behavior goldens, deterministic Primitive fakes, benchmark cells, Scale envelopes, and Hot-cell tiers. This change consumes them; it cannot be fully verified until YAGEL publishes its harness and the initial pinned revision is selected.

## What changes

- A go-lispico release CI job checks out the pinned YAGEL revision, replaces its go-lispico dependency with the release candidate, and runs YAGEL's correctness suites and benchmark cells. Cutting a release re-pins to the latest YAGEL release.
- The authoritative performance evidence is a Paired release run: Evaluator and VM variants interleaved in one hosted job with fixed concurrency and benchtime, at least ten samples, and benchstat confidence.
- Threshold evaluation follows ADR 0008: per-cell tiers committed before candidate results, burden-of-proof handling of inconclusive benchstat (one rerun at doubled benchtime; then improvement cells fail, non-regression cells pass), race runs separate and untimed.
- The improvement gate is one-shot: after first authorization, later releases compare the candidate VM against the previous release's VM baseline as non-regression.
- Ordinary pull requests run correctness and race checks only — no percentage gates.

## Capabilities

### New Capabilities

- `consumer-release-gate`: the release-time contract between go-lispico and its consumer — pinned-consumer checkout, paired benchmark run, tiered threshold evaluation, and the one-shot authorization semantics.

### Modified Capabilities

None.

## Impact

- ADRs: implements ADR 0008 end to end; does not change the global Engine default (ADR 0006's dialect-wide evidence still required).
- Out of scope: YAGEL-side harness content (corpus, goldens, cells, envelopes, tiers); Lua/goja parity as a gate; any user-facing execution flag or shadow run; VM representation redesign — passing the gate is the optimization stop line.
