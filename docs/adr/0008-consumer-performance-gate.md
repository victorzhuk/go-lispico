---
status: accepted
---

# go-lispico owns a gold-set performance gate; releases enforce it

Consumers opt into the VM only after every known VM parity, state-cleanup, cache-freshness, and malformed-input panic defect is fixed, followed by a one-shot per-cell gate over the repo-owned gold set plus scale envelopes; Lua/goja parity does not authorize rollout. Each cell first checks an expected behavior or invariant, hand-derived from the language contract, against both Evaluator and VM runs; neither path is the correctness oracle. Passing the complete gate authorizes YAGEL to add `WithBytecode()` directly, without a user-facing execution flag or side-effecting shadow run; rollback is a normal code or dependency revert. After that first authorization, later releases keep the paired run but compare the candidate VM against the previous release's VM baseline as a non-regression check — the 15%/20% improvement thresholds apply once, so a later Evaluator improvement can never fail the gate by shrinking the delta.

## Gate mechanics

- Timed cells evaluate the rule-shaped gold fixtures through the engine with deterministic fixture data, retaining GoFunc call overhead; scheduler and bus flows stay in YAGEL as untimed end-to-end behavior checks outside this gate.
- go-lispico owns the gate corpus as a gold set: rule-shaped fixtures with independent golden expected results, plus benchmark cells over them, committed in this repo (`internal/goldset`) and modeled on embedder rule workloads — dispatch, closure state, error handling, keyword lookups, macros, collection folds, rule loading. The release gate runs it self-contained — no consumer checkout, no revision pin, no cross-repo secret. Goldens are hand-derived from the language contract, never captured from either engine.
- The authoritative performance run interleaves both execution modes in one hosted job with fixed concurrency and benchtime, at least ten samples, and benchstat confidence; ordinary pull requests run correctness and race checks only. Race-detector runs are separate and untimed — no timing threshold is evaluated under `-race`.
- When benchstat is inconclusive on any cell, the whole paired run reruns once at doubled benchtime and every cell is re-judged from the rerun data. Still inconclusive after the rerun: improvement cells fail (the win was not demonstrated), non-regression cells pass (no regression was demonstrated).
- Each scaled data dimension has three checked-in levels: shipped baseline, an operational knee, and its supported boundary; a lower CI proxy is allowed only when a separate load test covers the real boundary.

## Thresholds

This ADR is the single owner of the numbers; the PRD and glossary reference them. No cell may regress beyond its tier's budget. Before candidate results are produced, a checked-in baseline profile classifies each cell:

- Engine-sensitive hot cells: at least 15% lower latency and 20% fewer allocated bytes, allocation count non-increasing.
- Data/output-dominated hot cells: within 5% latency, bytes and allocation count non-increasing — subject to any per-cell bytes allowance recorded in `internal/perfgate/tiers.json`'s `bytesAllowanceBOp`.
- Concurrent cells (distinct Rule closures on one Engine): within 5% throughput, bytes and allocation count non-increasing, race detector clean in the separate untimed run.
- Startup and Rule-load cells: within 5%, or at most 1 ms and 256 KiB absolute overhead under benchstat, so sub-millisecond one-time work cannot fail on percentage alone; bytes and allocation count stay non-increasing regardless of which of those two latency paths the cell takes — the absolute-overhead escape excuses the latency percentage only, never allocation.

"Within 5%" above is a bound on regression, matching this section's opening rule that no cell may *regress* beyond its tier's budget: a candidate that is faster than its baseline passes at any margin. Reading it as a two-sided band would fail a release for making the engine faster, which is the outcome this ADR rejects below under a standing improvement gate. Two exceptions, both because the two runs are then expected to measure the same cost rather than two releases. First, comparing the Evaluator and VM variants of one commit at first authorization, a data/output-dominated cost is mode-invariant by classification, so movement either way is a finding. Second, the concurrent tier's timed figure may be a throughput measure, where larger is better, so its bound stays two-sided until that sign convention is stated.

Note (resolved-binding cells): the per-chunk global-read site cache adds one
8-byte atomic pointer to every `Chunk`. A cell that recompiles a fresh chunk on
every eval (only `twice-macro`, whose `defmacro` bumps the macro epoch) therefore
carries ~+0.2% B/op with allocation count and latency unchanged — a fixed
per-chunk field, within CI benchstat noise, not a per-operation regression.

