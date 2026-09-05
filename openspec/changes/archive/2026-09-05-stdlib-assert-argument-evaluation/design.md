# Design

## Context

`assert` is registered as a Builtin — `env.RegisterValue("assert", core.GoFunc{...}, false)` in
`plugins/stdlib/control.go:registerControl` — so the apply site has already evaluated its
arguments by the time the body runs. The body then calls `eval.Eval` on them a second time,
at `control.go:17` for the condition and `:24` for the message. Those two calls are the only
`eval.Eval` calls in non-test code under `plugins/`.

A second evaluation is the identity for most values, which is why the defect is quiet. It is
not the identity for the two value types that are also forms: a `Symbol` is resolved as a
binding and a `List` is applied as a call.

Constraints the fix inherits:

- Reporting must be identical across four combinations: tree-walker and bytecode VM, Clojure
  and CL. Under CL the builtin is reachable only because `applyVocabulary`
  (`runtime/engine.go:444`, bridge loop at `:492-513`) copies every root-env `GoFunc` into the
  function cell under `IsLisp2()`; `registerControl` writes the value cell alone, and `assert`
  appears in no CL vocabulary entry.
- `isTruthy` (`plugins/stdlib/strings.go:888-896`) treats only `core.Nil` and `core.Bool{V:false}`
  as falsy, so a `Symbol` or `List` condition becomes truthy once it stops being re-evaluated.
  `(assert 'x)` and `(assert (list 1 2))` therefore change from a loud failure to a silent pass.
- Error codes on this path are bare string literals, not constants: `arityErrorf` produces
  `"ArityError"` and `domainErrorf` produces `"EvalError"` (`plugins/stdlib/errors.go:13,24`).
  There is no `core.CodeArityError` or `core.CodeEvalError` to name.
- Stdlib goldens are Go table literals under `runtime/*_test.go`, never on-disk fixtures.
  `assert` has exactly one golden today: an input-identity arm at
  `runtime/stdlib_family_goldens_test.go:503`, which asserts only that the bound collection is
  unmutated.

## Goals / Non-Goals

**Goals:**

- Remove both evaluator re-entries and report against the received arguments.
- Pin the reported `Code` and `Message` for every argument shape across all four
  mode/dialect combinations, byte-exact.
- Retire the two inventory rows that describe branches the fix deletes, and the comment
  above them that states assert evaluates through the caller's evaluator.
- Record the observable reporting change under `[Unreleased]`.

**Non-Goals:**

- The `%.200s` render behind a non-string message is unchanged. Its cost is owned by the
  archived `core-value-walk-sharing-bound`; this change neither widens nor narrows it.
- Registration, arity, truthiness, the error codes and the success return value are held fixed.
- No change to the `core.GoFunc.Fn` signature. The `eval` and `env` parameters become unused
  and stay named, matching the file-local idiom.

## Decisions

**A new golden table, not an extension of an existing one.** Neither existing error table can
express this contract. `stdlibErrorGoldens` (`runtime/stdlib_error_goldens_test.go:30`) carries
`name`/`src`/`code` and no message field, and runs Clojure-only (`:56`); its own header states
messages are deliberately excluded. `familyErrorGolden`
(`runtime/stdlib_family_goldens_test.go:245`) also pins `code` only. The spec requires a message
assertion across the dialect axis, so `c1` adds `runtime/assert_message_goldens_test.go` with
`assertMessageGolden{name, src, code, msg}` run over `goldenEvaluatorModes` × `{"", "cl"}`.
Neither existing table is extended.

**The seams split on whether behavior changes, not on which file is touched.** tasks.md 1.1 and
2.1 flip behavior and can be red. tasks.md 1.2 and 2.2 are worded as verification — "verify the
existing goldens already pass", "verify truthiness, arity errors and the returned value are
unchanged" — and pin behavior byte-identical before and after, so no test in them can fail
first. They are a separate seam carrying `NO-RED-WAIVER`. Keeping them in one seam would put
the waiver marker on the summary that also governs the half which must have a red stage.

**The inventory edit needs its own red assertion, because nothing else forces it.**
`reconcileResult` reports `MISSING_REGISTRATION` only when detected branches exceed recorded
rows — `if got, want := rowsByFunc[key], len(sf.branches); got < want`
(`plugins/stdlib/inventory_source_test.go:1257`). `reconcileWork` has the identical one-sided
count at `:1174`. A row that outlives its branch is tolerated everywhere.
`TestResultOwnership_EveryInventoriedBranchHasAnArm` ignores both rows too: `ownershipArmed`
exempts every `BranchLabel` ending in `"error return"`. Measured: the `control.go` fix applied
alone, with both stale rows still in place, leaves `./plugins/stdlib/`, `./runtime/` and
`./internal/...` fully green. So `c3` authors
`TestResultInventory_AssertBranchesMatchTheFixedCode`, asserting the sorted surviving label set
is exactly six, before deleting the rows.

**Six rows survive, not five.** `"message render error return"` also ends in `"error return"`
but describes the `core.ValueStringContext` error return at `control.go:32-34`, which the fix
does not touch. It is the row a careless reading of "remove the eval error rows" deletes by
mistake.

**`tasks.md` 3.1's "callback-owned" disposition is inapplicable, not deferred.** The
callback-owned demand fires only for an `eval.Eval`/`eval.Apply` call inside a loop
(`plugins/stdlib/inventory_source_test.go:883-898`, consumed at `:1190-1194`).
`registerControl` holds no loop before or after the fix.

**The changelog entry is in scope by decision.** tasks.md named no changelog task; the change
alters the reported `Code` for existing inputs, which embedders may match on. Task 4.1 was added
to tasks.md and `c4-changelog` owns it. It is parallel — it touches no Go package.

## Risks / Trade-offs

- **The reported `Code` changes, not only the message.** A symbol message moves
  `UndefinedError` → `EvalError`; a list message moves `TypeError` → `EvalError`. The
  proposal's compatibility note covers the message text only. This is what task 4.1 announces.
- **`(assert 'x)` and `(assert (list 1 2))` stop failing.** Measured `UndefinedError` and
  `TypeError` before, `core.Nil{}` after, in all four combinations. The spec names this as
  intended. No script can have used the old outcome as a value, but one using the crash as a
  signal loses it.
- **The pre-fix list-message failure is not stable across execution modes.** The tree-walker
  reports `expected function, got core.Int` and the VM `expected callable, got core.Int`.
  Anyone capturing today's wording as a baseline captures a mode-dependent string. Every golden
  must be derived from the contract, never from a run.
- **`runtime/value_walk_publication_test.go` changes mechanism, not outcome.** Its
  `{tree,bytecode}/assert` arms bind a 300-element vector and expect a Terminal refusal.
  Post-fix both arms refuse inside `core.ValueStringContext`'s walk budget, at a measured flip
  point of 4816 bytes = 301 × `core.MeterValueSlotBytes`. Pre-fix the tree arm refused earlier,
  on the eval meter (`allocation limit 4096 bytes exceeded`), never reaching the walk. All five
  assertions hold before and after; only the comment's causal clause is falsified. The margin
  narrows — most sharply in the bytecode arm, which needed 14724 bytes pre-fix and needs 4816
  post-fix — but 301 > 256 holds, and post-fix the requirement is `(len+1) × MeterValueSlotBytes`,
  derivable rather than incidental.
- **A shrunk fixture would silently disarm that arm.** Below 256 elements, or with the
  `meteringLimits` second argument above 4816, the subtest still compiles and still reports
  PASS on its try/catch half while asserting nothing about a Terminal. The rewritten comment
  states the arithmetic so the dependency is visible.
- **The set of inputs reaching `core.ValueStringContext` grows.** A list message previously died
  during re-application and never reached the render. The budget and the 818-byte ceiling are
  unchanged; `proposal.md` should not be read as claiming no new input reaches that branch.
- **The race arm covers `./plugins/...`, which includes `plugins/json`.**
  `TestDecodeHashMap_Scaling` has known thin headroom on its ratio threshold under load. A red
  there is not this change's — re-run it alone before treating it as one.

## Implementation plan

Five chunks. `c4-changelog` is parallel — it touches no Go package. Everything else is serial
on `plugins/stdlib`. The floor runs last.

For an agent without the kernel, the standing rules are: work in the assigned worktree, never
the primary checkout; a contract test once written is read-only — if it looks wrong, stop and
say so rather than edit it; commit with Conventional Commits against the repo's configured git
identity, never `--no-verify` and never an author override; no AI or tool attribution anywhere
in code, comments, commits or output.

### c1-assert-fix — tasks 1.1, 2.1 · seam `assert-received-arguments`

Serial head. Packages `runtime`, `plugins/stdlib`. Coder: `go-coder`.

Sites:

- `plugins/stdlib/control.go` · `registerControl` · anchor `cond, err := eval.Eval(ctx, args[0], env)`
  — delete the line and its 3-line err check (`:18-20`); use `args[0]` directly in the `isTruthy` test.
- `plugins/stdlib/control.go` · anchor `msg, err := eval.Eval(ctx, args[1], env)`
  — replace with `msg := args[1]`, delete the err check (`:25-27`).
- `plugins/stdlib/control.go` · anchor `rendered, err := core.ValueStringContext(ctx, msg)`
  — HOLD FIXED. `ctx` stays live here, so the `ctx` parameter stays used; `eval` and `env`
  become unused and stay **named**.
- `runtime/assert_message_goldens_test.go` — new file.
- `plugins/stdlib/assert_arguments_test.go` — new file.
- `runtime/value_walk_publication_test.go` — the comment rewrite below.
- `runtime/stdlib_error_goldens_test.go` · anchor `var stdlibErrorGoldens = []stdlibErrorGolden{`
  — READ ONLY, the reason a new file is needed.

Red first — `redTests`, each of which must fail on an assertion, not on compilation:
`TestAssertMessage_GoldensAcrossModesAndDialects`, `TestAssertMessage_ModesAndDialectsAgree`,
`TestAssert_DoesNotReEnterTheEvaluator`.

`c1` owns exactly seven golden rows: `(assert false 'x)`, `(assert false (list 1 2))`,
`(assert false '(1 2))`, `(assert 'x)`, `(assert (list 1 2))`, `(assert false x)` with `x`
unbound as the UndefinedError control, and `(assert false "boom")` as the String control.
Every other input class belongs to `c2` and must get no golden row here.

The error types and naming rulings the tests assert:

- Codes are bare string literals. `"ArityError"` from `arityErrorf`, `"EvalError"` from
  `domainErrorf`, `"UndefinedError"` from `core.NewUndefinedError`. There is no `core.Code*`
  constant for any of them; do not name one.
- Extract with `var le *core.LispicoError; require.ErrorAs(t, err, &le)`, then `le.Code` and
  `le.Message`. `Error()` prefixes the code and is not what either site formats.
- `core.Symbol.String()` returns the bare name, so `(assert false 'x)` reports
  `assertion failed: x` with no quote mark. `core.String.String()` renders `%q`, which is why a
  list-message golden must use `Int` elements — string elements would quote inside the container
  and pin an unrelated decision.
- `cl.Dialect()` is built `WithoutBracketLiterals()`, so no CL arm may use a bracket literal.

`redRun`:

```sh
go test -timeout 2m ./runtime/ -run 'TestAssertMessage' && go test -timeout 2m ./plugins/stdlib/ -run 'TestAssert_DoesNotReEnterTheEvaluator'
```

`verify`:

```sh
go build ./plugins/stdlib/ ./runtime/ && go test -timeout 2m ./plugins/stdlib/ ./runtime/ && go test -timeout 2m ./runtime/ -run 'TestValueWalk_CallerPublication/(tree|bytecode)/assert' -v && go vet ./plugins/stdlib/ ./runtime/ && golangci-lint run ./plugins/stdlib/... ./runtime/...
```

The `-v` on the third arm is deliberate: without it a `-run` regex that matches no subtest still
exits 0, so the named arms would silently drop out if the subtest were ever renamed.

