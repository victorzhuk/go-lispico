# Design: release-gate-baseline-comparability

## Context

The consumer release gate is three pieces: `.github/workflows/release.yml` collects
the evidence, `cmd/perfgate` orchestrates, and `internal/perfgate` decides. Until
v0.13.0 it had only ever run in first-authorization mode, where both arms of the
comparison — the Evaluator run and the VM run — come from a single hosted job. Every
defect this change addresses is invisible in that mode and appears the moment the
gate compares two separate runs.

Three facts about the existing code shape everything below.

**benchstat does not fail when it declines to pair.** When the two input files record
different `cpu:` values it exits 0 and emits two single-group tables. Nothing is
reported as an error; the failure surfaces later, in `parseBlock`
(`internal/perfgate/parse.go:73-75`), as `perfgate: benchstat csv data row too short`
— indistinguishable from a genuinely malformed CSV. Passing benchstat's own stderr
through cannot state the reason, because benchstat does not state one.

**Bytes and allocation counts were never sampled statistics.** `perfgate.go:288-292`
already says so: they are "exact counts in Go's benchmark output, not sampled
statistics, so unlike latency they carry no significance gate". Routing an exact count
through a pairing tool that can decline to pair is what loses it when the runner
changes.

**`internal/perfgate` is one package with one test file.** `perfgate.go`, `parse.go`,
`tiers.json` and the 780-line `perfgate_test.go` are a single compilation unit, and
`cmd/perfgate` is its only production caller. Seven of the eight code tasks land
there, so the chunk graph below is mostly serial. That is a property of the code, not
a planning failure.

## Goals / Non-Goals

**Goals**

- A latency comparison drawn against a baseline measured on different hardware is
  inconclusive by construction, naming both runners, rather than passed or failed.
- Allocation counts and allocated bytes stay enforced across differing runners.
- Every gate cell states its bytes allowance explicitly; an absent allowance is a
  configuration error rather than an implicit zero.
- A manually dispatched pre-flight resolves the same mode, and reaches the same
  per-cell verdicts, as the release it precedes.
- A failure to enumerate or download the stored baseline fails the gate instead of
  being read as the absence of a baseline.

**Non-Goals**

- Re-deriving or lowering `Goldset/guard-nil`'s existing 4 B/op allowance. It covers a
  reproducible between-engine offset, not sampling spread, and is licensed by ADR
  0008.
- Changing any tier assignment in `tiers.json`.
- Closing the benchstat-`~` blind spot on the latency axis. This change removes bytes
  and allocs from benchstat's path, which narrows that blind spot to latency; it does
  not resolve what remains.
- Any change to the language, the engines, or an embedder-visible API.
- Short-circuiting the doubled-benchtime rerun when every latency cell is inconclusive
  for a runner mismatch rather than a p-value. Real, and recorded under Risks; not in
  scope because `tasks.md` does not name it and doing it silently would change the
  rerun contract ADR 0008 records.

## Decisions

### D1 — the runner identity comes from the preamble the file already carries

`RunnerIdentity{GOOS, GOARCH, CPU string}`, read from the `goos:`/`goarch:`/`cpu:`
lines that `go test -bench` writes into every `bench-*.txt`. No sidecar asset: a
second asset would add one thing to publish, one thing to download, and a new way for
the two to disagree, all to record a fact the uploaded file already carries. Verified
present in all six checked-in bench files. `pkg:` is excluded — it names the
benchmarked package, not hardware.

`String()` renders `GOOS/GOARCH/CPU`, each empty field as the literal `unknown`. Each
value is trimmed and its internal space runs collapsed to one. That normalisation is
required rather than cosmetic: the AMD `cpu:` lines in the committed profiles end in
sixteen trailing spaces, so an unnormalised compare against a future run of the same
hardware could differ on whitespace alone. Normalising the identity string for
perfgate's own comparison is not normalising the input file — nothing is written, and
this is not a tool's pairing key.

Every checked-in bench file repeats the four preamble lines ten times, because the
workflow appends one `go test` run per sample (`release.yml:131-134`).
`ReadRunnerIdentity` reads all of them and returns `ErrInconsistentPreamble` when they
disagree: a file recording two runners has no single identity to report.

One honest correction to task 2.1. It says a baseline stored before this change
"reports its identity as unknown". Under D1 it does not — every real stored baseline
carries a `cpu:` line, so a pre-change baseline reports its true identity, which is
strictly better than the task expected. What actually produces `unknown` is a file
with no `cpu:` line: a hand-authored benchstat fixture, or a platform whose toolchain
emits none. The observable the task protects — no crash, graceful degradation — is
preserved and tested.

### D2 — perfgate reads bytes and allocs from the raw files; benchstat keeps latency only

Rejected: passing `-ignore=cpu`. This was measured against the pinned benchstat with a
differing `cpu:` line. Plain `-format csv` emits two single-group tables and no
`vs base`/`P` columns. `-format csv -ignore=cpu` emits one paired table **and drops
the `cpu:` line from its output entirely**. So the flag demonstrably removes the
configuration line that made the runs incomparable, in order to obtain the comparison
— the act the spec forbids. That the input files are untouched is not the test the
spec states; the test is whether the comparison was obtained by disregarding the
configuration, and it was.

Also rejected: parsing benchstat's two single-group tables and pairing them inside
perfgate. That reconstructs the pairing benchstat declined to make, from benchstat's
own output — the same objection with more moving parts.

The `-ignore=cpu` alternative was raised a second time during planning, on the reading
that the prohibition targets only "obtaining a latency conclusion by suppressing the
evidence of incomparability", which passing the flag while independently reporting
latency inconclusive arguably does not do. It was put to the author with both
trade-offs and settled in favour of the reader below. The deciding evidence is the
measured one above: the flag removes the `cpu:` line from benchstat's output, so the
comparison is obtained by disregarding the configuration, whatever it is used for
afterwards. Recorded here so the question is not reopened without new evidence.

Taken instead: `ReadBenchmarkMetrics` parses the `name iters ns/op B/op allocs/op`
lines straight from the raw files and takes the per-cell median. Four reasons.

1. It changes no number today. A median over the raw per-sample fields reproduces
   benchstat's own Old and New figures for all 27 cells on both axes in
   `profile-30637802780`, with zero mismatches.
2. These axes were never sampled statistics, as the existing code already says.
3. It needs no pairing, so it survives a differing runner by construction — exactly
   what the spec demands.
4. It adds no dependency: `golang.org/x/perf` is not in `go.mod`, and parsing those
   lines needs no library.

**The two spec scenarios are ordered, not overlapping.** perfgate compares the two
runner identities first, from the raw files, before benchstat is consulted. When they
differ it never asks benchstat for a paired latency comparison at all: every latency
cell is INCONCLUSIVE by perfgate's own rule, naming both identities, and the release
stays gated on correctness, the race suite, bytes and allocs. The "tool declines to
pair" scenario then covers the residual — identities match, yet benchstat still
returns an unpaired table, from a differing `goos:`, `goarch:` or `pkg:`, or a future
benchstat change. That is a stated failure with no retry.

**Detecting the refusal.** `parseBlock` discriminates on the metric header row
(`records[1]`), before any data row is read, three ways:

| header shape | meaning | action |
| --- | --- | --- |
| 7 fields, `header[5] == "vs base"`, `header[6] == "P"` | paired | proceed as today |
| 3 fields, `header[2] == "CI"` | single-group | return `ErrUnpairedComparison` |
| anything else | malformed | the existing generic error, which now means only what it says |

Both shapes were measured: the paired shape on all three blocks of the committed
`profile-30637802780/benchstat.csv`, the single-group shape by rewriting one `cpu:`
line and re-running the pinned benchstat, which produced six blocks — two
single-group tables of three metric blocks each — and exit 0. The existing data-row
check stays as the malformed-CSV backstop and becomes unreachable for the
single-group case.

`cmd/perfgate` catches it with `errors.Is` and re-reports it naming both identities,
then exits 3. The gate states the reason itself because benchstat states none: on the
measured run its entire stderr was `B65: summaries must be >0 to compute geomean`,
which is about the geomean, not about pairing.

### D3 — the mode decision moves into Go; `gh` stays in shell

The defect is that `gh release download --pattern` exits non-zero both when the asset
is absent and when the API fails, so the current loop (`release.yml:100`) cannot tell
them apart. The step is restructured to ask before it downloads: `gh release list`
non-zero is enumeration failure; `gh release view <tag> --json assets` non-zero is
enumeration failure; a tag whose asset list omits `bench-vm.txt` is absence *for that
tag*; `gh release download` non-zero after the asset was seen is download failure.
Only "every enumerated tag was inspected and none carried the asset" is absence.

The classification is a four-way decision with one safe answer, not I/O. It becomes
`func ResolveGateMode(l BaselineLookup) (Mode, BaselineOutcome, error)` in
`internal/perfgate`, called through a `resolve-mode` subcommand on `cmd/perfgate`,
which the workflow invokes instead of deciding inline. `gh` — which needs auth and a
network — stays in shell, and the step becomes a data collector. Tasks 1.3 and 4.3
become unit-testable without a network.

A resolution failure surfaces as a non-nil `error`, not a `Result`: it is a
gate-configuration failure, not a per-cell measurement outcome, and it must stop the
run before any cell is judged. `EnumerationOK` is false by zero value, so an
unpopulated lookup fails closed rather than selecting first-authorization.

### D4 — an explicit `0` is a stated allowance; the invariant is restated, not overturned

`tiers.json`'s comment and ADR 0008 both say the thirteen `GoldsetParse/*` cells keep
the exact non-increasing bound with no allowance. Task 3.1 requires every cell to
state one. These only look contradictory. The spec's failure mode is "reading the
absence as an allowance of zero" — the defect is silence, not the number zero. An
explicit `0` is a recorded, reviewable decision that a cell has no measured spread and
therefore no headroom, and it enforces exactly the bound the invariant names. So an
absent entry becomes the missing-config error, an explicit `0` satisfies "every cell
states an allowance", and the two texts are amended as clarifications of mechanism
rather than reversals of policy. That is why 5.1 is a documentation seam and not a
blocker.

This has a representation consequence. A stated `0` and an absent key are
indistinguishable after decoding into `map[string]float64` (`parse.go:184`), so
`tierConfigFile.BytesAllowanceBOp` becomes `map[string]*float64` and `CellTier` gains
`BytesAllowanceStated bool`. Without that, the "stated 0" reading is unimplementable.

**The rule.** Per-cell absolute, equal to that cell's within-run `B/op` spread — max
sample minus min sample over the ten samples — in
`internal/perfgate/testdata/profile-30637802780`. Not size-proportional and not a
formula: the spec ties the number to "observed sampling spread on that cell", and a
size-proportional rule would justify headroom by a cell's magnitude rather than its
measured behaviour, handing headroom to the fourteen cells that are bit-identical
across all ten samples and demonstrably have none.

**Why that profile alone.** `profile-30630796967` is corroboration, not a second term
in a max. Its committed files are a 400 ms measurement — the rerun step regenerated
them at doubled benchtime before the upload, 57092 iterations against 28188 — and
`B/op` is a total divided by an iteration count, so twice the iterations halves the
rounding granularity. Its spread is systematically smaller: across all 26 cells it
shares, every 400 ms spread is ≤ its 200 ms counterpart, without exception. The gate
benchmarks at 200 ms and escalates to 400 ms only on a rerun, where the finer average
makes a 200 ms-sized allowance conservative — the safe direction. A max across the two
would select the 200 ms figure every time anyway; naming one profile says why that is
right rather than leaving it a coincidence. `profile-30614184386` records a third CPU
and is used only as a directional check, and as the cross-runner fixture.

Within one profile's ten samples is sampling spread. Between profiles is not — the two
newer profiles measure different trees, one carries an added cell, and they were taken
at different benchtimes — so no number here is derived that way.

**Expected values**, which the coder recomputes and must not simply copy:
`Goldset/counter-closure` 1, `Goldset/guard-nil` 4 (the exception below),
`Goldset/kw-lookup` 1, `Goldset/loop-sum` 2, `Goldset/merge-config` 3,
`Goldset/pipeline` 2, `Goldset/queue-promote` 8, `Goldset/registry-fold` 2,
`Goldset/route-decision` 1, `Goldset/rule-load` 6, `Goldset/safe-parse` 2,
`Goldset/text-render` 1, `Goldset/twice-macro` 2, `GoldsetCall/call-boundary` 0, and 0
for each of the thirteen `GoldsetParse/*` cells.

**`Goldset/guard-nil` keeps its 4.** Its spread is 0; the 4 covers a reproducible
between-engine offset — 1128 Evaluator against 1129 VM, +0.09%, p=0.000, on three
hosted runs — not sampling spread, and ADR 0008 licenses it. That offset exists only
under first authorization, where the two arms are two engines; in non-regression both
arms are the VM and only the spread of 0 applies.

**The non-widening rule.** An allowance may be held or lowered freely. Raising one
requires a newer committed classification profile measured at the gate's own
benchtime whose within-run spread for that cell exceeds the current value, and the
raise is capped at that spread. This is mechanically checkable and is the seam's own
red test. It structurally cannot admit a measured regression: a regression shifts the
median while leaving the within-run spread where it was, so no regression can raise
the bound that would hide it.

**On the size-class argument.** `guard-nil`'s 4 was justified partly as "smaller than
Go's smallest 8-byte size class, so it cannot conceal even one added allocation". That
does not scale — `queue-promote` at ~16.5 KB has a measured spread of 8, exactly one
size class. It does not need to scale: the allocs axis keeps a zero allowance and is
exact in the same output, so an added allocation is caught there whatever the bytes
allowance is. The size-class reasoning was belt-and-braces; the allocs axis is the
actual guard, and this change does not touch it.

**No verdict moves.** Engine-sensitive cells take `evaluateEngineSensitiveImprovement`'s
inline 20%-fewer-bytes floor, which no allowance reaches (`perfgate.go:293-296`). The
only allowance-reading cells under first authorization are the thirteen
`GoldsetParse/*` (bit-identical, delta 0), `guard-nil` (unchanged) and `rule-load`
(bytes −36.22%, so `nonIncreasing` passes at `DeltaPct <= 0` and the allowance is
inert). `TestPinnedProfile`'s committed verdicts stand.

### D5 — the mode step loses its release guard; exactly one step keeps it

Removing `if: github.event_name == 'release'` from `Determine gate mode` means it also
runs on `workflow_dispatch`, where `github.event.release.tag_name` is empty. The skip
line is `[ "$tag" = "$RELEASE_TAG" ] && continue` (`release.yml:99`); with
`RELEASE_TAG` empty the comparison is false for every non-empty tag, so nothing is
skipped and the loop considers the newest release first. That is correct pre-flight
behaviour: on a dispatch there is no release being cut, so there is nothing to
exclude, and the baseline found is the one the next release will use.

Exactly one step keeps the release guard: `Store VM baseline on the authorized
release` (`release.yml:210-215`), the only `gh release upload` in the file. `Upload
release evidence` is `if: always()` and writes a workflow artifact, not a release
asset, so task 4.2 does not touch it.

`GH_TOKEN: ${{ github.token }}` is expected to be available on `workflow_dispatch` —
the workflow declares `permissions: contents: write` at file scope
(`release.yml:48-49`), which applies to every trigger. Not verifiable in this repo;
recorded under Risks.

### D6 — what carries a contract test and what cannot

Five seams carry a `NO-RED-WAIVER:` marker with its reason: task 4.2 is a property of
`release.yml` verified by reading it (a YAML-substring test pins indentation, not
behaviour); tasks 5.1 and 5.2 are documentation with no executable behaviour; task 1.4
is a re-run of the existing suite; task 6.1 is the floor; task 6.2 needs a hosted
runner and cannot be executed on a developer box, where the latency output would not
be trustworthy in any case.

### D7 — task 1.4's "unchanged", stated plainly

Task 1.4 asks that the existing gate tests "still pass unchanged, so the new rules are
additive". Once a missing allowance is a configuration error, two existing tier-config
fixtures stop loading: `TestLoadTierConfig` (`perfgate_test.go:369-379`), whose literal
carries no `bytesAllowanceBOp` map *and* whose assertion at `:378` states the very
semantics D4 abolishes, and `TestLoadTierConfig_BytesAllowance` (`:392-401`), which
lists two cells and one allowance. `TestLoadTierConfig_UnknownTier` (`:381-388`) is
unaffected — its bogus tier errors first and the test asserts only `require.Error`.

Chunk `c5` makes those two edits, in the same chunk that invalidates them, so
`internal/perfgate` is never left red for a later chunk to repair. `c10` is then a
verification chunk that authors nothing.

The claim 1.4 protects holds where it matters: no existing **assertion** changes except
the one that asserted the abolished semantics, and every existing test function keeps
its body. What changes is two config **fixtures** — inputs, not behaviour. Recorded here
rather than quietly reinterpreted, and rather than adding a production load option that
would exist only to spare two test literals.

## Risks / Trade-offs

- **The first cross-runner release burns a rerun.** Every latency cell goes
  inconclusive, `perfgate` exits 2, and the workflow spends a full doubled-benchtime
  rerun (`release.yml:168-178`, roughly doubling ~48 s per mode-pass) before `Resolve`
  collapses them all to PASS. The release passes; the rerun is waste. Short-circuiting
  it is a Non-Goal above.
- **A cross-runner release is gated on bytes and allocs alone.** That is the spec's
  intent, but a genuine latency regression that ships on a release landing on
  different hardware is not caught by this gate at all, and GitHub mixes vendors, so
  this will not be rare. Mitigation: the identity is recorded and named in the report,
  so the operator can see which releases were never latency-gated.
- **Every allowance comes from within-run spread on one runner.** The real comparison
  is between two releases months apart, and between-release variation of identical
  code is at least as large. The numbers are deliberately tight, so the failure mode
  is a false FAIL a human reads rather than a silent pass. Expect one on an early
  post-change release, and resist widening without the profile evidence the
  non-widening rule requires. What closes the gap: re-run the same procedure against
  the first pair of real consecutive release baselines, record the observed per-cell
  `|New − Old|` where the code did not change, and raise only what that licenses.
- **`Goldset/queue-promote`'s allowance of 8 is one Go size class**, so on the bytes
  axis alone it could absorb a single 8-byte allocation on a ~16.5 KB cell. The allocs
  axis keeps a zero allowance and would catch it, which is why the number is
  acceptable — and why giving the allocs axis an allowance would make it unsafe.
- **Moving bytes and allocs off benchstat removes a cross-check.** Two independent
  readings of the same figures agree exactly today on all 27 cells, but that agreement
  stops being re-verified on every run.
  `TestReadBenchmarkMetrics_MatchesBenchstat` keeps checking it against the committed
  corpus, which catches a parser regression but not a future benchstat change.
- **Two GitHub-side facts are load-bearing and unverifiable here**: `GH_TOKEN` on
  `workflow_dispatch`, and `gh release view --json assets` against a release with no
  assets. Task 6.2's hosted dispatch is the first place either is exercised, and a
  failure there is a design finding, not an implementation slip.
- **Every dispatch now calls the releases API.** A dispatch by someone without
  `contents: read`, or under a rate limit, now fails where it previously skipped the
  step. That is the intended fail-closed behaviour, but it changes what a dispatch
  costs.
- **`cmd/perfgate` has no test file today**, so the first test there establishes how
  `run` is exercised. If `runBenchstat` is left unseamed, the unpaired-comparison test
  needs a real benchstat invocation and therefore network access on a cold module
  cache. Give `runBenchstat` an injectable seam, or the `cmd` tests become the slowest
  and least reliable in the repo.
- **`TestPinnedProfile` cannot discriminate this change.** It re-evaluates against a
  corpus whose 27 verdicts are already PASS or INCONCLUSIVE, so it can detect a moved
  verdict but not a gate-logic break that happens to preserve verdicts. The
  cross-runner and allowance tests carry the real discrimination; a green
  `TestPinnedProfile` is not evidence this change works.
- **v0.13.0 is cut but not tagged.** Which release ends up carrying the first
  post-change `bench-vm.txt` determines whether the next gate run is first-authorization
  or non-regression. Worth confirming before the cut which release the gate will
  resolve as its baseline.




## Implementation plan

Twelve chunks, sixteen tasks. Eleven form one serial chain because `internal/perfgate`
is a single package with a single test file and `cmd/perfgate` is its only production
caller; the documentation chunk is the one parallel lane. Dispatch order is the order
below. A contract test, once written, is read-only: a later chunk that finds it wrong
raises that rather than editing it.

| chunk | tasks | after | seam | coder | shard |
| --- | --- | --- | --- | --- | --- |
| `c1` | 2.1 | first | runner-identity | `go-coder` | — |
| `c2` | 2.3 | `c1` — shares internal/perfgate | unpaired-comparison-refusal | `go-coder` | — |
| `c3` | 1.1, 2.2 | `c2` — shares internal/perfgate | cross-runner-verdicts | `go-coder` | — |
| `c4` | 3.1 | `c3` — shares internal/perfgate | bytes-allowance-values | `go-coder` | — |
| `c5` | 1.2, 3.2 | `c4` — shares internal/perfgate | bytes-allowance-config | `go-coder` | — |
| `c6` | 1.3, 4.3 | `c5` — shares internal/perfgate | gate-mode-resolution | `go-coder` | — |
| `c7` | 4.1 | `c6` — shares internal/perfgate | gate-mode-resolution | `go-coder` | — |
| `c8` | 4.2 | `c7` — shares .github/workflows | dispatch-publishes-nothing | `zpatcher` | — |
| `c9` | 5.1, 5.2 | parallel (shard `docs`) | gate-documentation | `coder` | docs |
| `c10` | 1.4 | `c8` — shares internal/perfgate | existing-tests-unchanged | `zpatcher` | — |
| `c11` | 6.1 | `c10` — shares whole tree | full-floor | `coder` | — |
| `c12` | 6.2 | `c11` — shares whole tree | hosted-dispatch-preflight | `coder` | — |

### Seam contracts

#### runner-identity — tasks 2.1

Land RunnerIdentity and ReadRunnerIdentity in internal/perfgate, declared and exercised but not yet wired into any verdict. Reads the goos/goarch/cpu preamble out of a raw bench-*.txt, normalises it, and refuses a file whose repeated preambles disagree. Nothing in cmd/perfgate consults it in this seam — cross-runner-verdicts does that, so the member lands before the behaviour that uses it.

**States**

- identity-known: RunnerIdentity with all three fields non-empty; Known() true
- identity-unknown: CPU empty; Known() false; String() renders the literal `unknown` in that position
- identity-inconsistent: the file's repeated preambles disagree; ReadRunnerIdentity returns ErrInconsistentPreamble and a zero RunnerIdentity

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| internal/perfgate/testdata/profile-30637802780/bench-vm.txt | identity-known: {GOOS: "linux", GOARCH: "amd64", CPU: "AMD EPYC 7763 64-Core Processor"}; String() == "linux/amd64/AMD EPYC 7763 64-Core Processor" | `set` | the file's `cpu: AMD EPYC 7763 64-Core Processor` line, repeated 10 times, with 16 trailing spaces that TrimSpace removes |
| internal/perfgate/testdata/profile-30614184386/bench-vm.txt | identity-known: CPU == "INTEL(R) XEON(R) PLATINUM 8573C"; String() == "linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C" | `set` | that file's cpu: line. This is the repo's own cross-runner counterexample and is what cross-runner-verdicts seeds from. |
| a reader over the four-line preamble with the cpu: line removed | identity-unknown: {GOOS: "linux", GOARCH: "amd64", CPU: ""}; Known() false; String() == "linux/amd64/unknown" | `clear` | task 2.1's 'reports its identity as unknown'. Under D1 this is the input that reaches it, not a pre-change baseline. |
| an empty reader | identity-unknown: zero RunnerIdentity; String() == "unknown/unknown/unknown"; no error | `clear` | a file with no preamble carries no identity; refusing it here would make the parser the gate's failure point rather than the comparison |
| a reader whose first preamble says `cpu: A` and whose second says `cpu: B` | identity-inconsistent: ErrInconsistentPreamble, zero RunnerIdentity | `forced` | the workflow appends one `go test` run per sample (release.yml:131-134), so a single file recording two CPUs means the ten samples did not all run on one machine and no single identity describes it |
| two preambles that differ only in trailing whitespace on the cpu: line | identity-known, no error; the two normalise to the same string | `no-op` | the AMD corpora carry 16 trailing spaces; treating that as an inconsistency would reject the repo's own committed files |
| any bench-*.txt already in internal/perfgate/testdata | identity-known; no file in the repo reaches identity-unknown or identity-inconsistent | `no-op` | all six checked-in bench files carry 10 cpu: lines each, verified |

**Forbidden**

- Reading the identity from benchstat's output. benchstat drops the cpu: line entirely under -ignore and reports only one group's preamble otherwise; the raw file is the only faithful source.
- Including `pkg:` in the identity. It names the benchmarked package, not the machine, and would make an unrelated package rename read as a hardware change.
- Writing to, rewriting, or copying any bench-*.txt. The identity is read; nothing is normalised on disk.
- Treating a missing cpu: line as an error, or a present one as optional to compare.
- Any use of ReadRunnerIdentity in cmd/perfgate in this seam — it lands unused on purpose.

**Seeding**

- identity-known: os.Open on internal/perfgate/testdata/profile-30637802780/bench-vm.txt and internal/perfgate/testdata/profile-30614184386/bench-vm.txt. Committed corpora, not constructed.
- identity-unknown and identity-inconsistent: strings.NewReader over a hand-written 3- or 8-line preamble in the test file. These two states are unreachable from any committed corpus, so a literal is the only legal path.
- Never: constructing a RunnerIdentity by hand and asserting String() alone — at least one arm must read a real corpus file so the parser is what is under test.

**Budgets**

- 10: preamble repetitions per checked-in bench file; ReadRunnerIdentity must read all of them, not just the first.
- 3: identity keys read (goos, goarch, cpu). pkg is skipped.
- 2: distinct CPU identities across the three committed profiles (AMD EPYC 7763 64-Core Processor; INTEL(R) XEON(R) PLATINUM 8573C).
- 0: files in the repo that reach identity-unknown.

#### unpaired-comparison-refusal — tasks 2.3

Turn benchstat's silent single-group degradation into a stated refusal. benchstat exits 0 and emits two single-group tables, so the failure has no path today except perfgate's own 'data row too short' - the bare failure the spec is telling the gate to stop producing. parseBlock gains a positive header-shape check that distinguishes single-group from malformed CSV; cmd/perfgate states the reason from the two runner identities and exits 3. Also introduces exit code 3 for gate-configuration failures, separating them from the exit-2 needs-rerun signal.

**States**

- paired: the metric header has 7 fields ending `vs base`,`P`; cells populate as today
- single-group: the metric header has 3 fields ending `CI`; ErrUnpairedComparison; no cells returned
- malformed: the header matches neither shape, or csv.ReadAll rejects the block; the existing generic error, which now means only what it says
- exit-taxonomy: 0 all pass, 1 any fail, 2 any cell needs a rerun, 3 the gate could not be configured or the evidence could not be paired
- post-c3 reachability: once c3 lands, a differing cpu: never reaches this path — perfgate compares identities first and skips benchstat. The remaining triggers are a differing goos:, goarch: or pkg:, and a future benchstat change.
- identities-match precondition: after c3, this path is reachable only when the two runner identities agree — a differing cpu: is handled by the identity comparison before benchstat is consulted

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| internal/perfgate/testdata/profile-30637802780/benchstat.csv | paired: 27 cells, no error | `no-op` | measured: all three blocks read `,sec/op,CI,sec/op,CI,vs base,P`, `,B/op,...`, `,allocs/op,...`, 7 fields each, and records[0] names two distinct files. TestPinnedProfile parses this file (perfgate_test.go:697-700) and must keep passing. |
| a benchstat -format csv run over two files whose cpu: lines differ | single-group: ErrUnpairedComparison naming the metric and the column count | `forced` | measured with the pinned benchstat over the committed profile-30637802780 pair with one cpu: line rewritten: benchstat EXITS 0 and emits six blocks - two single-group tables of three metric blocks each - whose metric headers read `,sec/op,CI` (3 fields) and whose data rows read `Goldset/counter-closure-2,8.507e-06,1%` (3 fields). |
| the same input, under today's code | the generic `perfgate: benchstat csv data row too short` from parse.go:73-75 | `clear` | this is the symptom reported for the v0.13.0 cut, and it is indistinguishable from a malformed CSV. The header check makes the data-row check unreachable for this input. |
| a CSV whose header matches neither shape, or whose rows are ragged | malformed: the existing generic error | `no-op` | parse.go:58-75; csv.ReadAll rejects ragged rows before the header is consulted |
| the same differing-cpu pair with -ignore=cpu | never produced; no code path constructs this argv | `forbidden` | measured: -ignore=cpu yields one paired table AND removes the cpu: line from the output, i.e. it obtains the comparison by disregarding the configuration line the spec forbids disregarding. |
| benchstat's stderr on the measured single-group run | carried into the message for the operator, but never used as the reason | `no-op` | measured: stderr read only `B65: summaries must be >0 to compute geomean`, which is about the geomean and not about pairing. cmd/perfgate/main.go:158-171 discards stderr whenever benchstat exits 0, so today even that is thrown away. |
| LoadTierConfig returns a configuration error | exit 3, not exit 2 | `set` | cmd/perfgate/main.go:55-60 maps every error to exit 2 today, and release.yml:169 treats exit 2 as 'rerun at doubled benchtime', so a configuration error costs a pointless doubled-benchtime rerun before failing. |
| a cell that is genuinely inconclusive | exit 2, unchanged; release.yml's rerun step still fires | `no-op` | release.yml:168-196; the rerun contract is not being changed |

**Forbidden**

- Detecting the single-group case from the data rows. The header row is the positive discriminator; a short data row is also what a malformed CSV produces.
- Passing -ignore, -filter, -col, -row or -table to benchstat. The argv stays exactly `-format csv <old> <new>`.
- Reporting benchstat's stderr AS the pairing reason - on the measured run it says nothing about pairing.
- Rewriting, copying, filtering or truncating either input file before handing it to benchstat.
- Retrying benchstat with different arguments after any failure.
- Removing goos:/goarch:/pkg:/cpu: from benchstatPreamblePrefixes - that filter drops those lines from benchstat's OUTPUT so the CSV parses; it does not touch the inputs and is not what the spec forbids.
- Reusing exit 2 for a configuration failure, or exit 3 for a needs-rerun signal.
- Asserting the two runner identities in this chunk's stderr line. Reading both raw files' identities in cmd/perfgate is c3's codeTask; c2 asserts the single-group reason and exit 3 only, and c3 adds the identities to the message.
- Treating this chunk's exit-3 abort as the handler for a cross-runner pair. It is not: c3 routes those away from benchstat entirely, and an abort here would foreclose the bytes and allocs verdicts spec scenario 1 requires.
- Seeding this chunk's cmd-level tests with two files whose runner identities differ. That input belongs to c3 and is routed away from benchstat.

