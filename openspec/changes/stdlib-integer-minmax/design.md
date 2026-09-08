## Context

See `proposal.md` for the reproduced failure at review commit `3bcf9c1`. `minMaxFunc` stores every candidate in `float64` and converts the winner back to `int64` when no float argument occurred. Existing comparison code in `internal/collections/order.go` already distinguishes exact integer pairs from mixed float comparisons.

## Goals / Non-Goals

**Goals:** keep integer extrema exact without changing the shared builtin's validation, mixed numeric behavior, or cooperative work budget.

**Non-Goals:** arithmetic overflow changes, a numeric tower, exact mixed integer/float comparison, new VM opcodes, or changes to JSON decoding.

## Decisions

- Keep the integer candidate in `int64` until the first `core.Float`; then promote and retain the existing float comparison path. This avoids a second argument traversal and preserves the rule that any float operand makes the result a float, even when an integer wins.
- Keep the existing `BuiltinWorkBudget` ownership and `finishBuiltin` returns. If control-flow edits change inventoried phases or return branches, update `internal/inventory/work_data.go` and `internal/inventory/result_data.go` rather than weakening completeness checks.
- Do not cast a rounded float back to recover an integer result. Adjacent integers can already have collapsed to one float at that point.

## Verification

Testing mode: existing-service-strict. Add failing regressions before changing the implementation. Extend `TestArithmetic_MinMax` and exercise actual `Engine.Call` and source evaluation under `WithTreeWalker()` and `WithBytecode()`.

Cover singleton endpoints `-9223372036854775808` and `9223372036854775807`; adjacent positive and negative integers around `9007199254740992`; reversed input order; repeated extrema; and full-range mixed signs. Assert `core.Int` and its exact value. Mixed cases such as `(max 2 1.5)` and `(min 1 1.5)` must still return `core.Float`.

Retain `TestNumeric_ShortCallsKeepExactValuesAndErrors`, the existing numeric traversal tests for cancellation, expired deadlines, and reduction limits, and executable inventory checks. Run resource-limited project checks through `make test` with a worker cap in `GOTESTFLAGS`; no benchmark change is required.

## Risks / Trade-offs

- Float promotion can still round large integers in mixed calls → preserve and document that existing policy rather than changing it in this fix.
- A new result branch can escape accounting inventory → keep static inventory validation in the verification floor.

## Migration Plan

No predecessor or data migration. Update the existing changelog for corrected integer results. Reverting the implementation restores the known precision defect; no persisted format changes are involved.

## Implementation plan

Tier standard, testing mode existing-service-strict, planned against `454b6a92da2a8eefcf4df5a7ed9656d64a91f0fc`.

Three chunks. `dispatch-parity` carries the production fix and runs first; `exact-int-table` is serial behind it on the shared `plugins/stdlib` package; `changelog-entry` runs in parallel in its own shard and touches no Go package.

One fix (task 2.1) turns both red tasks green, and a chunk holds at most two tasks with its red tasks a proper subset, so exactly one red task can be observed failing against unfixed code. It goes to 1.2. A new test file that is green from birth proves nothing about its own wiring — a mis-wired engine, a wrong dialect, or a case that never reaches `minMaxFunc` all look identical to a pass. Task 1.1 only appends rows to a table already exercised by four green rows (`plugins/stdlib/stdlib_test.go:255-258`) on the same `eval` helper and the same type-strict `Equals`, so its arrival already-green in the second chunk costs no evidence.

Task 0.1 owns no chunk: it was executed during planning, which re-verified the accumulator, the two detected work phases, the seven value-yielding returns and the three error-inventory rows at this base commit. It produces no edit. Task 3.1 owns no stage either — it is closed by the floor below, which is character-for-character the command `tasks.md` 3.1 mandates.

### Chunk 1 — `dispatch-parity`

Tasks 1.2 (red) and 2.1 (code). First chunk, no predecessor. Seals `runtime` and `plugins/stdlib`. Coder `go-coder`.

| Task | File | Symbol | Anchor |
| --- | --- | --- | --- |
| 1.2 | `runtime/minmax_integer_parity_test.go` | new test, package `runtime` | NEW FILE |
| 2.1 | `plugins/stdlib/arithmetic.go` | `minMaxFunc` accumulator | `var result float64` |
| 2.1 | `plugins/stdlib/arithmetic.go` | `minMaxFunc` argument loop | `for _, arg := range args[1:] {` |
| 2.1 | `plugins/stdlib/arithmetic.go` | `minMaxFunc` result returns | `return finishBuiltin(budget, core.BoxInt(int64(result)), nil)` |

Red test: `TestMinMax_ExactIntegersAcrossDispatchModes`, subtests `tree-walker/call`, `tree-walker/eval`, `vm/call`, `vm/eval`.

Build each engine with `newReentrantCallDepthEngine` (`runtime/vm_reentrant_call_depth_test.go:29`), passing exactly one of `WithTreeWalker()` or `WithBytecode()`. It prepends `WithDialect(clojure.Dialect())`, registers `t.Cleanup` on `Close`, loads `stdlib.New()`, and takes no `ResourceLimits`. Do not use `newMeteringStdlibEngine` or any helper with a mandatory ceiling: its only example limit is a 24 KiB allocation cap, under which a parity case can terminate with `core.CodeResourceLimit` before reaching the exactness comparison. The package's existing mode table `goldenEvaluatorModes` (`runtime/cl_adapters_golden_test.go:93-100`) may be reused verbatim for the mode axis.

Contract states — candidate state and dispatch mode are orthogonal axes:

- `int-exact` — no `core.Float` operand seen; the running extremum is an `int64` candidate; the terminal return is `core.BoxInt(candidate)`.
- `float-promoted` — at least one `core.Float` seen; the extremum is a `float64`; the terminal return is `core.Float{V: candidate}` and stays `Float` even when an integer operand wins.
- `arity-refused` / `type-refused` — at the `GoFunc`.
- `arity-refused-at-boundary` / `type-refused-at-boundary` — the same refusals observed through `Engine.Call` and `Engine.Eval`.
- `budget-refused` — a terminal sync error from the work budget. Out of scope for this chunk's red task, which sets no limits and no deadline.
- `mode-tree-walker`, `mode-vm`, `named-call`, `source-eval` — the four dispatch combinations.

Error types the tests assert: `*core.LispicoError` reached with `require.ErrorAs`, code `ArityError` with message `max: requires at least 1 argument`, and code `TypeError` with message `max: expected number, got core.String`. Assert `le.Message`, never `err.Error()` — `Error()` prefixes the position when `Source` is set (`core/error.go:22-27`). Both entry points yield the same error: the tree-walker returns a `GoFunc` error verbatim, and the runtime boundary re-wraps only a recovered panic and an undefined name.

Not every endpoint row is red at this base commit. `float64(math.MinInt64)` is exactly −2^63 and round-trips, so `(min -9223372036854775808)` already passes and is characterization. The rows that actually go red are the `math.MaxInt64` singleton, which wraps to `math.MinInt64`, and the pairs adjacent to 9007199254740992.

```sh
# red
go test -timeout 2m -run 'TestMinMax_ExactIntegersAcrossDispatchModes' ./runtime/
# verify
go build ./runtime/ ./plugins/stdlib/ && go vet ./runtime/ ./plugins/stdlib/ && go test -timeout 2m ./runtime/ ./plugins/stdlib/ && golangci-lint run ./runtime/... ./plugins/stdlib/...
```

### Chunk 2 — `exact-int-table`

Tasks 1.1 (red) and 2.2 (code). Serial behind `dispatch-parity`, sharing `plugins/stdlib`. Seals `plugins/stdlib` and `internal/inventory`. Coder `go-coder`.

| Task | File | Symbol | Anchor |
| --- | --- | --- | --- |
| 1.1 | `plugins/stdlib/stdlib_test.go` | `TestArithmetic_MinMax` | `func TestArithmetic_MinMax(t *testing.T) {` |
| 1.1 | `plugins/stdlib/stdlib_test.go` | mixed rows | `{"max with float", "(max 1 5.5 3)", core.Float{V: 5.5}},` |
| 2.2 | `internal/inventory/work_data.go` | `WorkPhases` max/min rows | `PhaseLabel:  "unary dispatch",` |
| 2.2 | `internal/inventory/result_data.go` | `ResultBranches` max/min rows | `BranchLabel: "int extremum return",` |

Task 2.2 is expected to be a no-op and must not become one by weakening a check. Both reconcilers fail only when the detected count exceeds the recorded count, and the fix adds no loop, no second `core.NewBuiltinWorkBudget` and no eighth return, so the recorded two `WorkPhases` rows and seven `ResultBranches` rows already match. Add a row only if the diff adds a phase or a branch. Never remove one: the reconcilers accept `recorded > detected` silently, so a removed row is caught by no test.

```sh
# red
go test -timeout 2m -run 'TestArithmetic_MinMax' ./plugins/stdlib/
# verify
go build ./plugins/stdlib/ ./internal/inventory/ && go vet ./plugins/stdlib/ ./internal/inventory/ && go test -timeout 2m ./plugins/stdlib/ ./internal/inventory/ && golangci-lint run ./plugins/stdlib/... ./internal/inventory/...
```

### Chunk 3 — `changelog-entry`

Tasks 3.1 and 3.2. Parallel, shard `changelog`, no Go package. Coder `zpatcher`.

`NO-RED-WAIVER:` a make-target result and a prose entry freeze no observable behavior a test can assert. `NO-TESTER-WAIVER:` the deliverables are a command result and a prose entry, both verified by the floor commands themselves.

Task 3.2 adds one entry under the existing `### Fixed` heading (`CHANGELOG.md:43`) describing exact `int64` extrema for all-integer `min` and `max`, stating that a float operand still promotes and can round, and naming the pinning tests. Its verify greps for both test names, neither of which is in the file at this base commit, so it fails before the edit and passes after.

```sh
grep -q 'TestMinMax_ExactIntegersAcrossDispatchModes' CHANGELOG.md && grep -q 'TestArithmetic_MinMax' CHANGELOG.md
```

### Floor, lenses and review

Floor, run once after the last chunk:

```sh
make test GOTESTFLAGS="-timeout 2m -p 2 -parallel 2" && make lint
```

Lenses `spec`, `quality`, `perf`. The `perf` lens is named for one specific diff shape: returning `core.Int{V: candidate}` instead of `core.BoxInt(candidate)` drops the shared-instance path that serves −128..1023 from a package-level array and adds a heap allocation on every small-integer result, which the consumer gate measures on its bytes axis.