#### The c1 comment rewrite

`runtime/value_walk_publication_test.go` lines 81-85, replaced verbatim, at three tabs of
indentation, preserving the file's em dashes:

```go
			// assert renders its non-string message through
			// ValueStringContext under the caller's context. The payload is
			// a vector of 300 scalars — one walk unit for the vector plus
			// one per element, 301 against the 256-unit ceiling — so the
			// render refuses before any message is emitted.
```

Lines 86-100 — the `bigVec` construction, the `Bind`, both `Eval` calls and every assertion —
stay exactly as they are. The budget numbers behind that comment: the ceiling is
`4096 / core.MeterValueSlotBytes` = 256 units; the fixture costs 301; the margin is 45 units;
`4816 = 301 × 16` is the measured flip point on both arms.

### c2-assert-invariants — tasks 1.2, 2.2 · seam `assert-invariants-hold`

`NO-RED-WAIVER: every assertion in this seam pins behavior that is byte-identical before and
after the fix, so no test here can fail first — tasks.md 1.2 and 2.2 are worded as verification,
not as new behavior.` Serial behind `c1`, shares `plugins/stdlib`. Coder: `go-coder`.

Authors `plugins/stdlib/assert_invariants_test.go` · `TestAssert_InvariantsUnchanged`, covering
exactly the eleven input classes this seam rows and no others. A Symbol or List **condition** is
out of scope here — those two shapes change and are owned by `c1`.

Confirms `TestAssert_MessageBoundedAndUnchanged` and `TestStdlibFamilies_InputsUnchanged` stay
green, editing neither file.

### c3-inventory-rows — task 3.1 · seam `assert-inventory-rows`

Serial behind `c2`, shares `plugins/stdlib`. Coder: `go-coder`.
`redTests`: `TestResultInventory_AssertBranchesMatchTheFixedCode`.

Red first: the sorted `BranchLabel` set of every `ResultBranches` row with `Fn == "assert"` is
exactly `["arity error return", "bare failure return", "message render error return",
"string failure return", "success nil return", "value failure return"]`. Then delete the
`"condition eval error return"` row (`:2644-2651`) and the `"message eval error return"` row
(`:2652-2659`) and rewrite the comment at `:2634-2635`.

`internal/inventory/work_data.go` is HOLD FIXED — both work rows survive; do not touch the
`Proof` text, the bounded-exception disposition, or `MaxWork 818`.

### c4-changelog — task 4.1 · seam `changelog-entry`

`NO-RED-WAIVER: a CHANGELOG.md entry carries no assertion and no test can be red on it.`
Parallel, own shard. Coder: `zpatcher`. One bullet in the existing `[Unreleased]` → `Changed`
list (`## [Unreleased]` at `:8`, `### Changed` at `:10`, bullets from `:12`), naming the two
`Code` transitions and the silent-pass shift. No version heading, no release cut.

### c5-floor — task 3.2 · seam `full-floor-verification`

`NO-RED-WAIVER` / `NO-TESTER-WAIVER`. Serial behind `c3`. Coder: `zpatcher`.

### Floor

```sh
make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint
```

`make test` is `go test -timeout 2m ./...` (Makefile `:14-15`); `make lint` is
`golangci-lint run` (`:19-20`). The Makefile has no race target and no vet target, so those two
arms are a stated fallback; both carry a wall-clock limit.

### Mode, lenses, review

Testing mode **existing-service-strict**: a shipped interpreter whose stdlib package carries
three source-reconciled inventories and a runtime golden matrix. MVP-light would leave the
reported bytes unpinned and the stale inventory rows undetected, since nothing in the current
suite fails when a row outlives its branch.

Lenses **spec** and **quality** — tier standard, no risk signal triggers `arch`, `sec` or
`perf`. The render budget is held fixed by the contract, so a performance lens would have
nothing to judge.

Plan reviewed by a second `zarchitect` that did not author the design. It returned one blocker —
the invariants chunk was instructed to assert pre-fix output for the two condition shapes that
change — plus nine warnings. The blocker was fixed by restricting that chunk to the ten input
classes its own transition table rows; the warnings were fixed in place.

## Plan appendix