**Seeding**

- paired: internal/perfgate/testdata/profile-30637802780/benchstat.csv, already committed.
- single-group: SYNTHESIZED, and necessarily so - no single-group benchstat CSV is committed anywhere in the repo, and the two CPU strings the proposal quotes for the failing v0.13.0 cut (`Intel(R) Xeon(R) Platinum 8370C`, `AMD EPYC 9V74`) appear in no committed file either. Produce it once by copying the two profile-30637802780 bench files, rewriting the cpu: line in the copy of bench-evaluator.txt to the CPU string profile-30614184386 actually records (`INTEL(R) XEON(R) PLATINUM 8573C`, so the fixture quotes a machine this project has really used), running the pinned benchstat over the pair, and committing the output verbatim as internal/perfgate/testdata/unpaired-single-group.csv with a README line recording exactly that recipe.
- Committed rather than generated in the test: generating it would make the unit test fetch benchstat over the network on a cold module cache.
- exit-taxonomy: a new cmd/perfgate/main_test.go calling run(stdout, stderr, args) directly and asserting the returned int. cmd/perfgate has no test file today.
- Never: asserting the absence of -ignore by grepping source; assert the error path end to end from the committed fixture instead.
- TestRun_UnpairedComparisonExitsThree and TestRun_ConfigErrorExitsThree: swap the package-level runBenchstat for one returning internal/perfgate/testdata/unpaired-single-group.csv, restoring it with t.Cleanup. No network, no benchstat invocation.
- The unpaired CSV is committed rather than generated: generating it needs benchstat, which is the dependency the seam exists to avoid.
- TestRun_UnpairedComparisonExitsThree seeds -old and -candidate with the two SAME-identity committed files: internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt and .../bench-vm.txt (both record `cpu: AMD EPYC 7763 64-Core Processor`). This is load-bearing after c3: the identity-first ordering routes a differing-identity pair away from benchstat entirely, so seeding differing files would make ErrUnpairedComparison unreachable and turn this sealed test red at c3. The unpaired shape comes from the injected runBenchstat returning the committed unpaired CSV, never from the input files' identities.
- Never seed empty or synthetic temp paths: after c3, ReadBenchmarkMetrics runs over both raw paths and would change the exit path.
- Both cmd-level tests pass `-tiers ../../internal/perfgate/tiers.json` explicitly. The flag's default is `internal/perfgate/tiers.json` (cmd/perfgate/main.go:29), which under `go test ./cmd/perfgate/...` resolves against the package directory and does not exist — os.Open fails at main.go:81-83 and returns exit 3 through main.go:55-60. Without the explicit path TestRun_ConfigErrorExitsThree passes while seeding nothing, and TestRun_UnpairedComparisonExitsThree gets the right exit code for the wrong reason.

**Budgets**

- 7: fields in a paired metric header, ending `vs base` and `P`.
- 3: fields in a single-group metric header, ending `CI`. Both measured.
- 6: blocks in the single-group output - two configurations times three metrics.
- 0: benchstat exit code on the single-group run, which is why nothing upstream notices.
- 4 exit codes: 0, 1, 2, 3.
- 0: retries after a pairing refusal.

#### cross-runner-verdicts — tasks 1.1, 2.2

Wire the identity comparison into the gate and move the bytes and allocs axes off benchstat onto a direct read of the raw bench files, so they survive a differing runner. When the two identities differ every latency cell is INCONCLUSIVE naming both; bytes and allocs are still decided.

**States**

- identities-match: both raw files report the same RunnerIdentity; latency judged as today
- identities-differ: the two RunnerIdentity values differ; every cell's latency verdict is INCONCLUSIVE and the report line names both
- bytes-source: the B/op figure a cell is judged on comes from the median of the raw per-sample B/op fields, never from benchstat
- allocs-source: likewise for allocs/op
- bytes-verdict / allocs-verdict: decided in both identity states
- delta-derived: MetricResult.DeltaPct and .Significant are computed from the two medians, never left at their zero values
- zero-baseline: the baseline median for an axis is 0; DeltaPct is 0 when the candidate is also 0 and +Inf when it is positive
- identity-first: the identity comparison precedes any benchstat invocation, so a differing cpu: never reaches the unpaired path

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| profile-30637802780/bench-evaluator.txt against profile-30637802780/bench-vm.txt | identities-match; latency judged by benchstat as today; TestPinnedProfile's 27 committed verdicts unchanged | `no-op` | both files read `cpu: AMD EPYC 7763 64-Core Processor`, verified |
| profile-30614184386/bench-vm.txt as baseline against profile-30637802780/bench-vm.txt as candidate, ModeNonRegression | identities-differ: linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C against linux/amd64/AMD EPYC 7763 64-Core Processor; every latency cell INCONCLUSIVE | `forced` | the spec's first scenario. Both corpora are committed; this is a real cross-runner VM-vs-VM pair, which is exactly the post-authorization shape that broke the v0.13.0 cut. |
| that same cross-runner pair, bytes axis | bytes-verdict decided, not skipped: each cell's Old and New come from the raw medians and nonIncreasing runs against the cell's stated allowance | `set` | the spec's 'Allocation counts and allocated bytes SHALL stay enforced across differing runners'. Measured feasibility: raw medians reproduce benchstat's Old and New for all 27 cells on both axes in profile-30637802780 with zero mismatches, so this substitution changes no number where a comparison was previously possible. |
| that same cross-runner pair, allocs axis | allocs-verdict decided against a zero allowance | `set` | same measurement; allocs matched benchstat exactly on all 27 cells |
| a cell present in the candidate but absent from the baseline (GoldsetCall/call-boundary is in profile-30637802780 and not in profile-30614184386 or profile-30630796967) | the cell has no baseline figure; its bytes and allocs are reported INCONCLUSIVE naming the missing baseline cell, not passed and not failed | `forced` | verified: profile-30614184386 holds 26 cells, profile-30637802780 holds 27. A missing baseline figure is absence of evidence; judging it against zero would fail every newly added cell. |
| identities-differ, ModeFirstAuthorization | the same rule applies: latency INCONCLUSIVE, bytes and allocs decided | `no-op` | the spec conditions on the identities differing, not on the mode. First authorization pairs two arms of one run, so in practice they never differ — the rule is stated for both modes so the code has no mode-dependent branch. |
| either file reports an unknown identity (no cpu: line) | identities-differ is NOT concluded from an unknown; the comparison is reported INCONCLUSIVE naming `unknown` as one side | `forced` | an unknown identity is not evidence that the runners matched, and equally not evidence that they differed. Treating unknown as a match would let a preamble-less baseline silently license a latency conclusion. |
| an INCONCLUSIVE latency cell from the identity rule, reaching the -rerun invocation | Resolve() collapses it exactly as it collapses any other inconclusive cell: PASS except engine-sensitive under first authorization | `no-op` | perfgate.go:176-181. A cross-runner latency cell is a non-regression claim that was not refuted, so PASS is the burden-of-proof answer ADR 0008:14 already gives. The release still fails on any bytes or allocs regression, which is what makes this safe. |
| a cell whose baseline and candidate medians differ | delta-derived: DeltaPct = (New-Old)/Old*100, Significant true | `set` | perfgate.go:297-303 — nonIncreasing returns PASS on DeltaPct <= 0 before reading New-Old, so an underived DeltaPct passes every cell |
| GoldsetCall/call-boundary, 0 B/op in both files | zero-baseline: DeltaPct 0 | `no-op` | internal/perfgate/testdata/profile-30637802780/bench-vm.txt — the cell reads 0 B/op and 0 allocs/op |
| GoldsetCall/call-boundary, 0 B/op baseline against a positive candidate | zero-baseline: DeltaPct +Inf | `forced` | a first allocation on a zero-byte cell must fail, not divide by zero |
| a cell in the candidate with no baseline row | reported INCONCLUSIVE at the report layer, naming the missing baseline cell | `forced` | cmd/perfgate/main.go:133-137 owns the verdict line; Evaluate is unchanged |
| profile-30614184386/bench-vm.txt against profile-30637802780/bench-vm.txt (cpu: differs) | identity-first: benchstat is not invoked for pairing; the cell loop runs to completion | `forced` | D2 ordering; without it cmd/perfgate/main.go:95-98 returns (0, err) and exits before the cell loop at :109 |

**Forbidden**

- Concluding a latency PASS or FAIL from a comparison whose two identities differ.
- Skipping, defaulting or zeroing the bytes or allocs verdict when identities differ.
- Taking bytes or allocs from benchstat once the raw reader exists — one source per axis, or the two can disagree silently.
- Judging a cell whose baseline figure is missing against an Old of 0.
- Treating an unknown identity as equal to any other identity, including another unknown.
- Changing perfgate.Evaluate's signature or its per-tier rules. The identity rule lives above it.
- Leaving MetricResult.DeltaPct or .Significant at their zero values when populating Bytes or Allocs from raw medians.
- Computing (New-Old)/Old without a guard on Old == 0.
- Adding a state to Verdict or changing Evaluate's signature to express the missing-baseline case.
- Invoking benchstat for a paired comparison when the two runner identities differ. That is the path that aborts before the cell loop and silently forecloses the bytes and allocs verdicts the spec requires.
- Leaving the precedence between exit 3, exit 1 and exit 2 to fall out of control flow.
- Assuming the two input files carry the same number of samples per cell.
- Authoring a test in internal/perfgate against logic that lives in cmd/perfgate. internal/perfgate cannot import package main, so such a test asserts against a reimplementation and goes green while the shipped binary is untested on this seam's central requirement.
- Moving the identity comparison or the report-layer missing-baseline line into internal/perfgate to make a misplaced test compile. If that relocation is wanted it is a design change, not a test fix.
- Exposing only the median from SampleMetrics. c4 needs the per-sample figures and must not reimplement the parser to get them.

**Seeding**

- identities-match: the profile-30637802780 pair, committed.
- identities-differ: profile-30614184386/bench-vm.txt against profile-30637802780/bench-vm.txt, both committed. No synthetic file.
- bytes-source / allocs-source: ReadBenchmarkMetrics over the committed raw files; the test cross-checks its output against the committed benchstat.csv Old/New columns for the matching-identity profile, which is the measurement that licenses the substitution.
- missing-baseline-cell: GoldsetCall/call-boundary, present in profile-30637802780 and absent from profile-30614184386. Already committed; do not construct one.
- unknown identity: strings.NewReader over a preamble with the cpu: line removed, as in the runner-identity seam.
- Never: hand-constructing a CellComparison to reach identities-differ. The state must come from two real files, because the whole defect was that the identity was never read.
- zero-baseline: GoldsetCall/call-boundary in the committed profile-30637802780 corpus, which genuinely reads 0 B/op — not a constructed literal.
- cmd-level tests: cmd/perfgate/main_test.go already exists at c3 because c2 creates it. Raw corpora are reached by relative path from cmd/perfgate; the identity-first path never calls benchstat, so those tests need no seam substitution.

**Budgets**

- 27: cells judged when both files are profile-30637802780; 26 when the baseline is profile-30614184386.
- 1: cells whose bytes and allocs are inconclusive for want of a baseline figure on that cross-runner pair (GoldsetCall/call-boundary).
- 10: samples per cell per file, from which the median is taken.
- 0: numbers that change on the matching-identity path — verified against all 27 cells on both axes.
- 2: identity strings named in an INCONCLUSIVE latency report line.
- 10: samples per cell per file in every committed corpus — an EVEN count, so "the median" is a choice (lower middle, upper middle, or the mean of the two). The convention must be the one benchstat uses; the coder verifies it against benchstat's own definition rather than assuming, because 14 of the 27 cells in profile-30637802780 have zero spread and cannot discriminate between conventions.
- Sample counts may differ between the -old and -candidate files: the rerun step re-measures at doubled benchtime (release.yml:168-178) while a stored baseline keeps its original count. The reader must not assume equal counts.

#### bytes-allowance-values — tasks 3.1

Give all 27 tiers.json cells a bytesAllowanceBOp entry derived from that cell's observed within-run B/op spread, and add the test that recomputes every number from the committed corpora so no allowance can outgrow its evidence. Lands BEFORE bytes-allowance-config, which turns a missing entry into an error: reversing the order breaks every gate invocation and TestPinnedProfile with it.

**States**

