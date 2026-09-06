# Verification — builtin-budget-clock-cadence

Candidate: `0755d815218a7fd37ac19ade191a5ee9a229cb2b` on
`zapply/builtin-budget-clock-cadence`. Base:
`39fe049115925fa1ec3e262eae0239c50e5d3b0c` on `master`.
Reviewed plan: existing-service-strict, heavy, full specification, quality and
performance review. The user approved the error-settlement and deterministic-cost
amendments; the exact eight-synchronization cadence remains binding.

Status: implementation fast-forwarded into local `master` at the candidate SHA.
Tasks 17/18 complete. Correctness, allocation proof and all three final reviews
pass with no blockers or warnings. Hosted task 5.1 remains open. No push, hosted
dispatch or release tag has been performed.

## Behavior and regression evidence

The shared countdown reads the clock on the first positive synchronization and
every eighth thereafter. Installing a deadline resets it. Cancellation and
reduction charging remain unconditional. `evalState` stays 192 bytes.

`BuiltinWorkBudget.Finish(error)` forces deadline/cancellation observation while
settling an unlatched ordinary error, including an empty remainder. Reduction
failure wins; terminal inputs retain identity; synchronization failures latch.
Five collection, adapter and standard-library selectors use `Finish` on error
paths. Successful paths retain `Flush` and existing result accounting.

The original cadence and deadline-install tests failed before their respective
fixes and now pass. Three forced-settlement tests likewise failed before the
forced check landed; successful cadence, exactly-once charging and latch replay
companions pass. The unchanged runtime regression
`TestCLAdapters_LateVMDeadline/deadline-wins-over-pending-type-error` failed before
consumer migration and passes afterward. Inventory guards remain unchanged.

The classifier amendment changes only `core/error.go`. It preserves the two
`errors.Is` searches, first typed match, joined-error traversal order, custom
`As` behavior and retained target identity. A target is allocated only when a
custom `As` subtree requires it. Standard precreated errors and wrappers allocate
zero; custom traversal may need one package-owned target cell. Go 1.24 remains
the declared minimum; allocation evidence uses `go1.27.0-X:nodwarf5`, linux/amd64.

`TestIsTerminalEvalError_StandardErrorsAllocateZero` and
`TestBuiltinWorkBudget_FinishStandardErrorsAllocateZero` observed 52 failing
allocation rows before implementation, each reporting one allocation instead
of zero. All 80 allocation rows now pass. Three traversal/custom-hook companions
passed before implementation and pass afterward. Existing tests were preserved.

## Correctness floor

The final candidate passed this complete command with exit zero:

```sh
env GOMAXPROCS=2 GOFLAGS=-p=2 timeout 20m zsh -c \
  'make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && make build && go vet ./... && make lint'
```

Ordinary tests, race tests, build, vet and lint pass; lint reports zero issues.
All committed VM allocation pins pass unchanged, including `queue-promote=174`.
The final clock and pin slice also passed separately:

```sh
env GOMAXPROCS=2 GOFLAGS=-p=2 timeout 3m zsh -c \
  "go test -p 2 -parallel 2 -timeout 2m -v ./core/ -run '^TestBuiltinWorkBudget_' && go test -p 2 -parallel 2 -timeout 2m -v ./internal/goldset/ -run '^TestGoldsetVMAllocations$'"
```

The complete logs are retained under `bin/builtin-budget-clock-cadence/` as
`revised-floor.log` and `revised-clock-and-pins.log`. Temporary-overlay and
filtered checks use bounded raw Go commands because no project target supports
them. Go cache and temporary-directory defaults remain unchanged.

## Allocation proof

The original 27-row precreated-input settlement probe now reports zero for
`finish_allocs`, `old_allocs`, `added_allocs` and `classifier_allocs` in every row.
This closes the recurring allocation defect found on the previous candidate.
The committed tests separately cover public budget seeding and custom hooks.

The first approved controlled protocol ran all 52 cells under source base,
classifier-restored candidate control and final candidate, twice. All twelve
commands passed; the exact comparison failed. All windows reported state size
192, zero GC and zero reader/VM pool misses. Fifty-one cells matched exactly;
evaluator `Eval/safe-parse` removed the expected 10,000 eight-byte targets but
also contained four to six small runtime cache allocations.

The source and existing allocation profile identify sampled type-assertion and
interface-switch caches. Runtime profile serialization removes leading runtime
frames, so allocations appear on the calling `errors.is` and `asLispicoError`
lines. Their six cache tables occupy 48, 48, 64, 64, 80 and 112 bytes. A 100-call
warmup can leave these probabilistically populated caches cold.

