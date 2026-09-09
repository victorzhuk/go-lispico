## Context

See proposal.md for the reproduced failure. `callBoundary` creates eval state for metered calls; `bytecodeEvaluator.applyOnVM` selects `SetDeadline` whenever state exists, even when its deadline is zero. `SetDeadline` marks the VM deadline armed, so the timeout is never derived. `Fn.Call` and `PinnedFn.Call` share this path.

## Goals / Non-Goals

**Goals:** compose deadline selection with existing state and meter ownership at the common call boundary; preserve already-resolved deadlines and handle parity.

**Non-Goals:** changing lease semantics, clock polling cadence, caller-context wrapping, direct core evaluator configuration, or ordinary evaluation side effects.

## Decisions

### Arm absent bounds without replacing inherited state

Keep the lean path unchanged. On the general boundary, distinguish existing state from an existing deadline: state creation for metering is not evidence that deadline ownership has been resolved. Resolve the configured engine bound when a top-level call lacks one, respecting the caller deadline, then install it into the existing eval state before VM dispatch. Reuse `evalDeadline`, `core.WithEvalDeadline`, and `core.EvalDeadlineFrom`; introduce no second eval state or lease lifecycle.

An inherited nonzero absolute eval deadline remains unchanged on reentry. A zero timeout introduces no bound and cannot erase an inherited one. The tree-walker branch must obey the same preservation rule when it currently calls `core.WithEvalDeadline` unconditionally. The callback timing start is independent of the inherited deadline; callback registration must not reset it.

Changing `SetDeadline` to reinterpret zero globally is rejected: zero is an intentional VM contract, including explicit engine timeout disablement. Allocating a timer context is also rejected by the existing deadline contract.

### Test state and enforcement separately

Existing-service-strict, regression-first. Runtime integration tests capture `core.EvalDeadlineFrom` inside a cooperative GoFunc for the meter/entry-point matrix. They assert absent versus present deadlines and exact equality on reentry. An already-expired inherited eval deadline exercises error propagation without sleeping. Keep the existing bounded long-work timeout regression as an end-to-end check.

Use `runtime/lazy_deadline_test.go`, `runtime/vm_reentry_deadline_test.go`, and `runtime/call_boundary_flag_test.go`. Their package-private `nowFunc` does not control the separate clocks in core and VM; do not assume one injected clock covers all layers. Existing tests for unobserved boundary clock reads guard the lean path.

## Risks / Trade-offs

- Eager deadline resolution on the general metered path adds a clock read when no bound exists → keep the no-meter lean path untouched and preserve an inherited bound without reading the clock again.
- Reentry can arrive with state owned by another boundary → retain its deadline and counters; never call through a fresh independent resource context to reset them.
- Deadline error precedence can change accidentally during unwind → retain existing terminal-error handling and lease-return tests.

## Migration Plan

No API or configuration migration. Update existing deadline docs and CHANGELOG. Reverting this change restores the metered-timeout defect; no stored data changes. Merge before the other two runtime lifecycle changes, with no semantic dependency.

## Implementation plan

Base `5bde17e4` (master). Tier **standard**, mode **existing-service-strict**, lenses
`spec` + `quality` + `perf` (perf: the diff touches the hot call boundary; the change's
own budget is zero added clock reads on the lean path). One seam:
`deadline-ownership-call-boundary` — which absolute deadline is installed into eval
state at the general call boundary shared by `Engine.Call`, `Fn.Call`, `PinnedFn.Call`.

### Chunk graph (dispatch order)

| # | chunk | tasks | shape | coder |
|---|-------|-------|-------|-------|
| 1 | `B-baseline` | 0.1, 0.2 | parallel, shard `baseline` (read-only recon) | zpatcher |
| 2 | `R1-red` | 1.1, 1.2 | serial-first of the runtime chain | go-coder (red stage) |
| 3 | `C1-code` | 2.1, 2.2 | serial, prev `R1-red`, sharedPkg `runtime` | go-coder |
| 4 | `C2-guard` | 2.3 | serial, prev `C1-code`, sharedPkg `runtime` | go-coder |
| 5 | `V-floor` | 3.1, 3.2 | serial, prev `C2-guard`, sharedPkg `runtime` | coder |
| 6 | `F-docs-seal` | 3.3, 3.4 | serial, prev `V-floor`, sharedPkg `runtime` | coder |

`B-baseline` records the `3bcf9c1` baseline and the Makefile facts (no focused-package
target → raw `go test` for narrowed runs). `R1-red` seals and writes the failing
regressions; `C1-code` lands the fix; `C2-guard` proves the lean path untouched;
`V-floor` records the focused run and runs the full floor + race; `F-docs-seal` updates
docs/CHANGELOG and seals the diff. All runtime work is one serial chain (same package);
the only parallelism is the read-only baseline chunk.

### Sites

- `R1-red` — `runtime/lazy_deadline_test.go` @ `TestEngineDeadline_UnobservedBoundaryReadsNoClock`
  and `runtime/vm_reentry_deadline_test.go` @ `TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline`.
  New tests (red, expected fail pre-fix on the meter/no-deadline and tree-walker-clears-inherited cases):
  `TestEngineDeadline_ContextMeterRetainsBound`, `_EngineMeterRetainsBound`,
  `_ExistingStateWithoutDeadlineAcquiresBound`, `_HandlesRetainEngineBound`,
  `_CooperativeGoFuncObservesEngineBound`, `_CallerEarlierDeadlineGoverns`,
  `_CallerLaterDeadlineDoesNotWeakenBound`, `_DisabledTimeoutAddsNoBound`,
  `_DisabledTimeoutPreservesInheritedDeadline`, `_ReentryRetainsAbsoluteDeadlineAndBudget`,
  `_TreeWalkerPreservesInheritedDeadline`.