- allowance-stated: every one of the 27 cells has a bytesAllowanceBOp entry
- allowance-justified: for every cell except Goldset/guard-nil, entry <= max observed within-run spread across the committed profiles
- allowance-zero: the 13 GoldsetParse/* cells and GoldsetCall/call-boundary state 0, which is the exact non-increasing bound

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| the 13 GoldsetParse/* cells | allowance-zero: stated 0; bit-identical B/op across all ten samples in both profiles | `set` | supplied spread measurement: every GoldsetParse cell reads 0 spread in profile-30630796967 and 0 in profile-30637802780. A stated 0 preserves the invariant tiers.json's comment and ADR 0008:60-62 record. |
| GoldsetCall/call-boundary | allowance-zero: stated 0; spread 0 in profile-30637802780 and absent from profile-30630796967 | `set` | supplied measurement (0 B/op, bit-identical); the cell's absence from the older profile is verified and means its evidence base is one profile, not two |
| Goldset/queue-promote | allowance 8, the largest in the table; spread 7 in profile-30630796967 and 8 in profile-30637802780 over a ~16.5 KB figure | `set` | supplied measurement (16582..16593). 8 equals one Go size class, so this allowance alone could absorb one 8-byte allocation on the bytes axis; the allocs axis keeps a zero allowance and would catch it. |
| Goldset/rule-load | allowance 6; spread 3 then 6 (5840..5848) | `set` | supplied measurement. This is the startup cell, so its allowance reaches nonIncreasing through evaluateStartup (perfgate.go:263). |
| Goldset/guard-nil | allowance 4, unchanged, exempt from the spread rule | `no-op` | its within-run spread is 0; the 4 covers a reproducible between-engine offset of +1 B/op licensed by ADR 0008:50-74 and three named hosted runs. Re-deriving it is out of scope. |
| the remaining nine Goldset/* cells | allowance = max observed spread: counter-closure 1, kw-lookup 1, loop-sum 2, merge-config 3, pipeline 2, registry-fold 2, route-decision 1, safe-parse 2, text-render 1 | `set` | supplied per-cell spread measurement across profile-30630796967 and profile-30637802780 |
| a future edit raising any allowance above the spread the committed corpora record | TestBytesAllowancesAreJustifiedBySpread fails, naming the cell, the stated value and the measured spread | `forced` | the spec's 'SHALL NOT be widened to admit a measured regression'. Bounding the allowance by within-run spread makes it structurally unable to admit a regression: a regression shifts the median while leaving the spread where it was. |
| the pinned profile re-evaluated with all 27 allowances in place | every committed verdict in profile-30637802780/verdict.txt is unchanged | `no-op` | verified: engine-sensitive cells take the inline 20% bytes floor no allowance reaches (perfgate.go:293-296); the allowance-reading cells under first authorization are the 13 GoldsetParse (delta 0), guard-nil (unchanged) and rule-load (bytes -36.22%, so nonIncreasing passes at DeltaPct <= 0 before the allowance is consulted). |

**Forbidden**

- A size-proportional or percentage allowance. The spec ties the number to observed spread on that cell.
- Rounding an allowance up to a size class, or to any value above the measured spread.
- Giving Goldset/guard-nil a new derivation, or removing its 4.
- Recording a number without the profile ids it came from.
- Raising an allowance in the same change as a bytes regression it would admit.
- Deriving any number from a developer-box run. tiers.json's comment already states a developer-box figure licenses nothing, and only bytes and allocs are locally exact in any case.
- Deriving any number from profile-30630796967: its committed files are a 400ms measurement and would understate the spread the gate sees at 200ms.
- Deriving a number from a difference taken between two profiles. They measure different trees at different benchtimes; such a figure blends code change, benchtime and hardware and presents it as noise.
- Leaving tiers.json:2's "no allowance" sentence in place. It is false the moment every cell carries an entry, and c9 cannot fix it — this chunk owns that file.

**Seeding**

- profile-30637802780/bench-vm.txt alone is the evidence base: ten samples at the gate's own BENCHTIME of 200ms (release.yml:66). For each cell, span = max(B/op) - min(B/op) over those ten samples; the allowance is that span.
- profile-30630796967 is a directional check only. Its committed files are a 400ms rerun measurement (its README.md:4-17, 57092 iterations against 28188), and B/op is a total over an iteration count, so twice the iterations halves the rounding granularity and every one of its spreads is <= its 200ms counterpart.
- profile-30614184386 is the cross-runner fixture, not an allowance source: it records a third CPU.
- Spread is recomputed through c3's SampleMetrics per-sample accessor, not by a second raw-file parser in the test.

**Budgets**

- 27: cells in tiers.json — 13 Goldset/*, 1 GoldsetCall/call-boundary, 13 GoldsetParse/*.
- 13: entries with a non-zero value, all of them Goldset/* cells (guard-nil among them, keeping its existing 4).
- 14: entries stating 0 — the 13 GoldsetParse/* cells plus GoldsetCall/call-boundary, every one bit-identical across all ten samples.
- 1: profiles in the evidence base.
- 8: the largest allowance in the table (Goldset/queue-promote), and one Go size class. Safe only because the allocs axis keeps a zero allowance.
- Expected table, all 27 entries: Goldset/counter-closure 1, Goldset/guard-nil 4 (the between-engine exception, unchanged), Goldset/kw-lookup 1, Goldset/loop-sum 2, Goldset/merge-config 3, Goldset/pipeline 2, Goldset/queue-promote 8, Goldset/registry-fold 2, Goldset/route-decision 1, Goldset/rule-load 6, Goldset/safe-parse 2, Goldset/text-render 1, Goldset/twice-macro 2 — thirteen non-zero, every one a Goldset/* cell. Then GoldsetCall/call-boundary 0 and each of the thirteen GoldsetParse/* cells 0 — fourteen stated zeros. Twenty-seven entries.

#### bytes-allowance-config — tasks 1.2, 3.2

Make an absent bytesAllowanceBOp entry a configuration error instead of an implicit zero. Requires changing tierConfigFile.BytesAllowanceBOp to map[string]*float64, because a stated 0 and an absent key are indistinguishable after decoding into map[string]float64 today. Lands AFTER bytes-allowance-values.

**States**

- allowance-present-nonzero: the cell states a positive allowance; nonIncreasing gets it
- allowance-present-zero: the cell states 0; nonIncreasing gets 0, the exact non-increasing bound
- allowance-absent: the cell is in `cells` and not in `bytesAllowanceBOp`; LoadTierConfig fails
- allocs-allowance: always 0, never configurable
- TestEvaluate_BytesAllowance (perfgate_test.go:590-650) is NOT affected: it builds CellComparison literals and calls Evaluate directly, never constructing a tier config. Its "unlisted cell gets no allowance" subtest name is stale wording in a passing test, left alone.

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| the shipped tiers.json after bytes-allowance-values | all 27 cells present; LoadTierConfig succeeds | `no-op` | TestPinnedProfile loads the real tiers.json (perfgate_test.go:702-705) and must keep passing |
| a tier config naming a cell in `cells` with no `bytesAllowanceBOp` entry | allowance-absent: error satisfying errors.Is(err, ErrMissingBytesAllowance), message naming the cell | `forced` | the spec's 'the gate SHALL fail with a missing-config error when a cell it is asked to judge has none, rather than reading the absence as an allowance of zero' |
| a tier config stating `"GoldsetParse/kw-lookup": 0` | allowance-present-zero: loads cleanly; CellTier{BytesAllowanceBOp: 0, BytesAllowanceStated: true} | `set` | the reading taken in the design's D4: an explicit 0 is a stated allowance enforcing the exact non-increasing bound; an absent entry is the error — a stated 0 is a recorded decision, not silence, and enforces the same exact bound |
| a tier config whose bytesAllowanceBOp names a cell absent from `cells` | the existing error is unchanged: `perfgate: bytesAllowanceBOp names cell %q, absent from cells` | `no-op` | parse.go:216-220, already covered by TestLoadTierConfig_BytesAllowance_UnknownCell (perfgate_test.go:406) |
| a negative allowance, e.g. -4 | rejected with a stated error naming the cell | `forced` | nonIncreasing compares `m.New-m.Old <= allowanceBOp` (perfgate.go:301); a negative allowance would fail a cell whose bytes did not increase, which no requirement asks for. Nothing rejects it today. |
| any cell's allocs axis | allocs-allowance stays 0 and stays unreachable from tiers.json | `no-op` | perfgate.go:194, :221, :242, :266 all pass a literal 0; the spec's 'Allocation counts are exact in the same output and SHALL keep a zero allowance' |

**Forbidden**

- Defaulting an absent entry to 0 anywhere, including in cmd/perfgate.
- Adding an allocs allowance key to tiers.json or an allowance parameter to the allocs call sites.
- Reporting the missing-allowance failure as a per-cell Result{Verdict: VerdictFail}. It is a configuration defect and must stop the run, not be reported as one cell's measurement.
- Letting a configuration error exit 2 and trigger the doubled-benchtime rerun.
- Changing LoadTierConfig's signature.
- Leaving internal/perfgate red for a later chunk to repair. The chunk that makes an absent allowance an error owns every existing fixture that absence made valid.

**Seeding**

- allowance-present-*: the shipped internal/perfgate/tiers.json for the happy path; strings.NewReader over a small hand-written JSON literal for the single-cell arms, matching how TestLoadTierConfig and TestLoadTierConfig_BytesAllowance already seed (perfgate_test.go:369-418).
- allowance-absent: a hand-written literal with one cell in `cells` and an empty `bytesAllowanceBOp`. Unreachable from the shipped file after bytes-allowance-values, so a literal is the only legal path.
- Never: deleting an entry from the real tiers.json inside a test.

**Budgets**

- 27: cells that must each state an allowance.
- 0: the allocs allowance, at all four call sites.
- 3: the exit code a missing allowance produces.
- 1: the number of load-time passes over the cell set — the check runs inside LoadTierConfig, not per judged cell.

#### gate-mode-resolution — tasks 1.3, 4.1, 4.3

Move the mode decision out of inline shell into a pure Go function plus a `resolve-mode` subcommand, and restructure the workflow step to observe absence and failure separately. Removes `if: github.event_name == 'release'` from the mode step so a dispatch resolves the same mode a release would.

**States**

- baseline-found: a stored bench-vm.txt was enumerated and downloaded; ModeNonRegression
- baseline-absent: every enumerated release was inspected and none carries the asset; ModeFirstAuthorization
- enumeration-failed: listing releases or listing one release's assets failed; ModeUnknown and an error
- download-failed: the asset was seen in a release's asset list and the download then failed; ModeUnknown and an error

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| BaselineLookup{EnumerationOK: true, Tags: ["v0.12.0"], DownloadedTag: "v0.12.0"} | baseline-found: (ModeNonRegression, BaselineFound, nil) | `set` | release.yml:105-107 selects non-regression on exactly this observation today |
| BaselineLookup{EnumerationOK: true, Tags: ["v0.11.0","v0.12.0"], DownloadedTag: ""} | baseline-absent: (ModeFirstAuthorization, BaselineAbsent, nil) | `set` | the spec's 'First-authorization SHALL be selected only when the repository is known to hold no baseline'. Known, here, means every enumerated tag was inspected and none carried the asset. |
| BaselineLookup{EnumerationOK: false} | enumeration-failed: (ModeUnknown, BaselineEnumerationFailed, ErrBaselineEnumerationFailed) | `forced` | defect 4: `gh release list` is unchecked at release.yml:98, so an API error yields an empty tag list and silently selects first-authorization improvement thresholds |
| BaselineLookup{EnumerationOK: true, Tags: ["v0.12.0"], DownloadFailed: true} | download-failed: (ModeUnknown, BaselineDownloadFailed, ErrBaselineDownloadFailed) | `forced` | the spec's 'enumerating or downloading the stored baseline fails for any reason other than the baseline not existing' |
| BaselineLookup{EnumerationOK: true, Tags: []} | baseline-absent: a repository with no releases at all holds no baseline; ModeFirstAuthorization | `set` | this is the genuine first-authorization case the mode exists for |
| the zero BaselineLookup | enumeration-failed: EnumerationOK is false by zero value, so an unpopulated lookup fails closed rather than selecting first-authorization | `forced` | the same fail-closed reasoning perfgate.go:33-38 and :44-50 apply to ModeUnknown and VerdictUnknown |
| a workflow_dispatch run against a tree whose repository holds a v0.13.0 baseline | baseline-found: the dispatch reports non-regression, the same mode the release run reaches | `forced` | the spec's 'Dispatch and release agree on the rule'; task 4.1 and, on a hosted runner, task 6.2 |
| RELEASE_TAG empty on a dispatch run | the skip line `[ "$tag" = "$RELEASE_TAG" ] && continue` matches nothing, so the newest release is considered first | `no-op` | release.yml:99. On a dispatch there is no release being cut, so there is nothing to exclude. |

**Forbidden**

- Returning ModeFirstAuthorization together with a non-nil error, or any Mode other than ModeUnknown on a failure.
- Deciding the mode from github.event_name anywhere.
- Treating a non-zero `gh release download` as absence. Absence is decided from the asset list, never from a download exit code.
- Leaving `gh release list` unchecked.
- Publishing a baseline or uploading a release asset from the resolve-mode path — it reads only.
- Making resolve-mode a second binary. It is a subcommand of the existing cmd/perfgate so the workflow builds one thing.
- Authoring a test that invokes `perfgate resolve-mode`. The subcommand lands in c7; a CLI test here fails this chunk's own verify.
- Editing .github/workflows/release.yml in this chunk. The workflow half of task 4.3 is carried by c7, which restructures the same step.

**Seeding**

- All four states: a BaselineLookup struct literal in internal/perfgate. No network, no gh, no filesystem — the point of the seam is that the classification is pure.
- Never: shelling out to gh from a test, or asserting the workflow YAML's shell body from Go.
- This chunk is the pure-Go arm only: ResolveGateMode is exercised by constructing BaselineLookup values in internal/perfgate. The `perfgate resolve-mode` subcommand does not exist yet — c7 adds it — so no CLI-arm test may be authored here.

**Budgets**

- 4: outcomes in the taxonomy (found, absent, enumeration-failed, download-failed).
- 1: outcome that selects ModeFirstAuthorization.
- 30: the release enumeration limit, unchanged from release.yml:98.
- 3: the exit code of a resolution failure.
- 0: release assets written by the resolve-mode path.

#### dispatch-publishes-nothing — tasks 4.2

NO-RED-WAIVER: no Go test can observe which Actions steps ran, so this seam authors no contract test. It is not unverified: the chunk's verify asserts that the workflow holds exactly one `gh release upload` and that the step holding it still carries the release guard — the two facts spec line 56 turns on. Counting occurrences pins behaviour, not indentation.

**States**

- dispatch-run: github.event_name == 'workflow_dispatch'; mode resolved, benchmarks run, verdict computed, no release asset written
- release-run: github.event_name == 'release'; the same plus the baseline upload

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| a workflow_dispatch run after gate-mode-resolution lands | dispatch-run: `Determine gate mode` now executes; `Store VM baseline on the authorized release` does not | `no-op` | .github/workflows/release.yml:210-215 keeps `if: github.event_name == 'release'`; it holds the only `gh release upload` in the file |
| the `Upload release evidence` step on a dispatch run | runs, as it does today, and writes a workflow artifact — not a release asset | `no-op` | release.yml:220-230 uses actions/upload-artifact and `if: always()`. Task 4.2 concerns release assets, so this step is unaffected. |
| the baseline download inside the ungated `Determine gate mode` step on a dispatch | reads only; downloads baseline-vm.txt into the workspace and publishes nothing | `no-op` | `gh release download` and `gh release view` are read operations; the step writes only $GITHUB_OUTPUT and a workspace file |

**Forbidden**

- Removing `if: github.event_name == 'release'` from `Store VM baseline on the authorized release`.
- Adding a `gh release upload`, `gh release create` or `gh release edit` to any ungated step.
- Gating the upload on the resolved mode instead of the event — a dispatch that resolves first-authorization would then publish a baseline.
- Quoting the literal string `gh release upload` inside the added comment. This chunk's verify counts occurrences of it and expects exactly one; word the comment around the command name.

**Seeding**

- Reading .github/workflows/release.yml at the seam's completion and confirming exactly one step carries the release guard and exactly one `gh release upload` exists, and that they are the same step.
- Never: asserting YAML text from a Go test.

**Budgets**

- 1: steps carrying `if: github.event_name == 'release'` after this change.
- 1: `gh release upload` invocations in the file.
- 0: release assets a dispatch run writes.

#### gate-documentation — tasks 5.1, 5.2

NO-RED-WAIVER: documentation only. ADR 0008 and CHANGELOG.md carry no executable behaviour, and a substring test over either would pin prose formatting rather than a rule. Amends the two texts that state the overturned-looking invariant and records the user-visible change.

**States**

- adr-amended: ADR 0008 states the runner-comparability rule and the exact-allocs vs averaged-bytes distinction
- changelog-recorded: CHANGELOG.md's [Unreleased] -> Changed names the inconclusive-across-runners behaviour

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| ADR 0008:60-62, 'The other thirteen data-dominated cells — every GoldsetParse/* cell — keep the exact non-increasing bound with no allowance.' | rewritten to say every cell states an allowance and that those thirteen state an explicit 0, which IS the exact non-increasing bound | `forced` | the sentence describes an absent entry; after bytes-allowance-config an absent entry is a configuration error. The rule it states is preserved; only its mechanism changes. |
| ADR 0008's Thresholds section | gains the runner-comparability rule: a latency conclusion requires matching runner identities, allocation counts and allocated bytes stay enforced regardless, and the gate never normalises a configuration line to obtain a comparison | `set` | task 5.1 |
| ADR 0008's Note on benchstat '~' (lines 34-40) | amended to record that the bytes and allocs axes no longer pass through benchstat at all, so the blind spot it describes now applies to latency only | `set` | cross-runner-verdicts moves both axes onto a direct read of the raw files. Leaving this note unamended would leave the ADR describing a gap the change closed. |
| CHANGELOG.md [Unreleased] -> Changed | one entry naming the inconclusive-latency-across-runners behaviour | `set` | task 5.2. Keep a Changelog 1.1.0 shape, matching the file's existing sections. |

**Forbidden**

- Describing this as a relaxation of the GoldsetParse/* bound. The bound is unchanged; only its representation moved from absence to an explicit 0.
- Recording per-cell allowance numbers in the ADR. They belong in tiers.json, which the ADR already points at.
- Creating a new ADR. 0008 is the single owner of these numbers (ADR 0008:19).
- Claiming the benchstat '~' blind spot is fully closed — it remains open for latency.
- Editing internal/perfgate/tiers.json. Its comment is c4's, and this chunk runs in parallel with the serial chain that owns that file.

**Seeding**

- Read docs/adr/0008-consumer-performance-gate.md and internal/perfgate/tiers.json before editing; both carry the same sentence and both must move together.
- Read CHANGELOG.md's existing [Unreleased] section for its heading shape.

**Budgets**

- 2: texts carrying the sentence that changes (ADR 0008:60-62 and tiers.json:2).
- 1: CHANGELOG entry.
- 0: new ADRs.

#### existing-tests-unchanged — tasks 1.4

NO-RED-WAIVER: a re-run of the existing suite against the changed production code, authoring no assertion. The deliverable is that every existing test function in internal/perfgate/perfgate_test.go passes. Two tier-config fixtures were updated in c5 — inputs, not assertions — which is the whole of what task 1.4's "unchanged" gives up, and it is stated in the design rather than reinterpreted.

**States**

- suite-green: every existing test function passes with no edit to its body
- pinned-verdicts-unchanged: TestPinnedProfile's 27 committed verdicts still match

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| TestPinnedProfile after all 27 allowances land | pinned-verdicts-unchanged | `no-op` | verified: no allowance-reading cell's verdict moves. Engine-sensitive cells use the inline bytes floor no allowance reaches; the 13 GoldsetParse cells read delta 0; guard-nil keeps 4; rule-load's bytes read -36.22%, passing before the allowance is consulted. |
| TestPinnedProfile after tierConfigFile.BytesAllowanceBOp becomes map[string]*float64 | suite-green: the test reads tiersFile.Comment only (perfgate_test.go:707-710), not the allowance map | `no-op` | perfgate_test.go:707-710 unmarshals tierConfigFile and touches only Comment |
| TestLoadTierConfig and TestLoadTierConfig_BytesAllowance, whose literals name cells with no allowance entry | these WILL fail once a missing allowance is an error, and must be updated — the only existing tests this change edits | `forced` | perfgate_test.go:369-391 seed tier configs from literals that carry no bytesAllowanceBOp. Naming them here rather than discovering them mid-implementation: task 1.4's 'unchanged' holds for 26 of the 28 functions, not all 28. |
| the 13 Evaluate/Resolve tests | suite-green: they construct CellComparison values directly and never touch tiers.json or benchstat | `no-op` | perfgate_test.go:18-313; perfgate.Evaluate's signature and rules are unchanged by every seam here |

**Forbidden**

- Editing any existing test body other than the two tier-config literals named above.
- Updating profile-30637802780/verdict.txt. If a verdict moves, the change is wrong, not the oracle.
- Updating pinnedBenchEvaluatorSHA256 or pinnedBenchVMSHA256 (perfgate_test.go:674-675). No seam here touches those two files.
- Editing any test in this chunk. c5 made the two fixture edits; this chunk runs the package and reports.

**Seeding**

- go test -timeout 2m ./internal/perfgate/ with no -run filter, so the whole package runs rather than a narrowed subset.

**Budgets**

- 28: existing test functions in internal/perfgate/perfgate_test.go.
- 2: existing test functions this change must edit, both of them in c5, not here (TestLoadTierConfig, TestLoadTierConfig_BytesAllowance).
- 27: pinned verdicts that must not move.
- 0: edits to any committed corpus file.

#### full-floor — tasks 6.1

NO-RED-WAIVER: the whole-change floor. Runs the repository suite, the race suite over the three trees task 6.1 names, go vet and the linter, and records each command's exit status. Authors no assertion.

**States**

- floor-status: pass or fail per command, in order

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| each command in fullFloor, run in order at the repository root | floor-status = pass | `forced` | task 6.1; make test is Makefile:14-15, make lint is Makefile:19-20 |

**Forbidden**

- Reporting a green floor from a narrowed -run.
- Dropping -timeout from any run, or running the race suite without a wall-clock limit.
- Substituting go build ./... for make test — go build never compiles _test.go, so a non-compiling test file passes it.
- Judging any latency figure from a local run.

**Seeding**

- Run from /home/zhuk/Projects/own/go-lispico with no environment overrides; GOTESTFLAGS stays at the Makefile default.

**Budgets**

- -timeout 2m for the non-race suite (Makefile:3); -timeout 10m for the race suite.

#### hosted-dispatch-preflight — tasks 6.2

NO-RED-WAIVER: requires a hosted GitHub Actions runner and cannot be executed by an implementer locally. Latency figures from a developer box are not trustworthy for this gate in any case. The deliverable is the dispatch run URL plus the recorded mode line, not a local command.

**States**

- dispatch-mode-recorded: the dispatched run's `Determine gate mode` step output names non-regression and the baseline tag it resolved
- no-asset-published: the dispatched run's `Store VM baseline on the authorized release` step is skipped

**Transitions**

| input | state | effect | evidence |
| --- | --- | --- | --- |
| gh workflow run against the release-candidate tree, once the repository holds a v0.13.0 baseline | dispatch-mode-recorded: mode=non-regression, baseline_tag=<the newest release carrying bench-vm.txt> | `forced` | the spec's 'Dispatch and release agree on the rule'; today the step is skipped entirely on a dispatch (release.yml:92) and the run falls through to first-authorization at :150 |
| the same run's step list | no-asset-published: the upload step reports skipped | `no-op` | release.yml:211, unchanged by this work |
| the same run's latency verdicts, if the hosted runner's CPU differs from the stored baseline's | every latency cell INCONCLUSIVE naming both identities; bytes and allocs still decided | `forced` | this is the end-to-end confirmation of cross-runner-verdicts, and the only place it can be observed against real hardware variation |

**Forbidden**

- Substituting a local run for the dispatch and reporting it as task 6.2.
- Dispatching against a branch that is not the release-candidate tree — `gh workflow run --ref` resolves against the remote, not the local worktree.
- Reading a first-authorization result as acceptable because 'no baseline was found' without checking whether enumeration failed.

**Seeding**

- gh workflow run on the Release consumer gate workflow, --ref pointing at the pushed release-candidate branch; then read the run's `Determine gate mode` step output and the step list.
- Never: a local `act` or shell reproduction — the defect being fixed is about the hosted API's failure modes.

**Budgets**

- 1: dispatch run required.
- 0: release assets it may publish.
- 2: facts recorded from it — the resolved mode line and the skipped-upload step.

### Chunks

#### `c1` — tasks 2.1 — seam runner-identity

First chunk in the serial chain. Coder `go-coder`.

Sealed test scope: `internal/perfgate`.

**Sites**

- `internal/perfgate/parse.go` — `benchstatPreamblePrefixes / parseBlock (identity reader is NEW)` — anchor `var benchstatPreamblePrefixes = []string{"goos:", "goarch:", "pkg:", "cpu:"}`
  - The cpu: line is the runner identity and is currently discarded here (parseBlock skips any line matching these prefixes via hasAnyPrefix, parse.go:49). Add a reader that extracts the identity from a raw bench-*.txt preamble and returns unknown when no cpu: line is present, so a baseline stored before this change does not crash. Under D1 task 2.1 needs no workflow change: the identity is already inside the uploaded file.
- `internal/perfgate/runner_identity.go` — `RunnerIdentity, ReadRunnerIdentity, ErrInconsistentPreamble` — anchor `NEW FILE — lands beside internal/perfgate/parse.go`
  - New file. NEW: the identity type, its String() and Known(), the raw-preamble reader, and the inconsistent-preamble sentinel. Lands declared and unused; c3 consumes it.
- `internal/perfgate/runner_identity_test.go` — `TestReadRunnerIdentity` — anchor `NEW FILE — lands beside internal/perfgate/perfgate_test.go`
  - New file. NEW: the contract test for the reader, seeded from the committed corpora plus literal readers for the unknown and inconsistent states.

**Red**

- 2.1 Author internal/perfgate/runner_identity_test.go: TestReadRunnerIdentity over the two committed corpora, the missing-cpu reader, the empty reader, the inconsistent-preamble reader, and the trailing-whitespace pair.
- Red shape: the first run is a BUILD failure, because TestReadRunnerIdentity names symbols this chunk's own codeTasks create. That is the ordinary Go shape and nothing is out of order, but a non-compiling package is not evidence the assertions are right — after the symbols land declared, re-run and confirm the failure is an assertion failure before handing over.

**Red tests** (binding — an absent name fails RED): `TestReadRunnerIdentity`

**Code**

- 2.1 Add internal/perfgate/runner_identity.go: RunnerIdentity, its String() and Known(), ReadRunnerIdentity, ErrInconsistentPreamble.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c2` — tasks 2.3 — seam unpaired-comparison-refusal

Serial after `c1`, sharing internal/perfgate. Coder `go-coder`.

Sealed test scope: `internal/perfgate`, `cmd/perfgate`.

**Sites**

- `cmd/perfgate/main.go` — `runBenchstat` — anchor `cmd := exec.Command("go", "run", "golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d", "-format", "csv", oldPath, newPath)`
  - Verification site: this arg list must never gain -ignore (or any preamble-stripping step), and a benchstat refusal must surface as the existing 'benchstat: %w: %s' error with benchstat's own stderr. The parse-side symptom of a declined pairing is 'perfgate: benchstat csv data row too short' from parseBlock (parse.go:74), which is a parse error rather than a stated refusal.
- `internal/perfgate/parse.go` — `parseBlock, ErrUnpairedComparison` — anchor `if len(records) < 3 {`
  - Classify the metric header row before any data row is read: 7 fields ending vs base/P is paired, 3 fields ending CI is single-group and returns ErrUnpairedComparison, anything else keeps the existing generic error.
- `cmd/perfgate/main_test.go` — `TestRun_UnpairedComparisonExitsThree, TestRun_ConfigErrorExitsThree` — anchor `NEW FILE — the package has no test file today`
  - New file. NEW: the first tests in cmd/perfgate, exercising run() through the injectable benchstat seam.
- `internal/perfgate/testdata/unpaired-single-group.csv` — `unpaired fixture` — anchor `NEW FILE — lands under internal/perfgate/testdata/`
  - New file. NEW: benchstat output over a pair whose cpu: lines differ, committed rather than generated so no test invokes benchstat. A README line records how it was produced.

**Red**

- 2.3 Author TestParseBenchstatCSV_UnpairedSingleGroup: the committed fixture yields errors.Is(err, ErrUnpairedComparison) and a message containing `single-group`.
- 2.3 Author TestParseBenchstatCSV_MalformedIsNotUnpaired: a ragged or wrong-header CSV yields an error that does NOT satisfy errors.Is(err, ErrUnpairedComparison), so the two failures stay distinguishable.
- 2.3 Author TestParseBenchstatCSV_PairedHeaderStillParses over the committed benchstat.csv.
- 2.3 Author cmd/perfgate/main_test.go: TestRun_UnpairedComparisonExitsThree asserting exit 3 and the single-group reason in the stderr line (NOT the runner identities — c3 adds those); TestRun_ConfigErrorExitsThree.
- Red shape: the first run is a BUILD failure, because ErrUnpairedComparison names symbols this chunk's own codeTasks create. That is the ordinary Go shape and nothing is out of order, but a non-compiling package is not evidence the assertions are right — after the symbols land declared, re-run and confirm the failure is an assertion failure before handing over.

**Red tests** (binding — an absent name fails RED): `TestParseBenchstatCSV_UnpairedSingleGroup`, `TestRun_UnpairedComparisonExitsThree`, `TestRun_ConfigErrorExitsThree`

**Code**

- 2.3 parse.go: add ErrUnpairedComparison; in parseBlock, classify records[1] three ways before reading any data row and return ErrUnpairedComparison for the single-group shape.
- 2.3 cmd/perfgate/main.go: capture benchstat's stderr on the success path; on errors.Is(err, ErrUnpairedComparison) re-report naming the single-group shape and the offending metric block (the runner identities are c3's — they are not read in this chunk); introduce exit code 3 for configuration and pairing failures; update the exit-code contract in the evaluate doc comment at :74-79.
- 2.3 Commit internal/perfgate/testdata/unpaired-single-group.csv plus the README line recording how it was produced.
- 2.3 cmd/perfgate/main.go: give runBenchstat an injectable seam — a package-level `var runBenchstat = func(oldPath, newPath string) ([]byte, error)` that evaluate calls through, so a test can supply the committed unpaired CSV without `go run`-ing benchstat over the network. Without it the cmd-level tests either need network on a cold module cache or assert nothing.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c3` — tasks 1.1, 2.2 — seam cross-runner-verdicts

Serial after `c2`, sharing internal/perfgate. Coder `go-coder`.

Sealed test scope: `internal/perfgate`, `cmd/perfgate`.

**Sites**

- `internal/perfgate/perfgate_test.go` — `cross-runner assertions — authored in cmd/perfgate/main_test.go, not here` — anchor `// TestEvaluate_BytesAllowance guards the per-cell absolute bytes allowance`
  - No new test lands in this file for the cross-runner rule. The identity comparison and the latency override live in cmd/perfgate's evaluate, which internal/perfgate cannot import, so TestCrossRunner_LatencyInconclusiveBytesEnforced, TestCrossRunner_UnknownIdentityIsNotAMatch and TestCrossRunner_MissingBaselineCellIsInconclusive are authored in cmd/perfgate/main_test.go. This file gains only the ReadBenchmarkMetrics derivation tests. Evaluate's signature and CellComparison's fields are unchanged.
- `internal/perfgate/perfgate.go` — `Evaluate` — anchor `func Evaluate(cell CellComparison, tier Tier, mode Mode) Result {`
  - A runner mismatch must short-circuit the latency conclusion to VerdictInconclusive naming both identities, after the nonIncreasing bytes and allocs checks still run. The bytes/allocs checks sit inside the four per-tier evaluators (evaluateEngineSensitiveImprovement:193, evaluateNonRegression:217, evaluateWithinTolerance:238, evaluateStartup:262), each calling nonIncreasing before its significance gate, so the mismatch cannot simply be an early return at the top of Evaluate. One production call site: cmd/perfgate/main.go:120.
- `cmd/perfgate/main.go` — `evaluate` — anchor `cells, err := perfgate.ParseBenchstatCSV(csvOut)`
  - Read both raw files' identities before benchstat, override every latency cell to INCONCLUSIVE when they differ, and populate Bytes and Allocs from ReadBenchmarkMetrics instead of the CSV.
- `internal/perfgate/bench_metrics.go` — `ReadBenchmarkMetrics, SampleMetrics` — anchor `NEW FILE — lands beside internal/perfgate/parse.go`
  - New file. NEW: the raw per-sample reader for B/op and allocs/op and the median-to-MetricResult derivation.

**Red**

- 1.1 Author TestCrossRunner_LatencyInconclusiveBytesEnforced in cmd/perfgate/main_test.go (the file c2 creates), exercising run() — the identity comparison and the latency override live in cmd/perfgate's evaluate, which internal/perfgate cannot import: the two committed cross-runner VM files produce INCONCLUSIVE latency naming both identities while bytes and allocs verdicts are decided.
- 1.1 Author TestCrossRunner_UnknownIdentityIsNotAMatch in cmd/perfgate/main_test.go — it asserts the gate's decision rule, which lives in evaluate.
- 2.2 Author TestReadBenchmarkMetrics_MatchesBenchstat in internal/perfgate — it targets ReadBenchmarkMetrics, which lands there: the raw medians equal the committed benchstat.csv Old and New for all 27 cells on both axes.
- 2.2 Author TestCrossRunner_MissingBaselineCellIsInconclusive in cmd/perfgate/main_test.go — the missing-baseline line is produced at the report layer (main.go:133-137) using GoldsetCall/call-boundary.
- 2.2 Author TestReadBenchmarkMetrics_DeltaPctDerivation in internal/perfgate — it targets the median-to-MetricResult derivation: for a cell whose median moves, DeltaPct is the signed percentage and Significant is true — a zero-value MetricResult must fail this test, because DeltaPct=0 makes nonIncreasing pass every cell unconditionally (perfgate.go:297-303 returns PASS on DeltaPct <= 0 before it ever reads New-Old).
- 2.2 Author TestCrossRunner_ZeroBaselineBytesFails in internal/perfgate — it targets the same derivation at Old == 0 using GoldsetCall/call-boundary, which reads 0 B/op and 0 allocs/op in profile-30637802780: unchanged at 0 passes, and any positive candidate figure fails rather than producing NaN or a silent pass.
- 1.1 The cross-runner tests seed from the raw corpora by path — internal/perfgate/testdata/profile-30614184386/bench-vm.txt as baseline against profile-30637802780/bench-vm.txt as candidate — reached from cmd/perfgate as ../../internal/perfgate/testdata/... . benchstat is never invoked on this path, so no seam substitution is needed for them.
- 2.2 Author the cmd-level assertion on the message this seam owns: the stderr or report line for a cross-runner pair names BOTH RunnerIdentity values. That assertion is c3's, not c2's.

**Red tests** (binding — an absent name fails RED): `TestCrossRunner_LatencyInconclusiveBytesEnforced`, `TestCrossRunner_UnknownIdentityIsNotAMatch`, `TestReadBenchmarkMetrics_MatchesBenchstat`, `TestCrossRunner_MissingBaselineCellIsInconclusive`, `TestReadBenchmarkMetrics_DeltaPctDerivation`, `TestCrossRunner_ZeroBaselineBytesFails`

**Code**

- 2.2 Add ReadBenchmarkMetrics and SampleMetrics to internal/perfgate.
- 2.2 Add the identity comparison and the per-cell latency override to cmd/perfgate's evaluate, reading both raw paths that are already passed as -old and -candidate.
- 2.2 Populate CellComparison.Bytes and .Allocs from ReadBenchmarkMetrics rather than from ParseBenchstatCSV; keep Latency on the benchstat path.
- 2.2 Derive MetricResult from SampleMetrics explicitly — never leave a field at its zero value. Old and New are the per-cell medians. DeltaPct = (New-Old)/Old*100 when Old > 0; when Old == 0, DeltaPct is 0 if New == 0 and math.Inf(1) if New > 0, so a first allocation on a 0 B/op cell fails instead of dividing by zero. Significant is true on both axes — these are exact counts and nonIncreasing carries no significance gate (perfgate.go:288-292). N is the sample count; PValue stays 0 and is unread on these axes.
- 2.2 cmd/perfgate: a cell present in the candidate but absent from the baseline is reported at the report layer as `<name>: INCONCLUSIVE (no baseline figure for this cell)` and counted into the needs-rerun signal. Evaluate's signature and Verdict's states are unchanged — the report layer already owns the verdict line format (main.go:133-137).
- 2.2 Order the two paths explicitly in cmd/perfgate's evaluate, because they otherwise contradict each other on the same input. Read both raw files' RunnerIdentity FIRST. When they differ, do not invoke benchstat for a paired comparison at all: set every cell's latency to INCONCLUSIVE naming both identities, populate Bytes and Allocs from ReadBenchmarkMetrics, and run the cell loop to completion. ErrUnpairedComparison therefore cannot arise from a differing cpu:, and the cross-runner obligation is never foreclosed by an abort.
- 2.2 State the exit precedence, which is currently unstated anywhere: 3 (the gate could not be configured, or evidence that should have paired did not) outranks 1 (a cell failed) outranks 2 (a cell needs a rerun). A pairing refusal that survives the identity check is a hard exit 3 before the cell loop — correct, because identities matched, so no cross-runner obligation applies to it.
- 2.2 SampleMetrics exposes the per-sample B/op and allocs/op figures, not only the median, so c4's spread tests recompute max-minus-min through this reader instead of parsing the raw files a second time. Name the accessor when the type lands.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c4` — tasks 3.1 — seam bytes-allowance-values

Serial after `c3`, sharing internal/perfgate. Coder `go-coder`.

Sealed test scope: `internal/perfgate`.

**Sites**

- `internal/perfgate/tiers.json` — `bytesAllowanceBOp` — anchor `"bytesAllowanceBOp": {`
  - Extend from one entry (Goldset/guard-nil: 4) to all 27 cells listed under "cells", each sized from observed spread, with the spread recorded in this file's "comment" the way guard-nil's is today ('three hosted runs ... each read 1128 against 1129 B/op, +0.09%, p=0.000, 0% CI on both arms'). The evidence for the other 27 is not in the repo (see gaps).

**Red**

- 3.1 Author TestBytesAllowancesAreJustifiedBySpread in internal/perfgate: recompute per-cell spread from the 200ms profile (profile-30637802780) alone and require every stated allowance to be <= it — a min or max taken across both profiles is wrong, and min-over-both would fail the plan's own values (rule-load 6 against the 400ms spread of 3, queue-promote 8 against 7), with Goldset/guard-nil exempted by name.
- 3.1 Author TestTierConfig_EveryCellStatesAnAllowance: all 27 cells in `cells` appear in `bytesAllowanceBOp`.
- 3.1 Author TestBytesAllowanceSpread_LongerBenchtimeIsTighter: every cell's 400ms span in profile-30630796967 is <= its 200ms span in profile-30637802780, which is why the 200ms profile is the evidence base and the other is only a check.

**Red tests** (binding — an absent name fails RED): `TestBytesAllowancesAreJustifiedBySpread`, `TestTierConfig_EveryCellStatesAnAllowance`, `TestBytesAllowanceSpread_LongerBenchtimeIsTighter`

**Code**

- 3.1 internal/perfgate/tiers.json: add the 26 missing bytesAllowanceBOp entries, expected values: Goldset/counter-closure 1, Goldset/guard-nil 4 (the between-engine exception, unchanged), Goldset/kw-lookup 1, Goldset/loop-sum 2, Goldset/merge-config 3, Goldset/pipeline 2, Goldset/queue-promote 8, Goldset/registry-fold 2, Goldset/route-decision 1, Goldset/rule-load 6, Goldset/safe-parse 2, Goldset/text-render 1, Goldset/twice-macro 2 — thirteen non-zero, every one a Goldset/* cell. Then GoldsetCall/call-boundary 0 and each of the thirteen GoldsetParse/* cells 0 — fourteen stated zeros. Twenty-seven entries. These are the expected RESULT of the derivation, not a substitute for it — recompute every one from profile-30637802780/bench-vm.txt and fail the seam if any disagrees.
- 3.1 internal/perfgate/tiers.json comment: record the derivation rule and the profile id the numbers came from, AND rewrite the now-false sentence at tiers.json:2 — "Every other data-dominated cell, including all thirteen GoldsetParse/* cells, keeps the exact non-increasing bound with no allowance" — to say that every cell states an allowance and that those fourteen state an explicit 0, which IS that exact bound. ADR 0008 carries the same sentence and c9 rewrites it there; the two must say the same thing.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c5` — tasks 1.2, 3.2 — seam bytes-allowance-config

Serial after `c4`, sharing internal/perfgate. Coder `go-coder`.

Sealed test scope: `internal/perfgate`.

**Sites**

- `internal/perfgate/perfgate_test.go` — `TestLoadTierConfig_MissingBytesAllowance (NEW)` — anchor `// TestLoadTierConfig_BytesAllowance_UnknownCell guards against a typo'd`
  - NEW test beside the existing typo guard: a cells entry with no bytesAllowanceBOp key makes LoadTierConfig return an error naming the cell and the missing allowance, instead of yielding CellTier{BytesAllowanceBOp: 0}.
- `internal/perfgate/parse.go` — `LoadTierConfig` — anchor `for name, allowance := range file.BytesAllowanceBOp {`
  - Invert the optionality: after the cells loop assigns tiers, require every cell to appear in file.BytesAllowanceBOp and return the package's standard 'perfgate: cell %q: ...' error when it does not. CellTier.BytesAllowanceBOp (parse.go:192-195) is a plain float64, so absence and a stated zero are currently indistinguishable. Allocs keep a hardcoded 0 allowance at their nonIncreasing call sites (perfgate.go:194, 221, 242, 266).

**Red**

- 1.2 Author TestLoadTierConfig_MissingBytesAllowance: a cell with no entry yields errors.Is(err, ErrMissingBytesAllowance) and a message naming the cell.
- 1.2 Author TestLoadTierConfig_StatedZeroIsNotAbsent: a stated 0 loads cleanly and sets BytesAllowanceStated.
- 3.2 Author TestLoadTierConfig_NegativeBytesAllowance.

**Red tests** (binding — an absent name fails RED): `TestLoadTierConfig_MissingBytesAllowance`, `TestLoadTierConfig_StatedZeroIsNotAbsent`, `TestLoadTierConfig_NegativeBytesAllowance`

**Code**

- 3.2 parse.go: tierConfigFile.BytesAllowanceBOp becomes map[string]*float64; CellTier gains BytesAllowanceStated bool; LoadTierConfig requires an entry for every cell and rejects a negative value; add ErrMissingBytesAllowance.
- 3.2 perfgate_test.go: update the two existing tier-config fixtures this change invalidates, in the chunk that invalidates them, so the package is never left red. TestLoadTierConfig (:369-379) — its literal carries no bytesAllowanceBOp map, and its :378 assertion states the very semantics this chunk abolishes, so the assertion is replaced by the stated-zero semantics, not just the literal. TestLoadTierConfig_BytesAllowance (:392-401) — two cells in `cells`, one allowance; give both an entry and keep the assert.Zero on the stated-zero cell. TestLoadTierConfig_UnknownTier (:381-388) needs no edit: its bogus tier errors first and the test asserts only require.Error.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c6` — tasks 1.3, 4.3 — seam gate-mode-resolution

Serial after `c5`, sharing internal/perfgate. Coder `go-coder`.

Sealed test scope: `internal/perfgate`.

**Sites**

- `internal/perfgate/perfgate_test.go` — `TestResolveBaselineMode (NEW)` — anchor `func TestLoadTierConfig_UnknownTier(t *testing.T) {`
  - NEW test that a baseline-lookup error and an empty baseline set are distinct outcomes, and only the empty set yields ModeFirstAuthorization. No code under test exists in Go today: mode resolution lives only in release.yml shell (see gaps).
- `.github/workflows/release.yml` — `Determine gate mode` — anchor `            if gh release download "$tag" --pattern bench-vm.txt --output baseline-vm.txt 2>/dev/null; then`
  - No edit in this chunk. Task 4.3's Go half — the four-way BaselineLookup taxonomy that distinguishes an absent asset from an API or auth failure — lands in internal/perfgate/gatemode.go here; c7 restructures this step to collect the lookup and to fail on resolve-mode's exit code, because the workflow cannot call a subcommand that does not exist yet.

**Red**

- 1.3 Author TestResolveGateMode over all four states plus the zero lookup, asserting the Mode, the BaselineOutcome and errors.Is on the sentinel.
- 4.3 Author TestResolveGateMode_FailureNeverSelectsFirstAuthorization: neither failure state returns ModeFirstAuthorization.

**Red tests** (binding — an absent name fails RED): `TestResolveGateMode`, `TestResolveGateMode_FailureNeverSelectsFirstAuthorization`

**Code**

- 1.3 Add internal/perfgate/gatemode.go: BaselineLookup, BaselineOutcome and its constants, ResolveGateMode, ErrBaselineEnumerationFailed, ErrBaselineDownloadFailed.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c7` — tasks 4.1 — seam gate-mode-resolution

Serial after `c6`, sharing internal/perfgate. Coder `go-coder`.

Sealed test scope: `internal/perfgate`, `cmd/perfgate`.

**Sites**

- `.github/workflows/release.yml` — `Determine gate mode` — anchor `      - name: Determine gate mode`
  - Drop the event gate so a workflow_dispatch run resolves mode from the stored baselines too; the loop's 'skip the release being gated' line ([ "$tag" = "$RELEASE_TAG" ] && continue) must stay correct when RELEASE_TAG is empty on dispatch. Downstream reader is 'Evaluate performance gate' (line 145), whose `[ -z "$mode" ] && mode="first-authorization"` default (line 150) is the silent fallback this task removes. The anchor is the step name, not its `if:` line: that line appears verbatim on `Store VM baseline on the authorized release` (release.yml:211) too, and removing it there would let a dispatch publish a baseline. Never touch the guard on that step.

**Red**

- 4.1 Author TestRun_ResolveModeSubcommand in cmd/perfgate: a lookup file describing a found baseline prints `mode=non-regression` and exits 0; a lookup describing an enumeration failure exits 3.

**Red tests** (binding — an absent name fails RED): `TestRun_ResolveModeSubcommand`

**Code**

- 4.1 cmd/perfgate/main.go: dispatch `resolve-mode` as a subcommand when it is args[0], leaving the existing flag path untouched for the two current invocations.
- 4.1 .github/workflows/release.yml: remove `if: github.event_name == 'release'` from `Determine gate mode` (:92); restructure its script to inspect each release's asset list with `gh release view --json assets` before downloading, write a lookup JSON, and call `bin/perfgate resolve-mode`; move the `go build -o bin/perfgate ./cmd/perfgate` line (:148) ahead of it.
- 4.1 release.yml: fail the step on a non-zero from resolve-mode instead of falling through to the `[ -z "$mode" ] && mode="first-authorization"` default at :150, which is the second place absence and failure are conflated today. Moved here from c6: the workflow cannot fail on resolve-mode's exit code until this chunk adds the subcommand and moves the bin/perfgate build ahead of the step.

**redRun**

```sh
go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...
```

**verify**

```sh
go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c8` — tasks 4.2 — seam dispatch-publishes-nothing

Serial after `c7`, sharing .github/workflows. Coder `zpatcher`.

**Sites**

- `.github/workflows/release.yml` — `Store VM baseline on the authorized release` — anchor `        run: gh release upload "$RELEASE_TAG" bench-vm.txt --clobber`
  - Verification site: this step's `if: github.event_name == 'release'` must remain, so widening the mode-resolution gate in 4.1 does not also make a dispatched run publish a baseline. 'Upload release evidence' (line 220) is the artifact-only path a dispatch keeps.

**No red stage.** NO-RED-WAIVER: no Go test can observe which Actions steps ran, so this seam authors no contract test. It is not unverified: the chunk's verify asserts that the workflow holds exactly one `gh release upload` and that the step holding it still carries the release guard — the two facts spec line 56 turns on. Counting occurrences pins behaviour, not indentation.

**Code**

- 4.2 release.yml: add a comment above the upload step recording that the guard is the only thing keeping a dispatch from consuming the baseline slot, and confirm by reading that no other step publishes.
- 4.2 Confirm by assertion, not by reading: the workflow holds exactly one `gh release upload`, and the step holding it still carries `if: github.event_name == 'release'`. That pair is what keeps a dispatch from consuming the baseline slot, and it is what this chunk's verify runs.

**verify**

```sh
test "$(grep -c 'gh release upload' .github/workflows/release.yml)" = 1 && grep -A6 'name: Store VM baseline on the authorized release' .github/workflows/release.yml | grep -q "if: github.event_name == 'release'"
```

#### `c9` — tasks 5.1, 5.2 — seam gate-documentation

Parallel, shard `docs`. Coder `coder`.

**Sites**

- `docs/adr/0008-consumer-performance-gate.md` — `## Thresholds` — anchor `Note (non-increasing bounds and benchstat "~"): every tier with a bytes or`
  - Add a note in the same voice as the three existing notes: baseline and candidate must be measured on the same runner identity for a latency conclusion, and allocation counts are exact per-op integers while B/op is an average that wobbles, so bytes carries a stated allowance where allocs stays exactly zero. The existing guard-nil note (line 50) already states the per-cell allowance mechanism and must stay consistent with 3.1 widening it to every cell.
- `CHANGELOG.md` — `## [Unreleased]` — anchor `## [Unreleased]`
  - Add a `### Changed` subsection under the currently empty [Unreleased] heading recording the observable change: the gate reports latency as inconclusive when the stored baseline and the candidate were measured on different runners, and still enforces allocation counts and bytes. Follow the 0.13.0 gate entry's voice (line 38).

**No red stage.** NO-RED-WAIVER: documentation only. ADR 0008 and CHANGELOG.md carry no executable behaviour, and a substring test over either would pin prose formatting rather than a rule. Amends the two texts that state the overturned-looking invariant and records the user-visible change.

**Code**

- 5.1 docs/adr/0008-consumer-performance-gate.md: add the runner-comparability rule to Thresholds; amend the allowance note at :50-74; amend the benchstat-'~' note at :34-40 to scope it to latency.
- 5.2 CHANGELOG.md: add the [Unreleased] -> Changed entry.

**verify**

```sh
go test -timeout 2m ./cl/... && openspec validate release-gate-baseline-comparability --strict
```

#### `c10` — tasks 1.4 — seam existing-tests-unchanged

Serial after `c8`, sharing internal/perfgate. Coder `zpatcher`.

Sealed test scope: `internal/perfgate`.

**Sites**

- `internal/perfgate/perfgate_test.go` — `TestPinnedProfile` — anchor `res := Evaluate(cell, ct.Tier, ModeFirstAuthorization)`
  - Read-only confirmation that every existing test function still passes. The two fixtures a stated allowance requires were updated in c5, in the chunk that required them.

**No red stage.** NO-RED-WAIVER: a re-run of the existing suite against the changed production code, authoring no assertion. The deliverable is that every existing test function in internal/perfgate/perfgate_test.go passes. Two tier-config fixtures were updated in c5 — inputs, not assertions — which is the whole of what task 1.4's "unchanged" gives up, and it is stated in the design rather than reinterpreted.

**Code**

- 1.4 Run the whole package and confirm every existing test function passes. c5 already updated the two tier-config fixtures a stated allowance requires; this chunk authors no edit. The deliverable is the recorded result.

**verify**

```sh
go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...
```

#### `c11` — tasks 6.1 — seam full-floor

Serial after `c10`, sharing whole tree. Coder `coder`.

**Sites**

- `Makefile` — `test / lint` — anchor `GOTESTFLAGS ?= -timeout 2m`
  - Verification only: `make test` (go test -timeout 2m ./...) and `make lint` (golangci-lint run). There is no race target and no vet target -- the race suite over core, plugins, runtime and `go vet ./...` are raw commands, as ci.yml runs them (.github/workflows/ci.yml:21-22).

**No red stage.** NO-RED-WAIVER: the whole-change floor. Runs the repository suite, the race suite over the three trees task 6.1 names, go vet and the linter, and records each command's exit status. Authors no assertion.

**Code**

- 6.1 Run the floor in order and record each command's exit status.

**verify**

```sh
make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint
```

#### `c12` — tasks 6.2 — seam hosted-dispatch-preflight

Serial after `c11`, sharing whole tree. Coder `coder`.

**Sites**

- `.github/workflows/release.yml` — `workflow_dispatch` — anchor `  workflow_dispatch: {}`
  - Verification only: dispatch this workflow against the candidate tree and confirm the 'Evaluate performance gate' step's `mode=` output reads non-regression. Requires a pushed ref and gh auth; the dispatch path cannot be exercised locally.

**No red stage.** NO-RED-WAIVER: requires a hosted GitHub Actions runner and cannot be executed by an implementer locally. Latency figures from a developer box are not trustworthy for this gate in any case. The deliverable is the dispatch run URL plus the recorded mode line, not a local command.

**Code**

- 6.2 SEQUENCING: this chunk runs AFTER the change is merged and pushed. A hosted Actions dispatch cannot be triggered from a pre-merge isolated worktree. It is not blocked and it is not done until the run exists — record it as outstanding at merge time.
- 6.2 Dispatch the gate against the release-candidate tree; record the run URL, the resolved mode line and the skipped upload step.
- 6.2 Record the run URL, the resolved mode line, and that the `Store VM baseline on the authorized release` step reports skipped. The verify above asserts the mode line; the other two go in the report.
- 6.2 Do not assert the mode by grepping the whole run log: GitHub Actions reprints each `run:` script body into the log, and release.yml:106 contains the literal `mode=non-regression` today, so a whole-log grep passes on script text. Read the resolve-mode step's own output or its stdout line, and record the resolved mode and the skipped upload step in the report.

**verify**

```sh
gh run view "$(gh run list --workflow 'Release consumer gate' --event workflow_dispatch --limit 1 --json databaseId --jq '.[0].databaseId')" --json jobs --jq '.jobs[].steps[] | select(.name=="Determine gate mode") | .conclusion' | grep -qx success
```

### Floor

Run after every chunk has merged.

```sh
make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint
```

### Mode, lenses, review

- Testing mode: `existing-service-strict` — the gate is existing, released machinery with a 780-line test file and committed golden corpora; every new rule is checked against those rather than against a new fixture where one already exists.
- Tier: `heavy`. Lenses: `spec`, `quality`, `arch`. `spec` because every chunk answers a SHALL line and the requirements map is the acceptance test; `quality` because the diff adds a second statistic path and a new exit code to a package whose error taxonomy is already load-bearing. No `sec` (no auth, input parsing of untrusted data, or secrets), no `perf` (the change measures performance, it does not have a hot path), no `arch` (no new package, no moved boundary).
- Plan review: **pass**, reviewer `zarchitect`, 4 round(s).

### Rules for an agent without the flow kernel

Inlined because an uninlined rule is an unfollowed rule.

- Work in the assigned worktree. Assert you are in it before the first edit; never edit the primary checkout.
- Native file tools first: read and search with the harness's file tools, not `rg`/`grep`/`cat`/`sed` through a shell. The shell is for commands that *do* something.
- A contract test, once written, is read-only. A later chunk that believes one is wrong raises it; it does not edit it.
- Run the chunk's literal `verify` above, whole-package. Do not narrow it with `-run`: a narrowed regex can report pass while the package is red.
- Every test run carries a resource limit. `-timeout 2m` for unit, `-timeout 10m` for the race suite.
- Commits: Conventional Commits, `<type>(<scope>): <description>`, imperative, lowercase, subject ≤72 chars, body wrapped at ≤100. Types: feat fix refactor perf docs test build ci chore revert. Describe only the staged diff. No AI or tool attribution anywhere in the message, the code, or the docs.
- Recompute, never copy, the bytes allowances in `c4`. The table in D4 is the expected result of a procedure, not a substitute for running it.

## Plan appendix

```json
{
  "v": 2,
  "change": "release-gate-baseline-comparability",
  "baseSha": "98f11d2d298ea69e4f4fd247b5347ff038964310",
  "generatedAt": "2026-09-05T14:52:46.714Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "arch"
  ],
  "chunks": [
    {
      "id": "c1",
      "taskIds": [
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "runner-identity",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/..."
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "internal/perfgate/parse.go",
          "symbol": "benchstatPreamblePrefixes / parseBlock (identity reader is NEW)",
          "anchor": "var benchstatPreamblePrefixes = []string{\"goos:\", \"goarch:\", \"pkg:\", \"cpu:\"}",
          "change": "The cpu: line is the runner identity and is currently discarded here (parseBlock skips any line matching these prefixes via hasAnyPrefix, parse.go:49). Add a reader that extracts the identity from a raw bench-*.txt preamble and returns unknown when no cpu: line is present, so a baseline stored before this change does not crash. Under D1 task 2.1 needs no workflow change: the identity is already inside the uploaded file."
        },
        {
          "task": "2.1",
          "file": "internal/perfgate/runner_identity.go",
          "symbol": "RunnerIdentity, ReadRunnerIdentity, ErrInconsistentPreamble",
          "anchor": "NEW FILE — lands beside internal/perfgate/parse.go",
          "change": "New file. NEW: the identity type, its String() and Known(), the raw-preamble reader, and the inconsistent-preamble sentinel. Lands declared and unused; c3 consumes it."
        },
        {
          "task": "2.1",
          "file": "internal/perfgate/runner_identity_test.go",
          "symbol": "TestReadRunnerIdentity",
          "anchor": "NEW FILE — lands beside internal/perfgate/perfgate_test.go",
          "change": "New file. NEW: the contract test for the reader, seeded from the committed corpora plus literal readers for the unknown and inconsistent states."
        }
      ],
      "contract": {
        "states": [
          "identity-known: RunnerIdentity with all three fields non-empty; Known() true",
          "identity-unknown: CPU empty; Known() false; String() renders the literal `unknown` in that position",
          "identity-inconsistent: the file's repeated preambles disagree; ReadRunnerIdentity returns ErrInconsistentPreamble and a zero RunnerIdentity"
        ],
        "transitions": [
          {
            "input": "internal/perfgate/testdata/profile-30637802780/bench-vm.txt",
            "state": "identity-known: {GOOS: \"linux\", GOARCH: \"amd64\", CPU: \"AMD EPYC 7763 64-Core Processor\"}; String() == \"linux/amd64/AMD EPYC 7763 64-Core Processor\"",
            "effect": "set",
            "evidence": "the file's `cpu: AMD EPYC 7763 64-Core Processor` line, repeated 10 times, with 16 trailing spaces that TrimSpace removes"
          },
          {
            "input": "internal/perfgate/testdata/profile-30614184386/bench-vm.txt",
            "state": "identity-known: CPU == \"INTEL(R) XEON(R) PLATINUM 8573C\"; String() == \"linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C\"",
            "effect": "set",
            "evidence": "that file's cpu: line. This is the repo's own cross-runner counterexample and is what cross-runner-verdicts seeds from."
          },
          {
            "input": "a reader over the four-line preamble with the cpu: line removed",
            "state": "identity-unknown: {GOOS: \"linux\", GOARCH: \"amd64\", CPU: \"\"}; Known() false; String() == \"linux/amd64/unknown\"",
            "effect": "clear",
            "evidence": "task 2.1's 'reports its identity as unknown'. Under D1 this is the input that reaches it, not a pre-change baseline."
          },
          {
            "input": "an empty reader",
            "state": "identity-unknown: zero RunnerIdentity; String() == \"unknown/unknown/unknown\"; no error",
            "effect": "clear",
            "evidence": "a file with no preamble carries no identity; refusing it here would make the parser the gate's failure point rather than the comparison"
          },
          {
            "input": "a reader whose first preamble says `cpu: A` and whose second says `cpu: B`",
            "state": "identity-inconsistent: ErrInconsistentPreamble, zero RunnerIdentity",
            "effect": "forced",
            "evidence": "the workflow appends one `go test` run per sample (release.yml:131-134), so a single file recording two CPUs means the ten samples did not all run on one machine and no single identity describes it"
          },
          {
            "input": "two preambles that differ only in trailing whitespace on the cpu: line",
            "state": "identity-known, no error; the two normalise to the same string",
            "effect": "no-op",
            "evidence": "the AMD corpora carry 16 trailing spaces; treating that as an inconsistency would reject the repo's own committed files"
          },
          {
            "input": "any bench-*.txt already in internal/perfgate/testdata",
            "state": "identity-known; no file in the repo reaches identity-unknown or identity-inconsistent",
            "effect": "no-op",
            "evidence": "all six checked-in bench files carry 10 cpu: lines each, verified"
          }
        ],
        "forbidden": [
          "Reading the identity from benchstat's output. benchstat drops the cpu: line entirely under -ignore and reports only one group's preamble otherwise; the raw file is the only faithful source.",
          "Including `pkg:` in the identity. It names the benchmarked package, not the machine, and would make an unrelated package rename read as a hardware change.",
          "Writing to, rewriting, or copying any bench-*.txt. The identity is read; nothing is normalised on disk.",
          "Treating a missing cpu: line as an error, or a present one as optional to compare.",
          "Any use of ReadRunnerIdentity in cmd/perfgate in this seam — it lands unused on purpose."
        ],
        "seeding": [
          "identity-known: os.Open on internal/perfgate/testdata/profile-30637802780/bench-vm.txt and internal/perfgate/testdata/profile-30614184386/bench-vm.txt. Committed corpora, not constructed.",
          "identity-unknown and identity-inconsistent: strings.NewReader over a hand-written 3- or 8-line preamble in the test file. These two states are unreachable from any committed corpus, so a literal is the only legal path.",
          "Never: constructing a RunnerIdentity by hand and asserting String() alone — at least one arm must read a real corpus file so the parser is what is under test."
        ],
        "budgets": [
          "10: preamble repetitions per checked-in bench file; ReadRunnerIdentity must read all of them, not just the first.",
          "3: identity keys read (goos, goarch, cpu). pkg is skipped.",
          "2: distinct CPU identities across the three committed profiles (AMD EPYC 7763 64-Core Processor; INTEL(R) XEON(R) PLATINUM 8573C).",
          "0: files in the repo that reach identity-unknown."
        ]
      },
      "redTasks": [
        "2.1 Author internal/perfgate/runner_identity_test.go: TestReadRunnerIdentity over the two committed corpora, the missing-cpu reader, the empty reader, the inconsistent-preamble reader, and the trailing-whitespace pair.",
        "Red shape: the first run is a BUILD failure, because TestReadRunnerIdentity names symbols this chunk's own codeTasks create. That is the ordinary Go shape and nothing is out of order, but a non-compiling package is not evidence the assertions are right — after the symbols land declared, re-run and confirm the failure is an assertion failure before handing over."
      ],
      "codeTasks": [
        "2.1 Add internal/perfgate/runner_identity.go: RunnerIdentity, its String() and Known(), ReadRunnerIdentity, ErrInconsistentPreamble."
      ],
      "redTests": [
        "TestReadRunnerIdentity"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c2",
      "taskIds": [
        "2.3"
      ],
      "prev": "c1",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "unpaired-comparison-refusal",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate",
        "cmd/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/...",
        "./cmd/perfgate/..."
      ],
      "sites": [
        {
          "task": "2.3",
          "file": "cmd/perfgate/main.go",
          "symbol": "runBenchstat",
          "anchor": "cmd := exec.Command(\"go\", \"run\", \"golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d\", \"-format\", \"csv\", oldPath, newPath)",
          "change": "Verification site: this arg list must never gain -ignore (or any preamble-stripping step), and a benchstat refusal must surface as the existing 'benchstat: %w: %s' error with benchstat's own stderr. The parse-side symptom of a declined pairing is 'perfgate: benchstat csv data row too short' from parseBlock (parse.go:74), which is a parse error rather than a stated refusal."
        },
        {
          "task": "2.3",
          "file": "internal/perfgate/parse.go",
          "symbol": "parseBlock, ErrUnpairedComparison",
          "anchor": "if len(records) < 3 {",
          "change": "Classify the metric header row before any data row is read: 7 fields ending vs base/P is paired, 3 fields ending CI is single-group and returns ErrUnpairedComparison, anything else keeps the existing generic error."
        },
        {
          "task": "2.3",
          "file": "cmd/perfgate/main_test.go",
          "symbol": "TestRun_UnpairedComparisonExitsThree, TestRun_ConfigErrorExitsThree",
          "anchor": "NEW FILE — the package has no test file today",
          "change": "New file. NEW: the first tests in cmd/perfgate, exercising run() through the injectable benchstat seam."
        },
        {
          "task": "2.3",
          "file": "internal/perfgate/testdata/unpaired-single-group.csv",
          "symbol": "unpaired fixture",
          "anchor": "NEW FILE — lands under internal/perfgate/testdata/",
          "change": "New file. NEW: benchstat output over a pair whose cpu: lines differ, committed rather than generated so no test invokes benchstat. A README line records how it was produced."
        }
      ],
      "contract": {
        "states": [
          "paired: the metric header has 7 fields ending `vs base`,`P`; cells populate as today",
          "single-group: the metric header has 3 fields ending `CI`; ErrUnpairedComparison; no cells returned",
          "malformed: the header matches neither shape, or csv.ReadAll rejects the block; the existing generic error, which now means only what it says",
          "exit-taxonomy: 0 all pass, 1 any fail, 2 any cell needs a rerun, 3 the gate could not be configured or the evidence could not be paired",
          "post-c3 reachability: once c3 lands, a differing cpu: never reaches this path — perfgate compares identities first and skips benchstat. The remaining triggers are a differing goos:, goarch: or pkg:, and a future benchstat change.",
          "identities-match precondition: after c3, this path is reachable only when the two runner identities agree — a differing cpu: is handled by the identity comparison before benchstat is consulted"
        ],
        "transitions": [
          {
            "input": "internal/perfgate/testdata/profile-30637802780/benchstat.csv",
            "state": "paired: 27 cells, no error",
            "effect": "no-op",
            "evidence": "measured: all three blocks read `,sec/op,CI,sec/op,CI,vs base,P`, `,B/op,...`, `,allocs/op,...`, 7 fields each, and records[0] names two distinct files. TestPinnedProfile parses this file (perfgate_test.go:697-700) and must keep passing."
          },
          {
            "input": "a benchstat -format csv run over two files whose cpu: lines differ",
            "state": "single-group: ErrUnpairedComparison naming the metric and the column count",
            "effect": "forced",
            "evidence": "measured with the pinned benchstat over the committed profile-30637802780 pair with one cpu: line rewritten: benchstat EXITS 0 and emits six blocks - two single-group tables of three metric blocks each - whose metric headers read `,sec/op,CI` (3 fields) and whose data rows read `Goldset/counter-closure-2,8.507e-06,1%` (3 fields)."
          },
          {
            "input": "the same input, under today's code",
            "state": "the generic `perfgate: benchstat csv data row too short` from parse.go:73-75",
            "effect": "clear",
            "evidence": "this is the symptom reported for the v0.13.0 cut, and it is indistinguishable from a malformed CSV. The header check makes the data-row check unreachable for this input."
          },
          {
            "input": "a CSV whose header matches neither shape, or whose rows are ragged",
            "state": "malformed: the existing generic error",
            "effect": "no-op",
            "evidence": "parse.go:58-75; csv.ReadAll rejects ragged rows before the header is consulted"
          },
          {
            "input": "the same differing-cpu pair with -ignore=cpu",
            "state": "never produced; no code path constructs this argv",
            "effect": "forbidden",
            "evidence": "measured: -ignore=cpu yields one paired table AND removes the cpu: line from the output, i.e. it obtains the comparison by disregarding the configuration line the spec forbids disregarding."
          },
          {
            "input": "benchstat's stderr on the measured single-group run",
            "state": "carried into the message for the operator, but never used as the reason",
            "effect": "no-op",
            "evidence": "measured: stderr read only `B65: summaries must be >0 to compute geomean`, which is about the geomean and not about pairing. cmd/perfgate/main.go:158-171 discards stderr whenever benchstat exits 0, so today even that is thrown away."
          },
          {
            "input": "LoadTierConfig returns a configuration error",
            "state": "exit 3, not exit 2",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:55-60 maps every error to exit 2 today, and release.yml:169 treats exit 2 as 'rerun at doubled benchtime', so a configuration error costs a pointless doubled-benchtime rerun before failing."
          },
          {
            "input": "a cell that is genuinely inconclusive",
            "state": "exit 2, unchanged; release.yml's rerun step still fires",
            "effect": "no-op",
            "evidence": "release.yml:168-196; the rerun contract is not being changed"
          }
        ],
        "forbidden": [
          "Detecting the single-group case from the data rows. The header row is the positive discriminator; a short data row is also what a malformed CSV produces.",
          "Passing -ignore, -filter, -col, -row or -table to benchstat. The argv stays exactly `-format csv <old> <new>`.",
          "Reporting benchstat's stderr AS the pairing reason - on the measured run it says nothing about pairing.",
          "Rewriting, copying, filtering or truncating either input file before handing it to benchstat.",
          "Retrying benchstat with different arguments after any failure.",
          "Removing goos:/goarch:/pkg:/cpu: from benchstatPreamblePrefixes - that filter drops those lines from benchstat's OUTPUT so the CSV parses; it does not touch the inputs and is not what the spec forbids.",
          "Reusing exit 2 for a configuration failure, or exit 3 for a needs-rerun signal.",
          "Asserting the two runner identities in this chunk's stderr line. Reading both raw files' identities in cmd/perfgate is c3's codeTask; c2 asserts the single-group reason and exit 3 only, and c3 adds the identities to the message.",
          "Treating this chunk's exit-3 abort as the handler for a cross-runner pair. It is not: c3 routes those away from benchstat entirely, and an abort here would foreclose the bytes and allocs verdicts spec scenario 1 requires.",
          "Seeding this chunk's cmd-level tests with two files whose runner identities differ. That input belongs to c3 and is routed away from benchstat."
        ],
        "seeding": [
          "paired: internal/perfgate/testdata/profile-30637802780/benchstat.csv, already committed.",
          "single-group: SYNTHESIZED, and necessarily so - no single-group benchstat CSV is committed anywhere in the repo, and the two CPU strings the proposal quotes for the failing v0.13.0 cut (`Intel(R) Xeon(R) Platinum 8370C`, `AMD EPYC 9V74`) appear in no committed file either. Produce it once by copying the two profile-30637802780 bench files, rewriting the cpu: line in the copy of bench-evaluator.txt to the CPU string profile-30614184386 actually records (`INTEL(R) XEON(R) PLATINUM 8573C`, so the fixture quotes a machine this project has really used), running the pinned benchstat over the pair, and committing the output verbatim as internal/perfgate/testdata/unpaired-single-group.csv with a README line recording exactly that recipe.",
          "Committed rather than generated in the test: generating it would make the unit test fetch benchstat over the network on a cold module cache.",
          "exit-taxonomy: a new cmd/perfgate/main_test.go calling run(stdout, stderr, args) directly and asserting the returned int. cmd/perfgate has no test file today.",
          "Never: asserting the absence of -ignore by grepping source; assert the error path end to end from the committed fixture instead.",
          "TestRun_UnpairedComparisonExitsThree and TestRun_ConfigErrorExitsThree: swap the package-level runBenchstat for one returning internal/perfgate/testdata/unpaired-single-group.csv, restoring it with t.Cleanup. No network, no benchstat invocation.",
          "The unpaired CSV is committed rather than generated: generating it needs benchstat, which is the dependency the seam exists to avoid.",
          "TestRun_UnpairedComparisonExitsThree seeds -old and -candidate with the two SAME-identity committed files: internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt and .../bench-vm.txt (both record `cpu: AMD EPYC 7763 64-Core Processor`). This is load-bearing after c3: the identity-first ordering routes a differing-identity pair away from benchstat entirely, so seeding differing files would make ErrUnpairedComparison unreachable and turn this sealed test red at c3. The unpaired shape comes from the injected runBenchstat returning the committed unpaired CSV, never from the input files' identities.",
          "Never seed empty or synthetic temp paths: after c3, ReadBenchmarkMetrics runs over both raw paths and would change the exit path.",
          "Both cmd-level tests pass `-tiers ../../internal/perfgate/tiers.json` explicitly. The flag's default is `internal/perfgate/tiers.json` (cmd/perfgate/main.go:29), which under `go test ./cmd/perfgate/...` resolves against the package directory and does not exist — os.Open fails at main.go:81-83 and returns exit 3 through main.go:55-60. Without the explicit path TestRun_ConfigErrorExitsThree passes while seeding nothing, and TestRun_UnpairedComparisonExitsThree gets the right exit code for the wrong reason."
        ],
        "budgets": [
          "7: fields in a paired metric header, ending `vs base` and `P`.",
          "3: fields in a single-group metric header, ending `CI`. Both measured.",
          "6: blocks in the single-group output - two configurations times three metrics.",
          "0: benchstat exit code on the single-group run, which is why nothing upstream notices.",
          "4 exit codes: 0, 1, 2, 3.",
          "0: retries after a pairing refusal."
        ]
      },
      "redTasks": [
        "2.3 Author TestParseBenchstatCSV_UnpairedSingleGroup: the committed fixture yields errors.Is(err, ErrUnpairedComparison) and a message containing `single-group`.",
        "2.3 Author TestParseBenchstatCSV_MalformedIsNotUnpaired: a ragged or wrong-header CSV yields an error that does NOT satisfy errors.Is(err, ErrUnpairedComparison), so the two failures stay distinguishable.",
        "2.3 Author TestParseBenchstatCSV_PairedHeaderStillParses over the committed benchstat.csv.",
        "2.3 Author cmd/perfgate/main_test.go: TestRun_UnpairedComparisonExitsThree asserting exit 3 and the single-group reason in the stderr line (NOT the runner identities — c3 adds those); TestRun_ConfigErrorExitsThree.",
        "Red shape: the first run is a BUILD failure, because ErrUnpairedComparison names symbols this chunk's own codeTasks create. That is the ordinary Go shape and nothing is out of order, but a non-compiling package is not evidence the assertions are right — after the symbols land declared, re-run and confirm the failure is an assertion failure before handing over."
      ],
      "codeTasks": [
        "2.3 parse.go: add ErrUnpairedComparison; in parseBlock, classify records[1] three ways before reading any data row and return ErrUnpairedComparison for the single-group shape.",
        "2.3 cmd/perfgate/main.go: capture benchstat's stderr on the success path; on errors.Is(err, ErrUnpairedComparison) re-report naming the single-group shape and the offending metric block (the runner identities are c3's — they are not read in this chunk); introduce exit code 3 for configuration and pairing failures; update the exit-code contract in the evaluate doc comment at :74-79.",
        "2.3 Commit internal/perfgate/testdata/unpaired-single-group.csv plus the README line recording how it was produced.",
        "2.3 cmd/perfgate/main.go: give runBenchstat an injectable seam — a package-level `var runBenchstat = func(oldPath, newPath string) ([]byte, error)` that evaluate calls through, so a test can supply the committed unpaired CSV without `go run`-ing benchstat over the network. Without it the cmd-level tests either need network on a cold module cache or assert nothing."
      ],
      "redTests": [
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree",
        "TestRun_ConfigErrorExitsThree"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c3",
      "taskIds": [
        "1.1",
        "2.2"
      ],
      "prev": "c2",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "cross-runner-verdicts",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate",
        "cmd/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/...",
        "./cmd/perfgate/..."
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "internal/perfgate/perfgate_test.go",
          "symbol": "cross-runner assertions — authored in cmd/perfgate/main_test.go, not here",
          "anchor": "// TestEvaluate_BytesAllowance guards the per-cell absolute bytes allowance",
          "change": "No new test lands in this file for the cross-runner rule. The identity comparison and the latency override live in cmd/perfgate's evaluate, which internal/perfgate cannot import, so TestCrossRunner_LatencyInconclusiveBytesEnforced, TestCrossRunner_UnknownIdentityIsNotAMatch and TestCrossRunner_MissingBaselineCellIsInconclusive are authored in cmd/perfgate/main_test.go. This file gains only the ReadBenchmarkMetrics derivation tests. Evaluate's signature and CellComparison's fields are unchanged."
        },
        {
          "task": "2.2",
          "file": "internal/perfgate/perfgate.go",
          "symbol": "Evaluate",
          "anchor": "func Evaluate(cell CellComparison, tier Tier, mode Mode) Result {",
          "change": "A runner mismatch must short-circuit the latency conclusion to VerdictInconclusive naming both identities, after the nonIncreasing bytes and allocs checks still run. The bytes/allocs checks sit inside the four per-tier evaluators (evaluateEngineSensitiveImprovement:193, evaluateNonRegression:217, evaluateWithinTolerance:238, evaluateStartup:262), each calling nonIncreasing before its significance gate, so the mismatch cannot simply be an early return at the top of Evaluate. One production call site: cmd/perfgate/main.go:120."
        },
        {
          "task": "2.2",
          "file": "cmd/perfgate/main.go",
          "symbol": "evaluate",
          "anchor": "cells, err := perfgate.ParseBenchstatCSV(csvOut)",
          "change": "Read both raw files' identities before benchstat, override every latency cell to INCONCLUSIVE when they differ, and populate Bytes and Allocs from ReadBenchmarkMetrics instead of the CSV."
        },
        {
          "task": "2.2",
          "file": "internal/perfgate/bench_metrics.go",
          "symbol": "ReadBenchmarkMetrics, SampleMetrics",
          "anchor": "NEW FILE — lands beside internal/perfgate/parse.go",
          "change": "New file. NEW: the raw per-sample reader for B/op and allocs/op and the median-to-MetricResult derivation."
        }
      ],
      "contract": {
        "states": [
          "identities-match: both raw files report the same RunnerIdentity; latency judged as today",
          "identities-differ: the two RunnerIdentity values differ; every cell's latency verdict is INCONCLUSIVE and the report line names both",
          "bytes-source: the B/op figure a cell is judged on comes from the median of the raw per-sample B/op fields, never from benchstat",
          "allocs-source: likewise for allocs/op",
          "bytes-verdict / allocs-verdict: decided in both identity states",
          "delta-derived: MetricResult.DeltaPct and .Significant are computed from the two medians, never left at their zero values",
          "zero-baseline: the baseline median for an axis is 0; DeltaPct is 0 when the candidate is also 0 and +Inf when it is positive",
          "identity-first: the identity comparison precedes any benchstat invocation, so a differing cpu: never reaches the unpaired path"
        ],
        "transitions": [
          {
            "input": "profile-30637802780/bench-evaluator.txt against profile-30637802780/bench-vm.txt",
            "state": "identities-match; latency judged by benchstat as today; TestPinnedProfile's 27 committed verdicts unchanged",
            "effect": "no-op",
            "evidence": "both files read `cpu: AMD EPYC 7763 64-Core Processor`, verified"
          },
          {
            "input": "profile-30614184386/bench-vm.txt as baseline against profile-30637802780/bench-vm.txt as candidate, ModeNonRegression",
            "state": "identities-differ: linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C against linux/amd64/AMD EPYC 7763 64-Core Processor; every latency cell INCONCLUSIVE",
            "effect": "forced",
            "evidence": "the spec's first scenario. Both corpora are committed; this is a real cross-runner VM-vs-VM pair, which is exactly the post-authorization shape that broke the v0.13.0 cut."
          },
          {
            "input": "that same cross-runner pair, bytes axis",
            "state": "bytes-verdict decided, not skipped: each cell's Old and New come from the raw medians and nonIncreasing runs against the cell's stated allowance",
            "effect": "set",
            "evidence": "the spec's 'Allocation counts and allocated bytes SHALL stay enforced across differing runners'. Measured feasibility: raw medians reproduce benchstat's Old and New for all 27 cells on both axes in profile-30637802780 with zero mismatches, so this substitution changes no number where a comparison was previously possible."
          },
          {
            "input": "that same cross-runner pair, allocs axis",
            "state": "allocs-verdict decided against a zero allowance",
            "effect": "set",
            "evidence": "same measurement; allocs matched benchstat exactly on all 27 cells"
          },
          {
            "input": "a cell present in the candidate but absent from the baseline (GoldsetCall/call-boundary is in profile-30637802780 and not in profile-30614184386 or profile-30630796967)",
            "state": "the cell has no baseline figure; its bytes and allocs are reported INCONCLUSIVE naming the missing baseline cell, not passed and not failed",
            "effect": "forced",
            "evidence": "verified: profile-30614184386 holds 26 cells, profile-30637802780 holds 27. A missing baseline figure is absence of evidence; judging it against zero would fail every newly added cell."
          },
          {
            "input": "identities-differ, ModeFirstAuthorization",
            "state": "the same rule applies: latency INCONCLUSIVE, bytes and allocs decided",
            "effect": "no-op",
            "evidence": "the spec conditions on the identities differing, not on the mode. First authorization pairs two arms of one run, so in practice they never differ — the rule is stated for both modes so the code has no mode-dependent branch."
          },
          {
            "input": "either file reports an unknown identity (no cpu: line)",
            "state": "identities-differ is NOT concluded from an unknown; the comparison is reported INCONCLUSIVE naming `unknown` as one side",
            "effect": "forced",
            "evidence": "an unknown identity is not evidence that the runners matched, and equally not evidence that they differed. Treating unknown as a match would let a preamble-less baseline silently license a latency conclusion."
          },
          {
            "input": "an INCONCLUSIVE latency cell from the identity rule, reaching the -rerun invocation",
            "state": "Resolve() collapses it exactly as it collapses any other inconclusive cell: PASS except engine-sensitive under first authorization",
            "effect": "no-op",
            "evidence": "perfgate.go:176-181. A cross-runner latency cell is a non-regression claim that was not refuted, so PASS is the burden-of-proof answer ADR 0008:14 already gives. The release still fails on any bytes or allocs regression, which is what makes this safe."
          },
          {
            "input": "a cell whose baseline and candidate medians differ",
            "state": "delta-derived: DeltaPct = (New-Old)/Old*100, Significant true",
            "effect": "set",
            "evidence": "perfgate.go:297-303 — nonIncreasing returns PASS on DeltaPct <= 0 before reading New-Old, so an underived DeltaPct passes every cell"
          },
          {
            "input": "GoldsetCall/call-boundary, 0 B/op in both files",
            "state": "zero-baseline: DeltaPct 0",
            "effect": "no-op",
            "evidence": "internal/perfgate/testdata/profile-30637802780/bench-vm.txt — the cell reads 0 B/op and 0 allocs/op"
          },
          {
            "input": "GoldsetCall/call-boundary, 0 B/op baseline against a positive candidate",
            "state": "zero-baseline: DeltaPct +Inf",
            "effect": "forced",
            "evidence": "a first allocation on a zero-byte cell must fail, not divide by zero"
          },
          {
            "input": "a cell in the candidate with no baseline row",
            "state": "reported INCONCLUSIVE at the report layer, naming the missing baseline cell",
            "effect": "forced",
            "evidence": "cmd/perfgate/main.go:133-137 owns the verdict line; Evaluate is unchanged"
          },
          {
            "input": "profile-30614184386/bench-vm.txt against profile-30637802780/bench-vm.txt (cpu: differs)",
            "state": "identity-first: benchstat is not invoked for pairing; the cell loop runs to completion",
            "effect": "forced",
            "evidence": "D2 ordering; without it cmd/perfgate/main.go:95-98 returns (0, err) and exits before the cell loop at :109"
          }
        ],
        "forbidden": [
          "Concluding a latency PASS or FAIL from a comparison whose two identities differ.",
          "Skipping, defaulting or zeroing the bytes or allocs verdict when identities differ.",
          "Taking bytes or allocs from benchstat once the raw reader exists — one source per axis, or the two can disagree silently.",
          "Judging a cell whose baseline figure is missing against an Old of 0.",
          "Treating an unknown identity as equal to any other identity, including another unknown.",
          "Changing perfgate.Evaluate's signature or its per-tier rules. The identity rule lives above it.",
          "Leaving MetricResult.DeltaPct or .Significant at their zero values when populating Bytes or Allocs from raw medians.",
          "Computing (New-Old)/Old without a guard on Old == 0.",
          "Adding a state to Verdict or changing Evaluate's signature to express the missing-baseline case.",
          "Invoking benchstat for a paired comparison when the two runner identities differ. That is the path that aborts before the cell loop and silently forecloses the bytes and allocs verdicts the spec requires.",
          "Leaving the precedence between exit 3, exit 1 and exit 2 to fall out of control flow.",
          "Assuming the two input files carry the same number of samples per cell.",
          "Authoring a test in internal/perfgate against logic that lives in cmd/perfgate. internal/perfgate cannot import package main, so such a test asserts against a reimplementation and goes green while the shipped binary is untested on this seam's central requirement.",
          "Moving the identity comparison or the report-layer missing-baseline line into internal/perfgate to make a misplaced test compile. If that relocation is wanted it is a design change, not a test fix.",
          "Exposing only the median from SampleMetrics. c4 needs the per-sample figures and must not reimplement the parser to get them."
        ],
        "seeding": [
          "identities-match: the profile-30637802780 pair, committed.",
          "identities-differ: profile-30614184386/bench-vm.txt against profile-30637802780/bench-vm.txt, both committed. No synthetic file.",
          "bytes-source / allocs-source: ReadBenchmarkMetrics over the committed raw files; the test cross-checks its output against the committed benchstat.csv Old/New columns for the matching-identity profile, which is the measurement that licenses the substitution.",
          "missing-baseline-cell: GoldsetCall/call-boundary, present in profile-30637802780 and absent from profile-30614184386. Already committed; do not construct one.",
          "unknown identity: strings.NewReader over a preamble with the cpu: line removed, as in the runner-identity seam.",
          "Never: hand-constructing a CellComparison to reach identities-differ. The state must come from two real files, because the whole defect was that the identity was never read.",
          "zero-baseline: GoldsetCall/call-boundary in the committed profile-30637802780 corpus, which genuinely reads 0 B/op — not a constructed literal.",
          "cmd-level tests: cmd/perfgate/main_test.go already exists at c3 because c2 creates it. Raw corpora are reached by relative path from cmd/perfgate; the identity-first path never calls benchstat, so those tests need no seam substitution."
        ],
        "budgets": [
          "27: cells judged when both files are profile-30637802780; 26 when the baseline is profile-30614184386.",
          "1: cells whose bytes and allocs are inconclusive for want of a baseline figure on that cross-runner pair (GoldsetCall/call-boundary).",
          "10: samples per cell per file, from which the median is taken.",
          "0: numbers that change on the matching-identity path — verified against all 27 cells on both axes.",
          "2: identity strings named in an INCONCLUSIVE latency report line.",
          "10: samples per cell per file in every committed corpus — an EVEN count, so \"the median\" is a choice (lower middle, upper middle, or the mean of the two). The convention must be the one benchstat uses; the coder verifies it against benchstat's own definition rather than assuming, because 14 of the 27 cells in profile-30637802780 have zero spread and cannot discriminate between conventions.",
          "Sample counts may differ between the -old and -candidate files: the rerun step re-measures at doubled benchtime (release.yml:168-178) while a stored baseline keeps its original count. The reader must not assume equal counts."
        ]
      },
      "redTasks": [
        "1.1 Author TestCrossRunner_LatencyInconclusiveBytesEnforced in cmd/perfgate/main_test.go (the file c2 creates), exercising run() — the identity comparison and the latency override live in cmd/perfgate's evaluate, which internal/perfgate cannot import: the two committed cross-runner VM files produce INCONCLUSIVE latency naming both identities while bytes and allocs verdicts are decided.",
        "1.1 Author TestCrossRunner_UnknownIdentityIsNotAMatch in cmd/perfgate/main_test.go — it asserts the gate's decision rule, which lives in evaluate.",
        "2.2 Author TestReadBenchmarkMetrics_MatchesBenchstat in internal/perfgate — it targets ReadBenchmarkMetrics, which lands there: the raw medians equal the committed benchstat.csv Old and New for all 27 cells on both axes.",
        "2.2 Author TestCrossRunner_MissingBaselineCellIsInconclusive in cmd/perfgate/main_test.go — the missing-baseline line is produced at the report layer (main.go:133-137) using GoldsetCall/call-boundary.",
        "2.2 Author TestReadBenchmarkMetrics_DeltaPctDerivation in internal/perfgate — it targets the median-to-MetricResult derivation: for a cell whose median moves, DeltaPct is the signed percentage and Significant is true — a zero-value MetricResult must fail this test, because DeltaPct=0 makes nonIncreasing pass every cell unconditionally (perfgate.go:297-303 returns PASS on DeltaPct <= 0 before it ever reads New-Old).",
        "2.2 Author TestCrossRunner_ZeroBaselineBytesFails in internal/perfgate — it targets the same derivation at Old == 0 using GoldsetCall/call-boundary, which reads 0 B/op and 0 allocs/op in profile-30637802780: unchanged at 0 passes, and any positive candidate figure fails rather than producing NaN or a silent pass.",
        "1.1 The cross-runner tests seed from the raw corpora by path — internal/perfgate/testdata/profile-30614184386/bench-vm.txt as baseline against profile-30637802780/bench-vm.txt as candidate — reached from cmd/perfgate as ../../internal/perfgate/testdata/... . benchstat is never invoked on this path, so no seam substitution is needed for them.",
        "2.2 Author the cmd-level assertion on the message this seam owns: the stderr or report line for a cross-runner pair names BOTH RunnerIdentity values. That assertion is c3's, not c2's."
      ],
      "codeTasks": [
        "2.2 Add ReadBenchmarkMetrics and SampleMetrics to internal/perfgate.",
        "2.2 Add the identity comparison and the per-cell latency override to cmd/perfgate's evaluate, reading both raw paths that are already passed as -old and -candidate.",
        "2.2 Populate CellComparison.Bytes and .Allocs from ReadBenchmarkMetrics rather than from ParseBenchstatCSV; keep Latency on the benchstat path.",
        "2.2 Derive MetricResult from SampleMetrics explicitly — never leave a field at its zero value. Old and New are the per-cell medians. DeltaPct = (New-Old)/Old*100 when Old > 0; when Old == 0, DeltaPct is 0 if New == 0 and math.Inf(1) if New > 0, so a first allocation on a 0 B/op cell fails instead of dividing by zero. Significant is true on both axes — these are exact counts and nonIncreasing carries no significance gate (perfgate.go:288-292). N is the sample count; PValue stays 0 and is unread on these axes.",
        "2.2 cmd/perfgate: a cell present in the candidate but absent from the baseline is reported at the report layer as `<name>: INCONCLUSIVE (no baseline figure for this cell)` and counted into the needs-rerun signal. Evaluate's signature and Verdict's states are unchanged — the report layer already owns the verdict line format (main.go:133-137).",
        "2.2 Order the two paths explicitly in cmd/perfgate's evaluate, because they otherwise contradict each other on the same input. Read both raw files' RunnerIdentity FIRST. When they differ, do not invoke benchstat for a paired comparison at all: set every cell's latency to INCONCLUSIVE naming both identities, populate Bytes and Allocs from ReadBenchmarkMetrics, and run the cell loop to completion. ErrUnpairedComparison therefore cannot arise from a differing cpu:, and the cross-runner obligation is never foreclosed by an abort.",
        "2.2 State the exit precedence, which is currently unstated anywhere: 3 (the gate could not be configured, or evidence that should have paired did not) outranks 1 (a cell failed) outranks 2 (a cell needs a rerun). A pairing refusal that survives the identity check is a hard exit 3 before the cell loop — correct, because identities matched, so no cross-runner obligation applies to it.",
        "2.2 SampleMetrics exposes the per-sample B/op and allocs/op figures, not only the median, so c4's spread tests recompute max-minus-min through this reader instead of parsing the raw files a second time. Name the accessor when the type lands."
      ],
      "redTests": [
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestReadBenchmarkMetrics_DeltaPctDerivation",
        "TestCrossRunner_ZeroBaselineBytesFails"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c4",
      "taskIds": [
        "3.1"
      ],
      "prev": "c3",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "bytes-allowance-values",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/..."
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "internal/perfgate/tiers.json",
          "symbol": "bytesAllowanceBOp",
          "anchor": "\"bytesAllowanceBOp\": {",
          "change": "Extend from one entry (Goldset/guard-nil: 4) to all 27 cells listed under \"cells\", each sized from observed spread, with the spread recorded in this file's \"comment\" the way guard-nil's is today ('three hosted runs ... each read 1128 against 1129 B/op, +0.09%, p=0.000, 0% CI on both arms'). The evidence for the other 27 is not in the repo (see gaps)."
        }
      ],
      "contract": {
        "states": [
          "allowance-stated: every one of the 27 cells has a bytesAllowanceBOp entry",
          "allowance-justified: for every cell except Goldset/guard-nil, entry <= max observed within-run spread across the committed profiles",
          "allowance-zero: the 13 GoldsetParse/* cells and GoldsetCall/call-boundary state 0, which is the exact non-increasing bound"
        ],
        "transitions": [
          {
            "input": "the 13 GoldsetParse/* cells",
            "state": "allowance-zero: stated 0; bit-identical B/op across all ten samples in both profiles",
            "effect": "set",
            "evidence": "supplied spread measurement: every GoldsetParse cell reads 0 spread in profile-30630796967 and 0 in profile-30637802780. A stated 0 preserves the invariant tiers.json's comment and ADR 0008:60-62 record."
          },
          {
            "input": "GoldsetCall/call-boundary",
            "state": "allowance-zero: stated 0; spread 0 in profile-30637802780 and absent from profile-30630796967",
            "effect": "set",
            "evidence": "supplied measurement (0 B/op, bit-identical); the cell's absence from the older profile is verified and means its evidence base is one profile, not two"
          },
          {
            "input": "Goldset/queue-promote",
            "state": "allowance 8, the largest in the table; spread 7 in profile-30630796967 and 8 in profile-30637802780 over a ~16.5 KB figure",
            "effect": "set",
            "evidence": "supplied measurement (16582..16593). 8 equals one Go size class, so this allowance alone could absorb one 8-byte allocation on the bytes axis; the allocs axis keeps a zero allowance and would catch it."
          },
          {
            "input": "Goldset/rule-load",
            "state": "allowance 6; spread 3 then 6 (5840..5848)",
            "effect": "set",
            "evidence": "supplied measurement. This is the startup cell, so its allowance reaches nonIncreasing through evaluateStartup (perfgate.go:263)."
          },
          {
            "input": "Goldset/guard-nil",
            "state": "allowance 4, unchanged, exempt from the spread rule",
            "effect": "no-op",
            "evidence": "its within-run spread is 0; the 4 covers a reproducible between-engine offset of +1 B/op licensed by ADR 0008:50-74 and three named hosted runs. Re-deriving it is out of scope."
          },
          {
            "input": "the remaining nine Goldset/* cells",
            "state": "allowance = max observed spread: counter-closure 1, kw-lookup 1, loop-sum 2, merge-config 3, pipeline 2, registry-fold 2, route-decision 1, safe-parse 2, text-render 1",
            "effect": "set",
            "evidence": "supplied per-cell spread measurement across profile-30630796967 and profile-30637802780"
          },
          {
            "input": "a future edit raising any allowance above the spread the committed corpora record",
            "state": "TestBytesAllowancesAreJustifiedBySpread fails, naming the cell, the stated value and the measured spread",
            "effect": "forced",
            "evidence": "the spec's 'SHALL NOT be widened to admit a measured regression'. Bounding the allowance by within-run spread makes it structurally unable to admit a regression: a regression shifts the median while leaving the spread where it was."
          },
          {
            "input": "the pinned profile re-evaluated with all 27 allowances in place",
            "state": "every committed verdict in profile-30637802780/verdict.txt is unchanged",
            "effect": "no-op",
            "evidence": "verified: engine-sensitive cells take the inline 20% bytes floor no allowance reaches (perfgate.go:293-296); the allowance-reading cells under first authorization are the 13 GoldsetParse (delta 0), guard-nil (unchanged) and rule-load (bytes -36.22%, so nonIncreasing passes at DeltaPct <= 0 before the allowance is consulted)."
          }
        ],
        "forbidden": [
          "A size-proportional or percentage allowance. The spec ties the number to observed spread on that cell.",
          "Rounding an allowance up to a size class, or to any value above the measured spread.",
          "Giving Goldset/guard-nil a new derivation, or removing its 4.",
          "Recording a number without the profile ids it came from.",
          "Raising an allowance in the same change as a bytes regression it would admit.",
          "Deriving any number from a developer-box run. tiers.json's comment already states a developer-box figure licenses nothing, and only bytes and allocs are locally exact in any case.",
          "Deriving any number from profile-30630796967: its committed files are a 400ms measurement and would understate the spread the gate sees at 200ms.",
          "Deriving a number from a difference taken between two profiles. They measure different trees at different benchtimes; such a figure blends code change, benchtime and hardware and presents it as noise.",
          "Leaving tiers.json:2's \"no allowance\" sentence in place. It is false the moment every cell carries an entry, and c9 cannot fix it — this chunk owns that file."
        ],
        "seeding": [
          "profile-30637802780/bench-vm.txt alone is the evidence base: ten samples at the gate's own BENCHTIME of 200ms (release.yml:66). For each cell, span = max(B/op) - min(B/op) over those ten samples; the allowance is that span.",
          "profile-30630796967 is a directional check only. Its committed files are a 400ms rerun measurement (its README.md:4-17, 57092 iterations against 28188), and B/op is a total over an iteration count, so twice the iterations halves the rounding granularity and every one of its spreads is <= its 200ms counterpart.",
          "profile-30614184386 is the cross-runner fixture, not an allowance source: it records a third CPU.",
          "Spread is recomputed through c3's SampleMetrics per-sample accessor, not by a second raw-file parser in the test."
        ],
        "budgets": [
          "27: cells in tiers.json — 13 Goldset/*, 1 GoldsetCall/call-boundary, 13 GoldsetParse/*.",
          "13: entries with a non-zero value, all of them Goldset/* cells (guard-nil among them, keeping its existing 4).",
          "14: entries stating 0 — the 13 GoldsetParse/* cells plus GoldsetCall/call-boundary, every one bit-identical across all ten samples.",
          "1: profiles in the evidence base.",
          "8: the largest allowance in the table (Goldset/queue-promote), and one Go size class. Safe only because the allocs axis keeps a zero allowance.",
          "Expected table, all 27 entries: Goldset/counter-closure 1, Goldset/guard-nil 4 (the between-engine exception, unchanged), Goldset/kw-lookup 1, Goldset/loop-sum 2, Goldset/merge-config 3, Goldset/pipeline 2, Goldset/queue-promote 8, Goldset/registry-fold 2, Goldset/route-decision 1, Goldset/rule-load 6, Goldset/safe-parse 2, Goldset/text-render 1, Goldset/twice-macro 2 — thirteen non-zero, every one a Goldset/* cell. Then GoldsetCall/call-boundary 0 and each of the thirteen GoldsetParse/* cells 0 — fourteen stated zeros. Twenty-seven entries."
        ]
      },
      "redTasks": [
        "3.1 Author TestBytesAllowancesAreJustifiedBySpread in internal/perfgate: recompute per-cell spread from the 200ms profile (profile-30637802780) alone and require every stated allowance to be <= it — a min or max taken across both profiles is wrong, and min-over-both would fail the plan's own values (rule-load 6 against the 400ms spread of 3, queue-promote 8 against 7), with Goldset/guard-nil exempted by name.",
        "3.1 Author TestTierConfig_EveryCellStatesAnAllowance: all 27 cells in `cells` appear in `bytesAllowanceBOp`.",
        "3.1 Author TestBytesAllowanceSpread_LongerBenchtimeIsTighter: every cell's 400ms span in profile-30630796967 is <= its 200ms span in profile-30637802780, which is why the 200ms profile is the evidence base and the other is only a check."
      ],
      "codeTasks": [
        "3.1 internal/perfgate/tiers.json: add the 26 missing bytesAllowanceBOp entries, expected values: Goldset/counter-closure 1, Goldset/guard-nil 4 (the between-engine exception, unchanged), Goldset/kw-lookup 1, Goldset/loop-sum 2, Goldset/merge-config 3, Goldset/pipeline 2, Goldset/queue-promote 8, Goldset/registry-fold 2, Goldset/route-decision 1, Goldset/rule-load 6, Goldset/safe-parse 2, Goldset/text-render 1, Goldset/twice-macro 2 — thirteen non-zero, every one a Goldset/* cell. Then GoldsetCall/call-boundary 0 and each of the thirteen GoldsetParse/* cells 0 — fourteen stated zeros. Twenty-seven entries. These are the expected RESULT of the derivation, not a substitute for it — recompute every one from profile-30637802780/bench-vm.txt and fail the seam if any disagrees.",
        "3.1 internal/perfgate/tiers.json comment: record the derivation rule and the profile id the numbers came from, AND rewrite the now-false sentence at tiers.json:2 — \"Every other data-dominated cell, including all thirteen GoldsetParse/* cells, keeps the exact non-increasing bound with no allowance\" — to say that every cell states an allowance and that those fourteen state an explicit 0, which IS that exact bound. ADR 0008 carries the same sentence and c9 rewrites it there; the two must say the same thing."
      ],
      "redTests": [
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance",
        "TestBytesAllowanceSpread_LongerBenchtimeIsTighter"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c5",
      "taskIds": [
        "1.2",
        "3.2"
      ],
      "prev": "c4",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "bytes-allowance-config",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/..."
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "internal/perfgate/perfgate_test.go",
          "symbol": "TestLoadTierConfig_MissingBytesAllowance (NEW)",
          "anchor": "// TestLoadTierConfig_BytesAllowance_UnknownCell guards against a typo'd",
          "change": "NEW test beside the existing typo guard: a cells entry with no bytesAllowanceBOp key makes LoadTierConfig return an error naming the cell and the missing allowance, instead of yielding CellTier{BytesAllowanceBOp: 0}."
        },
        {
          "task": "3.2",
          "file": "internal/perfgate/parse.go",
          "symbol": "LoadTierConfig",
          "anchor": "for name, allowance := range file.BytesAllowanceBOp {",
          "change": "Invert the optionality: after the cells loop assigns tiers, require every cell to appear in file.BytesAllowanceBOp and return the package's standard 'perfgate: cell %q: ...' error when it does not. CellTier.BytesAllowanceBOp (parse.go:192-195) is a plain float64, so absence and a stated zero are currently indistinguishable. Allocs keep a hardcoded 0 allowance at their nonIncreasing call sites (perfgate.go:194, 221, 242, 266)."
        }
      ],
      "contract": {
        "states": [
          "allowance-present-nonzero: the cell states a positive allowance; nonIncreasing gets it",
          "allowance-present-zero: the cell states 0; nonIncreasing gets 0, the exact non-increasing bound",
          "allowance-absent: the cell is in `cells` and not in `bytesAllowanceBOp`; LoadTierConfig fails",
          "allocs-allowance: always 0, never configurable",
          "TestEvaluate_BytesAllowance (perfgate_test.go:590-650) is NOT affected: it builds CellComparison literals and calls Evaluate directly, never constructing a tier config. Its \"unlisted cell gets no allowance\" subtest name is stale wording in a passing test, left alone."
        ],
        "transitions": [
          {
            "input": "the shipped tiers.json after bytes-allowance-values",
            "state": "all 27 cells present; LoadTierConfig succeeds",
            "effect": "no-op",
            "evidence": "TestPinnedProfile loads the real tiers.json (perfgate_test.go:702-705) and must keep passing"
          },
          {
            "input": "a tier config naming a cell in `cells` with no `bytesAllowanceBOp` entry",
            "state": "allowance-absent: error satisfying errors.Is(err, ErrMissingBytesAllowance), message naming the cell",
            "effect": "forced",
            "evidence": "the spec's 'the gate SHALL fail with a missing-config error when a cell it is asked to judge has none, rather than reading the absence as an allowance of zero'"
          },
          {
            "input": "a tier config stating `\"GoldsetParse/kw-lookup\": 0`",
            "state": "allowance-present-zero: loads cleanly; CellTier{BytesAllowanceBOp: 0, BytesAllowanceStated: true}",
            "effect": "set",
            "evidence": "the reading taken in the design's D4: an explicit 0 is a stated allowance enforcing the exact non-increasing bound; an absent entry is the error — a stated 0 is a recorded decision, not silence, and enforces the same exact bound"
          },
          {
            "input": "a tier config whose bytesAllowanceBOp names a cell absent from `cells`",
            "state": "the existing error is unchanged: `perfgate: bytesAllowanceBOp names cell %q, absent from cells`",
            "effect": "no-op",
            "evidence": "parse.go:216-220, already covered by TestLoadTierConfig_BytesAllowance_UnknownCell (perfgate_test.go:406)"
          },
          {
            "input": "a negative allowance, e.g. -4",
            "state": "rejected with a stated error naming the cell",
            "effect": "forced",
            "evidence": "nonIncreasing compares `m.New-m.Old <= allowanceBOp` (perfgate.go:301); a negative allowance would fail a cell whose bytes did not increase, which no requirement asks for. Nothing rejects it today."
          },
          {
            "input": "any cell's allocs axis",
            "state": "allocs-allowance stays 0 and stays unreachable from tiers.json",
            "effect": "no-op",
            "evidence": "perfgate.go:194, :221, :242, :266 all pass a literal 0; the spec's 'Allocation counts are exact in the same output and SHALL keep a zero allowance'"
          }
        ],
        "forbidden": [
          "Defaulting an absent entry to 0 anywhere, including in cmd/perfgate.",
          "Adding an allocs allowance key to tiers.json or an allowance parameter to the allocs call sites.",
          "Reporting the missing-allowance failure as a per-cell Result{Verdict: VerdictFail}. It is a configuration defect and must stop the run, not be reported as one cell's measurement.",
          "Letting a configuration error exit 2 and trigger the doubled-benchtime rerun.",
          "Changing LoadTierConfig's signature.",
          "Leaving internal/perfgate red for a later chunk to repair. The chunk that makes an absent allowance an error owns every existing fixture that absence made valid."
        ],
        "seeding": [
          "allowance-present-*: the shipped internal/perfgate/tiers.json for the happy path; strings.NewReader over a small hand-written JSON literal for the single-cell arms, matching how TestLoadTierConfig and TestLoadTierConfig_BytesAllowance already seed (perfgate_test.go:369-418).",
          "allowance-absent: a hand-written literal with one cell in `cells` and an empty `bytesAllowanceBOp`. Unreachable from the shipped file after bytes-allowance-values, so a literal is the only legal path.",
          "Never: deleting an entry from the real tiers.json inside a test."
        ],
        "budgets": [
          "27: cells that must each state an allowance.",
          "0: the allocs allowance, at all four call sites.",
          "3: the exit code a missing allowance produces.",
          "1: the number of load-time passes over the cell set — the check runs inside LoadTierConfig, not per judged cell."
        ]
      },
      "redTasks": [
        "1.2 Author TestLoadTierConfig_MissingBytesAllowance: a cell with no entry yields errors.Is(err, ErrMissingBytesAllowance) and a message naming the cell.",
        "1.2 Author TestLoadTierConfig_StatedZeroIsNotAbsent: a stated 0 loads cleanly and sets BytesAllowanceStated.",
        "3.2 Author TestLoadTierConfig_NegativeBytesAllowance."
      ],
      "codeTasks": [
        "3.2 parse.go: tierConfigFile.BytesAllowanceBOp becomes map[string]*float64; CellTier gains BytesAllowanceStated bool; LoadTierConfig requires an entry for every cell and rejects a negative value; add ErrMissingBytesAllowance.",
        "3.2 perfgate_test.go: update the two existing tier-config fixtures this change invalidates, in the chunk that invalidates them, so the package is never left red. TestLoadTierConfig (:369-379) — its literal carries no bytesAllowanceBOp map, and its :378 assertion states the very semantics this chunk abolishes, so the assertion is replaced by the stated-zero semantics, not just the literal. TestLoadTierConfig_BytesAllowance (:392-401) — two cells in `cells`, one allowance; give both an entry and keep the assert.Zero on the stated-zero cell. TestLoadTierConfig_UnknownTier (:381-388) needs no edit: its bogus tier errors first and the test asserts only require.Error."
      ],
      "redTests": [
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c6",
      "taskIds": [
        "1.3",
        "4.3"
      ],
      "prev": "c5",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "gate-mode-resolution",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/..."
      ],
      "sites": [
        {
          "task": "1.3",
          "file": "internal/perfgate/perfgate_test.go",
          "symbol": "TestResolveBaselineMode (NEW)",
          "anchor": "func TestLoadTierConfig_UnknownTier(t *testing.T) {",
          "change": "NEW test that a baseline-lookup error and an empty baseline set are distinct outcomes, and only the empty set yields ModeFirstAuthorization. No code under test exists in Go today: mode resolution lives only in release.yml shell (see gaps)."
        },
        {
          "task": "4.3",
          "file": ".github/workflows/release.yml",
          "symbol": "Determine gate mode",
          "anchor": "            if gh release download \"$tag\" --pattern bench-vm.txt --output baseline-vm.txt 2>/dev/null; then",
          "change": "No edit in this chunk. Task 4.3's Go half — the four-way BaselineLookup taxonomy that distinguishes an absent asset from an API or auth failure — lands in internal/perfgate/gatemode.go here; c7 restructures this step to collect the lookup and to fail on resolve-mode's exit code, because the workflow cannot call a subcommand that does not exist yet."
        }
      ],
      "contract": {
        "states": [
          "baseline-found: a stored bench-vm.txt was enumerated and downloaded; ModeNonRegression",
          "baseline-absent: every enumerated release was inspected and none carries the asset; ModeFirstAuthorization",
          "enumeration-failed: listing releases or listing one release's assets failed; ModeUnknown and an error",
          "download-failed: the asset was seen in a release's asset list and the download then failed; ModeUnknown and an error"
        ],
        "transitions": [
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.12.0\"], DownloadedTag: \"v0.12.0\"}",
            "state": "baseline-found: (ModeNonRegression, BaselineFound, nil)",
            "effect": "set",
            "evidence": "release.yml:105-107 selects non-regression on exactly this observation today"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.11.0\",\"v0.12.0\"], DownloadedTag: \"\"}",
            "state": "baseline-absent: (ModeFirstAuthorization, BaselineAbsent, nil)",
            "effect": "set",
            "evidence": "the spec's 'First-authorization SHALL be selected only when the repository is known to hold no baseline'. Known, here, means every enumerated tag was inspected and none carried the asset."
          },
          {
            "input": "BaselineLookup{EnumerationOK: false}",
            "state": "enumeration-failed: (ModeUnknown, BaselineEnumerationFailed, ErrBaselineEnumerationFailed)",
            "effect": "forced",
            "evidence": "defect 4: `gh release list` is unchecked at release.yml:98, so an API error yields an empty tag list and silently selects first-authorization improvement thresholds"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.12.0\"], DownloadFailed: true}",
            "state": "download-failed: (ModeUnknown, BaselineDownloadFailed, ErrBaselineDownloadFailed)",
            "effect": "forced",
            "evidence": "the spec's 'enumerating or downloading the stored baseline fails for any reason other than the baseline not existing'"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: []}",
            "state": "baseline-absent: a repository with no releases at all holds no baseline; ModeFirstAuthorization",
            "effect": "set",
            "evidence": "this is the genuine first-authorization case the mode exists for"
          },
          {
            "input": "the zero BaselineLookup",
            "state": "enumeration-failed: EnumerationOK is false by zero value, so an unpopulated lookup fails closed rather than selecting first-authorization",
            "effect": "forced",
            "evidence": "the same fail-closed reasoning perfgate.go:33-38 and :44-50 apply to ModeUnknown and VerdictUnknown"
          },
          {
            "input": "a workflow_dispatch run against a tree whose repository holds a v0.13.0 baseline",
            "state": "baseline-found: the dispatch reports non-regression, the same mode the release run reaches",
            "effect": "forced",
            "evidence": "the spec's 'Dispatch and release agree on the rule'; task 4.1 and, on a hosted runner, task 6.2"
          },
          {
            "input": "RELEASE_TAG empty on a dispatch run",
            "state": "the skip line `[ \"$tag\" = \"$RELEASE_TAG\" ] && continue` matches nothing, so the newest release is considered first",
            "effect": "no-op",
            "evidence": "release.yml:99. On a dispatch there is no release being cut, so there is nothing to exclude."
          }
        ],
        "forbidden": [
          "Returning ModeFirstAuthorization together with a non-nil error, or any Mode other than ModeUnknown on a failure.",
          "Deciding the mode from github.event_name anywhere.",
          "Treating a non-zero `gh release download` as absence. Absence is decided from the asset list, never from a download exit code.",
          "Leaving `gh release list` unchecked.",
          "Publishing a baseline or uploading a release asset from the resolve-mode path — it reads only.",
          "Making resolve-mode a second binary. It is a subcommand of the existing cmd/perfgate so the workflow builds one thing.",
          "Authoring a test that invokes `perfgate resolve-mode`. The subcommand lands in c7; a CLI test here fails this chunk's own verify.",
          "Editing .github/workflows/release.yml in this chunk. The workflow half of task 4.3 is carried by c7, which restructures the same step."
        ],
        "seeding": [
          "All four states: a BaselineLookup struct literal in internal/perfgate. No network, no gh, no filesystem — the point of the seam is that the classification is pure.",
          "Never: shelling out to gh from a test, or asserting the workflow YAML's shell body from Go.",
          "This chunk is the pure-Go arm only: ResolveGateMode is exercised by constructing BaselineLookup values in internal/perfgate. The `perfgate resolve-mode` subcommand does not exist yet — c7 adds it — so no CLI-arm test may be authored here."
        ],
        "budgets": [
          "4: outcomes in the taxonomy (found, absent, enumeration-failed, download-failed).",
          "1: outcome that selects ModeFirstAuthorization.",
          "30: the release enumeration limit, unchanged from release.yml:98.",
          "3: the exit code of a resolution failure.",
          "0: release assets written by the resolve-mode path."
        ]
      },
      "redTasks": [
        "1.3 Author TestResolveGateMode over all four states plus the zero lookup, asserting the Mode, the BaselineOutcome and errors.Is on the sentinel.",
        "4.3 Author TestResolveGateMode_FailureNeverSelectsFirstAuthorization: neither failure state returns ModeFirstAuthorization."
      ],
      "codeTasks": [
        "1.3 Add internal/perfgate/gatemode.go: BaselineLookup, BaselineOutcome and its constants, ResolveGateMode, ErrBaselineEnumerationFailed, ErrBaselineDownloadFailed."
      ],
      "redTests": [
        "TestResolveGateMode",
        "TestResolveGateMode_FailureNeverSelectsFirstAuthorization"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c7",
      "taskIds": [
        "4.1"
      ],
      "prev": "c6",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "gate-mode-resolution",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate",
        "cmd/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/...",
        "./cmd/perfgate/..."
      ],
      "sites": [
        {
          "task": "4.1",
          "file": ".github/workflows/release.yml",
          "symbol": "Determine gate mode",
          "anchor": "      - name: Determine gate mode",
          "change": "Drop the event gate so a workflow_dispatch run resolves mode from the stored baselines too; the loop's 'skip the release being gated' line ([ \"$tag\" = \"$RELEASE_TAG\" ] && continue) must stay correct when RELEASE_TAG is empty on dispatch. Downstream reader is 'Evaluate performance gate' (line 145), whose `[ -z \"$mode\" ] && mode=\"first-authorization\"` default (line 150) is the silent fallback this task removes. The anchor is the step name, not its `if:` line: that line appears verbatim on `Store VM baseline on the authorized release` (release.yml:211) too, and removing it there would let a dispatch publish a baseline. Never touch the guard on that step."
        }
      ],
      "contract": {
        "states": [
          "baseline-found: a stored bench-vm.txt was enumerated and downloaded; ModeNonRegression",
          "baseline-absent: every enumerated release was inspected and none carries the asset; ModeFirstAuthorization",
          "enumeration-failed: listing releases or listing one release's assets failed; ModeUnknown and an error",
          "download-failed: the asset was seen in a release's asset list and the download then failed; ModeUnknown and an error"
        ],
        "transitions": [
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.12.0\"], DownloadedTag: \"v0.12.0\"}",
            "state": "baseline-found: (ModeNonRegression, BaselineFound, nil)",
            "effect": "set",
            "evidence": "release.yml:105-107 selects non-regression on exactly this observation today"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.11.0\",\"v0.12.0\"], DownloadedTag: \"\"}",
            "state": "baseline-absent: (ModeFirstAuthorization, BaselineAbsent, nil)",
            "effect": "set",
            "evidence": "the spec's 'First-authorization SHALL be selected only when the repository is known to hold no baseline'. Known, here, means every enumerated tag was inspected and none carried the asset."
          },
          {
            "input": "BaselineLookup{EnumerationOK: false}",
            "state": "enumeration-failed: (ModeUnknown, BaselineEnumerationFailed, ErrBaselineEnumerationFailed)",
            "effect": "forced",
            "evidence": "defect 4: `gh release list` is unchecked at release.yml:98, so an API error yields an empty tag list and silently selects first-authorization improvement thresholds"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.12.0\"], DownloadFailed: true}",
            "state": "download-failed: (ModeUnknown, BaselineDownloadFailed, ErrBaselineDownloadFailed)",
            "effect": "forced",
            "evidence": "the spec's 'enumerating or downloading the stored baseline fails for any reason other than the baseline not existing'"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: []}",
            "state": "baseline-absent: a repository with no releases at all holds no baseline; ModeFirstAuthorization",
            "effect": "set",
            "evidence": "this is the genuine first-authorization case the mode exists for"
          },
          {
            "input": "the zero BaselineLookup",
            "state": "enumeration-failed: EnumerationOK is false by zero value, so an unpopulated lookup fails closed rather than selecting first-authorization",
            "effect": "forced",
            "evidence": "the same fail-closed reasoning perfgate.go:33-38 and :44-50 apply to ModeUnknown and VerdictUnknown"
          },
          {
            "input": "a workflow_dispatch run against a tree whose repository holds a v0.13.0 baseline",
            "state": "baseline-found: the dispatch reports non-regression, the same mode the release run reaches",
            "effect": "forced",
            "evidence": "the spec's 'Dispatch and release agree on the rule'; task 4.1 and, on a hosted runner, task 6.2"
          },
          {
            "input": "RELEASE_TAG empty on a dispatch run",
            "state": "the skip line `[ \"$tag\" = \"$RELEASE_TAG\" ] && continue` matches nothing, so the newest release is considered first",
            "effect": "no-op",
            "evidence": "release.yml:99. On a dispatch there is no release being cut, so there is nothing to exclude."
          }
        ],
        "forbidden": [
          "Returning ModeFirstAuthorization together with a non-nil error, or any Mode other than ModeUnknown on a failure.",
          "Deciding the mode from github.event_name anywhere.",
          "Treating a non-zero `gh release download` as absence. Absence is decided from the asset list, never from a download exit code.",
          "Leaving `gh release list` unchecked.",
          "Publishing a baseline or uploading a release asset from the resolve-mode path — it reads only.",
          "Making resolve-mode a second binary. It is a subcommand of the existing cmd/perfgate so the workflow builds one thing."
        ],
        "seeding": [
          "All four states: a BaselineLookup struct literal in internal/perfgate. No network, no gh, no filesystem — the point of the seam is that the classification is pure.",
          "The CLI arm: a lookup JSON file written into t.TempDir() and passed as `perfgate resolve-mode -lookup <path>`, asserting the exit code and the stdout `mode=` line.",
          "Never: shelling out to gh from a test, or asserting the workflow YAML's shell body from Go."
        ],
        "budgets": [
          "4: outcomes in the taxonomy (found, absent, enumeration-failed, download-failed).",
          "1: outcome that selects ModeFirstAuthorization.",
          "30: the release enumeration limit, unchanged from release.yml:98.",
          "3: the exit code of a resolution failure.",
          "0: release assets written by the resolve-mode path."
        ]
      },
      "redTasks": [
        "4.1 Author TestRun_ResolveModeSubcommand in cmd/perfgate: a lookup file describing a found baseline prints `mode=non-regression` and exits 0; a lookup describing an enumeration failure exits 3."
      ],
      "codeTasks": [
        "4.1 cmd/perfgate/main.go: dispatch `resolve-mode` as a subcommand when it is args[0], leaving the existing flag path untouched for the two current invocations.",
        "4.1 .github/workflows/release.yml: remove `if: github.event_name == 'release'` from `Determine gate mode` (:92); restructure its script to inspect each release's asset list with `gh release view --json assets` before downloading, write a lookup JSON, and call `bin/perfgate resolve-mode`; move the `go build -o bin/perfgate ./cmd/perfgate` line (:148) ahead of it.",
        "4.1 release.yml: fail the step on a non-zero from resolve-mode instead of falling through to the `[ -z \"$mode\" ] && mode=\"first-authorization\"` default at :150, which is the second place absence and failure are conflated today. Moved here from c6: the workflow cannot fail on resolve-mode's exit code until this chunk adds the subcommand and moves the bin/perfgate build ahead of the step."
      ],
      "redTests": [
        "TestRun_ResolveModeSubcommand"
      ],
      "redRun": "go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...",
      "verify": "go build ./internal/perfgate/... ./cmd/perfgate/... && go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/... && go vet ./internal/perfgate/... ./cmd/perfgate/... && golangci-lint run ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "go-coder"
    },
    {
      "id": "c8",
      "taskIds": [
        "4.2"
      ],
      "prev": "c7",
      "sharedPkg": ".github/workflows",
      "parallel": false,
      "seam": "dispatch-publishes-nothing",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "4.2",
          "file": ".github/workflows/release.yml",
          "symbol": "Store VM baseline on the authorized release",
          "anchor": "        run: gh release upload \"$RELEASE_TAG\" bench-vm.txt --clobber",
          "change": "Verification site: this step's `if: github.event_name == 'release'` must remain, so widening the mode-resolution gate in 4.1 does not also make a dispatched run publish a baseline. 'Upload release evidence' (line 220) is the artifact-only path a dispatch keeps."
        }
      ],
      "contract": {
        "states": [
          "dispatch-run: github.event_name == 'workflow_dispatch'; mode resolved, benchmarks run, verdict computed, no release asset written",
          "release-run: github.event_name == 'release'; the same plus the baseline upload"
        ],
        "transitions": [
          {
            "input": "a workflow_dispatch run after gate-mode-resolution lands",
            "state": "dispatch-run: `Determine gate mode` now executes; `Store VM baseline on the authorized release` does not",
            "effect": "no-op",
            "evidence": ".github/workflows/release.yml:210-215 keeps `if: github.event_name == 'release'`; it holds the only `gh release upload` in the file"
          },
          {
            "input": "the `Upload release evidence` step on a dispatch run",
            "state": "runs, as it does today, and writes a workflow artifact — not a release asset",
            "effect": "no-op",
            "evidence": "release.yml:220-230 uses actions/upload-artifact and `if: always()`. Task 4.2 concerns release assets, so this step is unaffected."
          },
          {
            "input": "the baseline download inside the ungated `Determine gate mode` step on a dispatch",
            "state": "reads only; downloads baseline-vm.txt into the workspace and publishes nothing",
            "effect": "no-op",
            "evidence": "`gh release download` and `gh release view` are read operations; the step writes only $GITHUB_OUTPUT and a workspace file"
          }
        ],
        "forbidden": [
          "Removing `if: github.event_name == 'release'` from `Store VM baseline on the authorized release`.",
          "Adding a `gh release upload`, `gh release create` or `gh release edit` to any ungated step.",
          "Gating the upload on the resolved mode instead of the event — a dispatch that resolves first-authorization would then publish a baseline.",
          "Quoting the literal string `gh release upload` inside the added comment. This chunk's verify counts occurrences of it and expects exactly one; word the comment around the command name."
        ],
        "seeding": [
          "Reading .github/workflows/release.yml at the seam's completion and confirming exactly one step carries the release guard and exactly one `gh release upload` exists, and that they are the same step.",
          "Never: asserting YAML text from a Go test."
        ],
        "budgets": [
          "1: steps carrying `if: github.event_name == 'release'` after this change.",
          "1: `gh release upload` invocations in the file.",
          "0: release assets a dispatch run writes."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "4.2 release.yml: add a comment above the upload step recording that the guard is the only thing keeping a dispatch from consuming the baseline slot, and confirm by reading that no other step publishes.",
        "4.2 Confirm by assertion, not by reading: the workflow holds exactly one `gh release upload`, and the step holding it still carries `if: github.event_name == 'release'`. That pair is what keeps a dispatch from consuming the baseline slot, and it is what this chunk's verify runs."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "test \"$(grep -c 'gh release upload' .github/workflows/release.yml)\" = 1 && grep -A6 'name: Store VM baseline on the authorized release' .github/workflows/release.yml | grep -q \"if: github.event_name == 'release'\"",
      "coder": "zpatcher"
    },
    {
      "id": "c9",
      "taskIds": [
        "5.1",
        "5.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "gate-documentation",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "5.1",
          "file": "docs/adr/0008-consumer-performance-gate.md",
          "symbol": "## Thresholds",
          "anchor": "Note (non-increasing bounds and benchstat \"~\"): every tier with a bytes or",
          "change": "Add a note in the same voice as the three existing notes: baseline and candidate must be measured on the same runner identity for a latency conclusion, and allocation counts are exact per-op integers while B/op is an average that wobbles, so bytes carries a stated allowance where allocs stays exactly zero. The existing guard-nil note (line 50) already states the per-cell allowance mechanism and must stay consistent with 3.1 widening it to every cell."
        },
        {
          "task": "5.2",
          "file": "CHANGELOG.md",
          "symbol": "## [Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Add a `### Changed` subsection under the currently empty [Unreleased] heading recording the observable change: the gate reports latency as inconclusive when the stored baseline and the candidate were measured on different runners, and still enforces allocation counts and bytes. Follow the 0.13.0 gate entry's voice (line 38)."
        }
      ],
      "contract": {
        "states": [
          "adr-amended: ADR 0008 states the runner-comparability rule and the exact-allocs vs averaged-bytes distinction",
          "changelog-recorded: CHANGELOG.md's [Unreleased] -> Changed names the inconclusive-across-runners behaviour"
        ],
        "transitions": [
          {
            "input": "ADR 0008:60-62, 'The other thirteen data-dominated cells — every GoldsetParse/* cell — keep the exact non-increasing bound with no allowance.'",
            "state": "rewritten to say every cell states an allowance and that those thirteen state an explicit 0, which IS the exact non-increasing bound",
            "effect": "forced",
            "evidence": "the sentence describes an absent entry; after bytes-allowance-config an absent entry is a configuration error. The rule it states is preserved; only its mechanism changes."
          },
          {
            "input": "ADR 0008's Thresholds section",
            "state": "gains the runner-comparability rule: a latency conclusion requires matching runner identities, allocation counts and allocated bytes stay enforced regardless, and the gate never normalises a configuration line to obtain a comparison",
            "effect": "set",
            "evidence": "task 5.1"
          },
          {
            "input": "ADR 0008's Note on benchstat '~' (lines 34-40)",
            "state": "amended to record that the bytes and allocs axes no longer pass through benchstat at all, so the blind spot it describes now applies to latency only",
            "effect": "set",
            "evidence": "cross-runner-verdicts moves both axes onto a direct read of the raw files. Leaving this note unamended would leave the ADR describing a gap the change closed."
          },
          {
            "input": "CHANGELOG.md [Unreleased] -> Changed",
            "state": "one entry naming the inconclusive-latency-across-runners behaviour",
            "effect": "set",
            "evidence": "task 5.2. Keep a Changelog 1.1.0 shape, matching the file's existing sections."
          }
        ],
        "forbidden": [
          "Describing this as a relaxation of the GoldsetParse/* bound. The bound is unchanged; only its representation moved from absence to an explicit 0.",
          "Recording per-cell allowance numbers in the ADR. They belong in tiers.json, which the ADR already points at.",
          "Creating a new ADR. 0008 is the single owner of these numbers (ADR 0008:19).",
          "Claiming the benchstat '~' blind spot is fully closed — it remains open for latency.",
          "Editing internal/perfgate/tiers.json. Its comment is c4's, and this chunk runs in parallel with the serial chain that owns that file."
        ],
        "seeding": [
          "Read docs/adr/0008-consumer-performance-gate.md and internal/perfgate/tiers.json before editing; both carry the same sentence and both must move together.",
          "Read CHANGELOG.md's existing [Unreleased] section for its heading shape."
        ],
        "budgets": [
          "2: texts carrying the sentence that changes (ADR 0008:60-62 and tiers.json:2).",
          "1: CHANGELOG entry.",
          "0: new ADRs."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "5.1 docs/adr/0008-consumer-performance-gate.md: add the runner-comparability rule to Thresholds; amend the allowance note at :50-74; amend the benchstat-'~' note at :34-40 to scope it to latency.",
        "5.2 CHANGELOG.md: add the [Unreleased] -> Changed entry."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./cl/... && openspec validate release-gate-baseline-comparability --strict",
      "coder": "coder"
    },
    {
      "id": "c10",
      "taskIds": [
        "1.4"
      ],
      "prev": "c8",
      "sharedPkg": "internal/perfgate",
      "parallel": false,
      "seam": "existing-tests-unchanged",
      "shard": "",
      "pkgDirs": [
        "internal/perfgate"
      ],
      "pkgs": [
        "./internal/perfgate/..."
      ],
      "sites": [
        {
          "task": "1.4",
          "file": "internal/perfgate/perfgate_test.go",
          "symbol": "TestPinnedProfile",
          "anchor": "res := Evaluate(cell, ct.Tier, ModeFirstAuthorization)",
          "change": "Read-only confirmation that every existing test function still passes. The two fixtures a stated allowance requires were updated in c5, in the chunk that required them."
        }
      ],
      "contract": {
        "states": [
          "suite-green: every existing test function passes with no edit to its body",
          "pinned-verdicts-unchanged: TestPinnedProfile's 27 committed verdicts still match"
        ],
        "transitions": [
          {
            "input": "TestPinnedProfile after all 27 allowances land",
            "state": "pinned-verdicts-unchanged",
            "effect": "no-op",
            "evidence": "verified: no allowance-reading cell's verdict moves. Engine-sensitive cells use the inline bytes floor no allowance reaches; the 13 GoldsetParse cells read delta 0; guard-nil keeps 4; rule-load's bytes read -36.22%, passing before the allowance is consulted."
          },
          {
            "input": "TestPinnedProfile after tierConfigFile.BytesAllowanceBOp becomes map[string]*float64",
            "state": "suite-green: the test reads tiersFile.Comment only (perfgate_test.go:707-710), not the allowance map",
            "effect": "no-op",
            "evidence": "perfgate_test.go:707-710 unmarshals tierConfigFile and touches only Comment"
          },
          {
            "input": "TestLoadTierConfig and TestLoadTierConfig_BytesAllowance, whose literals name cells with no allowance entry",
            "state": "these WILL fail once a missing allowance is an error, and must be updated — the only existing tests this change edits",
            "effect": "forced",
            "evidence": "perfgate_test.go:369-391 seed tier configs from literals that carry no bytesAllowanceBOp. Naming them here rather than discovering them mid-implementation: task 1.4's 'unchanged' holds for 26 of the 28 functions, not all 28."
          },
          {
            "input": "the 13 Evaluate/Resolve tests",
            "state": "suite-green: they construct CellComparison values directly and never touch tiers.json or benchstat",
            "effect": "no-op",
            "evidence": "perfgate_test.go:18-313; perfgate.Evaluate's signature and rules are unchanged by every seam here"
          }
        ],
        "forbidden": [
          "Editing any existing test body other than the two tier-config literals named above.",
          "Updating profile-30637802780/verdict.txt. If a verdict moves, the change is wrong, not the oracle.",
          "Updating pinnedBenchEvaluatorSHA256 or pinnedBenchVMSHA256 (perfgate_test.go:674-675). No seam here touches those two files.",
          "Editing any test in this chunk. c5 made the two fixture edits; this chunk runs the package and reports."
        ],
        "seeding": [
          "go test -timeout 2m ./internal/perfgate/ with no -run filter, so the whole package runs rather than a narrowed subset."
        ],
        "budgets": [
          "28: existing test functions in internal/perfgate/perfgate_test.go.",
          "2: existing test functions this change must edit, both of them in c5, not here (TestLoadTierConfig, TestLoadTierConfig_BytesAllowance).",
          "27: pinned verdicts that must not move.",
          "0: edits to any committed corpus file."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "1.4 Run the whole package and confirm every existing test function passes. c5 already updated the two tier-config fixtures a stated allowance requires; this chunk authors no edit. The deliverable is the recorded result."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./internal/perfgate/... ./cmd/perfgate/...",
      "coder": "zpatcher"
    },
    {
      "id": "c11",
      "taskIds": [
        "6.1"
      ],
      "prev": "c10",
      "sharedPkg": "whole tree",
      "parallel": false,
      "seam": "full-floor",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "6.1",
          "file": "Makefile",
          "symbol": "test / lint",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "Verification only: `make test` (go test -timeout 2m ./...) and `make lint` (golangci-lint run). There is no race target and no vet target -- the race suite over core, plugins, runtime and `go vet ./...` are raw commands, as ci.yml runs them (.github/workflows/ci.yml:21-22)."
        }
      ],
      "contract": {
        "states": [
          "floor-status: pass or fail per command, in order"
        ],
        "transitions": [
          {
            "input": "each command in fullFloor, run in order at the repository root",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "task 6.1; make test is Makefile:14-15, make lint is Makefile:19-20"
          }
        ],
        "forbidden": [
          "Reporting a green floor from a narrowed -run.",
          "Dropping -timeout from any run, or running the race suite without a wall-clock limit.",
          "Substituting go build ./... for make test — go build never compiles _test.go, so a non-compiling test file passes it.",
          "Judging any latency figure from a local run."
        ],
        "seeding": [
          "Run from /home/zhuk/Projects/own/go-lispico with no environment overrides; GOTESTFLAGS stays at the Makefile default."
        ],
        "budgets": [
          "-timeout 2m for the non-race suite (Makefile:3); -timeout 10m for the race suite."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "6.1 Run the floor in order and record each command's exit status."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
      "coder": "coder"
    },
    {
      "id": "c12",
      "taskIds": [
        "6.2"
      ],
      "prev": "c11",
      "sharedPkg": "whole tree",
      "parallel": false,
      "seam": "hosted-dispatch-preflight",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "6.2",
          "file": ".github/workflows/release.yml",
          "symbol": "workflow_dispatch",
          "anchor": "  workflow_dispatch: {}",
          "change": "Verification only: dispatch this workflow against the candidate tree and confirm the 'Evaluate performance gate' step's `mode=` output reads non-regression. Requires a pushed ref and gh auth; the dispatch path cannot be exercised locally."
        }
      ],
      "contract": {
        "states": [
          "dispatch-mode-recorded: the dispatched run's `Determine gate mode` step output names non-regression and the baseline tag it resolved",
          "no-asset-published: the dispatched run's `Store VM baseline on the authorized release` step is skipped"
        ],
        "transitions": [
          {
            "input": "gh workflow run against the release-candidate tree, once the repository holds a v0.13.0 baseline",
            "state": "dispatch-mode-recorded: mode=non-regression, baseline_tag=<the newest release carrying bench-vm.txt>",
            "effect": "forced",
            "evidence": "the spec's 'Dispatch and release agree on the rule'; today the step is skipped entirely on a dispatch (release.yml:92) and the run falls through to first-authorization at :150"
          },
          {
            "input": "the same run's step list",
            "state": "no-asset-published: the upload step reports skipped",
            "effect": "no-op",
            "evidence": "release.yml:211, unchanged by this work"
          },
          {
            "input": "the same run's latency verdicts, if the hosted runner's CPU differs from the stored baseline's",
            "state": "every latency cell INCONCLUSIVE naming both identities; bytes and allocs still decided",
            "effect": "forced",
            "evidence": "this is the end-to-end confirmation of cross-runner-verdicts, and the only place it can be observed against real hardware variation"
          }
        ],
        "forbidden": [
          "Substituting a local run for the dispatch and reporting it as task 6.2.",
          "Dispatching against a branch that is not the release-candidate tree — `gh workflow run --ref` resolves against the remote, not the local worktree.",
          "Reading a first-authorization result as acceptable because 'no baseline was found' without checking whether enumeration failed."
        ],
        "seeding": [
          "gh workflow run on the Release consumer gate workflow, --ref pointing at the pushed release-candidate branch; then read the run's `Determine gate mode` step output and the step list.",
          "Never: a local `act` or shell reproduction — the defect being fixed is about the hosted API's failure modes."
        ],
        "budgets": [
          "1: dispatch run required.",
          "0: release assets it may publish.",
          "2: facts recorded from it — the resolved mode line and the skipped-upload step."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "6.2 SEQUENCING: this chunk runs AFTER the change is merged and pushed. A hosted Actions dispatch cannot be triggered from a pre-merge isolated worktree. It is not blocked and it is not done until the run exists — record it as outstanding at merge time.",
        "6.2 Dispatch the gate against the release-candidate tree; record the run URL, the resolved mode line and the skipped upload step.",
        "6.2 Record the run URL, the resolved mode line, and that the `Store VM baseline on the authorized release` step reports skipped. The verify above asserts the mode line; the other two go in the report.",
        "6.2 Do not assert the mode by grepping the whole run log: GitHub Actions reprints each `run:` script body into the log, and release.yml:106 contains the literal `mode=non-regression` today, so a whole-log grep passes on script text. Read the resolve-mode step's own output or its stdout line, and record the resolved mode and the skipped upload step in the report."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "gh run view \"$(gh run list --workflow 'Release consumer gate' --event workflow_dispatch --limit 1 --json databaseId --jq '.[0].databaseId')\" --json jobs --jq '.jobs[].steps[] | select(.name==\"Determine gate mode\") | .conclusion' | grep -qx success",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "runner-identity",
      "tasks": [
        "2.1"
      ],
      "summary": "Land RunnerIdentity and ReadRunnerIdentity in internal/perfgate, declared and exercised but not yet wired into any verdict. Reads the goos/goarch/cpu preamble out of a raw bench-*.txt, normalises it, and refuses a file whose repeated preambles disagree. Nothing in cmd/perfgate consults it in this seam — cross-runner-verdicts does that, so the member lands before the behaviour that uses it.",
      "contract": {
        "states": [
          "identity-known: RunnerIdentity with all three fields non-empty; Known() true",
          "identity-unknown: CPU empty; Known() false; String() renders the literal `unknown` in that position",
          "identity-inconsistent: the file's repeated preambles disagree; ReadRunnerIdentity returns ErrInconsistentPreamble and a zero RunnerIdentity"
        ],
        "transitions": [
          {
            "input": "internal/perfgate/testdata/profile-30637802780/bench-vm.txt",
            "state": "identity-known: {GOOS: \"linux\", GOARCH: \"amd64\", CPU: \"AMD EPYC 7763 64-Core Processor\"}; String() == \"linux/amd64/AMD EPYC 7763 64-Core Processor\"",
            "effect": "set",
            "evidence": "the file's `cpu: AMD EPYC 7763 64-Core Processor` line, repeated 10 times, with 16 trailing spaces that TrimSpace removes"
          },
          {
            "input": "internal/perfgate/testdata/profile-30614184386/bench-vm.txt",
            "state": "identity-known: CPU == \"INTEL(R) XEON(R) PLATINUM 8573C\"; String() == \"linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C\"",
            "effect": "set",
            "evidence": "that file's cpu: line. This is the repo's own cross-runner counterexample and is what cross-runner-verdicts seeds from."
          },
          {
            "input": "a reader over the four-line preamble with the cpu: line removed",
            "state": "identity-unknown: {GOOS: \"linux\", GOARCH: \"amd64\", CPU: \"\"}; Known() false; String() == \"linux/amd64/unknown\"",
            "effect": "clear",
            "evidence": "task 2.1's 'reports its identity as unknown'. Under D1 this is the input that reaches it, not a pre-change baseline."
          },
          {
            "input": "an empty reader",
            "state": "identity-unknown: zero RunnerIdentity; String() == \"unknown/unknown/unknown\"; no error",
            "effect": "clear",
            "evidence": "a file with no preamble carries no identity; refusing it here would make the parser the gate's failure point rather than the comparison"
          },
          {
            "input": "a reader whose first preamble says `cpu: A` and whose second says `cpu: B`",
            "state": "identity-inconsistent: ErrInconsistentPreamble, zero RunnerIdentity",
            "effect": "forced",
            "evidence": "the workflow appends one `go test` run per sample (release.yml:131-134), so a single file recording two CPUs means the ten samples did not all run on one machine and no single identity describes it"
          },
          {
            "input": "two preambles that differ only in trailing whitespace on the cpu: line",
            "state": "identity-known, no error; the two normalise to the same string",
            "effect": "no-op",
            "evidence": "the AMD corpora carry 16 trailing spaces; treating that as an inconsistency would reject the repo's own committed files"
          },
          {
            "input": "any bench-*.txt already in internal/perfgate/testdata",
            "state": "identity-known; no file in the repo reaches identity-unknown or identity-inconsistent",
            "effect": "no-op",
            "evidence": "all six checked-in bench files carry 10 cpu: lines each, verified"
          }
        ],
        "forbidden": [
          "Reading the identity from benchstat's output. benchstat drops the cpu: line entirely under -ignore and reports only one group's preamble otherwise; the raw file is the only faithful source.",
          "Including `pkg:` in the identity. It names the benchmarked package, not the machine, and would make an unrelated package rename read as a hardware change.",
          "Writing to, rewriting, or copying any bench-*.txt. The identity is read; nothing is normalised on disk.",
          "Treating a missing cpu: line as an error, or a present one as optional to compare.",
          "Any use of ReadRunnerIdentity in cmd/perfgate in this seam — it lands unused on purpose."
        ],
        "seeding": [
          "identity-known: os.Open on internal/perfgate/testdata/profile-30637802780/bench-vm.txt and internal/perfgate/testdata/profile-30614184386/bench-vm.txt. Committed corpora, not constructed.",
          "identity-unknown and identity-inconsistent: strings.NewReader over a hand-written 3- or 8-line preamble in the test file. These two states are unreachable from any committed corpus, so a literal is the only legal path.",
          "Never: constructing a RunnerIdentity by hand and asserting String() alone — at least one arm must read a real corpus file so the parser is what is under test."
        ],
        "budgets": [
          "10: preamble repetitions per checked-in bench file; ReadRunnerIdentity must read all of them, not just the first.",
          "3: identity keys read (goos, goarch, cpu). pkg is skipped.",
          "2: distinct CPU identities across the three committed profiles (AMD EPYC 7763 64-Core Processor; INTEL(R) XEON(R) PLATINUM 8573C).",
          "0: files in the repo that reach identity-unknown."
        ]
      }
    },
    {
      "id": "unpaired-comparison-refusal",
      "tasks": [
        "2.3"
      ],
      "summary": "Turn benchstat's silent single-group degradation into a stated refusal. benchstat exits 0 and emits two single-group tables, so the failure has no path today except perfgate's own 'data row too short' - the bare failure the spec is telling the gate to stop producing. parseBlock gains a positive header-shape check that distinguishes single-group from malformed CSV; cmd/perfgate states the reason from the two runner identities and exits 3. Also introduces exit code 3 for gate-configuration failures, separating them from the exit-2 needs-rerun signal.",
      "contract": {
        "states": [
          "paired: the metric header has 7 fields ending `vs base`,`P`; cells populate as today",
          "single-group: the metric header has 3 fields ending `CI`; ErrUnpairedComparison; no cells returned",
          "malformed: the header matches neither shape, or csv.ReadAll rejects the block; the existing generic error, which now means only what it says",
          "exit-taxonomy: 0 all pass, 1 any fail, 2 any cell needs a rerun, 3 the gate could not be configured or the evidence could not be paired",
          "post-c3 reachability: once c3 lands, a differing cpu: never reaches this path — perfgate compares identities first and skips benchstat. The remaining triggers are a differing goos:, goarch: or pkg:, and a future benchstat change.",
          "identities-match precondition: after c3, this path is reachable only when the two runner identities agree — a differing cpu: is handled by the identity comparison before benchstat is consulted"
        ],
        "transitions": [
          {
            "input": "internal/perfgate/testdata/profile-30637802780/benchstat.csv",
            "state": "paired: 27 cells, no error",
            "effect": "no-op",
            "evidence": "measured: all three blocks read `,sec/op,CI,sec/op,CI,vs base,P`, `,B/op,...`, `,allocs/op,...`, 7 fields each, and records[0] names two distinct files. TestPinnedProfile parses this file (perfgate_test.go:697-700) and must keep passing."
          },
          {
            "input": "a benchstat -format csv run over two files whose cpu: lines differ",
            "state": "single-group: ErrUnpairedComparison naming the metric and the column count",
            "effect": "forced",
            "evidence": "measured with the pinned benchstat over the committed profile-30637802780 pair with one cpu: line rewritten: benchstat EXITS 0 and emits six blocks - two single-group tables of three metric blocks each - whose metric headers read `,sec/op,CI` (3 fields) and whose data rows read `Goldset/counter-closure-2,8.507e-06,1%` (3 fields)."
          },
          {
            "input": "the same input, under today's code",
            "state": "the generic `perfgate: benchstat csv data row too short` from parse.go:73-75",
            "effect": "clear",
            "evidence": "this is the symptom reported for the v0.13.0 cut, and it is indistinguishable from a malformed CSV. The header check makes the data-row check unreachable for this input."
          },
          {
            "input": "a CSV whose header matches neither shape, or whose rows are ragged",
            "state": "malformed: the existing generic error",
            "effect": "no-op",
            "evidence": "parse.go:58-75; csv.ReadAll rejects ragged rows before the header is consulted"
          },
          {
            "input": "the same differing-cpu pair with -ignore=cpu",
            "state": "never produced; no code path constructs this argv",
            "effect": "forbidden",
            "evidence": "measured: -ignore=cpu yields one paired table AND removes the cpu: line from the output, i.e. it obtains the comparison by disregarding the configuration line the spec forbids disregarding."
          },
          {
            "input": "benchstat's stderr on the measured single-group run",
            "state": "carried into the message for the operator, but never used as the reason",
            "effect": "no-op",
            "evidence": "measured: stderr read only `B65: summaries must be >0 to compute geomean`, which is about the geomean and not about pairing. cmd/perfgate/main.go:158-171 discards stderr whenever benchstat exits 0, so today even that is thrown away."
          },
          {
            "input": "LoadTierConfig returns a configuration error",
            "state": "exit 3, not exit 2",
            "effect": "set",
            "evidence": "cmd/perfgate/main.go:55-60 maps every error to exit 2 today, and release.yml:169 treats exit 2 as 'rerun at doubled benchtime', so a configuration error costs a pointless doubled-benchtime rerun before failing."
          },
          {
            "input": "a cell that is genuinely inconclusive",
            "state": "exit 2, unchanged; release.yml's rerun step still fires",
            "effect": "no-op",
            "evidence": "release.yml:168-196; the rerun contract is not being changed"
          }
        ],
        "forbidden": [
          "Detecting the single-group case from the data rows. The header row is the positive discriminator; a short data row is also what a malformed CSV produces.",
          "Passing -ignore, -filter, -col, -row or -table to benchstat. The argv stays exactly `-format csv <old> <new>`.",
          "Reporting benchstat's stderr AS the pairing reason - on the measured run it says nothing about pairing.",
          "Rewriting, copying, filtering or truncating either input file before handing it to benchstat.",
          "Retrying benchstat with different arguments after any failure.",
          "Removing goos:/goarch:/pkg:/cpu: from benchstatPreamblePrefixes - that filter drops those lines from benchstat's OUTPUT so the CSV parses; it does not touch the inputs and is not what the spec forbids.",
          "Reusing exit 2 for a configuration failure, or exit 3 for a needs-rerun signal.",
          "Asserting the two runner identities in this chunk's stderr line. Reading both raw files' identities in cmd/perfgate is c3's codeTask; c2 asserts the single-group reason and exit 3 only, and c3 adds the identities to the message.",
          "Treating this chunk's exit-3 abort as the handler for a cross-runner pair. It is not: c3 routes those away from benchstat entirely, and an abort here would foreclose the bytes and allocs verdicts spec scenario 1 requires.",
          "Seeding this chunk's cmd-level tests with two files whose runner identities differ. That input belongs to c3 and is routed away from benchstat."
        ],
        "seeding": [
          "paired: internal/perfgate/testdata/profile-30637802780/benchstat.csv, already committed.",
          "single-group: SYNTHESIZED, and necessarily so - no single-group benchstat CSV is committed anywhere in the repo, and the two CPU strings the proposal quotes for the failing v0.13.0 cut (`Intel(R) Xeon(R) Platinum 8370C`, `AMD EPYC 9V74`) appear in no committed file either. Produce it once by copying the two profile-30637802780 bench files, rewriting the cpu: line in the copy of bench-evaluator.txt to the CPU string profile-30614184386 actually records (`INTEL(R) XEON(R) PLATINUM 8573C`, so the fixture quotes a machine this project has really used), running the pinned benchstat over the pair, and committing the output verbatim as internal/perfgate/testdata/unpaired-single-group.csv with a README line recording exactly that recipe.",
          "Committed rather than generated in the test: generating it would make the unit test fetch benchstat over the network on a cold module cache.",
          "exit-taxonomy: a new cmd/perfgate/main_test.go calling run(stdout, stderr, args) directly and asserting the returned int. cmd/perfgate has no test file today.",
          "Never: asserting the absence of -ignore by grepping source; assert the error path end to end from the committed fixture instead.",
          "TestRun_UnpairedComparisonExitsThree and TestRun_ConfigErrorExitsThree: swap the package-level runBenchstat for one returning internal/perfgate/testdata/unpaired-single-group.csv, restoring it with t.Cleanup. No network, no benchstat invocation.",
          "The unpaired CSV is committed rather than generated: generating it needs benchstat, which is the dependency the seam exists to avoid.",
          "TestRun_UnpairedComparisonExitsThree seeds -old and -candidate with the two SAME-identity committed files: internal/perfgate/testdata/profile-30637802780/bench-evaluator.txt and .../bench-vm.txt (both record `cpu: AMD EPYC 7763 64-Core Processor`). This is load-bearing after c3: the identity-first ordering routes a differing-identity pair away from benchstat entirely, so seeding differing files would make ErrUnpairedComparison unreachable and turn this sealed test red at c3. The unpaired shape comes from the injected runBenchstat returning the committed unpaired CSV, never from the input files' identities.",
          "Never seed empty or synthetic temp paths: after c3, ReadBenchmarkMetrics runs over both raw paths and would change the exit path.",
          "Both cmd-level tests pass `-tiers ../../internal/perfgate/tiers.json` explicitly. The flag's default is `internal/perfgate/tiers.json` (cmd/perfgate/main.go:29), which under `go test ./cmd/perfgate/...` resolves against the package directory and does not exist — os.Open fails at main.go:81-83 and returns exit 3 through main.go:55-60. Without the explicit path TestRun_ConfigErrorExitsThree passes while seeding nothing, and TestRun_UnpairedComparisonExitsThree gets the right exit code for the wrong reason."
        ],
        "budgets": [
          "7: fields in a paired metric header, ending `vs base` and `P`.",
          "3: fields in a single-group metric header, ending `CI`. Both measured.",
          "6: blocks in the single-group output - two configurations times three metrics.",
          "0: benchstat exit code on the single-group run, which is why nothing upstream notices.",
          "4 exit codes: 0, 1, 2, 3.",
          "0: retries after a pairing refusal."
        ]
      }
    },
    {
      "id": "cross-runner-verdicts",
      "tasks": [
        "1.1",
        "2.2"
      ],
      "summary": "Wire the identity comparison into the gate and move the bytes and allocs axes off benchstat onto a direct read of the raw bench files, so they survive a differing runner. When the two identities differ every latency cell is INCONCLUSIVE naming both; bytes and allocs are still decided.",
      "contract": {
        "states": [
          "identities-match: both raw files report the same RunnerIdentity; latency judged as today",
          "identities-differ: the two RunnerIdentity values differ; every cell's latency verdict is INCONCLUSIVE and the report line names both",
          "bytes-source: the B/op figure a cell is judged on comes from the median of the raw per-sample B/op fields, never from benchstat",
          "allocs-source: likewise for allocs/op",
          "bytes-verdict / allocs-verdict: decided in both identity states",
          "delta-derived: MetricResult.DeltaPct and .Significant are computed from the two medians, never left at their zero values",
          "zero-baseline: the baseline median for an axis is 0; DeltaPct is 0 when the candidate is also 0 and +Inf when it is positive",
          "identity-first: the identity comparison precedes any benchstat invocation, so a differing cpu: never reaches the unpaired path"
        ],
        "transitions": [
          {
            "input": "profile-30637802780/bench-evaluator.txt against profile-30637802780/bench-vm.txt",
            "state": "identities-match; latency judged by benchstat as today; TestPinnedProfile's 27 committed verdicts unchanged",
            "effect": "no-op",
            "evidence": "both files read `cpu: AMD EPYC 7763 64-Core Processor`, verified"
          },
          {
            "input": "profile-30614184386/bench-vm.txt as baseline against profile-30637802780/bench-vm.txt as candidate, ModeNonRegression",
            "state": "identities-differ: linux/amd64/INTEL(R) XEON(R) PLATINUM 8573C against linux/amd64/AMD EPYC 7763 64-Core Processor; every latency cell INCONCLUSIVE",
            "effect": "forced",
            "evidence": "the spec's first scenario. Both corpora are committed; this is a real cross-runner VM-vs-VM pair, which is exactly the post-authorization shape that broke the v0.13.0 cut."
          },
          {
            "input": "that same cross-runner pair, bytes axis",
            "state": "bytes-verdict decided, not skipped: each cell's Old and New come from the raw medians and nonIncreasing runs against the cell's stated allowance",
            "effect": "set",
            "evidence": "the spec's 'Allocation counts and allocated bytes SHALL stay enforced across differing runners'. Measured feasibility: raw medians reproduce benchstat's Old and New for all 27 cells on both axes in profile-30637802780 with zero mismatches, so this substitution changes no number where a comparison was previously possible."
          },
          {
            "input": "that same cross-runner pair, allocs axis",
            "state": "allocs-verdict decided against a zero allowance",
            "effect": "set",
            "evidence": "same measurement; allocs matched benchstat exactly on all 27 cells"
          },
          {
            "input": "a cell present in the candidate but absent from the baseline (GoldsetCall/call-boundary is in profile-30637802780 and not in profile-30614184386 or profile-30630796967)",
            "state": "the cell has no baseline figure; its bytes and allocs are reported INCONCLUSIVE naming the missing baseline cell, not passed and not failed",
            "effect": "forced",
            "evidence": "verified: profile-30614184386 holds 26 cells, profile-30637802780 holds 27. A missing baseline figure is absence of evidence; judging it against zero would fail every newly added cell."
          },
          {
            "input": "identities-differ, ModeFirstAuthorization",
            "state": "the same rule applies: latency INCONCLUSIVE, bytes and allocs decided",
            "effect": "no-op",
            "evidence": "the spec conditions on the identities differing, not on the mode. First authorization pairs two arms of one run, so in practice they never differ — the rule is stated for both modes so the code has no mode-dependent branch."
          },
          {
            "input": "either file reports an unknown identity (no cpu: line)",
            "state": "identities-differ is NOT concluded from an unknown; the comparison is reported INCONCLUSIVE naming `unknown` as one side",
            "effect": "forced",
            "evidence": "an unknown identity is not evidence that the runners matched, and equally not evidence that they differed. Treating unknown as a match would let a preamble-less baseline silently license a latency conclusion."
          },
          {
            "input": "an INCONCLUSIVE latency cell from the identity rule, reaching the -rerun invocation",
            "state": "Resolve() collapses it exactly as it collapses any other inconclusive cell: PASS except engine-sensitive under first authorization",
            "effect": "no-op",
            "evidence": "perfgate.go:176-181. A cross-runner latency cell is a non-regression claim that was not refuted, so PASS is the burden-of-proof answer ADR 0008:14 already gives. The release still fails on any bytes or allocs regression, which is what makes this safe."
          },
          {
            "input": "a cell whose baseline and candidate medians differ",
            "state": "delta-derived: DeltaPct = (New-Old)/Old*100, Significant true",
            "effect": "set",
            "evidence": "perfgate.go:297-303 — nonIncreasing returns PASS on DeltaPct <= 0 before reading New-Old, so an underived DeltaPct passes every cell"
          },
          {
            "input": "GoldsetCall/call-boundary, 0 B/op in both files",
            "state": "zero-baseline: DeltaPct 0",
            "effect": "no-op",
            "evidence": "internal/perfgate/testdata/profile-30637802780/bench-vm.txt — the cell reads 0 B/op and 0 allocs/op"
          },
          {
            "input": "GoldsetCall/call-boundary, 0 B/op baseline against a positive candidate",
            "state": "zero-baseline: DeltaPct +Inf",
            "effect": "forced",
            "evidence": "a first allocation on a zero-byte cell must fail, not divide by zero"
          },
          {
            "input": "a cell in the candidate with no baseline row",
            "state": "reported INCONCLUSIVE at the report layer, naming the missing baseline cell",
            "effect": "forced",
            "evidence": "cmd/perfgate/main.go:133-137 owns the verdict line; Evaluate is unchanged"
          },
          {
            "input": "profile-30614184386/bench-vm.txt against profile-30637802780/bench-vm.txt (cpu: differs)",
            "state": "identity-first: benchstat is not invoked for pairing; the cell loop runs to completion",
            "effect": "forced",
            "evidence": "D2 ordering; without it cmd/perfgate/main.go:95-98 returns (0, err) and exits before the cell loop at :109"
          }
        ],
        "forbidden": [
          "Concluding a latency PASS or FAIL from a comparison whose two identities differ.",
          "Skipping, defaulting or zeroing the bytes or allocs verdict when identities differ.",
          "Taking bytes or allocs from benchstat once the raw reader exists — one source per axis, or the two can disagree silently.",
          "Judging a cell whose baseline figure is missing against an Old of 0.",
          "Treating an unknown identity as equal to any other identity, including another unknown.",
          "Changing perfgate.Evaluate's signature or its per-tier rules. The identity rule lives above it.",
          "Leaving MetricResult.DeltaPct or .Significant at their zero values when populating Bytes or Allocs from raw medians.",
          "Computing (New-Old)/Old without a guard on Old == 0.",
          "Adding a state to Verdict or changing Evaluate's signature to express the missing-baseline case.",
          "Invoking benchstat for a paired comparison when the two runner identities differ. That is the path that aborts before the cell loop and silently forecloses the bytes and allocs verdicts the spec requires.",
          "Leaving the precedence between exit 3, exit 1 and exit 2 to fall out of control flow.",
          "Assuming the two input files carry the same number of samples per cell.",
          "Authoring a test in internal/perfgate against logic that lives in cmd/perfgate. internal/perfgate cannot import package main, so such a test asserts against a reimplementation and goes green while the shipped binary is untested on this seam's central requirement.",
          "Moving the identity comparison or the report-layer missing-baseline line into internal/perfgate to make a misplaced test compile. If that relocation is wanted it is a design change, not a test fix.",
          "Exposing only the median from SampleMetrics. c4 needs the per-sample figures and must not reimplement the parser to get them."
        ],
        "seeding": [
          "identities-match: the profile-30637802780 pair, committed.",
          "identities-differ: profile-30614184386/bench-vm.txt against profile-30637802780/bench-vm.txt, both committed. No synthetic file.",
          "bytes-source / allocs-source: ReadBenchmarkMetrics over the committed raw files; the test cross-checks its output against the committed benchstat.csv Old/New columns for the matching-identity profile, which is the measurement that licenses the substitution.",
          "missing-baseline-cell: GoldsetCall/call-boundary, present in profile-30637802780 and absent from profile-30614184386. Already committed; do not construct one.",
          "unknown identity: strings.NewReader over a preamble with the cpu: line removed, as in the runner-identity seam.",
          "Never: hand-constructing a CellComparison to reach identities-differ. The state must come from two real files, because the whole defect was that the identity was never read.",
          "zero-baseline: GoldsetCall/call-boundary in the committed profile-30637802780 corpus, which genuinely reads 0 B/op — not a constructed literal.",
          "cmd-level tests: cmd/perfgate/main_test.go already exists at c3 because c2 creates it. Raw corpora are reached by relative path from cmd/perfgate; the identity-first path never calls benchstat, so those tests need no seam substitution."
        ],
        "budgets": [
          "27: cells judged when both files are profile-30637802780; 26 when the baseline is profile-30614184386.",
          "1: cells whose bytes and allocs are inconclusive for want of a baseline figure on that cross-runner pair (GoldsetCall/call-boundary).",
          "10: samples per cell per file, from which the median is taken.",
          "0: numbers that change on the matching-identity path — verified against all 27 cells on both axes.",
          "2: identity strings named in an INCONCLUSIVE latency report line.",
          "10: samples per cell per file in every committed corpus — an EVEN count, so \"the median\" is a choice (lower middle, upper middle, or the mean of the two). The convention must be the one benchstat uses; the coder verifies it against benchstat's own definition rather than assuming, because 14 of the 27 cells in profile-30637802780 have zero spread and cannot discriminate between conventions.",
          "Sample counts may differ between the -old and -candidate files: the rerun step re-measures at doubled benchtime (release.yml:168-178) while a stored baseline keeps its original count. The reader must not assume equal counts."
        ]
      }
    },
    {
      "id": "bytes-allowance-values",
      "tasks": [
        "3.1"
      ],
      "summary": "Give all 27 tiers.json cells a bytesAllowanceBOp entry derived from that cell's observed within-run B/op spread, and add the test that recomputes every number from the committed corpora so no allowance can outgrow its evidence. Lands BEFORE bytes-allowance-config, which turns a missing entry into an error: reversing the order breaks every gate invocation and TestPinnedProfile with it.",
      "contract": {
        "states": [
          "allowance-stated: every one of the 27 cells has a bytesAllowanceBOp entry",
          "allowance-justified: for every cell except Goldset/guard-nil, entry <= max observed within-run spread across the committed profiles",
          "allowance-zero: the 13 GoldsetParse/* cells and GoldsetCall/call-boundary state 0, which is the exact non-increasing bound"
        ],
        "transitions": [
          {
            "input": "the 13 GoldsetParse/* cells",
            "state": "allowance-zero: stated 0; bit-identical B/op across all ten samples in both profiles",
            "effect": "set",
            "evidence": "supplied spread measurement: every GoldsetParse cell reads 0 spread in profile-30630796967 and 0 in profile-30637802780. A stated 0 preserves the invariant tiers.json's comment and ADR 0008:60-62 record."
          },
          {
            "input": "GoldsetCall/call-boundary",
            "state": "allowance-zero: stated 0; spread 0 in profile-30637802780 and absent from profile-30630796967",
            "effect": "set",
            "evidence": "supplied measurement (0 B/op, bit-identical); the cell's absence from the older profile is verified and means its evidence base is one profile, not two"
          },
          {
            "input": "Goldset/queue-promote",
            "state": "allowance 8, the largest in the table; spread 7 in profile-30630796967 and 8 in profile-30637802780 over a ~16.5 KB figure",
            "effect": "set",
            "evidence": "supplied measurement (16582..16593). 8 equals one Go size class, so this allowance alone could absorb one 8-byte allocation on the bytes axis; the allocs axis keeps a zero allowance and would catch it."
          },
          {
            "input": "Goldset/rule-load",
            "state": "allowance 6; spread 3 then 6 (5840..5848)",
            "effect": "set",
            "evidence": "supplied measurement. This is the startup cell, so its allowance reaches nonIncreasing through evaluateStartup (perfgate.go:263)."
          },
          {
            "input": "Goldset/guard-nil",
            "state": "allowance 4, unchanged, exempt from the spread rule",
            "effect": "no-op",
            "evidence": "its within-run spread is 0; the 4 covers a reproducible between-engine offset of +1 B/op licensed by ADR 0008:50-74 and three named hosted runs. Re-deriving it is out of scope."
          },
          {
            "input": "the remaining nine Goldset/* cells",
            "state": "allowance = max observed spread: counter-closure 1, kw-lookup 1, loop-sum 2, merge-config 3, pipeline 2, registry-fold 2, route-decision 1, safe-parse 2, text-render 1",
            "effect": "set",
            "evidence": "supplied per-cell spread measurement across profile-30630796967 and profile-30637802780"
          },
          {
            "input": "a future edit raising any allowance above the spread the committed corpora record",
            "state": "TestBytesAllowancesAreJustifiedBySpread fails, naming the cell, the stated value and the measured spread",
            "effect": "forced",
            "evidence": "the spec's 'SHALL NOT be widened to admit a measured regression'. Bounding the allowance by within-run spread makes it structurally unable to admit a regression: a regression shifts the median while leaving the spread where it was."
          },
          {
            "input": "the pinned profile re-evaluated with all 27 allowances in place",
            "state": "every committed verdict in profile-30637802780/verdict.txt is unchanged",
            "effect": "no-op",
            "evidence": "verified: engine-sensitive cells take the inline 20% bytes floor no allowance reaches (perfgate.go:293-296); the allowance-reading cells under first authorization are the 13 GoldsetParse (delta 0), guard-nil (unchanged) and rule-load (bytes -36.22%, so nonIncreasing passes at DeltaPct <= 0 before the allowance is consulted)."
          }
        ],
        "forbidden": [
          "A size-proportional or percentage allowance. The spec ties the number to observed spread on that cell.",
          "Rounding an allowance up to a size class, or to any value above the measured spread.",
          "Giving Goldset/guard-nil a new derivation, or removing its 4.",
          "Recording a number without the profile ids it came from.",
          "Raising an allowance in the same change as a bytes regression it would admit.",
          "Deriving any number from a developer-box run. tiers.json's comment already states a developer-box figure licenses nothing, and only bytes and allocs are locally exact in any case.",
          "Deriving any number from profile-30630796967: its committed files are a 400ms measurement and would understate the spread the gate sees at 200ms.",
          "Deriving a number from a difference taken between two profiles. They measure different trees at different benchtimes; such a figure blends code change, benchtime and hardware and presents it as noise.",
          "Leaving tiers.json:2's \"no allowance\" sentence in place. It is false the moment every cell carries an entry, and c9 cannot fix it — this chunk owns that file."
        ],
        "seeding": [
          "profile-30637802780/bench-vm.txt alone is the evidence base: ten samples at the gate's own BENCHTIME of 200ms (release.yml:66). For each cell, span = max(B/op) - min(B/op) over those ten samples; the allowance is that span.",
          "profile-30630796967 is a directional check only. Its committed files are a 400ms rerun measurement (its README.md:4-17, 57092 iterations against 28188), and B/op is a total over an iteration count, so twice the iterations halves the rounding granularity and every one of its spreads is <= its 200ms counterpart.",
          "profile-30614184386 is the cross-runner fixture, not an allowance source: it records a third CPU.",
          "Spread is recomputed through c3's SampleMetrics per-sample accessor, not by a second raw-file parser in the test."
        ],
        "budgets": [
          "27: cells in tiers.json — 13 Goldset/*, 1 GoldsetCall/call-boundary, 13 GoldsetParse/*.",
          "13: entries with a non-zero value, all of them Goldset/* cells (guard-nil among them, keeping its existing 4).",
          "14: entries stating 0 — the 13 GoldsetParse/* cells plus GoldsetCall/call-boundary, every one bit-identical across all ten samples.",
          "1: profiles in the evidence base.",
          "8: the largest allowance in the table (Goldset/queue-promote), and one Go size class. Safe only because the allocs axis keeps a zero allowance.",
          "Expected table, all 27 entries: Goldset/counter-closure 1, Goldset/guard-nil 4 (the between-engine exception, unchanged), Goldset/kw-lookup 1, Goldset/loop-sum 2, Goldset/merge-config 3, Goldset/pipeline 2, Goldset/queue-promote 8, Goldset/registry-fold 2, Goldset/route-decision 1, Goldset/rule-load 6, Goldset/safe-parse 2, Goldset/text-render 1, Goldset/twice-macro 2 — thirteen non-zero, every one a Goldset/* cell. Then GoldsetCall/call-boundary 0 and each of the thirteen GoldsetParse/* cells 0 — fourteen stated zeros. Twenty-seven entries."
        ]
      }
    },
    {
      "id": "bytes-allowance-config",
      "tasks": [
        "1.2",
        "3.2"
      ],
      "summary": "Make an absent bytesAllowanceBOp entry a configuration error instead of an implicit zero. Requires changing tierConfigFile.BytesAllowanceBOp to map[string]*float64, because a stated 0 and an absent key are indistinguishable after decoding into map[string]float64 today. Lands AFTER bytes-allowance-values.",
      "contract": {
        "states": [
          "allowance-present-nonzero: the cell states a positive allowance; nonIncreasing gets it",
          "allowance-present-zero: the cell states 0; nonIncreasing gets 0, the exact non-increasing bound",
          "allowance-absent: the cell is in `cells` and not in `bytesAllowanceBOp`; LoadTierConfig fails",
          "allocs-allowance: always 0, never configurable",
          "TestEvaluate_BytesAllowance (perfgate_test.go:590-650) is NOT affected: it builds CellComparison literals and calls Evaluate directly, never constructing a tier config. Its \"unlisted cell gets no allowance\" subtest name is stale wording in a passing test, left alone."
        ],
        "transitions": [
          {
            "input": "the shipped tiers.json after bytes-allowance-values",
            "state": "all 27 cells present; LoadTierConfig succeeds",
            "effect": "no-op",
            "evidence": "TestPinnedProfile loads the real tiers.json (perfgate_test.go:702-705) and must keep passing"
          },
          {
            "input": "a tier config naming a cell in `cells` with no `bytesAllowanceBOp` entry",
            "state": "allowance-absent: error satisfying errors.Is(err, ErrMissingBytesAllowance), message naming the cell",
            "effect": "forced",
            "evidence": "the spec's 'the gate SHALL fail with a missing-config error when a cell it is asked to judge has none, rather than reading the absence as an allowance of zero'"
          },
          {
            "input": "a tier config stating `\"GoldsetParse/kw-lookup\": 0`",
            "state": "allowance-present-zero: loads cleanly; CellTier{BytesAllowanceBOp: 0, BytesAllowanceStated: true}",
            "effect": "set",
            "evidence": "the reading taken in the design's D4: an explicit 0 is a stated allowance enforcing the exact non-increasing bound; an absent entry is the error — a stated 0 is a recorded decision, not silence, and enforces the same exact bound"
          },
          {
            "input": "a tier config whose bytesAllowanceBOp names a cell absent from `cells`",
            "state": "the existing error is unchanged: `perfgate: bytesAllowanceBOp names cell %q, absent from cells`",
            "effect": "no-op",
            "evidence": "parse.go:216-220, already covered by TestLoadTierConfig_BytesAllowance_UnknownCell (perfgate_test.go:406)"
          },
          {
            "input": "a negative allowance, e.g. -4",
            "state": "rejected with a stated error naming the cell",
            "effect": "forced",
            "evidence": "nonIncreasing compares `m.New-m.Old <= allowanceBOp` (perfgate.go:301); a negative allowance would fail a cell whose bytes did not increase, which no requirement asks for. Nothing rejects it today."
          },
          {
            "input": "any cell's allocs axis",
            "state": "allocs-allowance stays 0 and stays unreachable from tiers.json",
            "effect": "no-op",
            "evidence": "perfgate.go:194, :221, :242, :266 all pass a literal 0; the spec's 'Allocation counts are exact in the same output and SHALL keep a zero allowance'"
          }
        ],
        "forbidden": [
          "Defaulting an absent entry to 0 anywhere, including in cmd/perfgate.",
          "Adding an allocs allowance key to tiers.json or an allowance parameter to the allocs call sites.",
          "Reporting the missing-allowance failure as a per-cell Result{Verdict: VerdictFail}. It is a configuration defect and must stop the run, not be reported as one cell's measurement.",
          "Letting a configuration error exit 2 and trigger the doubled-benchtime rerun.",
          "Changing LoadTierConfig's signature.",
          "Leaving internal/perfgate red for a later chunk to repair. The chunk that makes an absent allowance an error owns every existing fixture that absence made valid."
        ],
        "seeding": [
          "allowance-present-*: the shipped internal/perfgate/tiers.json for the happy path; strings.NewReader over a small hand-written JSON literal for the single-cell arms, matching how TestLoadTierConfig and TestLoadTierConfig_BytesAllowance already seed (perfgate_test.go:369-418).",
          "allowance-absent: a hand-written literal with one cell in `cells` and an empty `bytesAllowanceBOp`. Unreachable from the shipped file after bytes-allowance-values, so a literal is the only legal path.",
          "Never: deleting an entry from the real tiers.json inside a test."
        ],
        "budgets": [
          "27: cells that must each state an allowance.",
          "0: the allocs allowance, at all four call sites.",
          "3: the exit code a missing allowance produces.",
          "1: the number of load-time passes over the cell set — the check runs inside LoadTierConfig, not per judged cell."
        ]
      }
    },
    {
      "id": "gate-mode-resolution",
      "tasks": [
        "1.3",
        "4.1",
        "4.3"
      ],
      "summary": "Move the mode decision out of inline shell into a pure Go function plus a `resolve-mode` subcommand, and restructure the workflow step to observe absence and failure separately. Removes `if: github.event_name == 'release'` from the mode step so a dispatch resolves the same mode a release would.",
      "contract": {
        "states": [
          "baseline-found: a stored bench-vm.txt was enumerated and downloaded; ModeNonRegression",
          "baseline-absent: every enumerated release was inspected and none carries the asset; ModeFirstAuthorization",
          "enumeration-failed: listing releases or listing one release's assets failed; ModeUnknown and an error",
          "download-failed: the asset was seen in a release's asset list and the download then failed; ModeUnknown and an error"
        ],
        "transitions": [
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.12.0\"], DownloadedTag: \"v0.12.0\"}",
            "state": "baseline-found: (ModeNonRegression, BaselineFound, nil)",
            "effect": "set",
            "evidence": "release.yml:105-107 selects non-regression on exactly this observation today"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.11.0\",\"v0.12.0\"], DownloadedTag: \"\"}",
            "state": "baseline-absent: (ModeFirstAuthorization, BaselineAbsent, nil)",
            "effect": "set",
            "evidence": "the spec's 'First-authorization SHALL be selected only when the repository is known to hold no baseline'. Known, here, means every enumerated tag was inspected and none carried the asset."
          },
          {
            "input": "BaselineLookup{EnumerationOK: false}",
            "state": "enumeration-failed: (ModeUnknown, BaselineEnumerationFailed, ErrBaselineEnumerationFailed)",
            "effect": "forced",
            "evidence": "defect 4: `gh release list` is unchecked at release.yml:98, so an API error yields an empty tag list and silently selects first-authorization improvement thresholds"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: [\"v0.12.0\"], DownloadFailed: true}",
            "state": "download-failed: (ModeUnknown, BaselineDownloadFailed, ErrBaselineDownloadFailed)",
            "effect": "forced",
            "evidence": "the spec's 'enumerating or downloading the stored baseline fails for any reason other than the baseline not existing'"
          },
          {
            "input": "BaselineLookup{EnumerationOK: true, Tags: []}",
            "state": "baseline-absent: a repository with no releases at all holds no baseline; ModeFirstAuthorization",
            "effect": "set",
            "evidence": "this is the genuine first-authorization case the mode exists for"
          },
          {
            "input": "the zero BaselineLookup",
            "state": "enumeration-failed: EnumerationOK is false by zero value, so an unpopulated lookup fails closed rather than selecting first-authorization",
            "effect": "forced",
            "evidence": "the same fail-closed reasoning perfgate.go:33-38 and :44-50 apply to ModeUnknown and VerdictUnknown"
          },
          {
            "input": "a workflow_dispatch run against a tree whose repository holds a v0.13.0 baseline",
            "state": "baseline-found: the dispatch reports non-regression, the same mode the release run reaches",
            "effect": "forced",
            "evidence": "the spec's 'Dispatch and release agree on the rule'; task 4.1 and, on a hosted runner, task 6.2"
          },
          {
            "input": "RELEASE_TAG empty on a dispatch run",
            "state": "the skip line `[ \"$tag\" = \"$RELEASE_TAG\" ] && continue` matches nothing, so the newest release is considered first",
            "effect": "no-op",
            "evidence": "release.yml:99. On a dispatch there is no release being cut, so there is nothing to exclude."
          }
        ],
        "forbidden": [
          "Returning ModeFirstAuthorization together with a non-nil error, or any Mode other than ModeUnknown on a failure.",
          "Deciding the mode from github.event_name anywhere.",
          "Treating a non-zero `gh release download` as absence. Absence is decided from the asset list, never from a download exit code.",
          "Leaving `gh release list` unchecked.",
          "Publishing a baseline or uploading a release asset from the resolve-mode path — it reads only.",
          "Making resolve-mode a second binary. It is a subcommand of the existing cmd/perfgate so the workflow builds one thing.",
          "Authoring a test that invokes `perfgate resolve-mode`. The subcommand lands in c7; a CLI test here fails this chunk's own verify.",
          "Editing .github/workflows/release.yml in this chunk. The workflow half of task 4.3 is carried by c7, which restructures the same step."
        ],
        "seeding": [
          "All four states: a BaselineLookup struct literal in internal/perfgate. No network, no gh, no filesystem — the point of the seam is that the classification is pure.",
          "Never: shelling out to gh from a test, or asserting the workflow YAML's shell body from Go.",
          "This chunk is the pure-Go arm only: ResolveGateMode is exercised by constructing BaselineLookup values in internal/perfgate. The `perfgate resolve-mode` subcommand does not exist yet — c7 adds it — so no CLI-arm test may be authored here."
        ],
        "budgets": [
          "4: outcomes in the taxonomy (found, absent, enumeration-failed, download-failed).",
          "1: outcome that selects ModeFirstAuthorization.",
          "30: the release enumeration limit, unchanged from release.yml:98.",
          "3: the exit code of a resolution failure.",
          "0: release assets written by the resolve-mode path."
        ]
      }
    },
    {
      "id": "dispatch-publishes-nothing",
      "tasks": [
        "4.2"
      ],
      "summary": "NO-RED-WAIVER: no Go test can observe which Actions steps ran, so this seam authors no contract test. It is not unverified: the chunk's verify asserts that the workflow holds exactly one `gh release upload` and that the step holding it still carries the release guard — the two facts spec line 56 turns on. Counting occurrences pins behaviour, not indentation.",
      "contract": {
        "states": [
          "dispatch-run: github.event_name == 'workflow_dispatch'; mode resolved, benchmarks run, verdict computed, no release asset written",
          "release-run: github.event_name == 'release'; the same plus the baseline upload"
        ],
        "transitions": [
          {
            "input": "a workflow_dispatch run after gate-mode-resolution lands",
            "state": "dispatch-run: `Determine gate mode` now executes; `Store VM baseline on the authorized release` does not",
            "effect": "no-op",
            "evidence": ".github/workflows/release.yml:210-215 keeps `if: github.event_name == 'release'`; it holds the only `gh release upload` in the file"
          },
          {
            "input": "the `Upload release evidence` step on a dispatch run",
            "state": "runs, as it does today, and writes a workflow artifact — not a release asset",
            "effect": "no-op",
            "evidence": "release.yml:220-230 uses actions/upload-artifact and `if: always()`. Task 4.2 concerns release assets, so this step is unaffected."
          },
          {
            "input": "the baseline download inside the ungated `Determine gate mode` step on a dispatch",
            "state": "reads only; downloads baseline-vm.txt into the workspace and publishes nothing",
            "effect": "no-op",
            "evidence": "`gh release download` and `gh release view` are read operations; the step writes only $GITHUB_OUTPUT and a workspace file"
          }
        ],
        "forbidden": [
          "Removing `if: github.event_name == 'release'` from `Store VM baseline on the authorized release`.",
          "Adding a `gh release upload`, `gh release create` or `gh release edit` to any ungated step.",
          "Gating the upload on the resolved mode instead of the event — a dispatch that resolves first-authorization would then publish a baseline.",
          "Quoting the literal string `gh release upload` inside the added comment. This chunk's verify counts occurrences of it and expects exactly one; word the comment around the command name."
        ],
        "seeding": [
          "Reading .github/workflows/release.yml at the seam's completion and confirming exactly one step carries the release guard and exactly one `gh release upload` exists, and that they are the same step.",
          "Never: asserting YAML text from a Go test."
        ],
        "budgets": [
          "1: steps carrying `if: github.event_name == 'release'` after this change.",
          "1: `gh release upload` invocations in the file.",
          "0: release assets a dispatch run writes."
        ]
      }
    },
    {
      "id": "gate-documentation",
      "tasks": [
        "5.1",
        "5.2"
      ],
      "summary": "NO-RED-WAIVER: documentation only. ADR 0008 and CHANGELOG.md carry no executable behaviour, and a substring test over either would pin prose formatting rather than a rule. Amends the two texts that state the overturned-looking invariant and records the user-visible change.",
      "contract": {
        "states": [
          "adr-amended: ADR 0008 states the runner-comparability rule and the exact-allocs vs averaged-bytes distinction",
          "changelog-recorded: CHANGELOG.md's [Unreleased] -> Changed names the inconclusive-across-runners behaviour"
        ],
        "transitions": [
          {
            "input": "ADR 0008:60-62, 'The other thirteen data-dominated cells — every GoldsetParse/* cell — keep the exact non-increasing bound with no allowance.'",
            "state": "rewritten to say every cell states an allowance and that those thirteen state an explicit 0, which IS the exact non-increasing bound",
            "effect": "forced",
            "evidence": "the sentence describes an absent entry; after bytes-allowance-config an absent entry is a configuration error. The rule it states is preserved; only its mechanism changes."
          },
          {
            "input": "ADR 0008's Thresholds section",
            "state": "gains the runner-comparability rule: a latency conclusion requires matching runner identities, allocation counts and allocated bytes stay enforced regardless, and the gate never normalises a configuration line to obtain a comparison",
            "effect": "set",
            "evidence": "task 5.1"
          },
          {
            "input": "ADR 0008's Note on benchstat '~' (lines 34-40)",
            "state": "amended to record that the bytes and allocs axes no longer pass through benchstat at all, so the blind spot it describes now applies to latency only",
            "effect": "set",
            "evidence": "cross-runner-verdicts moves both axes onto a direct read of the raw files. Leaving this note unamended would leave the ADR describing a gap the change closed."
          },
          {
            "input": "CHANGELOG.md [Unreleased] -> Changed",
            "state": "one entry naming the inconclusive-latency-across-runners behaviour",
            "effect": "set",
            "evidence": "task 5.2. Keep a Changelog 1.1.0 shape, matching the file's existing sections."
          }
        ],
        "forbidden": [
          "Describing this as a relaxation of the GoldsetParse/* bound. The bound is unchanged; only its representation moved from absence to an explicit 0.",
          "Recording per-cell allowance numbers in the ADR. They belong in tiers.json, which the ADR already points at.",
          "Creating a new ADR. 0008 is the single owner of these numbers (ADR 0008:19).",
          "Claiming the benchstat '~' blind spot is fully closed — it remains open for latency.",
          "Editing internal/perfgate/tiers.json. Its comment is c4's, and this chunk runs in parallel with the serial chain that owns that file."
        ],
        "seeding": [
          "Read docs/adr/0008-consumer-performance-gate.md and internal/perfgate/tiers.json before editing; both carry the same sentence and both must move together.",
          "Read CHANGELOG.md's existing [Unreleased] section for its heading shape."
        ],
        "budgets": [
          "2: texts carrying the sentence that changes (ADR 0008:60-62 and tiers.json:2).",
          "1: CHANGELOG entry.",
          "0: new ADRs."
        ]
      }
    },
    {
      "id": "existing-tests-unchanged",
      "tasks": [
        "1.4"
      ],
      "summary": "NO-RED-WAIVER: a re-run of the existing suite against the changed production code, authoring no assertion. The deliverable is that every existing test function in internal/perfgate/perfgate_test.go passes. Two tier-config fixtures were updated in c5 — inputs, not assertions — which is the whole of what task 1.4's \"unchanged\" gives up, and it is stated in the design rather than reinterpreted.",
      "contract": {
        "states": [
          "suite-green: every existing test function passes with no edit to its body",
          "pinned-verdicts-unchanged: TestPinnedProfile's 27 committed verdicts still match"
        ],
        "transitions": [
          {
            "input": "TestPinnedProfile after all 27 allowances land",
            "state": "pinned-verdicts-unchanged",
            "effect": "no-op",
            "evidence": "verified: no allowance-reading cell's verdict moves. Engine-sensitive cells use the inline bytes floor no allowance reaches; the 13 GoldsetParse cells read delta 0; guard-nil keeps 4; rule-load's bytes read -36.22%, passing before the allowance is consulted."
          },
          {
            "input": "TestPinnedProfile after tierConfigFile.BytesAllowanceBOp becomes map[string]*float64",
            "state": "suite-green: the test reads tiersFile.Comment only (perfgate_test.go:707-710), not the allowance map",
            "effect": "no-op",
            "evidence": "perfgate_test.go:707-710 unmarshals tierConfigFile and touches only Comment"
          },
          {
            "input": "TestLoadTierConfig and TestLoadTierConfig_BytesAllowance, whose literals name cells with no allowance entry",
            "state": "these WILL fail once a missing allowance is an error, and must be updated — the only existing tests this change edits",
            "effect": "forced",
            "evidence": "perfgate_test.go:369-391 seed tier configs from literals that carry no bytesAllowanceBOp. Naming them here rather than discovering them mid-implementation: task 1.4's 'unchanged' holds for 26 of the 28 functions, not all 28."
          },
          {
            "input": "the 13 Evaluate/Resolve tests",
            "state": "suite-green: they construct CellComparison values directly and never touch tiers.json or benchstat",
            "effect": "no-op",
            "evidence": "perfgate_test.go:18-313; perfgate.Evaluate's signature and rules are unchanged by every seam here"
          }
        ],
        "forbidden": [
          "Editing any existing test body other than the two tier-config literals named above.",
          "Updating profile-30637802780/verdict.txt. If a verdict moves, the change is wrong, not the oracle.",
          "Updating pinnedBenchEvaluatorSHA256 or pinnedBenchVMSHA256 (perfgate_test.go:674-675). No seam here touches those two files.",
          "Editing any test in this chunk. c5 made the two fixture edits; this chunk runs the package and reports."
        ],
        "seeding": [
          "go test -timeout 2m ./internal/perfgate/ with no -run filter, so the whole package runs rather than a narrowed subset."
        ],
        "budgets": [
          "28: existing test functions in internal/perfgate/perfgate_test.go.",
          "2: existing test functions this change must edit, both of them in c5, not here (TestLoadTierConfig, TestLoadTierConfig_BytesAllowance).",
          "27: pinned verdicts that must not move.",
          "0: edits to any committed corpus file."
        ]
      }
    },
    {
      "id": "full-floor",
      "tasks": [
        "6.1"
      ],
      "summary": "NO-RED-WAIVER: the whole-change floor. Runs the repository suite, the race suite over the three trees task 6.1 names, go vet and the linter, and records each command's exit status. Authors no assertion.",
      "contract": {
        "states": [
          "floor-status: pass or fail per command, in order"
        ],
        "transitions": [
          {
            "input": "each command in fullFloor, run in order at the repository root",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "task 6.1; make test is Makefile:14-15, make lint is Makefile:19-20"
          }
        ],
        "forbidden": [
          "Reporting a green floor from a narrowed -run.",
          "Dropping -timeout from any run, or running the race suite without a wall-clock limit.",
          "Substituting go build ./... for make test — go build never compiles _test.go, so a non-compiling test file passes it.",
          "Judging any latency figure from a local run."
        ],
        "seeding": [
          "Run from /home/zhuk/Projects/own/go-lispico with no environment overrides; GOTESTFLAGS stays at the Makefile default."
        ],
        "budgets": [
          "-timeout 2m for the non-race suite (Makefile:3); -timeout 10m for the race suite."
        ]
      }
    },
    {
      "id": "hosted-dispatch-preflight",
      "tasks": [
        "6.2"
      ],
      "summary": "NO-RED-WAIVER: requires a hosted GitHub Actions runner and cannot be executed by an implementer locally. Latency figures from a developer box are not trustworthy for this gate in any case. The deliverable is the dispatch run URL plus the recorded mode line, not a local command.",
      "contract": {
        "states": [
          "dispatch-mode-recorded: the dispatched run's `Determine gate mode` step output names non-regression and the baseline tag it resolved",
          "no-asset-published: the dispatched run's `Store VM baseline on the authorized release` step is skipped"
        ],
        "transitions": [
          {
            "input": "gh workflow run against the release-candidate tree, once the repository holds a v0.13.0 baseline",
            "state": "dispatch-mode-recorded: mode=non-regression, baseline_tag=<the newest release carrying bench-vm.txt>",
            "effect": "forced",
            "evidence": "the spec's 'Dispatch and release agree on the rule'; today the step is skipped entirely on a dispatch (release.yml:92) and the run falls through to first-authorization at :150"
          },
          {
            "input": "the same run's step list",
            "state": "no-asset-published: the upload step reports skipped",
            "effect": "no-op",
            "evidence": "release.yml:211, unchanged by this work"
          },
          {
            "input": "the same run's latency verdicts, if the hosted runner's CPU differs from the stored baseline's",
            "state": "every latency cell INCONCLUSIVE naming both identities; bytes and allocs still decided",
            "effect": "forced",
            "evidence": "this is the end-to-end confirmation of cross-runner-verdicts, and the only place it can be observed against real hardware variation"
          }
        ],
        "forbidden": [
          "Substituting a local run for the dispatch and reporting it as task 6.2.",
          "Dispatching against a branch that is not the release-candidate tree — `gh workflow run --ref` resolves against the remote, not the local worktree.",
          "Reading a first-authorization result as acceptable because 'no baseline was found' without checking whether enumeration failed."
        ],
        "seeding": [
          "gh workflow run on the Release consumer gate workflow, --ref pointing at the pushed release-candidate branch; then read the run's `Determine gate mode` step output and the step list.",
          "Never: a local `act` or shell reproduction — the defect being fixed is about the hosted API's failure modes."
        ],
        "budgets": [
          "1: dispatch run required.",
          "0: release assets it may publish.",
          "2: facts recorded from it — the resolved mode line and the skipped-upload step."
        ]
      }
    }
  ],
  "requirements": [
    {
      "shall": "The stored VM baseline SHALL carry the identity of the runner that produced it,",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "and the gate SHALL compare that identity against the candidate run's before",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "drawing a latency conclusion from it. Where the two differ, the gate SHALL report",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "runners, and SHALL NOT convert the difference into a pass or a failure.",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "Allocation counts and allocated bytes SHALL stay enforced across differing",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "The gate SHALL NOT strip or normalise the configuration lines that make two",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "and the gate SHALL treat that refusal as a verdict input rather than an obstacle.",
      "tests": [
        "TestReadRunnerIdentity",
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive",
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "- **THEN** the latency cells judged against that baseline SHALL be reported inconclusive with both runner identities named, and the release SHALL still be gated on correctness, the race suite, allocation counts and allocated bytes",
      "tests": [
        "TestCrossRunner_LatencyInconclusiveBytesEnforced",
        "TestCrossRunner_UnknownIdentityIsNotAMatch",
        "TestReadBenchmarkMetrics_MatchesBenchstat",
        "TestCrossRunner_MissingBaselineCellIsInconclusive"
      ]
    },
    {
      "shall": "- **THEN** the gate SHALL fail with that reason stated, and SHALL NOT retry by removing the configuration lines that caused the refusal",
      "tests": [
        "TestParseBenchstatCSV_UnpairedSingleGroup",
        "TestRun_UnpairedComparisonExitsThree"
      ]
    },
    {
      "shall": "count, so repeated runs of identical code differ by a few bytes. Every cell SHALL",
      "tests": [
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance",
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance",
        "TestEvaluate_BytesAllowance"
      ]
    },
    {
      "shall": "therefore state a bytes allowance, and the gate SHALL fail with a missing-config",
      "tests": [
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance",
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance",
        "TestEvaluate_BytesAllowance"
      ]
    },
    {
      "shall": "An allowance SHALL be justified by observed sampling spread on that cell, and",
      "tests": [
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance",
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance",
        "TestEvaluate_BytesAllowance"
      ]
    },
    {
      "shall": "SHALL NOT be widened to admit a measured regression. Allocation counts are exact",
      "tests": [
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance",
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance",
        "TestEvaluate_BytesAllowance"
      ]
    },
    {
      "shall": "in the same output and SHALL keep a zero allowance.",
      "tests": [
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance",
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance",
        "TestEvaluate_BytesAllowance"
      ]
    },
    {
      "shall": "- **THEN** the gate SHALL fail naming that cell and the missing allowance, and SHALL NOT judge it against an implicit zero",
      "tests": [
        "TestLoadTierConfig_MissingBytesAllowance",
        "TestLoadTierConfig_StatedZeroIsNotAbsent",
        "TestLoadTierConfig_NegativeBytesAllowance"
      ]
    },
    {
      "shall": "- **THEN** the cell SHALL pass on the bytes axis",
      "tests": [
        "TestEvaluate_BytesAllowance",
        "TestBytesAllowancesAreJustifiedBySpread",
        "TestTierConfig_EveryCellStatesAnAllowance"
      ]
    },
    {
      "shall": "The gate SHALL resolve its comparison mode from the repository's stored",
      "tests": [
        "TestResolveGateMode",
        "TestResolveGateMode_FailureNeverSelectsFirstAuthorization",
        "TestRun_ResolveModeSubcommand"
      ]
    },
    {
      "shall": "A dispatched run SHALL NOT publish a baseline or upload a release asset.",
      "tests": [
        "NO-RED-WAIVER: seam dispatch-publishes-nothing — a property of .github/workflows/release.yml verified by reading it; no Go test can observe which Actions steps ran"
      ]
    },
    {
      "shall": "- **THEN** the dispatched run SHALL judge the candidate as non-regression against that baseline, reaching the same per-cell verdicts the release run reaches",
      "tests": [
        "TestResolveGateMode",
        "TestResolveGateMode_FailureNeverSelectsFirstAuthorization",
        "TestRun_ResolveModeSubcommand"
      ]
    },
    {
      "shall": "The gate SHALL treat a failure to enumerate or download release assets as a",
      "tests": [
        "TestResolveGateMode",
        "TestResolveGateMode_FailureNeverSelectsFirstAuthorization",
        "TestRun_ResolveModeSubcommand"
      ]
    },
    {
      "shall": "thresholds SHALL happen only when the repository is known to hold no baseline.",
      "tests": [
        "TestResolveGateMode",
        "TestResolveGateMode_FailureNeverSelectsFirstAuthorization",
        "TestRun_ResolveModeSubcommand"
      ]
    },
    {
      "shall": "- **THEN** the gate SHALL fail naming that failure, and SHALL NOT judge the candidate against first-authorization improvement thresholds",
      "tests": [
        "TestResolveGateMode",
        "TestResolveGateMode_FailureNeverSelectsFirstAuthorization",
        "TestRun_ResolveModeSubcommand"
      ]
    }
  ],
  "testHarness": [
    "modeName — internal/perfgate/perfgate_test.go:290 — maps a Mode to its subtest label, \"first-authorization\" or \"non-regression\"; every mode-looping table uses it to name subtests.",
    "hashHex — internal/perfgate/perfgate_test.go:743 — sha256 hex of a byte slice, used for the two pinned raw-file digests.",
    "parsePinnedVerdicts — internal/perfgate/perfgate_test.go:751 — parses cmd/perfgate's report format (\"name: VERDICT\" or \"name: VERDICT (reason)\") out of a profile's verdict.txt into map[string]Verdict keyed by the full cell name including the -N suffix.",
    "parseVerdict — internal/perfgate/perfgate_test.go:766 — PASS/FAIL/INCONCLUSIVE string to Verdict, t.Fatalf on anything else.",
    "pinnedProfileRunID / pinnedProfileDir — internal/perfgate/perfgate_test.go:662-665 — consts \"30637802780\" and \"testdata/profile-\" + pinnedProfileRunID; the single committed profile the suite reads.",
    "pinnedBenchEvaluatorSHA256 / pinnedBenchVMSHA256 — internal/perfgate/perfgate_test.go:673-676 — content digests of that profile's bench-evaluator.txt and bench-vm.txt; both must be updated together with benchstat.csv and verdict.txt when a profile is replaced.",
    "TestPinnedProfile — internal/perfgate/perfgate_test.go:686-741 — the only golden loader: digests the two raw files, parses benchstat.csv through ParseBenchstatCSV, loads tiers.json through LoadTierConfig and again through json.Unmarshal into tierConfigFile, reads verdict.txt as the oracle, asserts the three name sets match with assert.ElementsMatch, then calls Evaluate per cell in ModeFirstAuthorization.",
    "TestResolve_AfterRerun — internal/perfgate/perfgate_test.go:128-162 — table runner over {name, tier, mode, want} rows asserting Resolve.",
    "TestEvaluate_Startup_BytesOrAllocsIncreaseFails — internal/perfgate/perfgate_test.go:206-260 — table of {name, latency, bytes, allocs, wantReason} rows crossed with both modes; subtest name fmt.Sprintf(\"%s/%s\", tt.name, modeName(mode)); asserts VerdictFail plus a Reason fragment.",
    "TestEvaluate_Ordering_BytesAllocsBeforeSignificance — internal/perfgate/perfgate_test.go:506-545 — table of {name, tier, mode} rows, each expanded into two fixed subtests named tt.name+\"/bytes increased\" and tt.name+\"/allocs increased\".",
    "TestEvaluate_BytesAllowance — internal/perfgate/perfgate_test.go:590-650 — both modes crossed with four named subtests (within allowance, past allowance, unlisted cell gets no allowance, allowance does not reach allocs); the closest existing model for a new allowance or runner-mismatch test.",
    "TestTrimProcsSuffix — internal/perfgate/perfgate_test.go:347-367 — {name, in, want} table over TrimProcsSuffix.",
    "TestParseBenchstatCSV — internal/perfgate/perfgate_test.go:315-345 — inline three-block CSV const whose preamble carries goos:/goarch: only (no pkg:/cpu:); asserts DeltaPct with assert.InDelta and the Significant flags. Any new preamble handling needs a fixture with a cpu: line, which this const does not have.",
    "TestEvaluate_NonRegression_ImprovementPasses / _RegressionStillFails — internal/perfgate/perfgate_test.go:419-449 — loop over []Tier inline without t.Run, passing the tier into the assert message.",
    "No CellComparison builder and no cmd/perfgate test exist: cmd/perfgate/ has no _test.go file, and internal/perfgate/perfgate_test.go is the only test file in either package."
  ],
  "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 4
  }
}
```
