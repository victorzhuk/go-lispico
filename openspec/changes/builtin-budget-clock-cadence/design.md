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
deadline error shape unchanged. Change no allocation count and no `B/op` figure.

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
Verified before and after with `unsafe.Sizeof`, and again by comparing `B/op` on every
gold-set cell against a pre-change build — bytes and allocation counts are exact locally,
unlike latency.

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

No data, no schema, no API change. The Go API is untouched; the only observable difference
is when an expired deadline is noticed inside Builtin work. Rollback is the revert of the
three code chunks.

## Implementation plan

Seven chunks, six of them serial in `core` (only the changelog chunk is disjoint), one
post-merge. Mode `existing-service-strict`, tier `standard`, lenses `spec`, `quality`,
`perf`.

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
5. **`cost-verification`** (4.1, 4.3, `coder`) — CPU-profile attribution against a
   `v0.12.0` build, plus `B/op` and allocation counts unchanged on every gold-set cell.
6. **`suite-verification`** (4.2, `coder`) — the floor.
7. **`release-gate-dispatch`** (5.1, `coder`) — verify only; the dispatch resolves its ref
   on the remote and therefore cannot run before the merge is pushed. Task 5.1 stays open
   at merge and closes post-merge on the recorded run id.

**Floor:** `make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint`

