---
status: accepted
---

# YAGEL owns the consumer performance gate; go-lispico releases enforce it

YAGEL opts into the VM only after every known VM parity, state-cleanup, cache-freshness, and malformed-input panic defect is fixed, followed by a one-shot per-cell gate over its shipped rules plus scale envelopes; Lua/goja parity and copied benchmark fixtures do not authorize rollout. Each cell first checks an expected behavior or invariant derived from YAGEL's specifications against both Evaluator and VM runs; neither path is the correctness oracle. Passing the complete gate authorizes YAGEL to add `WithBytecode()` directly, without a user-facing execution flag or side-effecting shadow run; rollback is a normal code or dependency revert. After that first authorization, later releases keep the paired run but compare the candidate VM against the previous release's VM baseline as a non-regression check — the 15%/20% improvement thresholds apply once, so a later Evaluator improvement can never fail the gate by shrinking the delta.

## Gate mechanics

- Hot timing starts at `Evaluator.Apply` and runs through the Rule body with deterministic fake world Primitives, retaining GoFunc call overhead; scheduler and bus flows remain untimed end-to-end behavior checks.
- YAGEL owns the live corpus and benchmark harness; go-lispico release CI checks out YAGEL's `gold` ref — the blessed-release pointer YAGEL advances to the revision it stands behind — and replaces its lispico dependency with the candidate. No revision pin is recorded in go-lispico; YAGEL owns when the pointer moves, so the gate always runs against exactly what the consumer currently blesses.
- The authoritative performance run interleaves both execution modes in one hosted job with fixed concurrency and benchtime, at least ten samples, and benchstat confidence; ordinary pull requests run correctness and race checks only. Race-detector runs are separate and untimed — no timing threshold is evaluated under `-race`.
- When benchstat is inconclusive on a cell, the cell reruns once at doubled benchtime. Still inconclusive after the rerun: improvement cells fail (the win was not demonstrated), non-regression cells pass (no regression was demonstrated).
- Each scaled data dimension has three checked-in levels: shipped baseline, an operational knee, and its supported boundary; a lower CI proxy is allowed only when a separate load test covers the real boundary.

## Thresholds

This ADR is the single owner of the numbers; the PRD and glossary reference them. No cell may regress beyond its tier's budget. Before candidate results are produced, a checked-in baseline profile classifies each cell:

- Engine-sensitive hot cells: at least 15% lower latency and 20% fewer allocated bytes, allocation count non-increasing.
- Data/output-dominated hot cells: within 5% latency, bytes and allocation count non-increasing.
- Concurrent cells (distinct Rule closures on one Engine): within 5% throughput, bytes and allocation count non-increasing, race detector clean in the separate untimed run.
- Startup and Rule-load cells: within 5%, or at most 1 ms and 256 KiB absolute overhead under benchstat, so sub-millisecond one-time work cannot fail on percentage alone.

## Consequences

- Passing the gate ends VM-specific optimization; batched cancellation checks plus a cross-engine step budget, resolved-binding cells, and tagged slots all wait for a failing gate cell or another measured consumer need.
- A profile-proven shared asymptotic or allocation defect in a consumer envelope may also be fixed, but is reported separately and receives no credit toward the VM thresholds; the first case is stdlib `merge`, whose repeated immutable `Assoc` makes fresh-map construction O(n²).
- Changing the global Engine default still requires the dialect-wide evidence anticipated by ADR 0006.

## Considered options

- Keeping the gate only in the benchmark lab: rejected — not a release contract.
- Copying YAGEL fixtures into this repository: rejected — they would drift from the consumer. A revision pin recorded here was also rejected: it has the same drift failure mode and needs a re-pin ritual at every release cut; the consumer-owned `gold` ref removes the pin entirely and keeps pointer movement with YAGEL.
- A standing 15%-vs-Evaluator gate on every release: rejected — after authorization it punishes Evaluator improvements, failing a release for making the fallback path faster.
- Failing or endlessly rerunning inconclusive benchstat cells: rejected — hosted runners make inconclusive the common case; burden-of-proof (improvement claims fail, non-regression claims pass) keeps the gate decidable after one bounded retry.
