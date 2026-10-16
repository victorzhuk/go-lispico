---
status: accepted
---

# go-lispico owns a gold-set performance gate; releases enforce it

Consumers opt into the VM only after every known VM parity, state-cleanup, cache-freshness, and malformed-input panic defect is fixed, followed by a one-shot per-cell gate over the repo-owned gold set plus scale envelopes; Lua/goja parity does not authorize rollout. Each cell first checks an expected behavior or invariant, hand-derived from the language contract, against both Evaluator and VM runs; neither path is the correctness oracle. Passing the complete gate authorizes YAGEL to add `WithBytecode()` directly, without a user-facing execution flag or side-effecting shadow run; rollback is a normal code or dependency revert. After that first authorization, later releases keep the paired run but compare the candidate VM against the previous release's VM baseline as a non-regression check for every cell that baseline contains — the 15%/20% improvement thresholds apply once, so a later Evaluator improvement can never fail the gate by shrinking the delta. A configured cell that baseline does not contain is a first authorization against the current Evaluator, not a verdict on no evidence.

## Gate mechanics

- Timed cells evaluate the rule-shaped gold fixtures through the engine with deterministic fixture data, retaining GoFunc call overhead; scheduler and bus flows stay in YAGEL as untimed end-to-end behavior checks outside this gate.
- go-lispico owns the gate corpus as a gold set: rule-shaped fixtures with independent golden expected results, plus benchmark cells over them, committed in this repo (`internal/goldset`) and modeled on embedder rule workloads — dispatch, closure state, error handling, keyword lookups, macros, collection folds, rule loading. The release gate runs it self-contained — no consumer checkout, no revision pin, no cross-repo secret. Goldens are hand-derived from the language contract, never captured from either engine.
- The authoritative performance run interleaves both execution modes in one hosted job with fixed concurrency and benchtime, at least ten samples, and benchstat confidence; ordinary pull requests run correctness and race checks only. Race-detector runs are separate and untimed — no timing threshold is evaluated under `-race`.
- When a cell is inconclusive for a reason more data could settle, the whole paired run reruns once at doubled benchtime and every cell is re-judged from the rerun data. Still inconclusive after the rerun: improvement cells fail (the win was not demonstrated), non-regression cells pass (no regression was demonstrated). A cell inconclusive for a reason a rerun cannot change — a runner mismatch — is resolved immediately instead, so a release does not spend a doubled-benchtime run it cannot learn from. A cell the stored baseline never measured is not in that set: a rerun would still not supply the stored figure, so such a cell is settled against the current Evaluator reference instead.
- Each scaled data dimension has three checked-in levels: shipped baseline, an operational knee, and its supported boundary; a lower CI proxy is allowed only when a separate load test covers the real boundary.

## Thresholds

This ADR is the single owner of the numbers; the PRD and glossary reference them. No cell may regress beyond its tier's budget. Before candidate results are produced, a checked-in baseline profile classifies each cell:

- Engine-sensitive hot cells: at least 15% lower latency and 20% fewer allocated bytes, allocation count non-increasing.
- Data/output-dominated hot cells: within 5% latency, bytes and allocation count non-increasing — subject to the per-cell bytes allowance every cell records in `internal/perfgate/tiers.json`'s `bytesAllowanceBOp`.
- Concurrent cells (distinct Rule closures on one Engine): within 5% throughput, bytes and allocation count non-increasing. The timed figure is aggregate `sec/op`, lower better, so the throughput bound is the reciprocal: `candidate sec/op <= reference sec/op / 0.95`. A concurrent cell is evaluated only with explicit clean-race attestation from the separate untimed `-race` run; an unattested cell fails the tier closed.
- Startup and Rule-load cells: within 5%, or at most 1 ms and 256 KiB absolute overhead under benchstat, so sub-millisecond one-time work cannot fail on percentage alone; bytes and allocation count stay non-increasing regardless of which of those two latency paths the cell takes — the absolute-overhead escape excuses the latency percentage only, never allocation.