Note (non-increasing bounds and benchstat "~"): every tier with a bytes or
allocation-count non-increasing bound, including startup, shares one blind
spot. benchstat reports a non-significant metric delta as `~`, which this
gate's CSV parser turns into `DeltaPct = 0`; a real but non-significant
byte or allocation regression is therefore indistinguishable from no change
and passes the bound undetected. This is a pre-existing gap in the gate's
machinery, not something this ADR resolves.

Note (startup's absolute overhead reading, still open): "at most 1 ms and
256 KiB absolute overhead" above is ambiguous between an absolute *New*
value under the floor and an absolute *delta* (New − Old) under the floor.
The implementation reads it as the former — a cell passes the escape when
its New latency and New bytes each sit under the floor outright, regardless
of Old. The latter reading is unresolved and not implemented; this note
records the choice made, not a decision that the other reading is wrong.

Note (`Goldset/guard-nil`'s named bytes allowance): three hosted dispatch
runs (30630796967, 30637802780, 30639778105) each measured the same
deterministic figure for this cell — 1128 B/op under the Evaluator against
1129 under the VM, +0.09%, p=0.000, 0% CI on both arms. This is not a
removable allocation site: `core/vm.(*VM).run`'s cost offsets almost
exactly against `core.(*engine).Eval`'s, plus a small `sync.Pool`
GC-cadence remainder on the VM's chunk pool — two engines' honest cost for
the same logical work. `guard-nil` therefore carries a named, per-cell,
absolute allowance of 4 B/op on the bytes axis only, recorded in
`internal/perfgate/tiers.json`'s `bytesAllowanceBOp`, sized from those
hosted figures rather than a developer-box estimate. The other thirteen
data-dominated cells — every `GoldsetParse/*` cell — keep the exact
non-increasing bound with no allowance. The mechanism itself is not
tier-specific, and reads wider than this one cell: an entry in that map is
honored wherever a bytes non-increasing bound is applied — data/output-
dominated, concurrent, and startup cells in either mode, and
engine-sensitive cells once they compare against a previous release. It is
inert against the engine-sensitive tier's 20%-fewer-bytes improvement
floor, which is not a non-increasing bound, so an entry naming an
engine-sensitive cell would do nothing at first authorization and take
effect only afterwards. Only `Goldset/guard-nil` carries one today; a
second entry needs the same evidence this one does. This is a distinct fix from
the benchstat-`~` blind spot noted above, which this allowance does not
close — that gap applies to every non-increasing bound in the gate and
remains open. No cell's tier changes.

## Consequences

- Passing the gate ends VM-specific optimization; batched cancellation checks plus a cross-engine step budget, resolved-binding cells, and tagged slots all wait for a failing gate cell or another measured consumer need.
- A profile-proven shared asymptotic or allocation defect in a consumer envelope may also be fixed, but is reported separately and receives no credit toward the VM thresholds; the first case is stdlib `merge`, whose repeated immutable `Assoc` makes fresh-map construction O(n²).
- Changing the global Engine default still requires the dialect-wide evidence anticipated by ADR 0006.

## Considered options

- Keeping the gate only in the benchmark lab: rejected — not a release contract.
- Checking out the live consumer (a pinned revision or a consumer-advanced ref): rejected — it couples the public release job to a private repo (a cross-repo secret held in a public repo, private build output in world-readable logs) and the gate cannot run at all until the consumer publishes its harness. A consumer-exported corpus was also rejected: it leaves the gate blocked on the consumer indefinitely. The repo-owned gold set keeps the gate self-contained and always runnable; representativeness of a real consumer's workload is maintained by evolving the corpus against measured consumer needs.
- A standing 15%-vs-Evaluator gate on every release: rejected — after authorization it punishes Evaluator improvements, failing a release for making the fallback path faster.
- Failing or endlessly rerunning inconclusive benchstat cells: rejected — hosted runners make inconclusive the common case; burden-of-proof (improvement claims fail, non-regression claims pass) keeps the gate decidable after one bounded retry.