The reviewed measurement-setup refinement uses: uniform 32,768-call warmup,
10,000 measured calls, the same twelve commands and all 52 cells. An identical
temporary runtime overlay records both cache-builder counters. Every measured
window must show zero cache builds in addition to zero GC and pool misses.
Exact baseline/control equality, sole classifier-target reductions and exact
repeat identity remain required. No totals are subtracted, no fixtures excluded,
and no favorable reruns permitted.

All twelve calibrated commands exited zero. The comparator passed all 104
triple comparisons covering 52 cells twice. All 312 measured rows completed
10,000 calls with zero GC, reader/VM misses, type-assert cache builds and
interface-switch cache builds. Each variant repeated identically. Every base
and classifier-restored control cell matched exactly.

Fifty-one final-candidate cells also matched exactly. Evaluator `Eval/safe-parse`
changed from 54,080,000 bytes / 1,030,000 allocations to 54,000,000 bytes /
1,020,000 allocations per 10,000 calls: exactly one fewer eight-byte target per
call, with every other size-class count unchanged. Both repetitions matched.
The numerical rule applies uniformly; this fixture has no exception. Each
safe-parse evaluator warmup recorded three type-assert and three interface-switch
cache builds, confirming live counters before the zero-build measured window.

Execution records, all reports, manifests, overlays and exact comparison are
retained in `bin/builtin-budget-clock-cadence/cached-probe/`. The original failed
warm100 protocol remains in the sibling `deterministic-probe/` directory.
Profile attribution and independent setup-review packets are retained alongside
both. Standard-error settlement, state layout and VM pins pass separately.

## Earlier diagnostics and hosted gate

Earlier sampled `time.runtimeNow` shares were 10.74% on the base, 0.70% on
`v0.12.0`, 2.08% on the initial candidate and 2.48% after the settlement fix.
The old one-percent criterion failed. The user approved replacing local CPU
share and rounded raw-byte acceptance with exact clock/allocation proof plus
the unchanged hosted gate. Those earlier failures remain diagnostic history;
no local latency or hosted non-regression verdict is claimed.

Specification, quality and performance reviews pass on the final candidate. `Release consumer gate` resolves
as workflow 315632138. Workflow source, thresholds, tiers and exact VM pins are
unchanged. Task 5.1 stays open until the merged candidate is pushed and the gate
runs against its verified remote SHA. Manual dispatch cannot upload a release
baseline; upload remains conditional on a release event.

## Local closeout

All twelve implementation chunks closed under the reviewed plan. The release
chunk closes only its local workflow-resolution check; task 5.1 remains open
under the explicit post-merge plan.

| Chunk | Stages and result |
| --- | --- |
| clock-seam | Behavior-identical change; existing tests/build/vet/lint pass; RED waived |
| flush-clock-cadence | RED, implementation, sealed-test guard and verification pass |
| deadline-install-reset | RED, implementation, sealed-test guard and verification pass |
| decision-record | Documentation; verification pass; RED waived |
| finish-member | Initial behavior-identical API; tests/vet/lint pass; RED waived |
| finish-forced-error | RED, implementation, sealed-test guard and verification pass |
| finish-consumers | Existing regression reproduced, implementation and verification pass |
| finish-documentation | Existing ADR/release entry updated; documentation checks pass |
| terminal-classifier-allocation | Allocation RED, semantic companions, implementation and verification pass |
| cost-verification | Exact clock, state/pins, settlement and controlled allocation proof pass |
| suite-verification | Complete ordinary/race/build/vet/lint floor passes |
| release-gate-dispatch | Local workflow resolution passes; hosted result pending |

All seven final test seals pass. Every deleted return/function line maps to
cadence routing, forced error settlement, consumer migration or classifier
replacement. Formatting and diff checks are clean. Strict change validation
passes. Primary branch did not advance during implementation; no rebase or
stash was needed. The implementation changed eleven files.

The user selected both recorded scope decisions: revise the error-settlement
plan, then revise the cost proof. The original plan received five review rounds;
the subsequent cache setup refinement received independent technical review
within that approved methodology. No fastpass, silent approval or review lens
skip was used.

Run accounting is skipped because the change has no epic artifact:
`spego read --type epic --slug builtin-budget-clock-cadence --json` returned
`ARTIFACT_NOT_FOUND`. Existing flow configuration is present. There is no other
pending change to suggest; the board's next item is null.

Archive written to `2026-09-06-builtin-budget-clock-cadence`; `core-engine`
received the reviewed requirement delta. Full strict spec validation initially
failed on pre-existing `Purpose` placeholders in `dialect` and `repl-cli`, both
byte-identical to the base. Their two placeholder sentences were replaced with
summaries of their existing requirements; requirement bodies are unchanged.
Strict validation then passed all 14 specifications with zero failures.