"Within 5%" above is a bound on regression, matching this section's opening rule that no cell may *regress* beyond its tier's budget: a candidate that is faster than its baseline passes at any margin. Reading it as a two-sided band would fail a release for making the engine faster, which is the outcome this ADR rejects below under a standing improvement gate. The exception is a data/output-dominated cost compared at first authorization: the two runs are then the Evaluator and VM variants of one commit and the cost is mode-invariant by classification, so movement either way is a finding. The concurrent tier's bound is one-sided too, on its stated figure rather than on an open question: aggregate `sec/op`, lower better, bounded reciprocally at `candidate sec/op <= reference sec/op / 0.95`.

Note (resolved-binding cells): the per-chunk global-read site cache adds one
8-byte atomic pointer to every `Chunk`. A cell that recompiles a fresh chunk on
every eval (only `twice-macro`, whose `defmacro` bumps the macro epoch) therefore
carries ~+0.2% B/op with allocation count and latency unchanged — a fixed
per-chunk field, within CI benchstat noise, not a per-operation regression.

Note (runner comparability): a latency conclusion is only sound when the
stored baseline and the candidate ran on the same runner identity, so the
gate reports latency as inconclusive rather than compare figures across a
change of runner, and never normalizes a configuration line to manufacture
a comparison it cannot support. Allocation count survives a change of
runner: it is an exact per-op integer with no iteration-count term, so its
non-increasing bound already reads as an exact comparison independent of
which machine produced it. Allocated bytes do not survive: B/op divides a
benchmark's total bytes by its iteration count, and that iteration count
is itself a property of machine speed — on hosted run 33985766146 the
same cell's iteration counts differed 2.8x between runner identities at
one fixed benchtime. Per-allocation sizing moves independently of that
denominator: on the committed gold-set corpora, where iteration counts
sit within ~10% of each other across runners, every GoldsetParse/* cell
still moves +16 to +288 B/op between runners with allocs/op identical.
Both mechanisms make B/op a property of the machine rather than of the
code under test, so the gate reports the bytes axis as inconclusive
across a change of runner identity and enforces it against the cell's
stated allowance only when the stored baseline and the candidate share a
runner identity. Within a single runner B/op is a benchmark-tool average
that still wobbles run to run, which is why the bytes axis, and not the
allocation-count axis, carries a stated per-cell allowance at all.

Note (benchstat "~" blind spot, latency only): benchstat reports a
non-significant metric delta as `~`, which this gate's CSV parser turns
into `DeltaPct = 0`; a real but non-significant regression is therefore
indistinguishable from no change and passes undetected. The bytes and
allocation-count axes no longer pass through benchstat at all — the gate
reads both directly off the raw benchmark output — so this blind spot now
applies to the latency axis only. This is a pre-existing gap in the gate's
machinery, not something this ADR resolves.

Note (bytes/allocation-count axes depend on charge reproducibility): because
these two axes read their figures directly rather than through benchstat,
they carry no statistical test standing between the gate and the number it
compares. That reading is only decidable while every charge the ledger
applies reproduces exactly for a given input, per ADR 0011's determinism
requirement — a charge that could vary run to run for the same input would
move a cell's bytes or allocation count with no code change behind it, and
the gate would have no way to tell that apart from a real regression.

Note (startup's absolute overhead reading, still open): "at most 1 ms and
256 KiB absolute overhead" above is ambiguous between an absolute *New*
value under the floor and an absolute *delta* (New − Old) under the floor.
The implementation reads it as the former — a cell passes the escape when
its New latency and New bytes each sit under the floor outright, regardless
of Old. The latter reading is unresolved and not implemented; this note
records the choice made, not a decision that the other reading is wrong.

Note (per-cell bytes allowances): three hosted dispatch
runs (30630796967, 30637802780, 30639778105) each measured the same
deterministic figure for this cell — 1128 B/op under the Evaluator against
1129 under the VM, +0.09%, p=0.000, 0% CI on both arms. This is not a
removable allocation site: `core/vm.(*VM).run`'s cost offsets almost
exactly against `core.(*engine).Eval`'s, plus a small `sync.Pool`
GC-cadence remainder on the VM's chunk pool — two engines' honest cost for
the same logical work. `guard-nil` therefore carries a named, per-cell,
absolute allowance of 4 B/op on the bytes axis only, recorded in
`internal/perfgate/tiers.json`'s `bytesAllowanceBOp`, sized from those
hosted figures rather than a developer-box estimate.

Every gold-set cell now states an allowance in that map, and an absent
entry is a configuration error rather than a silent zero. Thirteen are
nonzero, ranging from 1 to 8 B/op; the remaining fourteen state an
explicit 0 — a stated zero, which is what the exact non-increasing bound
means. Each nonzero value is bounded by the within-run sampling spread
that cell shows on the checked-in 200 ms classification profile, so an
allowance never exceeds the wobble the measurement itself carries.
`guard-nil`'s 4 is the exception: it is sized from the reproducible
between-engine offset above, not from spread. The per-cell numbers live
in `tiers.json` and this ADR does not restate them; a new nonzero
allowance needs evidence of one of those two kinds. The mechanism itself
is not tier-specific, and reads wider than any one cell: an entry in that map is
honored wherever a bytes non-increasing bound is applied — data/output-
dominated, concurrent, and startup cells in either mode, and
engine-sensitive cells once they compare against a previous release. It is
inert against the engine-sensitive tier's 20%-fewer-bytes improvement
floor, which is not a non-increasing bound, so an entry naming an
engine-sensitive cell would do nothing at first authorization and take
effect only afterwards. This is a distinct fix from
the benchstat-`~` blind spot noted above, which these allowances do not
close — that gap applies to the latency axis and remains open. No cell's
tier changes.

## Known coverage gaps: thresholds stated here and NOT enforced

Both gaps below are current, known, and unexercised. Both are gaps in the
timed workload corpus, not in correctness coverage: correctness goldens for
Common Lisp and for distinct-closure sharing of one engine exist and pass
today. Neither is a defect in the gate's machinery; each is a threshold or a
coverage claim that no configured timed cell reaches. The timed fixtures now
exist (`BenchmarkGoldsetCL`, `BenchmarkGoldsetCLParse`,
`BenchmarkGoldsetConcurrent` in `internal/goldset/bench_test.go`), but no tier
is configured for any of them, so the gate does not evaluate them. Promoting
those measurements is DEFERRED, not rejected. The gate's own guards block the
straightforward fix, and the evidence that would unblock it is a hosted
classification capture, not an edit. Neither gap changes a tier, a threshold,
or an exit code.

### The concurrent tier's 5% throughput threshold is configured but unexercised

`internal/perfgate/tiers.json` configures 12 engine-sensitive cells, 14
data-dominated cells, and 1 startup cell. It configures zero concurrent
cells. The concurrent threshold stated above (distinct Rule closures on one
Engine, within 5% throughput, bytes and allocation count non-increasing,
clean-race attestation required) is therefore not enforced by any run, and
cannot be. A candidate that regresses concurrent throughput by far more than
5% passes this gate today. The `GoldsetConcurrent/distinct-closures` benchmark
measures shared-engine contention, but it is unconfigured, so no cell verdict
rests on it.

### Common Lisp, the shipped default dialect, has no timed workload coverage

The timed corpus constructs the Clojure dialect for its configured cells. Only
`stdlib` and `json` ship in `cmd/lispico`, so the dialect a consumer actually
gets with no option is Common Lisp. A green workload gate therefore cannot
establish non-regression for the shipped default, including its Lisp-2
function resolution, where an operator and a variable live in separate
namespaces. The CL fixtures (`GoldsetCL/function-cells`,
`GoldsetCL/list-adapters`, and their parse cells) run for correctness and as
capture input, but no tier is configured for them, so a regression in that
resolution would not fail this gate today.

### Two independent guards block the straightforward fix

1. The spread-justification guard,
   `TestBytesAllowancesAreJustifiedBySpread` in
   `internal/perfgate/bytes_allowance_spread_test.go:39-48`, requires
   historical profile samples for EVERY configured cell before it inspects
   the cell's stated allowance, including cells whose `bytesAllowanceBOp` is
   a stated `0`; setting an allowance to `0` does not avoid the assertion.
   The pinned profile is `profile-30637802780`
   (`internal/perfgate/perfgate_test.go:705-706`), a capture taken in the
   past, so it cannot contain a cell introduced after it. Any new gold-set
   cell, concurrent or Common Lisp or otherwise, therefore makes that test
   fail with `cell %q has no B/op samples in %s`.
2. The classification-inventory guard, `TestPinnedProfile` at
   `internal/perfgate/perfgate_test.go:705-781`, requires exact inventory
   equality between the selected classification profile, the committed tiers,
   and the verdicts. A new configured cell fails that guard whatever its
   allowance says.

Restricting the spread-justification requirement to explicit-zero cells, as
described next, removes the first blocker only. It does not authorize a new
tier or a new profile, and it is not by itself a reason to commit expanded
tiers.

### What a new cell would actually do to gate runs

Baseline discovery is run-wide: `ResolveGateMode`
(`internal/perfgate/gatemode.go:45-58`) selects one mode for the whole run from
whether a stored `bench-vm.txt` baseline was downloaded, and the run supplies a
primary reference dataset from `-old`. Cell mode is decided separately by
`ResolveCellMode` (`internal/perfgate/gatemode.go:63-74`): under a
first-authorization run every cell is first authorization, while under
non-regression a cell whose exact raw name the stored baseline measured keeps
non-regression and a configured cell the stored baseline does not contain is a
first authorization. Reference selection follows the same split in
`cmd/perfgate/main.go`: cells in the stored baseline compare against `-old`;
cells absent from it are drawn from a second reference, the current Evaluator
supplied by `-evaluator`, and their report line reads
`mode=first-authorization reference=evaluator`. Required supplemental evidence
that is absent refuses the gate with exit 3 rather than reading as a silent
pass, and a stored baseline that is present always wins for a cell — including
when its evidence is incomplete or the two runs are incomparable, which is the
existing partial-evidence policy below. A classification-only run proves the
capture prerequisites — that the corpus, both modes, the runner identity, and
the artifacts exist — and authorizes no ordinary gate decision, because it
produces no release verdict and writes no stored baseline.

A cross-runner run is governed by the partial-evidence policy stated above:
latency and allocated bytes stay undecided, allocation-count non-increase is
still enforced for common cells, and correctness plus race checks remain
workflow prerequisites. An exit of zero there is authorization under
comparable axes, never a latency or allocated-bytes non-regression proof.

Candidate evidence itself is preflight-checked before any verdict: every
configured cell must carry timed, `B/op`, and `allocs/op` samples in the
candidate run. A cell absent from the candidate and a cell that timed but
skipped a metric are both rejected with exit 3 before benchstat runs, before
the missing-baseline branch, and before any cell verdict; a measured zero is
evidence, not an absence.

Baseline evidence is checked on the same footing, and the two absences are
distinct. A configured cell the stored baseline never measured is not a
failure and not a zero: it is a first authorization against the current
Evaluator as described above, and it is not counted as a comparison against
the stored baseline. A configured cell the stored baseline *did* measure but
whose allocation samples are absent is refused with exit 3 before benchstat
runs and before any verdict is formed, naming the cell and every absent unit
in the error `compare samples for cell %q: missing allocation samples:
ITEM[, ITEM...]`, where the items are the literals `baseline B/op`,
`baseline allocs/op`, `candidate B/op`, and `candidate allocs/op`. Without
that refusal an absent sample set would read as a zero median and a silent
improvement against a real baseline. The candidate preflight above runs first,
so a cell whose both arms are incomplete is reported through the candidate
diagnostic rather than this one.

On the same runner, benchstat pairs only the exact raw benchmark names
present in both arms of one reference group, processor suffix included. A cell
measured by one arm alone is filtered out of that group's statistics rather
than aborting the run, and the cell then either takes the Evaluator reference
described above or drops out of the comparison entirely. A common cell whose
paired result carries no latency figure, or a zero sample count, is refused
with exit 3 as `cell %q: missing paired sec/op comparison`: benchstat's `~`
would otherwise let it read as an insignificant pass on no evidence. A run in
which no cell reached an actual metric comparison prints its per-cell report
and then exits 3 with `no comparison was performed` on stderr instead of
passing on aggregate evidence that compared nothing.

One delta token needs a second reading. The pinned benchstat renders a
zero-baseline percentage change as `?`, because the ratio has no finite
form. `parseMetricRow` (`internal/perfgate/parse.go:118-153`) accepts that
token only when the parsed old value is zero: a positive candidate becomes a
positive-infinite delta marked significant, matching what the sample
comparison already means for a zero-to-positive move, and an equal zero
becomes no change and is not significant. Any other `?`, and any other
unparseable delta, still returns `parse delta %q` and exits 3. The token is
narrow: nothing else about strict parsing moves.

### Remaining prerequisites for closing either gap

1. Amend the spread-justification guard so the historical-sample requirement
   applies to cells that carry a nonzero `bytesAllowanceBOp`, rather than to
   every configured cell. A cell stating `0` is bounded by the exact
   non-increasing bound itself and needs no spread evidence to justify a
   number it does not state. This corrects an overconstraint; it grants no
   coverage and no new authorization.
2. Capture fresh hosted classification evidence for the new cells, through a
   dedicated Evaluator/VM classification run, which is a separate step from
   ordinary release-baseline collection against the previous release's VM.
3. Commit the expanded tiers together with the matching selected profile,
   its provenance, its digests, the derived profile files, and the verdicts
   that `TestPinnedProfile` compares. Existing historical raw captures stay
   verbatim; they cannot measure a cell introduced later and are evidence for
   the cells they already cover.
4. Then add the concurrent timed cell and the Common Lisp timed cells and
   re-baseline at a hosted gate run. Until a run measures them, these
   thresholds remain unexercised.

## Consequences

- Passing the gate ends VM-specific optimization; batched cancellation checks plus a cross-engine step budget, resolved-binding cells, and tagged slots all wait for a failing gate cell or another measured consumer need.
- A profile-proven shared asymptotic or allocation defect in a consumer envelope may also be fixed, but is reported separately and receives no credit toward the VM thresholds; the first case is stdlib `merge`, whose repeated immutable `Assoc` makes fresh-map construction O(n²).
- Changing the global Engine default still requires the dialect-wide evidence anticipated by ADR 0006.

## Considered options

- Keeping the gate only in the benchmark lab: rejected — not a release contract.
- Checking out the live consumer (a pinned revision or a consumer-advanced ref): rejected — it couples the public release job to a private repo (a cross-repo secret held in a public repo, private build output in world-readable logs) and the gate cannot run at all until the consumer publishes its harness. A consumer-exported corpus was also rejected: it leaves the gate blocked on the consumer indefinitely. The repo-owned gold set keeps the gate self-contained and always runnable; representativeness of a real consumer's workload is maintained by evolving the corpus against measured consumer needs.
- A standing 15%-vs-Evaluator gate on every release: rejected — after authorization it punishes Evaluator improvements, failing a release for making the fallback path faster.
- Failing or endlessly rerunning inconclusive benchstat cells: rejected — hosted runners make inconclusive the common case; burden-of-proof (improvement claims fail, non-regression claims pass) keeps the gate decidable after one bounded retry.