Risks the plan carries: a changed `budget.Step()` count, which the three numeric argument-traversal tests catch; a return added outside `finishBuiltin`, which the budget-holder check catches as an unflushed return; setting the float candidate to the float operand instead of promoting the integer candidate first, which the existing mixed rows catch; and losing the strict comparison, which is invisible for equal integers but changes which of `+0.0` and `-0.0` a float-promoted call returns.

Plan review: `pass`, reviewer `zarchitect`, two rounds. Round one returned one blocker — the seam mandated two incompatible engine builders, one of which required a resource ceiling the seam does not want — plus a recommendation to move the observed red onto the new runtime file. Both were applied. Round two returned no blockers.

### Rules for an agent running this plan without the kernel

- Work in the change's own worktree on its own branch. Never edit the primary checkout outside the final fast-forward merge, and never touch `openspec/`.
- A contract test, once written and sealed, is read-only. If it is wrong, stop and say so; do not edit it to match the implementation.
- Commits follow Conventional Commits: `<type>(<scope>): <description>`, imperative, lowercase, at most 72 characters. Describe only the staged diff. No tool or process references anywhere in the message.
- Terse output. No comments explaining what the code does — rename or restructure instead. Comments are for why, invariants, edge cases and external contracts.
- Search and read with the native file tools; use a shell only for commands that run something.
- Merge only on green verification. Never push.

## Plan appendix