```json
{
  "v": 2,
  "change": "stdlib-assert-argument-evaluation",
  "baseSha": "2d660deea432285ac62fa8f895b72921cfb61af9",
  "generatedAt": "2026-09-05T11:10:57.153Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality"
  ],
  "chunks": [
    {
      "id": "c1-assert-fix",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "assert-received-arguments",
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
          "task": "1.1",
          "file": "runtime/assert_message_goldens_test.go",
          "symbol": "assertMessageGoldens / TestAssertMessage_GoldensAcrossModesAndDialects",
          "anchor": "NEW FILE",
          "new": true,
          "change": "New table assertMessageGolden{name, src, code, msg} run over goldenEvaluatorModes x {\"\", \"cl\"}. Carry the rows whose reporting changes plus the UndefinedError control; the invariant rows belong to c2. stdlibErrorGoldens cannot host it: Code-only and Clojure-only."
        },
        {
          "task": "1.1",
          "file": "runtime/stdlib_error_goldens_test.go",
          "symbol": "stdlibErrorGoldens",
          "anchor": "var stdlibErrorGoldens = []stdlibErrorGolden{",
          "change": "READ ONLY - the reason a new file is needed. Rows carry no message field and it runs Clojure-only (loadStdlibEngine(t, clojure.Dialect(), ...) at :56). Do not extend it."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/control.go",
          "symbol": "registerControl (condition path)",
          "anchor": "cond, err := eval.Eval(ctx, args[0], env)",
          "change": "Delete this line and its 3-line err check; use args[0] directly in the isTruthy test below."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/control.go",
          "symbol": "registerControl (message path)",
          "anchor": "msg, err := eval.Eval(ctx, args[1], env)",
          "change": "Replace with msg := args[1] and delete the 3-line err check."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/control.go",
          "symbol": "registerControl (render, HOLD FIXED)",
          "anchor": "rendered, err := core.ValueStringContext(ctx, msg)",
          "change": "UNCHANGED. ctx stays live here so the ctx parameter stays used; eval and env become unused and stay NAMED (file-local idiom; golangci-lint verified clean on a patched copy)."
        },
        {
          "task": "2.1",
          "file": "runtime/value_walk_publication_test.go",
          "symbol": "TestValueWalk_CallerPublication/{tree,bytecode}/assert",
          "anchor": "because the tree-walker hands GoFuncs pre-evaluated args that",
          "change": "Replace lines 81-85 with the exact five-line comment given in the design body under \"The c1 comment rewrite\" - three tabs of indentation, em dashes preserved. Change nothing else in the file; the subtest is green before and after, but the tree arm changes refusal mechanism from the eval meter to the walk budget."
        },
        {
          "task": "2.1",
          "file": "plugins/stdlib/assert_arguments_test.go",
          "symbol": "TestAssert_DoesNotReEnterTheEvaluator / assertRefusingEvaluator / errAssertReentry",
          "anchor": "NEW FILE",
          "new": true,
          "change": "New direct-apply test driving every argument shape through fn.Fn with an Evaluator whose Eval and Apply both return errAssertReentry. Red today: control.go:17 surfaces it on every call."
        }
      ],
      "contract": {
        "states": [
          "reported-code",
          "reported-message",
          "returned-value",
          "condition-source",
          "message-source",
          "evaluator-reentry",
          "mode-dialect-agreement"
        ],
        "transitions": [
          {
            "input": "condition a core.Symbol: (assert 'x) with x unbound  /  args[0] = core.Symbol{V: \"x\"}",
            "state": "condition-source = args[0]; returned-value = core.Nil{}, no error",
            "effect": "clear",
            "evidence": "spec requirement 'an argument whose value is a Symbol SHALL NOT be resolved as a binding'; isTruthy returns true for a Symbol (plugins/stdlib/strings.go:895). BEFORE: measured UndefinedError / \"undefined: x\" in all four combinations, from plugins/stdlib/control.go:17. AFTER: measured core.Nil{} in all four."
          },
          {
            "input": "condition a core.List: (assert (list 1 2)) and (assert (hash-map :a 1))  /  args[0] = core.NewList([]core.Value{core.Int{V:1}, core.Int{V:2}})",
            "state": "condition-source = args[0]; returned-value = core.Nil{}, no error",
            "effect": "clear",
            "evidence": "spec requirement 'an argument whose value is a List SHALL NOT be applied as a call'; isTruthy at plugins/stdlib/strings.go:895. BEFORE: measured TypeError, and the two modes disagreed - tree-walker \"expected function, got core.Int\" vs VM \"expected callable, got core.Int\". AFTER: measured core.Nil{} in all four combinations."
          },
          {
            "input": "two args, message a core.Symbol: (assert false 'x) and (assert nil 'y)  /  args[1] = core.Symbol{V: \"x\"}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: x\" (no quote mark; Symbol.String() returns s.V)",
            "effect": "set",
            "evidence": "plugins/stdlib/control.go:31-35 through core.ValueStringContext scalarRender (core/value_walk_context.go:212-223) and core/types.go:149. BEFORE: measured UndefinedError / \"undefined: x\" in all four combinations. AFTER: measured \"assertion failed: x\" in all four."
          },
          {
            "input": "two args, message a core.List: (assert false (list 1 2)) and (assert false '(1 2))",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: (1 2)\" (space-separated elements in parentheses)",
            "effect": "set",
            "evidence": "plugins/stdlib/control.go:31-35 through core.ValueStringContext walkString's List case (core/value_walk_context.go:128-143). BEFORE: measured TypeError with a mode split - \"expected function, got core.Int\" (tree-walker) vs \"expected callable, got core.Int\" (VM). AFTER: measured \"assertion failed: (1 2)\" in all four combinations."
          },
          {
            "input": "two args, message an unquoted unbound symbol: (assert false x) with x unbound",
            "state": "reported-code = UndefinedError, reported-message = \"undefined: x\" - not an assertion failure, not EvalError",
            "effect": "forced",
            "evidence": "core.NewUndefinedError, core/error.go:76-78; refused by symbol resolution at the apply site before the builtin is entered. Measured identical before and after the fix in all four combinations. This row exists so no golden confuses it with the quoted-symbol case, whose pre-fix output was byte-identical to it."
          },
          {
            "input": "any argument shape dispatched through fn.Fn(ctx, ev, args, env) with ev an assertRefusingEvaluator whose Eval and Apply both return errAssertReentry",
            "state": "evaluator-reentry = false: errAssertReentry never surfaces on any path",
            "effect": "clear",
            "evidence": "task 2.1. Today plugins/stdlib/control.go:17 surfaces it for every call and :24 for every two-argument falsy call. core.Evaluator is the two-method interface at core/types.go:21-24, so the stub is exactly Eval and Apply."
          },
          {
            "input": "each of the 7 golden sources run under goldenEvaluatorModes x {clojure.Dialect(), cl.Dialect()}",
            "state": "mode-dialect-agreement = true: the (Code, Message) pair is byte-identical across all four runs",
            "effect": "forced",
            "evidence": "spec scenario 'Reporting is identical across execution modes and dialects'. assert carries no VM native opcode and no CL rename (cl/cl.go:201-226 renames only set!/do, adds defun, and adapts nth/mapcar/sort), and is reachable under CL only through the Lisp-2 GoFunc bridge at runtime/engine.go:492-513. Measured: the measurement ran over 15 candidate sources and all four combinations agreed on every one; the table keeps the 7 listed in redTasks and c2 owns the remaining 8 input classes; pre-fix they disagree on the three list-shaped sources."
          },
          {
            "input": "(assert false bigvec) with bigvec a 300-element core.Vector, under meteringLimits(t, 1_000_000, 4<<10)",
            "state": "reported-code = core.CodeResourceLimit, Terminal, no result published; the refusal comes from core.ValueStringContext's walk budget in both execution modes",
            "effect": "forced",
            "evidence": "runtime/value_walk_publication_test.go:79-101; measured flip point 4816 = 301 * core.MeterValueSlotBytes on both arms post-fix, against 4964 (tree) and 14724 (bytecode) pre-fix. Pre-fix the tree arm refused on the eval meter (\"allocation limit 4096 bytes exceeded\"), not the walk; the fix converts it to the walk refusal the comment describes."
          }
        ],
        "forbidden": [
          "Any call to eval.Eval or eval.Apply inside the assert GoFunc body after the fix.",
          "Renaming the GoFunc's unused parameters to _. The file-local idiom keeps them named even when unused - every GoFunc in plugins/stdlib/types.go:12,24,37,50,63,76,89,102,115,128,141,154 spells (ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) and uses only some of them. Verified: golangci-lint run ./plugins/stdlib/... reports 0 issues on the patched copy with eval and env left named.",
          "Changing arityErrorf/domainErrorf to another constructor, altering the format strings \"assertion failed\", \"assertion failed: %.200s\" (both sites), or reordering the core.String branch ahead of the render.",
          "Replacing core.ValueStringContext with core.ValueString or with a direct %v: the ctx-carried walk budget is what makes the non-string branch budgeted (internal/inventory/work_data.go:993-1012), and the render's cost is owned by the archived core-value-walk-sharing-bound.",
          "Adding a nil check, a type check, an arity ceiling, or a resource charge that is not there today - three or more arguments stay legal and assert holds no budget.",
          "Making assert a special form, or registering it through anything other than env.RegisterValue(\"assert\", core.GoFunc{...}, false).",
          "A golden that asserts an assertion-failure message for (assert false x) with x unbound: that argument fails at the apply site and must keep failing there.",
          "A golden that reaches a symbol or list message by binding a name in the engine env and relying on the lookup - the value must arrive from a quote, a constructor call, or a literal.",
          "A golden whose expected string was captured from a run rather than derived from the contract (runtime/stdlib_family_goldens_test.go:171-173 states the rule for this harness).",
          "A list-message golden whose elements are strings: element rendering inside a container goes through walkString's scalarRender and quotes a core.String (core/types.go:134-136), pinning an unrelated decision. Use Int elements.",
          "Bracket-literal sources ([1 2], [x] fn params) in any CL arm: cl.Dialect() is built WithoutBracketLiterals() (cl/cl.go:204)."
        ],
        "seeding": [
          "engine-level goldens (runtime) — eng := loadStdlibEngine(t, familyDialect(dia), true, mode.opts...) with dia ranging over {\"\", \"cl\"} and mode over goldenEvaluatorModes; then eng.Eval(context.Background(), \"assert-goldens\", g.src). loadStdlibEngine is runtime/bootstrap_goldens_test.go:36-52; goldenEvaluatorModes is runtime/cl_adapters_golden_test.go:94-100; familyDialect is runtime/stdlib_family_goldens_test.go:18-23.",
          "reading Code and Message — var le *core.LispicoError; require.ErrorAs(t, err, &le); then le.Code and le.Message. Error() prefixes the code (core/error.go:22-27) and is not what either site formats - the same helper shape as plugins/stdlib/higher_order_budget_test.go:386-391 and runtime/stdlib_error_goldens_test.go:62-64.",
          "reported-code = UndefinedError — only by naming an identifier the engine env does not bind: source (assert false x), with no eng.Bind(\"x\", ...) anywhere in the test. Never by constructing the error in the test.",
          "message-source = core.Symbol — in source only as 'x (a quote form); at the direct-apply level only as args[1] = core.Symbol{V: \"x\"}.",
          "message-source = core.List — in source only as (list 1 2) or '(1 2); at the direct-apply level only as core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}}).",
          "direct-apply (plugins/stdlib) — env := setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19); fn := collectionGoFunc(t, env, \"assert\") (plugins/stdlib/collections_extra_test.go:328-335); ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30); fn.Fn(ctx, ev, args, env) - the exact shape at plugins/stdlib/higher_order_budget_test.go:376-382.",
          "evaluator-reentry = false — pass ev := assertRefusingEvaluator{} in place of core.NewEvaluator(); core.Evaluator is two methods (core/types.go:21-24), both returning errAssertReentry, and the assertion is errors.Is(err, errAssertReentry) == false on every shape.",
          "mode-dialect-agreement — collect the four (Code, Message) pairs for one source into a map keyed by mode.name+\"/\"+dia and compare each against the first, in the shape of runtime/stdlib_error_goldens_test.go:52-82.",
          "never — no state is reached by writing a field on a core.Value, by constructing a *core.LispicoError in the test and asserting against it, or by binding the message symbol so the lookup succeeds."
        ],
        "budgets": [
          "256 units = the walk ceiling at 4096 bytes (meteringLimits second argument, runtime/value_walk_publication_test.go:43)",
          "301 units = the 300-element fixture's cost, (len+1) * core.MeterValueSlotBytes",
          "45 units of post-fix margin, identical in both arms",
          "200 = the %.200s message precision, unchanged (hoAssertCap, plugins/stdlib/higher_order_budget_test.go:21)",
          "818 = MaxWork on the \"string failure message format\" WorkPhases row, unchanged"
        ]
      },
      "redTasks": [
        "1.1 Author runtime/assert_message_goldens_test.go: TestAssertMessage_GoldensAcrossModesAndDialects over the 7-row assertMessageGoldens table x goldenEvaluatorModes x {\"\", \"cl\"}. The rows that are red today are (assert false 'x), (assert false (list 1 2)), (assert false '(1 2)), and the two success rows (assert 'x) and (assert (list 1 2)).",
        "1.1 Author TestAssertMessage_ModesAndDialectsAgree: for each source, the four (Code, Message) pairs must be byte-identical. Red today on the three list-shaped sources, where the tree-walker says \"expected function, got core.Int\" and the VM says \"expected callable, got core.Int\".",
        "2.1 Author plugins/stdlib/assert_arguments_test.go: TestAssert_DoesNotReEnterTheEvaluator drives every argument shape through fn.Fn with assertRefusingEvaluator and asserts errAssertReentry never surfaces.",
        "c1 assertMessageGoldens rows (the reporting that changes, plus the two controls that disambiguate it): (assert false 'x); (assert false (list 1 2)); (assert false '(1 2)); (assert 'x); (assert (list 1 2)); (assert false x) with x unbound [UndefinedError control, must stay UndefinedError]; (assert false \"boom\") [String control, proves the fast path is untouched]. Seven rows.",
        "c2 owns every remaining input class at the direct-apply level and MUST NOT be given a golden row here: arity, Bool/Nil conditions, the Keyword/Int/Bool/Float/Nil scalar messages, three-or-more arguments. An input class rowed in both chunks seals the same assertion twice."
      ],
      "codeTasks": [
        "2.1 plugins/stdlib/control.go: replace cond, err := eval.Eval(ctx, args[0], env) and its err check (lines 17-20) with the direct use of args[0] in the isTruthy test at :22.",
        "2.1 plugins/stdlib/control.go: replace msg, err := eval.Eval(ctx, args[1], env) and its err check (lines 24-27) with msg := args[1].",
        "2.1 runtime/value_walk_publication_test.go:81-85: replace those five lines with the verbatim comment block given in design.md under \"The c1 comment rewrite\" - three tabs of indentation, em dashes preserved. Do not paraphrase it and change nothing else in the file; the subtest is green before and after."
      ],
      "redTests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects",
        "TestAssertMessage_ModesAndDialectsAgree",
        "TestAssert_DoesNotReEnterTheEvaluator"
      ],
      "redRun": "go test -timeout 2m ./runtime/ -run 'TestAssertMessage' && go test -timeout 2m ./plugins/stdlib/ -run 'TestAssert_DoesNotReEnterTheEvaluator'",
      "verify": "go build ./plugins/stdlib/ ./runtime/ && go test -timeout 2m ./plugins/stdlib/ ./runtime/ && go test -timeout 2m ./runtime/ -run 'TestValueWalk_CallerPublication/(tree|bytecode)/assert' -v && go vet ./plugins/stdlib/ ./runtime/ && golangci-lint run ./plugins/stdlib/... ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "c2-assert-invariants",
      "taskIds": [
        "1.2",
        "2.2"
      ],
      "prev": "c1-assert-fix",
      "sharedPkg": "plugins/stdlib",
      "parallel": false,
      "seam": "assert-invariants-hold",
      "shard": "",
      "pkgDirs": [
        "plugins/stdlib",
        "runtime"
      ],
      "pkgs": [
        "./plugins/stdlib",
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "plugins/stdlib/higher_order_budget_test.go",
          "symbol": "TestAssert_MessageBoundedAndUnchanged",
          "anchor": "func TestAssert_MessageBoundedAndUnchanged(t *testing.T) {",
          "change": "READ ONLY - existing string-message, bare-failure and keyword controls. Must stay green untouched; calls fn.Fn with pre-built values so it is invariant under the fix."
        },
        {
          "task": "1.2",
          "file": "runtime/stdlib_family_goldens_test.go",
          "symbol": "inputArms (assert row)",
          "anchor": "{name: \"assert\", src: `(assert v)`},",
          "change": "READ ONLY - assert's only existing golden, an input-identity arm. Must stay green; no new arm needed (the higher-order family is already armed by map/filter/reduce/apply)."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/assert_invariants_test.go",
          "symbol": "TestAssert_InvariantsUnchanged",
          "anchor": "NEW FILE",
          "new": true,
          "change": "Direct-apply rows for arity, truthiness, the core.Nil{} success return, the core.String fast path and the Keyword/Int/Bool/Float/Nil scalar messages. Separate file from c1's so no sealed file is reopened."
        }
      ],
      "contract": {
        "states": [
          "reported-code",
          "reported-message",
          "returned-value",
          "condition-source",
          "message-source",
          "evaluator-reentry",
          "mode-dialect-agreement"
        ],
        "transitions": [
          {
            "input": "no args: (assert)  /  fn.Fn(ctx, ev, nil, env)",
            "state": "reported-code = ArityError, reported-message = \"assert: requires at least 1 argument\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:13-15 via arityErrorf (plugins/stdlib/errors.go:13-15); already classified at plugins/stdlib/typed_errors_test.go:88 and plugins/stdlib/error_inventory_test.go:99. Measured identical in all four combinations before and after the fix."
          },
          {
            "input": "one arg, truthy: (assert true)  /  args[0] = core.Bool{V: true}",
            "state": "returned-value = core.Nil{}, no error",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:22,40; isTruthy at plugins/stdlib/strings.go:888-896; already pinned at plugins/stdlib/higher_order_budget_test.go:393-397. Measured unchanged."
          },
          {
            "input": "one arg, falsy Bool: (assert false)  /  args[0] = core.Bool{V: false}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:37; already pinned at plugins/stdlib/higher_order_budget_test.go:399-402 (task 1.2 control). Measured unchanged."
          },
          {
            "input": "one arg, condition core.Nil: (assert nil)  /  args[0] = core.Nil{}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/strings.go:889-891 makes core.Nil falsy; plugins/stdlib/control.go:37. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a core.String: (assert false \"boom\")",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: boom\" (raw bytes, unquoted; the %.200s fast path, no walk)",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:28-30 short-circuits on core.String before the render; already pinned at plugins/stdlib/higher_order_budget_test.go:404-409 (task 1.2 control). Measured unchanged in all four combinations. Note core.String.String() would quote it (core/types.go:134-136) - this branch never calls it."
          },
          {
            "input": "two args, message a non-string scalar Keyword: (assert false :k)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: :k\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:161-163; a Keyword is self-evaluating, so the deleted second Eval was the identity. Already pinned at plugins/stdlib/higher_order_budget_test.go:421-429. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a non-string scalar Int: (assert false 7)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: 7\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:77-79. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a non-string scalar Bool: (assert false true)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: true\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:44-49. Measured on the patched copy under tree-walker and VM."
          },
          {
            "input": "two args, message core.Nil: (assert false nil)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: nil\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:37. Measured unchanged in all four combinations."
          },
          {
            "input": "three or more args: (assert false \"a\" \"b\")",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: a\" - only args[1] is read, and there is no upper arity bound",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:23-24 reads args[1] only; :13 bounds arity from below only. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a non-string scalar Float: (assert false 1.5)  /  args[1] = core.Float{V: 1.5}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: 1.5\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:117-119, Float.String() via strconv.FormatFloat(v, 'f', -1, 64). A Float is self-evaluating, so the deleted second Eval was the identity on it."
          }
        ],
        "forbidden": [
          "Any call to eval.Eval or eval.Apply inside the assert GoFunc body after the fix.",
          "Renaming the GoFunc's unused parameters to _. The file-local idiom keeps them named even when unused - every GoFunc in plugins/stdlib/types.go:12,24,37,50,63,76,89,102,115,128,141,154 spells (ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) and uses only some of them. Verified: golangci-lint run ./plugins/stdlib/... reports 0 issues on the patched copy with eval and env left named.",
          "Changing arityErrorf/domainErrorf to another constructor, altering the format strings \"assertion failed\", \"assertion failed: %.200s\" (both sites), or reordering the core.String branch ahead of the render.",
          "Replacing core.ValueStringContext with core.ValueString or with a direct %v: the ctx-carried walk budget is what makes the non-string branch budgeted (internal/inventory/work_data.go:993-1012), and the render's cost is owned by the archived core-value-walk-sharing-bound.",
          "Adding a nil check, a type check, an arity ceiling, or a resource charge that is not there today - three or more arguments stay legal and assert holds no budget.",
          "Making assert a special form, or registering it through anything other than env.RegisterValue(\"assert\", core.GoFunc{...}, false).",
          "A golden that asserts an assertion-failure message for (assert false x) with x unbound: that argument fails at the apply site and must keep failing there.",
          "A golden that reaches a symbol or list message by binding a name in the engine env and relying on the lookup - the value must arrive from a quote, a constructor call, or a literal.",
          "A golden whose expected string was captured from a run rather than derived from the contract (runtime/stdlib_family_goldens_test.go:171-173 states the rule for this harness).",
          "A list-message golden whose elements are strings: element rendering inside a container goes through walkString's scalarRender and quotes a core.String (core/types.go:134-136), pinning an unrelated decision. Use Int elements.",
          "Bracket-literal sources ([1 2], [x] fn params) in any CL arm: cl.Dialect() is built WithoutBracketLiterals() (cl/cl.go:204)."
        ],
        "seeding": [
          "engine-level goldens (runtime) — eng := loadStdlibEngine(t, familyDialect(dia), true, mode.opts...) with dia ranging over {\"\", \"cl\"} and mode over goldenEvaluatorModes; then eng.Eval(context.Background(), \"assert-goldens\", g.src). loadStdlibEngine is runtime/bootstrap_goldens_test.go:36-52; goldenEvaluatorModes is runtime/cl_adapters_golden_test.go:94-100; familyDialect is runtime/stdlib_family_goldens_test.go:18-23.",
          "reading Code and Message — var le *core.LispicoError; require.ErrorAs(t, err, &le); then le.Code and le.Message. Error() prefixes the code (core/error.go:22-27) and is not what either site formats - the same helper shape as plugins/stdlib/higher_order_budget_test.go:386-391 and runtime/stdlib_error_goldens_test.go:62-64.",
          "reported-code = UndefinedError — only by naming an identifier the engine env does not bind: source (assert false x), with no eng.Bind(\"x\", ...) anywhere in the test. Never by constructing the error in the test.",
          "message-source = core.Symbol — in source only as 'x (a quote form); at the direct-apply level only as args[1] = core.Symbol{V: \"x\"}.",
          "message-source = core.List — in source only as (list 1 2) or '(1 2); at the direct-apply level only as core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}}).",
          "direct-apply (plugins/stdlib) — env := setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19); fn := collectionGoFunc(t, env, \"assert\") (plugins/stdlib/collections_extra_test.go:328-335); ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30); fn.Fn(ctx, ev, args, env) - the exact shape at plugins/stdlib/higher_order_budget_test.go:376-382.",
          "evaluator-reentry = false — pass ev := assertRefusingEvaluator{} in place of core.NewEvaluator(); core.Evaluator is two methods (core/types.go:21-24), both returning errAssertReentry, and the assertion is errors.Is(err, errAssertReentry) == false on every shape.",
          "mode-dialect-agreement — collect the four (Code, Message) pairs for one source into a map keyed by mode.name+\"/\"+dia and compare each against the first, in the shape of runtime/stdlib_error_goldens_test.go:52-82.",
          "never — no state is reached by writing a field on a core.Value, by constructing a *core.LispicoError in the test and asserting against it, or by binding the message symbol so the lookup succeeds."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "2.2 The Float scalar message renders through core/types.go Float.String(), strconv.FormatFloat(v, 'f', -1, 64): (assert false 1.5) -> EvalError / \"assertion failed: 1.5\". Read the rendering off that function, not off a run.",
        "1.2 Confirm the existing controls stay green untouched: TestAssert_MessageBoundedAndUnchanged (plugins/stdlib/higher_order_budget_test.go:374) and TestStdlibFamilies_InputsUnchanged (runtime/stdlib_family_goldens_test.go:581). Edit neither file.",
        "2.2 Author plugins/stdlib/assert_invariants_test.go: TestAssert_InvariantsUnchanged - direct-apply rows for exactly the input classes this seam rows in its contract and no others: arity (no args), a truthy core.Bool{V:true} condition, a falsy core.Bool{V:false} condition, a core.Nil condition, the core.Nil{} success return, the core.String message fast path, the Keyword/Int/Bool/Float/Nil scalar messages, and three-or-more arguments. Every row asserts the same Code and Message the pre-fix build produces. A Symbol or List CONDITION is out of scope here - those two shapes change (UndefinedError/TypeError before, core.Nil{} after) and are owned by c1 transitions 1 and 2 and by TestAssert_DoesNotReEnterTheEvaluator; asserting pre-fix output for them here would fail c2 verify.",
        "2.2 plugins/stdlib/control.go: assert by reading, not by editing, that arityErrorf/domainErrorf, both \"assertion failed\" format strings, the isTruthy call and the RegisterValue registration are untouched by the c1 diff."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "go build ./plugins/stdlib/ ./runtime/ && go test -timeout 2m ./plugins/stdlib/ ./runtime/ && go vet ./plugins/stdlib/ ./runtime/ && golangci-lint run ./plugins/stdlib/... ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "c3-inventory-rows",
      "taskIds": [
        "3.1"
      ],
      "prev": "c2-assert-invariants",
      "sharedPkg": "plugins/stdlib",
      "parallel": false,
      "seam": "assert-inventory-rows",
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
          "task": "3.1",
          "file": "plugins/stdlib/assert_inventory_test.go",
          "symbol": "TestResultInventory_AssertBranchesMatchTheFixedCode",
          "anchor": "NEW FILE",
          "new": true,
          "change": "New red assertion: the sorted BranchLabel set of every ResultBranches row with Fn == \"assert\" is exactly the six survivors. This is the ONLY thing forcing the row removal - reconcileResult tolerates surplus rows."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/result_data.go",
          "symbol": "ResultBranches, assert rows + comment",
          "anchor": "// assert evaluates both its condition and its message through the",
          "change": "Delete the \"condition eval error return\" row (:2644-2651) and the \"message eval error return\" row (:2652-2659); rewrite this comment. KEEP \"message render error return\" - it also ends in \"error return\" but describes a surviving branch."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases, assert rows (HOLD FIXED)",
          "anchor": "PhaseLabel:  \"value failure message format\",",
          "change": "UNCHANGED. Both work rows survive: the format branches and the ValueStringContext walk are untouched. Do not edit the Proof text, the bounded-exception disposition, or MaxWork 818."
        }
      ],
      "contract": {
        "states": [
          "assert-result-rows",
          "assert-work-rows",
          "assert-error-rows",
          "assert-comment"
        ],
        "transitions": [
          {
            "input": "the row labelled \"condition eval error return\" (Class \"borrowed\")",
            "state": "assert-result-rows: removed; it described the err return at plugins/stdlib/control.go:18-20, which the fix deletes",
            "effect": "clear",
            "evidence": "internal/inventory/result_data.go:2644-2651"
          },
          {
            "input": "the row labelled \"message eval error return\" (Class \"borrowed\")",
            "state": "assert-result-rows: removed; it described the err return at plugins/stdlib/control.go:25-27, which the fix deletes",
            "effect": "clear",
            "evidence": "internal/inventory/result_data.go:2652-2659"
          },
          {
            "input": "the row labelled \"message render error return\" (Class \"borrowed\")",
            "state": "assert-result-rows: kept unchanged - it describes the core.ValueStringContext error return at plugins/stdlib/control.go:32-34, which survives the fix",
            "effect": "no-op",
            "evidence": "internal/inventory/result_data.go:2668-2675; the render call stays at plugins/stdlib/control.go:31. This row is the one a careless reading of \"remove the eval error rows\" deletes by mistake."
          },
          {
            "input": "the five scalar-singleton rows: \"arity error return\", \"string failure return\", \"value failure return\", \"bare failure return\", \"success nil return\"",
            "state": "assert-result-rows: kept unchanged, five rows",
            "effect": "no-op",
            "evidence": "internal/inventory/result_data.go:2636-2643, :2660-2667, :2676-2683, :2684-2691, :2692-2699"
          },
          {
            "input": "the comment at internal/inventory/result_data.go:2634-2635, \"assert evaluates both its condition and its message through the caller's evaluator, so either error reaches the caller unchanged.\"",
            "state": "assert-comment: rewritten to say assert reports against the arguments the apply site handed it, and that the surviving borrowed return is the render's",
            "effect": "forced",
            "evidence": "the sentence is false after the fix; the spec requirement states the arguments arrive evaluated and SHALL NOT be evaluated again"
          },
          {
            "input": "the two WorkPhases rows: \"string failure message format\" (bounded-exception, MaxWork 818) and \"value failure message format\" (budgeted)",
            "state": "assert-work-rows: assert-work-rows unchanged - both dispositions, both Proof strings and MaxWork 818 kept verbatim",
            "effect": "no-op",
            "evidence": "internal/inventory/work_data.go:978-992 and :993-1012. Both format branches survive the fix untouched. The value row's Proof names core.ValueStringContext and the walk budget; that call site is unchanged."
          },
          {
            "input": "the four errorInventory rows for assert (one ArityError, three EvalError)",
            "state": "assert-error-rows: assert-error-rows unchanged",
            "effect": "no-op",
            "evidence": "plugins/stdlib/error_inventory_test.go:99-102. The three EvalError rows are the three domainErrorf sites at plugins/stdlib/control.go:29,35,37, all of which survive; the deleted err passthroughs were never inventoried there. errorInventory is reconciled against the registration surface (:333-373), not against a per-function source count, so deleting source returns cannot move it."
          },
          {
            "input": "tasks.md 3.1's \"callback-owned\" disposition",
            "state": "assert-result-rows: inapplicable to assert, not a missing deliverable",
            "effect": "no-op",
            "evidence": "the callback-owned demand fires only for an eval.Eval/eval.Apply call inside a loop (plugins/stdlib/inventory_source_test.go:883-898, consumed at :1190-1194). registerControl holds no loop before or after the fix, so sf.opaqueCalls is empty either way."
          },
          {
            "input": "the surplus-row direction of every reconciler in the tree",
            "state": "assert-result-rows: leaving both stale rows in place fails nothing, so the red assertion below is the only thing that forces their removal",
            "effect": "forced",
            "evidence": "reconcileResult compares got < want only (plugins/stdlib/inventory_source_test.go:1257-1259); reconcileWork does the same at :1174. Measured: the control.go fix applied alone, with the two stale rows still in place, leaves ./plugins/stdlib/, ./runtime/ and ./internal/... green."
          },
          {
            "input": "the new file plugins/stdlib/assert_inventory_test.go itself",
            "state": "assert-result-rows: no inventory sweep is triggered by adding it",
            "effect": "no-op",
            "evidence": "invSweepUnscopedFiles skips every path ending in _test.go (plugins/stdlib/inventory_source_test.go:385-387)"
          }
        ],
        "forbidden": [
          "Deleting or reclassifying any of the six surviving assert ResultBranches rows - in particular \"message render error return\", which also ends in \"error return\" but describes a branch that survives.",
          "Touching the WorkPhases Proof text, the bounded-exception disposition, or MaxWork 818.",
          "Adding a ChargeExpr to any assert row: every survivor is scalar-singleton or an error return, and a ChargeExpr on an exempt row is itself a failure (runtime/stdlib_result_ownership_test.go:247-251).",
          "Editing internal/inventory/registered.go:83 or internal/inventory/name_family.go:75 - assert's registration entry and its \"higher-order\" family do not change.",
          "Adding an ownershipArm for either deleted row, or for any surviving assert row: none of the eight is armed.",
          "Asserting a total count of inventory.ResultBranches: nothing in the tree does, and such an assertion would break on every unrelated row change."
        ],
        "seeding": [
          "assert-result-rows — read as data - range over inventory.ResultBranches and select row.Fn == \"assert\". No engine, no evaluation, no source parsing.",
          "the assertion shape — collect the surviving BranchLabel values into a []string, sort it, and require.Equal against the literal sorted six. A count-only assertion would pass if a row were renamed instead of removed.",
          "never — do not reach this state by re-deriving the row set from source with go/ast - that duplicates reconcileResult and would pass for the wrong reason."
        ],
        "budgets": [
          "8 -> 6: inventory.ResultBranches rows with Fn == \"assert\".",
          "2: inventory.WorkPhases rows with Fn == \"assert\", unchanged.",
          "4: errorInventory rows with Fn == \"assert\", unchanged (1 ArityError, 3 EvalError).",
          "81: the registered GoFunc surface, unchanged (plugins/stdlib/error_inventory_test.go:349).",
          "0: ownership arms naming any assert row, before and after."
        ]
      },
      "redTasks": [
        "3.1 Author plugins/stdlib/assert_inventory_test.go: TestResultInventory_AssertBranchesMatchTheFixedCode collects every inventory.ResultBranches row with Fn == \"assert\", sorts the BranchLabel values, and requires exactly [\"arity error return\", \"bare failure return\", \"message render error return\", \"string failure return\", \"success nil return\", \"value failure return\"]. Red today: \"condition eval error return\" and \"message eval error return\" are still present."
      ],
      "codeTasks": [
        "3.1 internal/inventory/result_data.go: delete the \"condition eval error return\" row (:2644-2651) and the \"message eval error return\" row (:2652-2659).",
        "3.1 internal/inventory/result_data.go:2634-2635: rewrite the comment so it states that assert reports against its received arguments, and that the one borrowed return left is the message render's."
      ],
      "redTests": [
        "TestResultInventory_AssertBranchesMatchTheFixedCode"
      ],
      "redRun": "go test -timeout 2m ./plugins/stdlib/ -run 'TestResultInventory_AssertBranchesMatchTheFixedCode'",
      "verify": "go build ./internal/inventory/ ./plugins/stdlib/ ./runtime/ && go test -timeout 2m ./plugins/stdlib/ ./runtime/ && go vet ./internal/inventory/ ./plugins/stdlib/ ./runtime/ && golangci-lint run ./internal/inventory/... ./plugins/stdlib/... ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "c4-changelog",
      "taskIds": [
        "4.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "seam": "changelog-entry",
      "shard": "changelog",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "4.1",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased] -> Changed",
          "anchor": "## [Unreleased]",
          "change": "Add one bullet to the existing Changed list naming the two Code transitions and the silent-pass shift. Do not add a version heading."
        }
      ],
      "contract": {
        "states": [
          "changelog-entry"
        ],
        "transitions": [
          {
            "input": "a symbol message: (assert false 'x)",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "Code moves UndefinedError -> EvalError, message \"undefined: x\" -> \"assertion failed: x\". Measured on both execution modes and both dialects."
          },
          {
            "input": "a list message: (assert false (list 1 2))",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "Code moves TypeError -> EvalError, message becomes \"assertion failed: (1 2)\". Pre-fix the wording was mode-dependent, so no single old string can be quoted."
          },
          {
            "input": "a symbol or list condition: (assert 'x), (assert (list 1 2))",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "These raised UndefinedError/TypeError and now return nil. This is the entry a reader is most likely to be surprised by: a call that failed loudly now passes silently."
          },
          {
            "input": "the %.200s render, arity, truthiness, the success return",
            "state": "changelog-entry",
            "effect": "no-op",
            "evidence": "Unchanged - must not appear in the entry."
          }
        ],
        "forbidden": [
          "Naming eval.Eval, the builtin body, inventory rows, or any implementation detail: Keep a Changelog entries state observable behavior.",
          "Filing it under Fixed or Added: the reported Code changes for existing inputs, which is Changed.",
          "Adding a version heading or cutting a release: the entry goes under the existing [Unreleased] -> Changed list only."
        ],
        "seeding": [
          "the entry is prose - no fixture, no seeding; the strings come from the c1 goldens table"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "4.1 CHANGELOG.md: add one bullet to the existing [Unreleased] -> Changed list (## [Unreleased] at :8, ### Changed at :10, the bullet list itself from :12). Name the two Code transitions and the silent-pass shift for symbol/list conditions. Match the surrounding entries' voice and wrap width."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "git -C . diff --stat CHANGELOG.md",
      "coder": "zpatcher"
    },
    {
      "id": "c5-floor",
      "taskIds": [
        "3.2"
      ],
      "prev": "c3-inventory-rows",
      "sharedPkg": "plugins/stdlib",
      "parallel": false,
      "seam": "full-floor-verification",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [
        "./..."
      ],
      "sites": [
        {
          "task": "3.2",
          "file": "Makefile",
          "symbol": "test / lint targets",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "READ ONLY. make test = go test -timeout 2m ./... (:14-15); make lint = golangci-lint run (:19-20). No race target and no help target exist, so the race arm and go vet are raw commands."
        }
      ],
      "contract": {
        "states": [
          "floor-status"
        ],
        "transitions": [
          {
            "input": "make test (go test -timeout 2m ./...)",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "Makefile:3,14-15; tasks.md 3.2 'the repository test suite'"
          },
          {
            "input": "go test -race -timeout 10m ./core/... ./plugins/... ./runtime/...",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "tasks.md 3.2 names exactly these three trees; the Makefile has no race target, so this is a stated fallback"
          },
          {
            "input": "go vet ./...",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "tasks.md 3.2; mirrors .github/workflows/ci.yml"
          },
          {
            "input": "make lint (golangci-lint run)",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "Makefile:19-20; tasks.md 3.2 'the linter'"
          }
        ],
        "forbidden": [
          "Reporting a green floor from a narrowed -run: the repository suite command must cover the whole ./... tree.",
          "Dropping -timeout from any run, or running the race suite without a wall-clock limit.",
          "Adding -count=1: it discards the build and test cache for no gain here.",
          "Substituting go build ./... for make test - go build skips _test.go entirely, so a non-compiling test file passes it."
        ],
        "seeding": [
          "all commands — run from /home/zhuk/Projects/own/go-lispico with no environment overrides; GOTESTFLAGS is left at the Makefile default."
        ],
        "budgets": [
          "-timeout 2m for the non-race suite (Makefile:3).",
          "-timeout 10m for the race suite.",
          "Measured on the patched copy without -race: ./plugins/stdlib/ 0.3s, ./runtime/ 10.6s, ./internal/... under 0.1s each."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.2 Run the floor in order and record each command's exit status."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "assert-received-arguments",
      "summary": "Delete both eval.Eval re-entries in the assert builtin and pin the corrected reporting. Red first: a runtime golden table over 7 sources x tree-walker/VM x Clojure/CL, plus a plugins/stdlib direct-apply test that drives every argument shape through an Evaluator whose Eval and Apply both refuse. Then the three-line edit in plugins/stdlib/control.go. Arity, truthiness, both domainErrorf format strings, the ValueStringContext render, registration and the core.Nil{} success return are held fixed.",
      "tasks": [
        "1.1",
        "2.1"
      ],
      "contract": {
        "states": [
          "reported-code",
          "reported-message",
          "returned-value",
          "condition-source",
          "message-source",
          "evaluator-reentry",
          "mode-dialect-agreement"
        ],
        "transitions": [
          {
            "input": "condition a core.Symbol: (assert 'x) with x unbound  /  args[0] = core.Symbol{V: \"x\"}",
            "state": "condition-source = args[0]; returned-value = core.Nil{}, no error",
            "effect": "clear",
            "evidence": "spec requirement 'an argument whose value is a Symbol SHALL NOT be resolved as a binding'; isTruthy returns true for a Symbol (plugins/stdlib/strings.go:895). BEFORE: measured UndefinedError / \"undefined: x\" in all four combinations, from plugins/stdlib/control.go:17. AFTER: measured core.Nil{} in all four."
          },
          {
            "input": "condition a core.List: (assert (list 1 2)) and (assert (hash-map :a 1))  /  args[0] = core.NewList([]core.Value{core.Int{V:1}, core.Int{V:2}})",
            "state": "condition-source = args[0]; returned-value = core.Nil{}, no error",
            "effect": "clear",
            "evidence": "spec requirement 'an argument whose value is a List SHALL NOT be applied as a call'; isTruthy at plugins/stdlib/strings.go:895. BEFORE: measured TypeError, and the two modes disagreed - tree-walker \"expected function, got core.Int\" vs VM \"expected callable, got core.Int\". AFTER: measured core.Nil{} in all four combinations."
          },
          {
            "input": "two args, message a core.Symbol: (assert false 'x) and (assert nil 'y)  /  args[1] = core.Symbol{V: \"x\"}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: x\" (no quote mark; Symbol.String() returns s.V)",
            "effect": "set",
            "evidence": "plugins/stdlib/control.go:31-35 through core.ValueStringContext scalarRender (core/value_walk_context.go:212-223) and core/types.go:149. BEFORE: measured UndefinedError / \"undefined: x\" in all four combinations. AFTER: measured \"assertion failed: x\" in all four."
          },
          {
            "input": "two args, message a core.List: (assert false (list 1 2)) and (assert false '(1 2))",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: (1 2)\" (space-separated elements in parentheses)",
            "effect": "set",
            "evidence": "plugins/stdlib/control.go:31-35 through core.ValueStringContext walkString's List case (core/value_walk_context.go:128-143). BEFORE: measured TypeError with a mode split - \"expected function, got core.Int\" (tree-walker) vs \"expected callable, got core.Int\" (VM). AFTER: measured \"assertion failed: (1 2)\" in all four combinations."
          },
          {
            "input": "two args, message an unquoted unbound symbol: (assert false x) with x unbound",
            "state": "reported-code = UndefinedError, reported-message = \"undefined: x\" - not an assertion failure, not EvalError",
            "effect": "forced",
            "evidence": "core.NewUndefinedError, core/error.go:76-78; refused by symbol resolution at the apply site before the builtin is entered. Measured identical before and after the fix in all four combinations. This row exists so no golden confuses it with the quoted-symbol case, whose pre-fix output was byte-identical to it."
          },
          {
            "input": "any argument shape dispatched through fn.Fn(ctx, ev, args, env) with ev an assertRefusingEvaluator whose Eval and Apply both return errAssertReentry",
            "state": "evaluator-reentry = false: errAssertReentry never surfaces on any path",
            "effect": "clear",
            "evidence": "task 2.1. Today plugins/stdlib/control.go:17 surfaces it for every call and :24 for every two-argument falsy call. core.Evaluator is the two-method interface at core/types.go:21-24, so the stub is exactly Eval and Apply."
          },
          {
            "input": "each of the 7 golden sources run under goldenEvaluatorModes x {clojure.Dialect(), cl.Dialect()}",
            "state": "mode-dialect-agreement = true: the (Code, Message) pair is byte-identical across all four runs",
            "effect": "forced",
            "evidence": "spec scenario 'Reporting is identical across execution modes and dialects'. assert carries no VM native opcode and no CL rename (cl/cl.go:201-226 renames only set!/do, adds defun, and adapts nth/mapcar/sort), and is reachable under CL only through the Lisp-2 GoFunc bridge at runtime/engine.go:492-513. Measured: the measurement ran over 15 candidate sources and all four combinations agreed on every one; the table keeps the 7 listed in redTasks and c2 owns the remaining 8 input classes; pre-fix they disagree on the three list-shaped sources."
          },
          {
            "input": "(assert false bigvec) with bigvec a 300-element core.Vector, under meteringLimits(t, 1_000_000, 4<<10)",
            "state": "reported-code = core.CodeResourceLimit, Terminal, no result published; the refusal comes from core.ValueStringContext's walk budget in both execution modes",
            "effect": "forced",
            "evidence": "runtime/value_walk_publication_test.go:79-101; measured flip point 4816 = 301 * core.MeterValueSlotBytes on both arms post-fix, against 4964 (tree) and 14724 (bytecode) pre-fix. Pre-fix the tree arm refused on the eval meter (\"allocation limit 4096 bytes exceeded\"), not the walk; the fix converts it to the walk refusal the comment describes."
          }
        ],
        "forbidden": [
          "Any call to eval.Eval or eval.Apply inside the assert GoFunc body after the fix.",
          "Renaming the GoFunc's unused parameters to _. The file-local idiom keeps them named even when unused - every GoFunc in plugins/stdlib/types.go:12,24,37,50,63,76,89,102,115,128,141,154 spells (ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) and uses only some of them. Verified: golangci-lint run ./plugins/stdlib/... reports 0 issues on the patched copy with eval and env left named.",
          "Changing arityErrorf/domainErrorf to another constructor, altering the format strings \"assertion failed\", \"assertion failed: %.200s\" (both sites), or reordering the core.String branch ahead of the render.",
          "Replacing core.ValueStringContext with core.ValueString or with a direct %v: the ctx-carried walk budget is what makes the non-string branch budgeted (internal/inventory/work_data.go:993-1012), and the render's cost is owned by the archived core-value-walk-sharing-bound.",
          "Adding a nil check, a type check, an arity ceiling, or a resource charge that is not there today - three or more arguments stay legal and assert holds no budget.",
          "Making assert a special form, or registering it through anything other than env.RegisterValue(\"assert\", core.GoFunc{...}, false).",
          "A golden that asserts an assertion-failure message for (assert false x) with x unbound: that argument fails at the apply site and must keep failing there.",
          "A golden that reaches a symbol or list message by binding a name in the engine env and relying on the lookup - the value must arrive from a quote, a constructor call, or a literal.",
          "A golden whose expected string was captured from a run rather than derived from the contract (runtime/stdlib_family_goldens_test.go:171-173 states the rule for this harness).",
          "A list-message golden whose elements are strings: element rendering inside a container goes through walkString's scalarRender and quotes a core.String (core/types.go:134-136), pinning an unrelated decision. Use Int elements.",
          "Bracket-literal sources ([1 2], [x] fn params) in any CL arm: cl.Dialect() is built WithoutBracketLiterals() (cl/cl.go:204)."
        ],
        "seeding": [
          "engine-level goldens (runtime) — eng := loadStdlibEngine(t, familyDialect(dia), true, mode.opts...) with dia ranging over {\"\", \"cl\"} and mode over goldenEvaluatorModes; then eng.Eval(context.Background(), \"assert-goldens\", g.src). loadStdlibEngine is runtime/bootstrap_goldens_test.go:36-52; goldenEvaluatorModes is runtime/cl_adapters_golden_test.go:94-100; familyDialect is runtime/stdlib_family_goldens_test.go:18-23.",
          "reading Code and Message — var le *core.LispicoError; require.ErrorAs(t, err, &le); then le.Code and le.Message. Error() prefixes the code (core/error.go:22-27) and is not what either site formats - the same helper shape as plugins/stdlib/higher_order_budget_test.go:386-391 and runtime/stdlib_error_goldens_test.go:62-64.",
          "reported-code = UndefinedError — only by naming an identifier the engine env does not bind: source (assert false x), with no eng.Bind(\"x\", ...) anywhere in the test. Never by constructing the error in the test.",
          "message-source = core.Symbol — in source only as 'x (a quote form); at the direct-apply level only as args[1] = core.Symbol{V: \"x\"}.",
          "message-source = core.List — in source only as (list 1 2) or '(1 2); at the direct-apply level only as core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}}).",
          "direct-apply (plugins/stdlib) — env := setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19); fn := collectionGoFunc(t, env, \"assert\") (plugins/stdlib/collections_extra_test.go:328-335); ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30); fn.Fn(ctx, ev, args, env) - the exact shape at plugins/stdlib/higher_order_budget_test.go:376-382.",
          "evaluator-reentry = false — pass ev := assertRefusingEvaluator{} in place of core.NewEvaluator(); core.Evaluator is two methods (core/types.go:21-24), both returning errAssertReentry, and the assertion is errors.Is(err, errAssertReentry) == false on every shape.",
          "mode-dialect-agreement — collect the four (Code, Message) pairs for one source into a map keyed by mode.name+\"/\"+dia and compare each against the first, in the shape of runtime/stdlib_error_goldens_test.go:52-82.",
          "never — no state is reached by writing a field on a core.Value, by constructing a *core.LispicoError in the test and asserting against it, or by binding the message symbol so the lookup succeeds."
        ],
        "budgets": [
          "256 units = the walk ceiling at 4096 bytes (meteringLimits second argument, runtime/value_walk_publication_test.go:43)",
          "301 units = the 300-element fixture's cost, (len+1) * core.MeterValueSlotBytes",
          "45 units of post-fix margin, identical in both arms",
          "200 = the %.200s message precision, unchanged (hoAssertCap, plugins/stdlib/higher_order_budget_test.go:21)",
          "818 = MaxWork on the \"string failure message format\" WorkPhases row, unchanged"
        ]
      },
      "redTasks": [
        "1.1 Author runtime/assert_message_goldens_test.go: TestAssertMessage_GoldensAcrossModesAndDialects over the 7-row assertMessageGoldens table x goldenEvaluatorModes x {\"\", \"cl\"}. The rows that are red today are (assert false 'x), (assert false (list 1 2)), (assert false '(1 2)), and the two success rows (assert 'x) and (assert (list 1 2)).",
        "1.1 Author TestAssertMessage_ModesAndDialectsAgree: for each source, the four (Code, Message) pairs must be byte-identical. Red today on the three list-shaped sources, where the tree-walker says \"expected function, got core.Int\" and the VM says \"expected callable, got core.Int\".",
        "2.1 Author plugins/stdlib/assert_arguments_test.go: TestAssert_DoesNotReEnterTheEvaluator drives every argument shape through fn.Fn with assertRefusingEvaluator and asserts errAssertReentry never surfaces.",
        "c1 assertMessageGoldens rows (the reporting that changes, plus the two controls that disambiguate it): (assert false 'x); (assert false (list 1 2)); (assert false '(1 2)); (assert 'x); (assert (list 1 2)); (assert false x) with x unbound [UndefinedError control, must stay UndefinedError]; (assert false \"boom\") [String control, proves the fast path is untouched]. Seven rows.",
        "c2 owns every remaining input class at the direct-apply level and MUST NOT be given a golden row here: arity, Bool/Nil conditions, the Keyword/Int/Bool/Float/Nil scalar messages, three-or-more arguments. An input class rowed in both chunks seals the same assertion twice."
      ],
      "codeTasks": [
        "2.1 plugins/stdlib/control.go: replace cond, err := eval.Eval(ctx, args[0], env) and its err check (lines 17-20) with the direct use of args[0] in the isTruthy test at :22.",
        "2.1 plugins/stdlib/control.go: replace msg, err := eval.Eval(ctx, args[1], env) and its err check (lines 24-27) with msg := args[1].",
        "2.1 runtime/value_walk_publication_test.go:81-85: replace those five lines with the verbatim comment block given in design.md under \"The c1 comment rewrite\" - three tabs of indentation, em dashes preserved. Do not paraphrase it and change nothing else in the file; the subtest is green before and after."
      ]
    },
    {
      "id": "assert-invariants-hold",
      "summary": "NO-RED-WAIVER: every assertion in this seam pins behavior that is byte-identical before and after the fix, so no test here can fail first - tasks.md 1.2 and 2.2 are worded as verification, not as new behavior. Arity, truthiness, the core.Nil{} success return, the core.String fast path and the non-string scalar messages (Keyword, Int, Bool, Float, Nil) are held fixed and proven so once the fix has landed.",
      "tasks": [
        "1.2",
        "2.2"
      ],
      "contract": {
        "states": [
          "reported-code",
          "reported-message",
          "returned-value",
          "condition-source",
          "message-source",
          "evaluator-reentry",
          "mode-dialect-agreement"
        ],
        "transitions": [
          {
            "input": "no args: (assert)  /  fn.Fn(ctx, ev, nil, env)",
            "state": "reported-code = ArityError, reported-message = \"assert: requires at least 1 argument\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:13-15 via arityErrorf (plugins/stdlib/errors.go:13-15); already classified at plugins/stdlib/typed_errors_test.go:88 and plugins/stdlib/error_inventory_test.go:99. Measured identical in all four combinations before and after the fix."
          },
          {
            "input": "one arg, truthy: (assert true)  /  args[0] = core.Bool{V: true}",
            "state": "returned-value = core.Nil{}, no error",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:22,40; isTruthy at plugins/stdlib/strings.go:888-896; already pinned at plugins/stdlib/higher_order_budget_test.go:393-397. Measured unchanged."
          },
          {
            "input": "one arg, falsy Bool: (assert false)  /  args[0] = core.Bool{V: false}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:37; already pinned at plugins/stdlib/higher_order_budget_test.go:399-402 (task 1.2 control). Measured unchanged."
          },
          {
            "input": "one arg, condition core.Nil: (assert nil)  /  args[0] = core.Nil{}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/strings.go:889-891 makes core.Nil falsy; plugins/stdlib/control.go:37. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a core.String: (assert false \"boom\")",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: boom\" (raw bytes, unquoted; the %.200s fast path, no walk)",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:28-30 short-circuits on core.String before the render; already pinned at plugins/stdlib/higher_order_budget_test.go:404-409 (task 1.2 control). Measured unchanged in all four combinations. Note core.String.String() would quote it (core/types.go:134-136) - this branch never calls it."
          },
          {
            "input": "two args, message a non-string scalar Keyword: (assert false :k)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: :k\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:161-163; a Keyword is self-evaluating, so the deleted second Eval was the identity. Already pinned at plugins/stdlib/higher_order_budget_test.go:421-429. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a non-string scalar Int: (assert false 7)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: 7\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:77-79. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a non-string scalar Bool: (assert false true)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: true\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:44-49. Measured on the patched copy under tree-walker and VM."
          },
          {
            "input": "two args, message core.Nil: (assert false nil)",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: nil\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:37. Measured unchanged in all four combinations."
          },
          {
            "input": "three or more args: (assert false \"a\" \"b\")",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: a\" - only args[1] is read, and there is no upper arity bound",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:23-24 reads args[1] only; :13 bounds arity from below only. Measured unchanged in all four combinations."
          },
          {
            "input": "two args, message a non-string scalar Float: (assert false 1.5)  /  args[1] = core.Float{V: 1.5}",
            "state": "reported-code = EvalError, reported-message = \"assertion failed: 1.5\"",
            "effect": "no-op",
            "evidence": "plugins/stdlib/control.go:31-35 + core/types.go:117-119, Float.String() via strconv.FormatFloat(v, 'f', -1, 64). A Float is self-evaluating, so the deleted second Eval was the identity on it."
          }
        ],
        "forbidden": [
          "Any call to eval.Eval or eval.Apply inside the assert GoFunc body after the fix.",
          "Renaming the GoFunc's unused parameters to _. The file-local idiom keeps them named even when unused - every GoFunc in plugins/stdlib/types.go:12,24,37,50,63,76,89,102,115,128,141,154 spells (ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) and uses only some of them. Verified: golangci-lint run ./plugins/stdlib/... reports 0 issues on the patched copy with eval and env left named.",
          "Changing arityErrorf/domainErrorf to another constructor, altering the format strings \"assertion failed\", \"assertion failed: %.200s\" (both sites), or reordering the core.String branch ahead of the render.",
          "Replacing core.ValueStringContext with core.ValueString or with a direct %v: the ctx-carried walk budget is what makes the non-string branch budgeted (internal/inventory/work_data.go:993-1012), and the render's cost is owned by the archived core-value-walk-sharing-bound.",
          "Adding a nil check, a type check, an arity ceiling, or a resource charge that is not there today - three or more arguments stay legal and assert holds no budget.",
          "Making assert a special form, or registering it through anything other than env.RegisterValue(\"assert\", core.GoFunc{...}, false).",
          "A golden that asserts an assertion-failure message for (assert false x) with x unbound: that argument fails at the apply site and must keep failing there.",
          "A golden that reaches a symbol or list message by binding a name in the engine env and relying on the lookup - the value must arrive from a quote, a constructor call, or a literal.",
          "A golden whose expected string was captured from a run rather than derived from the contract (runtime/stdlib_family_goldens_test.go:171-173 states the rule for this harness).",
          "A list-message golden whose elements are strings: element rendering inside a container goes through walkString's scalarRender and quotes a core.String (core/types.go:134-136), pinning an unrelated decision. Use Int elements.",
          "Bracket-literal sources ([1 2], [x] fn params) in any CL arm: cl.Dialect() is built WithoutBracketLiterals() (cl/cl.go:204)."
        ],
        "seeding": [
          "engine-level goldens (runtime) — eng := loadStdlibEngine(t, familyDialect(dia), true, mode.opts...) with dia ranging over {\"\", \"cl\"} and mode over goldenEvaluatorModes; then eng.Eval(context.Background(), \"assert-goldens\", g.src). loadStdlibEngine is runtime/bootstrap_goldens_test.go:36-52; goldenEvaluatorModes is runtime/cl_adapters_golden_test.go:94-100; familyDialect is runtime/stdlib_family_goldens_test.go:18-23.",
          "reading Code and Message — var le *core.LispicoError; require.ErrorAs(t, err, &le); then le.Code and le.Message. Error() prefixes the code (core/error.go:22-27) and is not what either site formats - the same helper shape as plugins/stdlib/higher_order_budget_test.go:386-391 and runtime/stdlib_error_goldens_test.go:62-64.",
          "reported-code = UndefinedError — only by naming an identifier the engine env does not bind: source (assert false x), with no eng.Bind(\"x\", ...) anywhere in the test. Never by constructing the error in the test.",
          "message-source = core.Symbol — in source only as 'x (a quote form); at the direct-apply level only as args[1] = core.Symbol{V: \"x\"}.",
          "message-source = core.List — in source only as (list 1 2) or '(1 2); at the direct-apply level only as core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}}).",
          "direct-apply (plugins/stdlib) — env := setupEnv(t) (plugins/stdlib/stdlib_test.go:11-19); fn := collectionGoFunc(t, env, \"assert\") (plugins/stdlib/collections_extra_test.go:328-335); ctx := core.WithEvalResourceLimits(context.Background(), 1<<20, 1<<30); fn.Fn(ctx, ev, args, env) - the exact shape at plugins/stdlib/higher_order_budget_test.go:376-382.",
          "evaluator-reentry = false — pass ev := assertRefusingEvaluator{} in place of core.NewEvaluator(); core.Evaluator is two methods (core/types.go:21-24), both returning errAssertReentry, and the assertion is errors.Is(err, errAssertReentry) == false on every shape.",
          "mode-dialect-agreement — collect the four (Code, Message) pairs for one source into a map keyed by mode.name+\"/\"+dia and compare each against the first, in the shape of runtime/stdlib_error_goldens_test.go:52-82.",
          "never — no state is reached by writing a field on a core.Value, by constructing a *core.LispicoError in the test and asserting against it, or by binding the message symbol so the lookup succeeds."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "2.2 The Float scalar message renders through core/types.go Float.String(), strconv.FormatFloat(v, 'f', -1, 64): (assert false 1.5) -> EvalError / \"assertion failed: 1.5\". Read the rendering off that function, not off a run.",
        "1.2 Confirm the existing controls stay green untouched: TestAssert_MessageBoundedAndUnchanged (plugins/stdlib/higher_order_budget_test.go:374) and TestStdlibFamilies_InputsUnchanged (runtime/stdlib_family_goldens_test.go:581). Edit neither file.",
        "2.2 Author plugins/stdlib/assert_invariants_test.go: TestAssert_InvariantsUnchanged - direct-apply rows for exactly the input classes this seam rows in its contract and no others: arity (no args), a truthy core.Bool{V:true} condition, a falsy core.Bool{V:false} condition, a core.Nil condition, the core.Nil{} success return, the core.String message fast path, the Keyword/Int/Bool/Float/Nil scalar messages, and three-or-more arguments. Every row asserts the same Code and Message the pre-fix build produces. A Symbol or List CONDITION is out of scope here - those two shapes change (UndefinedError/TypeError before, core.Nil{} after) and are owned by c1 transitions 1 and 2 and by TestAssert_DoesNotReEnterTheEvaluator; asserting pre-fix output for them here would fail c2 verify.",
        "2.2 plugins/stdlib/control.go: assert by reading, not by editing, that arityErrorf/domainErrorf, both \"assertion failed\" format strings, the isTruthy call and the RegisterValue registration are untouched by the c1 diff."
      ]
    },
    {
      "id": "assert-inventory-rows",
      "summary": "Retire the two ResultBranches rows that describe branches the fix deletes, and the comment above them that states assert evaluates through the caller's evaluator. Nothing in the current suite fails when a row outlives its branch, so this seam carries its own red assertion: the surviving assert row set is exactly six named labels.",
      "tasks": [
        "3.1"
      ],
      "contract": {
        "states": [
          "assert-result-rows",
          "assert-work-rows",
          "assert-error-rows",
          "assert-comment"
        ],
        "transitions": [
          {
            "input": "the row labelled \"condition eval error return\" (Class \"borrowed\")",
            "state": "assert-result-rows: removed; it described the err return at plugins/stdlib/control.go:18-20, which the fix deletes",
            "effect": "clear",
            "evidence": "internal/inventory/result_data.go:2644-2651"
          },
          {
            "input": "the row labelled \"message eval error return\" (Class \"borrowed\")",
            "state": "assert-result-rows: removed; it described the err return at plugins/stdlib/control.go:25-27, which the fix deletes",
            "effect": "clear",
            "evidence": "internal/inventory/result_data.go:2652-2659"
          },
          {
            "input": "the row labelled \"message render error return\" (Class \"borrowed\")",
            "state": "assert-result-rows: kept unchanged - it describes the core.ValueStringContext error return at plugins/stdlib/control.go:32-34, which survives the fix",
            "effect": "no-op",
            "evidence": "internal/inventory/result_data.go:2668-2675; the render call stays at plugins/stdlib/control.go:31. This row is the one a careless reading of \"remove the eval error rows\" deletes by mistake."
          },
          {
            "input": "the five scalar-singleton rows: \"arity error return\", \"string failure return\", \"value failure return\", \"bare failure return\", \"success nil return\"",
            "state": "assert-result-rows: kept unchanged, five rows",
            "effect": "no-op",
            "evidence": "internal/inventory/result_data.go:2636-2643, :2660-2667, :2676-2683, :2684-2691, :2692-2699"
          },
          {
            "input": "the comment at internal/inventory/result_data.go:2634-2635, \"assert evaluates both its condition and its message through the caller's evaluator, so either error reaches the caller unchanged.\"",
            "state": "assert-comment: rewritten to say assert reports against the arguments the apply site handed it, and that the surviving borrowed return is the render's",
            "effect": "forced",
            "evidence": "the sentence is false after the fix; the spec requirement states the arguments arrive evaluated and SHALL NOT be evaluated again"
          },
          {
            "input": "the two WorkPhases rows: \"string failure message format\" (bounded-exception, MaxWork 818) and \"value failure message format\" (budgeted)",
            "state": "assert-work-rows: assert-work-rows unchanged - both dispositions, both Proof strings and MaxWork 818 kept verbatim",
            "effect": "no-op",
            "evidence": "internal/inventory/work_data.go:978-992 and :993-1012. Both format branches survive the fix untouched. The value row's Proof names core.ValueStringContext and the walk budget; that call site is unchanged."
          },
          {
            "input": "the four errorInventory rows for assert (one ArityError, three EvalError)",
            "state": "assert-error-rows: assert-error-rows unchanged",
            "effect": "no-op",
            "evidence": "plugins/stdlib/error_inventory_test.go:99-102. The three EvalError rows are the three domainErrorf sites at plugins/stdlib/control.go:29,35,37, all of which survive; the deleted err passthroughs were never inventoried there. errorInventory is reconciled against the registration surface (:333-373), not against a per-function source count, so deleting source returns cannot move it."
          },
          {
            "input": "tasks.md 3.1's \"callback-owned\" disposition",
            "state": "assert-result-rows: inapplicable to assert, not a missing deliverable",
            "effect": "no-op",
            "evidence": "the callback-owned demand fires only for an eval.Eval/eval.Apply call inside a loop (plugins/stdlib/inventory_source_test.go:883-898, consumed at :1190-1194). registerControl holds no loop before or after the fix, so sf.opaqueCalls is empty either way."
          },
          {
            "input": "the surplus-row direction of every reconciler in the tree",
            "state": "assert-result-rows: leaving both stale rows in place fails nothing, so the red assertion below is the only thing that forces their removal",
            "effect": "forced",
            "evidence": "reconcileResult compares got < want only (plugins/stdlib/inventory_source_test.go:1257-1259); reconcileWork does the same at :1174. Measured: the control.go fix applied alone, with the two stale rows still in place, leaves ./plugins/stdlib/, ./runtime/ and ./internal/... green."
          },
          {
            "input": "the new file plugins/stdlib/assert_inventory_test.go itself",
            "state": "assert-result-rows: no inventory sweep is triggered by adding it",
            "effect": "no-op",
            "evidence": "invSweepUnscopedFiles skips every path ending in _test.go (plugins/stdlib/inventory_source_test.go:385-387)"
          }
        ],
        "forbidden": [
          "Deleting or reclassifying any of the six surviving assert ResultBranches rows - in particular \"message render error return\", which also ends in \"error return\" but describes a branch that survives.",
          "Touching the WorkPhases Proof text, the bounded-exception disposition, or MaxWork 818.",
          "Adding a ChargeExpr to any assert row: every survivor is scalar-singleton or an error return, and a ChargeExpr on an exempt row is itself a failure (runtime/stdlib_result_ownership_test.go:247-251).",
          "Editing internal/inventory/registered.go:83 or internal/inventory/name_family.go:75 - assert's registration entry and its \"higher-order\" family do not change.",
          "Adding an ownershipArm for either deleted row, or for any surviving assert row: none of the eight is armed.",
          "Asserting a total count of inventory.ResultBranches: nothing in the tree does, and such an assertion would break on every unrelated row change."
        ],
        "seeding": [
          "assert-result-rows — read as data - range over inventory.ResultBranches and select row.Fn == \"assert\". No engine, no evaluation, no source parsing.",
          "the assertion shape — collect the surviving BranchLabel values into a []string, sort it, and require.Equal against the literal sorted six. A count-only assertion would pass if a row were renamed instead of removed.",
          "never — do not reach this state by re-deriving the row set from source with go/ast - that duplicates reconcileResult and would pass for the wrong reason."
        ],
        "budgets": [
          "8 -> 6: inventory.ResultBranches rows with Fn == \"assert\".",
          "2: inventory.WorkPhases rows with Fn == \"assert\", unchanged.",
          "4: errorInventory rows with Fn == \"assert\", unchanged (1 ArityError, 3 EvalError).",
          "81: the registered GoFunc surface, unchanged (plugins/stdlib/error_inventory_test.go:349).",
          "0: ownership arms naming any assert row, before and after."
        ]
      },
      "redTasks": [
        "3.1 Author plugins/stdlib/assert_inventory_test.go: TestResultInventory_AssertBranchesMatchTheFixedCode collects every inventory.ResultBranches row with Fn == \"assert\", sorts the BranchLabel values, and requires exactly [\"arity error return\", \"bare failure return\", \"message render error return\", \"string failure return\", \"success nil return\", \"value failure return\"]. Red today: \"condition eval error return\" and \"message eval error return\" are still present."
      ],
      "codeTasks": [
        "3.1 internal/inventory/result_data.go: delete the \"condition eval error return\" row (:2644-2651) and the \"message eval error return\" row (:2652-2659).",
        "3.1 internal/inventory/result_data.go:2634-2635: rewrite the comment so it states that assert reports against its received arguments, and that the one borrowed return left is the message render's."
      ]
    },
    {
      "id": "changelog-entry",
      "summary": "NO-RED-WAIVER: a CHANGELOG.md entry carries no assertion and no test can be red on it. NO-TESTER-WAIVER: there is nothing to verify beyond the floor, which already runs. Records the observable reporting change for embedders who match on the error Code.",
      "tasks": [
        "4.1"
      ],
      "contract": {
        "states": [
          "changelog-entry"
        ],
        "transitions": [
          {
            "input": "a symbol message: (assert false 'x)",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "Code moves UndefinedError -> EvalError, message \"undefined: x\" -> \"assertion failed: x\". Measured on both execution modes and both dialects."
          },
          {
            "input": "a list message: (assert false (list 1 2))",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "Code moves TypeError -> EvalError, message becomes \"assertion failed: (1 2)\". Pre-fix the wording was mode-dependent, so no single old string can be quoted."
          },
          {
            "input": "a symbol or list condition: (assert 'x), (assert (list 1 2))",
            "state": "changelog-entry",
            "effect": "set",
            "evidence": "These raised UndefinedError/TypeError and now return nil. This is the entry a reader is most likely to be surprised by: a call that failed loudly now passes silently."
          },
          {
            "input": "the %.200s render, arity, truthiness, the success return",
            "state": "changelog-entry",
            "effect": "no-op",
            "evidence": "Unchanged - must not appear in the entry."
          }
        ],
        "forbidden": [
          "Naming eval.Eval, the builtin body, inventory rows, or any implementation detail: Keep a Changelog entries state observable behavior.",
          "Filing it under Fixed or Added: the reported Code changes for existing inputs, which is Changed.",
          "Adding a version heading or cutting a release: the entry goes under the existing [Unreleased] -> Changed list only."
        ],
        "seeding": [
          "the entry is prose - no fixture, no seeding; the strings come from the c1 goldens table"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "4.1 CHANGELOG.md: add one bullet to the existing [Unreleased] -> Changed list (## [Unreleased] at :8, ### Changed at :10, the bullet list itself from :12). Name the two Code transitions and the silent-pass shift for symbol/list conditions. Match the surrounding entries' voice and wrap width."
      ]
    },
    {
      "id": "full-floor-verification",
      "summary": "NO-RED-WAIVER: this seam authors no assertion. It runs the floor tasks.md 3.2 names and records each command's exit status. NO-TESTER-WAIVER: there is nothing for a test writer to write - every assertion the change owns is sealed by the two seams above.",
      "tasks": [
        "3.2"
      ],
      "contract": {
        "states": [
          "floor-status"
        ],
        "transitions": [
          {
            "input": "make test (go test -timeout 2m ./...)",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "Makefile:3,14-15; tasks.md 3.2 'the repository test suite'"
          },
          {
            "input": "go test -race -timeout 10m ./core/... ./plugins/... ./runtime/...",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "tasks.md 3.2 names exactly these three trees; the Makefile has no race target, so this is a stated fallback"
          },
          {
            "input": "go vet ./...",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "tasks.md 3.2; mirrors .github/workflows/ci.yml"
          },
          {
            "input": "make lint (golangci-lint run)",
            "state": "floor-status = pass",
            "effect": "forced",
            "evidence": "Makefile:19-20; tasks.md 3.2 'the linter'"
          }
        ],
        "forbidden": [
          "Reporting a green floor from a narrowed -run: the repository suite command must cover the whole ./... tree.",
          "Dropping -timeout from any run, or running the race suite without a wall-clock limit.",
          "Adding -count=1: it discards the build and test cache for no gain here.",
          "Substituting go build ./... for make test - go build skips _test.go entirely, so a non-compiling test file passes it."
        ],
        "seeding": [
          "all commands — run from /home/zhuk/Projects/own/go-lispico with no environment overrides; GOTESTFLAGS is left at the Makefile default."
        ],
        "budgets": [
          "-timeout 2m for the non-race suite (Makefile:3).",
          "-timeout 10m for the race suite.",
          "Measured on the patched copy without -race: ./plugins/stdlib/ 0.3s, ./runtime/ 10.6s, ./internal/... under 0.1s each."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "3.2 Run the floor in order and record each command's exit status."
      ]
    }
  ],
  "requirements": [
    {
      "shall": "`assert` is a Builtin, so its arguments arrive already evaluated. It SHALL use",
      "tests": [
        "TestAssert_DoesNotReEnterTheEvaluator",
        "TestAssertMessage_GoldensAcrossModesAndDialects"
      ]
    },
    {
      "shall": "those values as received and SHALL NOT evaluate them again. In particular, an",
      "tests": [
        "TestAssert_DoesNotReEnterTheEvaluator",
        "TestAssertMessage_GoldensAcrossModesAndDialects"
      ]
    },
    {
      "shall": "argument whose value is a `Symbol` SHALL NOT be resolved as a binding and an",
      "tests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects",
        "TestAssert_DoesNotReEnterTheEvaluator"
      ]
    },
    {
      "shall": "argument whose value is a `List` SHALL NOT be applied as a call.",
      "tests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects",
        "TestAssert_DoesNotReEnterTheEvaluator"
      ]
    },
    {
      "shall": "A failing assertion SHALL report its own message. When a message argument is",
      "tests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects",
        "TestAssert_InvariantsUnchanged"
      ]
    },
    {
      "shall": "supplied, the reported error SHALL be built from that argument's value; when",
      "tests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects"
      ]
    },
    {
      "shall": "none is supplied, it SHALL be the bare assertion failure. Arity errors, the",
      "tests": [
        "TestAssert_InvariantsUnchanged",
        "TestAssert_MessageBoundedAndUnchanged"
      ]
    },
    {
      "shall": "returned on success SHALL be unchanged.",
      "tests": [
        "TestAssert_InvariantsUnchanged",
        "TestAssert_MessageBoundedAndUnchanged",
        "TestStdlibFamilies_InputsUnchanged"
      ]
    },
    {
      "shall": "- **THEN** the error SHALL be the assertion failure naming that symbol, not an undefined-binding error",
      "tests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects"
      ]
    },
    {
      "shall": "- **THEN** the error SHALL be the assertion failure rendering that list, not the result of applying it as a call",
      "tests": [
        "TestAssertMessage_GoldensAcrossModesAndDialects"
      ]
    },
    {
      "shall": "- **THEN** the reported error SHALL be the same in all four combinations",
      "tests": [
        "TestAssertMessage_ModesAndDialectsAgree"
      ]
    }
  ],
  "testHarness": [
    "goldenEvaluatorModes — runtime/cl_adapters_golden_test.go:94 — the evaluator axis: []struct{name string; opts []EngineOption}{{\"tree-walker\", WithTreeWalker()}, {\"vm\", WithBytecode()}}",
    "newGoldenEngine — runtime/cl_adapters_golden_test.go:105 — New(nil, WithDialect(d), opts...) + Use(stdlib.New()), eager publication, engine closed via t.Cleanup",
    "loadStdlibEngine — runtime/bootstrap_goldens_test.go:36 — same, but defaults to WithTreeWalker() when no options are passed; used by the error goldens",
    "familyDialect — runtime/stdlib_family_goldens_test.go:18 — maps a row's dia field (\"\" -> clojure.Dialect(), \"cl\" -> cl.Dialect())",
    "newFamilyEngine — runtime/stdlib_family_goldens_test.go:93 — golden engine under the arm's dialect with the shared cb* callbacks bound",
    "familyReentryDispatch — runtime/stdlib_family_goldens_test.go:106 — runs the form from inside a GoFunc entered by a VM run (third dispatch path)",
    "familyApplyDispatch — runtime/stdlib_family_goldens_test.go:126 — pulls the builtin out of the env (GetFunc under cl, Get otherwise) and calls core.NewEvaluator().Apply directly (fourth path)",
    "familyInputFixtures — runtime/stdlib_family_goldens_test.go:532 — Go-built l/v/m/ls/pa collections bound into the engine",
    "deepCopyValue — runtime/stdlib_family_goldens_test.go:549 — structural copy sharing no backing array, for the input-identity assertions",
    "evalErrorBothPaths — runtime/stdlib_error_goldens_test.go:52 — runs src under both modes, requires a typed *core.LispicoError from each, returns them keyed by mode name",
    "assertPathsAgree — runtime/stdlib_error_goldens_test.go:73 — pins that the tree-walker and VM agree on Code AND on Message (the only existing message-equality helper)",
    "lispErrorCode — runtime/getin_goldens_test.go:43 — ErrorAs to *core.LispicoError and returns its Code",
    "resourceLimitErrorCode — used by the family goldens (runtime/stdlib_family_goldens_test.go:380) — same extraction, named for its resource-limit subtest",
    "bindPrebuiltSubject — runtime/prebuilt_subject_test.go:19 — binds an n-element descending integer list under a name",
    "ownershipArmed / ownershipArm.matches — runtime/stdlib_result_ownership_test.go:20,54 — selects which inventory rows need a proof arm and matches an arm to a row",
    "setupEnv — plugins/stdlib/stdlib_test.go:11 — bare core.NewEnv(nil) with stdlib Plugin.Init, no runtime engine",
    "eval / evalErr — plugins/stdlib/stdlib_test.go:21,38 — read one form and run it through core.NewEvaluator().Eval against that env",
    "collectionGoFunc — plugins/stdlib/collections_extra_test.go:328 — pulls a registered core.GoFunc out of the env by name for a direct fn.Fn call",
    "message (local closure) — plugins/stdlib/higher_order_budget_test.go:386 — ErrorAs to *core.LispicoError and returns lerr.Message (not Error(), which prefixes the Code)",
    "runClassRows / classRow — plugins/stdlib/typed_errors_test.go (assert row at :88) — direct-call rows asserting an error class and a message prefix",
    "reconcileResult / reconcileWork — plugins/stdlib/inventory_source_test.go:1209,1093 — AST reconciliation of the inventory tables against source, driven by TestResultInventory_MatchesSource:1274 and TestWorkInventory_MatchesSource:1268"
  ],
  "floor": "make test && go test -race -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 4
  }
}
```
