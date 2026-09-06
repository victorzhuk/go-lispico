## Context

`release-gate-baseline-comparability` made the gate refuse a latency conclusion across a
change of runner while keeping allocation counts and allocated bytes enforced, on the
reasoning that both "are properties of the program, not of the machine". The first dispatch
of that gate refuted the second half of the sentence, and `v0.13.0` is held untagged until
the gate passes and stores a baseline.

The gate lives in two packages. `cmd/perfgate/main.go` reads both raw benchmark files,
compares their runner identities (`crossRunner := !sameRunner(oldID, newID)`, main.go:197),
runs benchstat only when they match, and takes bytes and allocation figures off the raw
per-sample data either way. `internal/perfgate` owns the verdict: `Evaluate` dispatches per
tier to `evaluateNonRegression`, `evaluateWithinTolerance`, `evaluateStartup` or
`evaluateEngineSensitiveImprovement`, and three of those four open with
`nonIncreasing(cell.Bytes, "bytes", cell.BytesAllowanceBOp)`.

That opening call is the whole problem. It short-circuits ahead of the allocation check and
ahead of the latency significance gate, so a bytes failure on a cross-runner pair returns
before `main.go`'s `case crossRunner:` branch is ever reached — the branch that exists
precisely to say a cross-runner comparison is not evidence.

Two measurements taken during planning, both from corpora already committed to the
repository, fix the shape of the fix:

- **Swapped cross-runner pair** (`profile-30637802780` AMD as baseline, `profile-30614184386`
  Intel as candidate): all thirteen `Goldset/*` cells gain exactly one allocation, and all
  thirteen `GoldsetParse/*` cells hold `allocs/op` identical while moving +16 to +288 B/op.
  The partition is clean, and it is what the new tests are seeded from.
- **Iteration counts on that pair are within about 10%** — `Goldset/counter-closure-2` runs
  28153 against 28812 — not the 2.8x of the motivating run 33985766146. So on this pair the
  bytes movement is not amortisation over a different denominator at all; it is per-allocation
  sizing differing across machines. Two distinct mechanisms, one conclusion.

## Goals / Non-Goals

**Goals:**

- Report allocated bytes as inconclusive across differing runner identities, naming both
  runners, exactly as latency already is.
- Keep allocation counts enforced unconditionally, and keep allocated bytes fully enforced
  against the unchanged per-cell allowances whenever the identities match.
- Record in ADR 0008 which axis survives a change of runner, and why — covering both
  mechanisms, not only the denominator.

**Non-Goals:**

- Widening any allowance. `release-gate-baseline-comparability` established that an allowance
  is not widened to admit a measured regression, and the bound here is not too tight; the axis
  is not comparable.
- Changing any value in `internal/perfgate/tiers.json`.
- `evaluateEngineSensitiveImprovement`'s inline 20%-fewer bytes floor. It sits below the
  latency significance gate, which every cross-runner cell trips first, and it is an
  improvement claim measured between two arms of one run on one runner.
- `evaluateStartup`'s absolute `256 KiB` overhead bound. It reads one run's own figure as an
  escape valve that can only turn a latency FAIL into a PASS, so a machine-inflated `B/op`
  makes it harder to satisfy, never easier.

## Decisions

**The axis decision lives in `internal/perfgate`, on the cell.** A new
`CellComparison.BytesNotComparable string`, read by a new unexported `bytesResult`, wrapped by
a new unexported `exactAxes` that the three evaluators call in place of their current
bytes-then-allocs pair. `cmd/perfgate` populates it from the identity comparison it already
performs, beside `cell.RaceClean` and `cell.BytesAllowanceBOp`.

Rejected: neutralising the verdict in `cmd/perfgate`. It would have to recognise a retired
bytes failure by string-matching the evaluator's own prose, it cannot separate a bytes failure
that must be retired from an allocation failure that must stand — `main.go` sees one `Result`
— and it would leave `Evaluate` still converting an incomparable difference into a failure, so
`internal/perfgate`'s own tests would keep asserting the retired rule. Rejected: zeroing
`cell.Bytes` before `Evaluate`, which turns an undecided axis into a silent PASS
(`nonIncreasing` returns PASS on `DeltaPct <= 0`) and destroys the figures the report needs.

**The zero value enforces.** The field is a string; empty means comparable, so the bytes bound
is enforced and an unset field fails closed. This is the same discipline as
`CellComparison.RaceClean` at the opposite polarity, because for this axis the strict outcome
is enforcement rather than refusal. A string rather than a bool because the reason must name
both runners and only `cmd/perfgate` holds the `RunnerIdentity` values, so one field carries
the decision and its evidence. The consequence is deliberate and load-bearing:
`internal/perfgate/bench_metrics_test.go:80` `TestCrossRunner_ZeroBaselineBytesFails` builds a
bare `CellComparison` and still gets `VerdictFail` with reason `bytes increased`. That test is
unchanged, and must stay unchanged.

**Reason strings.** Package layer: `"bytes not comparable: " + BytesNotComparable`, joined with
`"; "` by a new `joinReasons` when latency is undecided too, so neither fact is lost. Command
layer, replacing the reason assignment at `main.go:262`:
`"latency and bytes not comparable: baseline ran on %s, candidate on %s"`. An undecided bytes
axis is distinguishable from a passing one because a passing bytes axis prints no bytes clause
at all. Note that the command layer's overwrite discards the package-built reason wholesale:
the two mechanisms never compose, and the package prefix is observable only through the
`internal/perfgate` unit test.

**No `needs-rerun`, and the exit code is unchanged in kind.** `main.go` already selects
`case crossRunner:` ahead of `case rerun:` and never raises `needsRerun` there — a rerun
regenerates only the candidate, so it cannot change which machine the baseline came from. The
cell collapses through `Resolve(tier, mode)`: under non-regression every tier returns
`VerdictPass`, so an otherwise-clean cross-runner run reaches exit 0 and the release it
authorizes stores a baseline. The printed line still reads `INCONCLUSIVE`. None of this is new
behaviour; it is the path the bytes axis now joins.

**Task 1.4 as originally written could not hold, and `tasks.md` was corrected.**
`cmd/perfgate/main_test.go:87` `TestCrossRunner_LatencyInconclusiveBytesEnforced` carried a
subtest asserting uniform `FAIL`, the reason substring `bytes increased`, and exit 1 on the
exact pair the delta spec now requires to be inconclusive. That subtest is the pin on the rule
being retired. Section 1 was rewritten so task 1.1 authors all three cross-runner tests in one
pass, and section 2 gained task 2.4 for the `cmd/perfgate` half of the narrowing.

## Risks / Trade-offs

- A real cross-runner allocated-bytes regression now goes unreported until a release whose
  runners match. Allocation count is the only allocation guard on the cross-runner path, and
  the delta spec accepts that trade.
- The red tests' cell partition is derived from two committed corpora. If either is replaced,
  the partition must be re-derived, not assumed. `TestPinnedProfile` pins both by sha256, so
  no fixture may be edited in place; filtered variants go to `t.TempDir()`.
- A cross-runner run reports `INCONCLUSIVE` while exiting 0. An operator reading only the exit
  code learns nothing about which axes were undecided, which is what makes the reason string
  load-bearing.
- Task 4.2 dispatches `.github/workflows/release.yml` and has no local execution path. A local
  run proves the verdicts, reason text and exit codes against the committed corpora; it cannot
  prove that the `success()` guard on `Store VM baseline on the authorized release` now lets a
  cross-runner release store one.
- The floor's race suite runs `plugins`, where `TestDecodeHashMap_Scaling` has roughly 0.1
  headroom on its 3.0 threshold under load.

## Implementation plan

Mode `existing-service-strict`. Tier `standard`. Lenses `spec` and `quality` — no new package
or moved boundary (no `arch`), no auth, input parsing, SQL or secrets (no `sec`), and the
change is a CI gate rather than a hot path (no `perf`).

Two parallel roots, then one serial chain. `axis-field` (shard `axis`) and `decision-record`
(shard `docs`) start together; everything else follows in order.

**1. `axis-field`** — tasks 2.1 · shard `axis`, parallel · coder `zpatcher`

Declare `BytesNotComparable string` on `CellComparison`, unused, so the red test compiles and
fails on assertions rather than on the build. Site anchor `BytesAllowanceBOp float64`
(`internal/perfgate/perfgate.go`). Doc-comment it in that field's idiom, stating that the zero
value enforces.
Verify: `go build ./... && go test -timeout 2m ./internal/perfgate/... && go vet ./internal/perfgate/... && golangci-lint run ./internal/perfgate/...`

**2. `axis-logic`** — tasks 2.1, 2.2 · prev `axis-field`, sharedPkg `internal/perfgate` · coder `go-coder`

Red: `TestEvaluate_BytesNotComparable`, six subtests — `inconclusive rather than failing`,
`allocs still decided`, `both undecided axes reported`, `startup tier does not reach the
absolute bound`, `within-tolerance tier is narrowed too` (`evaluateWithinTolerance` via
`TierDataDominated` under `ModeFirstAuthorization`), `concurrent tier is narrowed too`
(`TierConcurrent`, which requires `RaceClean: true` or `Evaluate` short-circuits to
`VerdictFail` at perfgate.go:156 before the bytes axis is reached). A seventh subtest,
`unset field enforces the bytes bound`, is a non-regression pin: it asserts today's behaviour
and must pass, not fail.
Code: `bytesResult`, `exactAxes`, `joinReasons`, and the rewire of all three evaluators.
Rewiring fewer than three leaves the untouched one enforcing bytes across runners while the
red stays green.
Task 2.2 is verify-only. The two existing `tiers.json` tests prove every cell states a
spread-justified allowance; that no committed number moved is proven by the diff.
Red run: `! go test -timeout 2m -run '^TestEvaluate_BytesNotComparable$' ./internal/perfgate/`
Verify: `go test -timeout 2m ./internal/perfgate/... && go vet ./internal/perfgate/... && golangci-lint run ./internal/perfgate/...`

**3. `cmd-cross-runner`** — tasks 1.1, 2.4 · prev `axis-logic`, sharedPkg `cmd/perfgate` · coder `go-coder`

This chunk's red stage is the **only** writer of a `_test.go` under `cmd/perfgate`; it seals
the package's tests, and every later chunk in this shard is verify-only.

Red, three tests in one pass:
- `TestCrossRunner_AllocsEnforcedBytesInconclusive` — the rename of
  `TestCrossRunner_LatencyInconclusiveBytesEnforced` at main_test.go:87, doc comment at
  :82-86 rewritten. Keep `latency inconclusive naming both runners` as is. Replace
  `bytes still enforced across differing runners` (:107-126) with
  `bytes inconclusive across differing runners` over the thirteen `GoldsetParse/*` cells, and
  add `allocation counts still enforced across differing runners` over the thirteen
  `Goldset/*` cells, asserting a reason naming the allocs axis and exit 1.
- `TestCrossRunner_CleanRunExitsZero` — a new `keepingCells` helper, modelled on
  `withoutCPULines`, copies the `goos:`/`goarch:`/`pkg:`/`cpu:` preamble **verbatim** and keeps
  only the `Benchmark` lines matching a prefix. Filter the Intel candidate to `GoldsetParse/`:
  all thirteen are data-dominated with allocs identical, so every cell collapses through
  `Resolve` to PASS and the run exits 0.
