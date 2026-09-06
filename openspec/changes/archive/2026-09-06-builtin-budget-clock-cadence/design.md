# Design — builtin-budget-clock-cadence

## Context

See proposal.md — Why. The measured facts this design answers to:
`core.(*BuiltinWorkBudget).flushPending` (`core/builtin_budget.go:56`) reads the wall
clock on every flush; `stdlib.finishBuiltin` (`plugins/stdlib/charges.go:13`) flushes
before every Builtin return; and a budget is constructed per GoFunc call, at eleven sites
across `plugins/stdlib`, `cl/cl.go:113` and `internal/collections/kernels.go`. So a
Builtin that performs a handful of work units pays one clock read per call.

`core` has no injectable clock today — `nowFunc` exists only in `core/vm` — so the tests
that count reads need a seam introduced first.

## Goals / Non-Goals

**Goals.** Amortize the clock read behind a bounded cadence carried by the evaluation.
Keep the Reduction charge and the caller-cancellation check unconditional. Keep the
deadline error shape and terminal-error precedence unchanged. Keep standard-error
classification and settlement allocation-free, preserve the evaluation-state size,
and permit only attributable allocation reductions in controlled gold-set totals,
with committed pins unchanged.

**Non-Goals.** The other two unconditional clock reads stay as they are:
`core/eval.go:316` already sits behind a 128-node budget, and
`core/value_walk_context.go:39` carries no measured cost. Neither is touched. No timer or
externally-armed expiry flag is introduced. No local benchmark-threshold test is written.

## Decisions

**The cadence lives on `evalState`, not on `BuiltinWorkBudget`.** A budget is confined to
one GoFunc call, so a per-budget countdown would read the clock once per call and amortize
nothing — the exact defect being fixed. `evalState` is the per-evaluation object every
budget already reaches through `b.st`.

**The counter is `atomic.Int32` packed into the padding hole after `calleeCharged bool`,
never appended.** `evalState` is 192 bytes. Appending an `atomic.Int64` beside `budget`
makes it 200, which allocates in the 208 size class — and `BenchmarkGoldset` calls
`eng.Eval` once per iteration, each installing eval state, so that is +16 B/op on every
`Goldset/*` cell. Per-cell bytes allowances run 0–8 (`internal/perfgate/tiers.json`), so
the change would fail the very gate it exists to unblock. Packed as an `Int32` in existing
padding, `unsafe.Sizeof` stays 192. A cadence of 8 fits an `int32` with room to spare.
Verify before and after with `unsafe.Sizeof` and controlled allocation totals for
every gold-set cell. Raw benchmark bytes can vary with pool activity; the revised
proof fixes one worker, warms each cell, disables GC during measurement, and
records reader and VM pool misses instead of assuming exact raw `B/op`.

**Read-due is `<= 0`, not `== 0`.** `Load` then `Add(-1)` is not atomic as a pair, so
concurrent flushes over one `evalState` can drive the counter negative; `== 0` would then
be a silent deadline bypass until it wrapped.

**Atomic, not plain.** `evalState` documents two regimes: atomic for anything touched
across sequential evaluator hops (`budget`, `core/eval.go:116-119`), plain for what only
the evaluating goroutine touches (`calleeCharged`, `core/eval.go:103-108`). The countdown
plays `budget`'s role, so it takes `budget`'s regime.

**Cadence 8, matching `deadlineClockCadence` (`core/vm/vm.go:836`).** Eight
synchronizations is 1,024 local work units — the same bound the spec's existing "Context
observed within the reduction budget" scenario already permits for cancellation.

**Reset at the install sites, not by caller discipline.** `WithEvalDeadline`
(`core/eval.go:403`) mutates an existing state whose countdown may be mid-flight, so it
resets explicitly. The lazy path (`core/eval.go:266`) materializes a brand-new state whose
zero value is already read-due, so it needs no store — a fact pinned by a test rather than
asserted in a comment alone. This mirrors how `SetDeadline`/`SetTimeout` zero the VM's
countdown at their own choke point (`core/vm/vm.go:335-347`).

**Force deadline observation when settling an ordinary error.** A final flush
between scheduled clock reads can miss expiry and let a callback error escape.
`BuiltinWorkBudget.Finish(error) error` settles pending units exactly once and
forces the armed deadline and caller-cancellation check for a nonterminal input,
including an empty remainder. A failed reduction charge wins before those checks.
An already-terminal input retains its identity after ordinary flushing; nil input
behaves exactly like `Flush`. Only synchronization errors latch. A successful
forced clock read resets the countdown to seven.

The five existing selectors are `finishSort`, `flushErr`, `finishAdapter`,
`finishBuiltin`, and `getInResult`. Their nonnil-error paths use `Finish`; their
successful paths retain direct `Flush` and existing result accounting. This also
preserves the source inventory's live helper recognition and return-site counts.
No inventory exemptions or test edits are needed. The existing runtime
deadline-precedence regression remains binding.

**Avoid a typed-error target until a custom `As` hook needs one.** Keep the two
existing `errors.Is` searches in their original order. A private concrete search
for the first `*LispicoError` follows single wrappers iteratively and joined
errors depth-first, left-to-right. A custom `As` subtree delegates to `errors.As`
with one lazily allocated target shared across siblings. Preserve target mutations
from false-returning hooks and update retained targets on later direct matches.
Standard errors and wrappers allocate zero during classification; custom `As`
may require one package-owned target cell. Arbitrary hook allocations are outside
that zero target. `Finish` and all clock/accounting code remain unchanged.

**Staging is FIELD-FIRST, split by what the linter allows.** `unused` is on by default
under this repo's config, so a chunk that declares the countdown and the constant without
using them fails its own verify. Chunk 1 therefore declares only `nowFunc` — immediately
used by the routed read — and the countdown and constant arrive in chunk 2, which uses
them. Consequently the sealed tests spell the cadence as the literal 8 and never name the
unexported constant or field, unlike `core/vm/deadline_clock_cadence_test.go:42`, which
could name the const because in the VM it already existed.

**Alternative rejected: an externally-armed expiry flag.** Already rejected for the VM in
`601b38f` on a correctness argument that applies here too — a `time.AfterFunc` callback on
its own goroutine can write its flag after an unrelated later call has reset the state,
producing a false-positive deadline error on an innocent evaluation.

## Risks / Trade-offs

- Worst-case deadline overrun inside Builtin work widens from one synchronization to eight
  (1,024 local work units) → a documented behavior change, recorded in the spec delta, in
  `CHANGELOG.md` under `[0.13.0]`, and pinned by
  `TestBuiltinWorkBudget_DeadlineCrossingBoundedBySynchronizations`.
- One uncontended atomic operation per flush where a plain `int` would cost none → kept,
  because `evalState` may be shared across sequential evaluator hops and correctness beats
  the smaller constant.
- Tests swapping the package-level `nowFunc` cannot run in parallel, unlike every existing
  test in `core/builtin_budget_test.go` → the two red stages are told so explicitly.
- `core/eval.go:316` and `core/value_walk_context.go:39` keep calling `time.Now()`
  directly while `flushPending` goes through `nowFunc` → deliberate: it keeps the sealed
  read counters measuring the budget path alone.

## Migration Plan

No data or schema migration. The Go API gains the additive `Finish(error) error`
method. Existing `Step` and `Flush` retain their contracts; migrated error
selectors preserve deadline precedence alongside the ordinary cadence. Rollback
reverts the cadence and settlement changes together.

## Implementation plan

Twelve chunks, with the error-settlement stages before cost/suite verification
and separate documentation shards. Mode `existing-service-strict`, tier `heavy`
for the additive API and cross-package consumers; lenses `spec`, `quality`, `perf`.

1. **`clock-seam`** (2.1, `go-coder`) — declare `var nowFunc = time.Now` beside
   `checkInterval` in `core/eval.go`; route `flushPending`'s existing read through it and
   drop the now-unused `time` import from `core/builtin_budget.go`. Nothing else: an
   unused const or field is rejected by `unused`. No red stage — no observable change.
   Verify: `go test -timeout 2m ./core/ && go build ./... && go vet ./core/... && golangci-lint run ./core/...`
2. **`flush-clock-cadence`** (1.1 red, 2.2 code, `go-coder`) — the countdown as
   `atomic.Int32` in the padding hole after `calleeCharged bool`, `const
   deadlineClockCadence = 8`, and the gate around the clock read only. The red stage
   authors four tests in `core/builtin_budget_test.go`; exactly one is expected to fail
   (`TestBuiltinWorkBudget_ShortCallsShareClockCadence`), the other three being pins on the
   half of `flushPending` the cadence must never gate.
   Red run: `go test -timeout 2m ./core/ -run 'TestBuiltinWorkBudget_ShortCallsShareClockCadence'`
3. **`deadline-install-reset`** (1.2 red, 2.3 code, `go-coder`) — reset at
   `WithEvalDeadline`; a comment, not a store, at the lazy site. One red
   (`TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync`) plus one companion pin on the
   lazy site, which must seed through `AdoptEvalStateWithMeter` from a context carrying no
   eval state — `budgetCtx` attaches one and never enters `lazyEvalStateCtx.Value`.
4. **`decision-record`** (3.1, `coder`, parallel) — the `CHANGELOG.md` entry under
   `[0.13.0]`, naming the observable change rather than the countdown.
5. **`cost-verification`** (4.1, 4.3, `coder`) — exact counting-clock tests, unchanged
   allocation pins and 192-byte state, plus controlled allocation totals for all
   52 evaluation/parser cells. Existing CPU profiles remain diagnostic.
6. **`suite-verification`** (4.2, `coder`) — the floor.
7. **`release-gate-dispatch`** (5.1, `coder`) — verify only; the dispatch resolves its ref
   on the remote and therefore cannot run before the merge is pushed. Task 5.1 stays open
   at merge and closes post-merge on the recorded run id.

The error-settlement stages extend the implementation before `cost-verification`:

- **`finish-member`** (2.4) adds `Finish` with existing flush-and-select semantics,
  so the deterministic tests compile before forced behavior lands.
- **`finish-forced-error`** (1.3 red, 2.5 code) pins six contract cases and adds
  forced observation for nonterminal inputs only. Existing core tests stay sealed.
- **`finish-consumers`** (1.4 red, 2.6 code) first reproduces the existing runtime
  regression, then migrates the five selectors. The regression and inventory
  assertions remain unchanged.
- **`finish-documentation`** (3.2, parallel documentation shard) updates the
  existing ADR and release entry for the error-settlement exception.
- **`terminal-classifier-allocation`** (1.5 red, 2.7 code) appends two allocation
  tests and three semantic companions, then changes only `core/error.go`.
  Existing tests stay sealed; Go 1.24 remains the minimum.

The approved cost revision replaces local `time.runtimeNow <= 1.0% flat` and
rounded raw-byte equality with deterministic evidence plus the unchanged hosted
gate. Earlier measurements failed those original criteria; they remain recorded
as failures in verification history. Clock tests require the first read and every
eighth ordinary synchronization, with the existing forced-error exception.

The controlled allocation proof uses the same toolchain, one worker, GC off,
32,768 warmups and 10,000 measured calls per cell. All 52 cells run twice in a fixed
order. Each window must have zero GC, zero reader-pool misses, zero VM-pool
misses and zero runtime type-assertion/interface-switch cache builds. A candidate overlay restoring only the baseline `core/error.go` must match
the base exactly. The final candidate must match this control or remove only
allocations in the size class of the classifier's pointer target, with byte totals
equal to that size times the removed count. Other size classes stay identical.
Every variant repeats identically. This allows the shared classifier to improve
existing catch paths without accepting positive or unexplained differences.
The standard-error allocation tests and existing 27-row settlement probe must
confirm removal of the recurring allocation. Custom `As` target storage remains
explicit package-owned cost, bounded to one cell per typed search.

**Measurement setup calibration.** The first single-worker protocol with 100
warmups failed exact comparison in evaluator `Eval/safe-parse`. An existing
allocation profile attributes its excess to sampled runtime interface caches:
five measured objects totaling 352 bytes, plus one 64-byte warmup table. Runtime
profile serialization hides leading runtime frames, leaving the assertion and
switch caller lines as the recorded sites. The failed reports remain unchanged.

A separate technical review approved uniform 32,768-call warmup and a shared
temporary runtime overlay that counts both cache builders immediately before
their existing allocations. Sampling, cache contents, allocation sizes and CAS
remain unchanged. Counter snapshots enclose the measured memory-stat window;
any measured cache build invalidates that window. Warmup counts provide a
positive observation of the counters. All 52 cells, two fixed repetitions,
10,000 measured calls, exact comparisons, unchanged pins and hosted gate remain
binding. This refines setup within the approved methodology; it grants no new
allocation allowance or fixture exception. The reviewed setup manifest is
`450fe638a7695c96e2d67440922acdd19dbf27af4019400e00e9162bd2b6d431`.

**Floor:** `make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint`

**Plan review:** `zarchitect`, 3 rounds, verdict pass. Round 1 raised five blockers —
`unused` rejecting the field-first chunk, a coder being handed test authoring on a sealed
file, the `evalState` size-class regression, an unreachable seeding path for the lazy site,
and a live remote dispatch scheduled before the fix existed. Round 2 raised two more — a
red set containing a test that cannot fail, and the dispatch command still rendering into a
code prompt. All are answered above. Round 3 confirmed the repairs with no blockers.
Round 4 approved the error-settlement amendment, retaining every cost criterion.
Round 5 approved the classifier and deterministic-cost amendment. Semantic
companions must pass before implementation, and exact VM pins remain binding.

**Two spec lines carry no test, by their nature.** The MODIFIED delta reproduces the whole
184-line requirement, and two of its blocks state permissions rather than obligations —
counter values are not required to be equal across evaluators or compiler versions, and a
difference is not to be treated as a defect. A coverage pass over the other 27 blocks found
real tests for 25 (19 confirmed, 6 partial); for these two it found none, and none was
invented. `validatePlan` reports them as its only two remaining errors.

## Plan appendix