- `C1-code` — `runtime/eval.go` @ `engineImpl.callBoundary`, anchors
  `needsEvalState := !fast && (core.HasEvalState(ctx) || core.HasEvalMeter(ctx) || e.config.engineMeter != nil)`
  (2.1: arm absent bound after `StartEval` top, install via `core.WithEvalDeadline` into the
  existing state; `applyOnVM`'s state path stays `SetDeadline(EvalDeadlineFrom(ctx))`) and
  `ctx = core.WithEvalDeadline(ctx, deadline)` (2.2: tree-walker installs only when unresolved,
  never zero over nonzero).
- `C2-guard` — `runtime/eval.go` @ `engineImpl.callBoundaryLean` (2.3: untouched; fix fallout only).
- `V-floor` / `F-docs-seal` — record anchors in `runtime/lazy_deadline_test.go`
  (`TestEngineDeadline_BytecodeLazyTimeoutStillFires`), `Makefile` (`test:`, `lint:`),
  `docs/adr/0010-embedder-owned-evaluation-deadlines.md`, `CHANGELOG.md` `[Unreleased]`.

### Seam contract (verbatim from the design packet)

States: `no-state`, `unresolved`, `engine-armed`, `inherited`. One transition row per
input class (meter/state presence × handle × evaluator × top/reentry, plus disablement
and caller-bound composition), effects `set|clear|no-op|forced`, evidence at
`file:lines` — full matrix in the appendix `seams[0].contract`. Key rows:

- top-level + ctx meter / engine meter / existing state w/o deadline, timeout > 0 →
  `engine-armed` (set) — the defect today leaves these `unresolved`.
- existing state with inherited nonzero deadline, any timeout → `inherited` (no-op);
  reentry never re-derives (`installReentrantDeadline` preserves the run instant).
- `WithTimeout(0)` → `unresolved` (no-op); must not clear `inherited`
  (tree-walker `eval.go:991` today does exactly this).
- caller deadline ≤ start+timeout → caller self-enforces (no-op); later caller
  deadline → `engine-armed` (set).
- expiry during VM bytecode → `forced`: VM checkpoint refuses every
  `checkInterval`=128 instructions, `fmt.Errorf("vm: %w", context.DeadlineExceeded)`
  (`core/vm/vm.go:878`) — tests assert `errors.Is(err, context.DeadlineExceeded)`
  surviving the `vm: ` wrap. Expiry inside a cooperative GoFunc → `forced`: bare
  `context.DeadlineExceeded` from `BuiltinWorkBudget` flush (`core/builtin_budget.go:73-79`).
  `DeadlineExceeded` is terminal (`core/error.go:36-47`); `FinishEval` unwind must not
  swallow it.

Forbidden: `unresolved` dispatched to the VM with timeout > 0; `inherited` overwritten,
extended or re-derived; `WithTimeout(0)` clearing an inherited bound; second eval state /
lease / timer context; lean-path clock reads (exactly 0 `nowFunc` ticks per unmetered
unobserved call); later caller deadline suppressing the engine bound; budget-latched
`DeadlineExceeded` swallowed in unwind; resetting a caller-owned adopted ledger
(`HasCallerEvalBudget` guard, `eval.go:637-641`).

Seeding (deterministic, no primary sleep reliance): `no-state` via plain context Call;
`unresolved` via `runtime.WithMeter(ctx, &recordingMeter{})` or
`core.AdoptEvalStateWithMeter(ctx, time.Time{}, 0, …)`; `engine-armed` post-fix via
`newDeadlineEngine(t, timeout)` observed inside a cooperative GoFunc through
`core.EvalDeadlineFrom(ctx)`; `inherited` via `core.WithEvalDeadline(ctx, outerDeadline)`
or natural reentry, identity via `require.Same` on `core.EvalStructCounter` /
`core.EvalCallCounter`; expiry via pre-expired instants + `BuiltinWorkBudget.Flush`,
end-to-end via the existing bounded-timeout regression. Budgets: 0 lean-path clock reads;
exactly 1 boundary clock read max on the metered path (owned by a `nowFunc` tick-count
assertion in 1.1); 128 instructions per cancellation checkpoint, 8 checkpoints between
deadline wall-clock reads (≤1024 instructions worst-case overrun); 128 work units per
BuiltinWorkBudget sync; suite bounded by `-timeout 2m -p 2 -parallel 2`.

### Red/code split and commands (literal)

Red stage (`R1-red`, seals `runtime` test files, written once then read-only):

```sh
go test -timeout 2m -p 2 -parallel 2 ./runtime -run '^TestEngineDeadline_(ContextMeterRetainsBound|EngineMeterRetainsBound|ExistingStateWithoutDeadlineAcquiresBound|HandlesRetainEngineBound|CooperativeGoFuncObservesEngineBound|CallerEarlierDeadlineGoverns|CallerLaterDeadlineDoesNotWeakenBound|DisabledTimeoutAddsNoBound|DisabledTimeoutPreservesInheritedDeadline|ReentryRetainsAbsoluteDeadlineAndBudget|TreeWalkerPreservesInheritedDeadline)$'
```

Verify per chunk (`C1`/`C2`): the focused pattern below + `go build ./...` +
`golangci-lint run ./runtime/...`. Focused pattern (task 3.1, also `V-floor`):

```sh
go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)'
```

Floor: `make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -timeout 2m
-p 2 -parallel 2 -race ./runtime && make lint && openspec validate
metered-call-deadlines --strict --json`. No `-count=1`; `-race` only in the dedicated
3.2 run. No waivers: every seam has red tasks.

### Rules for a foreign agent (no kernel)

- Work in your assigned worktree only; never the primary checkout. Commit there with
  Conventional Commits (`fix(runtime): …`), identity from the repo's configured git
  user, no AI/tool attribution, no personal data.
- Terse output; native file tools; batch reads; never re-read what you already read.
- A contract test once written is **read-only**: red-stage output goes to the chunk
  report, never back into sealed test files.
- Stay inside your chunk's `sites`; anything outside is a blocker to report, not to fix.
- Main is unreachable while you run: blockers go in your yield, immediately.

### Plan review

Round 1 (fresh zarchitect): 1 blocker — the red-run regex carried a trailing `/` after
the `$` anchor (zero matches, vacuous red gate) — fixed; warnings addressed (the
1-read budget now owned by a tick-count assertion; task 3.1 output pinned to the chunk
report). Round 2: **pass**, merge-ready; one cosmetic note (THEN-fragment requirement

## Plan appendix

```json
{
  "v": 2,
  "change": "metered-call-deadlines",
  "baseSha": "5bde17e438b4373459302cb58fd7fdbf94072cbc",
  "generatedAt": "2026-09-08T18:40:56.657Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "B-baseline",
      "taskIds": [
        "0.1",
        "0.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "shard": "baseline",
      "seam": "deadline-ownership-call-boundary",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "0.1",
          "file": "runtime/eval.go",
          "symbol": "bytecodeEvaluator.applyOnVM",
          "anchor": "v.SetDeadline(core.EvalDeadlineFrom(ctx))",
          "change": "Read-only: confirm vs commit 3bcf9c1 that HasEvalState installs zero deadline when state was created for metering; record baseline (no semantic prerequisite; merge before evaluation-outcome-settlement and plugin-binding-rollback)."
        },
        {
          "task": "0.2",
          "file": "Makefile",
          "symbol": "GOTESTFLAGS",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "Read-only: confirm wrapper has no focused-package target, targeted runs use raw go test; confirm existing-service-strict regression-first coverage files."
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "0.1",
        "0.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "git diff 3bcf9c1 --stat -- runtime/eval.go && go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)'",
      "coder": "zpatcher"
    },
    {
      "id": "R1-red",
      "taskIds": [
        "1.1",
        "1.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "shard": "",
      "seam": "deadline-ownership-call-boundary",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "runtime/lazy_deadline_test.go",
          "symbol": "TestEngineDeadline_UnobservedBoundaryReadsNoClock",
          "anchor": "func TestEngineDeadline_UnobservedBoundaryReadsNoClock(t *testing.T) {",
          "change": "Add failing integration matrix: context meter, engine meter, existing state w/o deadline across Engine.Call, Fn.Call, PinnedFn.Call; cooperative GoFunc records core.EvalDeadlineFrom(ctx) (pre-fix zero) and expiry errors.Is(err, context.DeadlineExceeded). Plus a nowFunc tick-count case: a metered top-level call performs exactly 1 boundary clock read (owns the contract budget; VM/core clocks are separate)."
        },
        {
          "task": "1.2",
          "file": "runtime/vm_reentry_deadline_test.go",
          "symbol": "TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline",
          "anchor": "func TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline(t *testing.T) {",
          "change": "Add deterministic caller/inherited-deadline, WithTimeout(0) disablement, pre-expired-instant propagation, reentry into both evaluators; deadline equality, require.Same state/budget identity; tree-walker inherited-deadline case fails pre-fix."
        }
      ],
      "contract": {
        "budgets": [
          "0 boundary clock reads on the lean path: assert.Equal(int64(0), ticks) for unmetered unobserved calls (runtime/lazy_deadline_test.go:57-59)",
          "128 instructions/reductions per cancellation checkpoint: checkInterval (core/eval.go:303, core/vm/vm.go:845)",
          "8 pollCancel checkpoints between deadline wall-clock reads: deadlineClockCadence (core/eval.go:305, core/vm/vm.go:854); worst-case overrun detection <= 8*128 = 1024 instructions past the instant",
          "128 local work units per BuiltinWorkBudget sync (core/builtin_budget.go:23-33)",
          "exactly 1 clock read max added on the general metered boundary: evalDeadline(ctx, nowFunc()) read once, only when top && unresolved",
          "test wall budget: new regressions rely on pre-expired instants and deadline equality, not sleeps; suite bounded by -timeout 2m -p 2 -parallel 2"
        ],
        "forbidden": [
          "state 'unresolved' dispatched to the VM with engine timeout>0 — meter attached but bound lost (the 3bcf9c1 defect: runtime/eval.go:476-481 SetDeadline(zero) on state-without-deadline)",
          "'inherited' overwritten, extended, restarted, or re-derived as now+timeout at any boundary, reentry, or late GoFunc entry",
          "WithTimeout(0) clearing or weakening an 'inherited' deadline (tree-walker runtime/eval.go:991 today does exactly this)",
          "a second eval state, second meter lease lifecycle, or timer context created by the boundary fix (StartEval runs once per boundary, runtime/eval.go:950)",
          "lean path ('no-state', fast condition) reading the clock: exactly 0 nowFunc ticks per unmetered unobserved call (runtime/lazy_deadline_test.go:33-62)",
          "a later caller deadline suppressing the engine bound — 'engine-armed' must still install",
          "BuiltinWorkBudget-latched context.DeadlineExceeded swallowed by non-terminal error precedence during FinishEval unwind",
          "boundary fix resetting resource limits or counters of a caller-owned adopted ledger (HasCallerEvalBudget guard, runtime/eval.go:637-641)"
        ],
        "seeding": [
          "'no-state': New(nil, WithBytecode(), WithDialect(clojure.Dialect())) + plain context Call (runtime/lazy_deadline_test.go:33-40); clock counted via package-private nowFunc override (runtime/lazy_deadline_test.go:44-47, runtime/func_handle_test.go:106-109, runtime/func_pinned_test.go:141-144)",
          "'unresolved' (state without deadline): runtime.WithMeter(context.Background(), &recordingMeter{}) (runtime/call_boundary_flag_test.go:72-76) or core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: ..., MaxAllocationBytes: ...}) with zero deadline (runtime/apply_pool_test.go:216-218) or core.EnsureEvalState (runtime/meter_test.go:183)",
          "'engine-armed': post-fix top-level metered call on newDeadlineEngine(t, timeout) (runtime/vm_reentry_deadline_test.go:21-24); observed inside a cooperative GoFunc via core.EvalDeadlineFrom(ctx) (runtime/vm_reentry_deadline_test.go:146-163 pattern)",
          "'inherited': top-level core.WithEvalDeadline(context.Background(), outerDeadline) (runtime/vm_reentry_deadline_test.go:146-163), or natural reentry: outer Call's GoFunc invokes eng.Call(ctx, inner) with the dispatch ctx (runtime/vm_reentry_deadline_test.go:183-227); budget/state identity asserted via require.Same on core.EvalStructCounter/core.EvalCallCounter across the nested ctx (runtime/vm_reentrant_call_depth_test.go:220-222)",
          "expiry without primary sleep reliance: install an already-past instant with core.WithEvalDeadline and let BuiltinWorkBudget.Flush fire (core/builtin_budget.go:73-79); end-to-end expiry keeps the existing bounded-timeout regression (runtime/lazy_deadline_test.go:17-30)"
        ],
        "states": [
          "no-state",
          "unresolved",
          "engine-armed",
          "inherited"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "runtime/eval.go:862 (fast condition), 1006-1017 (callBoundaryLean), runtime/func.go:140,160,211,238; bound arms lazily via SetTimeout, core/vm/vm.go:344-349,481-487 — unchanged",
            "input": "fast-condition call (fastPath && !HasEvalState && !HasEvalMeter), no callbacks — Engine.Call/Fn.Call/PinnedFn.Call lean spine",
            "state": "no-state"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:49-61 (WithEvalMeter carries meter, creates no deadline); runtime/eval.go:946-950 creates state via evalResourceContext (eval.go:633-641) then StartEval top=true; defect today: runtime/eval.go:476-481 SetDeadline(zero); delta spec scenario 'Context meter retains the engine bound'",
            "input": "top-level call, context meter (runtime.WithMeter), timeout>0, no caller deadline",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:946 (engineMeter forces needsEvalState), 634-635 (meter attach); delta spec scenario 'Engine meter retains the engine bound'",
            "input": "top-level call, engine meter (WithEngineMeter), no ctx meter/state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:279-283 (StartEval: evalDepth==0 -> top=true); core/metering.go:139-143 (WithEvalResourceLimits creates state); delta spec scenario 'Existing state without a deadline acquires the bound'",
            "input": "top-level call, existing eval state without deadline (host-seeded), timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:605-614 (evalDeadline) consulted only when unresolved; runtime/vm_reentry_deadline_test.go:146-163 (OuterEarlierWins: outer instant governs alone, exact equality); delta spec 'Earlier caller deadlines SHALL continue to govern'",
            "input": "top-level call, existing state carries inherited nonzero deadline (engine timeout earlier, equal, later, or disabled)",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (enclosing StartEval left evalDepth>0 -> nested top=false, never arms); core/vm/vm.go:560-573 (installReentrantDeadline preserves the run instant); runtime/vm_reentry_deadline_test.go:183-227; delta spec scenario 'Nested call does not restart its deadline'",
            "input": "reentry — nested Engine.Call/Fn.Call from a GoFunc during an enclosing evaluation, inherited nonzero deadline",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (top=false at reentry, no fresh now+timeout derivation); runtime/vm_reentry_deadline_test.go:183-199 ('unarmed run installs no deadline': observed.IsZero(), flush succeeds)",
            "input": "reentry — enclosing evaluation deadline zero (unarmed run)",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:606-607 (evalDeadline returns zero Time when timeout<=0); delta spec 'WithTimeout(0) SHALL add no engine deadline'; core/vm/vm.go:336-340 SetDeadline(zero) = ctx is the only bound",
            "input": "top-level call with WithTimeout(0) / timeout<=0, any meter or state combination",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:980-991 — today line 991 core.WithEvalDeadline(ctx, deadline) installs zero unconditionally and CLEARS the inherited instant; fix makes install conditional on unresolved; delta spec 'SHALL NOT clear an enclosing evaluation's deadline'",
            "input": "reentry into tree-walker boundary with inherited nonzero deadline while engine timeout is disabled (WithTimeout(0))",
            "state": "inherited"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:988-992 (state created via evalResourceContext, deadline installed before evaluator.Apply) — same arm-if-unresolved rule",
            "input": "top-level tree-walker call (no bytecode evaluator), no state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:611-613; core/eval.go:672-681 (ResolveDeadlineBound: caller deadline not looser suppresses engine instant, caller ctx self-enforces)",
            "input": "caller ctx deadline earlier than or equal to start+timeout",
            "state": "unresolved"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:605-614; delta spec 'later caller deadlines SHALL not weaken the engine's bound'",
            "input": "caller ctx deadline later than start+timeout",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/vm/vm.go:861-884 pollCancel returns fmt.Errorf(\"vm: %w\", context.DeadlineExceeded) at line 878 — errors.Is(err, context.DeadlineExceeded) holds; refusing layer: VM checkpoint, every checkInterval=128 instructions (vm.go:845)",
            "input": "armed instant expires during VM bytecode execution (any metered or unmetered dispatch)",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/builtin_budget.go:66-88 flushPending, lines 73-79: !nowFunc().Before(st.deadline) latches and returns bare context.DeadlineExceeded (no 'vm: ' prefix); refusing layer: BuiltinWorkBudget.Step/Flush, every 128 Step units (builtin_budget.go:23-33) or forced by Finish",
            "input": "armed or inherited instant expires inside a cooperative GoFunc using core.BuiltinWorkBudget",
            "state": "inherited"
          },
          {
            "effect": "forced",
            "evidence": "core/error.go:36-47 (DeadlineExceeded is terminal); runtime/eval.go:953-959 defer replaces err with FinishEval terminal error — precedence must keep surfacing the deadline error to the caller",
            "input": "deadline error meets FinishEval unwind at the boundary",
            "state": "engine-armed"
          }
        ]
      },
      "redTasks": [
        "1.1",
        "1.2"
      ],
      "codeTasks": [],
      "redTests": [
        "TestEngineDeadline_ContextMeterRetainsBound",
        "TestEngineDeadline_EngineMeterRetainsBound",
        "TestEngineDeadline_ExistingStateWithoutDeadlineAcquiresBound",
        "TestEngineDeadline_HandlesRetainEngineBound",
        "TestEngineDeadline_CooperativeGoFuncObservesEngineBound",
        "TestEngineDeadline_CallerEarlierDeadlineGoverns",
        "TestEngineDeadline_CallerLaterDeadlineDoesNotWeakenBound",
        "TestEngineDeadline_DisabledTimeoutAddsNoBound",
        "TestEngineDeadline_DisabledTimeoutPreservesInheritedDeadline",
        "TestEngineDeadline_ReentryRetainsAbsoluteDeadlineAndBudget",
        "TestEngineDeadline_TreeWalkerPreservesInheritedDeadline"
      ],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run '^TestEngineDeadline_(ContextMeterRetainsBound|EngineMeterRetainsBound|ExistingStateWithoutDeadlineAcquiresBound|HandlesRetainEngineBound|CooperativeGoFuncObservesEngineBound|CallerEarlierDeadlineGoverns|CallerLaterDeadlineDoesNotWeakenBound|DisabledTimeoutAddsNoBound|DisabledTimeoutPreservesInheritedDeadline|ReentryRetainsAbsoluteDeadlineAndBudget|TreeWalkerPreservesInheritedDeadline)$'",
      "verify": "go build ./... && go vet ./runtime",
      "coder": "go-coder"
    },
    {
      "id": "C1-code",
      "taskIds": [
        "2.1",
        "2.2"
      ],
      "prev": "R1-red",
      "sharedPkg": "runtime",
      "parallel": false,
      "shard": "",
      "seam": "deadline-ownership-call-boundary",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.callBoundary",
          "anchor": "needsEvalState := !fast && (core.HasEvalState(ctx) || core.HasEvalMeter(ctx) || e.config.engineMeter != nil)",
          "change": "After StartEval top=true and core.EvalDeadlineFrom(ctx).IsZero(): install e.evalDeadline(ctx, nowFunc()) via core.WithEvalDeadline into existing state before dispatch. applyOnVM state path (SetDeadline(EvalDeadlineFrom(ctx))) stays. One clock read max, only when top && unresolved. No second state/lease/timer."
        },
        {
          "task": "2.2",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.callBoundary",
          "anchor": "ctx = core.WithEvalDeadline(ctx, deadline)",
          "change": "Tree-walker branch: install deadline only when unresolved; never write zero over nonzero inherited (today this line clears the inherited instant on WithTimeout(0))."
        }
      ],
      "contract": {
        "budgets": [
          "0 boundary clock reads on the lean path: assert.Equal(int64(0), ticks) for unmetered unobserved calls (runtime/lazy_deadline_test.go:57-59)",
          "128 instructions/reductions per cancellation checkpoint: checkInterval (core/eval.go:303, core/vm/vm.go:845)",
          "8 pollCancel checkpoints between deadline wall-clock reads: deadlineClockCadence (core/eval.go:305, core/vm/vm.go:854); worst-case overrun detection <= 8*128 = 1024 instructions past the instant",
          "128 local work units per BuiltinWorkBudget sync (core/builtin_budget.go:23-33)",
          "exactly 1 clock read max added on the general metered boundary: evalDeadline(ctx, nowFunc()) read once, only when top && unresolved",
          "test wall budget: new regressions rely on pre-expired instants and deadline equality, not sleeps; suite bounded by -timeout 2m -p 2 -parallel 2"
        ],
        "forbidden": [
          "state 'unresolved' dispatched to the VM with engine timeout>0 — meter attached but bound lost (the 3bcf9c1 defect: runtime/eval.go:476-481 SetDeadline(zero) on state-without-deadline)",
          "'inherited' overwritten, extended, restarted, or re-derived as now+timeout at any boundary, reentry, or late GoFunc entry",
          "WithTimeout(0) clearing or weakening an 'inherited' deadline (tree-walker runtime/eval.go:991 today does exactly this)",
          "a second eval state, second meter lease lifecycle, or timer context created by the boundary fix (StartEval runs once per boundary, runtime/eval.go:950)",
          "lean path ('no-state', fast condition) reading the clock: exactly 0 nowFunc ticks per unmetered unobserved call (runtime/lazy_deadline_test.go:33-62)",
          "a later caller deadline suppressing the engine bound — 'engine-armed' must still install",
          "BuiltinWorkBudget-latched context.DeadlineExceeded swallowed by non-terminal error precedence during FinishEval unwind",
          "boundary fix resetting resource limits or counters of a caller-owned adopted ledger (HasCallerEvalBudget guard, runtime/eval.go:637-641)"
        ],
        "seeding": [
          "'no-state': New(nil, WithBytecode(), WithDialect(clojure.Dialect())) + plain context Call (runtime/lazy_deadline_test.go:33-40); clock counted via package-private nowFunc override (runtime/lazy_deadline_test.go:44-47, runtime/func_handle_test.go:106-109, runtime/func_pinned_test.go:141-144)",
          "'unresolved' (state without deadline): runtime.WithMeter(context.Background(), &recordingMeter{}) (runtime/call_boundary_flag_test.go:72-76) or core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: ..., MaxAllocationBytes: ...}) with zero deadline (runtime/apply_pool_test.go:216-218) or core.EnsureEvalState (runtime/meter_test.go:183)",
          "'engine-armed': post-fix top-level metered call on newDeadlineEngine(t, timeout) (runtime/vm_reentry_deadline_test.go:21-24); observed inside a cooperative GoFunc via core.EvalDeadlineFrom(ctx) (runtime/vm_reentry_deadline_test.go:146-163 pattern)",
          "'inherited': top-level core.WithEvalDeadline(context.Background(), outerDeadline) (runtime/vm_reentry_deadline_test.go:146-163), or natural reentry: outer Call's GoFunc invokes eng.Call(ctx, inner) with the dispatch ctx (runtime/vm_reentry_deadline_test.go:183-227); budget/state identity asserted via require.Same on core.EvalStructCounter/core.EvalCallCounter across the nested ctx (runtime/vm_reentrant_call_depth_test.go:220-222)",
          "expiry without primary sleep reliance: install an already-past instant with core.WithEvalDeadline and let BuiltinWorkBudget.Flush fire (core/builtin_budget.go:73-79); end-to-end expiry keeps the existing bounded-timeout regression (runtime/lazy_deadline_test.go:17-30)"
        ],
        "states": [
          "no-state",
          "unresolved",
          "engine-armed",
          "inherited"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "runtime/eval.go:862 (fast condition), 1006-1017 (callBoundaryLean), runtime/func.go:140,160,211,238; bound arms lazily via SetTimeout, core/vm/vm.go:344-349,481-487 — unchanged",
            "input": "fast-condition call (fastPath && !HasEvalState && !HasEvalMeter), no callbacks — Engine.Call/Fn.Call/PinnedFn.Call lean spine",
            "state": "no-state"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:49-61 (WithEvalMeter carries meter, creates no deadline); runtime/eval.go:946-950 creates state via evalResourceContext (eval.go:633-641) then StartEval top=true; defect today: runtime/eval.go:476-481 SetDeadline(zero); delta spec scenario 'Context meter retains the engine bound'",
            "input": "top-level call, context meter (runtime.WithMeter), timeout>0, no caller deadline",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:946 (engineMeter forces needsEvalState), 634-635 (meter attach); delta spec scenario 'Engine meter retains the engine bound'",
            "input": "top-level call, engine meter (WithEngineMeter), no ctx meter/state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:279-283 (StartEval: evalDepth==0 -> top=true); core/metering.go:139-143 (WithEvalResourceLimits creates state); delta spec scenario 'Existing state without a deadline acquires the bound'",
            "input": "top-level call, existing eval state without deadline (host-seeded), timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:605-614 (evalDeadline) consulted only when unresolved; runtime/vm_reentry_deadline_test.go:146-163 (OuterEarlierWins: outer instant governs alone, exact equality); delta spec 'Earlier caller deadlines SHALL continue to govern'",
            "input": "top-level call, existing state carries inherited nonzero deadline (engine timeout earlier, equal, later, or disabled)",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (enclosing StartEval left evalDepth>0 -> nested top=false, never arms); core/vm/vm.go:560-573 (installReentrantDeadline preserves the run instant); runtime/vm_reentry_deadline_test.go:183-227; delta spec scenario 'Nested call does not restart its deadline'",
            "input": "reentry — nested Engine.Call/Fn.Call from a GoFunc during an enclosing evaluation, inherited nonzero deadline",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (top=false at reentry, no fresh now+timeout derivation); runtime/vm_reentry_deadline_test.go:183-199 ('unarmed run installs no deadline': observed.IsZero(), flush succeeds)",
            "input": "reentry — enclosing evaluation deadline zero (unarmed run)",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:606-607 (evalDeadline returns zero Time when timeout<=0); delta spec 'WithTimeout(0) SHALL add no engine deadline'; core/vm/vm.go:336-340 SetDeadline(zero) = ctx is the only bound",
            "input": "top-level call with WithTimeout(0) / timeout<=0, any meter or state combination",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:980-991 — today line 991 core.WithEvalDeadline(ctx, deadline) installs zero unconditionally and CLEARS the inherited instant; fix makes install conditional on unresolved; delta spec 'SHALL NOT clear an enclosing evaluation's deadline'",
            "input": "reentry into tree-walker boundary with inherited nonzero deadline while engine timeout is disabled (WithTimeout(0))",
            "state": "inherited"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:988-992 (state created via evalResourceContext, deadline installed before evaluator.Apply) — same arm-if-unresolved rule",
            "input": "top-level tree-walker call (no bytecode evaluator), no state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:611-613; core/eval.go:672-681 (ResolveDeadlineBound: caller deadline not looser suppresses engine instant, caller ctx self-enforces)",
            "input": "caller ctx deadline earlier than or equal to start+timeout",
            "state": "unresolved"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:605-614; delta spec 'later caller deadlines SHALL not weaken the engine's bound'",
            "input": "caller ctx deadline later than start+timeout",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/vm/vm.go:861-884 pollCancel returns fmt.Errorf(\"vm: %w\", context.DeadlineExceeded) at line 878 — errors.Is(err, context.DeadlineExceeded) holds; refusing layer: VM checkpoint, every checkInterval=128 instructions (vm.go:845)",
            "input": "armed instant expires during VM bytecode execution (any metered or unmetered dispatch)",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/builtin_budget.go:66-88 flushPending, lines 73-79: !nowFunc().Before(st.deadline) latches and returns bare context.DeadlineExceeded (no 'vm: ' prefix); refusing layer: BuiltinWorkBudget.Step/Flush, every 128 Step units (builtin_budget.go:23-33) or forced by Finish",
            "input": "armed or inherited instant expires inside a cooperative GoFunc using core.BuiltinWorkBudget",
            "state": "inherited"
          },
          {
            "effect": "forced",
            "evidence": "core/error.go:36-47 (DeadlineExceeded is terminal); runtime/eval.go:953-959 defer replaces err with FinishEval terminal error — precedence must keep surfacing the deadline error to the caller",
            "input": "deadline error meets FinishEval unwind at the boundary",
            "state": "engine-armed"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "2.1",
        "2.2"
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)'",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)' && go build ./... && golangci-lint run ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "C2-guard",
      "taskIds": [
        "2.3"
      ],
      "prev": "C1-code",
      "sharedPkg": "runtime",
      "parallel": false,
      "shard": "",
      "seam": "deadline-ownership-call-boundary",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.3",
          "file": "runtime/eval.go",
          "symbol": "engineImpl.callBoundaryLean",
          "anchor": "func (e *engineImpl) callBoundaryLean(",
          "change": "Keep lean fast path and callback timing intact: fast condition (eval.go:862) and callBoundaryLean untouched; callback start independent of deadline resolution; zero boundary clock reads on unmetered unobserved calls; fix fallout only if TestEngineDeadline_UnobservedBoundaryReadsNoClock or flag/parity tests trip."
        }
      ],
      "contract": {
        "budgets": [
          "0 boundary clock reads on the lean path: assert.Equal(int64(0), ticks) for unmetered unobserved calls (runtime/lazy_deadline_test.go:57-59)",
          "128 instructions/reductions per cancellation checkpoint: checkInterval (core/eval.go:303, core/vm/vm.go:845)",
          "8 pollCancel checkpoints between deadline wall-clock reads: deadlineClockCadence (core/eval.go:305, core/vm/vm.go:854); worst-case overrun detection <= 8*128 = 1024 instructions past the instant",
          "128 local work units per BuiltinWorkBudget sync (core/builtin_budget.go:23-33)",
          "exactly 1 clock read max added on the general metered boundary: evalDeadline(ctx, nowFunc()) read once, only when top && unresolved",
          "test wall budget: new regressions rely on pre-expired instants and deadline equality, not sleeps; suite bounded by -timeout 2m -p 2 -parallel 2"
        ],
        "forbidden": [
          "state 'unresolved' dispatched to the VM with engine timeout>0 — meter attached but bound lost (the 3bcf9c1 defect: runtime/eval.go:476-481 SetDeadline(zero) on state-without-deadline)",
          "'inherited' overwritten, extended, restarted, or re-derived as now+timeout at any boundary, reentry, or late GoFunc entry",
          "WithTimeout(0) clearing or weakening an 'inherited' deadline (tree-walker runtime/eval.go:991 today does exactly this)",
          "a second eval state, second meter lease lifecycle, or timer context created by the boundary fix (StartEval runs once per boundary, runtime/eval.go:950)",
          "lean path ('no-state', fast condition) reading the clock: exactly 0 nowFunc ticks per unmetered unobserved call (runtime/lazy_deadline_test.go:33-62)",
          "a later caller deadline suppressing the engine bound — 'engine-armed' must still install",
          "BuiltinWorkBudget-latched context.DeadlineExceeded swallowed by non-terminal error precedence during FinishEval unwind",
          "boundary fix resetting resource limits or counters of a caller-owned adopted ledger (HasCallerEvalBudget guard, runtime/eval.go:637-641)"
        ],
        "seeding": [
          "'no-state': New(nil, WithBytecode(), WithDialect(clojure.Dialect())) + plain context Call (runtime/lazy_deadline_test.go:33-40); clock counted via package-private nowFunc override (runtime/lazy_deadline_test.go:44-47, runtime/func_handle_test.go:106-109, runtime/func_pinned_test.go:141-144)",
          "'unresolved' (state without deadline): runtime.WithMeter(context.Background(), &recordingMeter{}) (runtime/call_boundary_flag_test.go:72-76) or core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: ..., MaxAllocationBytes: ...}) with zero deadline (runtime/apply_pool_test.go:216-218) or core.EnsureEvalState (runtime/meter_test.go:183)",
          "'engine-armed': post-fix top-level metered call on newDeadlineEngine(t, timeout) (runtime/vm_reentry_deadline_test.go:21-24); observed inside a cooperative GoFunc via core.EvalDeadlineFrom(ctx) (runtime/vm_reentry_deadline_test.go:146-163 pattern)",
          "'inherited': top-level core.WithEvalDeadline(context.Background(), outerDeadline) (runtime/vm_reentry_deadline_test.go:146-163), or natural reentry: outer Call's GoFunc invokes eng.Call(ctx, inner) with the dispatch ctx (runtime/vm_reentry_deadline_test.go:183-227); budget/state identity asserted via require.Same on core.EvalStructCounter/core.EvalCallCounter across the nested ctx (runtime/vm_reentrant_call_depth_test.go:220-222)",
          "expiry without primary sleep reliance: install an already-past instant with core.WithEvalDeadline and let BuiltinWorkBudget.Flush fire (core/builtin_budget.go:73-79); end-to-end expiry keeps the existing bounded-timeout regression (runtime/lazy_deadline_test.go:17-30)"
        ],
        "states": [
          "no-state",
          "unresolved",
          "engine-armed",
          "inherited"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "runtime/eval.go:862 (fast condition), 1006-1017 (callBoundaryLean), runtime/func.go:140,160,211,238; bound arms lazily via SetTimeout, core/vm/vm.go:344-349,481-487 — unchanged",
            "input": "fast-condition call (fastPath && !HasEvalState && !HasEvalMeter), no callbacks — Engine.Call/Fn.Call/PinnedFn.Call lean spine",
            "state": "no-state"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:49-61 (WithEvalMeter carries meter, creates no deadline); runtime/eval.go:946-950 creates state via evalResourceContext (eval.go:633-641) then StartEval top=true; defect today: runtime/eval.go:476-481 SetDeadline(zero); delta spec scenario 'Context meter retains the engine bound'",
            "input": "top-level call, context meter (runtime.WithMeter), timeout>0, no caller deadline",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:946 (engineMeter forces needsEvalState), 634-635 (meter attach); delta spec scenario 'Engine meter retains the engine bound'",
            "input": "top-level call, engine meter (WithEngineMeter), no ctx meter/state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:279-283 (StartEval: evalDepth==0 -> top=true); core/metering.go:139-143 (WithEvalResourceLimits creates state); delta spec scenario 'Existing state without a deadline acquires the bound'",
            "input": "top-level call, existing eval state without deadline (host-seeded), timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:605-614 (evalDeadline) consulted only when unresolved; runtime/vm_reentry_deadline_test.go:146-163 (OuterEarlierWins: outer instant governs alone, exact equality); delta spec 'Earlier caller deadlines SHALL continue to govern'",
            "input": "top-level call, existing state carries inherited nonzero deadline (engine timeout earlier, equal, later, or disabled)",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (enclosing StartEval left evalDepth>0 -> nested top=false, never arms); core/vm/vm.go:560-573 (installReentrantDeadline preserves the run instant); runtime/vm_reentry_deadline_test.go:183-227; delta spec scenario 'Nested call does not restart its deadline'",
            "input": "reentry — nested Engine.Call/Fn.Call from a GoFunc during an enclosing evaluation, inherited nonzero deadline",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (top=false at reentry, no fresh now+timeout derivation); runtime/vm_reentry_deadline_test.go:183-199 ('unarmed run installs no deadline': observed.IsZero(), flush succeeds)",
            "input": "reentry — enclosing evaluation deadline zero (unarmed run)",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:606-607 (evalDeadline returns zero Time when timeout<=0); delta spec 'WithTimeout(0) SHALL add no engine deadline'; core/vm/vm.go:336-340 SetDeadline(zero) = ctx is the only bound",
            "input": "top-level call with WithTimeout(0) / timeout<=0, any meter or state combination",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:980-991 — today line 991 core.WithEvalDeadline(ctx, deadline) installs zero unconditionally and CLEARS the inherited instant; fix makes install conditional on unresolved; delta spec 'SHALL NOT clear an enclosing evaluation's deadline'",
            "input": "reentry into tree-walker boundary with inherited nonzero deadline while engine timeout is disabled (WithTimeout(0))",
            "state": "inherited"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:988-992 (state created via evalResourceContext, deadline installed before evaluator.Apply) — same arm-if-unresolved rule",
            "input": "top-level tree-walker call (no bytecode evaluator), no state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:611-613; core/eval.go:672-681 (ResolveDeadlineBound: caller deadline not looser suppresses engine instant, caller ctx self-enforces)",
            "input": "caller ctx deadline earlier than or equal to start+timeout",
            "state": "unresolved"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:605-614; delta spec 'later caller deadlines SHALL not weaken the engine's bound'",
            "input": "caller ctx deadline later than start+timeout",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/vm/vm.go:861-884 pollCancel returns fmt.Errorf(\"vm: %w\", context.DeadlineExceeded) at line 878 — errors.Is(err, context.DeadlineExceeded) holds; refusing layer: VM checkpoint, every checkInterval=128 instructions (vm.go:845)",
            "input": "armed instant expires during VM bytecode execution (any metered or unmetered dispatch)",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/builtin_budget.go:66-88 flushPending, lines 73-79: !nowFunc().Before(st.deadline) latches and returns bare context.DeadlineExceeded (no 'vm: ' prefix); refusing layer: BuiltinWorkBudget.Step/Flush, every 128 Step units (builtin_budget.go:23-33) or forced by Finish",
            "input": "armed or inherited instant expires inside a cooperative GoFunc using core.BuiltinWorkBudget",
            "state": "inherited"
          },
          {
            "effect": "forced",
            "evidence": "core/error.go:36-47 (DeadlineExceeded is terminal); runtime/eval.go:953-959 defer replaces err with FinishEval terminal error — precedence must keep surfacing the deadline error to the caller",
            "input": "deadline error meets FinishEval unwind at the boundary",
            "state": "engine-armed"
          }
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "2.3"
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)'",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)' && golangci-lint run ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "V-floor",
      "taskIds": [
        "3.1",
        "3.2"
      ],
      "prev": "C2-guard",
      "sharedPkg": "runtime",
      "parallel": false,
      "shard": "",
      "seam": "deadline-ownership-call-boundary",
      "pkgDirs": [],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "runtime/lazy_deadline_test.go",
          "symbol": "TestEngineDeadline_BytecodeLazyTimeoutStillFires",
          "anchor": "func TestEngineDeadline_BytecodeLazyTimeoutStillFires(t *testing.T) {",
          "change": "Record bounded focused run: command, test names, results incl. every new regression. Output goes to the chunk report only — never edit sealed red test files post-red."
        },
        {
          "task": "3.2",
          "file": "Makefile",
          "symbol": "test",
          "anchor": "test:\n\tgo test $(GOTESTFLAGS) ./...",
          "change": "make test GOTESTFLAGS + go test -race ./runtime under same limits; classify baseline failures without weakening required checks."
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "3.1",
        "3.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)' && make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -timeout 2m -p 2 -parallel 2 -race ./runtime",
      "coder": "coder"
    },
    {
      "id": "F-docs-seal",
      "taskIds": [
        "3.3",
        "3.4"
      ],
      "prev": "V-floor",
      "sharedPkg": "runtime",
      "parallel": false,
      "shard": "",
      "seam": "deadline-ownership-call-boundary",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.3",
          "file": "docs/adr/0010-embedder-owned-evaluation-deadlines.md",
          "symbol": "A fully covering embedder may own evaluation deadlines",
          "anchor": "# A fully covering embedder may own evaluation deadlines",
          "change": "Document meter-independent enforcement, inherited bounds preserved across reentry, unchanged explicit disablement."
        },
        {
          "task": "3.3",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Changed entry: meter attachment preserves configured call deadlines and inherited evaluation bounds."
        },
        {
          "task": "3.4",
          "file": "Makefile",
          "symbol": "lint",
          "anchor": "lint:\n\tgolangci-lint run",
          "change": "make lint + openspec validate --strict; final diff contains only this change."
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "3.3",
        "3.4"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make lint && openspec validate metered-call-deadlines --strict --json && git diff --stat",
      "coder": "coder"
    }
  ],
  "seams": [
    {
      "id": "deadline-ownership-call-boundary",
      "tasks": [
        "0.1",
        "0.2",
        "1.1",
        "1.2",
        "2.1",
        "2.2",
        "2.3",
        "3.1",
        "3.2",
        "3.3",
        "3.4"
      ],
      "summary": "Single behavior seam: which absolute deadline is installed into the eval state at the general call boundary shared by Engine.Call, Fn.Call, PinnedFn.Call (runtime/eval.go callBoundary:939 -> applyOnVM:476 for VM, eval.go:980-991 for tree-walker). Today meter setup creates eval state without a deadline (evalResourceContext via core.WithEvalResourceLimits, core/metering.go:139-143) and applyOnVM selects SetDeadline(EvalDeadlineFrom(ctx)) whenever core.HasEvalState(ctx) — a zero deadline arms the VM with no bound, so WithTimeout is silently disabled on every metered call (defect reproduced at 3bcf9c1). Fix: on the general boundary, when StartEval reports top and core.EvalDeadlineFrom(ctx) is unresolved (zero), resolve the engine bound once via evalDeadline(ctx, nowFunc()) (caller-deadline respecting) and install it into the existing state with core.WithEvalDeadline before dispatch; no second state, lease, or timer context. Lean path (eval.go:862, 1006-1017) untouched.",
      "contract": {
        "budgets": [
          "0 boundary clock reads on the lean path: assert.Equal(int64(0), ticks) for unmetered unobserved calls (runtime/lazy_deadline_test.go:57-59)",
          "128 instructions/reductions per cancellation checkpoint: checkInterval (core/eval.go:303, core/vm/vm.go:845)",
          "8 pollCancel checkpoints between deadline wall-clock reads: deadlineClockCadence (core/eval.go:305, core/vm/vm.go:854); worst-case overrun detection <= 8*128 = 1024 instructions past the instant",
          "128 local work units per BuiltinWorkBudget sync (core/builtin_budget.go:23-33)",
          "exactly 1 clock read max added on the general metered boundary: evalDeadline(ctx, nowFunc()) read once, only when top && unresolved",
          "test wall budget: new regressions rely on pre-expired instants and deadline equality, not sleeps; suite bounded by -timeout 2m -p 2 -parallel 2"
        ],
        "forbidden": [
          "state 'unresolved' dispatched to the VM with engine timeout>0 — meter attached but bound lost (the 3bcf9c1 defect: runtime/eval.go:476-481 SetDeadline(zero) on state-without-deadline)",
          "'inherited' overwritten, extended, restarted, or re-derived as now+timeout at any boundary, reentry, or late GoFunc entry",
          "WithTimeout(0) clearing or weakening an 'inherited' deadline (tree-walker runtime/eval.go:991 today does exactly this)",
          "a second eval state, second meter lease lifecycle, or timer context created by the boundary fix (StartEval runs once per boundary, runtime/eval.go:950)",
          "lean path ('no-state', fast condition) reading the clock: exactly 0 nowFunc ticks per unmetered unobserved call (runtime/lazy_deadline_test.go:33-62)",
          "a later caller deadline suppressing the engine bound — 'engine-armed' must still install",
          "BuiltinWorkBudget-latched context.DeadlineExceeded swallowed by non-terminal error precedence during FinishEval unwind",
          "boundary fix resetting resource limits or counters of a caller-owned adopted ledger (HasCallerEvalBudget guard, runtime/eval.go:637-641)"
        ],
        "seeding": [
          "'no-state': New(nil, WithBytecode(), WithDialect(clojure.Dialect())) + plain context Call (runtime/lazy_deadline_test.go:33-40); clock counted via package-private nowFunc override (runtime/lazy_deadline_test.go:44-47, runtime/func_handle_test.go:106-109, runtime/func_pinned_test.go:141-144)",
          "'unresolved' (state without deadline): runtime.WithMeter(context.Background(), &recordingMeter{}) (runtime/call_boundary_flag_test.go:72-76) or core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: ..., MaxAllocationBytes: ...}) with zero deadline (runtime/apply_pool_test.go:216-218) or core.EnsureEvalState (runtime/meter_test.go:183)",
          "'engine-armed': post-fix top-level metered call on newDeadlineEngine(t, timeout) (runtime/vm_reentry_deadline_test.go:21-24); observed inside a cooperative GoFunc via core.EvalDeadlineFrom(ctx) (runtime/vm_reentry_deadline_test.go:146-163 pattern)",
          "'inherited': top-level core.WithEvalDeadline(context.Background(), outerDeadline) (runtime/vm_reentry_deadline_test.go:146-163), or natural reentry: outer Call's GoFunc invokes eng.Call(ctx, inner) with the dispatch ctx (runtime/vm_reentry_deadline_test.go:183-227); budget/state identity asserted via require.Same on core.EvalStructCounter/core.EvalCallCounter across the nested ctx (runtime/vm_reentrant_call_depth_test.go:220-222)",
          "expiry without primary sleep reliance: install an already-past instant with core.WithEvalDeadline and let BuiltinWorkBudget.Flush fire (core/builtin_budget.go:73-79); end-to-end expiry keeps the existing bounded-timeout regression (runtime/lazy_deadline_test.go:17-30)"
        ],
        "states": [
          "no-state",
          "unresolved",
          "engine-armed",
          "inherited"
        ],
        "transitions": [
          {
            "effect": "set",
            "evidence": "runtime/eval.go:862 (fast condition), 1006-1017 (callBoundaryLean), runtime/func.go:140,160,211,238; bound arms lazily via SetTimeout, core/vm/vm.go:344-349,481-487 — unchanged",
            "input": "fast-condition call (fastPath && !HasEvalState && !HasEvalMeter), no callbacks — Engine.Call/Fn.Call/PinnedFn.Call lean spine",
            "state": "no-state"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:49-61 (WithEvalMeter carries meter, creates no deadline); runtime/eval.go:946-950 creates state via evalResourceContext (eval.go:633-641) then StartEval top=true; defect today: runtime/eval.go:476-481 SetDeadline(zero); delta spec scenario 'Context meter retains the engine bound'",
            "input": "top-level call, context meter (runtime.WithMeter), timeout>0, no caller deadline",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:946 (engineMeter forces needsEvalState), 634-635 (meter attach); delta spec scenario 'Engine meter retains the engine bound'",
            "input": "top-level call, engine meter (WithEngineMeter), no ctx meter/state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "set",
            "evidence": "core/metering.go:279-283 (StartEval: evalDepth==0 -> top=true); core/metering.go:139-143 (WithEvalResourceLimits creates state); delta spec scenario 'Existing state without a deadline acquires the bound'",
            "input": "top-level call, existing eval state without deadline (host-seeded), timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:605-614 (evalDeadline) consulted only when unresolved; runtime/vm_reentry_deadline_test.go:146-163 (OuterEarlierWins: outer instant governs alone, exact equality); delta spec 'Earlier caller deadlines SHALL continue to govern'",
            "input": "top-level call, existing state carries inherited nonzero deadline (engine timeout earlier, equal, later, or disabled)",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (enclosing StartEval left evalDepth>0 -> nested top=false, never arms); core/vm/vm.go:560-573 (installReentrantDeadline preserves the run instant); runtime/vm_reentry_deadline_test.go:183-227; delta spec scenario 'Nested call does not restart its deadline'",
            "input": "reentry — nested Engine.Call/Fn.Call from a GoFunc during an enclosing evaluation, inherited nonzero deadline",
            "state": "inherited"
          },
          {
            "effect": "no-op",
            "evidence": "core/metering.go:279-283 (top=false at reentry, no fresh now+timeout derivation); runtime/vm_reentry_deadline_test.go:183-199 ('unarmed run installs no deadline': observed.IsZero(), flush succeeds)",
            "input": "reentry — enclosing evaluation deadline zero (unarmed run)",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:606-607 (evalDeadline returns zero Time when timeout<=0); delta spec 'WithTimeout(0) SHALL add no engine deadline'; core/vm/vm.go:336-340 SetDeadline(zero) = ctx is the only bound",
            "input": "top-level call with WithTimeout(0) / timeout<=0, any meter or state combination",
            "state": "unresolved"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:980-991 — today line 991 core.WithEvalDeadline(ctx, deadline) installs zero unconditionally and CLEARS the inherited instant; fix makes install conditional on unresolved; delta spec 'SHALL NOT clear an enclosing evaluation's deadline'",
            "input": "reentry into tree-walker boundary with inherited nonzero deadline while engine timeout is disabled (WithTimeout(0))",
            "state": "inherited"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:988-992 (state created via evalResourceContext, deadline installed before evaluator.Apply) — same arm-if-unresolved rule",
            "input": "top-level tree-walker call (no bytecode evaluator), no state, timeout>0",
            "state": "engine-armed"
          },
          {
            "effect": "no-op",
            "evidence": "runtime/eval.go:611-613; core/eval.go:672-681 (ResolveDeadlineBound: caller deadline not looser suppresses engine instant, caller ctx self-enforces)",
            "input": "caller ctx deadline earlier than or equal to start+timeout",
            "state": "unresolved"
          },
          {
            "effect": "set",
            "evidence": "runtime/eval.go:605-614; delta spec 'later caller deadlines SHALL not weaken the engine's bound'",
            "input": "caller ctx deadline later than start+timeout",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/vm/vm.go:861-884 pollCancel returns fmt.Errorf(\"vm: %w\", context.DeadlineExceeded) at line 878 — errors.Is(err, context.DeadlineExceeded) holds; refusing layer: VM checkpoint, every checkInterval=128 instructions (vm.go:845)",
            "input": "armed instant expires during VM bytecode execution (any metered or unmetered dispatch)",
            "state": "engine-armed"
          },
          {
            "effect": "forced",
            "evidence": "core/builtin_budget.go:66-88 flushPending, lines 73-79: !nowFunc().Before(st.deadline) latches and returns bare context.DeadlineExceeded (no 'vm: ' prefix); refusing layer: BuiltinWorkBudget.Step/Flush, every 128 Step units (builtin_budget.go:23-33) or forced by Finish",
            "input": "armed or inherited instant expires inside a cooperative GoFunc using core.BuiltinWorkBudget",
            "state": "inherited"
          },
          {
            "effect": "forced",
            "evidence": "core/error.go:36-47 (DeadlineExceeded is terminal); runtime/eval.go:953-959 defer replaces err with FinishEval terminal error — precedence must keep surfacing the deadline error to the caller",
            "input": "deadline error meets FinishEval unwind at the boundary",
            "state": "engine-armed"
          }
        ]
      },
      "redTasks": [
        "1.1: failing integration matrix — context meter, engine meter, host-seeded existing state w/o deadline across Engine.Call, Fn.Call, PinnedFn.Call on WithTimeout engines; cooperative GoFunc records core.EvalDeadlineFrom(ctx) and must observe the engine bound (pre-fix observes zero) and expiry errors.Is(err, context.DeadlineExceeded)",
        "1.2: deterministic caller/inherited-deadline and timeout-disablement cases incl. reentry into both evaluators — deadline equality (observed.Equal(instant)), retained state/budget identity (Same on core.EvalStructCounter/core.EvalCallCounter), pre-expired-instant error propagation, WithTimeout(0) adds no bound and never clears an inherited one (tree-walker case fails pre-fix)"
      ],
      "codeTasks": [
        "2.1: arm absent engine bound on the general call boundary — after StartEval returns top=true and core.EvalDeadlineFrom(ctx).IsZero(), install e.evalDeadline(ctx, nowFunc()) via core.WithEvalDeadline into the existing state before dispatch; all 1.1 cases pass through applyOnVM (runtime/eval.go:476-484 stays SetDeadline(EvalDeadlineFrom(ctx)) on the state path)",
        "2.2: preserve inherited deadlines in the tree-walker branch (runtime/eval.go:980-991) and timeout-disablement path — install only when unresolved; never write a zero deadline over a nonzero one",
        "2.3: keep lean path and callback timing intact — fast condition (runtime/eval.go:862) and callBoundaryLean untouched; callback start independent of deadline resolution",
        "3.1: record focused run go test -timeout 2m -p 2 -parallel 2 ./runtime -run 'Test(EngineDeadline|EngineImpl_EvalDeadline|CallReentrancy|CallBoundary|Pinned|Func)'",
        "3.2: make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' + go test -timeout 2m -p 2 -parallel 2 -race ./runtime; classify baseline failures",
        "3.3: update existing deadline docs + CHANGELOG [Unreleased]: meter-independent enforcement, inherited bounds, unchanged explicit disablement",
        "3.4: make lint + openspec validate metered-call-deadlines --strict --json; diff contains only this change"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "Attaching a context meter, an engine meter, or existing evaluation state SHALL NOT disable the configured timeout of Engine.Call, Fn.Call, or PinnedFn.Call.",
      "tests": [
        "TestEngineDeadline_ContextMeterRetainsBound",
        "TestEngineDeadline_EngineMeterRetainsBound",
        "TestEngineDeadline_ExistingStateWithoutDeadlineAcquiresBound",
        "TestEngineDeadline_HandlesRetainEngineBound"
      ]
    },
    {
      "shall": "Each call SHALL retain the same deadline ownership and cooperative enforcement as its unmetered equivalent.",
      "tests": [
        "TestEngineDeadline_CooperativeGoFuncObservesEngineBound",
        "TestEngineDeadline_HandlesRetainEngineBound",
        "TestEngineDeadline_BytecodeLazyTimeoutStillFires"
      ]
    },
    {
      "shall": "Reentry SHALL preserve the enclosing absolute evaluation deadline and shared resource budget.",
      "tests": [
        "TestEngineDeadline_ReentryRetainsAbsoluteDeadlineAndBudget",
        "TestEngineDeadline_TreeWalkerPreservesInheritedDeadline",
        "TestCallReentrancy_VMLateGoFuncEntryDoesNotReDeriveDeadline"
      ]
    },
    {
      "shall": "WithTimeout(0) SHALL add no engine deadline and SHALL NOT clear an enclosing evaluation's deadline.",
      "tests": [
        "TestEngineDeadline_DisabledTimeoutAddsNoBound",
        "TestEngineDeadline_DisabledTimeoutPreservesInheritedDeadline"
      ]
    },
    {
      "shall": "Earlier caller deadlines SHALL continue to govern; later caller deadlines SHALL not weaken the engine's bound.",
      "tests": [
        "TestEngineDeadline_CallerEarlierDeadlineGoverns",
        "TestEngineDeadline_CallerLaterDeadlineDoesNotWeakenBound"
      ]
    },
    {
      "shall": "WHEN an unmetered call without callbacks or existing evaluation state completes without observing a deadline THEN this change SHALL add no boundary clock read, timer, or derived deadline context.",
      "tests": [
        "TestEngineDeadline_UnobservedBoundaryReadsNoClock"
      ]
    },
    {
      "shall": "- **THEN** the engine deadline SHALL remain present and expiry SHALL return a deadline error",
      "tests": [
        "TestEngineDeadline_ContextMeterRetainsBound"
      ]
    },
    {
      "shall": "- **THEN** the configured engine timeout SHALL govern that call exactly as without the meter",
      "tests": [
        "TestEngineDeadline_EngineMeterRetainsBound"
      ]
    },
    {
      "shall": "- **THEN** the engine SHALL enforce its configured timeout while preserving that state's resource budget",
      "tests": [
        "TestEngineDeadline_ExistingStateWithoutDeadlineAcquiresBound"
      ]
    },
    {
      "shall": "- **THEN** the nested call SHALL retain the enclosing absolute deadline without extending it or starting an independent budget",
      "tests": [
        "TestEngineDeadline_ReentryRetainsAbsoluteDeadlineAndBudget"
      ]
    },
    {
      "shall": "- **THEN** deadline selection SHALL match the existing deadline-ownership contract, and disablement SHALL leave an inherited evaluation deadline intact",
      "tests": [
        "TestEngineDeadline_DisabledTimeoutPreservesInheritedDeadline",
        "TestEngineDeadline_CallerEarlierDeadlineGoverns",
        "TestEngineDeadline_CallerLaterDeadlineDoesNotWeakenBound"
      ]
    },
    {
      "shall": "- **THEN** this change SHALL add no boundary clock read, timer, or derived deadline context",
      "tests": [
        "TestEngineDeadline_UnobservedBoundaryReadsNoClock"
      ]
    }
  ],
  "testHarness": [
    "newDeadlineEngine — runtime/vm_reentry_deadline_test.go:17 — builds a fresh engine with bytecode, Clojure dialect, configured timeout, and bound = and - builtins",
    "spinDef — runtime/vm_reentry_deadline_test.go:14 — pure-bytecode spin loop definition (loop [n 500000] ...) running entirely in VM before GoFunc dispatch",
    "nowFunc — runtime/eval.go:22 — package-private time source controlling engine eval deadline calculation and call duration telemetry, but NOT controlling independent VM instruction-interval checks or core deadline checks",
    "nowFunc override pattern — runtime/lazy_deadline_test.go:44 — atomic tick counter replacing nowFunc restored via t.Cleanup, asserting zero clock reads on unobserved lean calls",
    "cooperative GoFunc probe — runtime/vm_reentry_deadline_test.go:47 — registers a GoFunc asserting core.EvalDeadlineFrom(ctx) and testing NewBuiltinWorkBudget(ctx).Flush() expiry",
    "WithMeter / WithEngineMeter — runtime/meter.go:134,143 — attaches context-borne or engine-level recordingMeter to assert meter attribution without resetting execution budgets",
    "recordingMeter — runtime/meter_test.go:22 — test double implementing Meter to capture allocations, reductions, and retained sizes across evaluations",
    "core.NewBuiltinWorkBudget — core/eval.go:372 — creates a cooperative step/flush work budget bound to ctx evaluation state and deadline",
    "bindBuiltin — runtime/eval_test.go:33 — binds a standard library builtin by name into the engine root environment"
  ],
  "floor": "make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && go test -timeout 2m -p 2 -parallel 2 -race ./runtime && make lint && openspec validate metered-call-deadlines --strict --json",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