- `TestSameRunner_BytesStillEnforced` — `swapBenchstat`, not `runGate` (whose benchstat stub
  fails the test if reached). Roles swapped inside `profile-30637802780`. Assert the exact
  thirteen-cell FAIL set on bytes — the twelve `Goldset/*` other than `guard-nil`, plus
  `GoldsetCall/call-boundary-2` at 0 → 768 B/op — and the fourteen clean cells
  (`Goldset/guard-nil-2`, which *decreases* 1129 → 1128, and all thirteen `GoldsetParse/*` at
  exactly +0). Never assert uniformity. **This test is green before and after the narrowing.**
  It is a characterization pin, not in `redTests`, and a red-sanity check must expect it to
  PASS.

Code: set the field from `crossRunner` beside `cell.Bytes, cell.Allocs = CompareSamples(...)`,
and replace the reason at main.go:262. A cell that FAILs keeps its own reason — the overwrite
is guarded on `VerdictInconclusive`, which is what keeps an allocation failure legible.

Red run — the exit code alone cannot close this gate, because `go test -run <regex>` exits 0
when the regex matches nothing, so a red stage that never wrote the test would pass vacuously:

```
! go test -timeout 2m -run '^TestCrossRunner_AllocsEnforcedBytesInconclusive$' ./cmd/perfgate/ && \
! go test -timeout 2m -run '^TestCrossRunner_CleanRunExitsZero$' ./cmd/perfgate/ && \
go test -timeout 2m -v -run '^TestSameRunner_BytesStillEnforced$' ./cmd/perfgate/ | grep -q -- '--- PASS: TestSameRunner_BytesStillEnforced'
```

Verify: `go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...`

**4. `cmd-verify-others`** — tasks 1.2, 2.3 · prev `cmd-cross-runner`, sharedPkg `cmd/perfgate` · coder `coder`

Writes nothing and takes no seal. Confirms `TestCrossRunner_UnknownIdentityIsNotAMatch`,
`TestCrossRunner_MissingBaselineCellIsInconclusive`, `TestRun_UnpairedComparisonExitsThree`,
`TestCrossRunner_ZeroBaselineBytesFails` and the allowance-spread tests all pass unchanged, and
that a bytes axis left undecided sets neither `anyFail` nor `needsRerun`.
Verify: as chunk 3.

**5. `decision-record`** — tasks 3.1, 3.2 · shard `docs`, parallel · coder `coder`

Amend ADR 0008's `Note (runner comparability)` in place; it currently asserts the opposite.
State **both** mechanisms by which `B/op` is a property of the machine. ADR 0008's threshold
list at lines 21-24 is **not** amended — the note alone carries the exception. Add a `Changed`
entry under `[Unreleased]` naming the observable change, not the implementation.
Verify — `openspec validate` alone cannot fail on an unamended note, so the gate greps:

```
grep -q 'iteration count' docs/adr/0008-consumer-performance-gate.md && \
! grep -q 'Allocation count and allocated bytes stay' docs/adr/0008-consumer-performance-gate.md && \
grep -q 'allocation counts rather than allocated bytes' CHANGELOG.md && \
openspec validate gate-bytes-not-runner-portable --strict
```

**6. `release-verify`** — task 4.1 · prev `cmd-verify-others` · coder `coder`

Runs the floor. **Task 4.2 is ticked by nothing in this run**: it dispatches
`.github/workflows/release.yml` (`workflow_dispatch` at release.yml:16) and is closed
post-merge by recording the dispatch run id.

**Floor:** `make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint`

**Requirements map:** all sixteen SHALL lines of the delta spec map to named tests, six of them
to tests that already exist and must keep passing.

**Plan review:** `zarchitect`, independent of the design author, two rounds, verdict **pass**.
Round 1 returned two blockers, both accepted and fixed: a chunk authoring new tests at its code
stage into a `_test.go` the previous chunk's red stage had sealed, and a task-1.1 site marked
"verify only, no edit" for a task that *is* an edit — which would have produced a vacuous red
that reads as a passing gate. It also confirmed that one red test reached only two of the three
evaluators. Round 1 refuted the concern that an unused-field linter would break the FIELD-FIRST
chunk: the repo's `.golangci.yml` takes v2's standard set, and `unused` treats exported fields
as used.

## Plan appendix

## Plan appendix