```json
{
  "v": 2,
  "change": "builtin-budget-clock-cadence",
  "baseSha": "39fe049115925fa1ec3e262eae0239c50e5d3b0c",
  "generatedAt": "2026-09-06T12:50:30.377025+00:00",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "clock-seam",
      "taskIds": [
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "clock-seam",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core/"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "core/eval.go",
          "symbol": "checkInterval",
          "anchor": "const checkInterval int64 = 128",
          "change": "declare `var nowFunc = time.Now` beside it, mirroring core/vm/vm.go:15. Declare nothing else here: an unused const or field is rejected by `unused`, which is on by default under this repo's .golangci.yml."
        },
        {
          "task": "2.1",
          "file": "core/builtin_budget.go",
          "symbol": "(*BuiltinWorkBudget).flushPending",
          "anchor": "if !b.st.deadline.IsZero() && !time.Now().Before(b.st.deadline) {",
          "change": "route this read through nowFunc() and change nothing else. Same branch, same value, no gating — the gating is the next chunk, whose red test must be able to fail. `time.Now()` at line 56 is this file's only use of the time package, so routing it through nowFunc() means dropping the `time` import — otherwise the package does not compile."
        }
      ],
      "contract": {
        "states": [
          "seam-declared"
        ],
        "transitions": [
          {
            "input": "any flush with an armed deadline",
            "state": "seam-declared",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:56 — the read moves from time.Now() to nowFunc(), same value, same branch"
          }
        ],
        "forbidden": [
          "declaring the countdown field or the cadence constant in this chunk — both would be unused and `unused` is on by default under .golangci.yml",
          "any change to when the clock is read"
        ],
        "seeding": [
          {
            "state": "seam-declared",
            "path": "no test seeds this chunk; its verify is the existing core suite plus the linter"
          }
        ],
        "identifiers": [
          "nowFunc",
          "flushPending",
          "time.Now"
        ],
        "numbers": [
          {
            "name": "behavior changes in this chunk",
            "value": "0"
          },
          {
            "name": "symbols left declared-and-unused",
            "value": "0"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./core/ && go build ./... && go vet ./core/... && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "flush-clock-cadence",
      "taskIds": [
        "1.1",
        "2.2"
      ],
      "prev": "clock-seam",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "flush-clock-cadence",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core/"
      ],
      "sites": [
        {
          "task": "2.2",
          "file": "core/eval.go",
          "symbol": "evalState",
          "anchor": "calleeCharged     bool",
          "change": "declare the countdown as `atomic.Int32` in the padding hole after this field, before pendingCellAllocs. Appending it beside `budget` instead grows evalState 192 -> 200 bytes, into the 208 size class: every Eval allocates one, so that is +16 B/op on every Goldset cell against allowances of 0-8, failing the gate this change exists to pass. Verify with unsafe.Sizeof before and after. Also declare `const deadlineClockCadence = 8` beside checkInterval — it becomes used in this chunk."
        },
        {
          "task": "2.2",
          "file": "core/builtin_budget.go",
          "symbol": "(*BuiltinWorkBudget).flushPending",
          "anchor": "if !b.st.deadline.IsZero() && !nowFunc().Before(b.st.deadline) {",
          "change": "gate only the clock read behind the countdown: read when due, then reset to deadlineClockCadence-1; otherwise decrement. `b.st.chargeReductions(n)` above and `b.ctx.Err()` below stay unconditional on every flush. Use a `<= 0` read-due predicate, never `== 0`. The anchor carries nowFunc() because the previous chunk routes the read through it; it does not exist at baseSha.",
          "new": true
        },
        {
          "task": "1.1",
          "file": "core/builtin_budget_test.go",
          "symbol": "budgetCtx",
          "anchor": "func budgetCtx(",
          "change": "the red stage adds four tests here: one red (bounded clock reads) and three regression pins that hold before and after (bounded expiry detection, cancellation and the Reduction charge unconditional mid-cadence). Tests that swap the package-level nowFunc must not call t.Parallel, unlike every existing test in this file. Spell the cadence as the literal 8; do not name the unexported constant or field. Exactly one of the four is expected to fail; the three pins assert the half of flushPending the cadence must never gate, so they pass on authoring and fail only if the gating over-reaches."
        }
      ],
      "contract": {
        "states": [
          "no-deadline",
          "read-due",
          "mid-cadence",
          "latched"
        ],
        "transitions": [
          {
            "input": "flushPending, st.deadline is zero",
            "state": "no-deadline",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:56 — the whole deadline branch is already guarded by !st.deadline.IsZero(); the counter must not move and nowFunc must not be called"
          },
          {
            "input": "flushPending, deadline armed, deadlineClockPolls <= 0, now before deadline",
            "state": "read-due",
            "effect": "forced",
            "evidence": "core/vm/vm.go:856-864 — read nowFunc(), then store deadlineClockCadence-1 so the next read is 8 synchronizations out"
          },
          {
            "input": "flushPending, deadline armed, deadlineClockPolls <= 0, now at or after deadline",
            "state": "read-due",
            "effect": "set",
            "evidence": "core/builtin_budget.go:56-58 — b.latched = context.DeadlineExceeded, returned bare, not wrapped and not a *LispicoError"
          },
          {
            "input": "flushPending, deadline armed, deadlineClockPolls > 0",
            "state": "mid-cadence",
            "effect": "clear",
            "evidence": "core/vm/vm.go:863 — decrement by one, no clock read, no deadline verdict"
          },
          {
            "input": "flushPending at any cadence phase with a cancelled ctx",
            "state": "mid-cadence",
            "effect": "forced",
            "evidence": "core/builtin_budget.go:60-63 — b.ctx.Err() is checked after the deadline branch on every flush, never gated by the cadence"
          },
          {
            "input": "flushPending at any cadence phase crossing the reduction ceiling",
            "state": "mid-cadence",
            "effect": "forced",
            "evidence": "core/builtin_budget.go:52-55 — chargeReductions runs first and returns the terminal ResourceLimitError before the deadline branch is reached"
          },
          {
            "input": "Flush with pending == 0",
            "state": "no-deadline",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:42-45 — an empty successful flush never enters flushPending, so it moves no cadence position and reads no clock"
          },
          {
            "input": "Step with pending < 128",
            "state": "mid-cadence",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:31-33 — local steps touch no shared state"
          },
          {
            "input": "Step or Flush after b.latched is set",
            "state": "latched",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:27-29, 40-42 — the latched error replays by reference and performs no further sync, so no clock read and no counter movement"
          }
        ],
        "forbidden": [
          "deadlineClockPolls at or below 0 without the next flush reading the clock (deadline starvation) — the predicate is `<= 0`, never `== 0`",
          "chargeReductions or b.ctx.Err() reachable only on the read-due phase",
          "a clock read while st.deadline.IsZero()",
          "a test writing st.deadlineClockPolls or st.deadline directly instead of going through WithEvalDeadline and Step/Flush",
          "t.Parallel in any test that assigns nowFunc — package core runs its budget tests in parallel (core/builtin_budget_test.go:41, 77, 91, 118, 133) and a parallel test mutating a package global races them under the floor's -race leg; core/vm/deadline_clock_cadence_test.go sets the precedent by using no t.Parallel at all",
          "widening evalState into a larger size class — it is 192 bytes and every Eval allocates one, so a wider struct moves B/op on every gold-set cell against allowances of 0-8",
          "a `== 0` read-due predicate: Load-then-Add is not atomic as a pair and concurrent flushes can drive the counter negative",
          "naming the unexported cadence constant or countdown field from a sealed test — spell the cadence as the literal 8"
        ],
        "seeding": [
          {
            "state": "no-deadline",
            "path": "budgetCtx(context.Background(), n) with no WithEvalDeadline call (core/builtin_budget_test.go:12-14)"
          },
          {
            "state": "read-due",
            "path": "a freshly built ctx from WithEvalDeadline(budgetCtx(...), t) — a fresh evalState has deadlineClockPolls at its zero value, which is read-due; never assign the field"
          },
          {
            "state": "mid-cadence",
            "path": "from read-due, drive exactly one synchronization: NewBuiltinWorkBudget(ctx), one Step, one Flush. Repeat to advance the phase. There is no other legal way to move the counter"
          },
          {
            "state": "latched",
            "path": "let a synchronization fail (ceiling crossed via budgetCtx with a low limit, or an expired deadline), then call Step/Flush again"
          },
          {
            "state": "controlled clock",
            "path": "assign nowFunc in the test body with t.Cleanup(func() { nowFunc = restore }), no t.Parallel (core/vm/deadline_clock_cadence_test.go:25-30)"
          }
        ],
        "identifiers": [
          "nowFunc",
          "deadlineClockCadence",
          "deadlineClockPolls",
          "NewBuiltinWorkBudget",
          "BuiltinWorkBudget",
          "Step",
          "Flush",
          "flushPending",
          "WithEvalDeadline",
          "WithEvalResourceLimits",
          "budgetCtx",
          "stepN",
          "errCode",
          "checkInterval",
          "IsTerminalEvalError",
          "CodeResourceLimit",
          "context.DeadlineExceeded",
          "context.Canceled"
        ],
        "numbers": [
          {
            "name": "deadlineClockCadence",
            "value": "8"
          },
          {
            "name": "synchronizations between wall-clock reads",
            "value": "8"
          },
          {
            "name": "local work units between wall-clock reads",
            "value": "1024 (8 x checkInterval 128, core/eval.go:291) — the same 1,024 the spec's `Context observed within the reduction budget` scenario already permits for cancellation observation, which is what the cadence is derived from"
          },
          {
            "name": "clock reads for n synchronizations under an armed deadline",
            "value": "(n + 7) / 8, integer division"
          },
          {
            "name": "synchronizations between a deadline passing and termination",
            "value": "at most 8"
          },
          {
            "name": "sub-test n values that discriminate reset-to-8 from reset-to-7",
            "value": "1, 8, 9, 16, 17, 37 (core/vm/deadline_clock_cadence_test.go:21 — 37 alone cannot)"
          },
          {
            "name": "unsafe.Sizeof(evalState) before and after",
            "value": "192 both — pack the counter as atomic.Int32 into the padding hole after `calleeCharged bool`, never append it"
          }
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestBuiltinWorkBudget_ShortCallsShareClockCadence"
      ],
      "redRun": "go test -timeout 2m ./core/ -run 'TestBuiltinWorkBudget_ShortCallsShareClockCadence'",
      "verify": "go test -timeout 2m ./core/ && go build ./... && go vet ./core/... && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "deadline-install-reset",
      "taskIds": [
        "1.2",
        "2.3"
      ],
      "prev": "flush-clock-cadence",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "deadline-install-reset",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core/"
      ],
      "sites": [
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "WithEvalDeadline",
          "anchor": "evalStateFrom(ctx).deadline = deadline",
          "change": "reset the countdown to due-now beside this assignment. This state may already be mid-countdown, so the reset is required here."
        },
        {
          "task": "2.3",
          "file": "core/eval.go",
          "symbol": "lazyEvalStateCtx.Value",
          "anchor": "st.deadline = deadline",
          "change": "this materializes a brand-new evalState whose zero value is already read-due, so no store is needed if and only if zero means due-now. State that convention in a comment; add no redundant assignment."
        },
        {
          "task": "1.2",
          "file": "core/builtin_budget_test.go",
          "symbol": "budgetCtx",
          "anchor": "func budgetCtx(",
          "change": "the red stage adds two tests here. Only TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync is red — it fails until 2.3 resets the countdown at WithEvalDeadline. TestBuiltinWorkBudget_FreshEvalStateReadsClockAtFirstSync is a companion pin that passes on authoring: the lazy site needs no store because a fresh evalState's zero value is already read-due, and the pin exists to keep that true. Expect exactly one failure from this stage. The lazy-site pin must seed through AdoptEvalStateWithMeter(context.Background(), deadline, 0, EvalMeterSnapshot{}) starting from a context that carries NO eval state: that call returns ctx unchanged when one is already attached (core/eval.go:425-431), and budgetCtx attaches one via ensureEvalState, so seeding through budgetCtx would never enter lazyEvalStateCtx.Value."
        }
      ],
      "contract": {
        "states": [
          "fresh-state",
          "armed-mid-cadence",
          "rearmed"
        ],
        "transitions": [
          {
            "input": "WithEvalDeadline on a ctx whose evalState is already mid-cadence",
            "state": "armed-mid-cadence",
            "effect": "forced",
            "evidence": "core/eval.go:401-405 — the deadline write is a plain field store on an existing state; the counter must be stored to 0 alongside it, matching SetDeadline/SetTimeout in core/vm/vm.go:335-347"
          },
          {
            "input": "lazyEvalStateCtx.Value materializing a state that carries a deadline",
            "state": "fresh-state",
            "effect": "no-op",
            "evidence": "core/eval.go:257-266 — the state comes from newEvalStateWithLimits, so deadlineClockPolls is already at its read-due zero value; storing 0 here would be dead work. This is a deliberate deviation from the literal wording of task 2.3 (`reset at both sites`): the obligation is satisfied at core/eval.go:266 by construction and is pinned by a test rather than by a store, exactly as core/vm/vm.go:684-685 documents for the Apply copy"
          },
          {
            "input": "RearmReentrantEvalState reusing a wrapper for a new run",
            "state": "rearmed",
            "effect": "forced",
            "evidence": "core/eval.go:557-559 — the rearm drops the materialized evalState, so the next materialization builds a fresh one and inherits nothing; no third install site exists"
          },
          {
            "input": "AdoptEvalStateWithMeter building a wrapper with an eager deadline",
            "state": "fresh-state",
            "effect": "no-op",
            "evidence": "core/eval.go:445 — the deadline goes on the wrapper, not on an evalState; the state it later materializes is fresh"
          }
        ],
        "forbidden": [
          "an evaluation observing a cadence position established before its deadline existed",
          "a synchronization skipping its clock read on the first flush after a deadline is installed",
          "relying on caller discipline (a documented `reset it yourself` rule) instead of resetting at the install site",
          "assigning the countdown field directly from a test — seed through the production install paths only",
          "t.Parallel in any test that swaps nowFunc — the seam is a package-level var, and every existing test in core/builtin_budget_test.go calls t.Parallel"
        ],
        "seeding": [
          {
            "state": "armed-mid-cadence",
            "path": "WithEvalDeadline(budgetCtx(...), far-future t) to arm, THEN flush exactly 3 times: the countdown only advances inside the armed branch, and the count must stay strictly between 1 and deadlineClockCadence-1 so the state is genuinely mid-cadence. Flushing a multiple of 8 returns it to read-due and the red test goes green for the wrong reason."
          },
          {
            "state": "fresh-state",
            "path": "AdoptEvalStateWithMeter(parent, deadline, 0, EvalMeterSnapshot{}) (core/eval.go:424) then NewBuiltinWorkBudget on the returned ctx — this is the only path that materializes the state through lazyEvalStateCtx.Value. budgetCtx does NOT reach it: WithEvalResourceLimits attaches a concrete state via ensureEvalState, so a test built that way would pass without touching the lazy site."
          },
          {
            "state": "rearmed",
            "path": "install a second deadline on an evaluation whose countdown has already advanced"
          },
          {
            "state": "controlled clock",
            "path": "save the package-level nowFunc, replace it with a counting or scripted stub, restore via t.Cleanup — the pattern at core/vm/deadline_clock_cadence_test.go:25-30"
          }
        ],
        "identifiers": [
          "WithEvalDeadline",
          "deadlineClockPolls",
          "deadlineClockCadence",
          "nowFunc",
          "evalStateFrom",
          "newEvalStateWithLimits",
          "lazyEvalStateCtx",
          "RearmReentrantEvalState",
          "budgetCtx"
        ],
        "numbers": [
          {
            "name": "synchronizations between installing a deadline and the next clock read",
            "value": "1 — the next one"
          },
          {
            "name": "cadence positions inherited across evaluations",
            "value": "0"
          }
        ]
      },
      "redTasks": [
        "1.2"
      ],
      "codeTasks": [
        "2.3"
      ],
      "redTests": [
        "TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync"
      ],
      "redRun": "go test -timeout 2m ./core/ -run 'TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync'",
      "verify": "go test -timeout 2m ./core/ && go build ./... && go vet ./core/... && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "decision-record",
      "taskIds": [
        "3.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "decision-record",
      "shard": "docs",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "CHANGELOG.md",
          "symbol": "[0.13.0] Changed",
          "anchor": "## [0.13.0] - 2026-09-05",
          "change": "add one entry under this section's `### Changed`, not under `[Unreleased]`: v0.13.0 is cut but untagged, so nothing has shipped under it and this change is part of what unblocks it — the same reasoning as commit e28f79e. Name the observable change (a Builtin observes an expired deadline within a bounded number of synchronizations rather than at the very next one), not the countdown that implements it."
        }
      ],
      "contract": {
        "states": [
          "entry-recorded"
        ],
        "transitions": [
          {
            "input": "an expired deadline during Builtin work",
            "state": "entry-recorded",
            "effect": "no-op",
            "evidence": "documentation only; the behavior is pinned by TestBuiltinWorkBudget_DeadlineCrossingBoundedBySynchronizations"
          }
        ],
        "forbidden": [
          "recording it under [Unreleased] — that section is empty and v0.13.0 is the unreleased version",
          "describing the countdown, the constant or evalState: the entry names what a consumer observes"
        ],
        "seeding": [
          {
            "state": "entry-recorded",
            "path": "no test seeds this; `go test ./cl/...` covers the changelog-head assertions that read this section"
          }
        ],
        "identifiers": [
          "CHANGELOG.md",
          "[0.13.0]",
          "### Changed"
        ],
        "numbers": [
          {
            "name": "synchronizations of deadline-observation lag the entry states",
            "value": "8"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./cl/",
      "coder": "coder"
    },
    {
      "id": "finish-member",
      "taskIds": [
        "2.4"
      ],
      "redRun": "",
      "verify": "go test -timeout 2m ./core/ && go vet ./core/... && golangci-lint run ./core/...",
      "prev": "deadline-install-reset",
      "parallel": false,
      "shard": "",
      "seam": "finish-member",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core/"
      ],
      "sites": [
        {
          "file": "core/builtin_budget.go",
          "symbol": "(*BuiltinWorkBudget).Finish (new)",
          "anchor": "func (b *BuiltinWorkBudget) Flush() error {",
          "change": "Task 2.4 adds the exported method adjacent to Flush with the existing selector semantics and no forced check, making new tests compile. Task 2.5 gives only its nonterminal-input path the forced settlement defined above. Add required concise API doc comment.",
          "new": true
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "2.4"
      ],
      "redTests": [],
      "coder": "go-coder",
      "sharedPkg": "core"
    },
    {
      "id": "finish-forced-error",
      "taskIds": [
        "1.3",
        "2.5"
      ],
      "redRun": "go test -timeout 2m ./core/ -run '^TestBuiltinWorkBudget_Finish'",
      "verify": "go test -timeout 2m ./core/ && go vet ./core/... && golangci-lint run ./core/...",
      "prev": "finish-member",
      "parallel": false,
      "shard": "",
      "seam": "finish-forced-error",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core/"
      ],
      "sites": [
        {
          "file": "core/builtin_budget.go",
          "symbol": "(*BuiltinWorkBudget).Finish (new)",
          "anchor": "func (b *BuiltinWorkBudget) Flush() error {",
          "change": "Task 2.4 adds the exported method adjacent to Flush with the existing selector semantics and no forced check, making new tests compile. Task 2.5 gives only its nonterminal-input path the forced settlement defined above. Add required concise API doc comment.",
          "new": true
        },
        {
          "file": "core/builtin_budget.go",
          "symbol": "(*BuiltinWorkBudget).flushPending",
          "anchor": "func (b *BuiltinWorkBudget) flushPending() error {",
          "change": "Task 2.5 adds forceDeadline bool and uses forceDeadline || b.st.deadlineClockPolls.Load() <= 0 inside the existing armed-deadline branch. Preserve reduction-before-deadline-before-context ordering, bare errors, successful-read reset to 7, pending drain once, and latched replay. Step and Flush explicitly pass false; Finish supplies true only at the error boundary."
        },
        {
          "file": "core/builtin_budget_test.go",
          "symbol": "new Finish contract tests",
          "anchor": "func TestBuiltinWorkBudget_ShortCallsShareClockCadence(t *testing.T) {",
          "change": "Task 1.3 adds six proposed tests listed in the core chunk, without altering existing tests or helper contracts. Seed through budgetCtx, WithEvalDeadline, NewBuiltinWorkBudget, Step, Flush and Finish; inspect accounting through EvalMeterFrom(ctx).Snapshot(). No direct countdown/deadline/pending/latched assignments. Clock-swapping tests and their subtests must not call t.Parallel.",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "no-deadline",
          "read-due",
          "mid-cadence",
          "pending-zero",
          "pending-nonzero",
          "input-nil",
          "input-nonterminal",
          "input-terminal",
          "latched-sync-error"
        ],
        "transitions": [
          {
            "input": "Finish(nil), any pending state",
            "state": "input-nil",
            "evidence": "Exactly ordinary Flush. Zero pending and no latch -> no shared work or clock/context access; nonzero pending -> existing cadence.",
            "effect": "no-op"
          },
          {
            "input": "Finish(nonterminal), no latch, pending nonzero",
            "state": "input-nonterminal",
            "evidence": "Charge and drain actual units once; force armed deadline read; then ctx.Err. Return terminal synchronization failure over supplied error, otherwise supplied error unchanged.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no latch, pending zero",
            "state": "input-nonterminal",
            "evidence": "Charge zero units; same forced deadline/context check; no synthetic Step and no forced reduction charge.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), unexpired armed deadline, any previous cadence",
            "state": "input-nonterminal",
            "evidence": "Exactly one clock read; reset cadence to 7. Ordinary nonempty synchronization number 8 after this reads again.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), expired armed deadline",
            "state": "input-nonterminal",
            "evidence": "Latch and return bare context.DeadlineExceeded; no subsequent ctx.Err check because deadline wins at this synchronization.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no engine deadline",
            "state": "input-nonterminal",
            "evidence": "No clock read or countdown movement. Caller cancellation still observed, including pending zero.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reduction ceiling crossed and deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Reduction error latches and wins; no clock read or context check after failed charging.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reductions allowed, deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Bare context.DeadlineExceeded wins; preserve existing within-sync order.",
            "effect": "forced"
          },
          {
            "input": "Finish(terminal), no latch, pending nonzero",
            "state": "input-terminal",
            "evidence": "Ordinary Flush charges pending units once; original supplied terminal is returned by identity even if Flush produces a different terminal failure. Flush's first synchronization failure still latches internally.",
            "effect": "no-op"
          },
          {
            "input": "Finish(terminal), no latch, pending zero",
            "state": "input-terminal",
            "evidence": "Ordinary empty Flush does nothing; preserve supplied error identity; no forced read.",
            "effect": "no-op"
          },
          {
            "input": "Any Finish input, already latched synchronization failure",
            "state": "latched-sync-error",
            "evidence": "No extra charge, clock read, context read or cadence movement. Apply existing error-selection rule, including retaining a supplied terminal error over a different latched terminal.",
            "effect": "no-op"
          },
          {
            "input": "Successful forced check returns supplied nonterminal error",
            "state": "input-nonterminal",
            "evidence": "Do not latch operation error. Repeated Finish cannot recharge drained pending units; Step and Flush remain usable under their existing rules.",
            "effect": "forced"
          }
        ],
        "forbidden": [
          "Forcing clock reads from Step, ordinary Flush, constructors, pre-callback sites or successful result returns.",
          "Changing terminal classification, wrapping the new deadline result, changing reduction/cancellation ordering, or overwriting the first synchronization-error latch.",
          "Flushing once ordinarily and then again forcibly for the same pending units; Finish must choose one settlement mode.",
          "Calling IsTerminalEvalError on nil on successful helper paths when the direct Flush path already suffices.",
          "Adding a per-budget field, widening evalState, allocating wrappers/interfaces/closures in settlement, resetting cadence on each budget construction.",
          "Editing existing sealed test bodies, reducing timeout/precedence assertions, weakening inventory guards, changing allocation pins or acceptance criteria.",
          "Expanding scope to VM/runtime deadline checkpoints or automatic GoFunc error wrapping."
        ],
        "seeding": [
          "Use budgetCtx and WithEvalDeadline, prime 1 through 7 nonempty Step/Flush synchronizations to exercise all mid-cadence positions; never assign the private counter.",
          "Swap nowFunc with a counting/scripted clock only in sequential tests and restore via t.Cleanup. Advance from before deadline to equality/after deadline after priming.",
          "Exercise pending zero using a fresh budget sharing the primed context or an already-drained budget; pending remainder via fewer than 128 Step calls.",
          "Create sync latches through actual reduction crossing, expired deadline or canceled context; do not assign b.latched.",
          "Read exact accounting through EvalMeterFrom(ctx).Snapshot().Reductions and AllocationBytes, verified at core/metering.go:83,145,154.",
          "Seed operation errors with existing core constructors or errors.New in tests; wrapped terminal inputs use fmt.Errorf with %w around context.Canceled/context.DeadlineExceeded or an actual ResourceLimitError."
        ],
        "budgets": {
          "ordinaryClockCadence": 8,
          "ordinaryReadCount": "(n + 7) / 8 for n nonempty synchronizations after deadline installation",
          "forcedReads": "One additional armed-deadline read per unlatched nonterminal error settlement that reaches the clock branch; zero when reduction charging fails or no deadline is armed.",
          "successfulForcedReadReset": 7,
          "syntheticReductions": 0,
          "pendingChargeMultiplicity": 1,
          "addedFields": 0,
          "addedSettlementAllocations": "Zero for standard precreated error inputs; custom As may require one shared lazy target cell, preserving stdlib semantics.",
          "evalStateBytes": 192,
          "allocationPin": "queue-promote 174, unchanged; all existing pins unchanged",
          "goldsetBytesDelta": "All 52 controlled cells: classifier-restored candidate equals base; final candidate equals control or removes only classifier target allocations; both single-P repetitions exact. Raw B/op diagnostic only.",
          "cpuAcceptance": "Exact counting-clock contract plus unchanged hosted gate; CPU profiles diagnostic only, approved cost-scope amendment."
        },
        "identifiers": [
          "BuiltinWorkBudget",
          "Finish",
          "Flush",
          "Step",
          "nowFunc",
          "context.DeadlineExceeded",
          "context.Canceled",
          "IsTerminalEvalError",
          "EvalMeterFrom",
          "Snapshot"
        ],
        "testCases": {
          "TestBuiltinWorkBudget_FinishForcesDeadlineAfterError": "Prime each phase 1..7 before expiry, then return nonterminal input at deadline equality and after deadline; cover zero and nonzero pending. Require exact bare DeadlineExceeded and exactly one added read.",
          "TestBuiltinWorkBudget_FinishPreservesSuccessfulCadence": "Finish(nil) across short fresh budgets retains exact ordinary read count and empty no-op. Existing ShortCallsShareClockCadence remains untouched.",
          "TestBuiltinWorkBudget_FinishPreservesErrorPrecedence": "Table nil/nonterminal/terminal input, no deadline/live/expired deadline, live/canceled parent and simultaneous reduction crossing. Preserve incoming terminal and nonterminal identities when selected; observe cancellation with zero pending and no deadline.",
          "TestBuiltinWorkBudget_FinishChargesPendingOnce": "Snapshot actual reductions/allocations before and after first/repeated settlement for nil/nonterminal/terminal inputs and zero/nonzero pending. Retrying with drained pending adds no reductions and no allocation charge.",
          "TestBuiltinWorkBudget_FinishReplaysLatchedError": "Induce terminal sync error through real public budget operations, then repeat Step/Flush/Finish with nil and ordinary errors; same latch by identity and no clock calls. A distinct terminal input still wins by identity without disturbing later latch replay.",
          "TestBuiltinWorkBudget_FinishResetsDeadlineCadence": "Prime mid-cadence, perform a successful forced check against future deadline with ordinary input, then drive 7 ordinary nonempty syncs without another read and observe next read on sync 8."
        }
      },
      "redTasks": [
        "1.3"
      ],
      "codeTasks": [
        "2.5"
      ],
      "redTests": [
        "TestBuiltinWorkBudget_FinishForcesDeadlineAfterError",
        "TestBuiltinWorkBudget_FinishPreservesErrorPrecedence",
        "TestBuiltinWorkBudget_FinishResetsDeadlineCadence"
      ],
      "coder": "go-coder",
      "sharedPkg": "core"
    },
    {
      "id": "finish-consumers",
      "taskIds": [
        "1.4",
        "2.6"
      ],
      "redRun": "go test -timeout 2m ./runtime/ -run '^TestCLAdapters_LateVMDeadline$/^deadline-wins-over-pending-type-error$' -count=1",
      "verify": "go test -timeout 2m ./runtime/ -run '^(TestCLAdapters_LateVMDeadline|TestStdlibFamilies_TerminalOutranksCallbackError)$' -count=1 && go test -timeout 2m ./internal/collections/ ./cl/ ./plugins/stdlib/",
      "prev": "finish-forced-error",
      "parallel": false,
      "shard": "",
      "seam": "finish-consumers",
      "pkgDirs": [
        "runtime",
        "internal/collections",
        "cl",
        "plugins/stdlib"
      ],
      "pkgs": [
        "./runtime/",
        "./internal/collections/",
        "./cl/",
        "./plugins/stdlib/"
      ],
      "sites": [
        {
          "file": "internal/collections/kernels.go",
          "symbol": "finishSort",
          "anchor": "func finishSort(b *core.BuiltinWorkBudget, sorted []core.Value, err error) ([]core.Value, error) {",
          "change": "For err != nil, return nil, b.Finish(err). For nil input, retain a direct b.Flush error check then return sorted, nil. Keep three return sites and existing callback scheduling unchanged."
        },
        {
          "file": "internal/collections/kernels.go",
          "symbol": "flushErr",
          "anchor": "func flushErr(b *core.BuiltinWorkBudget, err error) error {",
          "change": "For err != nil return b.Finish(err); otherwise return b.Flush(). Keep two return sites and a real direct Flush on the successful path."
        },
        {
          "file": "cl/charges.go",
          "symbol": "finishAdapter",
          "anchor": "func finishAdapter(b *core.BuiltinWorkBudget, v core.Value, err error) (core.Value, error) {",
          "change": "For err != nil return nil, b.Finish(err). For nil input, retain a direct b.Flush error check then return v, nil. Keep three return sites. No change to clSort's pre-kernel ordinary Flush."
        },
        {
          "file": "plugins/stdlib/charges.go",
          "symbol": "finishBuiltin",
          "anchor": "func finishBuiltin(b *core.BuiltinWorkBudget, v core.Value, err error) (core.Value, error) {",
          "change": "Same three-branch shape as finishAdapter: nonnil input uses Finish, nil input retains direct Flush and success return. Existing budget holders keep calling this helper."
        },
        {
          "file": "plugins/stdlib/collections.go",
          "symbol": "getInResult",
          "anchor": "func getInResult(ctx context.Context, budget *core.BuiltinWorkBudget, v core.Value, err error) (core.Value, error) {",
          "change": "For err != nil return nil, budget.Finish(err). For nil input retain direct budget.Flush check, then chargeBorrowedResult(ctx), then return v. Keep all four return sites and charge borrowed-result ownership only on success."
        }
      ],
      "contract": {
        "states": [
          "no-deadline",
          "read-due",
          "mid-cadence",
          "pending-zero",
          "pending-nonzero",
          "input-nil",
          "input-nonterminal",
          "input-terminal",
          "latched-sync-error"
        ],
        "transitions": [
          {
            "input": "Finish(nil), any pending state",
            "state": "input-nil",
            "evidence": "Exactly ordinary Flush. Zero pending and no latch -> no shared work or clock/context access; nonzero pending -> existing cadence.",
            "effect": "no-op"
          },
          {
            "input": "Finish(nonterminal), no latch, pending nonzero",
            "state": "input-nonterminal",
            "evidence": "Charge and drain actual units once; force armed deadline read; then ctx.Err. Return terminal synchronization failure over supplied error, otherwise supplied error unchanged.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no latch, pending zero",
            "state": "input-nonterminal",
            "evidence": "Charge zero units; same forced deadline/context check; no synthetic Step and no forced reduction charge.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), unexpired armed deadline, any previous cadence",
            "state": "input-nonterminal",
            "evidence": "Exactly one clock read; reset cadence to 7. Ordinary nonempty synchronization number 8 after this reads again.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), expired armed deadline",
            "state": "input-nonterminal",
            "evidence": "Latch and return bare context.DeadlineExceeded; no subsequent ctx.Err check because deadline wins at this synchronization.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no engine deadline",
            "state": "input-nonterminal",
            "evidence": "No clock read or countdown movement. Caller cancellation still observed, including pending zero.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reduction ceiling crossed and deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Reduction error latches and wins; no clock read or context check after failed charging.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reductions allowed, deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Bare context.DeadlineExceeded wins; preserve existing within-sync order.",
            "effect": "forced"
          },
          {
            "input": "Finish(terminal), no latch, pending nonzero",
            "state": "input-terminal",
            "evidence": "Ordinary Flush charges pending units once; original supplied terminal is returned by identity even if Flush produces a different terminal failure. Flush's first synchronization failure still latches internally.",
            "effect": "no-op"
          },
          {
            "input": "Finish(terminal), no latch, pending zero",
            "state": "input-terminal",
            "evidence": "Ordinary empty Flush does nothing; preserve supplied error identity; no forced read.",
            "effect": "no-op"
          },
          {
            "input": "Any Finish input, already latched synchronization failure",
            "state": "latched-sync-error",
            "evidence": "No extra charge, clock read, context read or cadence movement. Apply existing error-selection rule, including retaining a supplied terminal error over a different latched terminal.",
            "effect": "no-op"
          },
          {
            "input": "Successful forced check returns supplied nonterminal error",
            "state": "input-nonterminal",
            "evidence": "Do not latch operation error. Repeated Finish cannot recharge drained pending units; Step and Flush remain usable under their existing rules.",
            "effect": "forced"
          }
        ],
        "forbidden": [
          "Forcing clock reads from Step, ordinary Flush, constructors, pre-callback sites or successful result returns.",
          "Changing terminal classification, wrapping the new deadline result, changing reduction/cancellation ordering, or overwriting the first synchronization-error latch.",
          "Flushing once ordinarily and then again forcibly for the same pending units; Finish must choose one settlement mode.",
          "Calling IsTerminalEvalError on nil on successful helper paths when the direct Flush path already suffices.",
          "Adding a per-budget field, widening evalState, allocating wrappers/interfaces/closures in settlement, resetting cadence on each budget construction.",
          "Editing existing sealed test bodies, reducing timeout/precedence assertions, weakening inventory guards, changing allocation pins or acceptance criteria.",
          "Expanding scope to VM/runtime deadline checkpoints or automatic GoFunc error wrapping."
        ],
        "seeding": [
          "Use budgetCtx and WithEvalDeadline, prime 1 through 7 nonempty Step/Flush synchronizations to exercise all mid-cadence positions; never assign the private counter.",
          "Swap nowFunc with a counting/scripted clock only in sequential tests and restore via t.Cleanup. Advance from before deadline to equality/after deadline after priming.",
          "Exercise pending zero using a fresh budget sharing the primed context or an already-drained budget; pending remainder via fewer than 128 Step calls.",
          "Create sync latches through actual reduction crossing, expired deadline or canceled context; do not assign b.latched.",
          "Read exact accounting through EvalMeterFrom(ctx).Snapshot().Reductions and AllocationBytes, verified at core/metering.go:83,145,154.",
          "Seed operation errors with existing core constructors or errors.New in tests; wrapped terminal inputs use fmt.Errorf with %w around context.Canceled/context.DeadlineExceeded or an actual ResourceLimitError."
        ],
        "budgets": {
          "ordinaryClockCadence": 8,
          "ordinaryReadCount": "(n + 7) / 8 for n nonempty synchronizations after deadline installation",
          "forcedReads": "One additional armed-deadline read per unlatched nonterminal error settlement that reaches the clock branch; zero when reduction charging fails or no deadline is armed.",
          "successfulForcedReadReset": 7,
          "syntheticReductions": 0,
          "pendingChargeMultiplicity": 1,
          "addedFields": 0,
          "addedSettlementAllocations": "Zero for standard precreated error inputs; custom As may require one shared lazy target cell, preserving stdlib semantics.",
          "evalStateBytes": 192,
          "allocationPin": "queue-promote 174, unchanged; all existing pins unchanged",
          "goldsetBytesDelta": "All 52 controlled cells: classifier-restored candidate equals base; final candidate equals control or removes only classifier target allocations; both single-P repetitions exact. Raw B/op diagnostic only.",
          "cpuAcceptance": "Exact counting-clock contract plus unchanged hosted gate; CPU profiles diagnostic only, approved cost-scope amendment."
        },
        "identifiers": [
          "BuiltinWorkBudget",
          "Finish",
          "Flush",
          "Step",
          "nowFunc",
          "context.DeadlineExceeded",
          "context.Canceled",
          "IsTerminalEvalError",
          "EvalMeterFrom",
          "Snapshot"
        ],
        "testCases": {
          "TestBuiltinWorkBudget_FinishForcesDeadlineAfterError": "Prime each phase 1..7 before expiry, then return nonterminal input at deadline equality and after deadline; cover zero and nonzero pending. Require exact bare DeadlineExceeded and exactly one added read.",
          "TestBuiltinWorkBudget_FinishPreservesSuccessfulCadence": "Finish(nil) across short fresh budgets retains exact ordinary read count and empty no-op. Existing ShortCallsShareClockCadence remains untouched.",
          "TestBuiltinWorkBudget_FinishPreservesErrorPrecedence": "Table nil/nonterminal/terminal input, no deadline/live/expired deadline, live/canceled parent and simultaneous reduction crossing. Preserve incoming terminal and nonterminal identities when selected; observe cancellation with zero pending and no deadline.",
          "TestBuiltinWorkBudget_FinishChargesPendingOnce": "Snapshot actual reductions/allocations before and after first/repeated settlement for nil/nonterminal/terminal inputs and zero/nonzero pending. Retrying with drained pending adds no reductions and no allocation charge.",
          "TestBuiltinWorkBudget_FinishReplaysLatchedError": "Induce terminal sync error through real public budget operations, then repeat Step/Flush/Finish with nil and ordinary errors; same latch by identity and no clock calls. A distinct terminal input still wins by identity without disturbing later latch replay.",
          "TestBuiltinWorkBudget_FinishResetsDeadlineCadence": "Prime mid-cadence, perform a successful forced check against future deadline with ordinary input, then drive 7 ordinary nonempty syncs without another read and observe next read on sync 8."
        }
      },
      "redTasks": [
        "1.4"
      ],
      "codeTasks": [
        "2.6"
      ],
      "redTests": [
        "TestCLAdapters_LateVMDeadline"
      ],
      "coder": "go-coder",
      "sharedPkg": "core"
    },
    {
      "id": "finish-documentation",
      "taskIds": [
        "3.2"
      ],
      "redRun": "",
      "verify": "go test -timeout 2m ./cl/",
      "prev": null,
      "parallel": true,
      "shard": "finish-docs",
      "seam": "finish-documentation",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "file": "docs/adr/0011-reduction-and-allocation-metering.md",
          "symbol": "Builtin accounting model",
          "anchor": "- Builtin Go function (`GoFunc`):",
          "change": "Task 3.2, main-owned: qualify every-eight cadence as ordinary Step/Flush behavior; document Finish's forced deadline observation for pending nonterminal error, including no pending units. State existing terminal identity wins and no new reductions are invented. Keep successful-path bounded observation wording and existing 192-byte constraint."
        },
        {
          "file": "CHANGELOG.md",
          "symbol": "[0.13.0] Changed deadline entry",
          "anchor": "## [0.13.0] - 2026-09-05",
          "change": "Task 3.2, main-owned: retain existing eight-synchronization statement for continuing/successful work, add that final settlement checks an expired deadline before returning an ordinary builtin error. Do not move the entry or weaken CPU/byte acceptance."
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "3.2"
      ],
      "redTests": [],
      "coder": "coder"
    },
    {
      "id": "terminal-classifier-allocation",
      "taskIds": [
        "1.5",
        "2.7"
      ],
      "prev": "finish-consumers",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "terminal-classifier-allocation",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core/"
      ],
      "sites": [
        {
          "file": "core/error.go",
          "symbol": "IsTerminalEvalError",
          "anchor": "func IsTerminalEvalError(err error) bool {",
          "change": "Retain nil fast path and the existing two short-circuited errors.Is calls verbatim. Replace only the var lerr / errors.As block with a local lazy target slot, the private typed search and the same found-and-CodeResourceLimit decision. Do not add a nil guard that changes existing custom-As nil-result panic behavior.",
          "task": "2.7"
        },
        {
          "file": "core/error.go",
          "symbol": "asLispicoError (proposed new private function)",
          "anchor": "// NewReadError builds a LispicoError for a tokenizer/parser failure at the",
          "change": "Insert immediately before this unique existing comment. Concrete helper, no new named interface/type or generic API. Walk single Unwrap chains iteratively; recurse only through multi-error children. Allocate one **LispicoError slot only when custom As is reached; share that slot across siblings.",
          "new": true,
          "task": "2.7"
        },
        {
          "file": "core/error_test.go",
          "symbol": "new classifier contract tests",
          "anchor": "func TestIsTerminalEvalError(t *testing.T) {",
          "change": "Append new top-level tests and their narrow test-only fixtures after existing file contents; do not modify the anchored existing test. Tests: TestIsTerminalEvalError_StandardErrorsAllocateZero, TestIsTerminalEvalError_TraversalSemantics, TestIsTerminalEvalError_CustomHooks",
          "new": true,
          "task": "1.5"
        },
        {
          "file": "core/builtin_budget_test.go",
          "symbol": "new classifier contract tests",
          "anchor": "func TestBuiltinWorkBudget_FinishResetsDeadlineCadence(t *testing.T) {",
          "change": "Append two new top-level tests after the end of this currently-final function; do not modify its body. Tests: TestBuiltinWorkBudget_FinishStandardErrorsAllocateZero, TestBuiltinWorkBudget_FinishCustomErrorClassification",
          "new": true,
          "task": "1.5"
        }
      ],
      "contract": {
        "states": [
          "nil",
          "sentinel-match",
          "ordinary-leaf",
          "first-typed-ordinary",
          "first-typed-resource",
          "single-wrapper",
          "joined-tree",
          "custom-As-unseen",
          "custom-As-true",
          "custom-As-false",
          "custom-target-retained",
          "no-match",
          "Finish-empty",
          "Finish-pending",
          "Finish-latched",
          "typed-search"
        ],
        "transitions": [
          {
            "input": "nil",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Return false without typed search or allocation."
          },
          {
            "input": "Any errors.Is match",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Preserve current full canceled traversal followed, only if needed, by full deadline traversal. Match anywhere in the tree outranks first typed-error classification; do not invoke As afterward."
          },
          {
            "input": "Direct *LispicoError reached by typed search",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Stop at this first typed match even if Code is ordinary and a deeper/later resource error exists. If a custom target was already materialized, write this pointer into that same target."
          },
          {
            "input": "Unwrap() error",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Call once in the typed traversal at this node and continue iteratively; preserve prior errors.Is traversals unchanged."
          },
          {
            "input": "Unwrap() []error",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Call once in typed traversal, then visit children depth-first and left-to-right. Ignore nil children and stop on the first typed/custom match, not the first resource match."
          },
          {
            "input": "First encountered As(any) bool hook",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Allocate target cell once for this classification and call errors.As on that exact current subtree. No restart from root or extra Unwrap. Its hook invocation ordering and descendants are handled by stdlib."
          },
          {
            "input": "Custom subtree returns false",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Continue later siblings with the same target cell and any mutation retained in it. Ignore its pointer value as a match unless found is true."
          },
          {
            "input": "Custom subtree returns true",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Stop with target exactly as custom hook set it, whether resource or ordinary. Preserve the existing panic if custom As claims a nil typed match; do not silently reinterpret invalid custom behavior."
          },
          {
            "input": "Custom hook retains target, later direct match succeeds",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Retained target reflects the eventual direct match just as errors.As writes into its original target."
          },
          {
            "input": "Finish called with standard terminal input",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Existing ordinary Flush semantics and original input identity remain; no forced deadline read merely for terminal classification."
          },
          {
            "input": "Finish called with standard/custom nonterminal input",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Existing forced deadline/cancellation check remains even with zero pending; terminal synchronization result wins, accounting charges only real units once."
          },
          {
            "input": "Any latched budget",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "No new synchronization; Finish still preserves supplied terminal identity versus latch according to unchanged existing rules."
          }
        ],
        "forbidden": [
          "Changing the order, number or short-circuiting of the existing two errors.Is calls.",
          "Stopping at the first resource error instead of the first *LispicoError/custom As match.",
          "Flattening or breadth-first traversal of errors.Join or multi-%w trees.",
          "Restarting from the root after custom-hook discovery; custom Unwrap methods may be stateful.",
          "Separate lazy target cells for sibling branches, or discarding a false-returning custom As target mutation.",
          "Allocating a target before a custom As hook is reached; using a pool or mutable global scratch.",
          "Calling errors.AsType, raising go.mod's Go version, adding dependencies, a public helper, a generic error traversal framework or new production types.",
          "Guaranteeing zero allocations inside arbitrary custom Is, As or Unwrap methods.",
          "Weakening existing tests/pins, editing Finish/Step/Flush/cadence/reset paths, synthetic reductions or additional budget/state fields.",
          "Recovery, nil sanitization, traversal limits or cycle detection that changes existing error semantics."
        ],
        "seeding": [
          "Build all standard errors before allocation measurement: errors.New, NewTypeError, NewResourceLimitError, fmt.Errorf with single %w, fmt.Errorf with two %w, errors.Join, and nested LispicoError Cause fields. These constructors' own allocations are outside the measured classification/settlement.",
          "Use testing.AllocsPerRun(1000, closure), matching core/hashmap_test.go:76. Allocation tests and subtests stay sequential. Store/validate result without constructing errors, formatting messages or calling Fatal inside the measured success path.",
          "Standard rows: nil; plain ordinary; typed ordinary; wrapped plain; wrapped typed; bare/wrapped resource; bare/wrapped canceled and deadline; joined ordinary-only; joined ordinary typed before resource; joined resource before ordinary typed; nested ordinary typed containing resource; nested typed containing canceled/deadline. Record expected classification explicitly.",
          "For Finish allocation rows create context and budget before AllocsPerRun. Use budgetCtx with ceiling 1_000_000 and no deadline. Reuse the budget; call zero or three Step operations inside each measured invocation, then Finish. At most 3003 reductions for the 1001 calls including warm-up. Supplied operation errors must not latch.",
          "For latched Finish rows induce cancellation through a real context.WithCancel parent, one Step and Flush before measurement. Reuse the latched budget; assert return identity for ordinary versus supplied terminal inputs.",
          "Proposed test-only terminalErrorHook has optional Is/As callbacks and a single-error Unwrap callback so tests can record exact calls and target identity; terminalErrorList can supply a fixed []error containing nil to pin multi-error nil handling. No fixtures in production.",
          "Semantic fixtures use fresh state for each assertion. Compare explicit expected results and selected hook-call traces with direct errors.Is/errors.As behavior, not the proposed private helper.",
          "Finish custom-classification cases use budgetCtx, WithEvalDeadline, NewBuiltinWorkBudget, Step and Flush to prime exactly three nonempty synchronizations. Swap nowFunc sequentially with t.Cleanup restoration, advance clock to equality, then Finish with zero/three pending units. Terminal inputs return original identity with zero clock reads; nonterminal inputs return bare context.DeadlineExceeded with exactly one read. Never assign private cadence/pending/latch fields."
        ],
        "semanticCases": [
          "errors.Join(typeError, resource) is nonterminal; reversed order is terminal.",
          "Outer ordinary LispicoError with resource Cause remains nonterminal; outer resource with ordinary Cause is terminal.",
          "A canceled/deadline descendant still makes an outer ordinary typed error terminal because errors.Is runs before typed search.",
          "Nested joins establish depth-first order; multi-%w wrappers follow their Unwrap() []error order; nil children are skipped.",
          "Custom As true with resource target is terminal; true with ordinary target masks deeper/later resource; false allows descendants and later siblings.",
          "Custom As false can mutate target to resource yet no eventual match must still classify false.",
          "Custom As false in an earlier sibling and a later custom As must receive the same target pointer and its prior value.",
          "Custom As can retain its target pointer while returning false; a later direct typed match must update that retained cell.",
          "Custom Is matching canceled short-circuits deadline traversal and As; matching only deadline runs canceled pass first then short-circuits As.",
          "Custom Unwrap call traces remain those of the original two sentinel searches and one typed search; no preflight traversal.",
          "Custom As true with nil target preserves baseline panic, using a fixture whose sentinel traversals themselves do not panic.",
          "Custom terminal/nonterminal classification feeds unchanged Finish identity and forced-observation behavior with both empty and pending budgets."
        ],
        "numericBudgets": {
          "standardClassifierAllocsPerCall": 0,
          "standardFinishAllocsPerCall": 0,
          "measuredCalls": 1000,
          "allAllocsPerRunCallsIncludingWarmup": 1001,
          "customAsTargetCellsPerTypedSearchAtMost": 1,
          "customHookAllocationGuarantee": "None for arbitrary custom method implementations; at most one explicit lazy target cell is allocated by the typed search. Zero applies when no custom As is reached and traversed methods themselves allocate nothing.",
          "productionFilesChanged": 1,
          "newPrivateFunctions": 1,
          "newNamedProductionTypes": 0,
          "dependencyChanges": 0,
          "minimumGo": "1.24.0",
          "ordinaryDeadlineCadence": 8,
          "successfulDeadlineReset": 7,
          "addedBudgetOrEvalStateFields": 0,
          "evalStateBytes": 192,
          "syntheticReductions": 0
        },
        "implementation": {
          "changedFiles": [
            "core/error.go"
          ],
          "unchanged": [
            "core/builtin_budget.go",
            "core/eval.go",
            "all five Finish consumers",
            "go.mod",
            "go.sum",
            "all existing test bodies and allocation pins"
          ],
          "sites": [
            {
              "file": "core/error.go",
              "symbol": "IsTerminalEvalError",
              "anchor": "func IsTerminalEvalError(err error) bool {",
              "change": "Retain nil fast path and the existing two short-circuited errors.Is calls verbatim. Replace only the var lerr / errors.As block with a local lazy target slot, the private typed search and the same found-and-CodeResourceLimit decision. Do not add a nil guard that changes existing custom-As nil-result panic behavior."
            },
            {
              "file": "core/error.go",
              "symbol": "asLispicoError (proposed new private function)",
              "anchor": "// NewReadError builds a LispicoError for a tokenizer/parser failure at the",
              "change": "Insert immediately before this unique existing comment. Concrete helper, no new named interface/type or generic API. Walk single Unwrap chains iteratively; recurse only through multi-error children. Allocate one **LispicoError slot only when custom As is reached; share that slot across siblings."
            }
          ],
          "sketch": "func IsTerminalEvalError(err error) bool {\n\tif err == nil {\n\t\treturn false\n\t}\n\tif errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {\n\t\treturn true\n\t}\n\tvar target **LispicoError\n\tlerr, ok := asLispicoError(err, &target)\n\treturn ok && lerr.Code == CodeResourceLimit\n}\n\nfunc asLispicoError(err error, target ***LispicoError) (*LispicoError, bool) {\n\tfor err != nil {\n\t\tif lerr, ok := err.(*LispicoError); ok {\n\t\t\tif *target != nil {\n\t\t\t\t**target = lerr\n\t\t\t}\n\t\t\treturn lerr, true\n\t\t}\n\t\tif _, ok := err.(interface{ As(any) bool }); ok {\n\t\t\tif *target == nil {\n\t\t\t\t*target = new(*LispicoError)\n\t\t\t}\n\t\t\tok := errors.As(err, *target)\n\t\t\treturn **target, ok\n\t\t}\n\t\tswitch x := err.(type) {\n\t\tcase interface{ Unwrap() error }:\n\t\t\terr = x.Unwrap()\n\t\tcase interface{ Unwrap() []error }:\n\t\t\tfor _, child := range x.Unwrap() {\n\t\t\t\tif lerr, ok := asLispicoError(child, target); ok {\n\t\t\t\t\treturn lerr, true\n\t\t\t\t}\n\t\t\t}\n\t\t\treturn nil, false\n\t\tdefault:\n\t\t\treturn nil, false\n\t\t}\n\t}\n\treturn nil, false\n}",
          "invariant": "The pointer-to-pointer-to-pointer is the address of a stack-local lazy target slot, not global storage. Only the new(*LispicoError) cell enters arbitrary custom As code. If a previous custom As retained or modified that cell before returning false, every later sibling sees the same cell; a later direct typed match must also update it before returning. An unmatched subtree may leave a nonnil cell value, but only the explicit found boolean decides whether a match exists.",
          "comments": "Keep existing comments. If the shared-target update needs explanation, allow one concise WHY/invariant comment explaining preservation of a target retained by a prior custom As; no traversal narration."
        },
        "testCases": [
          "errors.Join(typeError, resource) is nonterminal; reversed order is terminal.",
          "Outer ordinary LispicoError with resource Cause remains nonterminal; outer resource with ordinary Cause is terminal.",
          "A canceled/deadline descendant still makes an outer ordinary typed error terminal because errors.Is runs before typed search.",
          "Nested joins establish depth-first order; multi-%w wrappers follow their Unwrap() []error order; nil children are skipped.",
          "Custom As true with resource target is terminal; true with ordinary target masks deeper/later resource; false allows descendants and later siblings.",
          "Custom As false can mutate target to resource yet no eventual match must still classify false.",
          "Custom As false in an earlier sibling and a later custom As must receive the same target pointer and its prior value.",
          "Custom As can retain its target pointer while returning false; a later direct typed match must update that retained cell.",
          "Custom Is matching canceled short-circuits deadline traversal and As; matching only deadline runs canceled pass first then short-circuits As.",
          "Custom Unwrap call traces remain those of the original two sentinel searches and one typed search; no preflight traversal.",
          "Custom As true with nil target preserves baseline panic, using a fixture whose sentinel traversals themselves do not panic.",
          "Custom terminal/nonterminal classification feeds unchanged Finish identity and forced-observation behavior with both empty and pending budgets."
        ],
        "companionRun": "go test -timeout 2m ./core/ -run '^(TestIsTerminalEvalError_(TraversalSemantics|CustomHooks)|TestBuiltinWorkBudget_FinishCustomErrorClassification)$'"
      },
      "redTasks": [
        "1.5"
      ],
      "codeTasks": [
        "2.7"
      ],
      "redTests": [
        "TestIsTerminalEvalError_StandardErrorsAllocateZero",
        "TestBuiltinWorkBudget_FinishStandardErrorsAllocateZero"
      ],
      "redRun": "go test -timeout 2m ./core/ -run '^(TestIsTerminalEvalError_StandardErrorsAllocateZero|TestBuiltinWorkBudget_FinishStandardErrorsAllocateZero)$'",
      "verify": "go test -timeout 2m ./core/ && go build ./core/... && go vet ./core/... && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "cost-verification",
      "taskIds": [
        "4.1",
        "4.3"
      ],
      "prev": "terminal-classifier-allocation",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "cost-and-suite-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "4.1",
          "file": "internal/goldset/bench_test.go",
          "symbol": "BenchmarkGoldset",
          "anchor": "func BenchmarkGoldset(",
          "change": "Verify only: retain existing profiles as diagnostics and prove exact clock reads using unchanged counting-clock tests. No local CPU-percentage gate."
        },
        {
          "task": "4.3",
          "file": "internal/goldset/alloc_test.go",
          "symbol": "vmAllocCeilings",
          "anchor": "\"queue-promote\":   174,",
          "change": "verify only, no edit — this change moves no allocation, so every pin must still hold unchanged."
        },
        {
          "task": "4.3",
          "file": "internal/goldset/bench_test.go",
          "symbol": "BenchmarkGoldset",
          "anchor": "func BenchmarkGoldset(",
          "change": "Verify only: compare all52 controlled byte/count/size-class totals under the amended single-P protocol, plus unchanged allocation pins. Raw B/op is diagnostic only."
        }
      ],
      "contract": {
        "states": [
          "clock-proved",
          "allocations-proved",
          "suite-green"
        ],
        "transitions": [],
        "forbidden": [
          "Changing eight-sync cadence or existing semantic tests.",
          "Editing allocation pins, fixture inputs, hosted workflow or perfgate tiers.",
          "Treating sampled CPU share, rounded raw B/op or local latency as acceptance.",
          "Ignoring unexplained controlled allocation differences or repeating until a favorable pair appears."
        ],
        "seeding": [
          {
            "cadence": 8,
            "successful_sync_reads": "For N positive successful unforced synchronizations on one freshly armed state, reads=(N+7)/8 using integer division. Existing N={1,8,9,16,17,37} must yield {1,1,2,2,3,5}. Budget-object replacement must not reset this sequence.",
            "first_read": "Exactly 1 read on the next real synchronization after deadline installation or fresh-state materialization.",
            "ordinary_error": "Exactly 1 forced clock read for ordinary-error Finish with an armed live/expired deadline, even with pending=0, unless a synchronization failure is already latched. A successful forced read resets cadence: next 7 unforced synchronizations read 0 times; the eighth reads 1 time.",
            "no_work_or_deadline": "0 clock reads for unarmed deadlines, successful empty Flush, and latched-error replay.",
            "observation": "Expired deadline detected within 8 synchronizations; cancellation and reduction checks remain unconditional at every synchronization. Existing error identity, priority and exactly-once reduction settlement tests must pass unchanged.",
            "status": "All named deterministic proofs must pass on the final corrected tree; no substitute percentage or latency threshold."
          },
          {
            "coverage": "13 Eval plus 13 Parse fixtures in each of eval and vm modes: all 52 cells, no omissions or selected-cell substitution.",
            "settings": "Same Go version/build settings/GOOS/GOARCH for base and candidate; GOMAXPROCS=1; GOGC=off; normal lazy registration; sequential cells; 32768 warmup calls and exactly 10000 measured calls. Explicit GC occurs before warmup only. Both context and engine construction precede measurement. No formatting, JSON encoding or engine Close within a measured window.",
            "repetitions": "Exactly 2 predetermined fresh-process repetitions of all 6 variant/mode combinations: source base, final candidate with source-base core/error.go restored only by overlay, and final candidate; each under eval and vm. Compare every cell in each triple, and require every variant/mode to repeat identically. Do not keep retrying until a favorable triple appears.",
            "valid_window": "Each measured window must have num_gc=0, vm_pool_misses=0, reader_pool_misses=0, type_assert_cache_builds=0 and interface_switch_cache_builds=0. Counter snapshots bracket both ReadMemStats calls. A nonzero counter invalidates the window; retain evidence and diagnose it.",
            "totals": "For every cell in both repetitions, rollback-control and source-base TotalAlloc, Mallocs and all size-class allocation counts must match exactly. Final candidate must either equal control exactly or have strictly fewer Mallocs, with TotalAlloc reduction equal to removed malloc count multiplied by the allocator class containing one *LispicoError pointer slot. Only that class may decrease; every other class must be unchanged. Thus every positive total or class-count delta is rejected, and all accepted reductions have an exact removed-target allocation signature. Each variant/mode must also reproduce its own exact totals and histogram in the second repetition.",
            "causal_control": "Build control from the final candidate with only core/error.go replaced by git show 39fe049115925fa1ec3e262eae0239c50e5d3b0c:core/error.go; all other source and instrumentation stays identical. Source review must confirm the candidate's changes in that file are restricted to terminal-error classification and its helpers. If control fails to compile, fails semantics, or differs from base in any cell, attribution fails and the task stays open. Do not add compensating overlays or subtract that discrepancy. A passing numerical checker still requires this causal source review.",
            "no_adjustment": "No subtraction of unexplained allocations, no per-fixture tolerance, no altered fixture/pin, no forced pooling. The checker rejects every positive or unexplained delta, including one allocation. A negative delta is accepted only through the uniform rollback-control, target-size and reproducibility checks; no fixture names are specialcased.",
            "unresolved_noise": "Any remaining mismatch or invalid window leaves revised 4.3 open. Use recorded size-class deltas and both pool counters to identify the responsible site before choosing a further bounded diagnostic. A later methodology change requires a reviewed rationale; unexplained deltas are never accepted by default."
          }
        ],
        "clock": {
          "replacement": "Prove the clock schedule with the existing counting-clock tests; retain CPU profiles only as diagnostics.",
          "acceptance": {
            "cadence": 8,
            "successful_sync_reads": "For N positive successful unforced synchronizations on one freshly armed state, reads=(N+7)/8 using integer division. Existing N={1,8,9,16,17,37} must yield {1,1,2,2,3,5}. Budget-object replacement must not reset this sequence.",
            "first_read": "Exactly 1 read on the next real synchronization after deadline installation or fresh-state materialization.",
            "ordinary_error": "Exactly 1 forced clock read for ordinary-error Finish with an armed live/expired deadline, even with pending=0, unless a synchronization failure is already latched. A successful forced read resets cadence: next 7 unforced synchronizations read 0 times; the eighth reads 1 time.",
            "no_work_or_deadline": "0 clock reads for unarmed deadlines, successful empty Flush, and latched-error replay.",
            "observation": "Expired deadline detected within 8 synchronizations; cancellation and reduction checks remain unconditional at every synchronization. Existing error identity, priority and exactly-once reduction settlement tests must pass unchanged.",
            "status": "All named deterministic proofs must pass on the final corrected tree; no substitute percentage or latency threshold."
          },
          "evidence": [
            "core/builtin_budget_test.go:74",
            "core/builtin_budget_test.go:102",
            "core/builtin_budget_test.go:140",
            "core/builtin_budget_test.go:169",
            "core/builtin_budget_test.go:419",
            "core/builtin_budget_test.go:709",
            "core/eval.go:296"
          ],
          "command_cwd": "/home/zhuk/Projects/own/go-lispico/.worktrees/zapply-builtin-budget-clock-cadence",
          "command": "env GOMAXPROCS=2 timeout 2m go test -p 2 -parallel 2 -timeout 2m ./core/ -run '^TestBuiltinWorkBudget_'",
          "profile_disposition": "Retain /tmp/builtin-budget-clock-cadence-amended-pprof.txt (2.48% runtimeNow) and the earlier profiles as diagnostic history. The empty short-profile output remains inconclusive. No further local profile is needed to close revised 4.1; no local latency A/B verdict. The prior <=1% claim did fail and is replaced by the newly approved deterministic proof, not reinterpreted as having passed."
        },
        "allocations": {
          "replacement": "Verify allocation layout, existing pin floor, all 52 controlled fixture cells and zero added settlement allocations. Permit only reproducible reductions isolated by the classifier rollback control and matching the removed target-allocation size; never positive or unexplained movement.",
          "layout": "unsafe.Sizeof(evalState{}) must be exactly 192 bytes in both base and final candidate on the same linux/amd64 toolchain. The improved probe reports this directly; no field widening or minimum-toolchain bump.",
          "vm_pins": {
            "counter-closure": 56,
            "guard-nil": 30,
            "kw-lookup": 31,
            "loop-sum": 87,
            "merge-config": 58,
            "pipeline": 71,
            "queue-promote": 174,
            "registry-fold": 69,
            "route-decision": 48,
            "rule-load": 164,
            "safe-parse": 71,
            "text-render": 42,
            "twice-macro": 43
          },
          "pin_evidence": "internal/goldset/alloc_test.go:25; no pin edits permitted by this task. The existing test compares exact counts, not only ceilings (line 86). If a verified shared-classifier reduction changes a pinned VM count, retain that floor failure and request a separate pin-policy decision; do not silently weaken the assertion or invent a replacement value.",
          "pin_command": "env GOMAXPROCS=2 timeout 2m go test -p 2 -parallel 2 -timeout 2m ./internal/goldset/ -run '^TestGoldsetVMAllocations$'",
          "controlled_fixture_acceptance": {
            "coverage": "13 Eval plus 13 Parse fixtures in each of eval and vm modes: all 52 cells, no omissions or selected-cell substitution.",
            "settings": "Same Go version/build settings/GOOS/GOARCH for base and candidate; GOMAXPROCS=1; GOGC=off; normal lazy registration; sequential cells; 32768 warmup calls and exactly 10000 measured calls. Explicit GC occurs before warmup only. Both context and engine construction precede measurement. No formatting, JSON encoding or engine Close within a measured window.",
            "repetitions": "Exactly 2 predetermined fresh-process repetitions of all 6 variant/mode combinations: source base, final candidate with source-base core/error.go restored only by overlay, and final candidate; each under eval and vm. Compare every cell in each triple, and require every variant/mode to repeat identically. Do not keep retrying until a favorable triple appears.",
            "valid_window": "Each measured window must have num_gc=0, vm_pool_misses=0, reader_pool_misses=0, type_assert_cache_builds=0 and interface_switch_cache_builds=0. Counter snapshots bracket both ReadMemStats calls. A nonzero counter invalidates the window; retain evidence and diagnose it.",
            "totals": "For every cell in both repetitions, rollback-control and source-base TotalAlloc, Mallocs and all size-class allocation counts must match exactly. Final candidate must either equal control exactly or have strictly fewer Mallocs, with TotalAlloc reduction equal to removed malloc count multiplied by the allocator class containing one *LispicoError pointer slot. Only that class may decrease; every other class must be unchanged. Thus every positive total or class-count delta is rejected, and all accepted reductions have an exact removed-target allocation signature. Each variant/mode must also reproduce its own exact totals and histogram in the second repetition.",
            "causal_control": "Build control from the final candidate with only core/error.go replaced by git show 39fe049115925fa1ec3e262eae0239c50e5d3b0c:core/error.go; all other source and instrumentation stays identical. Source review must confirm the candidate's changes in that file are restricted to terminal-error classification and its helpers. If control fails to compile, fails semantics, or differs from base in any cell, attribution fails and the task stays open. Do not add compensating overlays or subtract that discrepancy. A passing numerical checker still requires this causal source review.",
            "no_adjustment": "No subtraction of unexplained allocations, no per-fixture tolerance, no altered fixture/pin, no forced pooling. The checker rejects every positive or unexplained delta, including one allocation. A negative delta is accepted only through the uniform rollback-control, target-size and reproducibility checks; no fixture names are specialcased.",
            "unresolved_noise": "Any remaining mismatch or invalid window leaves revised 4.3 open. Use recorded size-class deltas and both pool counters to identify the responsible site before choosing a further bounded diagnostic. A later methodology change requires a reviewed rationale; unexplained deltas are never accepted by default."
          },
          "settlement_acceptance": {
            "existing_probe": "/tmp/builtin-budget-clock-cadence-finish-allocation-probe-results.json",
            "values": "On the final settlement fix, added_allocs=Finish-old must be <=0 for all 27 existing precreated-input/control rows. Empty/pending ordinary, typed/wrapped ordinary, direct/wrapped resource-limit inputs must have Finish=0 where old=0. Nil and direct context-terminal controls remain 0. Error identities and precedence remain unchanged.",
            "custom_hooks": "Keep user As/Is/Unwrap hooks behaviorally compatible. Distinguish allocations executed inside a user hook from package-owned traversal/target allocation using precreated inputs, allocation-free hook controls and direct-hook controls with recorded invocation counts. Arbitrary user-hook allocations are not assigned a new package cost limit. A library-created errors.As target escaping through a hook is still package-owned overhead and cannot be relabeled as user allocation. This extension belongs to the separate settlement/classifier work; its concrete tests and attribution must be reviewed there. The classifier contract explicitly permits at most one package-owned lazy target cell when a custom As is reached; this is not charged to user code or covered by the standard-error zero target.",
            "minimum": "Retain declared Go 1.24.0 support. Record actual compiler in allocation evidence; do not use a newer error API to raise the minimum."
          }
        },
        "probe": {
          "setup_script": "/tmp/builtin-budget-clock-cadence-cached-probe-setup.py",
          "setup_command": "python3 /tmp/builtin-budget-clock-cadence-cached-probe-setup.py",
          "template": "/tmp/builtin-budget-clock-cadence-allocation-probe/candidate/allocation_probe_test.go",
          "changes": "Retain identical source-base/control/final instrumentation. Uniform warm32768, measure10000. Shared temporary runtime/iface.go overlay adds only two atomic builder counters, increments immediately before the existing mallocgc calls, and an allocation-free snapshot accessor. Preserve sampled cache updates and CAS. Record warmup and measured snapshots; require zero measured builds. Manifests pin all inputs; runner refuses previous execution or result files.",
          "commands_file_after_setup": "/tmp/builtin-budget-clock-cadence-cached-probe/commands.json",
          "example_exact_execution_cwd": "/home/zhuk/Projects/own/go-lispico/.worktrees/zapply-builtin-budget-clock-cadence",
          "example_exact_execution_command": "env GOMAXPROCS=1 GOGC=off GOLDSET_LAZY= GOLDSET_MODE=vm ALLOCATION_PROBE_OUTPUT=/tmp/builtin-budget-clock-cadence-cached-probe/candidate-vm-1.json timeout 2m go test -p 1 -parallel 1 -timeout 2m -count=1 -v -overlay /tmp/builtin-budget-clock-cadence-cached-probe/candidate/overlay.json ./internal/goldset/ -run '^TestAllocationProbe$'",
          "run_order": "Execute generated twelve commands sequentially in their recorded cwd. Each repetition runs base VM/control VM/candidate VM, then base eval/control eval/candidate eval. Each command has a 2m process timeout, 2m Go timeout and worker cap 1. Regenerate overlays after the final implementation change.",
          "comparison_command": "python3 /tmp/builtin-budget-clock-cadence-cached-probe-setup.py --compare",
          "comparison_result": "/tmp/builtin-budget-clock-cadence-cached-probe/comparison.json",
          "comparison_exit": "0 only when every coverage, environment, window, base-control equality, candidate-control equality-or-target-reduction, and repetition check passes; 1 on any recorded gap. Missing/invalid inputs also fail instead of producing a pass. Source attribution review remains explicitly required even when these numerical checks pass.",
          "wrapper_reason": "No project target supports temporary Go overlays or these filtered diagnostic tests; bounded raw go test commands are used. GOCACHE and GOTMPDIR remain unchanged.",
          "status": "Prepared and source-reviewed calibration; execution evidence recorded separately.",
          "runner": "/tmp/builtin-budget-clock-cadence-run-cached-probe.py",
          "reviewed_setup_sha256": "450fe638a7695c96e2d67440922acdd19dbf27af4019400e00e9162bd2b6d431",
          "calibration_rationale": "Existing allocation profile confirms the runtime cache cause at errors.is and asLispicoError caller lines. Five measured cache allocations total352bytes and exactly account for the diagnostic window excess; one64byte table was allocated during warmup. Profile serialization intentionally hides leading runtime frames.",
          "history": "Original warm100 protocol and failed comparison retained unchanged. No subtraction or numerical acceptance change."
        },
        "numbers": [
          {
            "name": "clock cadence",
            "value": "8"
          },
          {
            "name": "evalState bytes",
            "value": "192"
          },
          {
            "name": "controlled fixture cells",
            "value": "52"
          },
          {
            "name": "fixed repetitions",
            "value": "2"
          },
          {
            "name": "warmup calls",
            "value": "32768"
          },
          {
            "name": "measured calls",
            "value": "10000"
          },
          {
            "name": "base versus classifier-restored control delta",
            "value": "0"
          },
          {
            "name": "final candidate delta",
            "value": "0 or exact removed classifier target allocations; no positive or unexplained delta"
          },
          {
            "name": "GC, both pool misses and both cache builders",
            "value": "0"
          }
        ],
        "measurementCalibration": {
          "scope": "Setup refinement within the approved deterministic proof; exact acceptance floor unchanged.",
          "review": "/tmp/builtin-budget-clock-cadence-cache-calibration-review.json",
          "manifest_sha256": "450fe638a7695c96e2d67440922acdd19dbf27af4019400e00e9162bd2b6d431",
          "rationale": "Existing allocation profile confirms the runtime cache cause at errors.is and asLispicoError caller lines. Five measured cache allocations total352bytes and exactly account for the diagnostic window excess; one64byte table was allocated during warmup. Profile serialization intentionally hides leading runtime frames."
        }
      },
      "redTasks": [],
      "codeTasks": [
        "4.1",
        "4.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m ./core/ -run '^TestBuiltinWorkBudget_' && go test -timeout 2m ./internal/goldset/ && python3 /tmp/builtin-budget-clock-cadence-cached-probe-setup.py --compare",
      "coder": "coder"
    },
    {
      "id": "suite-verification",
      "taskIds": [
        "4.2"
      ],
      "prev": "cost-verification",
      "sharedPkg": "core",
      "parallel": false,
      "seam": "cost-and-suite-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "4.2",
          "file": "Makefile",
          "symbol": "test / lint targets",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "verify only, no edit — `make test` and `make lint` are the wrappers. There is no race target: the race suite over core, plugins and runtime is a raw run and carries its own -timeout."
        }
      ],
      "contract": {
        "states": [
          "clock-proved",
          "allocations-proved",
          "suite-green"
        ],
        "transitions": [],
        "forbidden": [
          "Changing eight-sync cadence or existing semantic tests.",
          "Editing allocation pins, fixture inputs, hosted workflow or perfgate tiers.",
          "Treating sampled CPU share, rounded raw B/op or local latency as acceptance.",
          "Ignoring unexplained controlled allocation differences or repeating until a favorable pair appears."
        ],
        "seeding": [
          {
            "cadence": 8,
            "successful_sync_reads": "For N positive successful unforced synchronizations on one freshly armed state, reads=(N+7)/8 using integer division. Existing N={1,8,9,16,17,37} must yield {1,1,2,2,3,5}. Budget-object replacement must not reset this sequence.",
            "first_read": "Exactly 1 read on the next real synchronization after deadline installation or fresh-state materialization.",
            "ordinary_error": "Exactly 1 forced clock read for ordinary-error Finish with an armed live/expired deadline, even with pending=0, unless a synchronization failure is already latched. A successful forced read resets cadence: next 7 unforced synchronizations read 0 times; the eighth reads 1 time.",
            "no_work_or_deadline": "0 clock reads for unarmed deadlines, successful empty Flush, and latched-error replay.",
            "observation": "Expired deadline detected within 8 synchronizations; cancellation and reduction checks remain unconditional at every synchronization. Existing error identity, priority and exactly-once reduction settlement tests must pass unchanged.",
            "status": "All named deterministic proofs must pass on the final corrected tree; no substitute percentage or latency threshold."
          },
          {
            "coverage": "13 Eval plus 13 Parse fixtures in each of eval and vm modes: all 52 cells, no omissions or selected-cell substitution.",
            "settings": "Same Go version/build settings/GOOS/GOARCH for base and candidate; GOMAXPROCS=1; GOGC=off; normal lazy registration; sequential cells; 100 warmup calls and exactly 10000 measured calls. Explicit GC occurs before warmup only. Both context and engine construction precede measurement. No formatting, JSON encoding or engine Close within a measured window.",
            "repetitions": "Exactly 2 predetermined fresh-process repetitions of all 6 variant/mode combinations: source base, final candidate with source-base core/error.go restored only by overlay, and final candidate; each under eval and vm. Compare every cell in each triple, and require every variant/mode to repeat identically. Do not keep retrying until a favorable triple appears.",
            "valid_window": "Each measured window must have num_gc=0, vm_pool_misses=0 and reader_pool_misses=0. A nonzero counter invalidates the attempted deterministic proof; retain its evidence and diagnose it.",
            "totals": "For every cell in both repetitions, rollback-control and source-base TotalAlloc, Mallocs and all size-class allocation counts must match exactly. Final candidate must either equal control exactly or have strictly fewer Mallocs, with TotalAlloc reduction equal to removed malloc count multiplied by the allocator class containing one *LispicoError pointer slot. Only that class may decrease; every other class must be unchanged. Thus every positive total or class-count delta is rejected, and all accepted reductions have an exact removed-target allocation signature. Each variant/mode must also reproduce its own exact totals and histogram in the second repetition.",
            "causal_control": "Build control from the final candidate with only core/error.go replaced by git show 39fe049115925fa1ec3e262eae0239c50e5d3b0c:core/error.go; all other source and instrumentation stays identical. Source review must confirm the candidate's changes in that file are restricted to terminal-error classification and its helpers. If control fails to compile, fails semantics, or differs from base in any cell, attribution fails and the task stays open. Do not add compensating overlays or subtract that discrepancy. A passing numerical checker still requires this causal source review.",
            "no_adjustment": "No subtraction of unexplained allocations, no per-fixture tolerance, no altered fixture/pin, no forced pooling. The checker rejects every positive or unexplained delta, including one allocation. A negative delta is accepted only through the uniform rollback-control, target-size and reproducibility checks; no fixture names are specialcased.",
            "unresolved_noise": "Any remaining mismatch or invalid window leaves revised 4.3 open. Use recorded size-class deltas and both pool counters to identify the responsible site before choosing a further bounded diagnostic. A later methodology change requires a reviewed rationale; unexplained deltas are never accepted by default."
          }
        ],
        "clock": {
          "replacement": "Prove the clock schedule with the existing counting-clock tests; retain CPU profiles only as diagnostics.",
          "acceptance": {
            "cadence": 8,
            "successful_sync_reads": "For N positive successful unforced synchronizations on one freshly armed state, reads=(N+7)/8 using integer division. Existing N={1,8,9,16,17,37} must yield {1,1,2,2,3,5}. Budget-object replacement must not reset this sequence.",
            "first_read": "Exactly 1 read on the next real synchronization after deadline installation or fresh-state materialization.",
            "ordinary_error": "Exactly 1 forced clock read for ordinary-error Finish with an armed live/expired deadline, even with pending=0, unless a synchronization failure is already latched. A successful forced read resets cadence: next 7 unforced synchronizations read 0 times; the eighth reads 1 time.",
            "no_work_or_deadline": "0 clock reads for unarmed deadlines, successful empty Flush, and latched-error replay.",
            "observation": "Expired deadline detected within 8 synchronizations; cancellation and reduction checks remain unconditional at every synchronization. Existing error identity, priority and exactly-once reduction settlement tests must pass unchanged.",
            "status": "All named deterministic proofs must pass on the final corrected tree; no substitute percentage or latency threshold."
          },
          "evidence": [
            "core/builtin_budget_test.go:74",
            "core/builtin_budget_test.go:102",
            "core/builtin_budget_test.go:140",
            "core/builtin_budget_test.go:169",
            "core/builtin_budget_test.go:419",
            "core/builtin_budget_test.go:709",
            "core/eval.go:296"
          ],
          "command_cwd": "/home/zhuk/Projects/own/go-lispico/.worktrees/zapply-builtin-budget-clock-cadence",
          "command": "env GOMAXPROCS=2 timeout 2m go test -p 2 -parallel 2 -timeout 2m ./core/ -run '^TestBuiltinWorkBudget_'",
          "profile_disposition": "Retain /tmp/builtin-budget-clock-cadence-amended-pprof.txt (2.48% runtimeNow) and the earlier profiles as diagnostic history. The empty short-profile output remains inconclusive. No further local profile is needed to close revised 4.1; no local latency A/B verdict. The prior <=1% claim did fail and is replaced by the newly approved deterministic proof, not reinterpreted as having passed."
        },
        "allocations": {
          "replacement": "Verify allocation layout, existing pin floor, all 52 controlled fixture cells and zero added settlement allocations. Permit only reproducible reductions isolated by the classifier rollback control and matching the removed target-allocation size; never positive or unexplained movement.",
          "layout": "unsafe.Sizeof(evalState{}) must be exactly 192 bytes in both base and final candidate on the same linux/amd64 toolchain. The improved probe reports this directly; no field widening or minimum-toolchain bump.",
          "vm_pins": {
            "counter-closure": 56,
            "guard-nil": 30,
            "kw-lookup": 31,
            "loop-sum": 87,
            "merge-config": 58,
            "pipeline": 71,
            "queue-promote": 174,
            "registry-fold": 69,
            "route-decision": 48,
            "rule-load": 164,
            "safe-parse": 71,
            "text-render": 42,
            "twice-macro": 43
          },
          "pin_evidence": "internal/goldset/alloc_test.go:25; no pin edits permitted by this task. The existing test compares exact counts, not only ceilings (line 86). If a verified shared-classifier reduction changes a pinned VM count, retain that floor failure and request a separate pin-policy decision; do not silently weaken the assertion or invent a replacement value.",
          "pin_command": "env GOMAXPROCS=2 timeout 2m go test -p 2 -parallel 2 -timeout 2m ./internal/goldset/ -run '^TestGoldsetVMAllocations$'",
          "controlled_fixture_acceptance": {
            "coverage": "13 Eval plus 13 Parse fixtures in each of eval and vm modes: all 52 cells, no omissions or selected-cell substitution.",
            "settings": "Same Go version/build settings/GOOS/GOARCH for base and candidate; GOMAXPROCS=1; GOGC=off; normal lazy registration; sequential cells; 100 warmup calls and exactly 10000 measured calls. Explicit GC occurs before warmup only. Both context and engine construction precede measurement. No formatting, JSON encoding or engine Close within a measured window.",
            "repetitions": "Exactly 2 predetermined fresh-process repetitions of all 6 variant/mode combinations: source base, final candidate with source-base core/error.go restored only by overlay, and final candidate; each under eval and vm. Compare every cell in each triple, and require every variant/mode to repeat identically. Do not keep retrying until a favorable triple appears.",
            "valid_window": "Each measured window must have num_gc=0, vm_pool_misses=0 and reader_pool_misses=0. A nonzero counter invalidates the attempted deterministic proof; retain its evidence and diagnose it.",
            "totals": "For every cell in both repetitions, rollback-control and source-base TotalAlloc, Mallocs and all size-class allocation counts must match exactly. Final candidate must either equal control exactly or have strictly fewer Mallocs, with TotalAlloc reduction equal to removed malloc count multiplied by the allocator class containing one *LispicoError pointer slot. Only that class may decrease; every other class must be unchanged. Thus every positive total or class-count delta is rejected, and all accepted reductions have an exact removed-target allocation signature. Each variant/mode must also reproduce its own exact totals and histogram in the second repetition.",
            "causal_control": "Build control from the final candidate with only core/error.go replaced by git show 39fe049115925fa1ec3e262eae0239c50e5d3b0c:core/error.go; all other source and instrumentation stays identical. Source review must confirm the candidate's changes in that file are restricted to terminal-error classification and its helpers. If control fails to compile, fails semantics, or differs from base in any cell, attribution fails and the task stays open. Do not add compensating overlays or subtract that discrepancy. A passing numerical checker still requires this causal source review.",
            "no_adjustment": "No subtraction of unexplained allocations, no per-fixture tolerance, no altered fixture/pin, no forced pooling. The checker rejects every positive or unexplained delta, including one allocation. A negative delta is accepted only through the uniform rollback-control, target-size and reproducibility checks; no fixture names are specialcased.",
            "unresolved_noise": "Any remaining mismatch or invalid window leaves revised 4.3 open. Use recorded size-class deltas and both pool counters to identify the responsible site before choosing a further bounded diagnostic. A later methodology change requires a reviewed rationale; unexplained deltas are never accepted by default."
          },
          "settlement_acceptance": {
            "existing_probe": "/tmp/builtin-budget-clock-cadence-finish-allocation-probe-results.json",
            "values": "On the final settlement fix, added_allocs=Finish-old must be <=0 for all 27 existing precreated-input/control rows. Empty/pending ordinary, typed/wrapped ordinary, direct/wrapped resource-limit inputs must have Finish=0 where old=0. Nil and direct context-terminal controls remain 0. Error identities and precedence remain unchanged.",
            "custom_hooks": "Keep user As/Is/Unwrap hooks behaviorally compatible. Distinguish allocations executed inside a user hook from package-owned traversal/target allocation using precreated inputs, allocation-free hook controls and direct-hook controls with recorded invocation counts. Arbitrary user-hook allocations are not assigned a new package cost limit. A library-created errors.As target escaping through a hook is still package-owned overhead and cannot be relabeled as user allocation. This extension belongs to the separate settlement/classifier work; its concrete tests and attribution must be reviewed there. The classifier contract explicitly permits at most one package-owned lazy target cell when a custom As is reached; this is not charged to user code or covered by the standard-error zero target.",
            "minimum": "Retain declared Go 1.24.0 support. Record actual compiler in allocation evidence; do not use a newer error API to raise the minimum."
          }
        },
        "probe": {
          "setup_script": "/tmp/builtin-budget-clock-cadence-deterministic-probe-setup.py",
          "setup_command": "python3 /tmp/builtin-budget-clock-cadence-deterministic-probe-setup.py",
          "template": "/tmp/builtin-budget-clock-cadence-allocation-probe/candidate/allocation_probe_test.go",
          "changes": "Temporary overlays only: retain the existing VM miss counter; add readerScratchPool.New miss counter, evalState size accessor and classifier target-pointer size accessor; change diagnostic GOMAXPROCS guard to 1; record both pool counters; preserve original warmup/window code. Add a third control overlay on the candidate with source-base core/error.go restored verbatim. Manifests record its source commit/hash. Production constructors remain unchanged except counter increments in overlay copies.",
          "commands_file_after_setup": "/tmp/builtin-budget-clock-cadence-deterministic-probe/commands.json",
          "example_exact_execution_cwd": "/home/zhuk/Projects/own/go-lispico/.worktrees/zapply-builtin-budget-clock-cadence",
          "example_exact_execution_command": "env GOMAXPROCS=1 GOGC=off GOLDSET_LAZY= GOLDSET_MODE=vm ALLOCATION_PROBE_OUTPUT=/tmp/builtin-budget-clock-cadence-deterministic-probe/candidate-vm-1.json timeout 2m go test -p 1 -parallel 1 -timeout 2m -count=1 -v -overlay /tmp/builtin-budget-clock-cadence-deterministic-probe/candidate/overlay.json ./internal/goldset/ -run '^TestAllocationProbe$'",
          "run_order": "Execute generated twelve commands sequentially in their recorded cwd. Each repetition runs base VM/control VM/candidate VM, then base eval/control eval/candidate eval. Each command has a 2m process timeout, 2m Go timeout and worker cap 1. Regenerate overlays after the final implementation change.",
          "comparison_command": "python3 /tmp/builtin-budget-clock-cadence-deterministic-probe-setup.py --compare",
          "comparison_result": "/tmp/builtin-budget-clock-cadence-deterministic-probe/comparison.json",
          "comparison_exit": "0 only when every coverage, environment, window, base-control equality, candidate-control equality-or-target-reduction, and repetition check passes; 1 on any recorded gap. Missing/invalid inputs also fail instead of producing a pass. Source attribution review remains explicitly required even when these numerical checks pass.",
          "wrapper_reason": "No project target supports temporary Go overlays or these filtered diagnostic tests; bounded raw go test commands are used. GOCACHE and GOTMPDIR remain unchanged.",
          "status": "Prepared only. Setup, Go compilation and all diagnostic measurements remain unexecuted in this planning round."
        },
        "numbers": [
          {
            "name": "clock cadence",
            "value": "8"
          },
          {
            "name": "evalState bytes",
            "value": "192"
          },
          {
            "name": "controlled fixture cells",
            "value": "52"
          },
          {
            "name": "fixed repetitions",
            "value": "2"
          },
          {
            "name": "warmup calls",
            "value": "100"
          },
          {
            "name": "measured calls",
            "value": "10000"
          },
          {
            "name": "base versus classifier-restored control delta",
            "value": "0"
          },
          {
            "name": "final candidate delta",
            "value": "0 or exact removed classifier target allocations; no positive or unexplained delta"
          },
          {
            "name": "GC and both pool misses",
            "value": "0"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "4.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
      "coder": "coder"
    },
    {
      "id": "release-gate-dispatch",
      "taskIds": [
        "5.1"
      ],
      "prev": "suite-verification",
      "sharedPkg": null,
      "parallel": true,
      "seam": "release-gate-dispatch",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "5.1",
          "file": ".github/workflows/release.yml",
          "symbol": "consumer-gate job",
          "anchor": "run: go build -o bin/perfgate ./cmd/perfgate",
          "change": "verify only, no edit, and NO dispatch during the implementation run. This chunk's verify only confirms the workflow resolves; the dispatch itself is `gh workflow run \"Release consumer gate\" --ref master`, which resolves its ref on the remote and therefore cannot run until the merge is pushed — an earlier run would gate the pre-fix tree and record that FAIL as this task's evidence. Task 5.1 stays open at merge and is closed post-merge by recording the dispatch run id, exactly as task 4.2 of gate-bytes-not-runner-portable was."
        }
      ],
      "contract": {
        "states": [
          "dispatched",
          "verdict-recorded"
        ],
        "transitions": [
          {
            "input": "gh workflow run against the candidate ref",
            "state": "dispatched",
            "effect": "forced",
            "evidence": ".github/workflows/release.yml:13-16 — the workflow is named `Release consumer gate` and its workflow_dispatch takes no inputs, so the ref is the only knob"
          },
          {
            "input": "a dispatch against a ref that was never pushed",
            "state": "dispatched",
            "effect": "no-op",
            "evidence": "the dispatch resolves the ref on the remote, so an unpushed candidate silently gates the previous commit; push first, then dispatch"
          },
          {
            "input": "gate exit 0 on the candidate",
            "state": "verdict-recorded",
            "effect": "set",
            "evidence": ".github/workflows/release.yml:188-268 — `Evaluate performance gate`, `Enforce performance gate verdict`, then `Store VM baseline on the authorized release`"
          }
        ],
        "forbidden": [
          "closing 5.1 from a local run — no local path exists",
          "dispatching before the candidate commit is pushed"
        ],
        "seeding": [
          {
            "state": "dispatched",
            "path": "push the candidate branch, then gh workflow run \"Release consumer gate\" --ref <branch>"
          },
          {
            "state": "verdict-recorded",
            "path": "post-merge: the dispatch run id recorded against task 5.1"
          }
        ],
        "identifiers": [
          "Release consumer gate",
          "Goldset/queue-promote",
          "Determine gate mode",
          "Enforce performance gate verdict",
          "Store VM baseline on the authorized release"
        ],
        "numbers": [
          {
            "name": "required gate exit code",
            "value": "0"
          },
          {
            "name": "failing cells required after the change",
            "value": "0"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "5.1"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "gh workflow view \"Release consumer gate\"",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "clock-seam",
      "summary": "NO-RED-WAIVER: introduces `nowFunc` in package core and routes flushPending's existing read through it. Behavior-identical and immediately used, so it has no observable contract of its own and leaves nothing declared-and-unused for `unused` to reject.",
      "tasks": [
        "2.1"
      ],
      "contract": {
        "states": [
          "seam-declared"
        ],
        "transitions": [
          {
            "input": "any flush with an armed deadline",
            "state": "seam-declared",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:56 — the read moves from time.Now() to nowFunc(), same value, same branch"
          }
        ],
        "forbidden": [
          "declaring the countdown field or the cadence constant in this chunk — both would be unused and `unused` is on by default under .golangci.yml",
          "any change to when the clock is read"
        ],
        "seeding": [
          {
            "state": "seam-declared",
            "path": "no test seeds this chunk; its verify is the existing core suite plus the linter"
          }
        ],
        "identifiers": [
          "nowFunc",
          "flushPending",
          "time.Now"
        ],
        "numbers": [
          {
            "name": "behavior changes in this chunk",
            "value": "0"
          },
          {
            "name": "symbols left declared-and-unused",
            "value": "0"
          }
        ]
      },
      "redTasks": []
    },
    {
      "id": "flush-clock-cadence",
      "summary": "flushPending reads the wall clock once every 8 synchronizations while the reduction charge and ctx.Err() stay unconditional at every synchronization.",
      "tasks": [
        "1.1",
        "2.2"
      ],
      "contract": {
        "states": [
          "no-deadline",
          "read-due",
          "mid-cadence",
          "latched"
        ],
        "transitions": [
          {
            "input": "flushPending, st.deadline is zero",
            "state": "no-deadline",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:56 — the whole deadline branch is already guarded by !st.deadline.IsZero(); the counter must not move and nowFunc must not be called"
          },
          {
            "input": "flushPending, deadline armed, deadlineClockPolls <= 0, now before deadline",
            "state": "read-due",
            "effect": "forced",
            "evidence": "core/vm/vm.go:856-864 — read nowFunc(), then store deadlineClockCadence-1 so the next read is 8 synchronizations out"
          },
          {
            "input": "flushPending, deadline armed, deadlineClockPolls <= 0, now at or after deadline",
            "state": "read-due",
            "effect": "set",
            "evidence": "core/builtin_budget.go:56-58 — b.latched = context.DeadlineExceeded, returned bare, not wrapped and not a *LispicoError"
          },
          {
            "input": "flushPending, deadline armed, deadlineClockPolls > 0",
            "state": "mid-cadence",
            "effect": "clear",
            "evidence": "core/vm/vm.go:863 — decrement by one, no clock read, no deadline verdict"
          },
          {
            "input": "flushPending at any cadence phase with a cancelled ctx",
            "state": "mid-cadence",
            "effect": "forced",
            "evidence": "core/builtin_budget.go:60-63 — b.ctx.Err() is checked after the deadline branch on every flush, never gated by the cadence"
          },
          {
            "input": "flushPending at any cadence phase crossing the reduction ceiling",
            "state": "mid-cadence",
            "effect": "forced",
            "evidence": "core/builtin_budget.go:52-55 — chargeReductions runs first and returns the terminal ResourceLimitError before the deadline branch is reached"
          },
          {
            "input": "Flush with pending == 0",
            "state": "no-deadline",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:42-45 — an empty successful flush never enters flushPending, so it moves no cadence position and reads no clock"
          },
          {
            "input": "Step with pending < 128",
            "state": "mid-cadence",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:31-33 — local steps touch no shared state"
          },
          {
            "input": "Step or Flush after b.latched is set",
            "state": "latched",
            "effect": "no-op",
            "evidence": "core/builtin_budget.go:27-29, 40-42 — the latched error replays by reference and performs no further sync, so no clock read and no counter movement"
          }
        ],
        "forbidden": [
          "deadlineClockPolls at or below 0 without the next flush reading the clock (deadline starvation) — the predicate is `<= 0`, never `== 0`",
          "chargeReductions or b.ctx.Err() reachable only on the read-due phase",
          "a clock read while st.deadline.IsZero()",
          "a test writing st.deadlineClockPolls or st.deadline directly instead of going through WithEvalDeadline and Step/Flush",
          "t.Parallel in any test that assigns nowFunc — package core runs its budget tests in parallel (core/builtin_budget_test.go:41, 77, 91, 118, 133) and a parallel test mutating a package global races them under the floor's -race leg; core/vm/deadline_clock_cadence_test.go sets the precedent by using no t.Parallel at all",
          "widening evalState into a larger size class — it is 192 bytes and every Eval allocates one, so a wider struct moves B/op on every gold-set cell against allowances of 0-8",
          "a `== 0` read-due predicate: Load-then-Add is not atomic as a pair and concurrent flushes can drive the counter negative",
          "naming the unexported cadence constant or countdown field from a sealed test — spell the cadence as the literal 8"
        ],
        "seeding": [
          {
            "state": "no-deadline",
            "path": "budgetCtx(context.Background(), n) with no WithEvalDeadline call (core/builtin_budget_test.go:12-14)"
          },
          {
            "state": "read-due",
            "path": "a freshly built ctx from WithEvalDeadline(budgetCtx(...), t) — a fresh evalState has deadlineClockPolls at its zero value, which is read-due; never assign the field"
          },
          {
            "state": "mid-cadence",
            "path": "from read-due, drive exactly one synchronization: NewBuiltinWorkBudget(ctx), one Step, one Flush. Repeat to advance the phase. There is no other legal way to move the counter"
          },
          {
            "state": "latched",
            "path": "let a synchronization fail (ceiling crossed via budgetCtx with a low limit, or an expired deadline), then call Step/Flush again"
          },
          {
            "state": "controlled clock",
            "path": "assign nowFunc in the test body with t.Cleanup(func() { nowFunc = restore }), no t.Parallel (core/vm/deadline_clock_cadence_test.go:25-30)"
          }
        ],
        "identifiers": [
          "nowFunc",
          "deadlineClockCadence",
          "deadlineClockPolls",
          "NewBuiltinWorkBudget",
          "BuiltinWorkBudget",
          "Step",
          "Flush",
          "flushPending",
          "WithEvalDeadline",
          "WithEvalResourceLimits",
          "budgetCtx",
          "stepN",
          "errCode",
          "checkInterval",
          "IsTerminalEvalError",
          "CodeResourceLimit",
          "context.DeadlineExceeded",
          "context.Canceled"
        ],
        "numbers": [
          {
            "name": "deadlineClockCadence",
            "value": "8"
          },
          {
            "name": "synchronizations between wall-clock reads",
            "value": "8"
          },
          {
            "name": "local work units between wall-clock reads",
            "value": "1024 (8 x checkInterval 128, core/eval.go:291) — the same 1,024 the spec's `Context observed within the reduction budget` scenario already permits for cancellation observation, which is what the cadence is derived from"
          },
          {
            "name": "clock reads for n synchronizations under an armed deadline",
            "value": "(n + 7) / 8, integer division"
          },
          {
            "name": "synchronizations between a deadline passing and termination",
            "value": "at most 8"
          },
          {
            "name": "sub-test n values that discriminate reset-to-8 from reset-to-7",
            "value": "1, 8, 9, 16, 17, 37 (core/vm/deadline_clock_cadence_test.go:21 — 37 alone cannot)"
          },
          {
            "name": "unsafe.Sizeof(evalState) before and after",
            "value": "192 both — pack the counter as atomic.Int32 into the padding hole after `calleeCharged bool`, never append it"
          }
        ]
      },
      "redTasks": [
        "1.1"
      ]
    },
    {
      "id": "deadline-install-reset",
      "summary": "A deadline landing on an evaluation makes that evaluation's next synchronization read the clock, so no evaluation inherits a cadence position from an earlier one.",
      "tasks": [
        "1.2",
        "2.3"
      ],
      "contract": {
        "states": [
          "fresh-state",
          "armed-mid-cadence",
          "rearmed"
        ],
        "transitions": [
          {
            "input": "WithEvalDeadline on a ctx whose evalState is already mid-cadence",
            "state": "armed-mid-cadence",
            "effect": "forced",
            "evidence": "core/eval.go:401-405 — the deadline write is a plain field store on an existing state; the counter must be stored to 0 alongside it, matching SetDeadline/SetTimeout in core/vm/vm.go:335-347"
          },
          {
            "input": "lazyEvalStateCtx.Value materializing a state that carries a deadline",
            "state": "fresh-state",
            "effect": "no-op",
            "evidence": "core/eval.go:257-266 — the state comes from newEvalStateWithLimits, so deadlineClockPolls is already at its read-due zero value; storing 0 here would be dead work. This is a deliberate deviation from the literal wording of task 2.3 (`reset at both sites`): the obligation is satisfied at core/eval.go:266 by construction and is pinned by a test rather than by a store, exactly as core/vm/vm.go:684-685 documents for the Apply copy"
          },
          {
            "input": "RearmReentrantEvalState reusing a wrapper for a new run",
            "state": "rearmed",
            "effect": "forced",
            "evidence": "core/eval.go:557-559 — the rearm drops the materialized evalState, so the next materialization builds a fresh one and inherits nothing; no third install site exists"
          },
          {
            "input": "AdoptEvalStateWithMeter building a wrapper with an eager deadline",
            "state": "fresh-state",
            "effect": "no-op",
            "evidence": "core/eval.go:445 — the deadline goes on the wrapper, not on an evalState; the state it later materializes is fresh"
          }
        ],
        "forbidden": [
          "an evaluation observing a cadence position established before its deadline existed",
          "a synchronization skipping its clock read on the first flush after a deadline is installed",
          "relying on caller discipline (a documented `reset it yourself` rule) instead of resetting at the install site",
          "assigning the countdown field directly from a test — seed through the production install paths only",
          "t.Parallel in any test that swaps nowFunc — the seam is a package-level var, and every existing test in core/builtin_budget_test.go calls t.Parallel"
        ],
        "seeding": [
          {
            "state": "armed-mid-cadence",
            "path": "WithEvalDeadline(budgetCtx(...), far-future t) to arm, THEN flush exactly 3 times: the countdown only advances inside the armed branch, and the count must stay strictly between 1 and deadlineClockCadence-1 so the state is genuinely mid-cadence. Flushing a multiple of 8 returns it to read-due and the red test goes green for the wrong reason."
          },
          {
            "state": "fresh-state",
            "path": "AdoptEvalStateWithMeter(parent, deadline, 0, EvalMeterSnapshot{}) (core/eval.go:424) then NewBuiltinWorkBudget on the returned ctx — this is the only path that materializes the state through lazyEvalStateCtx.Value. budgetCtx does NOT reach it: WithEvalResourceLimits attaches a concrete state via ensureEvalState, so a test built that way would pass without touching the lazy site."
          },
          {
            "state": "rearmed",
            "path": "install a second deadline on an evaluation whose countdown has already advanced"
          },
          {
            "state": "controlled clock",
            "path": "save the package-level nowFunc, replace it with a counting or scripted stub, restore via t.Cleanup — the pattern at core/vm/deadline_clock_cadence_test.go:25-30"
          }
        ],
        "identifiers": [
          "WithEvalDeadline",
          "deadlineClockPolls",
          "deadlineClockCadence",
          "nowFunc",
          "evalStateFrom",
          "newEvalStateWithLimits",
          "lazyEvalStateCtx",
          "RearmReentrantEvalState",
          "budgetCtx"
        ],
        "numbers": [
          {
            "name": "synchronizations between installing a deadline and the next clock read",
            "value": "1 — the next one"
          },
          {
            "name": "cadence positions inherited across evaluations",
            "value": "0"
          }
        ]
      },
      "redTasks": [
        "1.2"
      ]
    },
    {
      "id": "cost-and-suite-verification",
      "summary": "NO-RED-WAIVER: verification only against existing clock tests, unchanged pins, controlled allocation measurements and the full floor. NO-TESTER-WAIVER: no production implementation in this seam; root records actual results. CPU profiles and raw B/op remain diagnostic; unchanged hosted gate required separately.",
      "tasks": [
        "4.1",
        "4.2",
        "4.3"
      ],
      "contract": {
        "states": [
          "clock-proved",
          "allocations-proved",
          "suite-green"
        ],
        "transitions": [],
        "forbidden": [
          "Changing eight-sync cadence or existing semantic tests.",
          "Editing allocation pins, fixture inputs, hosted workflow or perfgate tiers.",
          "Treating sampled CPU share, rounded raw B/op or local latency as acceptance.",
          "Ignoring unexplained controlled allocation differences or repeating until a favorable pair appears."
        ],
        "seeding": [
          {
            "cadence": 8,
            "successful_sync_reads": "For N positive successful unforced synchronizations on one freshly armed state, reads=(N+7)/8 using integer division. Existing N={1,8,9,16,17,37} must yield {1,1,2,2,3,5}. Budget-object replacement must not reset this sequence.",
            "first_read": "Exactly 1 read on the next real synchronization after deadline installation or fresh-state materialization.",
            "ordinary_error": "Exactly 1 forced clock read for ordinary-error Finish with an armed live/expired deadline, even with pending=0, unless a synchronization failure is already latched. A successful forced read resets cadence: next 7 unforced synchronizations read 0 times; the eighth reads 1 time.",
            "no_work_or_deadline": "0 clock reads for unarmed deadlines, successful empty Flush, and latched-error replay.",
            "observation": "Expired deadline detected within 8 synchronizations; cancellation and reduction checks remain unconditional at every synchronization. Existing error identity, priority and exactly-once reduction settlement tests must pass unchanged.",
            "status": "All named deterministic proofs must pass on the final corrected tree; no substitute percentage or latency threshold."
          },
          {
            "coverage": "13 Eval plus 13 Parse fixtures in each of eval and vm modes: all 52 cells, no omissions or selected-cell substitution.",
            "settings": "Same Go version/build settings/GOOS/GOARCH for base and candidate; GOMAXPROCS=1; GOGC=off; normal lazy registration; sequential cells; 100 warmup calls and exactly 10000 measured calls. Explicit GC occurs before warmup only. Both context and engine construction precede measurement. No formatting, JSON encoding or engine Close within a measured window.",
            "repetitions": "Exactly 2 predetermined fresh-process repetitions of all 6 variant/mode combinations: source base, final candidate with source-base core/error.go restored only by overlay, and final candidate; each under eval and vm. Compare every cell in each triple, and require every variant/mode to repeat identically. Do not keep retrying until a favorable triple appears.",
            "valid_window": "Each measured window must have num_gc=0, vm_pool_misses=0 and reader_pool_misses=0. A nonzero counter invalidates the attempted deterministic proof; retain its evidence and diagnose it.",
            "totals": "For every cell in both repetitions, rollback-control and source-base TotalAlloc, Mallocs and all size-class allocation counts must match exactly. Final candidate must either equal control exactly or have strictly fewer Mallocs, with TotalAlloc reduction equal to removed malloc count multiplied by the allocator class containing one *LispicoError pointer slot. Only that class may decrease; every other class must be unchanged. Thus every positive total or class-count delta is rejected, and all accepted reductions have an exact removed-target allocation signature. Each variant/mode must also reproduce its own exact totals and histogram in the second repetition.",
            "causal_control": "Build control from the final candidate with only core/error.go replaced by git show 39fe049115925fa1ec3e262eae0239c50e5d3b0c:core/error.go; all other source and instrumentation stays identical. Source review must confirm the candidate's changes in that file are restricted to terminal-error classification and its helpers. If control fails to compile, fails semantics, or differs from base in any cell, attribution fails and the task stays open. Do not add compensating overlays or subtract that discrepancy. A passing numerical checker still requires this causal source review.",
            "no_adjustment": "No subtraction of unexplained allocations, no per-fixture tolerance, no altered fixture/pin, no forced pooling. The checker rejects every positive or unexplained delta, including one allocation. A negative delta is accepted only through the uniform rollback-control, target-size and reproducibility checks; no fixture names are specialcased.",
            "unresolved_noise": "Any remaining mismatch or invalid window leaves revised 4.3 open. Use recorded size-class deltas and both pool counters to identify the responsible site before choosing a further bounded diagnostic. A later methodology change requires a reviewed rationale; unexplained deltas are never accepted by default."
          }
        ],
        "clock": {
          "replacement": "Prove the clock schedule with the existing counting-clock tests; retain CPU profiles only as diagnostics.",
          "acceptance": {
            "cadence": 8,
            "successful_sync_reads": "For N positive successful unforced synchronizations on one freshly armed state, reads=(N+7)/8 using integer division. Existing N={1,8,9,16,17,37} must yield {1,1,2,2,3,5}. Budget-object replacement must not reset this sequence.",
            "first_read": "Exactly 1 read on the next real synchronization after deadline installation or fresh-state materialization.",
            "ordinary_error": "Exactly 1 forced clock read for ordinary-error Finish with an armed live/expired deadline, even with pending=0, unless a synchronization failure is already latched. A successful forced read resets cadence: next 7 unforced synchronizations read 0 times; the eighth reads 1 time.",
            "no_work_or_deadline": "0 clock reads for unarmed deadlines, successful empty Flush, and latched-error replay.",
            "observation": "Expired deadline detected within 8 synchronizations; cancellation and reduction checks remain unconditional at every synchronization. Existing error identity, priority and exactly-once reduction settlement tests must pass unchanged.",
            "status": "All named deterministic proofs must pass on the final corrected tree; no substitute percentage or latency threshold."
          },
          "evidence": [
            "core/builtin_budget_test.go:74",
            "core/builtin_budget_test.go:102",
            "core/builtin_budget_test.go:140",
            "core/builtin_budget_test.go:169",
            "core/builtin_budget_test.go:419",
            "core/builtin_budget_test.go:709",
            "core/eval.go:296"
          ],
          "command_cwd": "/home/zhuk/Projects/own/go-lispico/.worktrees/zapply-builtin-budget-clock-cadence",
          "command": "env GOMAXPROCS=2 timeout 2m go test -p 2 -parallel 2 -timeout 2m ./core/ -run '^TestBuiltinWorkBudget_'",
          "profile_disposition": "Retain /tmp/builtin-budget-clock-cadence-amended-pprof.txt (2.48% runtimeNow) and the earlier profiles as diagnostic history. The empty short-profile output remains inconclusive. No further local profile is needed to close revised 4.1; no local latency A/B verdict. The prior <=1% claim did fail and is replaced by the newly approved deterministic proof, not reinterpreted as having passed."
        },
        "allocations": {
          "replacement": "Verify allocation layout, existing pin floor, all 52 controlled fixture cells and zero added settlement allocations. Permit only reproducible reductions isolated by the classifier rollback control and matching the removed target-allocation size; never positive or unexplained movement.",
          "layout": "unsafe.Sizeof(evalState{}) must be exactly 192 bytes in both base and final candidate on the same linux/amd64 toolchain. The improved probe reports this directly; no field widening or minimum-toolchain bump.",
          "vm_pins": {
            "counter-closure": 56,
            "guard-nil": 30,
            "kw-lookup": 31,
            "loop-sum": 87,
            "merge-config": 58,
            "pipeline": 71,
            "queue-promote": 174,
            "registry-fold": 69,
            "route-decision": 48,
            "rule-load": 164,
            "safe-parse": 71,
            "text-render": 42,
            "twice-macro": 43
          },
          "pin_evidence": "internal/goldset/alloc_test.go:25; no pin edits permitted by this task. The existing test compares exact counts, not only ceilings (line 86). If a verified shared-classifier reduction changes a pinned VM count, retain that floor failure and request a separate pin-policy decision; do not silently weaken the assertion or invent a replacement value.",
          "pin_command": "env GOMAXPROCS=2 timeout 2m go test -p 2 -parallel 2 -timeout 2m ./internal/goldset/ -run '^TestGoldsetVMAllocations$'",
          "controlled_fixture_acceptance": {
            "coverage": "13 Eval plus 13 Parse fixtures in each of eval and vm modes: all 52 cells, no omissions or selected-cell substitution.",
            "settings": "Same Go version/build settings/GOOS/GOARCH for base and candidate; GOMAXPROCS=1; GOGC=off; normal lazy registration; sequential cells; 100 warmup calls and exactly 10000 measured calls. Explicit GC occurs before warmup only. Both context and engine construction precede measurement. No formatting, JSON encoding or engine Close within a measured window.",
            "repetitions": "Exactly 2 predetermined fresh-process repetitions of all 6 variant/mode combinations: source base, final candidate with source-base core/error.go restored only by overlay, and final candidate; each under eval and vm. Compare every cell in each triple, and require every variant/mode to repeat identically. Do not keep retrying until a favorable triple appears.",
            "valid_window": "Each measured window must have num_gc=0, vm_pool_misses=0 and reader_pool_misses=0. A nonzero counter invalidates the attempted deterministic proof; retain its evidence and diagnose it.",
            "totals": "For every cell in both repetitions, rollback-control and source-base TotalAlloc, Mallocs and all size-class allocation counts must match exactly. Final candidate must either equal control exactly or have strictly fewer Mallocs, with TotalAlloc reduction equal to removed malloc count multiplied by the allocator class containing one *LispicoError pointer slot. Only that class may decrease; every other class must be unchanged. Thus every positive total or class-count delta is rejected, and all accepted reductions have an exact removed-target allocation signature. Each variant/mode must also reproduce its own exact totals and histogram in the second repetition.",
            "causal_control": "Build control from the final candidate with only core/error.go replaced by git show 39fe049115925fa1ec3e262eae0239c50e5d3b0c:core/error.go; all other source and instrumentation stays identical. Source review must confirm the candidate's changes in that file are restricted to terminal-error classification and its helpers. If control fails to compile, fails semantics, or differs from base in any cell, attribution fails and the task stays open. Do not add compensating overlays or subtract that discrepancy. A passing numerical checker still requires this causal source review.",
            "no_adjustment": "No subtraction of unexplained allocations, no per-fixture tolerance, no altered fixture/pin, no forced pooling. The checker rejects every positive or unexplained delta, including one allocation. A negative delta is accepted only through the uniform rollback-control, target-size and reproducibility checks; no fixture names are specialcased.",
            "unresolved_noise": "Any remaining mismatch or invalid window leaves revised 4.3 open. Use recorded size-class deltas and both pool counters to identify the responsible site before choosing a further bounded diagnostic. A later methodology change requires a reviewed rationale; unexplained deltas are never accepted by default."
          },
          "settlement_acceptance": {
            "existing_probe": "/tmp/builtin-budget-clock-cadence-finish-allocation-probe-results.json",
            "values": "On the final settlement fix, added_allocs=Finish-old must be <=0 for all 27 existing precreated-input/control rows. Empty/pending ordinary, typed/wrapped ordinary, direct/wrapped resource-limit inputs must have Finish=0 where old=0. Nil and direct context-terminal controls remain 0. Error identities and precedence remain unchanged.",
            "custom_hooks": "Keep user As/Is/Unwrap hooks behaviorally compatible. Distinguish allocations executed inside a user hook from package-owned traversal/target allocation using precreated inputs, allocation-free hook controls and direct-hook controls with recorded invocation counts. Arbitrary user-hook allocations are not assigned a new package cost limit. A library-created errors.As target escaping through a hook is still package-owned overhead and cannot be relabeled as user allocation. This extension belongs to the separate settlement/classifier work; its concrete tests and attribution must be reviewed there. The classifier contract explicitly permits at most one package-owned lazy target cell when a custom As is reached; this is not charged to user code or covered by the standard-error zero target.",
            "minimum": "Retain declared Go 1.24.0 support. Record actual compiler in allocation evidence; do not use a newer error API to raise the minimum."
          }
        },
        "probe": {
          "setup_script": "/tmp/builtin-budget-clock-cadence-deterministic-probe-setup.py",
          "setup_command": "python3 /tmp/builtin-budget-clock-cadence-deterministic-probe-setup.py",
          "template": "/tmp/builtin-budget-clock-cadence-allocation-probe/candidate/allocation_probe_test.go",
          "changes": "Temporary overlays only: retain the existing VM miss counter; add readerScratchPool.New miss counter, evalState size accessor and classifier target-pointer size accessor; change diagnostic GOMAXPROCS guard to 1; record both pool counters; preserve original warmup/window code. Add a third control overlay on the candidate with source-base core/error.go restored verbatim. Manifests record its source commit/hash. Production constructors remain unchanged except counter increments in overlay copies.",
          "commands_file_after_setup": "/tmp/builtin-budget-clock-cadence-deterministic-probe/commands.json",
          "example_exact_execution_cwd": "/home/zhuk/Projects/own/go-lispico/.worktrees/zapply-builtin-budget-clock-cadence",
          "example_exact_execution_command": "env GOMAXPROCS=1 GOGC=off GOLDSET_LAZY= GOLDSET_MODE=vm ALLOCATION_PROBE_OUTPUT=/tmp/builtin-budget-clock-cadence-deterministic-probe/candidate-vm-1.json timeout 2m go test -p 1 -parallel 1 -timeout 2m -count=1 -v -overlay /tmp/builtin-budget-clock-cadence-deterministic-probe/candidate/overlay.json ./internal/goldset/ -run '^TestAllocationProbe$'",
          "run_order": "Execute generated twelve commands sequentially in their recorded cwd. Each repetition runs base VM/control VM/candidate VM, then base eval/control eval/candidate eval. Each command has a 2m process timeout, 2m Go timeout and worker cap 1. Regenerate overlays after the final implementation change.",
          "comparison_command": "python3 /tmp/builtin-budget-clock-cadence-deterministic-probe-setup.py --compare",
          "comparison_result": "/tmp/builtin-budget-clock-cadence-deterministic-probe/comparison.json",
          "comparison_exit": "0 only when every coverage, environment, window, base-control equality, candidate-control equality-or-target-reduction, and repetition check passes; 1 on any recorded gap. Missing/invalid inputs also fail instead of producing a pass. Source attribution review remains explicitly required even when these numerical checks pass.",
          "wrapper_reason": "No project target supports temporary Go overlays or these filtered diagnostic tests; bounded raw go test commands are used. GOCACHE and GOTMPDIR remain unchanged.",
          "status": "Prepared only. Setup, Go compilation and all diagnostic measurements remain unexecuted in this planning round."
        },
        "numbers": [
          {
            "name": "clock cadence",
            "value": "8"
          },
          {
            "name": "evalState bytes",
            "value": "192"
          },
          {
            "name": "controlled fixture cells",
            "value": "52"
          },
          {
            "name": "fixed repetitions",
            "value": "2"
          },
          {
            "name": "warmup calls",
            "value": "100"
          },
          {
            "name": "measured calls",
            "value": "10000"
          },
          {
            "name": "base versus classifier-restored control delta",
            "value": "0"
          },
          {
            "name": "final candidate delta",
            "value": "0 or exact removed classifier target allocations; no positive or unexplained delta"
          },
          {
            "name": "GC and both pool misses",
            "value": "0"
          }
        ]
      },
      "redTasks": []
    },
    {
      "id": "release-gate-dispatch",
      "summary": "NO-RED-WAIVER: the gate has no local execution path — it runs on the hosted runner and is closed post-merge by recording the run id.",
      "tasks": [
        "5.1"
      ],
      "contract": {
        "states": [
          "dispatched",
          "verdict-recorded"
        ],
        "transitions": [
          {
            "input": "gh workflow run against the candidate ref",
            "state": "dispatched",
            "effect": "forced",
            "evidence": ".github/workflows/release.yml:13-16 — the workflow is named `Release consumer gate` and its workflow_dispatch takes no inputs, so the ref is the only knob"
          },
          {
            "input": "a dispatch against a ref that was never pushed",
            "state": "dispatched",
            "effect": "no-op",
            "evidence": "the dispatch resolves the ref on the remote, so an unpushed candidate silently gates the previous commit; push first, then dispatch"
          },
          {
            "input": "gate exit 0 on the candidate",
            "state": "verdict-recorded",
            "effect": "set",
            "evidence": ".github/workflows/release.yml:188-268 — `Evaluate performance gate`, `Enforce performance gate verdict`, then `Store VM baseline on the authorized release`"
          }
        ],
        "forbidden": [
          "closing 5.1 from a local run — no local path exists",
          "dispatching before the candidate commit is pushed"
        ],
        "seeding": [
          {
            "state": "dispatched",
            "path": "push the candidate branch, then gh workflow run \"Release consumer gate\" --ref <branch>"
          },
          {
            "state": "verdict-recorded",
            "path": "post-merge: the dispatch run id recorded against task 5.1"
          }
        ],
        "identifiers": [
          "Release consumer gate",
          "Goldset/queue-promote",
          "Determine gate mode",
          "Enforce performance gate verdict",
          "Store VM baseline on the authorized release"
        ],
        "numbers": [
          {
            "name": "required gate exit code",
            "value": "0"
          },
          {
            "name": "failing cells required after the change",
            "value": "0"
          }
        ]
      },
      "redTasks": []
    },
    {
      "id": "decision-record",
      "summary": "NO-RED-WAIVER: a changelog entry has no observable contract of its own; the behavior it describes is pinned by the cadence seam's tests.",
      "tasks": [
        "3.1"
      ],
      "contract": {
        "states": [
          "entry-recorded"
        ],
        "transitions": [
          {
            "input": "an expired deadline during Builtin work",
            "state": "entry-recorded",
            "effect": "no-op",
            "evidence": "documentation only; the behavior is pinned by TestBuiltinWorkBudget_DeadlineCrossingBoundedBySynchronizations"
          }
        ],
        "forbidden": [
          "recording it under [Unreleased] — that section is empty and v0.13.0 is the unreleased version",
          "describing the countdown, the constant or evalState: the entry names what a consumer observes"
        ],
        "seeding": [
          {
            "state": "entry-recorded",
            "path": "no test seeds this; `go test ./cl/...` covers the changelog-head assertions that read this section"
          }
        ],
        "identifiers": [
          "CHANGELOG.md",
          "[0.13.0]",
          "### Changed"
        ],
        "numbers": [
          {
            "name": "synchronizations of deadline-observation lag the entry states",
            "value": "8"
          }
        ]
      },
      "redTasks": []
    },
    {
      "id": "finish-member",
      "summary": "NO-RED-WAIVER: add the exported unused-by-consumers Finish method with existing Flush-and-select semantics only; existing behavior is unchanged until consumer migration.",
      "tasks": [
        "2.4"
      ],
      "redTasks": [],
      "codeTasks": [
        "2.4"
      ],
      "contract": {}
    },
    {
      "id": "finish-forced-error",
      "summary": "Force terminal deadline/cancellation observation when settling a pending ordinary error; preserve accounting and ordinary cadence.",
      "tasks": [
        "1.3",
        "2.5"
      ],
      "redTasks": [
        "1.3"
      ],
      "codeTasks": [
        "2.5"
      ],
      "contract": {
        "states": [
          "no-deadline",
          "read-due",
          "mid-cadence",
          "pending-zero",
          "pending-nonzero",
          "input-nil",
          "input-nonterminal",
          "input-terminal",
          "latched-sync-error"
        ],
        "transitions": [
          {
            "input": "Finish(nil), any pending state",
            "state": "input-nil",
            "evidence": "Exactly ordinary Flush. Zero pending and no latch -> no shared work or clock/context access; nonzero pending -> existing cadence.",
            "effect": "no-op"
          },
          {
            "input": "Finish(nonterminal), no latch, pending nonzero",
            "state": "input-nonterminal",
            "evidence": "Charge and drain actual units once; force armed deadline read; then ctx.Err. Return terminal synchronization failure over supplied error, otherwise supplied error unchanged.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no latch, pending zero",
            "state": "input-nonterminal",
            "evidence": "Charge zero units; same forced deadline/context check; no synthetic Step and no forced reduction charge.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), unexpired armed deadline, any previous cadence",
            "state": "input-nonterminal",
            "evidence": "Exactly one clock read; reset cadence to 7. Ordinary nonempty synchronization number 8 after this reads again.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), expired armed deadline",
            "state": "input-nonterminal",
            "evidence": "Latch and return bare context.DeadlineExceeded; no subsequent ctx.Err check because deadline wins at this synchronization.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no engine deadline",
            "state": "input-nonterminal",
            "evidence": "No clock read or countdown movement. Caller cancellation still observed, including pending zero.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reduction ceiling crossed and deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Reduction error latches and wins; no clock read or context check after failed charging.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reductions allowed, deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Bare context.DeadlineExceeded wins; preserve existing within-sync order.",
            "effect": "forced"
          },
          {
            "input": "Finish(terminal), no latch, pending nonzero",
            "state": "input-terminal",
            "evidence": "Ordinary Flush charges pending units once; original supplied terminal is returned by identity even if Flush produces a different terminal failure. Flush's first synchronization failure still latches internally.",
            "effect": "no-op"
          },
          {
            "input": "Finish(terminal), no latch, pending zero",
            "state": "input-terminal",
            "evidence": "Ordinary empty Flush does nothing; preserve supplied error identity; no forced read.",
            "effect": "no-op"
          },
          {
            "input": "Any Finish input, already latched synchronization failure",
            "state": "latched-sync-error",
            "evidence": "No extra charge, clock read, context read or cadence movement. Apply existing error-selection rule, including retaining a supplied terminal error over a different latched terminal.",
            "effect": "no-op"
          },
          {
            "input": "Successful forced check returns supplied nonterminal error",
            "state": "input-nonterminal",
            "evidence": "Do not latch operation error. Repeated Finish cannot recharge drained pending units; Step and Flush remain usable under their existing rules.",
            "effect": "forced"
          }
        ],
        "forbidden": [
          "Forcing clock reads from Step, ordinary Flush, constructors, pre-callback sites or successful result returns.",
          "Changing terminal classification, wrapping the new deadline result, changing reduction/cancellation ordering, or overwriting the first synchronization-error latch.",
          "Flushing once ordinarily and then again forcibly for the same pending units; Finish must choose one settlement mode.",
          "Calling IsTerminalEvalError on nil on successful helper paths when the direct Flush path already suffices.",
          "Adding a per-budget field, widening evalState, allocating wrappers/interfaces/closures in settlement, resetting cadence on each budget construction.",
          "Editing existing sealed test bodies, reducing timeout/precedence assertions, weakening inventory guards, changing allocation pins or acceptance criteria.",
          "Expanding scope to VM/runtime deadline checkpoints or automatic GoFunc error wrapping."
        ],
        "seeding": [
          "Use budgetCtx and WithEvalDeadline, prime 1 through 7 nonempty Step/Flush synchronizations to exercise all mid-cadence positions; never assign the private counter.",
          "Swap nowFunc with a counting/scripted clock only in sequential tests and restore via t.Cleanup. Advance from before deadline to equality/after deadline after priming.",
          "Exercise pending zero using a fresh budget sharing the primed context or an already-drained budget; pending remainder via fewer than 128 Step calls.",
          "Create sync latches through actual reduction crossing, expired deadline or canceled context; do not assign b.latched.",
          "Read exact accounting through EvalMeterFrom(ctx).Snapshot().Reductions and AllocationBytes, verified at core/metering.go:83,145,154.",
          "Seed operation errors with existing core constructors or errors.New in tests; wrapped terminal inputs use fmt.Errorf with %w around context.Canceled/context.DeadlineExceeded or an actual ResourceLimitError."
        ],
        "budgets": {
          "ordinaryClockCadence": 8,
          "ordinaryReadCount": "(n + 7) / 8 for n nonempty synchronizations after deadline installation",
          "forcedReads": "One additional armed-deadline read per unlatched nonterminal error settlement that reaches the clock branch; zero when reduction charging fails or no deadline is armed.",
          "successfulForcedReadReset": 7,
          "syntheticReductions": 0,
          "pendingChargeMultiplicity": 1,
          "addedFields": 0,
          "addedSettlementAllocations": "Zero for standard precreated error inputs; custom As may require one shared lazy target cell, preserving stdlib semantics.",
          "evalStateBytes": 192,
          "allocationPin": "queue-promote 174, unchanged; all existing pins unchanged",
          "goldsetBytesDelta": "All 52 controlled cells: classifier-restored candidate equals base; final candidate equals control or removes only classifier target allocations; both single-P repetitions exact. Raw B/op diagnostic only.",
          "cpuAcceptance": "Exact counting-clock contract plus unchanged hosted gate; CPU profiles diagnostic only, approved cost-scope amendment."
        },
        "identifiers": [
          "BuiltinWorkBudget",
          "Finish",
          "Flush",
          "Step",
          "nowFunc",
          "context.DeadlineExceeded",
          "context.Canceled",
          "IsTerminalEvalError",
          "EvalMeterFrom",
          "Snapshot"
        ],
        "testCases": {
          "TestBuiltinWorkBudget_FinishForcesDeadlineAfterError": "Prime each phase 1..7 before expiry, then return nonterminal input at deadline equality and after deadline; cover zero and nonzero pending. Require exact bare DeadlineExceeded and exactly one added read.",
          "TestBuiltinWorkBudget_FinishPreservesSuccessfulCadence": "Finish(nil) across short fresh budgets retains exact ordinary read count and empty no-op. Existing ShortCallsShareClockCadence remains untouched.",
          "TestBuiltinWorkBudget_FinishPreservesErrorPrecedence": "Table nil/nonterminal/terminal input, no deadline/live/expired deadline, live/canceled parent and simultaneous reduction crossing. Preserve incoming terminal and nonterminal identities when selected; observe cancellation with zero pending and no deadline.",
          "TestBuiltinWorkBudget_FinishChargesPendingOnce": "Snapshot actual reductions/allocations before and after first/repeated settlement for nil/nonterminal/terminal inputs and zero/nonzero pending. Retrying with drained pending adds no reductions and no allocation charge.",
          "TestBuiltinWorkBudget_FinishReplaysLatchedError": "Induce terminal sync error through real public budget operations, then repeat Step/Flush/Finish with nil and ordinary errors; same latch by identity and no clock calls. A distinct terminal input still wins by identity without disturbing later latch replay.",
          "TestBuiltinWorkBudget_FinishResetsDeadlineCadence": "Prime mid-cadence, perform a successful forced check against future deadline with ordinary input, then drive 7 ordinary nonempty syncs without another read and observe next read on sync 8."
        }
      }
    },
    {
      "id": "finish-consumers",
      "summary": "Migrate the five pending-error selectors, keeping direct Flush on success and all existing inventory guards.",
      "tasks": [
        "1.4",
        "2.6"
      ],
      "redTasks": [
        "1.4"
      ],
      "codeTasks": [
        "2.6"
      ],
      "contract": {
        "states": [
          "no-deadline",
          "read-due",
          "mid-cadence",
          "pending-zero",
          "pending-nonzero",
          "input-nil",
          "input-nonterminal",
          "input-terminal",
          "latched-sync-error"
        ],
        "transitions": [
          {
            "input": "Finish(nil), any pending state",
            "state": "input-nil",
            "evidence": "Exactly ordinary Flush. Zero pending and no latch -> no shared work or clock/context access; nonzero pending -> existing cadence.",
            "effect": "no-op"
          },
          {
            "input": "Finish(nonterminal), no latch, pending nonzero",
            "state": "input-nonterminal",
            "evidence": "Charge and drain actual units once; force armed deadline read; then ctx.Err. Return terminal synchronization failure over supplied error, otherwise supplied error unchanged.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no latch, pending zero",
            "state": "input-nonterminal",
            "evidence": "Charge zero units; same forced deadline/context check; no synthetic Step and no forced reduction charge.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), unexpired armed deadline, any previous cadence",
            "state": "input-nonterminal",
            "evidence": "Exactly one clock read; reset cadence to 7. Ordinary nonempty synchronization number 8 after this reads again.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), expired armed deadline",
            "state": "input-nonterminal",
            "evidence": "Latch and return bare context.DeadlineExceeded; no subsequent ctx.Err check because deadline wins at this synchronization.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), no engine deadline",
            "state": "input-nonterminal",
            "evidence": "No clock read or countdown movement. Caller cancellation still observed, including pending zero.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reduction ceiling crossed and deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Reduction error latches and wins; no clock read or context check after failed charging.",
            "effect": "forced"
          },
          {
            "input": "Finish(nonterminal), reductions allowed, deadline expired and parent canceled",
            "state": "input-nonterminal",
            "evidence": "Bare context.DeadlineExceeded wins; preserve existing within-sync order.",
            "effect": "forced"
          },
          {
            "input": "Finish(terminal), no latch, pending nonzero",
            "state": "input-terminal",
            "evidence": "Ordinary Flush charges pending units once; original supplied terminal is returned by identity even if Flush produces a different terminal failure. Flush's first synchronization failure still latches internally.",
            "effect": "no-op"
          },
          {
            "input": "Finish(terminal), no latch, pending zero",
            "state": "input-terminal",
            "evidence": "Ordinary empty Flush does nothing; preserve supplied error identity; no forced read.",
            "effect": "no-op"
          },
          {
            "input": "Any Finish input, already latched synchronization failure",
            "state": "latched-sync-error",
            "evidence": "No extra charge, clock read, context read or cadence movement. Apply existing error-selection rule, including retaining a supplied terminal error over a different latched terminal.",
            "effect": "no-op"
          },
          {
            "input": "Successful forced check returns supplied nonterminal error",
            "state": "input-nonterminal",
            "evidence": "Do not latch operation error. Repeated Finish cannot recharge drained pending units; Step and Flush remain usable under their existing rules.",
            "effect": "forced"
          }
        ],
        "forbidden": [
          "Forcing clock reads from Step, ordinary Flush, constructors, pre-callback sites or successful result returns.",
          "Changing terminal classification, wrapping the new deadline result, changing reduction/cancellation ordering, or overwriting the first synchronization-error latch.",
          "Flushing once ordinarily and then again forcibly for the same pending units; Finish must choose one settlement mode.",
          "Calling IsTerminalEvalError on nil on successful helper paths when the direct Flush path already suffices.",
          "Adding a per-budget field, widening evalState, allocating wrappers/interfaces/closures in settlement, resetting cadence on each budget construction.",
          "Editing existing sealed test bodies, reducing timeout/precedence assertions, weakening inventory guards, changing allocation pins or acceptance criteria.",
          "Expanding scope to VM/runtime deadline checkpoints or automatic GoFunc error wrapping."
        ],
        "seeding": [
          "Use budgetCtx and WithEvalDeadline, prime 1 through 7 nonempty Step/Flush synchronizations to exercise all mid-cadence positions; never assign the private counter.",
          "Swap nowFunc with a counting/scripted clock only in sequential tests and restore via t.Cleanup. Advance from before deadline to equality/after deadline after priming.",
          "Exercise pending zero using a fresh budget sharing the primed context or an already-drained budget; pending remainder via fewer than 128 Step calls.",
          "Create sync latches through actual reduction crossing, expired deadline or canceled context; do not assign b.latched.",
          "Read exact accounting through EvalMeterFrom(ctx).Snapshot().Reductions and AllocationBytes, verified at core/metering.go:83,145,154.",
          "Seed operation errors with existing core constructors or errors.New in tests; wrapped terminal inputs use fmt.Errorf with %w around context.Canceled/context.DeadlineExceeded or an actual ResourceLimitError."
        ],
        "budgets": {
          "ordinaryClockCadence": 8,
          "ordinaryReadCount": "(n + 7) / 8 for n nonempty synchronizations after deadline installation",
          "forcedReads": "One additional armed-deadline read per unlatched nonterminal error settlement that reaches the clock branch; zero when reduction charging fails or no deadline is armed.",
          "successfulForcedReadReset": 7,
          "syntheticReductions": 0,
          "pendingChargeMultiplicity": 1,
          "addedFields": 0,
          "addedSettlementAllocations": "Zero for standard precreated error inputs; custom As may require one shared lazy target cell, preserving stdlib semantics.",
          "evalStateBytes": 192,
          "allocationPin": "queue-promote 174, unchanged; all existing pins unchanged",
          "goldsetBytesDelta": "All 52 controlled cells: classifier-restored candidate equals base; final candidate equals control or removes only classifier target allocations; both single-P repetitions exact. Raw B/op diagnostic only.",
          "cpuAcceptance": "Exact counting-clock contract plus unchanged hosted gate; CPU profiles diagnostic only, approved cost-scope amendment."
        },
        "identifiers": [
          "BuiltinWorkBudget",
          "Finish",
          "Flush",
          "Step",
          "nowFunc",
          "context.DeadlineExceeded",
          "context.Canceled",
          "IsTerminalEvalError",
          "EvalMeterFrom",
          "Snapshot"
        ],
        "testCases": {
          "TestBuiltinWorkBudget_FinishForcesDeadlineAfterError": "Prime each phase 1..7 before expiry, then return nonterminal input at deadline equality and after deadline; cover zero and nonzero pending. Require exact bare DeadlineExceeded and exactly one added read.",
          "TestBuiltinWorkBudget_FinishPreservesSuccessfulCadence": "Finish(nil) across short fresh budgets retains exact ordinary read count and empty no-op. Existing ShortCallsShareClockCadence remains untouched.",
          "TestBuiltinWorkBudget_FinishPreservesErrorPrecedence": "Table nil/nonterminal/terminal input, no deadline/live/expired deadline, live/canceled parent and simultaneous reduction crossing. Preserve incoming terminal and nonterminal identities when selected; observe cancellation with zero pending and no deadline.",
          "TestBuiltinWorkBudget_FinishChargesPendingOnce": "Snapshot actual reductions/allocations before and after first/repeated settlement for nil/nonterminal/terminal inputs and zero/nonzero pending. Retrying with drained pending adds no reductions and no allocation charge.",
          "TestBuiltinWorkBudget_FinishReplaysLatchedError": "Induce terminal sync error through real public budget operations, then repeat Step/Flush/Finish with nil and ordinary errors; same latch by identity and no clock calls. A distinct terminal input still wins by identity without disturbing later latch replay.",
          "TestBuiltinWorkBudget_FinishResetsDeadlineCadence": "Prime mid-cadence, perform a successful forced check against future deadline with ordinary input, then drive 7 ordinary nonempty syncs without another read and observe next read on sync 8."
        }
      }
    },
    {
      "id": "finish-documentation",
      "summary": "NO-RED-WAIVER: NO-TESTER-WAIVER: update existing release notes and metering ADR for the approved error-settlement boundary; documentation only.",
      "tasks": [
        "3.2"
      ],
      "redTasks": [],
      "codeTasks": [
        "3.2"
      ],
      "contract": {}
    },
    {
      "id": "terminal-classifier-allocation",
      "summary": "Classify standard errors without allocation while retaining exact errors.Is/errors.As behavior. Append-only tests precede the core/error.go-only implementation.",
      "tasks": [
        "1.5",
        "2.7"
      ],
      "redTasks": [
        "1.5"
      ],
      "codeTasks": [
        "2.7"
      ],
      "contract": {
        "states": [
          "nil",
          "sentinel-match",
          "ordinary-leaf",
          "first-typed-ordinary",
          "first-typed-resource",
          "single-wrapper",
          "joined-tree",
          "custom-As-unseen",
          "custom-As-true",
          "custom-As-false",
          "custom-target-retained",
          "no-match",
          "Finish-empty",
          "Finish-pending",
          "Finish-latched",
          "typed-search"
        ],
        "transitions": [
          {
            "input": "nil",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Return false without typed search or allocation."
          },
          {
            "input": "Any errors.Is match",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Preserve current full canceled traversal followed, only if needed, by full deadline traversal. Match anywhere in the tree outranks first typed-error classification; do not invoke As afterward."
          },
          {
            "input": "Direct *LispicoError reached by typed search",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Stop at this first typed match even if Code is ordinary and a deeper/later resource error exists. If a custom target was already materialized, write this pointer into that same target."
          },
          {
            "input": "Unwrap() error",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Call once in the typed traversal at this node and continue iteratively; preserve prior errors.Is traversals unchanged."
          },
          {
            "input": "Unwrap() []error",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Call once in typed traversal, then visit children depth-first and left-to-right. Ignore nil children and stop on the first typed/custom match, not the first resource match."
          },
          {
            "input": "First encountered As(any) bool hook",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Allocate target cell once for this classification and call errors.As on that exact current subtree. No restart from root or extra Unwrap. Its hook invocation ordering and descendants are handled by stdlib."
          },
          {
            "input": "Custom subtree returns false",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Continue later siblings with the same target cell and any mutation retained in it. Ignore its pointer value as a match unless found is true."
          },
          {
            "input": "Custom subtree returns true",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Stop with target exactly as custom hook set it, whether resource or ordinary. Preserve the existing panic if custom As claims a nil typed match; do not silently reinterpret invalid custom behavior."
          },
          {
            "input": "Custom hook retains target, later direct match succeeds",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Retained target reflects the eventual direct match just as errors.As writes into its original target."
          },
          {
            "input": "Finish called with standard terminal input",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Existing ordinary Flush semantics and original input identity remain; no forced deadline read merely for terminal classification."
          },
          {
            "input": "Finish called with standard/custom nonterminal input",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "Existing forced deadline/cancellation check remains even with zero pending; terminal synchronization result wins, accounting charges only real units once."
          },
          {
            "input": "Any latched budget",
            "effect": "no-op",
            "state": "typed-search",
            "evidence": "No new synchronization; Finish still preserves supplied terminal identity versus latch according to unchanged existing rules."
          }
        ],
        "forbidden": [
          "Changing the order, number or short-circuiting of the existing two errors.Is calls.",
          "Stopping at the first resource error instead of the first *LispicoError/custom As match.",
          "Flattening or breadth-first traversal of errors.Join or multi-%w trees.",
          "Restarting from the root after custom-hook discovery; custom Unwrap methods may be stateful.",
          "Separate lazy target cells for sibling branches, or discarding a false-returning custom As target mutation.",
          "Allocating a target before a custom As hook is reached; using a pool or mutable global scratch.",
          "Calling errors.AsType, raising go.mod's Go version, adding dependencies, a public helper, a generic error traversal framework or new production types.",
          "Guaranteeing zero allocations inside arbitrary custom Is, As or Unwrap methods.",
          "Weakening existing tests/pins, editing Finish/Step/Flush/cadence/reset paths, synthetic reductions or additional budget/state fields.",
          "Recovery, nil sanitization, traversal limits or cycle detection that changes existing error semantics."
        ],
        "seeding": [
          "Build all standard errors before allocation measurement: errors.New, NewTypeError, NewResourceLimitError, fmt.Errorf with single %w, fmt.Errorf with two %w, errors.Join, and nested LispicoError Cause fields. These constructors' own allocations are outside the measured classification/settlement.",
          "Use testing.AllocsPerRun(1000, closure), matching core/hashmap_test.go:76. Allocation tests and subtests stay sequential. Store/validate result without constructing errors, formatting messages or calling Fatal inside the measured success path.",
          "Standard rows: nil; plain ordinary; typed ordinary; wrapped plain; wrapped typed; bare/wrapped resource; bare/wrapped canceled and deadline; joined ordinary-only; joined ordinary typed before resource; joined resource before ordinary typed; nested ordinary typed containing resource; nested typed containing canceled/deadline. Record expected classification explicitly.",
          "For Finish allocation rows create context and budget before AllocsPerRun. Use budgetCtx with ceiling 1_000_000 and no deadline. Reuse the budget; call zero or three Step operations inside each measured invocation, then Finish. At most 3003 reductions for the 1001 calls including warm-up. Supplied operation errors must not latch.",
          "For latched Finish rows induce cancellation through a real context.WithCancel parent, one Step and Flush before measurement. Reuse the latched budget; assert return identity for ordinary versus supplied terminal inputs.",
          "Proposed test-only terminalErrorHook has optional Is/As callbacks and a single-error Unwrap callback so tests can record exact calls and target identity; terminalErrorList can supply a fixed []error containing nil to pin multi-error nil handling. No fixtures in production.",
          "Semantic fixtures use fresh state for each assertion. Compare explicit expected results and selected hook-call traces with direct errors.Is/errors.As behavior, not the proposed private helper.",
          "Finish custom-classification cases use budgetCtx, WithEvalDeadline, NewBuiltinWorkBudget, Step and Flush to prime exactly three nonempty synchronizations. Swap nowFunc sequentially with t.Cleanup restoration, advance clock to equality, then Finish with zero/three pending units. Terminal inputs return original identity with zero clock reads; nonterminal inputs return bare context.DeadlineExceeded with exactly one read. Never assign private cadence/pending/latch fields."
        ],
        "semanticCases": [
          "errors.Join(typeError, resource) is nonterminal; reversed order is terminal.",
          "Outer ordinary LispicoError with resource Cause remains nonterminal; outer resource with ordinary Cause is terminal.",
          "A canceled/deadline descendant still makes an outer ordinary typed error terminal because errors.Is runs before typed search.",
          "Nested joins establish depth-first order; multi-%w wrappers follow their Unwrap() []error order; nil children are skipped.",
          "Custom As true with resource target is terminal; true with ordinary target masks deeper/later resource; false allows descendants and later siblings.",
          "Custom As false can mutate target to resource yet no eventual match must still classify false.",
          "Custom As false in an earlier sibling and a later custom As must receive the same target pointer and its prior value.",
          "Custom As can retain its target pointer while returning false; a later direct typed match must update that retained cell.",
          "Custom Is matching canceled short-circuits deadline traversal and As; matching only deadline runs canceled pass first then short-circuits As.",
          "Custom Unwrap call traces remain those of the original two sentinel searches and one typed search; no preflight traversal.",
          "Custom As true with nil target preserves baseline panic, using a fixture whose sentinel traversals themselves do not panic.",
          "Custom terminal/nonterminal classification feeds unchanged Finish identity and forced-observation behavior with both empty and pending budgets."
        ],
        "numericBudgets": {
          "standardClassifierAllocsPerCall": 0,
          "standardFinishAllocsPerCall": 0,
          "measuredCalls": 1000,
          "allAllocsPerRunCallsIncludingWarmup": 1001,
          "customAsTargetCellsPerTypedSearchAtMost": 1,
          "customHookAllocationGuarantee": "None for arbitrary custom method implementations; at most one explicit lazy target cell is allocated by the typed search. Zero applies when no custom As is reached and traversed methods themselves allocate nothing.",
          "productionFilesChanged": 1,
          "newPrivateFunctions": 1,
          "newNamedProductionTypes": 0,
          "dependencyChanges": 0,
          "minimumGo": "1.24.0",
          "ordinaryDeadlineCadence": 8,
          "successfulDeadlineReset": 7,
          "addedBudgetOrEvalStateFields": 0,
          "evalStateBytes": 192,
          "syntheticReductions": 0
        },
        "implementation": {
          "changedFiles": [
            "core/error.go"
          ],
          "unchanged": [
            "core/builtin_budget.go",
            "core/eval.go",
            "all five Finish consumers",
            "go.mod",
            "go.sum",
            "all existing test bodies and allocation pins"
          ],
          "sites": [
            {
              "file": "core/error.go",
              "symbol": "IsTerminalEvalError",
              "anchor": "func IsTerminalEvalError(err error) bool {",
              "change": "Retain nil fast path and the existing two short-circuited errors.Is calls verbatim. Replace only the var lerr / errors.As block with a local lazy target slot, the private typed search and the same found-and-CodeResourceLimit decision. Do not add a nil guard that changes existing custom-As nil-result panic behavior."
            },
            {
              "file": "core/error.go",
              "symbol": "asLispicoError (proposed new private function)",
              "anchor": "// NewReadError builds a LispicoError for a tokenizer/parser failure at the",
              "change": "Insert immediately before this unique existing comment. Concrete helper, no new named interface/type or generic API. Walk single Unwrap chains iteratively; recurse only through multi-error children. Allocate one **LispicoError slot only when custom As is reached; share that slot across siblings."
            }
          ],
          "sketch": "func IsTerminalEvalError(err error) bool {\n\tif err == nil {\n\t\treturn false\n\t}\n\tif errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {\n\t\treturn true\n\t}\n\tvar target **LispicoError\n\tlerr, ok := asLispicoError(err, &target)\n\treturn ok && lerr.Code == CodeResourceLimit\n}\n\nfunc asLispicoError(err error, target ***LispicoError) (*LispicoError, bool) {\n\tfor err != nil {\n\t\tif lerr, ok := err.(*LispicoError); ok {\n\t\t\tif *target != nil {\n\t\t\t\t**target = lerr\n\t\t\t}\n\t\t\treturn lerr, true\n\t\t}\n\t\tif _, ok := err.(interface{ As(any) bool }); ok {\n\t\t\tif *target == nil {\n\t\t\t\t*target = new(*LispicoError)\n\t\t\t}\n\t\t\tok := errors.As(err, *target)\n\t\t\treturn **target, ok\n\t\t}\n\t\tswitch x := err.(type) {\n\t\tcase interface{ Unwrap() error }:\n\t\t\terr = x.Unwrap()\n\t\tcase interface{ Unwrap() []error }:\n\t\t\tfor _, child := range x.Unwrap() {\n\t\t\t\tif lerr, ok := asLispicoError(child, target); ok {\n\t\t\t\t\treturn lerr, true\n\t\t\t\t}\n\t\t\t}\n\t\t\treturn nil, false\n\t\tdefault:\n\t\t\treturn nil, false\n\t\t}\n\t}\n\treturn nil, false\n}",
          "invariant": "The pointer-to-pointer-to-pointer is the address of a stack-local lazy target slot, not global storage. Only the new(*LispicoError) cell enters arbitrary custom As code. If a previous custom As retained or modified that cell before returning false, every later sibling sees the same cell; a later direct typed match must also update it before returning. An unmatched subtree may leave a nonnil cell value, but only the explicit found boolean decides whether a match exists.",
          "comments": "Keep existing comments. If the shared-target update needs explanation, allow one concise WHY/invariant comment explaining preservation of a target retained by a prior custom As; no traversal narration."
        },
        "testCases": [
          "errors.Join(typeError, resource) is nonterminal; reversed order is terminal.",
          "Outer ordinary LispicoError with resource Cause remains nonterminal; outer resource with ordinary Cause is terminal.",
          "A canceled/deadline descendant still makes an outer ordinary typed error terminal because errors.Is runs before typed search.",
          "Nested joins establish depth-first order; multi-%w wrappers follow their Unwrap() []error order; nil children are skipped.",
          "Custom As true with resource target is terminal; true with ordinary target masks deeper/later resource; false allows descendants and later siblings.",
          "Custom As false can mutate target to resource yet no eventual match must still classify false.",
          "Custom As false in an earlier sibling and a later custom As must receive the same target pointer and its prior value.",
          "Custom As can retain its target pointer while returning false; a later direct typed match must update that retained cell.",
          "Custom Is matching canceled short-circuits deadline traversal and As; matching only deadline runs canceled pass first then short-circuits As.",
          "Custom Unwrap call traces remain those of the original two sentinel searches and one typed search; no preflight traversal.",
          "Custom As true with nil target preserves baseline panic, using a fixture whose sentinel traversals themselves do not panic.",
          "Custom terminal/nonterminal classification feeds unchanged Finish identity and forced-observation behavior with both empty and pending budgets."
        ],
        "companionRun": "go test -timeout 2m ./core/ -run '^(TestIsTerminalEvalError_(TraversalSemantics|CustomHooks)|TestBuiltinWorkBudget_FinishCustomErrorClassification)$'"
      }
    }
  ],
  "requirements": [
    {
      "shall": "The core evaluator SHALL carry a per-call reduction counter and a per-call cumulative allocation counter on the `evalState` of every evaluation, never shared across concurrent evaluations on the same engine. Step definitions are per-execution-path and normative: the Evaluator SHALL charge one reduction per apply-trampoline iteration and per form dispatch; the VM SHALL charge one reduction per instruction decode; macro expansion SHALL charge one per expansion step; the compiler SHALL charge one per emitted instruction, and compilation allocation SHALL charge the evaluation that triggered compilation before any chunk is cached; GoFunc dispatch SHALL charge one reduction plus the result's shallow allocation size at the centralized apply sites, unless the callee has already accounted for that same value, including a zero-byte account for a wholly borrowed result, in which case the apply site SHALL NOT charge it again.",
      "tests": [
        "TestLimits_MeteringCounterIsolationRace",
        "TestEval_ContextCancellation_StraightLineBudget",
        "TestMetering_FusedArithmeticChargesLedger",
        "TestChargeGoFuncResultBytes_ZeroByteBorrowed_Evaluator"
      ]
    },
    {
      "shall": "The core SHALL provide a local work budget for Builtin phases whose uninterrupted Go work scales with evaluated input and does not otherwise re-enter the Evaluator or VM for that unit. A phase using this facility SHALL accrue one local step per semantic unit. The budget SHALL synchronize with shared evaluation state every 128 pending units and SHALL flush any remainder before every return. Synchronization SHALL charge the logical Reductions and observe the Engine-owned Evaluation deadline and caller cancellation. Observing the deadline SHALL NOT require a wall-clock read at every synchronization: the core MAY read the clock at a reduced, fixed multiple of the synchronization interval. That cadence SHALL be carried by the evaluation rather than by the budget, because a budget is confined to one GoFunc call and a per-budget cadence would read the clock once per call however short the call is. The interval between a deadline passing and a Builtin terminating SHALL be bounded and documented: no more than a small fixed number of synchronizations, plus any single opaque phase's own execution time as today. Caller cancellation and the Reduction charge SHALL still occur at every synchronization. Installing a deadline on an evaluation SHALL make the next synchronization read the clock, so no evaluation inherits a cadence position from an earlier one. No atomic ledger operation or clock read SHALL occur per local step. A consumer SHALL NOT replace the facility with a direct per-unit ledger charge, per-unit evaluation-state poll, or caller-context check, and SHALL NOT double-charge callback execution already accounted by re-entry. When a consumer assigns a budget to a callback-driven operation, separate uninterrupted copying, traversal, and result-construction phases SHALL retain their own ownership.",
      "tests": [
        "TestBuiltinWorkBudget_ShortCallsShareClockCadence",
        "TestBuiltinWorkBudget_DeadlineCrossingBoundedBySynchronizations",
        "TestBuiltinWorkBudget_CancellationUnconditionalMidCadence",
        "TestBuiltinWorkBudget_ReductionChargeUnconditionalMidCadence",
        "TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync",
        "TestBuiltinWorkBudget_StepsLocalUntil128thUnit"
      ]
    },
    {
      "shall": "The Go API SHALL expose `NewBuiltinWorkBudget(context.Context)`, `(*BuiltinWorkBudget).Step() error`, `(*BuiltinWorkBudget).Flush() error`, and `(*BuiltinWorkBudget).Finish(error) error`. A budget SHALL be confined to one GoFunc call and goroutine, SHALL latch and replay its first synchronization error, and SHALL make an empty successful flush idempotent. If a pending non-Terminal error and a Terminal flush error coexist, the Terminal error SHALL win; otherwise the original validation/callback error SHALL be preserved.",
      "tests": [
        "TestBuiltinWorkBudget_FlushIdempotentEmpty",
        "TestBuiltinWorkBudget_LatchesFirstSyncError",
        "TestBuiltinWorkBudget_StepsLocalUntil128thUnit"
      ]
    },
    {
      "shall": "Before a VM dispatches a GoFunc, its re-entry context SHALL carry the absolute deadline already resolved for that VM run. An earlier non-zero deadline from an outer evaluation SHALL win. Starting a Builtin or nested evaluator callback SHALL NOT compute a fresh `now + timeout` deadline or otherwise restore time already consumed by compilation or bytecode execution.",
      "tests": [
        "TestCallReentrancy_VMAbsoluteDeadline_OuterEarlierWins",
        "TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline",
        "TestCallReentrancy_NestedCallbackInheritsVMAbsoluteDeadline"
      ]
    },
    {
      "shall": "Every consumer migrated to this contract SHALL assign an accounting owner to each reachable helper phase that scales with user input. An opaque scalable library/helper call SHALL be replaced by an interruptible budgeted kernel, rejected before entry by a deterministic input/work ceiling, or documented as a bounded exception with its proof and maximum work. A check only before and after opaque work SHALL NOT count as bounded interruption.",
      "tests": [
        "TestWorkInventory_MatchesSource",
        "TestWorkInventory_BoundedExceptionsCarryProofAndMaxWork",
        "TestWorkInventory_CoversEveryRegisteredName"
      ]
    },
    {
      "shall": "Calls into methods of host-provided Go `Value` implementations SHALL be recorded as trusted-host boundaries and are outside the core-owned interruption guarantee. Core-owned `Value` formatting, equality, hashing, and traversal receive no such exception and SHALL satisfy the interruptible/bounded rule.",
      "tests": [
        "TestWorkInventory_TrustedHostRowsNameAllowedCallee"
      ]
    },
    {
      "shall": "Builtin kernels and result validators SHALL obtain collection-length and construction-depth limits from the active evaluator passed to the GoFunc. They SHALL NOT use `env.Evaluator()` as dynamic policy because a child lexical environment need not own the evaluator currently executing it.",
      "tests": [
        "TestLookup_ActiveEvaluatorPolicyNestedLambda",
        "TestStdlibSources_NoEnvEvaluatorInBuiltins"
      ]
    },
    {
      "shall": "Counter values are NOT required to be equal across evaluators, and are NOT required to be equal across compiler versions for the same evaluator and the same source: because the VM charges one reduction per instruction decode, a compiler change that alters how many instructions a given form compiles to (a superinstruction fusing what were previously several instructions into one, for example) changes the reductions charged for evaluating that form, even with the evaluator and the source both unchanged. A resource-limit ceiling a program previously crossed at some iteration count MAY no longer be crossed at that count after such a compiler change; this is expected, not a defect, but SHALL be disclosed wherever a compiler change is documented as altering instruction count.",
      "tests": []
    },
    {
      "shall": "Reduction accounting, including synchronized Builtin batches, SHALL use the existing evaluation state. Exceeding either ceiling SHALL raise `Code: \"ResourceLimitError\"`, terminal per the non-catchability requirement, on both execution paths. A failed full or partial batch flush SHALL be returned before a result is published.",
      "tests": [
        "TestBuiltinWorkBudget_TerminalFlushWins_OriginalErrorPreserved",
        "TestBuiltinWorkBudget_EvaluatorGoFunc_TerminalUnderLowReductions"
      ]
    },
    {
      "shall": "Allocation charging SHALL be incremental for structurally derived results: an operation whose result shares substructure with one of its arguments SHALL charge the storage it newly allocated, not a deep measure of the whole result, because the shared substructure was charged when it was created. The core SHALL support a zero-byte callee disposition meaning that the returned argument, stored member, caller-supplied default, or other value is wholly borrowed; the centralized apply site SHALL not charge that declared storage again. An operation that builds a sequence or map from unrelated values SHALL charge the result's deep size. Retained-state accounting SHALL keep using a deep measure, since a binding holds its whole reachable structure alive.",
      "tests": [
        "TestChargeGoFuncResultBytes_ZeroByteBorrowed_Evaluator",
        "TestChargeGoFuncResultBytes_ZeroByteBorrowed_VM",
        "TestChargeGoFuncResultBytes_MixedIncrementalChargesFreshOnly"
      ]
    },
    {
      "shall": "- **WHEN** a loop extends an accumulator N times and each result shares the previous accumulator's storage - **THEN** the total allocation charged SHALL grow linearly in N, and no iteration SHALL charge the shared prefix again",
      "tests": [
        "TestAccumulation100k",
        "TestMapAccumulation20k"
      ]
    },
    {
      "shall": "- **WHEN** a GoFunc charges the ledger for the value it returns, and that value is then returned through a centralized apply site - **THEN** the apply site SHALL NOT add its own shallow charge for that value, so a loop of N such calls charges O(N) in total rather than a size-proportional amount per call",
      "tests": [
        "TestChargeGoFuncResultBytes_ZeroByteBorrowed_Evaluator"
      ]
    },
    {
      "shall": "- **WHEN** a Builtin returns an existing argument, stored collection, or caller-supplied default without allocating result storage - **THEN** it SHALL mark zero result-allocation bytes and the apply site SHALL NOT charge the borrowed value's shallow size",
      "tests": [
        "TestBorrowed_GetReturnsStoredCollection",
        "TestBorrowed_GetDefaultIsBorrowed",
        "TestBorrowed_GetInStoredValueAndEmptyPathSubject"
      ]
    },
    {
      "shall": "- **WHEN** a GoFunc returns a value without charging the ledger for it - **THEN** the apply site SHALL charge that value's shallow allocation size, and a callee's own nested evaluation SHALL NOT be mistaken for the callee having charged its result",
      "tests": [
        "TestChargeGoFuncResultBytes_ZeroByteBorrowed_Reentry"
      ]
    },
    {
      "shall": "- **WHEN** `list`, `vector`, `range`, or `json/decode` builds a result from values that share nothing with an argument - **THEN** the evaluation SHALL be charged the result's deep allocation size",
      "tests": [
        "TestDecodeChargesDeepResultBytes"
      ]
    },
    {
      "shall": "- **WHEN** an Engine runs a loop that allocates faster than it reduces, configured with `MaxAllocationBytes: 1<<20` - **THEN** evaluation SHALL fail with `Code: \"ResourceLimitError\"` before the host is exhausted, and `try`/`catch` SHALL NOT intercept the error",
      "tests": [
        "TestLimits_MeteringAdversariesTripTightLimits",
        "TestLimits_MeteringTryCatchNotCatchable"
      ]
    },
    {
      "shall": "- **WHEN** an Engine runs a macro-amplified recursion that exceeds `MaxReductions` before tripping `MaxDepth` - **THEN** evaluation SHALL fail with `Code: \"ResourceLimitError\"`",
      "tests": [
        "TestLimits_MeteringAdversariesTripTightLimits"
      ]
    },
    {
      "shall": "- **WHEN** a Builtin performs a long uninterrupted input-sized loop under an exhausted Reduction budget - **THEN** its next bounded budget synchronization SHALL stop with `Code: \"ResourceLimitError\"` rather than allowing one dispatch to run unbounded",
      "tests": [
        "TestBuiltinWorkBudget_EvaluatorGoFunc_TerminalUnderLowReductions",
        "TestGetIn_ReductionBudgetExhausted"
      ]
    },
    {
      "shall": "- **WHEN** a Builtin completes 127 uninterrupted logical work units - **THEN** those steps SHALL use only its local counter, and the final flush SHALL perform one shared synchronization for the remainder",
      "tests": [
        "TestBuiltinWorkBudget_StepsLocalUntil128thUnit"
      ]
    },
    {
      "shall": "- **WHEN** scalable Builtin work runs after the Engine-owned Evaluation deadline expires while the caller context remains live - **THEN** budget synchronization SHALL return `context.DeadlineExceeded` within the documented number of synchronizations of the expiry",
      "tests": [
        "TestBuiltinWorkBudget_DeadlineCrossingBoundedBySynchronizations"
      ]
    },
    {
      "shall": "- **WHEN** an evaluation with an armed deadline calls many short Builtins, each constructing its own work budget and flushing a small remainder on return - **THEN** the wall-clock reads SHALL be bounded by the documented fraction of synchronizations rather than reaching one per Builtin call",
      "tests": [
        "TestBuiltinWorkBudget_ShortCallsShareClockCadence"
      ]
    },
    {
      "shall": "- **WHEN** an Evaluation deadline is installed and a Builtin then synchronizes its budget - **THEN** that synchronization SHALL read the clock rather than continue a cadence position established before the deadline existed",
      "tests": [
        "TestBuiltinWorkBudget_InstalledDeadlineReadAtNextSync",
        "TestBuiltinWorkBudget_FreshEvalStateReadsClockAtFirstSync"
      ]
    },
    {
      "shall": "- **WHEN** a VM resolves its Evaluation deadline, performs substantial bytecode work, and then enters a long Builtin - **THEN** the Builtin SHALL observe that same absolute deadline rather than receiving a new timeout interval",
      "tests": [
        "TestCallReentrancy_VMAbsoluteDeadlineNotRestarted"
      ]
    },
    {
      "shall": "- **WHEN** a Builtin re-enters the Evaluator or VM once for every input element and performs no separate scalable uninterrupted phase - **THEN** those execution steps SHALL account for callback execution, while any separate input-copying, traversal, or result-construction phase SHALL retain its own Builtin work charge",
      "tests": [
        "TestHigherOrder_CallbackCountUnchanged",
        "TestHigherOrder_TerminalUnderLowReductions"
      ]
    },
    {
      "shall": "- **WHEN** an active Builtin would call an opaque helper whose work scales with user input - **THEN** it SHALL use an interruptible kernel, enforce a deterministic pre-entry work bound, or carry a reviewed bounded-exception proof in the frozen inventory",
      "tests": [
        "TestWorkInventory_MatchesSource",
        "TestWorkInventory_BoundedExceptionsCarryProofAndMaxWork"
      ]
    },
    {
      "shall": "- **WHEN** a Builtin executes in a child lexical environment without its own evaluator - **THEN** its collection-length and construction-depth checks SHALL still use the limits of the evaluator that dispatched the GoFunc",
      "tests": [
        "TestLookup_ActiveEvaluatorPolicyNestedLambda"
      ]
    },
    {
      "shall": "- **WHEN** the caller's context is cancelled mid-evaluation - **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation",
      "tests": [
        "TestEval_ContextCancellation_StraightLineBudget",
        "TestEval_Loop_ContextCancellation"
      ]
    },
    {
      "shall": "- **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race` - **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race",
      "tests": [
        "TestLimits_MeteringCounterIsolationRace"
      ]
    },
    {
      "shall": "- **WHEN** the same adversarial program is evaluated by the Evaluator and VM under identical limits - **THEN** both SHALL terminate with the same terminal error class; counter values MAY differ between evaluators",
      "tests": [
        "TestVMVsTreeWalker_TerminalErrorNotCaught",
        "TestVMVsTreeWalker_ReentrantCallDepthBound_BoundaryMatrix"
      ]
    },
    {
      "shall": "- **WHEN** a host invokes a Lisp function via `core.Evaluator.Apply` without going through an `Engine` entry point, under tight limits - **THEN** the evaluation SHALL be bounded by the same per-evaluation counters",
      "tests": [
        "TestMeter_DirectEvaluatorApplyUsesContextMeter",
        "TestChargeGoFuncResultBytes_ZeroByteBorrowed_DirectApply"
      ]
    },
    {
      "shall": "- **WHEN** a compiler version change alters how many VM instructions a given form compiles to (e.g. fusing several instructions into one), and the same source is evaluated against the same `MaxReductions` ceiling on both compiler versions - **THEN** the iteration count at which the ceiling is crossed MAY differ between the two versions, and this difference SHALL NOT be treated as a counter-consistency defect",
      "tests": []
    },
    {
      "shall": "Settling a pending non-Terminal error through `Finish` SHALL check the armed Evaluation deadline and caller cancellation even between scheduled clock reads or when no local work remains pending. A reduction-limit failure SHALL retain precedence over that check. Settling a nil error SHALL retain ordinary `Flush` behavior; an existing Terminal input error SHALL retain its identity. Consumers SHALL settle pending validation/callback errors through this operation before returning them. Forced error settlement SHALL NOT charge local work twice.",
      "tests": [
        "TestBuiltinWorkBudget_FinishForcesDeadlineAfterError",
        "TestBuiltinWorkBudget_FinishPreservesSuccessfulCadence",
        "TestBuiltinWorkBudget_FinishPreservesErrorPrecedence",
        "TestBuiltinWorkBudget_FinishChargesPendingOnce",
        "TestBuiltinWorkBudget_FinishReplaysLatchedError",
        "TestBuiltinWorkBudget_FinishResetsDeadlineCadence",
        "TestCLAdapters_LateVMDeadline"
      ]
    }
  ],
  "testHarness": [
    "budgetCtx — core/builtin_budget_test.go:12-14 — returns a context carrying a fresh eval state via WithEvalResourceLimits(parent, maxReductions, 1<<30) (effectively unlimited allocation ceiling)",
    "errCode — core/builtin_budget_test.go:17-24 — extracts the LispicoError.Code from err via errors.As, t.Fatalf if err is not a *LispicoError",
    "stepN — core/builtin_budget_test.go:27-34 — performs n Step() calls on a *BuiltinWorkBudget, t.Fatalf on any error before the expected sync point",
    "Read1 — core/builtin_budget_test.go:227-237 — reads exactly one form from a source string via Read(), t.Fatalf on parse error or form count != 1",
    "newTestEnv — core/eval_test.go:45 — builds the *Env used by builtin_budget_test.go's GoFunc-dispatch tests (spin closures)",
    "nowFunc swap — core/vm/deadline_clock_cadence_test.go:25-30 (and similarly 58-65, 95-97, 120-146) — saves the package var nowFunc, replaces it with a counting/scripted stub, restores via t.Cleanup; this is the pattern the new core package's clock seam test must copy once nowFunc is introduced in core",
    "WithEvalDeadline — core/builtin_budget_test.go:120 — used directly to arm ctx.deadline in TestBuiltinWorkBudget_DeadlineObservedAtSyncWithLiveParentCtx; this is the production entry point task 2.3's second reset site instruments",
    "Finish amendment — core/builtin_budget_test.go: budgetCtx, WithEvalDeadline, stepN and controlled nowFunc seed state without private field mutation; EvalMeterFrom(ctx).Snapshot reads exact accounting. Existing runtime TestCLAdapters_LateVMDeadline is executed unchanged before and after consumer migration.",
    "Build all standard errors before allocation measurement: errors.New, NewTypeError, NewResourceLimitError, fmt.Errorf with single %w, fmt.Errorf with two %w, errors.Join, and nested LispicoError Cause fields. These constructors' own allocations are outside the measured classification/settlement.",
    "Use testing.AllocsPerRun(1000, closure), matching core/hashmap_test.go:76. Allocation tests and subtests stay sequential. Store/validate result without constructing errors, formatting messages or calling Fatal inside the measured success path.",
    "Standard rows: nil; plain ordinary; typed ordinary; wrapped plain; wrapped typed; bare/wrapped resource; bare/wrapped canceled and deadline; joined ordinary-only; joined ordinary typed before resource; joined resource before ordinary typed; nested ordinary typed containing resource; nested typed containing canceled/deadline. Record expected classification explicitly.",
    "For Finish allocation rows create context and budget before AllocsPerRun. Use budgetCtx with ceiling 1_000_000 and no deadline. Reuse the budget; call zero or three Step operations inside each measured invocation, then Finish. At most 3003 reductions for the 1001 calls including warm-up. Supplied operation errors must not latch.",
    "For latched Finish rows induce cancellation through a real context.WithCancel parent, one Step and Flush before measurement. Reuse the latched budget; assert return identity for ordinary versus supplied terminal inputs.",
    "Proposed test-only terminalErrorHook has optional Is/As callbacks and a single-error Unwrap callback so tests can record exact calls and target identity; terminalErrorList can supply a fixed []error containing nil to pin multi-error nil handling. No fixtures in production.",
    "Semantic fixtures use fresh state for each assertion. Compare explicit expected results and selected hook-call traces with direct errors.Is/errors.As behavior, not the proposed private helper.",
    "Finish custom-classification cases use budgetCtx, WithEvalDeadline, NewBuiltinWorkBudget, Step and Flush to prime exactly three nonempty synchronizations. Swap nowFunc sequentially with t.Cleanup restoration, advance clock to equality, then Finish with zero/three pending units. Terminal inputs return original identity with zero clock reads; nonterminal inputs return bare context.DeadlineExceeded with exactly one read. Never assign private cadence/pending/latch fields."
  ],
  "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 5
  }
}
```