**Plan review:** `zarchitect`, 3 rounds, verdict pass. Round 1 raised five blockers —
`unused` rejecting the field-first chunk, a coder being handed test authoring on a sealed
file, the `evalState` size-class regression, an unreachable seeding path for the lazy site,
and a live remote dispatch scheduled before the fix existed. Round 2 raised two more — a
red set containing a test that cannot fail, and the dispatch command still rendering into a
code prompt. All are answered above. Round 3 confirmed the repairs with no blockers.

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
  "baseSha": "d986ea2c8e2cac7dce792ca17b621004c78ef99f",
  "generatedAt": "2026-09-06T13:02:16+03:00",
  "tier": "standard",
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
      "id": "cost-verification",
      "taskIds": [
        "4.1",
        "4.3"
      ],
      "prev": "deadline-install-reset",
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
          "change": "verify only, no edit — profile this cell and confirm time.runtimeNow falls from 11.05% toward the 0.45% v0.12.0 carried. Anchor the bench pattern: an unanchored Goldset/... also matches GoldsetParse and GoldsetCall. Do not write a benchmark-threshold test; this workstation's wall-clock noise band swallows the signal."
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
          "change": "verify only, no edit — compare B/op on every Goldset/* and GoldsetParse/* cell against a pre-change build. Bytes and allocation counts are exact locally, unlike latency, so a size-class shift in evalState is caught here rather than on the runner."
        }
      ],
      "contract": {
        "states": [
          "profiled",
          "pins-unchanged"
        ],
        "transitions": [
          {
            "input": "make profile then pprof -peek on the VM profile",
            "state": "profiled",
            "effect": "forced",
            "evidence": "Makefile:25-28 pins GOMAXPROCS=2 and -benchtime=200ms to mirror the release gate's fixed run parameters, so a local profile is comparable to what the gate measures"
          },
          {
            "input": "an anchored bench pattern",
            "state": "profiled",
            "effect": "forced",
            "evidence": "internal/goldset/bench_test.go:27, 91 — an unanchored Goldset/... also matches BenchmarkGoldsetParse; use ^BenchmarkGoldset$/^queue-promote$"
          },
          {
            "input": "goldset allocation pins after the change",
            "state": "pins-unchanged",
            "effect": "no-op",
            "evidence": "internal/goldset/alloc_test.go:25-39 — this change allocates nothing new; queue-promote stays at 174 and every other pin stays put. A moved pin is a defect in the change, not a number to update"
          }
        ],
        "forbidden": [
          "a wall-clock A/B or git bisect verdict on this workstation",
          "a benchmark-threshold test committed to the repository",
          "editing any number in vmAllocCeilings"
        ],
        "seeding": [
          {
            "state": "profiled",
            "path": "make profile, then go tool pprof against profiles/vm.test + profiles/vm.cpu.prof"
          },
          {
            "state": "pins-unchanged",
            "path": "go test -timeout 2m ./internal/goldset/ (alloc_test.go carries //go:build !race, so it is measured without the detector)"
          }
        ],
        "identifiers": [
          "time.runtimeNow",
          "BenchmarkGoldset",
          "queue-promote",
          "vmAllocCeilings",
          "TestGoldsetVMAllocations",
          "GOLDSET_MODE",
          "ModeEvaluator",
          "ModeVM",
          "profiles/vm.cpu.prof",
          "profiles/vm.test"
        ],
        "numbers": [
          {
            "name": "time.runtimeNow share of the queue-promote VM profile before",
            "value": "11.05%"
          },
          {
            "name": "time.runtimeNow share at v0.12.0 (the target)",
            "value": "0.45%"
          },
          {
            "name": "acceptance ceiling for time.runtimeNow after the change",
            "value": "<= 1.0% flat"
          },
          {
            "name": "queue-promote gate regression before the change",
            "value": "+19.75%"
          },
          {
            "name": "queue-promote allocation pin",
            "value": "174, unchanged"
          },
          {
            "name": "goldset fixtures that must agree under both modes",
            "value": "13"
          },
          {
            "name": "permitted B/op movement on any gold-set cell",
            "value": "0"
          },
          {
            "name": "unsafe.Sizeof(evalState)",
            "value": "192, unchanged"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "4.1",
        "4.3"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "GOMAXPROCS=2 GOLDSET_MODE=vm go test -timeout 2m ./internal/goldset/ -run '^$' -bench '^BenchmarkGoldset$/^queue-promote$' -benchtime=200ms -benchmem -cpuprofile=profiles/vm.cpu.prof -o profiles/vm.test && go tool pprof -peek '^time\\.runtimeNow$' profiles/vm.test profiles/vm.cpu.prof && go test -timeout 2m ./internal/goldset/",
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
          "suite-green"
        ],
        "transitions": [
          {
            "input": "the full floor over the changed tree",
            "state": "suite-green",
            "effect": "set",
            "evidence": "tasks.md 4.2 — repository suite, race suite over core/plugins/runtime, go vet, linter, each exiting successfully"
          }
        ],
        "forbidden": [
          "treating a load-sensitive flake as a regression without re-running it standalone — TestDecodeHashMap_Scaling has ~0.1 headroom on its 3.0 threshold under load"
        ],
        "seeding": [
          {
            "state": "suite-green",
            "path": "run the floor command on the chunk's worktree"
          }
        ],
        "identifiers": [
          "make test",
          "make lint",
          "go vet",
          "TestDecodeHashMap_Scaling"
        ],
        "numbers": [
          {
            "name": "commands that must exit successfully",
            "value": "4"
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
      }
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
      }
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
      }
    },
    {
      "id": "cost-and-suite-verification",
      "summary": "NO-TESTER-WAIVER: the verdict is CPU-profile attribution plus the existing suites — this workstation's wall-clock noise band swallows the signal, so no new test can adjudicate it and no benchmark-threshold test may be written.",
      "tasks": [
        "4.1",
        "4.2",
        "4.3"
      ],
      "contract": {
        "states": [
          "profiled",
          "pins-unchanged"
        ],
        "transitions": [
          {
            "input": "make profile then pprof -peek on the VM profile",
            "state": "profiled",
            "effect": "forced",
            "evidence": "Makefile:25-28 pins GOMAXPROCS=2 and -benchtime=200ms to mirror the release gate's fixed run parameters, so a local profile is comparable to what the gate measures"
          },
          {
            "input": "an anchored bench pattern",
            "state": "profiled",
            "effect": "forced",
            "evidence": "internal/goldset/bench_test.go:27, 91 — an unanchored Goldset/... also matches BenchmarkGoldsetParse; use ^BenchmarkGoldset$/^queue-promote$"
          },
          {
            "input": "goldset allocation pins after the change",
            "state": "pins-unchanged",
            "effect": "no-op",
            "evidence": "internal/goldset/alloc_test.go:25-39 — this change allocates nothing new; queue-promote stays at 174 and every other pin stays put. A moved pin is a defect in the change, not a number to update"
          }
        ],
        "forbidden": [
          "a wall-clock A/B or git bisect verdict on this workstation",
          "a benchmark-threshold test committed to the repository",
          "editing any number in vmAllocCeilings"
        ],
        "seeding": [
          {
            "state": "profiled",
            "path": "make profile, then go tool pprof against profiles/vm.test + profiles/vm.cpu.prof"
          },
          {
            "state": "pins-unchanged",
            "path": "go test -timeout 2m ./internal/goldset/ (alloc_test.go carries //go:build !race, so it is measured without the detector)"
          }
        ],
        "identifiers": [
          "time.runtimeNow",
          "BenchmarkGoldset",
          "queue-promote",
          "vmAllocCeilings",
          "TestGoldsetVMAllocations",
          "GOLDSET_MODE",
          "ModeEvaluator",
          "ModeVM",
          "profiles/vm.cpu.prof",
          "profiles/vm.test"
        ],
        "numbers": [
          {
            "name": "time.runtimeNow share of the queue-promote VM profile before",
            "value": "11.05%"
          },
          {
            "name": "time.runtimeNow share at v0.12.0 (the target)",
            "value": "0.45%"
          },
          {
            "name": "acceptance ceiling for time.runtimeNow after the change",
            "value": "<= 1.0% flat"
          },
          {
            "name": "queue-promote gate regression before the change",
            "value": "+19.75%"
          },
          {
            "name": "queue-promote allocation pin",
            "value": "174, unchanged"
          },
          {
            "name": "goldset fixtures that must agree under both modes",
            "value": "13"
          },
          {
            "name": "permitted B/op movement on any gold-set cell",
            "value": "0"
          },
          {
            "name": "unsafe.Sizeof(evalState)",
            "value": "192, unchanged"
          }
        ]
      }
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
      }
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
      "shall": "The Go API SHALL expose `NewBuiltinWorkBudget(context.Context)`, `(*BuiltinWorkBudget).Step() error`, and `(*BuiltinWorkBudget).Flush() error`. A budget SHALL be confined to one GoFunc call and goroutine, SHALL latch and replay its first synchronization error, and SHALL make an empty successful flush idempotent. If a pending non-Terminal error and a Terminal flush error coexist, the Terminal error SHALL win; otherwise the original validation/callback error SHALL be preserved.",
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
    }
  ],
  "testHarness": [
    "budgetCtx — core/builtin_budget_test.go:12-14 — returns a context carrying a fresh eval state via WithEvalResourceLimits(parent, maxReductions, 1<<30) (effectively unlimited allocation ceiling)",
    "errCode — core/builtin_budget_test.go:17-24 — extracts the LispicoError.Code from err via errors.As, t.Fatalf if err is not a *LispicoError",
    "stepN — core/builtin_budget_test.go:27-34 — performs n Step() calls on a *BuiltinWorkBudget, t.Fatalf on any error before the expected sync point",
    "Read1 — core/builtin_budget_test.go:227-237 — reads exactly one form from a source string via Read(), t.Fatalf on parse error or form count != 1",
    "newTestEnv — core/eval_test.go:45 — builds the *Env used by builtin_budget_test.go's GoFunc-dispatch tests (spin closures)",
    "nowFunc swap — core/vm/deadline_clock_cadence_test.go:25-30 (and similarly 58-65, 95-97, 120-146) — saves the package var nowFunc, replaces it with a counting/scripted stub, restores via t.Cleanup; this is the pattern the new core package's clock seam test must copy once nowFunc is introduced in core",
    "WithEvalDeadline — core/builtin_budget_test.go:120 — used directly to arm ctx.deadline in TestBuiltinWorkBudget_DeadlineObservedAtSyncWithLiveParentCtx; this is the production entry point task 2.3's second reset site instruments"
  ],
  "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 3
  }
}
```