```json
{
  "v": 2,
  "change": "stdlib-integer-minmax",
  "baseSha": "454b6a92da2a8eefcf4df5a7ed9656d64a91f0fc",
  "generatedAt": "2026-09-08T07:28:43.078Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "dispatch-parity",
      "taskIds": [
        "1.2",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "minmax-public-dispatch-parity",
      "shard": "",
      "pkgDirs": [
        "runtime",
        "plugins/stdlib"
      ],
      "pkgs": [
        "./runtime",
        "./plugins/stdlib"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "runtime/minmax_integer_parity_test.go",
          "symbol": "new test (package runtime)",
          "anchor": "NEW FILE",
          "change": "New dual-mode regression: build one engine per mode with the existing unlimited stdlib helper newReentrantCallDepthEngine (runtime/vm_reentrant_call_depth_test.go:29) — newReentrantCallDepthEngine(t, WithTreeWalker()) and newReentrantCallDepthEngine(t, WithBytecode()); it prepends WithDialect(clojure.Dialect()), registers t.Cleanup(Close) and Use(stdlib.New()), and imposes no ResourceLimits, so no case can terminate on a ceiling before the exactness comparison. Drive the same integer-extrema cases through Engine.Eval(ctx, name, src) AND Engine.Call(ctx, \"max\"/\"min\", core.Int{...}); assert exact core.Int value and Type() parity between vm and treewalker; pin (max 2 1.5)/(min 1 1.5) as core.Float and the arity/type typed errors as *core.LispicoError."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/arithmetic.go",
          "symbol": "minMaxFunc accumulator",
          "anchor": "\t\tvar result float64",
          "change": "Replace the single float64 accumulator with an int64 candidate plus the float64 one and the existing hasFloat flag; seed from args[0] exactly, promoting only on core.Float. Keep budget.Step() placement, the same finishBuiltin returns, and identical error text."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/arithmetic.go",
          "symbol": "minMaxFunc argument loop",
          "anchor": "\t\tfor _, arg := range args[1:] {",
          "change": "Compare int-vs-int exactly (int64 compare, mirroring collections.NumCmp) and promote the int candidate to float on the first core.Float, then keep the existing float comparison. One traversal only, one budget.Step() per argument as today."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/arithmetic.go",
          "symbol": "minMaxFunc result returns",
          "anchor": "\t\treturn finishBuiltin(budget, core.BoxInt(int64(result)), nil)",
          "change": "Return core.BoxInt of the int64 candidate, never a float64 cast; the hasFloat branch keeps returning core.Float{V: result} unchanged."
        }
      ],
      "contract": {
        "states": [
          "int-exact (no core.Float operand seen yet; the running extremum lives in an int64 candidate; the terminal return is core.BoxInt(candidate), core/types.go:105-113, which yields a shared core.Int for -128..1023 and a fresh core.Int{V:...} otherwise)",
          "float-promoted (at least one core.Float operand has been seen; the running extremum lives in a float64 candidate; the terminal return is core.Float{V: candidate}, core/types.go:116, and stays Float even when an integer operand wins)",
          "arity-refused (zero arguments; finishBuiltin(budget, nil, arityErrorf(...)) returns (nil, *core.LispicoError{Code:\"ArityError\"}))",
          "type-refused (a non-number operand; finishBuiltin(budget, nil, typeErrorf(...)) returns (nil, *core.LispicoError{Code:\"TypeError\"}))",
          "budget-refused (budget.Step(), budget.Finish() or budget.Flush() returned a terminal sync error — reduction ceiling, engine deadline, caller cancellation — and the value is nil)",
          "mode-tree-walker (engine built with runtime.WithTreeWalker(), runtime/engine.go:158)",
          "mode-vm (engine built with runtime.WithBytecode(), runtime/engine.go:149)",
          "named-call (dispatch through Engine.Call(ctx, name, args...), runtime/engine.go:33, implementation runtime/eval.go:838)",
          "source-eval (dispatch through Engine.Eval(ctx, source, input), runtime/engine.go:23)",
          "arity-refused-at-boundary (a zero-argument max or min at the Engine boundary yields a non-nil error carrying *core.LispicoError with Code \"ArityError\" and Message \"max: requires at least 1 argument\" / \"min: requires at least 1 argument\", and a nil value)",
          "type-refused-at-boundary (a non-number operand at the Engine boundary yields a non-nil error carrying *core.LispicoError with Code \"TypeError\" and Message \"max: expected number, got core.String\", and a nil value)"
        ],
        "transitions": [
          {
            "input": "len(args) == 0",
            "state": "arity-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:434-436 — arityErrorf(\"%s: requires at least 1 argument\", name), name is exactly \"max\" or \"min\" (arithmetic.go:216-226)"
          },
          {
            "input": "budget.Step() error before the leading argument",
            "state": "budget-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:441-443 — finishBuiltin(budget, nil, err); inventory branch label \"leading step error return\" (internal/inventory/result_data.go:535-542)"
          },
          {
            "input": "leading operand is core.Int",
            "state": "int-exact",
            "effect": "set",
            "evidence": "spec requirement \"Integer min and max preserve exact extrema\" — the candidate takes v.V with no float64 conversion. The site is the leading-operand switch, which today reads `case core.Int: result = float64(v.V)` at plugins/stdlib/arithmetic.go:445-446 and is exactly what this change replaces."
          },
          {
            "input": "leading operand is core.Float",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:447-449 — hasFloat = true at the leading operand"
          },
          {
            "input": "leading operand is neither core.Int nor core.Float",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:450-452 — typeErrorf(\"%s: expected number, got %T\", name, args[0])"
          },
          {
            "input": "budget.Step() error inside the argument loop",
            "state": "budget-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:455-457; inventory branch label \"step error return\" (internal/inventory/result_data.go:551-558)"
          },
          {
            "input": "core.Int operand while int-exact and (isMax && v.V > candidate) || (!isMax && v.V < candidate)",
            "state": "int-exact",
            "effect": "set",
            "evidence": "spec scenario \"Adjacent large integers remain distinguishable\"; the comparison is int64 vs int64, never float64 vs float64 (plugins/stdlib/arithmetic.go:469-477 is the comparison shape to keep, with int64 operands)"
          },
          {
            "input": "core.Int operand while int-exact and the strict comparison is false, including an equal operand",
            "state": "int-exact",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:469-477 — strict > / <, so on a tie the earlier operand is kept (first-wins)"
          },
          {
            "input": "first core.Float operand while int-exact",
            "state": "float-promoted",
            "effect": "forced",
            "evidence": "spec requirement \"If any argument is a Float, the operation SHALL retain its existing float promotion, comparison, and result type behavior\" — promotion sets the float candidate to float64(intCandidate) and then runs the SAME comparison against the float operand at plugins/stdlib/arithmetic.go:469-477; the operand-reading switch is at arithmetic.go:462-464, which today only sets result and hasFloat. Promotion must never adopt the float operand as the candidate unconditionally."
          },
          {
            "input": "core.Int operand while float-promoted and (isMax && float64(v.V) > candidate) || (!isMax && float64(v.V) < candidate)",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:460-461, 469-477 — existing rounding behavior is retained deliberately (design.md Decisions, Risks)"
          },
          {
            "input": "core.Int operand while float-promoted and the strict float64 comparison is false, including an operand equal after conversion",
            "state": "float-promoted",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:460-461, 469-477 — existing rounding behavior is retained deliberately (design.md Decisions, Risks)"
          },
          {
            "input": "core.Float operand while float-promoted and (isMax && v.V > candidate) || (!isMax && v.V < candidate)",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:462-463, 469-477"
          },
          {
            "input": "core.Float operand while float-promoted and the strict float64 comparison is false, including an equal operand and a NaN operand",
            "state": "float-promoted",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:462-463, 469-477"
          },
          {
            "input": "non-number operand in the loop in any state",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:465-467 — typeErrorf(\"%s: expected number, got %T\", name, arg)"
          },
          {
            "input": "argument list exhausted while int-exact",
            "state": "int-exact",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:483 is the branch (today finishBuiltin(budget, core.BoxInt(int64(result)), nil)); after the change it hands core.BoxInt(candidate) with no float64 round trip. Inventory branch label \"int extremum return\" (internal/inventory/result_data.go:575-582); spec scenario \"Singleton endpoint retains its value\"."
          },
          {
            "input": "argument list exhausted while float-promoted",
            "state": "float-promoted",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:480-482 — finishBuiltin(budget, core.Float{V: candidate}, nil); inventory branch label \"float extremum return\" (internal/inventory/result_data.go:567-574); spec scenario \"A float operand preserves float promotion\""
          },
          {
            "input": "newReentrantCallDepthEngine(t, WithTreeWalker())",
            "state": "mode-tree-walker",
            "effect": "set",
            "evidence": "runtime/vm_reentrant_call_depth_test.go:29-38 — func newReentrantCallDepthEngine(t testing.TB, opts ...EngineOption) Engine; clojure dialect, stdlib loaded, Close registered through t.Cleanup, no ResourceLimits"
          },
          {
            "input": "newReentrantCallDepthEngine(t, WithBytecode())",
            "state": "mode-vm",
            "effect": "set",
            "evidence": "runtime/vm_reentrant_call_depth_test.go:29-38 with runtime/engine.go:149; last-wins if both options are passed, so a test passes exactly one (CLAUDE.md, TCO section)"
          },
          {
            "input": "eng.Call(ctx, \"max\", core.Int{V: math.MaxInt64}) in either mode",
            "state": "named-call",
            "effect": "forced",
            "evidence": "spec scenario \"Public dispatch agrees across execution modes\"; result must satisfy got.Equals(core.Int{V: math.MaxInt64})"
          },
          {
            "input": "eng.Call(ctx, \"min\", core.Int{V: math.MinInt64}) in either mode",
            "state": "named-call",
            "effect": "forced",
            "evidence": "spec scenario \"Singleton endpoint retains its value\"; this row already passes at HEAD (float64(math.MinInt64) round-trips exactly) and is characterization, not red"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(max 9007199254740992 9007199254740993)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "spec scenario \"Adjacent large integers remain distinguishable\"; expect core.Int{V: 9007199254740993}, and the reversed operand order expects the same"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(min 9007199254740992 9007199254740993)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "same scenario; expect core.Int{V: 9007199254740992} in either operand order"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(max -9223372036854775808 9223372036854775807)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "spec scenario \"Negative and mixed-sign integer extrema remain exact\"; expect core.Int{V: math.MaxInt64}, and min expects core.Int{V: math.MinInt64}"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(max 2 1.5)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "spec scenario \"A float operand preserves float promotion\"; expect core.Float{V: 2}, and (min 1 1.5) expects core.Float{V: 1}"
          },
          {
            "input": "eng.Call(ctx, \"max\") and eng.Eval(ctx, \"minmax\", \"(max)\") in either mode",
            "state": "arity-refused-at-boundary",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:434-436 produces the error; the Engine boundary does not re-wrap it — runtime/eval.go wraps only a recovered panic (eval.go:690-695) and returns a plain fmt.Errorf only for an undefined name (eval.go:915-921) — so both entry points are asserted to yield the SAME *core.LispicoError Code and Message, reached with require.ErrorAs(t, err, &le). Assert le.Message, not err.Error(), because Error() prefixes the position when Source is set (core/error.go:22-27)"
          },
          {
            "input": "eng.Call(ctx, \"max\", core.Int{V: 1}, core.String{V: \"a\"}) and eng.Eval(ctx, \"minmax\", \"(max 1 \\\"a\\\")\") in either mode",
            "state": "type-refused-at-boundary",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:465-467; same non-re-wrapping argument as the arity row, so both entry points are asserted to yield the same Code \"TypeError\" and the same Message \"max: expected number, got core.String\""
          },
          {
            "input": "no dialect option passed (runtime.New default is the CL dialect)",
            "state": "named-call",
            "effect": "no-op",
            "evidence": "the names \"max\" and \"min\" are not remapped by either dialect (zero hits in cl/cl.go and clojure/clojure.go), so the same source text works under both; newReentrantCallDepthEngine pins the clojure dialect explicitly (runtime/vm_reentrant_call_depth_test.go:31)"
          }
        ],
        "forbidden": [
          "int-exact -> float-promoted that adopts the float operand without running the pending comparison: (max 5 1.5) would return Float 1.5 instead of Float 5",
          "float-promoted -> int-exact: there is no demotion; (max 2 1.5) is core.Float{V:2} and (min 1 1.5) is core.Float{V:1}, never core.Int",
          "any float64(operand) conversion while in int-exact",
          "int64(floatCandidate) to recover an integer result (design.md Decision 3): adjacent integers have already collapsed at that point",
          "a second argument traversal or a second core.NewBuiltinWorkBudget call inside minMaxFunc: it changes the detected work-phase count from 2 (plugins/stdlib/inventory_source_test.go:902-912, 1174-1177)",
          "an eighth value-yielding return statement inside the returned closure without a matching ResultBranches row (plugins/stdlib/inventory_source_test.go:914-919, 1257-1260)",
          "any exit from the closure that does not go through finishBuiltin: UNFLUSHED_RETURN (plugins/stdlib/inventory_source_test.go:1196-1199, 1280-1292)",
          "relaxing >/< to >=/<=: it flips tie resolution from first-wins to last-wins",
          "returning core.Int{V: candidate} instead of core.BoxInt(candidate): it drops the shared-instance path for -128..1023 (core/types.go:105-113)",
          "a test that seeds a state by constructing core.GoFunc or the closure directly, or by writing a candidate field",
          "asserting a dedicated VM opcode or a VM-native min/max fast path: none exists",
          "binding a Lisp-level replacement for max or min in the test engine instead of exercising the registered builtin (plugins/stdlib/arithmetic.go:216-226)",
          "asserting only the numeric value through a float64 conversion: the assertion must pin the concrete core.Int type",
          "passing both WithTreeWalker() and WithBytecode() to one engine",
          "building the engine through newMeteringStdlibEngine(t, bytecode, limits) (runtime/resource_limits_test.go:29) or any helper that takes a mandatory ResourceLimits: this seam asserts values and refusals, not accounting, and a ceiling would let a terminal ResourceLimitError mask the ArityError and TypeError rows",
          "asserting the refusals on err.Error() rather than on the unwrapped *core.LispicoError fields"
        ],
        "seeding": [
          "plugins/stdlib package only — not used by task 1.2: kernel values, tree-walking evaluator: setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19) then eval(t, env, \"(max 9223372036854775807)\") (plugins/stdlib/stdlib_test.go:21-36). This is the only legal seeding path for int-exact and float-promoted at the unit level.",
          "plugins/stdlib package only — not used by task 1.2: typed errors and budget states: collectionGoFunc(t, env, \"max\").Fn(ctx, nil, args, env) — see plugins/stdlib/numeric_budget_test.go:71-76 and the builtinErr(t, env, name, args...) helper used at numeric_budget_test.go:246-253.",
          "plugins/stdlib package only — not used by task 1.2: budget-refused only through core.WithEvalResourceLimits(ctx, 100, 1<<30), core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond)), or a cancelled parent context — exactly as plugins/stdlib/numeric_budget_test.go:67-115 does. Never by calling budget methods from a test.",
          "plugins/stdlib package only — not used by task 1.2: source literals: core.Read parses a digit token with an optional leading '-' as one number (core/reader.go:351-363) and parseNumber returns BoxInt via strconv.ParseInt(s, 10, 64) (core/reader.go:766-779), so \"-9223372036854775808\" is a single core.Int literal, not (- 9223372036854775808).",
          "plugins/stdlib package only — not used by task 1.2: names — concrete types: core.Int struct{ V int64 } (core/types.go:74), read as got.(core.Int).V; core.Float struct{ V float64 } (core/types.go:116), read as got.(core.Float).V; constructor for the int result is core.BoxInt(int64) core.Value (core/types.go:108).",
          "plugins/stdlib package only — not used by task 1.2: names — assertion: core.Int.Equals returns false for a core.Float and vice versa (core/types.go:81-86, 123-128), so got.Equals(core.Int{V: want}) already discriminates the concrete type; the existing table at plugins/stdlib/stdlib_test.go:261-268 uses exactly that.",
          "plugins/stdlib package only — not used by task 1.2: names — errors: arityErrorf returns *core.LispicoError{Code: \"ArityError\"} and typeErrorf returns *core.LispicoError{Code: \"TypeError\"} (plugins/stdlib/errors.go:13-20); Error() renders \"<Code>: <Message>\" when Source is empty (core/error.go:22-27). Assert with require.ErrorAs(t, err, &le) then le.Code and require.Contains(le.Message, ...), as plugins/stdlib/numeric_budget_test.go:246-253 does. Exact messages: \"max: requires at least 1 argument\", \"min: requires at least 1 argument\", \"max: expected number, got core.String\".",
          "plugins/stdlib package only — not used by task 1.2: names — constants a test may name: math.MaxInt64 (9223372036854775807), math.MinInt64 (-9223372036854775808), 9007199254740992, 9007199254740993, -9007199254740992, -9007199254740993.",
          "engine only through newReentrantCallDepthEngine(t testing.TB, opts ...EngineOption) Engine at runtime/vm_reentrant_call_depth_test.go:29-38 — same package, clojure dialect, stdlib.New() loaded, eng.Close registered via t.Cleanup, and no ResourceLimits. It is passed exactly one of WithTreeWalker() or WithBytecode().",
          "mode table: []struct{ name string; opts []EngineOption }{{\"tree-walker\", []EngineOption{WithTreeWalker()}}, {\"vm\", []EngineOption{WithBytecode()}}} — the existing goldenEvaluatorModes at runtime/cl_adapters_golden_test.go:94-100 is in the same package and may be reused verbatim.",
          "arguments for named-call as core.Value literals (core.Int{V: ...}, core.String{V: \"a\"}); arguments for source-eval as literal source text read by core.Read.",
          "refusal states are reached only by calling max or min with zero arguments or with a core.String operand through Engine.Call / Engine.Eval; never by invoking the GoFunc directly from this package.",
          "context from t.Context(); no resource limits and no deadline, so budget-refused is out of this seam's scope."
        ],
        "budgets": [
          "plugins/stdlib package only — not used by task 1.2: budget.Step() calls per invocation: exactly len(args) — 1 for the leading argument plus 1 per remaining argument. Unchanged by this fix.",
          "plugins/stdlib package only — not used by task 1.2: work phases detected in minMaxFunc: exactly 2 (one range loop + one core.NewBuiltinWorkBudget call), matching the 2 WorkPhases rows at internal/inventory/work_data.go:213-228.",
          "plugins/stdlib package only — not used by task 1.2: value-yielding return statements detected inside the closure: exactly 7, matching the 7 ResultBranches rows at internal/inventory/result_data.go:527-582. The outer `return func(...)` in minMaxFunc is not counted, because minMaxFunc's own result type is a func type, not core.Value (plugins/stdlib/inventory_source_test.go:453-464, 914-919).",
          "plugins/stdlib package only — not used by task 1.2: budget batch interval: 128 units (core/builtin_budget.go:23-32); the traversal tests drive 200 arguments (plugins/stdlib/numeric_budget_test.go:17).",
          "plugins/stdlib package only — not used by task 1.2: exact integer range preserved: the whole signed 64-bit range, -9223372036854775808..9223372036854775807. float64 loses exactness above 2^53 = 9007199254740992, and math.MinInt64 = -2^63 is the one endpoint float64 already represents exactly.",
          "dispatch paths per case: 2 modes x 2 entry points = 4.",
          "value cases: 5 integer-exactness cases (both int64 endpoints as singletons, adjacent 2^53 pair in both orders for max and for min, adjacent negative 2^53 pair, mixed-sign endpoints) plus the 2 float-promotion cases (max 2 1.5), (min 1 1.5).",
          "refusal cases: 2 (arity-refused, type-refused), each asserted through both entry points in both modes.",
          "no ResourceLimits and no deadline are configured for this seam's engines, so no core.WithEvalResourceLimits value applies."
        ]
      },
      "redTasks": [
        "1.2"
      ],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [
        "TestMinMax_ExactIntegersAcrossDispatchModes"
      ],
      "redRun": "go test -timeout 2m -run 'TestMinMax_ExactIntegersAcrossDispatchModes' ./runtime/",
      "verify": "go build ./runtime/ ./plugins/stdlib/ && go vet ./runtime/ ./plugins/stdlib/ && go test -timeout 2m ./runtime/ ./plugins/stdlib/ && golangci-lint run ./runtime/... ./plugins/stdlib/...",
      "coder": "go-coder"
    },
    {
      "id": "exact-int-table",
      "taskIds": [
        "1.1",
        "2.2"
      ],
      "prev": "dispatch-parity",
      "sharedPkg": "plugins/stdlib",
      "parallel": false,
      "seam": "minmax-exact-integer-selection",
      "shard": "",
      "pkgDirs": [
        "plugins/stdlib",
        "internal/inventory"
      ],
      "pkgs": [
        "./plugins/stdlib",
        "./internal/inventory"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "plugins/stdlib/stdlib_test.go",
          "symbol": "TestArithmetic_MinMax",
          "anchor": "func TestArithmetic_MinMax(t *testing.T) {",
          "change": "Extend the existing name/input/expected table with singleton signed endpoints (-9223372036854775808, 9223372036854775807), adjacent integers around +/-9007199254740992, both argument orders, repeated extrema and full-range mixed signs; assertion must be exact-typed (core.Int value + type), not only Equals, so a float64 round-trip fails."
        },
        {
          "task": "1.1",
          "file": "plugins/stdlib/stdlib_test.go",
          "symbol": "TestArithmetic_MinMax mixed rows",
          "anchor": "\t\t{\"max with float\", \"(max 1 5.5 3)\", core.Float{V: 5.5}},",
          "change": "Keep the two mixed-float rows unchanged and add (max 2 1.5) -> core.Float{V: 2} and (min 1 1.5) -> core.Float{V: 1} per the spec scenario."
        },
        {
          "task": "2.2",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases max/min rows",
          "anchor": "\t\tPhaseLabel:  \"unary dispatch\",",
          "change": "The two max min rows sit immediately after this anchor (argument budget, argument walk). reconcileWork counts loops+budgets per func: leave both rows untouched while the fix keeps exactly one budget and one loop; add a row only if a second loop appears."
        },
        {
          "task": "2.2",
          "file": "internal/inventory/result_data.go",
          "symbol": "ResultBranches max/min rows",
          "anchor": "\t\tBranchLabel: \"int extremum return\",",
          "change": "reconcileResult counts value-yielding return statements per func (currently 7 rows for 7 returns). If the fix adds or removes a return, append or drop a scalar-singleton row here so the row count is never below the detected branch count; labels are free-form, duplicates are rejected."
        }
      ],
      "contract": {
        "states": [
          "int-exact (no core.Float operand seen yet; the running extremum lives in an int64 candidate; the terminal return is core.BoxInt(candidate), core/types.go:105-113, which yields a shared core.Int for -128..1023 and a fresh core.Int{V:...} otherwise)",
          "float-promoted (at least one core.Float operand has been seen; the running extremum lives in a float64 candidate; the terminal return is core.Float{V: candidate}, core/types.go:116, and stays Float even when an integer operand wins)",
          "arity-refused (zero arguments; finishBuiltin(budget, nil, arityErrorf(...)) returns (nil, *core.LispicoError{Code:\"ArityError\"}))",
          "type-refused (a non-number operand; finishBuiltin(budget, nil, typeErrorf(...)) returns (nil, *core.LispicoError{Code:\"TypeError\"}))",
          "budget-refused (budget.Step(), budget.Finish() or budget.Flush() returned a terminal sync error — reduction ceiling, engine deadline, caller cancellation — and the value is nil)"
        ],
        "transitions": [
          {
            "input": "len(args) == 0",
            "state": "arity-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:434-436 — arityErrorf(\"%s: requires at least 1 argument\", name), name is exactly \"max\" or \"min\" (arithmetic.go:216-226)"
          },
          {
            "input": "budget.Step() error before the leading argument",
            "state": "budget-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:441-443 — finishBuiltin(budget, nil, err); inventory branch label \"leading step error return\" (internal/inventory/result_data.go:535-542)"
          },
          {
            "input": "leading operand is core.Int",
            "state": "int-exact",
            "effect": "set",
            "evidence": "spec requirement \"Integer min and max preserve exact extrema\" — the candidate takes v.V with no float64 conversion. The site is the leading-operand switch, which today reads `case core.Int: result = float64(v.V)` at plugins/stdlib/arithmetic.go:445-446 and is exactly what this change replaces."
          },
          {
            "input": "leading operand is core.Float",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:447-449 — hasFloat = true at the leading operand"
          },
          {
            "input": "leading operand is neither core.Int nor core.Float",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:450-452 — typeErrorf(\"%s: expected number, got %T\", name, args[0])"
          },
          {
            "input": "budget.Step() error inside the argument loop",
            "state": "budget-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:455-457; inventory branch label \"step error return\" (internal/inventory/result_data.go:551-558)"
          },
          {
            "input": "core.Int operand while int-exact and (isMax && v.V > candidate) || (!isMax && v.V < candidate)",
            "state": "int-exact",
            "effect": "set",
            "evidence": "spec scenario \"Adjacent large integers remain distinguishable\"; the comparison is int64 vs int64, never float64 vs float64 (plugins/stdlib/arithmetic.go:469-477 is the comparison shape to keep, with int64 operands)"
          },
          {
            "input": "core.Int operand while int-exact and the strict comparison is false, including an equal operand",
            "state": "int-exact",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:469-477 — strict > / <, so on a tie the earlier operand is kept (first-wins)"
          },
          {
            "input": "first core.Float operand while int-exact",
            "state": "float-promoted",
            "effect": "forced",
            "evidence": "spec requirement \"If any argument is a Float, the operation SHALL retain its existing float promotion, comparison, and result type behavior\" — promotion sets the float candidate to float64(intCandidate) and then runs the SAME comparison against the float operand at plugins/stdlib/arithmetic.go:469-477; the operand-reading switch is at arithmetic.go:462-464, which today only sets result and hasFloat. Promotion must never adopt the float operand as the candidate unconditionally."
          },
          {
            "input": "core.Int operand while float-promoted and (isMax && float64(v.V) > candidate) || (!isMax && float64(v.V) < candidate)",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:460-461, 469-477 — existing rounding behavior is retained deliberately (design.md Decisions, Risks)"
          },
          {
            "input": "core.Int operand while float-promoted and the strict float64 comparison is false, including an operand equal after conversion",
            "state": "float-promoted",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:460-461, 469-477 — existing rounding behavior is retained deliberately (design.md Decisions, Risks)"
          },
          {
            "input": "core.Float operand while float-promoted and (isMax && v.V > candidate) || (!isMax && v.V < candidate)",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:462-463, 469-477"
          },
          {
            "input": "core.Float operand while float-promoted and the strict float64 comparison is false, including an equal operand and a NaN operand",
            "state": "float-promoted",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:462-463, 469-477"
          },
          {
            "input": "non-number operand in the loop in any state",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:465-467 — typeErrorf(\"%s: expected number, got %T\", name, arg)"
          },
          {
            "input": "argument list exhausted while int-exact",
            "state": "int-exact",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:483 is the branch (today finishBuiltin(budget, core.BoxInt(int64(result)), nil)); after the change it hands core.BoxInt(candidate) with no float64 round trip. Inventory branch label \"int extremum return\" (internal/inventory/result_data.go:575-582); spec scenario \"Singleton endpoint retains its value\"."
          },
          {
            "input": "argument list exhausted while float-promoted",
            "state": "float-promoted",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:480-482 — finishBuiltin(budget, core.Float{V: candidate}, nil); inventory branch label \"float extremum return\" (internal/inventory/result_data.go:567-574); spec scenario \"A float operand preserves float promotion\""
          }
        ],
        "forbidden": [
          "int-exact -> float-promoted that adopts the float operand without running the pending comparison: (max 5 1.5) would return Float 1.5 instead of Float 5",
          "float-promoted -> int-exact: there is no demotion; (max 2 1.5) is core.Float{V:2} and (min 1 1.5) is core.Float{V:1}, never core.Int",
          "any float64(operand) conversion while in int-exact",
          "int64(floatCandidate) to recover an integer result (design.md Decision 3): adjacent integers have already collapsed at that point",
          "a second argument traversal or a second core.NewBuiltinWorkBudget call inside minMaxFunc: it changes the detected work-phase count from 2 (plugins/stdlib/inventory_source_test.go:902-912, 1174-1177)",
          "an eighth value-yielding return statement inside the returned closure without a matching ResultBranches row (plugins/stdlib/inventory_source_test.go:914-919, 1257-1260)",
          "any exit from the closure that does not go through finishBuiltin: UNFLUSHED_RETURN (plugins/stdlib/inventory_source_test.go:1196-1199, 1280-1292)",
          "relaxing >/< to >=/<=: it flips tie resolution from first-wins to last-wins",
          "returning core.Int{V: candidate} instead of core.BoxInt(candidate): it drops the shared-instance path for -128..1023 (core/types.go:105-113)",
          "a test that seeds a state by constructing core.GoFunc or the closure directly, or by writing a candidate field"
        ],
        "seeding": [
          "kernel values, tree-walking evaluator: setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19) then eval(t, env, \"(max 9223372036854775807)\") (plugins/stdlib/stdlib_test.go:21-36). This is the only legal seeding path for int-exact and float-promoted at the unit level.",
          "typed errors and budget states: collectionGoFunc(t, env, \"max\").Fn(ctx, nil, args, env) — see plugins/stdlib/numeric_budget_test.go:71-76 and the builtinErr(t, env, name, args...) helper used at numeric_budget_test.go:246-253.",
          "budget-refused only through core.WithEvalResourceLimits(ctx, 100, 1<<30), core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond)), or a cancelled parent context — exactly as plugins/stdlib/numeric_budget_test.go:67-115 does. Never by calling budget methods from a test.",
          "source literals: core.Read parses a digit token with an optional leading '-' as one number (core/reader.go:351-363) and parseNumber returns BoxInt via strconv.ParseInt(s, 10, 64) (core/reader.go:766-779), so \"-9223372036854775808\" is a single core.Int literal, not (- 9223372036854775808).",
          "names — concrete types: core.Int struct{ V int64 } (core/types.go:74), read as got.(core.Int).V; core.Float struct{ V float64 } (core/types.go:116), read as got.(core.Float).V; constructor for the int result is core.BoxInt(int64) core.Value (core/types.go:108).",
          "names — assertion: core.Int.Equals returns false for a core.Float and vice versa (core/types.go:81-86, 123-128), so got.Equals(core.Int{V: want}) already discriminates the concrete type; the existing table at plugins/stdlib/stdlib_test.go:261-268 uses exactly that.",
          "names — errors: arityErrorf returns *core.LispicoError{Code: \"ArityError\"} and typeErrorf returns *core.LispicoError{Code: \"TypeError\"} (plugins/stdlib/errors.go:13-20); Error() renders \"<Code>: <Message>\" when Source is empty (core/error.go:22-27). Assert with require.ErrorAs(t, err, &le) then le.Code and require.Contains(le.Message, ...), as plugins/stdlib/numeric_budget_test.go:246-253 does. Exact messages: \"max: requires at least 1 argument\", \"min: requires at least 1 argument\", \"max: expected number, got core.String\".",
          "names — constants a test may name: math.MaxInt64 (9223372036854775807), math.MinInt64 (-9223372036854775808), 9007199254740992, 9007199254740993, -9007199254740992, -9007199254740993."
        ],
        "budgets": [
          "budget.Step() calls per invocation: exactly len(args) — 1 for the leading argument plus 1 per remaining argument. Unchanged by this fix.",
          "work phases detected in minMaxFunc: exactly 2 (one range loop + one core.NewBuiltinWorkBudget call), matching the 2 WorkPhases rows at internal/inventory/work_data.go:213-228.",
          "value-yielding return statements detected inside the closure: exactly 7, matching the 7 ResultBranches rows at internal/inventory/result_data.go:527-582. The outer `return func(...)` in minMaxFunc is not counted, because minMaxFunc's own result type is a func type, not core.Value (plugins/stdlib/inventory_source_test.go:453-464, 914-919).",
          "budget batch interval: 128 units (core/builtin_budget.go:23-32); the traversal tests drive 200 arguments (plugins/stdlib/numeric_budget_test.go:17).",
          "exact integer range preserved: the whole signed 64-bit range, -9223372036854775808..9223372036854775807. float64 loses exactness above 2^53 = 9007199254740992, and math.MinInt64 = -2^63 is the one endpoint float64 already represents exactly."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestArithmetic_MinMax"
      ],
      "redRun": "go test -timeout 2m -run 'TestArithmetic_MinMax' ./plugins/stdlib/",
      "verify": "go build ./plugins/stdlib/ ./internal/inventory/ && go vet ./plugins/stdlib/ ./internal/inventory/ && go test -timeout 2m ./plugins/stdlib/ ./internal/inventory/ && golangci-lint run ./plugins/stdlib/... ./internal/inventory/...",
      "coder": "go-coder"
    },
    {
      "id": "changelog-entry",
      "taskIds": [
        "3.1",
        "3.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "minmax-floor-and-changelog",
      "shard": "changelog",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "Makefile",
          "symbol": "test, lint targets",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "Verification floor: make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" and make lint. No Makefile edit."
        },
        {
          "task": "3.2",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased] Fixed",
          "anchor": "## [Unreleased]",
          "change": "Add one entry under the existing ### Fixed heading (line 43) describing exact int64 extrema for all-integer min/max, explicitly noting that a float operand still promotes and can round; name the pinning tests."
        }
      ],
      "contract": {
        "states": [
          "floor-green (make test with the capped GOTESTFLAGS and make lint both pass)",
          "changelog-recorded (CHANGELOG.md [Unreleased] carries a Fixed entry for min and max)"
        ],
        "transitions": [
          {
            "input": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" and make lint both exit 0",
            "state": "floor-green",
            "effect": "set",
            "evidence": "openspec/changes/stdlib-integer-minmax/tasks.md 3.1"
          },
          {
            "input": "a Fixed subsection is added under ## [Unreleased] in CHANGELOG.md",
            "state": "changelog-recorded",
            "effect": "set",
            "evidence": "CHANGELOG.md heads with ## [Unreleased] followed by ### Added and ### Changed; a ### Fixed subsection is the right home (tasks.md 3.2)"
          },
          {
            "input": "the entry claims exact mixed integer/float results",
            "state": "changelog-recorded",
            "effect": "forced",
            "evidence": "design.md Risks — float promotion still rounds large integers in mixed calls; the entry must say so or stay silent on it, never claim the opposite"
          }
        ],
        "forbidden": [
          "a changelog entry that describes mixed int/float calls as exact",
          "closing 3.1 on a narrowed run instead of the whole-project gate",
          "adding a new CHANGELOG section heading above [Unreleased]"
        ],
        "seeding": [
          "the floor is reached only through the Makefile targets; the repo Makefile defines build, test, test-unit, lint, fmt, profile, profile-report and GOTESTFLAGS ?= -timeout 2m, with no test-fast and no PKG parameter.",
          "task 3.1 has no stage of its own: it is closed by the plan-level `floor`, which is character-for-character the command tasks.md 3.1 mandates. No coder or red stage owns it.",
          "task 3.2 writes one entry under the existing `### Fixed` heading (CHANGELOG.md:43) and must name the pinning tests, which is what this chunk's verify greps for."
        ],
        "budgets": [
          "test wall-clock limit: -timeout 2m; package parallelism -p 2; in-package parallelism -parallel 2."
        ]
      },
      "codeTasks": [
        "3.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "grep -q 'TestMinMax_ExactIntegersAcrossDispatchModes' CHANGELOG.md && grep -q 'TestArithmetic_MinMax' CHANGELOG.md",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "minmax-exact-integer-selection",
      "tasks": [
        "0.1",
        "1.1",
        "2.1"
      ],
      "summary": "minMaxFunc (plugins/stdlib/arithmetic.go:430-485) keeps the running extremum in an int64 candidate while every operand seen so far is core.Int, and promotes to the existing float64 path on the first core.Float. Same single argument walk, same single BuiltinWorkBudget, same seven value-yielding return statements, same finishBuiltin exits. The int result returns core.BoxInt(candidate) with no float64 round trip. Red is task 1.1 (kernel-level table), code is task 2.1. Red-stage reality check: not every endpoint row fails before the fix. float64(math.MinInt64) is exactly -2^63 and round-trips, so (min -9223372036854775808) ALREADY PASSES at this HEAD and is a characterization row, not a red row. The rows that actually go red are the math.MaxInt64 singleton — int64(float64(math.MaxInt64)) wraps to math.MinInt64 — and the 2^53-adjacent pairs, where 9007199254740993 collapses onto 9007199254740992. Judge the red stage on those.",
      "contract": {
        "states": [
          "int-exact (no core.Float operand seen yet; the running extremum lives in an int64 candidate; the terminal return is core.BoxInt(candidate), core/types.go:105-113, which yields a shared core.Int for -128..1023 and a fresh core.Int{V:...} otherwise)",
          "float-promoted (at least one core.Float operand has been seen; the running extremum lives in a float64 candidate; the terminal return is core.Float{V: candidate}, core/types.go:116, and stays Float even when an integer operand wins)",
          "arity-refused (zero arguments; finishBuiltin(budget, nil, arityErrorf(...)) returns (nil, *core.LispicoError{Code:\"ArityError\"}))",
          "type-refused (a non-number operand; finishBuiltin(budget, nil, typeErrorf(...)) returns (nil, *core.LispicoError{Code:\"TypeError\"}))",
          "budget-refused (budget.Step(), budget.Finish() or budget.Flush() returned a terminal sync error — reduction ceiling, engine deadline, caller cancellation — and the value is nil)"
        ],
        "transitions": [
          {
            "input": "len(args) == 0",
            "state": "arity-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:434-436 — arityErrorf(\"%s: requires at least 1 argument\", name), name is exactly \"max\" or \"min\" (arithmetic.go:216-226)"
          },
          {
            "input": "budget.Step() error before the leading argument",
            "state": "budget-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:441-443 — finishBuiltin(budget, nil, err); inventory branch label \"leading step error return\" (internal/inventory/result_data.go:535-542)"
          },
          {
            "input": "leading operand is core.Int",
            "state": "int-exact",
            "effect": "set",
            "evidence": "spec requirement \"Integer min and max preserve exact extrema\" — the candidate takes v.V with no float64 conversion. The site is the leading-operand switch, which today reads `case core.Int: result = float64(v.V)` at plugins/stdlib/arithmetic.go:445-446 and is exactly what this change replaces."
          },
          {
            "input": "leading operand is core.Float",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:447-449 — hasFloat = true at the leading operand"
          },
          {
            "input": "leading operand is neither core.Int nor core.Float",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:450-452 — typeErrorf(\"%s: expected number, got %T\", name, args[0])"
          },
          {
            "input": "budget.Step() error inside the argument loop",
            "state": "budget-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:455-457; inventory branch label \"step error return\" (internal/inventory/result_data.go:551-558)"
          },
          {
            "input": "core.Int operand while int-exact and (isMax && v.V > candidate) || (!isMax && v.V < candidate)",
            "state": "int-exact",
            "effect": "set",
            "evidence": "spec scenario \"Adjacent large integers remain distinguishable\"; the comparison is int64 vs int64, never float64 vs float64 (plugins/stdlib/arithmetic.go:469-477 is the comparison shape to keep, with int64 operands)"
          },
          {
            "input": "core.Int operand while int-exact and the strict comparison is false, including an equal operand",
            "state": "int-exact",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:469-477 — strict > / <, so on a tie the earlier operand is kept (first-wins)"
          },
          {
            "input": "first core.Float operand while int-exact",
            "state": "float-promoted",
            "effect": "forced",
            "evidence": "spec requirement \"If any argument is a Float, the operation SHALL retain its existing float promotion, comparison, and result type behavior\" — promotion sets the float candidate to float64(intCandidate) and then runs the SAME comparison against the float operand at plugins/stdlib/arithmetic.go:469-477; the operand-reading switch is at arithmetic.go:462-464, which today only sets result and hasFloat. Promotion must never adopt the float operand as the candidate unconditionally."
          },
          {
            "input": "core.Int operand while float-promoted and (isMax && float64(v.V) > candidate) || (!isMax && float64(v.V) < candidate)",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:460-461, 469-477 — existing rounding behavior is retained deliberately (design.md Decisions, Risks)"
          },
          {
            "input": "core.Int operand while float-promoted and the strict float64 comparison is false, including an operand equal after conversion",
            "state": "float-promoted",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:460-461, 469-477 — existing rounding behavior is retained deliberately (design.md Decisions, Risks)"
          },
          {
            "input": "core.Float operand while float-promoted and (isMax && v.V > candidate) || (!isMax && v.V < candidate)",
            "state": "float-promoted",
            "effect": "set",
            "evidence": "plugins/stdlib/arithmetic.go:462-463, 469-477"
          },
          {
            "input": "core.Float operand while float-promoted and the strict float64 comparison is false, including an equal operand and a NaN operand",
            "state": "float-promoted",
            "effect": "no-op",
            "evidence": "plugins/stdlib/arithmetic.go:462-463, 469-477"
          },
          {
            "input": "non-number operand in the loop in any state",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:465-467 — typeErrorf(\"%s: expected number, got %T\", name, arg)"
          },
          {
            "input": "argument list exhausted while int-exact",
            "state": "int-exact",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:483 is the branch (today finishBuiltin(budget, core.BoxInt(int64(result)), nil)); after the change it hands core.BoxInt(candidate) with no float64 round trip. Inventory branch label \"int extremum return\" (internal/inventory/result_data.go:575-582); spec scenario \"Singleton endpoint retains its value\"."
          },
          {
            "input": "argument list exhausted while float-promoted",
            "state": "float-promoted",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:480-482 — finishBuiltin(budget, core.Float{V: candidate}, nil); inventory branch label \"float extremum return\" (internal/inventory/result_data.go:567-574); spec scenario \"A float operand preserves float promotion\""
          }
        ],
        "forbidden": [
          "int-exact -> float-promoted that adopts the float operand without running the pending comparison: (max 5 1.5) would return Float 1.5 instead of Float 5",
          "float-promoted -> int-exact: there is no demotion; (max 2 1.5) is core.Float{V:2} and (min 1 1.5) is core.Float{V:1}, never core.Int",
          "any float64(operand) conversion while in int-exact",
          "int64(floatCandidate) to recover an integer result (design.md Decision 3): adjacent integers have already collapsed at that point",
          "a second argument traversal or a second core.NewBuiltinWorkBudget call inside minMaxFunc: it changes the detected work-phase count from 2 (plugins/stdlib/inventory_source_test.go:902-912, 1174-1177)",
          "an eighth value-yielding return statement inside the returned closure without a matching ResultBranches row (plugins/stdlib/inventory_source_test.go:914-919, 1257-1260)",
          "any exit from the closure that does not go through finishBuiltin: UNFLUSHED_RETURN (plugins/stdlib/inventory_source_test.go:1196-1199, 1280-1292)",
          "relaxing >/< to >=/<=: it flips tie resolution from first-wins to last-wins",
          "returning core.Int{V: candidate} instead of core.BoxInt(candidate): it drops the shared-instance path for -128..1023 (core/types.go:105-113)",
          "a test that seeds a state by constructing core.GoFunc or the closure directly, or by writing a candidate field"
        ],
        "seeding": [
          "kernel values, tree-walking evaluator: setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19) then eval(t, env, \"(max 9223372036854775807)\") (plugins/stdlib/stdlib_test.go:21-36). This is the only legal seeding path for int-exact and float-promoted at the unit level.",
          "typed errors and budget states: collectionGoFunc(t, env, \"max\").Fn(ctx, nil, args, env) — see plugins/stdlib/numeric_budget_test.go:71-76 and the builtinErr(t, env, name, args...) helper used at numeric_budget_test.go:246-253.",
          "budget-refused only through core.WithEvalResourceLimits(ctx, 100, 1<<30), core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond)), or a cancelled parent context — exactly as plugins/stdlib/numeric_budget_test.go:67-115 does. Never by calling budget methods from a test.",
          "source literals: core.Read parses a digit token with an optional leading '-' as one number (core/reader.go:351-363) and parseNumber returns BoxInt via strconv.ParseInt(s, 10, 64) (core/reader.go:766-779), so \"-9223372036854775808\" is a single core.Int literal, not (- 9223372036854775808).",
          "names — concrete types: core.Int struct{ V int64 } (core/types.go:74), read as got.(core.Int).V; core.Float struct{ V float64 } (core/types.go:116), read as got.(core.Float).V; constructor for the int result is core.BoxInt(int64) core.Value (core/types.go:108).",
          "names — assertion: core.Int.Equals returns false for a core.Float and vice versa (core/types.go:81-86, 123-128), so got.Equals(core.Int{V: want}) already discriminates the concrete type; the existing table at plugins/stdlib/stdlib_test.go:261-268 uses exactly that.",
          "names — errors: arityErrorf returns *core.LispicoError{Code: \"ArityError\"} and typeErrorf returns *core.LispicoError{Code: \"TypeError\"} (plugins/stdlib/errors.go:13-20); Error() renders \"<Code>: <Message>\" when Source is empty (core/error.go:22-27). Assert with require.ErrorAs(t, err, &le) then le.Code and require.Contains(le.Message, ...), as plugins/stdlib/numeric_budget_test.go:246-253 does. Exact messages: \"max: requires at least 1 argument\", \"min: requires at least 1 argument\", \"max: expected number, got core.String\".",
          "names — constants a test may name: math.MaxInt64 (9223372036854775807), math.MinInt64 (-9223372036854775808), 9007199254740992, 9007199254740993, -9007199254740992, -9007199254740993."
        ],
        "budgets": [
          "budget.Step() calls per invocation: exactly len(args) — 1 for the leading argument plus 1 per remaining argument. Unchanged by this fix.",
          "work phases detected in minMaxFunc: exactly 2 (one range loop + one core.NewBuiltinWorkBudget call), matching the 2 WorkPhases rows at internal/inventory/work_data.go:213-228.",
          "value-yielding return statements detected inside the closure: exactly 7, matching the 7 ResultBranches rows at internal/inventory/result_data.go:527-582. The outer `return func(...)` in minMaxFunc is not counted, because minMaxFunc's own result type is a func type, not core.Value (plugins/stdlib/inventory_source_test.go:453-464, 914-919).",
          "budget batch interval: 128 units (core/builtin_budget.go:23-32); the traversal tests drive 200 arguments (plugins/stdlib/numeric_budget_test.go:17).",
          "exact integer range preserved: the whole signed 64-bit range, -9223372036854775808..9223372036854775807. float64 loses exactness above 2^53 = 9007199254740992, and math.MinInt64 = -2^63 is the one endpoint float64 already represents exactly."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.1"
      ]
    },
    {
      "id": "minmax-public-dispatch-parity",
      "tasks": [
        "1.2"
      ],
      "summary": "Runtime-level coverage that both execution modes and both public entry points return the same exact core.Int, and that the two refusals survive the Engine boundary unchanged. No production code: neither core/vm nor core/compiler has a native min or max (zero hits for \"max\"/\"min\" in core/vm/*.go and core/compiler/*.go), so both modes reach the same stdlib GoFunc and the fix in seam 1 covers both. Red only.",
      "contract": {
        "states": [
          "mode-tree-walker (engine built with runtime.WithTreeWalker(), runtime/engine.go:158)",
          "mode-vm (engine built with runtime.WithBytecode(), runtime/engine.go:149)",
          "named-call (dispatch through Engine.Call(ctx, name, args...), runtime/engine.go:33, implementation runtime/eval.go:838)",
          "source-eval (dispatch through Engine.Eval(ctx, source, input), runtime/engine.go:23)",
          "arity-refused (a zero-argument max or min at the Engine boundary yields a non-nil error carrying *core.LispicoError with Code \"ArityError\" and Message \"max: requires at least 1 argument\" / \"min: requires at least 1 argument\", and a nil value)",
          "type-refused (a non-number operand at the Engine boundary yields a non-nil error carrying *core.LispicoError with Code \"TypeError\" and Message \"max: expected number, got core.String\", and a nil value)"
        ],
        "transitions": [
          {
            "input": "newReentrantCallDepthEngine(t, WithTreeWalker())",
            "state": "mode-tree-walker",
            "effect": "set",
            "evidence": "runtime/vm_reentrant_call_depth_test.go:29-38 — func newReentrantCallDepthEngine(t testing.TB, opts ...EngineOption) Engine; clojure dialect, stdlib loaded, Close registered through t.Cleanup, no ResourceLimits"
          },
          {
            "input": "newReentrantCallDepthEngine(t, WithBytecode())",
            "state": "mode-vm",
            "effect": "set",
            "evidence": "runtime/vm_reentrant_call_depth_test.go:29-38 with runtime/engine.go:149; last-wins if both options are passed, so a test passes exactly one (CLAUDE.md, TCO section)"
          },
          {
            "input": "eng.Call(ctx, \"max\", core.Int{V: math.MaxInt64}) in either mode",
            "state": "named-call",
            "effect": "forced",
            "evidence": "spec scenario \"Public dispatch agrees across execution modes\"; result must satisfy got.Equals(core.Int{V: math.MaxInt64})"
          },
          {
            "input": "eng.Call(ctx, \"min\", core.Int{V: math.MinInt64}) in either mode",
            "state": "named-call",
            "effect": "forced",
            "evidence": "spec scenario \"Singleton endpoint retains its value\"; this row already passes at HEAD (float64(math.MinInt64) round-trips exactly) and is characterization, not red"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(max 9007199254740992 9007199254740993)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "spec scenario \"Adjacent large integers remain distinguishable\"; expect core.Int{V: 9007199254740993}, and the reversed operand order expects the same"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(min 9007199254740992 9007199254740993)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "same scenario; expect core.Int{V: 9007199254740992} in either operand order"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(max -9223372036854775808 9223372036854775807)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "spec scenario \"Negative and mixed-sign integer extrema remain exact\"; expect core.Int{V: math.MaxInt64}, and min expects core.Int{V: math.MinInt64}"
          },
          {
            "input": "eng.Eval(ctx, \"minmax\", \"(max 2 1.5)\") in either mode",
            "state": "source-eval",
            "effect": "forced",
            "evidence": "spec scenario \"A float operand preserves float promotion\"; expect core.Float{V: 2}, and (min 1 1.5) expects core.Float{V: 1}"
          },
          {
            "input": "eng.Call(ctx, \"max\") and eng.Eval(ctx, \"minmax\", \"(max)\") in either mode",
            "state": "arity-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:434-436 produces the error; the Engine boundary does not re-wrap it — runtime/eval.go wraps only a recovered panic (eval.go:690-695) and returns a plain fmt.Errorf only for an undefined name (eval.go:915-921) — so both entry points are asserted to yield the SAME *core.LispicoError Code and Message, reached with require.ErrorAs(t, err, &le). Assert le.Message, not err.Error(), because Error() prefixes the position when Source is set (core/error.go:22-27)"
          },
          {
            "input": "eng.Call(ctx, \"max\", core.Int{V: 1}, core.String{V: \"a\"}) and eng.Eval(ctx, \"minmax\", \"(max 1 \\\"a\\\")\") in either mode",
            "state": "type-refused",
            "effect": "forced",
            "evidence": "plugins/stdlib/arithmetic.go:465-467; same non-re-wrapping argument as the arity row, so both entry points are asserted to yield the same Code \"TypeError\" and the same Message \"max: expected number, got core.String\""
          },
          {
            "input": "no dialect option passed (runtime.New default is the CL dialect)",
            "state": "named-call",
            "effect": "no-op",
            "evidence": "the names \"max\" and \"min\" are not remapped by either dialect (zero hits in cl/cl.go and clojure/clojure.go), so the same source text works under both; newReentrantCallDepthEngine pins the clojure dialect explicitly (runtime/vm_reentrant_call_depth_test.go:31)"
          }
        ],
        "forbidden": [
          "asserting a dedicated VM opcode or a VM-native min/max fast path: none exists",
          "binding a Lisp-level replacement for max or min in the test engine instead of exercising the registered builtin (plugins/stdlib/arithmetic.go:216-226)",
          "asserting only the numeric value through a float64 conversion: the assertion must pin the concrete core.Int type",
          "passing both WithTreeWalker() and WithBytecode() to one engine",
          "building the engine through newMeteringStdlibEngine(t, bytecode, limits) (runtime/resource_limits_test.go:29) or any helper that takes a mandatory ResourceLimits: this seam asserts values and refusals, not accounting, and a ceiling would let a terminal ResourceLimitError mask the ArityError and TypeError rows",
          "asserting the refusals on err.Error() rather than on the unwrapped *core.LispicoError fields"
        ],
        "seeding": [
          "engine only through newReentrantCallDepthEngine(t testing.TB, opts ...EngineOption) Engine at runtime/vm_reentrant_call_depth_test.go:29-38 — same package, clojure dialect, stdlib.New() loaded, eng.Close registered via t.Cleanup, and no ResourceLimits. It is passed exactly one of WithTreeWalker() or WithBytecode().",
          "mode table: []struct{ name string; opts []EngineOption }{{\"tree-walker\", []EngineOption{WithTreeWalker()}}, {\"vm\", []EngineOption{WithBytecode()}}} — the existing goldenEvaluatorModes at runtime/cl_adapters_golden_test.go:94-100 is in the same package and may be reused verbatim.",
          "arguments for named-call as core.Value literals (core.Int{V: ...}, core.String{V: \"a\"}); arguments for source-eval as literal source text read by core.Read.",
          "refusal states are reached only by calling max or min with zero arguments or with a core.String operand through Engine.Call / Engine.Eval; never by invoking the GoFunc directly from this package.",
          "context from t.Context(); no resource limits and no deadline, so budget-refused is out of this seam's scope."
        ],
        "budgets": [
          "dispatch paths per case: 2 modes x 2 entry points = 4.",
          "value cases: 5 integer-exactness cases (both int64 endpoints as singletons, adjacent 2^53 pair in both orders for max and for min, adjacent negative 2^53 pair, mixed-sign endpoints) plus the 2 float-promotion cases (max 2 1.5), (min 1 1.5).",
          "refusal cases: 2 (arity-refused, type-refused), each asserted through both entry points in both modes.",
          "no ResourceLimits and no deadline are configured for this seam's engines, so no core.WithEvalResourceLimits value applies."
        ]
      },
      "redTasks": [
        "1.2"
      ]
    },
    {
      "id": "minmax-accounting-inventory",
      "tasks": [
        "2.2"
      ],
      "summary": "NO-RED-WAIVER: the seam freezes no new observable contract — it verifies an expected no-op, and TestWorkInventory_MatchesSource and TestResultInventory_MatchesSource already fail closed in the growth direction, so a red test would only restate an existing check. The waiver is bounded: both reconcilers compare recorded < detected (plugins/stdlib/inventory_source_test.go:1174-1177, 1257-1260), so a REMOVED loop or return is accepted silently and leaves a dead row behind; the forbidden list bans removing a row precisely because nothing else would catch it. NO-TESTER-WAIVER: no tester stage applies for the same reason; the gate is the existing package run, not a new test. CONDITIONAL, and expected to be a no-op. Judgment: the inventory tables need no edit if seam 1 keeps one loop, one core.NewBuiltinWorkBudget and exactly seven value-yielding returns, which the design's own no-second-traversal decision already requires. This seam is verification, not an edit, and does not deserve its own chunk unless the coder's diff adds a return or a loop. If it does, the fix is to add a row, never to relax a check.",
      "contract": {
        "states": [
          "inventory-in-sync (detected work phases <= recorded WorkPhases rows and detected result branches <= recorded ResultBranches rows for plugins/stdlib/arithmetic.go:minMaxFunc)",
          "inventory-drift (a detected count exceeds its recorded count and the reconciler emits MISSING_REGISTRATION)"
        ],
        "transitions": [
          {
            "input": "implementation keeps 1 loop, 1 core.NewBuiltinWorkBudget, 7 value-yielding returns",
            "state": "inventory-in-sync",
            "effect": "no-op",
            "evidence": "internal/inventory/work_data.go:213-228 (2 rows), internal/inventory/result_data.go:527-582 (7 rows); counting logic at plugins/stdlib/inventory_source_test.go:902-919"
          },
          {
            "input": "implementation adds an eighth value-yielding return inside the closure",
            "state": "inventory-drift",
            "effect": "forced",
            "evidence": "plugins/stdlib/inventory_source_test.go:1257-1260; the remedy is one added ResultBranches row with Families []string{\"numeric\"}, Fn \"max min\", File \"plugins/stdlib/arithmetic.go\", Func \"minMaxFunc\", a new BranchLabel, Class \"scalar-singleton\", and no ChargeExpr"
          },
          {
            "input": "implementation adds a second loop or a second budget",
            "state": "inventory-drift",
            "effect": "forced",
            "evidence": "plugins/stdlib/inventory_source_test.go:1174-1177; forbidden anyway by the no-second-traversal decision in design.md"
          },
          {
            "input": "implementation removes a loop or a value-yielding return",
            "state": "inventory-in-sync",
            "effect": "no-op",
            "evidence": "both reconcilers only fire on got < want (plugins/stdlib/inventory_source_test.go:1174-1177, 1257-1260), so a shrink passes silently and leaves a row describing code that no longer exists — the reason removal is on the forbidden list rather than test-enforced"
          },
          {
            "input": "a ResultBranches row is added for minMaxFunc",
            "state": "inventory-in-sync",
            "effect": "set",
            "evidence": "runtime/stdlib_result_ownership_test.go:20-24 — ownershipArmed excludes Class \"scalar-singleton\", so a new min/max row needs no ownership arm and no golden edit in runtime/stdlib_family_goldens_test.go"
          },
          {
            "input": "an existing row is deleted to match a lowered count",
            "state": "inventory-drift",
            "effect": "forced",
            "evidence": "design.md Decision 2 — never weaken the completeness checks"
          }
        ],
        "forbidden": [
          "editing plugins/stdlib/inventory_source_test.go, plugins/stdlib/inventory_guard_test.go or runtime/stdlib_result_ownership_test.go to accommodate the change",
          "adding an allowlist entry or a skip for minMaxFunc",
          "removing a WorkPhases or ResultBranches row — no test catches it, because both reconcilers only fail when detected exceeds recorded",
          "adding a ResultBranches row that names a ChargeExpr while Class is \"scalar-singleton\" (runtime/stdlib_result_ownership_test.go:247-251)"
        ],
        "seeding": [
          "the reconcilers are driven only by the package test run: TestWorkInventory_MatchesSource and TestResultInventory_MatchesSource (plugins/stdlib/inventory_source_test.go:1268-1278) parse the source tree under moduleRoot(t); no fixture construction is legal.",
          "TestWorkInventory_BudgetHoldersReturnThroughFinishHelper (plugins/stdlib/inventory_source_test.go:1280) is the guard for the finishBuiltin exit rule.",
          "TestResultOwnership_EveryInventoriedBranchHasAnArm lives in the runtime package (runtime/stdlib_result_ownership_test.go:238) and runs with the runtime package test."
        ],
        "budgets": [
          "recorded rows today: 2 WorkPhases, 7 ResultBranches for plugins/stdlib/arithmetic.go:minMaxFunc.",
          "expected row edits for this change: 0."
        ]
      },
      "codeTasks": [
        "2.2"
      ]
    },
    {
      "id": "minmax-floor-and-changelog",
      "tasks": [
        "3.1",
        "3.2"
      ],
      "summary": "NO-RED-WAIVER: a whole-project gate run and a CHANGELOG entry expose no observable behavior a test can freeze. NO-TESTER-WAIVER: no tester stage applies — the deliverables are a command result and a prose entry, both verified by the floor commands themselves. Whole-project gate and the [Unreleased] fix entry. The entry states exact integer extrema for all-Int calls and must not claim exact mixed integer/float arithmetic, which this change deliberately leaves rounding.",
      "contract": {
        "states": [
          "floor-green (make test with the capped GOTESTFLAGS and make lint both pass)",
          "changelog-recorded (CHANGELOG.md [Unreleased] carries a Fixed entry for min and max)"
        ],
        "transitions": [
          {
            "input": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" and make lint both exit 0",
            "state": "floor-green",
            "effect": "set",
            "evidence": "openspec/changes/stdlib-integer-minmax/tasks.md 3.1"
          },
          {
            "input": "a Fixed subsection is added under ## [Unreleased] in CHANGELOG.md",
            "state": "changelog-recorded",
            "effect": "set",
            "evidence": "CHANGELOG.md heads with ## [Unreleased] followed by ### Added and ### Changed; a ### Fixed subsection is the right home (tasks.md 3.2)"
          },
          {
            "input": "the entry claims exact mixed integer/float results",
            "state": "changelog-recorded",
            "effect": "forced",
            "evidence": "design.md Risks — float promotion still rounds large integers in mixed calls; the entry must say so or stay silent on it, never claim the opposite"
          }
        ],
        "forbidden": [
          "a changelog entry that describes mixed int/float calls as exact",
          "closing 3.1 on a narrowed run instead of the whole-project gate",
          "adding a new CHANGELOG section heading above [Unreleased]"
        ],
        "seeding": [
          "the floor is reached only through the Makefile targets; the repo Makefile defines build, test, test-unit, lint, fmt, profile, profile-report and GOTESTFLAGS ?= -timeout 2m, with no test-fast and no PKG parameter."
        ],
        "budgets": [
          "test wall-clock limit: -timeout 2m; package parallelism -p 2; in-package parallelism -parallel 2."
        ]
      },
      "codeTasks": [
        "3.2"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "When every argument is an `Int`, `min` and `max` SHALL return the exact minimum or maximum as an `Int` throughout the signed 64-bit integer range.",
      "tests": [
        "TestArithmetic_MinMax",
        "TestMinMax_ExactIntegersAcrossDispatchModes"
      ]
    },
    {
      "shall": "A singleton call SHALL return that integer unchanged in value.",
      "tests": [
        "TestArithmetic_MinMax",
        "TestMinMax_ExactIntegersAcrossDispatchModes"
      ]
    },
    {
      "shall": "If any argument is a `Float`, the operation SHALL retain its existing float promotion, comparison, and result type behavior.",
      "tests": [
        "TestArithmetic_MinMax",
        "TestNumeric_ShortCallsKeepExactValuesAndErrors"
      ]
    },
    {
      "shall": "Existing arity, type-error, cancellation, and resource-accounting contracts SHALL remain unchanged.",
      "tests": [
        "TestNumeric_ShortCallsKeepExactValuesAndErrors",
        "TestNumeric_ArgTraversalTerminalUnderLowReductions",
        "TestNumeric_ArgTraversalTerminalUnderExpiredDeadline",
        "TestNumeric_ArgTraversalTerminalUnderCancellation",
        "TestWorkInventory_MatchesSource",
        "TestResultInventory_MatchesSource"
      ]
    },
    {
      "shall": "- **THEN** it SHALL return an `Int` with exactly the supplied value",
      "tests": [
        "TestArithmetic_MinMax",
        "TestMinMax_ExactIntegersAcrossDispatchModes"
      ]
    },
    {
      "shall": "- **THEN** they SHALL return `9007199254740992` and `9007199254740993`, respectively, as `Int` values",
      "tests": [
        "TestArithmetic_MinMax"
      ]
    },
    {
      "shall": "- **THEN** each SHALL select the exact integer extremum without rounding or changing its sign",
      "tests": [
        "TestArithmetic_MinMax"
      ]
    },
    {
      "shall": "- **THEN** they SHALL return `Float` values `2` and `1`, respectively",
      "tests": [
        "TestArithmetic_MinMax"
      ]
    },
    {
      "shall": "- **THEN** both modes SHALL return the same exact integer value and type",
      "tests": [
        "TestMinMax_ExactIntegersAcrossDispatchModes"
      ]
    }
  ],
  "testHarness": [
    "setupEnv — plugins/stdlib/stdlib_test.go:11 — builds a bare core.NewEnv(nil) and runs stdlib New().Init(env) into it; the standard env for every plugins/stdlib table test.",
    "eval — plugins/stdlib/stdlib_test.go:21 — core.Read(code) then core.NewEvaluator().Eval(context.Background(), forms[0], env); t.Fatal on read or eval error. Returns core.Value. This is the source-string evaluator used by TestArithmetic_MinMax.",
    "evalErr — plugins/stdlib/stdlib_test.go:38 — same read+eval path but returns the error instead of failing; used for reader/eval failure assertions.",
    "TestArithmetic_MinMax table shape — plugins/stdlib/stdlib_test.go:247-269 — `tests := []struct{ name string; input string; expected core.Value }` with rows `{\"max ints\", \"(max 1 5 3)\", core.Int{V: 5}}`, iterated as `for _, tt := range tests { t.Run(tt.name, func(t *testing.T){ result := eval(t, env, tt.input); if !result.Equals(tt.expected) { t.Errorf(\"expected %v, got %v\", tt.expected, result) } }) }`. One shared env built once outside the loop. NOTE: `.Equals` alone is type-strict (core/types.go:81 Int.Equals returns false for a Float), so an exact core.Int expectation already discriminates, but an explicit `got.(core.Int)` + value assert states the intent for the endpoint rows.",
    "callBuiltin — plugins/stdlib/typed_errors_test.go:16 — looks the name up with env.Get, asserts it is a core.GoFunc, and invokes fn.Fn(ctx, core.NewEvaluator(), args, env) directly: no reader, no engine. Use it to drive core.Int arguments that have no exact source literal concern.",
    "builtinErr — plugins/stdlib/typed_errors_test.go:25 — callBuiltin with context.Background(), requires a non-nil error and returns it; the typed-error rows in numeric_budget_test.go assert on it via `require.ErrorAs(t, err, &le)` + le.Code + le.Message.",
    "runClassRows / classRow — plugins/stdlib/typed_errors_test.go:43,50 — table shape {name, builtin, args []core.Value, wantMsg} run against one wantCode; builds its own env via setupEnv.",
    "collectionGoFunc — plugins/stdlib/collections_extra_test.go:328 — env.Get(name) asserted to core.GoFunc and returned, for tests that need to call fn.Fn with a custom ctx (budget/deadline/cancellation).",
    "requireResourceLimit — plugins/stdlib/format_test.go:288 — asserts an error is a core.CodeResourceLimit *core.LispicoError.",
    "numTraversalOps — plugins/stdlib/numeric_budget_test.go:22 — the {name, descending} list of variadic numeric builtins (\"max\" and \"min\" among them) that all three traversal tests iterate; adding or removing a traversal changes nothing here, but max/min must keep charging one Step per argument or those three tests go red.",
    "numArgs — plugins/stdlib/numeric_budget_test.go:41 — builds numArgCount(=200, :17) distinct non-zero core.Int arguments, ascending or descending, so the argument walk crosses the 128-unit batch interval and reaches a sync point.",
    "numDeepList — plugins/stdlib/numeric_budget_test.go:55 — a 200-element core.List used by the deep-comparison budget test; not needed for min/max.",
    "TestNumeric_ShortCallsKeepExactValuesAndErrors — plugins/stdlib/numeric_budget_test.go:122 — characterisation table: subtest \"values\" is `[]struct{ input string; want core.Value }` asserted with `require.Truef(t, got.Equals(tt.want), ...)` over eval(); subtest \"typedErrors\" is `[]struct{ name, builtin string; args []core.Value; wantCode, wantMsg string }` asserted with builtinErr + ErrorAs. The four existing min/max value rows are at :165-168 and the two arity rows at :225-226 — these must keep passing unrelaxed.",
    "numApplyAllocCharge — plugins/stdlib/numeric_budget_test.go:294 — dispatches a fn through core.NewEvaluator().Apply under WithEvalResourceLimits and returns ctx meter AllocationBytes; used by TestNumeric_ResultsStayUnmarked (:331), which requires an arithmetic Int result to stay UNMARKED (no ChargeGoFuncResultBytes call added inside minMaxFunc).",
    "moduleRoot — plugins/stdlib/plain_error_ban_test.go:245 — repo root for the AST reconcilers in inventory_source_test.go.",
    "newReentrantCallDepthEngine — runtime/vm_reentrant_call_depth_test.go:29 — `func(t testing.TB, opts ...EngineOption) Engine`: prepends WithDialect(clojure.Dialect()), appends the caller's opts, New(nil, all...), require.NoError, t.Cleanup(Close), eng.Use(stdlib.New()). No ResourceLimits and no ceiling. THE helper task 1.2 reuses: newReentrantCallDepthEngine(t, WithTreeWalker()) and newReentrantCallDepthEngine(t, WithBytecode()).",
    "newMeteringStdlibEngine — runtime/resource_limits_test.go:29 — `(t testing.TB, bytecode bool, limits ResourceLimits) Engine`: the mode-parameterized builder for RESOURCE-LIMITED tests. It requires a ResourceLimits and its only example ceiling (meteringLimits(t, 1_000_000, 24<<10), :622) can terminate a call with core.CodeResourceLimit before any value comparison, so it is the wrong seam for exactness parity.",
    "meteringLimits — runtime/resource_limits_test.go:90 — `(t, maxReductions, maxAllocationBytes) ResourceLimits` with MaxReaderDepth/MaxStructuralDepth 1<<20, MaxCollectionLen 1<<30, MaxCacheEntries 4096; only for tests that want a ceiling.",
    "newLimitsEngine — runtime/resource_limits_test.go:22 — newMeteringStdlibEngine plus bindBuiltin(t, e, \"+\") ; the extra fake \"+\" binding overwrites stdlib's, so do NOT use it for numeric-value assertions.",
    "bindBuiltin — runtime/eval_test.go:634 — binds hand-written fake \"+\"/\"*\" GoFuncs onto an engine; not a stdlib path.",
    "newEngine (local closure) — runtime/vm_literal_parity_test.go:57 — `func(t *testing.T, d core.Dialect, bytecode bool) Engine`: picks WithTreeWalker()/WithBytecode(), New(nil, WithDialect(d), mode), t.Cleanup(Close). The mode-table + cold/repeated parity idiom to copy (:69-128), but it loads no plugin, so max/min are unbound under it.",
    "goldenEvaluatorModes — runtime/cl_adapters_golden_test.go:93-100 — `var []struct{ name string; opts []EngineOption }` = {\"tree-walker\", WithTreeWalker()} and {\"vm\", WithBytecode()}; the package's existing mode-axis table, iterated at :126 as `for _, mode := range goldenEvaluatorModes { t.Run(mode.name, ...) }`. Pass mode.opts straight into newReentrantCallDepthEngine.",
    "newBytecodeStdlibEngineWithOptions — runtime/bootstrap_isolation_test.go:162 — always prepends WithBytecode() then extra opts, New + Use(stdlib.New()); bytecode-only, no Cleanup.",
    "newGoldenEngine — runtime/cl_adapters_golden_test.go:105 — `(t, d core.Dialect, eager bool, opts ...EngineOption) Engine`; newFamilyEngine (runtime/stdlib_family_goldens_test.go:93) wraps it and binds shared callbacks.",
    "raceEnabled — runtime/norace_test.go:5 / runtime/race_test.go:8 — build-tagged const guarding alloc-count assertions; only needed if the new test measures allocations.",
    "Engine surface for task 1.2 — runtime/engine.go:23 `Eval(ctx context.Context, source, input string) (core.Value, error)` and :33 `Call(ctx context.Context, name string, args ...core.Value) (core.Value, error)`; usage example runtime/dialect_native_op_test.go:109 `eng.Call(ctx, \"add\", core.Int{V: 1}, core.Int{V: 2})`."
  ],
  "floor": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