```json
{
  "v": 2,
  "change": "gate-bytes-not-runner-portable",
  "baseSha": "d717fe7c0b05baae745ade820dade31104bea372",
  "generatedAt": "2026-09-06T05:21:04.889Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality"
  ],
  "chunks": [
    {
      "id": "axis-field",
      "taskIds": [
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "perfgate-bytes-axis",
      "shard": "axis",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "CellComparison",
          "anchor": "BytesAllowanceBOp float64",
          "change": "Add the field `BytesNotComparable string` to CellComparison, declared and unused, so the red tests compile. Zero value (empty) means the identities match and the bytes bound is ENFORCED, so an unset field fails closed — the opposite polarity from RaceClean, and the reason internal/perfgate/bench_metrics_test.go:80 TestCrossRunner_ZeroBaselineBytesFails keeps its current outcome unchanged. Doc-comment it in the idiom of BytesAllowanceBOp at perfgate.go:97."
        }
      ],
      "contract": {
        "states": [
          "bytes-comparable-within-bound",
          "bytes-comparable-over-bound",
          "bytes-undecided",
          "allocs-decided"
        ],
        "stateDescriptions": {
          "bytes-comparable-within-bound": "BytesNotComparable is empty and the bytes delta is inside the cell's allowance; the bytes axis passes",
          "bytes-comparable-over-bound": "BytesNotComparable is empty and the bytes delta exceeds the allowance; the bytes axis fails",
          "bytes-undecided": "BytesNotComparable is non-empty; the bytes axis is inconclusive regardless of the delta",
          "allocs-decided": "the allocs axis is evaluated on every path, including bytes-undecided"
        },
        "transitions": [
          {
            "input": "BytesNotComparable empty, Bytes.DeltaPct <= 0",
            "state": "bytes-comparable-within-bound",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:297-300"
          },
          {
            "input": "BytesNotComparable empty, Bytes.New-Bytes.Old <= BytesAllowanceBOp",
            "state": "bytes-comparable-within-bound",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:301-303"
          },
          {
            "input": "BytesNotComparable empty, Bytes.New-Bytes.Old > BytesAllowanceBOp",
            "state": "bytes-comparable-over-bound: cell VerdictFail with the existing allowance reason",
            "effect": "forced",
            "evidence": "internal/perfgate/perfgate.go:304-308; delta spec scenario 'Allocated bytes are enforced when the runners match'"
          },
          {
            "input": "BytesNotComparable non-empty, any bytes delta",
            "state": "bytes-undecided",
            "effect": "set",
            "evidence": "delta spec: 'Where the identities differ, the gate SHALL report the bytes axis inconclusive ... and SHALL NOT convert the difference into a pass or a failure'"
          },
          {
            "input": "bytes-undecided and Allocs.DeltaPct > 0",
            "state": "allocs-decided: cell VerdictFail, reason 'allocs increased by N%'",
            "effect": "forced",
            "evidence": "delta spec scenario 'An allocation count regresses across a change of runner'; internal/perfgate/perfgate.go:221-223"
          },
          {
            "input": "bytes-undecided and allocs non-increasing and Latency.Significant false",
            "state": "bytes-undecided: cell VerdictInconclusive, reason joins the bytes reason and the latency reason with '; '",
            "effect": "set",
            "evidence": "internal/perfgate/perfgate.go:224-226 for the existing latency reason; task 2.1"
          },
          {
            "input": "bytes-undecided and allocs non-increasing and latency significant and within tolerance",
            "state": "bytes-undecided: cell VerdictInconclusive carrying only the bytes reason",
            "effect": "set",
            "evidence": "delta spec: an undecided axis SHALL NOT be converted into a pass"
          },
          {
            "input": "BytesNotComparable empty, bytes over allowance and allocs also increased",
            "state": "bytes-comparable-over-bound: cell VerdictFail, reason 'bytes increased ...'",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:218-223; bytes is checked before allocs today and stays first, so internal/perfgate/perfgate_test.go:217-246 and :562-584 keep their reasons"
          },
          {
            "input": "TierStartup with BytesNotComparable non-empty",
            "state": "bytes-undecided: cell VerdictInconclusive; the absolute-overhead block is not reached",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:262-286; the absolute bound at :277 is untouched"
          },
          {
            "input": "TierEngineSensitive under ModeFirstAuthorization",
            "state": "bytes-comparable-within-bound: unchanged; the inline 20% bytes floor is not routed through the new field",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:193-209"
          }
        ],
        "forbidden": [
          "A cell reporting VerdictPass while BytesNotComparable is non-empty -- an undecided axis must never read as a passing one",
          "A bytes INCONCLUSIVE short-circuiting ahead of the allocs check and hiding an allocation-count regression",
          "BytesNotComparable being consulted by evaluateEngineSensitiveImprovement's inline 20%-fewer floor",
          "Any change to nonIncreasing's signature, its allowance arithmetic, or its two reason strings",
          "Any change to a value in internal/perfgate/tiers.json",
          "Rewiring fewer than all three of evaluateNonRegression (perfgate.go:217), evaluateWithinTolerance (:238) and evaluateStartup (:262) — a red covering only two leaves the third enforcing bytes across differing runners"
        ],
        "seeding": {
          "bytes-comparable-within-bound": "Construct CellComparison with BytesNotComparable left unset and pass it to Evaluate, as internal/perfgate/perfgate_test.go already does",
          "bytes-comparable-over-bound": "Same, with Bytes.DeltaPct > 0 and Bytes.New-Bytes.Old above BytesAllowanceBOp; never by calling nonIncreasing or bytesResult directly",
          "bytes-undecided": "Set CellComparison.BytesNotComparable to a non-empty reason and call Evaluate; the field is the only legal way to reach this state",
          "allocs-decided": "Set Allocs.DeltaPct > 0 with Allocs.Significant true on the same cell, alongside a set BytesNotComparable For TierConcurrent, RaceClean must be set true or Evaluate short-circuits to VerdictFail at internal/perfgate/perfgate.go:156 before the bytes axis is reached."
        },
        "identifiers": {
          "CellComparison.BytesNotComparable": "BytesNotComparable string",
          "bytesResult": "func bytesResult(cell CellComparison) Result",
          "exactAxes": "func exactAxes(cell CellComparison) Result",
          "joinReasons": "func joinReasons(first, second string) string",
          "bytes-undecided reason prefix": "bytes not comparable: ",
          "joinReasons separator": "; ",
          "existing bytes failure reason (unchanged)": "%s increased by %.2f%% (+%.0f B/op against a %.0f B/op allowance)",
          "existing latency inconclusive reason (unchanged)": "latency delta not statistically significant",
          "verdict constants": "VerdictPass, VerdictFail, VerdictInconclusive; collapse through Resolve(tier, mode)",
          "red test": "TestEvaluate_BytesNotComparable",
          "red subtests": "\"inconclusive rather than failing\" (evaluateNonRegression), \"allocs still decided\", \"both undecided axes reported\", \"startup tier does not reach the absolute bound\" (evaluateStartup), \"within-tolerance tier is narrowed too\" (evaluateWithinTolerance via TierDataDominated under ModeFirstAuthorization), \"concurrent tier is narrowed too\" (evaluateWithinTolerance via TierConcurrent with RaceClean true)",
          "non-red pin, not in redTests": "The subtest \"unset field enforces the bytes bound\" asserts today's behaviour, which the FIELD-FIRST chunk preserves exactly. It is a non-regression pin authored inside the red stage; a per-subtest red-sanity check must not expect it to fail.",
          "task 2.2 proof split": "The two existing tiers.json tests (TestTierConfig_EveryCellStatesAnAllowance, TestBytesAllowancesAreJustifiedBySpread) prove every cell states an allowance justified by within-run spread. They do not prove no committed number moved; that half of task 2.2 is proven by the diff."
        }
      },
      "redTasks": [],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go build ./... && go test -timeout 2m ./internal/perfgate/... && go vet ./internal/perfgate/... && golangci-lint run ./internal/perfgate/...",
      "coder": "zpatcher"
    },
    {
      "id": "axis-logic",
      "taskIds": [
        "2.1",
        "2.2"
      ],
      "prev": "axis-field",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "perfgate-bytes-axis",
      "shard": "axis",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "evaluateNonRegression",
          "anchor": "func evaluateNonRegression(cell CellComparison, tolerancePct float64) Result {",
          "change": "The nonIncreasing(cell.Bytes, \"bytes\", cell.BytesAllowanceBOp) short-circuit must not decide the cell when the identities differ; the allocs check below it must still decide it. Reason text must distinguish an undecided bytes axis from a passing one."
        },
        {
          "task": "2.1",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "evaluateWithinTolerance",
          "anchor": "func evaluateWithinTolerance(cell CellComparison, tolerancePct float64) Result {",
          "change": "Same bytes-axis narrowing as evaluateNonRegression; this evaluator serves TierDataDominated under first-authorization and TierConcurrent in both modes."
        },
        {
          "task": "2.1",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "evaluateStartup",
          "anchor": "func evaluateStartup(cell CellComparison, mode Mode) Result {",
          "change": "Same bytes-axis narrowing. Note the absolute-overhead branch below also reads cell.Bytes.New (startupMaxBytes), which is a candidate-only absolute figure, not a cross-run comparison — decide explicitly whether it stays."
        },
        {
          "task": "2.1",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "nonIncreasing",
          "anchor": "func nonIncreasing(m MetricResult, label string, allowanceBOp float64) Result {",
          "change": "Only place that reads allowanceBOp. If the narrowing is expressed here rather than at the three call sites, it must not reach the allocs calls, which pass allowanceBOp 0 and stay unconditional."
        },
        {
          "task": "2.1",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "evaluateEngineSensitiveImprovement",
          "anchor": "if cell.Bytes.DeltaPct > -engineSensitiveBytesImprovementPct {",
          "change": "The one bytes check that does not go through nonIncreasing: a 20%-fewer improvement floor. Cross-runner, this floor is judged on the same iteration-count-bearing B/op figure, so state whether it narrows too or stays out of scope."
        },
        {
          "task": "2.2",
          "file": "internal/perfgate/tiers.json",
          "symbol": "bytesAllowanceBOp",
          "anchor": "\"bytesAllowanceBOp\": {",
          "change": "verify only, no edit — every per-cell value stays as committed. The two existing tiers.json tests prove each cell states a spread-justified allowance; that no committed number moved is proven by the diff, not by a test."
        }
      ],
      "contract": {
        "states": [
          "bytes-comparable-within-bound",
          "bytes-comparable-over-bound",
          "bytes-undecided",
          "allocs-decided"
        ],
        "stateDescriptions": {
          "bytes-comparable-within-bound": "BytesNotComparable is empty and the bytes delta is inside the cell's allowance; the bytes axis passes",
          "bytes-comparable-over-bound": "BytesNotComparable is empty and the bytes delta exceeds the allowance; the bytes axis fails",
          "bytes-undecided": "BytesNotComparable is non-empty; the bytes axis is inconclusive regardless of the delta",
          "allocs-decided": "the allocs axis is evaluated on every path, including bytes-undecided"
        },
        "transitions": [
          {
            "input": "BytesNotComparable empty, Bytes.DeltaPct <= 0",
            "state": "bytes-comparable-within-bound",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:297-300"
          },
          {
            "input": "BytesNotComparable empty, Bytes.New-Bytes.Old <= BytesAllowanceBOp",
            "state": "bytes-comparable-within-bound",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:301-303"
          },
          {
            "input": "BytesNotComparable empty, Bytes.New-Bytes.Old > BytesAllowanceBOp",
            "state": "bytes-comparable-over-bound: cell VerdictFail with the existing allowance reason",
            "effect": "forced",
            "evidence": "internal/perfgate/perfgate.go:304-308; delta spec scenario 'Allocated bytes are enforced when the runners match'"
          },
          {
            "input": "BytesNotComparable non-empty, any bytes delta",
            "state": "bytes-undecided",
            "effect": "set",
            "evidence": "delta spec: 'Where the identities differ, the gate SHALL report the bytes axis inconclusive ... and SHALL NOT convert the difference into a pass or a failure'"
          },
          {
            "input": "bytes-undecided and Allocs.DeltaPct > 0",
            "state": "allocs-decided: cell VerdictFail, reason 'allocs increased by N%'",
            "effect": "forced",
            "evidence": "delta spec scenario 'An allocation count regresses across a change of runner'; internal/perfgate/perfgate.go:221-223"
          },
          {
            "input": "bytes-undecided and allocs non-increasing and Latency.Significant false",
            "state": "bytes-undecided: cell VerdictInconclusive, reason joins the bytes reason and the latency reason with '; '",
            "effect": "set",
            "evidence": "internal/perfgate/perfgate.go:224-226 for the existing latency reason; task 2.1"
          },
          {
            "input": "bytes-undecided and allocs non-increasing and latency significant and within tolerance",
            "state": "bytes-undecided: cell VerdictInconclusive carrying only the bytes reason",
            "effect": "set",
            "evidence": "delta spec: an undecided axis SHALL NOT be converted into a pass"
          },
          {
            "input": "BytesNotComparable empty, bytes over allowance and allocs also increased",
            "state": "bytes-comparable-over-bound: cell VerdictFail, reason 'bytes increased ...'",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:218-223; bytes is checked before allocs today and stays first, so internal/perfgate/perfgate_test.go:217-246 and :562-584 keep their reasons"
          },
          {
            "input": "TierStartup with BytesNotComparable non-empty",
            "state": "bytes-undecided: cell VerdictInconclusive; the absolute-overhead block is not reached",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:262-286; the absolute bound at :277 is untouched"
          },
          {
            "input": "TierEngineSensitive under ModeFirstAuthorization",
            "state": "bytes-comparable-within-bound: unchanged; the inline 20% bytes floor is not routed through the new field",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:193-209"
          }
        ],
        "forbidden": [
          "A cell reporting VerdictPass while BytesNotComparable is non-empty -- an undecided axis must never read as a passing one",
          "A bytes INCONCLUSIVE short-circuiting ahead of the allocs check and hiding an allocation-count regression",
          "BytesNotComparable being consulted by evaluateEngineSensitiveImprovement's inline 20%-fewer floor",
          "Any change to nonIncreasing's signature, its allowance arithmetic, or its two reason strings",
          "Any change to a value in internal/perfgate/tiers.json",
          "Rewiring fewer than all three of evaluateNonRegression (perfgate.go:217), evaluateWithinTolerance (:238) and evaluateStartup (:262) — a red covering only two leaves the third enforcing bytes across differing runners"
        ],
        "seeding": {
          "bytes-comparable-within-bound": "Construct CellComparison with BytesNotComparable left unset and pass it to Evaluate, as internal/perfgate/perfgate_test.go already does",
          "bytes-comparable-over-bound": "Same, with Bytes.DeltaPct > 0 and Bytes.New-Bytes.Old above BytesAllowanceBOp; never by calling nonIncreasing or bytesResult directly",
          "bytes-undecided": "Set CellComparison.BytesNotComparable to a non-empty reason and call Evaluate; the field is the only legal way to reach this state",
          "allocs-decided": "Set Allocs.DeltaPct > 0 with Allocs.Significant true on the same cell, alongside a set BytesNotComparable For TierConcurrent, RaceClean must be set true or Evaluate short-circuits to VerdictFail at internal/perfgate/perfgate.go:156 before the bytes axis is reached."
        },
        "identifiers": {
          "CellComparison.BytesNotComparable": "BytesNotComparable string",
          "bytesResult": "func bytesResult(cell CellComparison) Result",
          "exactAxes": "func exactAxes(cell CellComparison) Result",
          "joinReasons": "func joinReasons(first, second string) string",
          "bytes-undecided reason prefix": "bytes not comparable: ",
          "joinReasons separator": "; ",
          "existing bytes failure reason (unchanged)": "%s increased by %.2f%% (+%.0f B/op against a %.0f B/op allowance)",
          "existing latency inconclusive reason (unchanged)": "latency delta not statistically significant",
          "verdict constants": "VerdictPass, VerdictFail, VerdictInconclusive; collapse through Resolve(tier, mode)",
          "red test": "TestEvaluate_BytesNotComparable",
          "red subtests": "\"inconclusive rather than failing\" (evaluateNonRegression), \"allocs still decided\", \"both undecided axes reported\", \"startup tier does not reach the absolute bound\" (evaluateStartup), \"within-tolerance tier is narrowed too\" (evaluateWithinTolerance via TierDataDominated under ModeFirstAuthorization), \"concurrent tier is narrowed too\" (evaluateWithinTolerance via TierConcurrent with RaceClean true)",
          "non-red pin, not in redTests": "The subtest \"unset field enforces the bytes bound\" asserts today's behaviour, which the FIELD-FIRST chunk preserves exactly. It is a non-regression pin authored inside the red stage; a per-subtest red-sanity check must not expect it to fail.",
          "task 2.2 proof split": "The two existing tiers.json tests (TestTierConfig_EveryCellStatesAnAllowance, TestBytesAllowancesAreJustifiedBySpread) prove every cell states an allowance justified by within-run spread. They do not prove no committed number moved; that half of task 2.2 is proven by the diff.",
          "red gate is not the exit code": "go test -run <regex> exits 0 when the regex matches nothing, so a red stage that only checks the exit code passes vacuously against a test it never wrote. redRun negates each red test's own run, which turns a missing test into a red-gate failure, and greps --- PASS for the pin."
        }
      },
      "redTasks": [
        "2.1"
      ],
      "codeTasks": [
        "2.1",
        "2.2"
      ],
      "redTests": [
        "TestEvaluate_BytesNotComparable"
      ],
      "redRun": "! go test -timeout 2m -run '^TestEvaluate_BytesNotComparable$' ./internal/perfgate/",
      "verify": "go test -timeout 2m ./internal/perfgate/... && go vet ./internal/perfgate/... && golangci-lint run ./internal/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "cmd-cross-runner",
      "taskIds": [
        "1.1",
        "2.4"
      ],
      "prev": "axis-logic",
      "sharedPkg": "cmd/perfgate",
      "parallel": false,
      "seam": "perfgate-cmd-cross-runner",
      "shard": "cli",
      "pkgDirs": [
        "cmd/perfgate"
      ],
      "pkgs": [
        "./cmd/perfgate"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "cmd/perfgate/main_test.go",
          "symbol": "TestCrossRunner_LatencyInconclusiveBytesEnforced",
          "anchor": "func TestCrossRunner_LatencyInconclusiveBytesEnforced(t *testing.T) {",
          "change": "(modify) Rename this test to TestCrossRunner_AllocsEnforcedBytesInconclusive at :87 and rewrite its doc comment at :82-86, which still states the retired rule. Keep the subtest \"latency inconclusive naming both runners\" exactly as it is. Replace the subtest \"bytes still enforced across differing runners\" (:107-126) with \"bytes inconclusive across differing runners\" asserting INCONCLUSIVE on the 13 GoldsetParse/* cells, and add \"allocation counts still enforced across differing runners\" asserting FAIL with a reason naming the allocs axis on the 13 Goldset/* cells and exit 1. Measured on the committed corpora for the swapped pair: every Goldset/* cell gains exactly one allocation, every GoldsetParse/* cell holds allocs identical and moves only on bytes."
        },
        {
          "task": "2.4",
          "file": "cmd/perfgate/main.go",
          "symbol": "evaluate",
          "anchor": "cell.Bytes, cell.Allocs = perfgate.CompareSamples(baseline, newMetrics[name])",
          "change": "Populate the new cell field from crossRunner here, next to cell.RaceClean and cell.BytesAllowanceBOp."
        },
        {
          "task": "2.4",
          "file": "cmd/perfgate/main.go",
          "symbol": "evaluate (cross-runner reason branch)",
          "anchor": "res.Reason = fmt.Sprintf(\"latency not comparable: baseline ran on %s, candidate on %s\", oldID, newID)",
          "change": "This rewrite currently overwrites every cross-runner INCONCLUSIVE reason with a latency-only sentence. It must name the bytes axis too when bytes is what was left undecided, and must not overwrite a reason that already reports a decided allocs failure."
        },
        {
          "task": "2.4",
          "file": "cmd/perfgate/main.go",
          "symbol": "crossRunner",
          "anchor": "crossRunner := !sameRunner(oldID, newID)",
          "change": "The single source of the identity comparison; the value that reaches the cell field originates here."
        },
        {
          "task": "1.1",
          "file": "cmd/perfgate/main_test.go",
          "symbol": "TestCrossRunner_CleanRunExitsZero",
          "anchor": "func withoutCPULines(t *testing.T, path string) string {",
          "change": "New test beside withoutCPULines, whose t.TempDir() copy idiom the new keepingCells helper follows. keepingCells copies the goos:/goarch:/pkg:/cpu: preamble verbatim and keeps only the Benchmark lines matching a prefix. Filter the Intel candidate to its GoldsetParse/ cells: all 13 are data-dominated with allocs identical, so every cell collapses through Resolve to PASS and the run exits 0."
        },
        {
          "task": "1.1",
          "file": "cmd/perfgate/main_test.go",
          "symbol": "TestSameRunner_BytesStillEnforced",
          "anchor": "func swapBenchstat(t *testing.T, fn func(oldPath, newPath string) ([]byte, error)) {",
          "change": "New test beside swapBenchstat, which it uses: -old profile-30637802780/bench-vm.txt -candidate profile-30637802780/bench-evaluator.txt with the committed benchstat.csv injected. runGate must NOT be used — its benchstat stub fails the test if reached. Assert the exact 13-cell FAIL set on bytes (the 12 Goldset/* other than guard-nil, plus GoldsetCall/call-boundary-2 at 0 -> 768 B/op) and the 14 clean cells (Goldset/guard-nil-2, which decreases 1129 -> 1128, and all 13 GoldsetParse/* at exactly +0). Never assert uniformity. This test is green before and after the narrowing: it is a characterization pin, not a red assertion."
        }
      ],
      "contract": {
        "states": [
          "cross-runner",
          "same-runner",
          "cross-runner-clean",
          "cross-runner-allocs-regressed"
        ],
        "stateDescriptions": {
          "cross-runner": "sameRunner(oldID, newID) is false, so every cell carries BytesNotComparable",
          "same-runner": "sameRunner is true, so BytesNotComparable stays empty and benchstat runs as today",
          "cross-runner-clean": "cross-runner with no cell failing on allocs or tier lookup; the run exits 0",
          "cross-runner-allocs-regressed": "cross-runner with at least one cell's allocation count increased; the run exits 1"
        },
        "transitions": [
          {
            "input": "two raw files with differing runner identities",
            "state": "cross-runner",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:197 crossRunner := !sameRunner(oldID, newID)"
          },
          {
            "input": "cross-runner cell, allocs identical, bytes higher",
            "state": "cross-runner: cell INCONCLUSIVE, reason names both runners, no failure",
            "effect": "set",
            "evidence": "delta spec scenario 'Allocated bytes move across a change of runner with the allocation count unchanged'; cmd/perfgate/main.go:254-265"
          },
          {
            "input": "cross-runner cell, allocation count increased",
            "state": "cross-runner-allocs-regressed: cell FAIL, reason 'allocs increased by N%', not overwritten by the runner text",
            "effect": "forced",
            "evidence": "cmd/perfgate/main.go:254 guards the overwrite on VerdictInconclusive, so a FAIL keeps its own reason"
          },
          {
            "input": "cross-runner, every cell inconclusive",
            "state": "cross-runner-clean: exitPass",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:241-243 and :263-265 collapse through Resolve; under non-regression every tier returns VerdictPass"
          },
          {
            "input": "same-runner pair whose candidate allocates more bytes than its allowance",
            "state": "same-runner: cell FAIL, reason 'bytes increased', exit 1",
            "effect": "no-op",
            "evidence": "delta spec scenario 'Allocated bytes are enforced when the runners match'"
          },
          {
            "input": "cross-runner cell absent from the baseline",
            "state": "cross-runner: cell INCONCLUSIVE, reason 'no baseline figure for this cell'",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:235-246; unchanged by this change"
          },
          {
            "input": "benchstat declines to pair a same-runner pair",
            "state": "same-runner: exitNotComparable 3 with the pairing refusal as the reason, no retry",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:206-212; delta spec scenario 'An incomparable pair is never forced into a comparison'; pinned by TestRun_UnpairedComparisonExitsThree"
          },
          {
            "input": "an unknown runner identity on either side",
            "state": "cross-runner",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:308-310 sameRunner; pinned by TestCrossRunner_UnknownIdentityIsNotAMatch, whose uniform-INCONCLUSIVE and exit-0 assertions stay true"
          }
        ],
        "forbidden": [
          "Setting BytesNotComparable on a same-runner pair",
          "Stripping, rewriting or normalising goos/goarch/pkg/cpu lines in a committed corpus, or re-running benchstat after a refusal to pair",
          "Editing any file under internal/perfgate/testdata; a filtered fixture is written to t.TempDir(), following cmd/perfgate/main_test.go:255-274",
          "Letting the cross-runner reason overwrite an allocs FAIL reason",
          "Raising needsRerun (exit 2) for a cross-runner cell",
          "Asserting a uniform verdict over all cells of the same-runner fixture; its failing set is a stated partition, not the whole cell set",
          "Authoring any test under cmd/perfgate outside this seam's red stage: that stage seals every _test.go under cmd/perfgate, so a later chunk cannot add or amend one"
        ],
        "seeding": {
          "cross-runner": "-old internal/perfgate/testdata/profile-30637802780/bench-vm.txt -candidate internal/perfgate/testdata/profile-30614184386/bench-vm.txt (roles swapped relative to the first subtest), through the existing runGate helper at cmd/perfgate/main_test.go:280",
          "cross-runner-clean": "The same swapped pair with the candidate filtered to its GoldsetParse/ cells only, written to t.TempDir() by a new keepingCells helper modelled on withoutCPULines (cmd/perfgate/main_test.go:255-274). keepingCells MUST copy the goos:/goarch:/pkg:/cpu: preamble lines verbatim and filter only Benchmark result lines: a filtered file without its preamble reads as an unknown identity and turns the test into a different case.",
          "same-runner": "-old internal/perfgate/testdata/profile-30637802780/bench-vm.txt -candidate internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt, with swapBenchstat (cmd/perfgate/main_test.go:64) returning the committed profile-30637802780/benchstat.csv; runGate must NOT be used here because it fails the test if benchstat is reached",
          "cross-runner-allocs-regressed": "The unfiltered swapped pair: its 13 Goldset/ cells each gain exactly one allocation"
        },
        "identifiers": {
          "cross-runner reason format string": "latency and bytes not comparable: baseline ran on %s, candidate on %s",
          "BytesNotComparable value set by main.go": "fmt.Sprintf(\"baseline ran on %s, candidate on %s\", oldID, newID)",
          "renamed test": "TestCrossRunner_AllocsEnforcedBytesInconclusive",
          "renamed-from": "TestCrossRunner_LatencyInconclusiveBytesEnforced (cmd/perfgate/main_test.go:87)",
          "subtest kept": "\"latency inconclusive naming both runners\"",
          "subtest replacing \"bytes still enforced across differing runners\"": "\"bytes inconclusive across differing runners\"",
          "subtest added": "\"allocation counts still enforced across differing runners\"",
          "new test": "TestCrossRunner_CleanRunExitsZero",
          "new test 2": "TestSameRunner_BytesStillEnforced",
          "new test helper": "func keepingCells(t *testing.T, path, prefix string) string -- copies the goos:/goarch:/pkg:/cpu: preamble verbatim, keeps only Benchmark lines whose trimmed name starts with prefix, writes to t.TempDir()",
          "existing helpers to reuse": "runGate, swapBenchstat, reportLines, verdictsFor, uniformVerdicts, withoutCell, candidateCells, identityOf (cmd/perfgate/main_test.go:280, 64, 173, 214, 225, 233, 195, 243)",
          "exit codes": "exitPass 0, exitFail 1, exitNeedsRerun 2, exitNotComparable 3 (cmd/perfgate/main.go:28-33)",
          "engine-sensitive first-authorization note": "The cross-runner reason overwrite at cmd/perfgate/main.go:262 also fires on a TierEngineSensitive first-authorization cell that stopped at the latency-significance gate. First authorization compares two arms of one run on one runner, so release.yml does not produce a cross-runner first-authorization pair; the wording stays accurate either way because bytes genuinely is not comparable there.",
          "package reason is not observable through the command": "cmd/perfgate/main.go:254-262 overwrites the reason of every cross-runner INCONCLUSIVE cell, so the package-built \"bytes not comparable: \" prefix and joinReasons can only be asserted by the internal/perfgate unit test, never through the gate binary. The two mechanisms do not compose and are not meant to.",
          "non-red pin, not in redTests": "TestSameRunner_BytesStillEnforced is authored in this red stage but is NOT in redTests: it asserts same-runner behaviour the narrowing preserves exactly, so it is green before and after. A red-sanity check must expect it to PASS, never to fail.",
          "red gate is not the exit code": "go test -run <regex> exits 0 when the regex matches nothing, so a red stage that only checks the exit code passes vacuously against a test it never wrote. redRun negates each red test's own run, which turns a missing test into a red-gate failure, and greps --- PASS for the pin."
        }
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.4"
      ],
      "redTests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive",
        "TestCrossRunner_CleanRunExitsZero"
      ],
      "redRun": "! go test -timeout 2m -run '^TestCrossRunner_AllocsEnforcedBytesInconclusive$' ./cmd/perfgate/ && ! go test -timeout 2m -run '^TestCrossRunner_CleanRunExitsZero$' ./cmd/perfgate/ && go test -timeout 2m -v -run '^TestSameRunner_BytesStillEnforced$' ./cmd/perfgate/ | grep -q -- '--- PASS: TestSameRunner_BytesStillEnforced'",
      "verify": "go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "cmd-verify-others",
      "taskIds": [
        "1.2",
        "2.3"
      ],
      "prev": "cmd-cross-runner",
      "sharedPkg": "cmd/perfgate",
      "parallel": false,
      "seam": "perfgate-cmd-cross-runner",
      "shard": "cli",
      "pkgDirs": [
        "cmd/perfgate"
      ],
      "pkgs": [
        "./cmd/perfgate"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "cmd/perfgate/main_test.go",
          "symbol": "TestCrossRunner_UnknownIdentityIsNotAMatch",
          "anchor": "func TestCrossRunner_UnknownIdentityIsNotAMatch(t *testing.T) {",
          "change": "verify only, no edit — this test and TestCrossRunner_MissingBaselineCellIsInconclusive, TestRun_UnpairedComparisonExitsThree, TestCrossRunner_ZeroBaselineBytesFails and the allowance-spread tests must all pass unchanged."
        },
        {
          "task": "2.3",
          "file": "cmd/perfgate/main.go",
          "symbol": "evaluate exit precedence",
          "anchor": "case needsRerun:",
          "change": "verify only, no edit — a bytes axis left undecided sets neither anyFail nor needsRerun, so an otherwise clean cross-runner run keeps returning exitPass. Proven by TestCrossRunner_CleanRunExitsZero, authored in the previous chunk."
        }
      ],
      "contract": {
        "states": [
          "cross-runner",
          "same-runner",
          "cross-runner-clean",
          "cross-runner-allocs-regressed"
        ],
        "stateDescriptions": {
          "cross-runner": "sameRunner(oldID, newID) is false, so every cell carries BytesNotComparable",
          "same-runner": "sameRunner is true, so BytesNotComparable stays empty and benchstat runs as today",
          "cross-runner-clean": "cross-runner with no cell failing on allocs or tier lookup; the run exits 0",
          "cross-runner-allocs-regressed": "cross-runner with at least one cell's allocation count increased; the run exits 1"
        },
        "transitions": [
          {
            "input": "two raw files with differing runner identities",
            "state": "cross-runner",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:197 crossRunner := !sameRunner(oldID, newID)"
          },
          {
            "input": "cross-runner cell, allocs identical, bytes higher",
            "state": "cross-runner: cell INCONCLUSIVE, reason names both runners, no failure",
            "effect": "set",
            "evidence": "delta spec scenario 'Allocated bytes move across a change of runner with the allocation count unchanged'; cmd/perfgate/main.go:254-265"
          },
          {
            "input": "cross-runner cell, allocation count increased",
            "state": "cross-runner-allocs-regressed: cell FAIL, reason 'allocs increased by N%', not overwritten by the runner text",
            "effect": "forced",
            "evidence": "cmd/perfgate/main.go:254 guards the overwrite on VerdictInconclusive, so a FAIL keeps its own reason"
          },
          {
            "input": "cross-runner, every cell inconclusive",
            "state": "cross-runner-clean: exitPass",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:241-243 and :263-265 collapse through Resolve; under non-regression every tier returns VerdictPass"
          },
          {
            "input": "same-runner pair whose candidate allocates more bytes than its allowance",
            "state": "same-runner: cell FAIL, reason 'bytes increased', exit 1",
            "effect": "no-op",
            "evidence": "delta spec scenario 'Allocated bytes are enforced when the runners match'"
          },
          {
            "input": "cross-runner cell absent from the baseline",
            "state": "cross-runner: cell INCONCLUSIVE, reason 'no baseline figure for this cell'",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:235-246; unchanged by this change"
          },
          {
            "input": "benchstat declines to pair a same-runner pair",
            "state": "same-runner: exitNotComparable 3 with the pairing refusal as the reason, no retry",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:206-212; delta spec scenario 'An incomparable pair is never forced into a comparison'; pinned by TestRun_UnpairedComparisonExitsThree"
          },
          {
            "input": "an unknown runner identity on either side",
            "state": "cross-runner",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:308-310 sameRunner; pinned by TestCrossRunner_UnknownIdentityIsNotAMatch, whose uniform-INCONCLUSIVE and exit-0 assertions stay true"
          }
        ],
        "forbidden": [
          "Setting BytesNotComparable on a same-runner pair",
          "Stripping, rewriting or normalising goos/goarch/pkg/cpu lines in a committed corpus, or re-running benchstat after a refusal to pair",
          "Editing any file under internal/perfgate/testdata; a filtered fixture is written to t.TempDir(), following cmd/perfgate/main_test.go:255-274",
          "Letting the cross-runner reason overwrite an allocs FAIL reason",
          "Raising needsRerun (exit 2) for a cross-runner cell",
          "Asserting a uniform verdict over all cells of the same-runner fixture; its failing set is a stated partition, not the whole cell set",
          "Authoring any test under cmd/perfgate outside this seam's red stage: that stage seals every _test.go under cmd/perfgate, so a later chunk cannot add or amend one"
        ],
        "seeding": {
          "cross-runner": "-old internal/perfgate/testdata/profile-30637802780/bench-vm.txt -candidate internal/perfgate/testdata/profile-30614184386/bench-vm.txt (roles swapped relative to the first subtest), through the existing runGate helper at cmd/perfgate/main_test.go:280",
          "cross-runner-clean": "The same swapped pair with the candidate filtered to its GoldsetParse/ cells only, written to t.TempDir() by a new keepingCells helper modelled on withoutCPULines (cmd/perfgate/main_test.go:255-274). keepingCells MUST copy the goos:/goarch:/pkg:/cpu: preamble lines verbatim and filter only Benchmark result lines: a filtered file without its preamble reads as an unknown identity and turns the test into a different case.",
          "same-runner": "-old internal/perfgate/testdata/profile-30637802780/bench-vm.txt -candidate internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt, with swapBenchstat (cmd/perfgate/main_test.go:64) returning the committed profile-30637802780/benchstat.csv; runGate must NOT be used here because it fails the test if benchstat is reached",
          "cross-runner-allocs-regressed": "The unfiltered swapped pair: its 13 Goldset/ cells each gain exactly one allocation"
        },
        "identifiers": {
          "cross-runner reason format string": "latency and bytes not comparable: baseline ran on %s, candidate on %s",
          "BytesNotComparable value set by main.go": "fmt.Sprintf(\"baseline ran on %s, candidate on %s\", oldID, newID)",
          "renamed test": "TestCrossRunner_AllocsEnforcedBytesInconclusive",
          "renamed-from": "TestCrossRunner_LatencyInconclusiveBytesEnforced (cmd/perfgate/main_test.go:87)",
          "subtest kept": "\"latency inconclusive naming both runners\"",
          "subtest replacing \"bytes still enforced across differing runners\"": "\"bytes inconclusive across differing runners\"",
          "subtest added": "\"allocation counts still enforced across differing runners\"",
          "new test": "TestCrossRunner_CleanRunExitsZero",
          "new test 2": "TestSameRunner_BytesStillEnforced",
          "new test helper": "func keepingCells(t *testing.T, path, prefix string) string -- copies the goos:/goarch:/pkg:/cpu: preamble verbatim, keeps only Benchmark lines whose trimmed name starts with prefix, writes to t.TempDir()",
          "existing helpers to reuse": "runGate, swapBenchstat, reportLines, verdictsFor, uniformVerdicts, withoutCell, candidateCells, identityOf (cmd/perfgate/main_test.go:280, 64, 173, 214, 225, 233, 195, 243)",
          "exit codes": "exitPass 0, exitFail 1, exitNeedsRerun 2, exitNotComparable 3 (cmd/perfgate/main.go:28-33)",
          "engine-sensitive first-authorization note": "The cross-runner reason overwrite at cmd/perfgate/main.go:262 also fires on a TierEngineSensitive first-authorization cell that stopped at the latency-significance gate. First authorization compares two arms of one run on one runner, so release.yml does not produce a cross-runner first-authorization pair; the wording stays accurate either way because bytes genuinely is not comparable there.",
          "package reason is not observable through the command": "cmd/perfgate/main.go:254-262 overwrites the reason of every cross-runner INCONCLUSIVE cell, so the package-built \"bytes not comparable: \" prefix and joinReasons can only be asserted by the internal/perfgate unit test, never through the gate binary. The two mechanisms do not compose and are not meant to."
        }
      },
      "redTasks": [],
      "codeTasks": [
        "1.2",
        "2.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "coder"
    },
    {
      "id": "decision-record",
      "taskIds": [
        "3.1",
        "3.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "gate-decision-record",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "docs/adr/0008-consumer-performance-gate.md",
          "symbol": "Note (runner comparability)",
          "anchor": "Note (runner comparability): a latency conclusion is only sound when the",
          "change": "Amend this note in place. Its current text asserts the opposite of the change: \"Allocation count and allocated bytes stay enforced regardless of runner identity\". Rewrite so allocation count survives a change of runner and allocated bytes does not, and state BOTH mechanisms by which B/op is a property of the machine: the iteration-count denominator (run 33985766146, iterations 2.8x apart at one BENCHTIME) and per-allocation sizing (the committed corpora, whose iteration counts are within ~10% while every GoldsetParse/* cell moves +16..+288 B/op with allocs/op identical). ADR 0008 lines 21-24 are NOT amended."
        },
        {
          "task": "3.2",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Add the entry under a Changed heading inside [Unreleased] (the section is currently empty): a release measured against a baseline from a different runner is gated on allocation counts rather than allocated bytes."
        }
      ],
      "contract": {
        "states": [
          "adr-amended",
          "changelog-recorded"
        ],
        "stateDescriptions": {
          "adr-amended": "ADR 0008 states which measurement axis survives a change of runner and why",
          "changelog-recorded": "CHANGELOG.md [Unreleased] names the observable gate change"
        },
        "transitions": [
          {
            "input": "ADR 0008 lines 38-44 claim 'Allocation count and allocated bytes stay enforced regardless of runner identity'",
            "state": "adr-amended: allocation count survives a change of runner because it is an exact per-op integer with no iteration-count term; allocated bytes do not, so B/op is reported inconclusive across differing identities and enforced against the cell's stated allowance whenever they match",
            "effect": "forced",
            "evidence": "docs/adr/0008-consumer-performance-gate.md:34-44 against the delta spec's MODIFIED requirement"
          },
          {
            "input": "an amendment naming only the iteration denominator as the mechanism",
            "state": "adr-amended: rejected -- the note states both mechanisms",
            "effect": "forced",
            "evidence": "On the committed pair the iteration counts are within ~10% (Goldset/counter-closure-2 runs 28153 on AMD against 28812 on Intel) yet every GoldsetParse/ cell moves +16 to +288 B/op with allocs/op identical: per-allocation sizing differs across machines. The 2.8x denominator effect the proposal cites comes from run 33985766146, a different pair. Both mechanisms make B/op a property of the machine; a note naming only the denominator would be narrower than the repository's own fixture."
          },
          {
            "input": "CHANGELOG.md [Unreleased] at line 8 is empty",
            "state": "changelog-recorded: carries a Changed entry stating that a release measured against a baseline from a different runner is gated on allocation counts rather than allocated bytes",
            "effect": "set",
            "evidence": "CHANGELOG.md:8; task 3.2"
          }
        ],
        "forbidden": [
          "Amending ADR 0008's threshold list at lines 21-24 or its per-cell allowance derivation at lines 65-77 -- the allowances are unchanged in value and still enforced on matching runners",
          "An ADR note attributing the movement solely to the iteration count",
          "A CHANGELOG entry that describes the implementation (a new field, a helper) rather than the observable gate behaviour"
        ],
        "seeding": {
          "adr-amended": "Edit the existing 'Note (runner comparability)' paragraph in place; do not add a competing note",
          "changelog-recorded": "Add a '### Changed' bullet under the existing '## [Unreleased]' heading"
        },
        "identifiers": {
          "ADR anchor": "docs/adr/0008-consumer-performance-gate.md:34 'Note (runner comparability):'",
          "CHANGELOG anchor": "CHANGELOG.md:8 '## [Unreleased]'",
          "CHANGELOG section": "### Changed",
          "threshold lines stay as they are": "ADR 0008 lines 21-24 are not amended. The runner-comparability note alone carries the exception; no per-tier pointer is added. This is a ruling, not a question for the coder."
        }
      },
      "redTasks": [],
      "codeTasks": [
        "3.1",
        "3.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "grep -q 'iteration count' docs/adr/0008-consumer-performance-gate.md && ! grep -q 'Allocation count and allocated bytes stay' docs/adr/0008-consumer-performance-gate.md && grep -q 'allocation counts rather than allocated bytes' CHANGELOG.md && openspec validate gate-bytes-not-runner-portable --strict",
      "coder": "coder"
    },
    {
      "id": "release-verify",
      "taskIds": [
        "4.1"
      ],
      "prev": "cmd-verify-others",
      "sharedPkg": "cmd/perfgate",
      "parallel": false,
      "seam": "release-verification",
      "shard": "cli",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "4.1",
          "file": "Makefile",
          "symbol": "test / lint targets",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "verify only, no edit — `make test` (go test -timeout 2m ./...) and `make lint` (golangci-lint run) are the wrappers. There is no race target: the race suite over core, plugins and runtime is a raw run and must carry its own -timeout."
        },
        {
          "task": "4.2",
          "file": ".github/workflows/release.yml",
          "symbol": "perfgate invocation",
          "anchor": "run: go build -o bin/perfgate ./cmd/perfgate",
          "change": "verify only, no edit — the gate is built as a binary so exit 2 and exit 3 stay distinguishable. Task 4.2 dispatches this workflow (workflow_dispatch at release.yml:16) and has NO local execution path: the implementation run ticks 4.1 only and reports 4.2 as outstanding, closed post-merge by recording the dispatch run id."
        }
      ],
      "contract": {
        "states": [
          "floor-clean",
          "gate-dispatched"
        ],
        "stateDescriptions": {
          "floor-clean": "the repository suite, the race suite over core/plugins/runtime, go vet and the linter all exit successfully",
          "gate-dispatched": "the gate has run in CI against the release candidate tree and its reported behaviour has been read"
        },
        "transitions": [
          {
            "input": "the floor command",
            "state": "floor-clean",
            "effect": "forced",
            "evidence": "task 4.1; Makefile:1-23 defines test (go test -timeout 2m ./...) and lint (golangci-lint run)"
          },
          {
            "input": "a workflow dispatch of the gate against the release candidate tree",
            "state": "gate-dispatched: the run reports non-regression, states the runner comparison, reports the bytes axis inconclusive on the cross-runner pair and exits 0",
            "effect": "forced",
            "evidence": "task 4.2; .github/workflows/release.yml guards 'Store VM baseline on the authorized release' on an implicit success()"
          }
        ],
        "forbidden": [
          "Marking 4.2 complete from a local run: no local invocation exercises the workflow's baseline-storage guard",
          "Narrowing the race suite below core, plugins and runtime, or dropping the -timeout limits"
        ],
        "seeding": {
          "floor-clean": "Run the packet's floor command from the repository root",
          "gate-dispatched": "Dispatch the release workflow; the run id and its verdict report are the evidence"
        },
        "identifiers": {
          "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
          "workflow": ".github/workflows/release.yml",
          "baseline step": "Store VM baseline on the authorized release",
          "4.2 is not closable in the run": "Task 4.2 dispatches .github/workflows/release.yml (workflow_dispatch at release.yml:16) and has no local execution path. It is closed post-merge by recording the dispatch run id; the implementation run ticks 4.1 only and reports 4.2 as outstanding."
        }
      },
      "redTasks": [],
      "codeTasks": [
        "4.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "perfgate-bytes-axis",
      "summary": "DECLARE-FIRST: CellComparison.BytesNotComparable must land as a bare field declaration before the red tests compile; then the red tests; then bytesResult/exactAxes/joinReasons and the three evaluator rewires. An empty field reproduces today's behaviour exactly, which is what makes the narrowing additive.",
      "tasks": [
        "2.1",
        "2.2"
      ],
      "contract": {
        "states": [
          "bytes-comparable-within-bound",
          "bytes-comparable-over-bound",
          "bytes-undecided",
          "allocs-decided"
        ],
        "stateDescriptions": {
          "bytes-comparable-within-bound": "BytesNotComparable is empty and the bytes delta is inside the cell's allowance; the bytes axis passes",
          "bytes-comparable-over-bound": "BytesNotComparable is empty and the bytes delta exceeds the allowance; the bytes axis fails",
          "bytes-undecided": "BytesNotComparable is non-empty; the bytes axis is inconclusive regardless of the delta",
          "allocs-decided": "the allocs axis is evaluated on every path, including bytes-undecided"
        },
        "transitions": [
          {
            "input": "BytesNotComparable empty, Bytes.DeltaPct <= 0",
            "state": "bytes-comparable-within-bound",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:297-300"
          },
          {
            "input": "BytesNotComparable empty, Bytes.New-Bytes.Old <= BytesAllowanceBOp",
            "state": "bytes-comparable-within-bound",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:301-303"
          },
          {
            "input": "BytesNotComparable empty, Bytes.New-Bytes.Old > BytesAllowanceBOp",
            "state": "bytes-comparable-over-bound: cell VerdictFail with the existing allowance reason",
            "effect": "forced",
            "evidence": "internal/perfgate/perfgate.go:304-308; delta spec scenario 'Allocated bytes are enforced when the runners match'"
          },
          {
            "input": "BytesNotComparable non-empty, any bytes delta",
            "state": "bytes-undecided",
            "effect": "set",
            "evidence": "delta spec: 'Where the identities differ, the gate SHALL report the bytes axis inconclusive ... and SHALL NOT convert the difference into a pass or a failure'"
          },
          {
            "input": "bytes-undecided and Allocs.DeltaPct > 0",
            "state": "allocs-decided: cell VerdictFail, reason 'allocs increased by N%'",
            "effect": "forced",
            "evidence": "delta spec scenario 'An allocation count regresses across a change of runner'; internal/perfgate/perfgate.go:221-223"
          },
          {
            "input": "bytes-undecided and allocs non-increasing and Latency.Significant false",
            "state": "bytes-undecided: cell VerdictInconclusive, reason joins the bytes reason and the latency reason with '; '",
            "effect": "set",
            "evidence": "internal/perfgate/perfgate.go:224-226 for the existing latency reason; task 2.1"
          },
          {
            "input": "bytes-undecided and allocs non-increasing and latency significant and within tolerance",
            "state": "bytes-undecided: cell VerdictInconclusive carrying only the bytes reason",
            "effect": "set",
            "evidence": "delta spec: an undecided axis SHALL NOT be converted into a pass"
          },
          {
            "input": "BytesNotComparable empty, bytes over allowance and allocs also increased",
            "state": "bytes-comparable-over-bound: cell VerdictFail, reason 'bytes increased ...'",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:218-223; bytes is checked before allocs today and stays first, so internal/perfgate/perfgate_test.go:217-246 and :562-584 keep their reasons"
          },
          {
            "input": "TierStartup with BytesNotComparable non-empty",
            "state": "bytes-undecided: cell VerdictInconclusive; the absolute-overhead block is not reached",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:262-286; the absolute bound at :277 is untouched"
          },
          {
            "input": "TierEngineSensitive under ModeFirstAuthorization",
            "state": "bytes-comparable-within-bound: unchanged; the inline 20% bytes floor is not routed through the new field",
            "effect": "no-op",
            "evidence": "internal/perfgate/perfgate.go:193-209"
          }
        ],
        "forbidden": [
          "A cell reporting VerdictPass while BytesNotComparable is non-empty -- an undecided axis must never read as a passing one",
          "A bytes INCONCLUSIVE short-circuiting ahead of the allocs check and hiding an allocation-count regression",
          "BytesNotComparable being consulted by evaluateEngineSensitiveImprovement's inline 20%-fewer floor",
          "Any change to nonIncreasing's signature, its allowance arithmetic, or its two reason strings",
          "Any change to a value in internal/perfgate/tiers.json",
          "Rewiring fewer than all three of evaluateNonRegression (perfgate.go:217), evaluateWithinTolerance (:238) and evaluateStartup (:262) — a red covering only two leaves the third enforcing bytes across differing runners"
        ],
        "seeding": {
          "bytes-comparable-within-bound": "Construct CellComparison with BytesNotComparable left unset and pass it to Evaluate, as internal/perfgate/perfgate_test.go already does",
          "bytes-comparable-over-bound": "Same, with Bytes.DeltaPct > 0 and Bytes.New-Bytes.Old above BytesAllowanceBOp; never by calling nonIncreasing or bytesResult directly",
          "bytes-undecided": "Set CellComparison.BytesNotComparable to a non-empty reason and call Evaluate; the field is the only legal way to reach this state",
          "allocs-decided": "Set Allocs.DeltaPct > 0 with Allocs.Significant true on the same cell, alongside a set BytesNotComparable For TierConcurrent, RaceClean must be set true or Evaluate short-circuits to VerdictFail at internal/perfgate/perfgate.go:156 before the bytes axis is reached."
        },
        "identifiers": {
          "CellComparison.BytesNotComparable": "BytesNotComparable string",
          "bytesResult": "func bytesResult(cell CellComparison) Result",
          "exactAxes": "func exactAxes(cell CellComparison) Result",
          "joinReasons": "func joinReasons(first, second string) string",
          "bytes-undecided reason prefix": "bytes not comparable: ",
          "joinReasons separator": "; ",
          "existing bytes failure reason (unchanged)": "%s increased by %.2f%% (+%.0f B/op against a %.0f B/op allowance)",
          "existing latency inconclusive reason (unchanged)": "latency delta not statistically significant",
          "verdict constants": "VerdictPass, VerdictFail, VerdictInconclusive; collapse through Resolve(tier, mode)",
          "red test": "TestEvaluate_BytesNotComparable",
          "red subtests": "\"inconclusive rather than failing\" (evaluateNonRegression), \"allocs still decided\", \"both undecided axes reported\", \"startup tier does not reach the absolute bound\" (evaluateStartup), \"within-tolerance tier is narrowed too\" (evaluateWithinTolerance via TierDataDominated under ModeFirstAuthorization), \"concurrent tier is narrowed too\" (evaluateWithinTolerance via TierConcurrent with RaceClean true)",
          "non-red pin, not in redTests": "The subtest \"unset field enforces the bytes bound\" asserts today's behaviour, which the FIELD-FIRST chunk preserves exactly. It is a non-regression pin authored inside the red stage; a per-subtest red-sanity check must not expect it to fail.",
          "task 2.2 proof split": "The two existing tiers.json tests (TestTierConfig_EveryCellStatesAnAllowance, TestBytesAllowancesAreJustifiedBySpread) prove every cell states an allowance justified by within-run spread. They do not prove no committed number moved; that half of task 2.2 is proven by the diff."
        }
      }
    },
    {
      "id": "perfgate-cmd-cross-runner",
      "summary": "cmd/perfgate sets BytesNotComparable on the cross-runner path and reports both undecided axes on one line; the retired subtest is rewritten here, which is the blocker against task 1.4.",
      "tasks": [
        "1.1",
        "1.2",
        "2.3",
        "2.4"
      ],
      "contract": {
        "states": [
          "cross-runner",
          "same-runner",
          "cross-runner-clean",
          "cross-runner-allocs-regressed"
        ],
        "stateDescriptions": {
          "cross-runner": "sameRunner(oldID, newID) is false, so every cell carries BytesNotComparable",
          "same-runner": "sameRunner is true, so BytesNotComparable stays empty and benchstat runs as today",
          "cross-runner-clean": "cross-runner with no cell failing on allocs or tier lookup; the run exits 0",
          "cross-runner-allocs-regressed": "cross-runner with at least one cell's allocation count increased; the run exits 1"
        },
        "transitions": [
          {
            "input": "two raw files with differing runner identities",
            "state": "cross-runner",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:197 crossRunner := !sameRunner(oldID, newID)"
          },
          {
            "input": "cross-runner cell, allocs identical, bytes higher",
            "state": "cross-runner: cell INCONCLUSIVE, reason names both runners, no failure",
            "effect": "set",
            "evidence": "delta spec scenario 'Allocated bytes move across a change of runner with the allocation count unchanged'; cmd/perfgate/main.go:254-265"
          },
          {
            "input": "cross-runner cell, allocation count increased",
            "state": "cross-runner-allocs-regressed: cell FAIL, reason 'allocs increased by N%', not overwritten by the runner text",
            "effect": "forced",
            "evidence": "cmd/perfgate/main.go:254 guards the overwrite on VerdictInconclusive, so a FAIL keeps its own reason"
          },
          {
            "input": "cross-runner, every cell inconclusive",
            "state": "cross-runner-clean: exitPass",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:241-243 and :263-265 collapse through Resolve; under non-regression every tier returns VerdictPass"
          },
          {
            "input": "same-runner pair whose candidate allocates more bytes than its allowance",
            "state": "same-runner: cell FAIL, reason 'bytes increased', exit 1",
            "effect": "no-op",
            "evidence": "delta spec scenario 'Allocated bytes are enforced when the runners match'"
          },
          {
            "input": "cross-runner cell absent from the baseline",
            "state": "cross-runner: cell INCONCLUSIVE, reason 'no baseline figure for this cell'",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:235-246; unchanged by this change"
          },
          {
            "input": "benchstat declines to pair a same-runner pair",
            "state": "same-runner: exitNotComparable 3 with the pairing refusal as the reason, no retry",
            "effect": "no-op",
            "evidence": "cmd/perfgate/main.go:206-212; delta spec scenario 'An incomparable pair is never forced into a comparison'; pinned by TestRun_UnpairedComparisonExitsThree"
          },
          {
            "input": "an unknown runner identity on either side",
            "state": "cross-runner",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:308-310 sameRunner; pinned by TestCrossRunner_UnknownIdentityIsNotAMatch, whose uniform-INCONCLUSIVE and exit-0 assertions stay true"
          }
        ],
        "forbidden": [
          "Setting BytesNotComparable on a same-runner pair",
          "Stripping, rewriting or normalising goos/goarch/pkg/cpu lines in a committed corpus, or re-running benchstat after a refusal to pair",
          "Editing any file under internal/perfgate/testdata; a filtered fixture is written to t.TempDir(), following cmd/perfgate/main_test.go:255-274",
          "Letting the cross-runner reason overwrite an allocs FAIL reason",
          "Raising needsRerun (exit 2) for a cross-runner cell",
          "Asserting a uniform verdict over all cells of the same-runner fixture; its failing set is a stated partition, not the whole cell set",
          "Authoring any test under cmd/perfgate outside this seam's red stage: that stage seals every _test.go under cmd/perfgate, so a later chunk cannot add or amend one"
        ],
        "seeding": {
          "cross-runner": "-old internal/perfgate/testdata/profile-30637802780/bench-vm.txt -candidate internal/perfgate/testdata/profile-30614184386/bench-vm.txt (roles swapped relative to the first subtest), through the existing runGate helper at cmd/perfgate/main_test.go:280",
          "cross-runner-clean": "The same swapped pair with the candidate filtered to its GoldsetParse/ cells only, written to t.TempDir() by a new keepingCells helper modelled on withoutCPULines (cmd/perfgate/main_test.go:255-274). keepingCells MUST copy the goos:/goarch:/pkg:/cpu: preamble lines verbatim and filter only Benchmark result lines: a filtered file without its preamble reads as an unknown identity and turns the test into a different case.",
          "same-runner": "-old internal/perfgate/testdata/profile-30637802780/bench-vm.txt -candidate internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt, with swapBenchstat (cmd/perfgate/main_test.go:64) returning the committed profile-30637802780/benchstat.csv; runGate must NOT be used here because it fails the test if benchstat is reached",
          "cross-runner-allocs-regressed": "The unfiltered swapped pair: its 13 Goldset/ cells each gain exactly one allocation"
        },
        "identifiers": {
          "cross-runner reason format string": "latency and bytes not comparable: baseline ran on %s, candidate on %s",
          "BytesNotComparable value set by main.go": "fmt.Sprintf(\"baseline ran on %s, candidate on %s\", oldID, newID)",
          "renamed test": "TestCrossRunner_AllocsEnforcedBytesInconclusive",
          "renamed-from": "TestCrossRunner_LatencyInconclusiveBytesEnforced (cmd/perfgate/main_test.go:87)",
          "subtest kept": "\"latency inconclusive naming both runners\"",
          "subtest replacing \"bytes still enforced across differing runners\"": "\"bytes inconclusive across differing runners\"",
          "subtest added": "\"allocation counts still enforced across differing runners\"",
          "new test": "TestCrossRunner_CleanRunExitsZero",
          "new test 2": "TestSameRunner_BytesStillEnforced",
          "new test helper": "func keepingCells(t *testing.T, path, prefix string) string -- copies the goos:/goarch:/pkg:/cpu: preamble verbatim, keeps only Benchmark lines whose trimmed name starts with prefix, writes to t.TempDir()",
          "existing helpers to reuse": "runGate, swapBenchstat, reportLines, verdictsFor, uniformVerdicts, withoutCell, candidateCells, identityOf (cmd/perfgate/main_test.go:280, 64, 173, 214, 225, 233, 195, 243)",
          "exit codes": "exitPass 0, exitFail 1, exitNeedsRerun 2, exitNotComparable 3 (cmd/perfgate/main.go:28-33)",
          "engine-sensitive first-authorization note": "The cross-runner reason overwrite at cmd/perfgate/main.go:262 also fires on a TierEngineSensitive first-authorization cell that stopped at the latency-significance gate. First authorization compares two arms of one run on one runner, so release.yml does not produce a cross-runner first-authorization pair; the wording stays accurate either way because bytes genuinely is not comparable there.",
          "package reason is not observable through the command": "cmd/perfgate/main.go:254-262 overwrites the reason of every cross-runner INCONCLUSIVE cell, so the package-built \"bytes not comparable: \" prefix and joinReasons can only be asserted by the internal/perfgate unit test, never through the gate binary. The two mechanisms do not compose and are not meant to.",
          "non-red pin, not in redTests": "TestSameRunner_BytesStillEnforced is authored in this red stage but is NOT in redTests: it asserts same-runner behaviour the narrowing preserves exactly, so it is green before and after. A red-sanity check must expect it to PASS, never to fail."
        }
      }
    },
    {
      "id": "gate-decision-record",
      "summary": "NO-RED-WAIVER: docs-only. ADR 0008's 'Note (runner comparability)' currently asserts the opposite of the new rule and must be amended, plus a CHANGELOG entry under [Unreleased].",
      "tasks": [
        "3.1",
        "3.2"
      ],
      "contract": {
        "states": [
          "adr-amended",
          "changelog-recorded"
        ],
        "stateDescriptions": {
          "adr-amended": "ADR 0008 states which measurement axis survives a change of runner and why",
          "changelog-recorded": "CHANGELOG.md [Unreleased] names the observable gate change"
        },
        "transitions": [
          {
            "input": "ADR 0008 lines 38-44 claim 'Allocation count and allocated bytes stay enforced regardless of runner identity'",
            "state": "adr-amended: allocation count survives a change of runner because it is an exact per-op integer with no iteration-count term; allocated bytes do not, so B/op is reported inconclusive across differing identities and enforced against the cell's stated allowance whenever they match",
            "effect": "forced",
            "evidence": "docs/adr/0008-consumer-performance-gate.md:34-44 against the delta spec's MODIFIED requirement"
          },
          {
            "input": "an amendment naming only the iteration denominator as the mechanism",
            "state": "adr-amended: rejected -- the note states both mechanisms",
            "effect": "forced",
            "evidence": "On the committed pair the iteration counts are within ~10% (Goldset/counter-closure-2 runs 28153 on AMD against 28812 on Intel) yet every GoldsetParse/ cell moves +16 to +288 B/op with allocs/op identical: per-allocation sizing differs across machines. The 2.8x denominator effect the proposal cites comes from run 33985766146, a different pair. Both mechanisms make B/op a property of the machine; a note naming only the denominator would be narrower than the repository's own fixture."
          },
          {
            "input": "CHANGELOG.md [Unreleased] at line 8 is empty",
            "state": "changelog-recorded: carries a Changed entry stating that a release measured against a baseline from a different runner is gated on allocation counts rather than allocated bytes",
            "effect": "set",
            "evidence": "CHANGELOG.md:8; task 3.2"
          }
        ],
        "forbidden": [
          "Amending ADR 0008's threshold list at lines 21-24 or its per-cell allowance derivation at lines 65-77 -- the allowances are unchanged in value and still enforced on matching runners",
          "An ADR note attributing the movement solely to the iteration count",
          "A CHANGELOG entry that describes the implementation (a new field, a helper) rather than the observable gate behaviour"
        ],
        "seeding": {
          "adr-amended": "Edit the existing 'Note (runner comparability)' paragraph in place; do not add a competing note",
          "changelog-recorded": "Add a '### Changed' bullet under the existing '## [Unreleased]' heading"
        },
        "identifiers": {
          "ADR anchor": "docs/adr/0008-consumer-performance-gate.md:34 'Note (runner comparability):'",
          "CHANGELOG anchor": "CHANGELOG.md:8 '## [Unreleased]'",
          "CHANGELOG section": "### Changed",
          "threshold lines stay as they are": "ADR 0008 lines 21-24 are not amended. The runner-comparability note alone carries the exception; no per-tier pointer is added. This is a ruling, not a question for the coder."
        }
      }
    },
    {
      "id": "release-verification",
      "summary": "NO-RED-WAIVER: 4.1 is the floor command itself and 4.2 is a CI dispatch that cannot run on a developer machine; neither has an assertion a red test could hold.",
      "tasks": [
        "4.1",
        "4.2"
      ],
      "contract": {
        "states": [
          "floor-clean",
          "gate-dispatched"
        ],
        "stateDescriptions": {
          "floor-clean": "the repository suite, the race suite over core/plugins/runtime, go vet and the linter all exit successfully",
          "gate-dispatched": "the gate has run in CI against the release candidate tree and its reported behaviour has been read"
        },
        "transitions": [
          {
            "input": "the floor command",
            "state": "floor-clean",
            "effect": "forced",
            "evidence": "task 4.1; Makefile:1-23 defines test (go test -timeout 2m ./...) and lint (golangci-lint run)"
          },
          {
            "input": "a workflow dispatch of the gate against the release candidate tree",
            "state": "gate-dispatched: the run reports non-regression, states the runner comparison, reports the bytes axis inconclusive on the cross-runner pair and exits 0",
            "effect": "forced",
            "evidence": "task 4.2; .github/workflows/release.yml guards 'Store VM baseline on the authorized release' on an implicit success()"
          }
        ],
        "forbidden": [
          "Marking 4.2 complete from a local run: no local invocation exercises the workflow's baseline-storage guard",
          "Narrowing the race suite below core, plugins and runtime, or dropping the -timeout limits"
        ],
        "seeding": {
          "floor-clean": "Run the packet's floor command from the repository root",
          "gate-dispatched": "Dispatch the release workflow; the run id and its verdict report are the evidence"
        },
        "identifiers": {
          "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
          "workflow": ".github/workflows/release.yml",
          "baseline step": "Store VM baseline on the authorized release",
          "4.2 is not closable in the run": "Task 4.2 dispatches .github/workflows/release.yml (workflow_dispatch at release.yml:16) and has no local execution path. It is closed post-merge by recording the dispatch run id; the implementation run ticks 4.1 only and reports 4.2 as outstanding."
        }
      }
    }
  ],
  "requirements": [
    {
      "shall": "The stored VM baseline SHALL carry the identity of the runner that produced it,",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/latency inconclusive naming both runners",
        "TestCrossRunner_UnknownIdentityIsNotAMatch"
      ]
    },
    {
      "shall": "and the gate SHALL compare that identity against the candidate run's before",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/latency inconclusive naming both runners",
        "TestCrossRunner_UnknownIdentityIsNotAMatch"
      ]
    },
    {
      "shall": "drawing a latency conclusion from it. Where the two differ, the gate SHALL report",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/latency inconclusive naming both runners",
        "TestCrossRunner_UnknownIdentityIsNotAMatch"
      ]
    },
    {
      "shall": "runners, and SHALL NOT convert the difference into a pass or a failure.",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/latency inconclusive naming both runners",
        "TestCrossRunner_UnknownIdentityIsNotAMatch"
      ]
    },
    {
      "shall": "Allocation counts SHALL stay enforced across differing runners. They are exact",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/allocation counts still enforced across differing runners",
        "TestEvaluate_BytesNotComparable/allocs still decided"
      ]
    },
    {
      "shall": "Allocated bytes SHALL NOT be judged across differing runner identities. `B/op` is",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/bytes inconclusive across differing runners",
        "TestEvaluate_BytesNotComparable/inconclusive rather than failing",
        "TestEvaluate_BytesNotComparable/both undecided axes reported"
      ]
    },
    {
      "shall": "per-operation figure. Where the identities differ, the gate SHALL report the bytes",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/bytes inconclusive across differing runners",
        "TestEvaluate_BytesNotComparable/inconclusive rather than failing",
        "TestEvaluate_BytesNotComparable/both undecided axes reported"
      ]
    },
    {
      "shall": "axis inconclusive, naming both runners, and SHALL NOT convert the difference into",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/bytes inconclusive across differing runners",
        "TestEvaluate_BytesNotComparable/inconclusive rather than failing",
        "TestEvaluate_BytesNotComparable/both undecided axes reported"
      ]
    },
    {
      "shall": "a pass or a failure. Where the identities match, allocated bytes SHALL be enforced",
      "tests": [
        "TestSameRunner_BytesStillEnforced",
        "TestEvaluate_BytesNotComparable/unset field enforces the bytes bound",
        "TestCrossRunner_ZeroBaselineBytesFails"
      ]
    },
    {
      "shall": "The gate SHALL NOT strip or normalise the configuration lines that make two",
      "tests": [
        "TestRun_UnpairedComparisonExitsThree",
        "TestParseBenchstatCSV_UnpairedSingleGroup"
      ]
    },
    {
      "shall": "and the gate SHALL treat that refusal as a verdict input rather than an obstacle.",
      "tests": [
        "TestRun_UnpairedComparisonExitsThree",
        "TestParseBenchstatCSV_UnpairedSingleGroup"
      ]
    },
    {
      "shall": "- **THEN** the latency cells judged against that baseline SHALL be reported inconclusive with both runner identities named, and the release SHALL still be gated on correctness, the race suite and allocation counts",
      "tests": [
        "TestCrossRunner_CleanRunExitsZero",
        "TestCrossRunner_MissingBaselineCellIsInconclusive"
      ]
    },
    {
      "shall": "- **THEN** that cell's bytes verdict SHALL be reported inconclusive naming both runners, and SHALL NOT fail the release",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/bytes inconclusive across differing runners",
        "TestEvaluate_BytesNotComparable/inconclusive rather than failing",
        "TestEvaluate_BytesNotComparable/both undecided axes reported"
      ]
    },
    {
      "shall": "- **THEN** that cell SHALL fail, exactly as it does today",
      "tests": [
        "TestSameRunner_BytesStillEnforced",
        "TestEvaluate_BytesNotComparable/unset field enforces the bytes bound",
        "TestCrossRunner_ZeroBaselineBytesFails"
      ]
    },
    {
      "shall": "- **THEN** that cell SHALL fail, because the allocation count carries no iteration-count term and the change of machine cannot explain it",
      "tests": [
        "TestCrossRunner_AllocsEnforcedBytesInconclusive/allocation counts still enforced across differing runners",
        "TestEvaluate_BytesNotComparable/allocs still decided"
      ]
    },
    {
      "shall": "- **THEN** the gate SHALL fail with that reason stated, and SHALL NOT retry by removing the configuration lines that caused the refusal",
      "tests": [
        "TestRun_UnpairedComparisonExitsThree",
        "TestParseBenchstatCSV_UnpairedSingleGroup"
      ]
    }
  ],
  "testHarness": [
    "swapBenchstat — cmd/perfgate/main_test.go:64 — replaces the package-level runBenchstat func var for one test and restores it via t.Cleanup",
    "runGate — cmd/perfgate/main_test.go:280 — installs a benchstat stub that fails the test if reached, runs run() with -tiers committedTiers plus the caller's args, returns (exit code, stdout, stderr)",
    "reportLine — cmd/perfgate/main_test.go:166 — struct{verdict, reason string}, the parsed shape of one report line",
    "reportLines — cmd/perfgate/main_test.go:173 — parses the whole report (\"name: VERDICT\" / \"name: VERDICT (reason)\") into map[string]reportLine keyed by full cell name including the -GOMAXPROCS suffix",
    "candidateCells — cmd/perfgate/main_test.go:195 — sorted cell-name slice read from pinnedProfileDir/benchstat.csv through ParseBenchstatCSV, with require.Len(cells, 27)",
    "verdictsFor — cmd/perfgate/main_test.go:214 — map of one verdict per requested name, \"\" for a cell the report never mentioned",
    "uniformVerdicts — cmd/perfgate/main_test.go:225 — the expected map when every named cell must carry the same verdict",
    "withoutCell — cmd/perfgate/main_test.go:233 — drops one name from a cell-name slice (used for callBoundaryCell)",
    "identityOf — cmd/perfgate/main_test.go:243 — RunnerIdentity.String() of a raw bench file, via ReadRunnerIdentity",
    "withoutCPULines — cmd/perfgate/main_test.go:257 — copies a committed raw bench file into t.TempDir() with its cpu: lines removed, returns the temp path",
    "writeLookup — cmd/perfgate/main_test.go:342 — marshals a perfgate.BaselineLookup into a temp JSON file for the resolve-mode subcommand",
    "pinnedProfileDir — cmd/perfgate/main_test.go:23 — \"../../internal/perfgate/testdata/profile-30637802780\" (AMD EPYC 7763)",
    "committedTiers — cmd/perfgate/main_test.go:24 — \"../../internal/perfgate/tiers.json\"",
    "unpairedFixture — cmd/perfgate/main_test.go:25 — \"../../internal/perfgate/testdata/unpaired-single-group.csv\"",
    "crossRunnerBaselineDir — cmd/perfgate/main_test.go:76 — \"../../internal/perfgate/testdata/profile-30614184386\" (INTEL(R) XEON(R) PLATINUM 8573C)",
    "callBoundaryCell — cmd/perfgate/main_test.go:79 — \"GoldsetCall/call-boundary-2\", present in the AMD corpus and absent from the Intel one",
    "readSampleMetrics — internal/perfgate/bench_metrics_test.go:114 — ReadBenchmarkMetrics over pinnedProfileDir/<file>; hardcoded to the pinned (AMD) profile, so a cross-runner unit test needs its own reader or a profileDir parameter",
    "parsePinnedBenchstat — internal/perfgate/bench_metrics_test.go:126 — ParseBenchstatCSV over pinnedProfileDir/benchstat.csv",
    "samplesPerCell — internal/perfgate/bench_metrics_test.go:15 — 10, the release workflow's appended-run count",
    "benchVMBytesSpread — internal/perfgate/bytes_allowance_spread_test.go:113 — per-cell max-min B/op over one profile's bench-vm.txt, keyed with the -GOMAXPROCS suffix trimmed; takes a profileDir, so it is the one corpus reader already parameterised",
    "readTierConfigFile — internal/perfgate/bytes_allowance_spread_test.go:134 — unmarshals tiers.json into the unexported tierConfigFile, keeping a missing allowance distinguishable from a stated 0",
    "longerBenchtimeProfileDir — internal/perfgate/bytes_allowance_spread_test.go:22 — \"testdata/profile-30630796967\", the 400ms rerun; directional check only, never an allowance source",
    "bytesAllowanceExemptCell — internal/perfgate/bytes_allowance_spread_test.go:27 — \"Goldset/guard-nil\"",
    "resolveGateMode — internal/perfgate/gatemode_test.go:13 — ResolveGateMode wrapper returning mode and outcome for one BaselineLookup",
    "unpairedFixture — internal/perfgate/parse_unpaired_test.go:16 — \"testdata/unpaired-single-group.csv\" (the package-local twin of the cmd constant)",
    "modeName — internal/perfgate/perfgate_test.go:290 — subtest-name label for a Mode",
    "pinnedProfileRunID / pinnedProfileDir — internal/perfgate/perfgate_test.go:705-706 — \"30637802780\" and \"testdata/profile-\" + that ID",
    "pinnedBenchEvaluatorSHA256 / pinnedBenchVMSHA256 — internal/perfgate/perfgate_test.go:716-717 — content digests of the two pinned raw files",
    "hashHex — internal/perfgate/perfgate_test.go:785 — sha256 hex of a byte slice",
    "parsePinnedVerdicts — internal/perfgate/perfgate_test.go:793 — parses a committed verdict.txt into map[string]Verdict",
    "parseVerdict — internal/perfgate/perfgate_test.go:808 — one verdict word to Verdict, t.Fatalf on anything else",
    "intelProfileDir — internal/perfgate/runner_identity_test.go:16 — \"testdata/profile-30614184386\", the package-local name for the cross-runner counterexample",
    "corpus — internal/perfgate/runner_identity_test.go:21 — closure local to TestReadRunnerIdentity that opens <dir>/bench-vm.txt; not reachable from another test as written"
  ],
  "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
