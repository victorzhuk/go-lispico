## Context

See `proposal.md` for the 16-byte versus 32-byte scalar reproduction at commit `3bcf9c1`. `json/decode` calls `ChargeEvalAllocBytes` after `ValueDeepBytesContext`; the tree-walker and VM then apply their normal shallow return charge. `TestDecodeChargesDeepResultBytes` invokes the plugin function directly, bypassing that second charge.

The accepted baseline for implementation is `json-int64-decoding` after implementation and archive. This change adds one requirement and does not replace that predecessor's JSON requirement blocks.

## Goals / Non-Goals

**Goals:** preserve full deep accounting while suppressing only the duplicate dispatch charge for that same successful result.

**Non-Goals:** numeric conversion changes, global ledger redesign, eager JSON parsing limits, or changes to encoding.

## Decisions

- At the existing successful return boundary, use `core.ChargeGoFuncResultBytes(ctx, deep)` instead of the generic allocation charge. This API charges the full amount and marks only the active dispatch as accounted.
- Preserve depth and deep-size checks before the result charge; propagate their terminal errors without publishing the decoded value. Do not charge zero: the entire decoded result is newly constructed.
- Keep direct plugin tests, but make public `Engine.Call` and `core.Evaluator.Apply` the regression seams. They reach the centralized fallback that a direct `GoFunc.Fn` call omits. Run the named-call seam with `WithTreeWalker()` and `WithBytecode()` explicitly.
- The static guard `TestChargeGoFuncResultBytesCalledOnlyAsReturn` (`core/chargegofuncresultbytes_usage_test.go`) admits `ChargeGoFuncResultBytes` only as the sole expression of a single-value return. The replacement therefore lands as an unexported wrapper in `plugins/json/plugin.go` — `func chargeDecodedResult(ctx context.Context, deep int64) error { return core.ChargeGoFuncResultBytes(ctx, deep) }` — called from the existing `if err := ...; err != nil` shape, mirroring `plugins/stdlib/charges.go`.

## Verification

Testing mode: existing-service-strict. First add a regression proving that decoding `"42"` through dispatch consumes 32 bytes instead of its 16-byte deep value charge. After the fix, assert exact equality with `core.ValueDeepBytes` for scalars, strings, empty containers, and nested vectors/maps.

Use fresh engines and fresh contexts from `core.WithEvalResourceLimits`; give reductions ample headroom. At the named-call boundary there is no source-reader/compiler allocation to confuse result bytes. For each fixture, allocation ceiling equal to its expected deep charge must succeed; one byte below must fail with terminal `core.CodeResourceLimit` and no value. Keep expected result values independent of the failing invocation.

Exercise two decodes sharing one evaluation ledger and a decode followed by another builtin result, so the callee marker cannot suppress later calls. Preserve predecessor numeric tests and `runtime/value_walk_publication_test.go` terminal-refusal coverage.

## Risks / Trade-offs

- A zero-byte marker would suppress required deep charges → assert exact full-result bytes, not merely lower totals.
- Meter setup can hide source overhead → use direct named calls for thresholds and source evaluation only for parity.
- A marker escaping its dispatch would undercharge later calls → retain cumulative and sequential-dispatch assertions.
- A literal one-line swap inside the `if err := core.ChargeEvalAllocBytes(...)` initializer fails the AST usage guard → wrapper helper, semantics identical.

## Implementation plan

Base SHA `02df94824a3a67ea144ee166b4af5f03bb1823ea` (master). Tier standard, mode existing-service-strict, lenses `spec` + `quality` (no arch/sec/perf triggers: single charge site, no boundary moves, no new input surface, no hot-loop changes). Plan review: **pass**, reviewer zarchitect, 1 round, no blockers; warnings were `plan.json` regeneration (done), task-1.3 seam straddling (covered: the plugin-side exact-budget matrix rides chunk 1's red stage, the runtime-side matrix rides chunk 3; the tasks.md checkbox for 1.3 is owned by chunk 3), and cosmetic line-citation drift in seam evidence (symbols and edit-site anchors exact).

Dispatch order — wave 1 runs the two red chunks in parallel (disjoint packages `plugins/json` and `runtime`); each later chunk is serial behind its predecessor.

### Chunk 1 — `json-charge-red` (task 1.1; parallel, shard `json-plugin-red`)

- Sites: `plugins/json/json_test.go` at `func TestDecodeChargesDeepResultBytes(t *testing.T) {` — add `TestDecodeApplyChargesResultOnce` (direct `fn.Fn` arm asserts ledger == `core.ValueDeepBytes(core.Int{V: 42})` == 16; `core.NewEvaluator().Apply` arm asserts the same 16 — RED pre-fix at 32, recorded as the task-1.1 reproduction) and `TestDecodeApplyExactBudgetMatrix` (rows `"42"`, `"hi"` JSON-quoted, `[]`, `{}`, `[1,[2,3]]`; fresh `core.WithEvalResourceLimits(t.Context(), 1<<20, deep)` succeeds with value parity against an independently built expected; `deep-1` fails `errors.As` `*core.LispicoError`, `Code == core.CodeResourceLimit`, nil value — success rows RED pre-fix). This matrix is the plugin-side half of task 1.3.
- Seeding: `setupEnv(t)`, `decodeGoFunc(t, env)`; budgets only from `core.ValueDeepBytes` on independently constructed values (`core.Int`, `core.String`, `core.NewVector`, `core.NewHashMap()` + `Set(keyword, v)` mirroring `fromJSONValue`); ledger readback `core.EvalMeterFrom(ctx).Snapshot().AllocationBytes`.
- redRun: `go test -timeout 2m ./plugins/json/ -run 'TestDecodeApply'` (must fail on assertions, not compilation).
- verify: `go test -timeout 2m ./plugins/json/ -run 'TestDecode' && go build ./...`.
- Coder: go-test-writer.

### Chunk 2 — `json-charge-fix` (task 2.1; serial after chunk 1, sharedPkg `plugins/json`)

- Site: `plugins/json/plugin.go` at `if err := core.ChargeEvalAllocBytes(ctx, deep); err != nil {` — add `chargeDecodedResult` wrapper (sole-expression return, per the AST guard) and swap the call; preserve order `CheckConstructionDepthContextEnv` → `ValueDeepBytesContext` → charge → `return res, nil`; `deep` always non-zero.
- redRun: `go test -timeout 2m ./plugins/json/ -run '^$'` (compile gate).
- verify: `go test -timeout 2m ./plugins/json/ -run 'TestDecode' && go test -timeout 2m ./core/ -run 'TestChargeGoFuncResultBytes' && go build ./... && golangci-lint run ./plugins/json/...`.
- Coder: go-coder.

### Chunk 3 — `runtime-metering-red` (tasks 1.2, 1.3; parallel, shard `runtime-metering-red`)

- Site: new file `runtime/json_result_metering_test.go`, reusing `newJSONParityEngine` (`runtime/json_integer_parity_test.go`) and the `goldenEvaluatorModes` table (`runtime/cl_adapters_golden_test.go`).
- `TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes`: both modes × {scalar, string, empty vector, empty object, nested}; fresh engine per cell with `WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep)})`; `eng.Call(ctx, "json/decode", ...)` succeeds with `assertJSONParityValue` parity — RED pre-fix.
- `TestJSON_DecodeOneByteBelowBudgetFailsClosed`: `MaxAllocationBytes == deep-1` refuses terminally, nil value, both modes — green pre and post.
- `TestJSON_DecodeLaterDispatchesKeepCharges`: caller-metered ctx via `core.AdoptEvalStateWithMeter` — two decodes at `2*deep` succeed / `2*deep-1` second refused; decode-then-`json/encode` at `deep + core.ValueShallowBytes(expectedEncoded)` succeeds / minus one refused — RED pre-fix.
- redRun: `go test -timeout 2m ./runtime/ -run 'TestJSON_Decode'`.
- verify: `go test -timeout 2m ./runtime/ -run 'TestJSON' && go build ./...`.
- Coder: go-test-writer.

### Chunk 4 — `runtime-metering-verify` (task 2.2; serial after chunk 3, sharedPkg `runtime`; dispatch only after chunk 2 lands)

- Verification only, no edits expected: new `TestJSON_Decode*` green in both modes; `TestJSON_ExactIntegersAcrossDispatchModes` and `TestValueWalk_CallerPublication` unchanged and green.
- verify: `go test -timeout 2m ./runtime/ -run 'TestJSON' && go test -timeout 2m ./runtime/ -run 'TestValueWalk' && go test -timeout 2m ./plugins/json/ -run 'TestDecode' && go build ./...`.
- Coder: go-tester.

### Chunk 5 — `floor-changelog` (tasks 3.1, 3.2; wave-of-one after chunk 4)

- Sites: `Makefile` `test:` target (no edit — run and record), `CHANGELOG.md` `## [Unreleased]` — add a `### Fixed` entry: json/decode charged its decoded result's root twice through public dispatch; now charged exactly once per dispatch in both modes, deep accounting / numeric conversion / terminal refusals unchanged.
- verify = floor: `make test GOTESTFLAGS="-timeout 2m -p 2 -parallel 2" && make lint`.
- Coder: zpatcher.

### Error identity and naming rulings (binding on every stage)

Refusals assert `errors.As(err, &lerr)` with `lerr *core.LispicoError`, `lerr.Code == core.CodeResourceLimit`, terminal, and a nil value — never message text. New test names are exactly: `TestDecodeApplyChargesResultOnce`, `TestDecodeApplyExactBudgetMatrix`, `TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes`, `TestJSON_DecodeOneByteBelowBudgetFailsClosed`, `TestJSON_DecodeLaterDispatchesKeepCharges`. No `-count=1`, no `-race`; every recorded run carries `-timeout 2m` (floor adds `-p 2 -parallel 2`).

### Waivers

Seam `C-verification-changelog` — `NO-RED-WAIVER:` verification-and-changelog-only; every observable contract is frozen by seams A and B. `NO-TESTER-WAIVER:` no separate tester stage; the zpatcher chunk runs the literal commands and records results itself.

### Standing rules for a foreign agent (no kernel)

Work in your assigned isolated worktree only; never touch the primary checkout. Conventional Commits (`fix(json): ...` style), identity from the repo's configured git user, no AI/tool attribution. A contract test once written is read-only — later stages run it, never rewrite it. Batch independent reads; never re-read what you hold; native file tools over shell. Verify with the literal commands above; the floor is the merge gate. Planning artifacts (`openspec/`) are the orchestrator's to write.

## Migration Plan

Section 0 of the task list blocks implementation until `json-int64-decoding` is implemented and archived — confirmed archived at `openspec/changes/archive/2026-09-08-json-int64-decoding/`. Update the existing changelog for corrected allocation-limit behavior. No data migration or new architecture document is needed; rollback restores premature budget rejection.

## Plan appendix

```json
{
  "v": 2,
  "change": "json-result-metering",
  "baseSha": "02df94824a3a67ea144ee166b4af5f03bb1823ea",
  "generatedAt": "2026-09-08T16:51:01.612Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality"
  ],
  "chunks": [
    {
      "id": "json-charge-red",
      "taskIds": [
        "1.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "shard": "json-plugin-red",
      "seam": "A-json-decode-charge",
      "pkgDirs": [
        "plugins/json"
      ],
      "pkgs": [
        "./plugins/json"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "plugins/json/json_test.go",
          "symbol": "TestDecodeChargesDeepResultBytes",
          "anchor": "func TestDecodeChargesDeepResultBytes(t *testing.T) {",
          "change": "NEW TESTS beside it: TestDecodeApplyChargesResultOnce (direct fn.Fn arm asserts ledger == core.ValueDeepBytes(core.Int{V:42}) == 16; core.NewEvaluator().Apply arm asserts the same 16 — RED pre-fix at 32; record observed 32 vs deep 16 as the task-1.1 reproduction) and TestDecodeApplyExactBudgetMatrix (rows \"42\", \"\"hi\"\", \"[]\", \"{}\", \"[1,[2,3]]\"; fresh core.WithEvalResourceLimits(t.Context(), 1<<20, deep) succeeds with value parity against independently built expected; deep-1 fails errors.As *core.LispicoError Code core.CodeResourceLimit, nil value — success rows RED pre-fix)"
        }
      ],
      "contract": {
        "states": [
          "fresh-ledger",
          "decode-charged-once",
          "decode-refused-terminal",
          "marker-expired-post-dispatch",
          "numeric-classification-pinned"
        ],
        "transitions": [
          {
            "input": "fn.Fn direct call with budget >= deep (payload \"42\", deep = 16): the plugin-level dispatch-free arm",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:83-93 (depth check :83, deep measure :86, charge :90, return :93); scalar root = MeterScalarBytes = 16 (core/metering.go:20); existing TestDecodeChargesDeepResultBytes (plugins/json/json_test.go:69-90) already pins ledger == deep on this arm and stays green after the fix (no dispatch, no fallback, marker set but never read)"
          },
          {
            "input": "core.Evaluator.Apply dispatch of json/decode (tree-walker apply), budget >= deep",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "core/eval.go:866-892: GoFunc case charges 1 reduction, brackets calleeCharged (:870-874), and adds ValueShallowBytes(result) only when the callee did not mark itself charged (:886-890); today plugin.go:90 leaves the marker unset so \"42\" bills 16 deep + 16 shallow = 32 (task 1.1 reproduction); post-fix core.ChargeGoFuncResultBytes (core/metering.go:222-226) charges deep and sets the marker, so exactly deep once"
          },
          {
            "input": "decode through fn.Fn or Evaluator.Apply with fresh ctx budget == deep exactly (spec scenario: exact result budget succeeds)",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "openspec/changes/json-result-metering/specs/json-plugin/spec.md scenario 'Exact result budget succeeds'; RED pre-fix on the Apply arm: 16-byte budget fails because 32 bytes are charged"
          },
          {
            "input": "same calls with fresh ctx budget == deep - 1",
            "state": "decode-refused-terminal",
            "effect": "forced",
            "evidence": "charge refusal at the plugin's result charge returns (nil, err) with *core.LispicoError code core.CodeResourceLimit — plugins/json/plugin.go:90-92; assertion precedent plugins/json/json_test.go:79-84; spec scenario 'One byte below the result budget fails closed' (no value published)"
          },
          {
            "input": "second decode on the same ledger ctx after a successful first decode",
            "state": "marker-expired-post-dispatch",
            "effect": "set",
            "evidence": "core/metering.go:246-266: BeginGoFuncDispatch saves-and-clears, EndGoFuncDispatch reads-and-restores around each GoFunc.Fn call (core/vm/vm.go:806-808 and :2151-2153; tree-walker inline bracket core/eval.go:870-874), so the first decode's marker never suppresses the second decode's own full deep charge"
          },
          {
            "input": "payload nesting deeper than the structural-depth limit",
            "state": "decode-refused-terminal",
            "effect": "forced",
            "evidence": "plugins/json/plugin.go:83-85 CheckConstructionDepthContextEnv runs before any charge; existing TestDecodeRejectsOverDeepJSON (plugins/json/json_test.go:93-103) stays green and unchanged"
          },
          {
            "input": "numeric payloads (int64 endpoints, 2^53 pair, first past int64, fractional, 1e400)",
            "state": "numeric-classification-pinned",
            "effect": "no-op",
            "evidence": "fromJSONValue/exactInt classification (plugins/json/plugin.go:114-158) precedes and is untouched by the charge-site change; accepted spec openspec/changes/archive/2026-09-08-json-int64-decoding/specs/json-plugin/spec.md remains in force"
          }
        ],
        "forbidden": [
          "core.ChargeGoFuncResultBytes(ctx, 0) for a decoded result: the zero-byte borrowed marker on a wholly fresh value undercharges the root",
          "any second root charge for the same result: after the plugin charges deep, the apply-site fallback (core/eval.go:886-890, core/vm/vm.go pendingValue path) must see charged == true and add nothing",
          "calleeCharged marker surviving past its own dispatch (would suppress a later dispatch's fallback charge); per-dispatch bracketing at core/metering.go:246-266 is the guard",
          "new tests deriving expected bytes by measuring the invocation under test (core.ValueDeepBytes(got) on the result just returned); the grandfathered TestDecodeChargesDeepResultBytes (plugins/json/json_test.go:71-75) is left unchanged but is not a template for new assertions",
          "a direct core.ChargeGoFuncResultBytes call in if-init, assignment, or two-result return shape in non-test code: core/chargegofuncresultbytes_usage_test.go:59-70 allows only the sole expression of a single-result return",
          "modifying runtime/value_walk_publication_test.go or predecessor numeric tests (TestJSON_ExactIntegersAcrossDispatchModes, TestDecodeNumericClassification, TestDecodeLargeIntegers)"
        ],
        "seeding": [
          "fresh env per test: setupEnv(t) (plugins/json/json_test.go:16-27); GoFunc handle: decodeGoFunc(t, env) (plugins/json/json_test.go:59-66)",
          "fresh ctx per threshold cell: core.WithEvalResourceLimits(t.Context(), 1<<20, budget) — precedent plugins/json/json_test.go:71",
          "exact budget derivation: deep := core.ValueDeepBytes(expected) on an independently constructed expected value — core.Int{V: 42}; core.String{V: \"hi\"}; core.NewVector([]core.Value{...}) for arrays (mirrors fromJSONValue, plugins/json/plugin.go:146-155); maps via core.NewHashMap() + Set(core.Keyword{V: \"k\"}, v) (mirrors plugin.go:134-145; Set not Assoc, matching production storage form)",
          "dispatch arm: ev := core.NewEvaluator(); ev.Apply(ctx, fn, []core.Value{core.String{V: payload}}, env) — exported Evaluator.Apply, core/eval.go:702, interface core/types.go:23",
          "ledger readback: core.EvalMeterFrom(ctx).Snapshot().AllocationBytes (precedent plugins/json/json_test.go:73)"
        ],
        "budgets": [
          "scalar root (Nil/Bool/Int/Float) deep bytes: 16 = MeterScalarBytes (core/metering.go:20)",
          "current duplicate through dispatch for \"42\": 32 = 16 deep + 16 ValueShallowBytes fallback (core/eval.go:886-890 with core/metering.go:654-661)",
          "string deep bytes: 16 + len (MeterStringHeaderBytes, core/metering.go:21); empty vector deep: 24 (MeterCollectionHeaderBytes, :22); empty object deep: 32 (MeterHashMapHeaderBytes, :23)",
          "reductions headroom: fixture ctx carries 1<<20 reductions against 1 reduction per GoFunc dispatch (core/eval.go:867-869)"
        ]
      },
      "redTasks": [
        "1.1: reproduce the 32-byte dispatch charge for \"42\" through core.Evaluator.Apply against its 16-byte deep result (RED on the apply arm)"
      ],
      "codeTasks": [
        "TestDecodeApplyChargesResultOnce (plugins/json/json_test.go): subtest direct-Fn asserts ledger == core.ValueDeepBytes(core.Int{V: 42}) == 16 == core.MeterScalarBytes; subtest evaluator-apply asserts the same 16 through core.NewEvaluator().Apply — RED pre-fix: the apply arm charges 32; record the observed 32 vs deep 16 as task 1.1's reproduction evidence in the red-stage notes",
        "TestDecodeApplyExactBudgetMatrix (plugins/json/json_test.go): rows scalar \"42\", string \"hi\" (JSON-quoted), empty vector \"[]\", empty object \"{}\", nested \"[1,[2,3]]\"; per row, fresh ctx with budget == deep succeeds and returns a value equal to the independently constructed expected; fresh ctx with budget == deep - 1 fails with errors.As *core.LispicoError, Code == core.CodeResourceLimit, and no value — success rows are RED pre-fix (task 1.3's exact-deep-budget + one-byte-below via fresh contexts)",
        "red stage compiles against current production symbols only (verified: core.NewEvaluator, Evaluator.Apply, WithEvalResourceLimits, EvalMeterFrom, ValueDeepBytes, MeterScalarBytes, NewVector, NewHashMap/Set all exist) — no new production symbols required"
      ],
      "redTests": [
        "TestDecodeApplyChargesResultOnce",
        "TestDecodeApplyExactBudgetMatrix"
      ],
      "redRun": "go test -timeout 2m ./plugins/json/ -run 'TestDecodeApply'",
      "verify": "go test -timeout 2m ./plugins/json/ -run 'TestDecode' && go build ./...",
      "coder": "go-test-writer"
    },
    {
      "id": "runtime-metering-red",
      "taskIds": [
        "1.2",
        "1.3"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "shard": "runtime-metering-red",
      "seam": "B-runtime-dispatch-regressions",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "runtime/json_result_metering_test.go",
          "symbol": "(new file)",
          "anchor": "",
          "change": "NEW FILE: TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes — goldenEvaluatorModes (runtime/cl_adapters_golden_test.go) x {scalar \"42\", string, empty vector \"[]\", empty object \"{}\", nested}; fresh newJSONParityEngine(t, append(mode.opts, WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep)}))...); eng.Call(ctx, \"json/decode\", core.String{V: payload}) succeeds with assertJSONParityValue parity against independently built expected; deep from core.ValueDeepBytes(expected) only — RED pre-fix"
        },
        {
          "task": "1.3",
          "file": "runtime/json_result_metering_test.go",
          "symbol": "(new file)",
          "anchor": "",
          "change": "SAME NEW FILE: TestJSON_DecodeOneByteBelowBudgetFailsClosed (MaxAllocationBytes == deep-1: errors.As *core.LispicoError, Code core.CodeResourceLimit, nil value, both modes — green pre and post) and TestJSON_DecodeLaterDispatchesKeepCharges (caller-metered ctx via core.AdoptEvalStateWithMeter: two decodes at 2*deep succeed / 2*deep-1 second refused; decode-then-json/encode at deep+core.ValueShallowBytes(expectedEncoded) succeeds / minus one refused — RED pre-fix)"
        }
      ],
      "contract": {
        "states": [
          "fresh-engine",
          "exact-budget-succeeded",
          "one-below-refused",
          "cumulative-second-call-refused",
          "later-builtin-charged",
          "predecessor-parity-green"
        ],
        "transitions": [
          {
            "input": "Engine.Call(ctx, \"json/decode\", core.String{V: payload}) with WithResourceLimits(ResourceLimits{MaxAllocationBytes: deep}), tree-walker and VM modes",
            "state": "exact-budget-succeeded",
            "effect": "set",
            "evidence": "budget wiring: runtime/eval.go:627 (engine limits -> core.WithEvalResourceLimits); fields runtime/engine.go:166-177; VM lean path arms the same limits inside applyOnVM (runtime/eval.go:990-996, bytecodeEvaluator maxAllocBytes runtime/eval.go:138); tree-walker arms at runtime/eval.go:972-974; RED pre-fix: the dispatch fallback re-charges the root (core/eval.go:886-890; core/vm/vm.go:2151-2168), so an exact-deep budget is refused"
          },
          {
            "input": "same Call with MaxAllocationBytes == deep - 1, both modes",
            "state": "one-below-refused",
            "effect": "forced",
            "evidence": "spec scenario 'One byte below the result budget fails closed'; assertion precedent runtime/getin_goldens_test.go:185-188 (code core.CodeResourceLimit); green pre-fix and post-fix — pins the terminal refusal"
          },
          {
            "input": "two sequential Calls on one caller-metered ctx with meter budget == 2*deep, then with 2*deep - 1",
            "state": "cumulative-second-call-refused",
            "effect": "forced",
            "evidence": "caller-meter fixture: core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{...}) (precedent runtime/apply_pool_test.go:216-218); HasEvalMeter routes Call to the general boundary (runtime/eval.go:847-849, 930-933); each decode charges its own full deep because the marker is per-dispatch (core/metering.go:246-266) — at 2*deep both succeed, at 2*deep-1 the second fails terminally with no value; RED pre-fix (first call already needs 2*deep)"
          },
          {
            "input": "decode \"42\" then Call \"json/encode\" of the decoded value on the same metered ctx, budget == deep + core.ValueShallowBytes(expectedEncoded)",
            "state": "later-builtin-charged",
            "effect": "set",
            "evidence": "json/encode returns core.String without self-charging (plugins/json/plugin.go:54-68), so the dispatch fallback (core/eval.go:886-890 / core/vm/vm.go pendingValue) must charge its shallow bytes — proves the decode marker did not leak past its dispatch (core/metering.go:246-266 bracketing); at budget - 1 the encode call is refused"
          },
          {
            "input": "predecessor numeric payloads through both modes and both entry points (Call and Eval)",
            "state": "predecessar-parity-green",
            "effect": "no-op",
            "evidence": "runtime/json_integer_parity_test.go:73-145 TestJSON_ExactIntegersAcrossDispatchModes stays green and byte-for-byte unchanged (task 2.2)"
          }
        ],
        "forbidden": [
          "reusing one engine or one ctx across threshold cells: fresh newJSONParityEngine per case (runtime/json_integer_parity_test.go:24-33) and fresh limits/meter per cell",
          "exact-threshold assertions through Engine.Eval: the Eval arm charges reader bytes first (runtime/eval.go:638-646), so exact thresholds go through Call only",
          "deriving expected bytes from the invocation under test; budgets come only from core.ValueDeepBytes/core.ValueShallowBytes over independently constructed values",
          "asserting error message text or comparing error strings across modes; assert errors.As *core.LispicoError with Code core.CodeResourceLimit and nil value only",
          "suppressing a later dispatch's charge via any marker carry-over (the cumulative and encode cases exist to catch exactly this)"
        ],
        "seeding": [
          "fresh engine per cell: newJSONParityEngine(t, mode.opts...) — explicit clojure dialect, json plugin, no stdlib (runtime/json_integer_parity_test.go:24-33); modes from goldenEvaluatorModes (runtime/cl_adapters_golden_test.go:94)",
          "per-Call threshold budget: WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep)}) at New (runtime/engine.go:166-177, 264-267)",
          "cumulative ledger: meteredCtx, _, _ := core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: 1 << 20, MaxAllocationBytes: budget}); eng.Call(meteredCtx, ...) (precedent runtime/apply_pool_test.go:216-218)",
          "expected values built independently: core.Int{V: 42}; core.String{V: \"hi\"}; core.NewVector([]core.Value{...}) (arrays, plugins/json/plugin.go:146-155); core.NewHashMap() + Set(core.Keyword{...}, v) (objects, plugin.go:134-145); encode expectation: core.String{V: \"42\"} with shallow bytes via core.ValueShallowBytes",
          "value parity assertions via assertJSONParityValue(t, want, got, label) (runtime/json_integer_parity_test.go:56-64)"
        ],
        "budgets": [
          "scalar \"42\": deep 16; string \"hi\": deep 18 (16 header + 2); empty vector \"[]\": deep 24; empty object \"{}\": deep 32; nested: core.ValueDeepBytes(expected)",
          "duplicate root today per dispatch: + core.ValueShallowBytes(root) — scalar totals 32 pre-fix",
          "later-builtin encode of 42: core.ValueShallowBytes(core.String{V: \"42\"}) = 16 + 2 = 18",
          "reductions: engine default MaxReductions 10_000_000 (runtime/engine.go:186); metered-ctx fixture 1<<20; cost per GoFunc dispatch 1 reduction (core/eval.go:867-869, core/vm/vm.go:805-806)"
        ]
      },
      "redTasks": [
        "write runtime/json_result_metering_test.go with TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes — modes x {scalar, string, empty-container, nested}: fresh engine, MaxAllocationBytes == deep, Call succeeds, value parity against the independently built expected; RED pre-fix (tasks 1.2 + 1.3)",
        "write TestJSON_DecodeOneByteBelowBudgetFailsClosed — modes x payloads, MaxAllocationBytes == deep - 1: errors.As *core.LispicoError, Code core.CodeResourceLimit, nil value; green pre- and post-fix (pins terminal refusal, task 1.3)",
        "write TestJSON_DecodeLaterDispatchesKeepCharges — cumulative two-decode ledger at 2*deep (succeeds) and 2*deep-1 (second refused terminally), plus decode-then-encode at deep + ValueShallowBytes(expected) (succeeds) and minus one byte (refused); RED pre-fix (task 1.3 cumulative + later-builtin)",
        "all three compile against current production symbols only — confirmed: newJSONParityEngine, goldenEvaluatorModes, ResourceLimits, WithResourceLimits, WithTreeWalker, WithBytecode, core.AdoptEvalStateWithMeter, core.EvalMeterSnapshot, core.ValueDeepBytes, core.ValueShallowBytes, core.LispicoError, core.CodeResourceLimit all exist at HEAD"
      ],
      "codeTasks": [
        "go-test-writer: write runtime/json_result_metering_test.go reusing newJSONParityEngine (runtime/json_integer_parity_test.go:20) and the goldenEvaluatorModes mode table; fresh engine and fresh limits per cell; testify require for setup, assert for parity; compile against current production symbols only"
      ],
      "redTests": [
        "TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes",
        "TestJSON_DecodeOneByteBelowBudgetFailsClosed",
        "TestJSON_DecodeLaterDispatchesKeepCharges"
      ],
      "redRun": "go test -timeout 2m ./runtime/ -run 'TestJSON_Deccode|TestJSON_Decode'",
      "verify": "go test -timeout 2m ./runtime/ -run 'TestJSON' && go build ./...",
      "coder": "go-test-writer"
    },
    {
      "id": "json-charge-fix",
      "taskIds": [
        "2.1"
      ],
      "prev": "json-charge-red",
      "sharedPkg": "plugins/json",
      "parallel": false,
      "shard": "",
      "seam": "A-json-decode-charge",
      "pkgDirs": [
        "plugins/json"
      ],
      "pkgs": [
        "./plugins/json"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "plugins/json/plugin.go",
          "symbol": "decode",
          "anchor": "if err := core.ChargeEvalAllocBytes(ctx, deep); err != nil {",
          "change": "add unexported helper in plugin.go: func chargeDecodedResult(ctx context.Context, deep int64) error { return core.ChargeGoFuncResultBytes(ctx, deep) } (sole-expression return — static guard core/chargegofuncresultbytes_usage_test.go admits nothing else); swap the charge call to chargeDecodedResult(ctx, deep) in the existing if-init shape; preserve order CheckConstructionDepthContextEnv (:83) -> ValueDeepBytesContext (:86) -> charge -> return res, nil; deep is always non-zero (whole result fresh)"
        }
      ],
      "contract": {
        "states": [
          "fresh-ledger",
          "decode-charged-once",
          "decode-refused-terminal",
          "marker-expired-post-dispatch",
          "numeric-classification-pinned"
        ],
        "transitions": [
          {
            "input": "fn.Fn direct call with budget >= deep (payload \"42\", deep = 16): the plugin-level dispatch-free arm",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:83-93 (depth check :83, deep measure :86, charge :90, return :93); scalar root = MeterScalarBytes = 16 (core/metering.go:20); existing TestDecodeChargesDeepResultBytes (plugins/json/json_test.go:69-90) already pins ledger == deep on this arm and stays green after the fix (no dispatch, no fallback, marker set but never read)"
          },
          {
            "input": "core.Evaluator.Apply dispatch of json/decode (tree-walker apply), budget >= deep",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "core/eval.go:866-892: GoFunc case charges 1 reduction, brackets calleeCharged (:870-874), and adds ValueShallowBytes(result) only when the callee did not mark itself charged (:886-890); today plugin.go:90 leaves the marker unset so \"42\" bills 16 deep + 16 shallow = 32 (task 1.1 reproduction); post-fix core.ChargeGoFuncResultBytes (core/metering.go:222-226) charges deep and sets the marker, so exactly deep once"
          },
          {
            "input": "decode through fn.Fn or Evaluator.Apply with fresh ctx budget == deep exactly (spec scenario: exact result budget succeeds)",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "openspec/changes/json-result-metering/specs/json-plugin/spec.md scenario 'Exact result budget succeeds'; RED pre-fix on the Apply arm: 16-byte budget fails because 32 bytes are charged"
          },
          {
            "input": "same calls with fresh ctx budget == deep - 1",
            "state": "decode-refused-terminal",
            "effect": "forced",
            "evidence": "charge refusal at the plugin's result charge returns (nil, err) with *core.LispicoError code core.CodeResourceLimit — plugins/json/plugin.go:90-92; assertion precedent plugins/json/json_test.go:79-84; spec scenario 'One byte below the result budget fails closed' (no value published)"
          },
          {
            "input": "second decode on the same ledger ctx after a successful first decode",
            "state": "marker-expired-post-dispatch",
            "effect": "set",
            "evidence": "core/metering.go:246-266: BeginGoFuncDispatch saves-and-clears, EndGoFuncDispatch reads-and-restores around each GoFunc.Fn call (core/vm/vm.go:806-808 and :2151-2153; tree-walker inline bracket core/eval.go:870-874), so the first decode's marker never suppresses the second decode's own full deep charge"
          },
          {
            "input": "payload nesting deeper than the structural-depth limit",
            "state": "decode-refused-terminal",
            "effect": "forced",
            "evidence": "plugins/json/plugin.go:83-85 CheckConstructionDepthContextEnv runs before any charge; existing TestDecodeRejectsOverDeepJSON (plugins/json/json_test.go:93-103) stays green and unchanged"
          },
          {
            "input": "numeric payloads (int64 endpoints, 2^53 pair, first past int64, fractional, 1e400)",
            "state": "numeric-classification-pinned",
            "effect": "no-op",
            "evidence": "fromJSONValue/exactInt classification (plugins/json/plugin.go:114-158) precedes and is untouched by the charge-site change; accepted spec openspec/changes/archive/2026-09-08-json-int64-decoding/specs/json-plugin/spec.md remains in force"
          }
        ],
        "forbidden": [
          "core.ChargeGoFuncResultBytes(ctx, 0) for a decoded result: the zero-byte borrowed marker on a wholly fresh value undercharges the root",
          "any second root charge for the same result: after the plugin charges deep, the apply-site fallback (core/eval.go:886-890, core/vm/vm.go pendingValue path) must see charged == true and add nothing",
          "calleeCharged marker surviving past its own dispatch (would suppress a later dispatch's fallback charge); per-dispatch bracketing at core/metering.go:246-266 is the guard",
          "new tests deriving expected bytes by measuring the invocation under test (core.ValueDeepBytes(got) on the result just returned); the grandfathered TestDecodeChargesDeepResultBytes (plugins/json/json_test.go:71-75) is left unchanged but is not a template for new assertions",
          "a direct core.ChargeGoFuncResultBytes call in if-init, assignment, or two-result return shape in non-test code: core/chargegofuncresultbytes_usage_test.go:59-70 allows only the sole expression of a single-result return",
          "modifying runtime/value_walk_publication_test.go or predecessor numeric tests (TestJSON_ExactIntegersAcrossDispatchModes, TestDecodeNumericClassification, TestDecodeLargeIntegers)"
        ],
        "seeding": [
          "fresh env per test: setupEnv(t) (plugins/json/json_test.go:16-27); GoFunc handle: decodeGoFunc(t, env) (plugins/json/json_test.go:59-66)",
          "fresh ctx per threshold cell: core.WithEvalResourceLimits(t.Context(), 1<<20, budget) — precedent plugins/json/json_test.go:71",
          "exact budget derivation: deep := core.ValueDeepBytes(expected) on an independently constructed expected value — core.Int{V: 42}; core.String{V: \"hi\"}; core.NewVector([]core.Value{...}) for arrays (mirrors fromJSONValue, plugins/json/plugin.go:146-155); maps via core.NewHashMap() + Set(core.Keyword{V: \"k\"}, v) (mirrors plugin.go:134-145; Set not Assoc, matching production storage form)",
          "dispatch arm: ev := core.NewEvaluator(); ev.Apply(ctx, fn, []core.Value{core.String{V: payload}}, env) — exported Evaluator.Apply, core/eval.go:702, interface core/types.go:23",
          "ledger readback: core.EvalMeterFrom(ctx).Snapshot().AllocationBytes (precedent plugins/json/json_test.go:73)"
        ],
        "budgets": [
          "scalar root (Nil/Bool/Int/Float) deep bytes: 16 = MeterScalarBytes (core/metering.go:20)",
          "current duplicate through dispatch for \"42\": 32 = 16 deep + 16 ValueShallowBytes fallback (core/eval.go:886-890 with core/metering.go:654-661)",
          "string deep bytes: 16 + len (MeterStringHeaderBytes, core/metering.go:21); empty vector deep: 24 (MeterCollectionHeaderBytes, :22); empty object deep: 32 (MeterHashMapHeaderBytes, :23)",
          "reductions headroom: fixture ctx carries 1<<20 reductions against 1 reduction per GoFunc dispatch (core/eval.go:867-869)"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "plugins/json/plugin.go:90-92 — replace core.ChargeEvalAllocBytes(ctx, deep) with the self-accounting charge: add unexported wrapper (in plugin.go or a new plugins/json/charges.go) func chargeDecodedResult(ctx context.Context, deep int64) error { return core.ChargeGoFuncResultBytes(ctx, deep) } (pattern plugins/stdlib/charges.go:24-26) and call it in the existing if-init shape so over-budget still returns (nil, err); a literal one-line swap inside the if-init, or a two-result return res, core.ChargeGoFuncResultBytes(...), is rejected by the AST guard",
        "preserve check order: CheckConstructionDepthContextEnv (plugin.go:83) then ValueDeepBytesContext (:86) then the result charge; non-zero n always (the whole decoded result is fresh)",
        "verify seam: go test -timeout 2m ./plugins/json/ -run 'TestDecode' — new tests green, TestDecodeChargesDeepResultBytes and TestDecodeRejectsOverDeepJSON unchanged and green; AST guard green: go test -timeout 2m ./core/ -run 'TestChargeGoFuncResultBytes'"
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m ./plugins/json/ -run '^$'",
      "verify": "go test -timeout 2m ./plugins/json/ -run 'TestDecode' && go test -timeout 2m ./core/ -run 'TestChargeGoFuncResultBytes' && go build ./... && golangci-lint run ./plugins/json/...",
      "coder": "go-coder"
    },
    {
      "id": "runtime-metering-verify",
      "taskIds": [
        "2.2"
      ],
      "prev": "runtime-metering-red",
      "sharedPkg": "runtime",
      "parallel": false,
      "shard": "",
      "seam": "B-runtime-dispatch-regressions",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.2",
          "file": "runtime/json_result_metering_test.go",
          "symbol": "(verification only)",
          "anchor": "",
          "change": "dispatch AFTER json-charge-fix lands: run the scoped matrix — new TestJSON_Decode* green in both modes, TestJSON_ExactIntegersAcrossDispatchModes and TestValueWalk_CallerPublication unchanged and green; no edits expected"
        }
      ],
      "contract": {
        "states": [
          "fresh-engine",
          "exact-budget-succeeded",
          "one-below-refused",
          "cumulative-second-call-refused",
          "later-builtin-charged",
          "predecessor-parity-green"
        ],
        "transitions": [
          {
            "input": "Engine.Call(ctx, \"json/decode\", core.String{V: payload}) with WithResourceLimits(ResourceLimits{MaxAllocationBytes: deep}), tree-walker and VM modes",
            "state": "exact-budget-succeeded",
            "effect": "set",
            "evidence": "budget wiring: runtime/eval.go:627 (engine limits -> core.WithEvalResourceLimits); fields runtime/engine.go:166-177; VM lean path arms the same limits inside applyOnVM (runtime/eval.go:990-996, bytecodeEvaluator maxAllocBytes runtime/eval.go:138); tree-walker arms at runtime/eval.go:972-974; RED pre-fix: the dispatch fallback re-charges the root (core/eval.go:886-890; core/vm/vm.go:2151-2168), so an exact-deep budget is refused"
          },
          {
            "input": "same Call with MaxAllocationBytes == deep - 1, both modes",
            "state": "one-below-refused",
            "effect": "forced",
            "evidence": "spec scenario 'One byte below the result budget fails closed'; assertion precedent runtime/getin_goldens_test.go:185-188 (code core.CodeResourceLimit); green pre-fix and post-fix — pins the terminal refusal"
          },
          {
            "input": "two sequential Calls on one caller-metered ctx with meter budget == 2*deep, then with 2*deep - 1",
            "state": "cumulative-second-call-refused",
            "effect": "forced",
            "evidence": "caller-meter fixture: core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{...}) (precedent runtime/apply_pool_test.go:216-218); HasEvalMeter routes Call to the general boundary (runtime/eval.go:847-849, 930-933); each decode charges its own full deep because the marker is per-dispatch (core/metering.go:246-266) — at 2*deep both succeed, at 2*deep-1 the second fails terminally with no value; RED pre-fix (first call already needs 2*deep)"
          },
          {
            "input": "decode \"42\" then Call \"json/encode\" of the decoded value on the same metered ctx, budget == deep + core.ValueShallowBytes(expectedEncoded)",
            "state": "later-builtin-charged",
            "effect": "set",
            "evidence": "json/encode returns core.String without self-charging (plugins/json/plugin.go:54-68), so the dispatch fallback (core/eval.go:886-890 / core/vm/vm.go pendingValue) must charge its shallow bytes — proves the decode marker did not leak past its dispatch (core/metering.go:246-266 bracketing); at budget - 1 the encode call is refused"
          },
          {
            "input": "predecessor numeric payloads through both modes and both entry points (Call and Eval)",
            "state": "predecessar-parity-green",
            "effect": "no-op",
            "evidence": "runtime/json_integer_parity_test.go:73-145 TestJSON_ExactIntegersAcrossDispatchModes stays green and byte-for-byte unchanged (task 2.2)"
          }
        ],
        "forbidden": [
          "reusing one engine or one ctx across threshold cells: fresh newJSONParityEngine per case (runtime/json_integer_parity_test.go:24-33) and fresh limits/meter per cell",
          "exact-threshold assertions through Engine.Eval: the Eval arm charges reader bytes first (runtime/eval.go:638-646), so exact thresholds go through Call only",
          "deriving expected bytes from the invocation under test; budgets come only from core.ValueDeepBytes/core.ValueShallowBytes over independently constructed values",
          "asserting error message text or comparing error strings across modes; assert errors.As *core.LispicoError with Code core.CodeResourceLimit and nil value only",
          "suppressing a later dispatch's charge via any marker carry-over (the cumulative and encode cases exist to catch exactly this)"
        ],
        "seeding": [
          "fresh engine per cell: newJSONParityEngine(t, mode.opts...) — explicit clojure dialect, json plugin, no stdlib (runtime/json_integer_parity_test.go:24-33); modes from goldenEvaluatorModes (runtime/cl_adapters_golden_test.go:94)",
          "per-Call threshold budget: WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep)}) at New (runtime/engine.go:166-177, 264-267)",
          "cumulative ledger: meteredCtx, _, _ := core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: 1 << 20, MaxAllocationBytes: budget}); eng.Call(meteredCtx, ...) (precedent runtime/apply_pool_test.go:216-218)",
          "expected values built independently: core.Int{V: 42}; core.String{V: \"hi\"}; core.NewVector([]core.Value{...}) (arrays, plugins/json/plugin.go:146-155); core.NewHashMap() + Set(core.Keyword{...}, v) (objects, plugin.go:134-145); encode expectation: core.String{V: \"42\"} with shallow bytes via core.ValueShallowBytes",
          "value parity assertions via assertJSONParityValue(t, want, got, label) (runtime/json_integer_parity_test.go:56-64)"
        ],
        "budgets": [
          "scalar \"42\": deep 16; string \"hi\": deep 18 (16 header + 2); empty vector \"[]\": deep 24; empty object \"{}\": deep 32; nested: core.ValueDeepBytes(expected)",
          "duplicate root today per dispatch: + core.ValueShallowBytes(root) — scalar totals 32 pre-fix",
          "later-builtin encode of 42: core.ValueShallowBytes(core.String{V: \"42\"}) = 16 + 2 = 18",
          "reductions: engine default MaxReductions 10_000_000 (runtime/engine.go:186); metered-ctx fixture 1<<20; cost per GoFunc dispatch 1 reduction (core/eval.go:867-869, core/vm/vm.go:805-806)"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "task 2.2 verification after the plugin fix lands: go test -timeout 2m ./runtime/ -run 'TestJSON' (new metering tests + TestJSON_ExactIntegersAcrossDispatchModes green) and go test -timeout 2m ./runtime/ -run 'TestValueWalk' (terminal publication refusals unchanged)"
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m ./runtime/ -run '^$'",
      "verify": "go test -timeout 2m ./runtime/ -run 'TestJSON' && go test -timeout 2m ./runtime/ -run 'TestValueWalk' && go test -timeout 2m ./plugins/json/ -run 'TestDecode' && go build ./...",
      "coder": "go-tester"
    },
    {
      "id": "floor-changelog",
      "taskIds": [
        "3.1",
        "3.2"
      ],
      "prev": "runtime-metering-verify",
      "sharedPkg": null,
      "parallel": true,
      "shard": "",
      "seam": "C-verification-changelog",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.1",
          "file": "Makefile",
          "symbol": "test",
          "anchor": "test:",
          "change": "no edit: run and record make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" and make lint with results and regression names"
        },
        {
          "task": "3.2",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "add ### Fixed subsection entry under [Unreleased]: json/decode charged its decoded result root twice through public dispatch (plugin deep charge + dispatcher shallow root charge), so a payload fitting the allocation budget exactly could be rejected; full deep result now charged exactly once per dispatch in both modes, deep accounting / numeric conversion / terminal refusals unchanged"
        }
      ],
      "contract": {
        "states": [
          "unverified",
          "suite-green",
          "lint-clean",
          "changelog-entered"
        ],
        "transitions": [
          {
            "input": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\"",
            "state": "suite-green",
            "effect": "set",
            "evidence": "Makefile test -> go test $(GOTESTFLAGS) ./... (GOTESTFLAGS ?= -timeout 2m); task 3.1 requires recording commands, results, and regression names"
          },
          {
            "input": "make lint",
            "state": "lint-clean",
            "effect": "set",
            "evidence": "Makefile lint -> golangci-lint run; task 3.1"
          },
          {
            "input": "append a concise ### Fixed entry under [Unreleased] in CHANGELOG.md",
            "state": "changelog-entered",
            "effect": "set",
            "evidence": "task 3.2; Keep a Changelog 1.1.0 six-type grouping (Fixed = bug fixes); [Unreleased] sits on top (CHANGELOG.md:9)"
          }
        ],
        "forbidden": [
          "-count=1, -race, or cache-busting flags in recorded commands (task 3.1 names the exact flags above)",
          "editing released version sections or compare links",
          "commit-message-style or internal-mechanics narration in the entry; one user-visible line",
          "running the full floor while sibling edits are mid-flight; scoped package commands only until the final floor"
        ],
        "seeding": [
          "[Unreleased] section already exists with Added and Changed entries (CHANGELOG.md:9-33); add ### Fixed below them"
        ],
        "budgets": [
          "suite wall budget: -timeout 2m -p 2 -parallel 2 exactly as task 3.1 specifies",
          "changelog entry: one concise line (plus a short clarifying clause), no section sprawl"
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "task 3.1: run and record make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" and make lint with results and the regression test names (TestDecodeApplyChargesResultOnce, TestDecodeApplyExactBudgetMatrix, TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes, TestJSON_DecodeOneByteBelowBudgetFailsClosed, TestJSON_DecodeLaterDispatchesKeepCharges, plus stay-green TestDecodeChargesDeepResultBytes, TestJSON_ExactIntegersAcrossDispatchModes, TestValueWalk_CallerPublication, TestChargeGoFuncResultBytesCalledOnlyAsReturn)",
        "task 3.2: add under [Unreleased] a ### Fixed entry to the effect: json/decode charged its decoded result's root allocation twice through public dispatch (the plugin's deep charge followed by the dispatcher's shallow root charge), so a payload fitting the allocation budget exactly could be rejected; the full deep result size is now charged exactly once per dispatch in both execution modes, with deep accounting, numeric conversion, and terminal refusals unchanged"
      ],
      "redTests": [],
      "redRun": "go build ./...",
      "verify": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" && make lint",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "A-json-decode-charge",
      "tasks": [
        "0.1",
        "1.1",
        "2.1"
      ],
      "summary": "Depth: Standard — single-site behavioral fix at an existing charge boundary following the established wrapper pattern (plugins/stdlib/charges.go:24-26); the bulk of this seam is the sealed red contract, not design space. Task 0.1 confirmed: predecessor json-int64-decoding is implemented and archived (openspec/changes/archive/2026-09-08-json-int64-decoding/ with accepted specs/json-plugin/spec.md) and its numeric policy is the undisturbed baseline; testing mode fixed to existing-service-strict. The defect: plugins/json/plugin.go:90 charges the decoded value deeply via core.ChargeEvalAllocBytes but never marks the result accounted, so every apply site adds a second shallow root charge (core/eval.go:886-890, core/vm/vm.go fallbacks) — decoding \"42\" bills 32 bytes against a 16-byte deep result and a payload fitting its budget exactly is rejected. The fix: charge the full deep result once through core.ChargeGoFuncResultBytes (marker suppresses the dispatch root charge for that dispatch only). New Go tests, exact names: TestDecodeApplyChargesResultOnce and TestDecodeApplyExactBudgetMatrix in plugins/json/json_test.go. Exact replacement site: plugins/json/plugin.go:90-92, via a wrapper helper because the module-wide AST guard core/chargegofuncresultbytes_usage_test.go:36-83 admits ChargeGoFuncResultBytes only as the sole expression of a single-value return in non-test files. Error identity asserted everywhere: errors.As(err, &lerr) with lerr *core.LispicoError and lerr.Code == core.CodeResourceLimit, terminal, alongside a nil value.",
      "contract": {
        "states": [
          "fresh-ledger",
          "decode-charged-once",
          "decode-refused-terminal",
          "marker-expired-post-dispatch",
          "numeric-classification-pinned"
        ],
        "transitions": [
          {
            "input": "fn.Fn direct call with budget >= deep (payload \"42\", deep = 16): the plugin-level dispatch-free arm",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:83-93 (depth check :83, deep measure :86, charge :90, return :93); scalar root = MeterScalarBytes = 16 (core/metering.go:20); existing TestDecodeChargesDeepResultBytes (plugins/json/json_test.go:69-90) already pins ledger == deep on this arm and stays green after the fix (no dispatch, no fallback, marker set but never read)"
          },
          {
            "input": "core.Evaluator.Apply dispatch of json/decode (tree-walker apply), budget >= deep",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "core/eval.go:866-892: GoFunc case charges 1 reduction, brackets calleeCharged (:870-874), and adds ValueShallowBytes(result) only when the callee did not mark itself charged (:886-890); today plugin.go:90 leaves the marker unset so \"42\" bills 16 deep + 16 shallow = 32 (task 1.1 reproduction); post-fix core.ChargeGoFuncResultBytes (core/metering.go:222-226) charges deep and sets the marker, so exactly deep once"
          },
          {
            "input": "decode through fn.Fn or Evaluator.Apply with fresh ctx budget == deep exactly (spec scenario: exact result budget succeeds)",
            "state": "decode-charged-once",
            "effect": "set",
            "evidence": "openspec/changes/json-result-metering/specs/json-plugin/spec.md scenario 'Exact result budget succeeds'; RED pre-fix on the Apply arm: 16-byte budget fails because 32 bytes are charged"
          },
          {
            "input": "same calls with fresh ctx budget == deep - 1",
            "state": "decode-refused-terminal",
            "effect": "forced",
            "evidence": "charge refusal at the plugin's result charge returns (nil, err) with *core.LispicoError code core.CodeResourceLimit — plugins/json/plugin.go:90-92; assertion precedent plugins/json/json_test.go:79-84; spec scenario 'One byte below the result budget fails closed' (no value published)"
          },
          {
            "input": "second decode on the same ledger ctx after a successful first decode",
            "state": "marker-expired-post-dispatch",
            "effect": "set",
            "evidence": "core/metering.go:246-266: BeginGoFuncDispatch saves-and-clears, EndGoFuncDispatch reads-and-restores around each GoFunc.Fn call (core/vm/vm.go:806-808 and :2151-2153; tree-walker inline bracket core/eval.go:870-874), so the first decode's marker never suppresses the second decode's own full deep charge"
          },
          {
            "input": "payload nesting deeper than the structural-depth limit",
            "state": "decode-refused-terminal",
            "effect": "forced",
            "evidence": "plugins/json/plugin.go:83-85 CheckConstructionDepthContextEnv runs before any charge; existing TestDecodeRejectsOverDeepJSON (plugins/json/json_test.go:93-103) stays green and unchanged"
          },
          {
            "input": "numeric payloads (int64 endpoints, 2^53 pair, first past int64, fractional, 1e400)",
            "state": "numeric-classification-pinned",
            "effect": "no-op",
            "evidence": "fromJSONValue/exactInt classification (plugins/json/plugin.go:114-158) precedes and is untouched by the charge-site change; accepted spec openspec/changes/archive/2026-09-08-json-int64-decoding/specs/json-plugin/spec.md remains in force"
          }
        ],
        "forbidden": [
          "core.ChargeGoFuncResultBytes(ctx, 0) for a decoded result: the zero-byte borrowed marker on a wholly fresh value undercharges the root",
          "any second root charge for the same result: after the plugin charges deep, the apply-site fallback (core/eval.go:886-890, core/vm/vm.go pendingValue path) must see charged == true and add nothing",
          "calleeCharged marker surviving past its own dispatch (would suppress a later dispatch's fallback charge); per-dispatch bracketing at core/metering.go:246-266 is the guard",
          "new tests deriving expected bytes by measuring the invocation under test (core.ValueDeepBytes(got) on the result just returned); the grandfathered TestDecodeChargesDeepResultBytes (plugins/json/json_test.go:71-75) is left unchanged but is not a template for new assertions",
          "a direct core.ChargeGoFuncResultBytes call in if-init, assignment, or two-result return shape in non-test code: core/chargegofuncresultbytes_usage_test.go:59-70 allows only the sole expression of a single-result return",
          "modifying runtime/value_walk_publication_test.go or predecessor numeric tests (TestJSON_ExactIntegersAcrossDispatchModes, TestDecodeNumericClassification, TestDecodeLargeIntegers)"
        ],
        "seeding": [
          "fresh env per test: setupEnv(t) (plugins/json/json_test.go:16-27); GoFunc handle: decodeGoFunc(t, env) (plugins/json/json_test.go:59-66)",
          "fresh ctx per threshold cell: core.WithEvalResourceLimits(t.Context(), 1<<20, budget) — precedent plugins/json/json_test.go:71",
          "exact budget derivation: deep := core.ValueDeepBytes(expected) on an independently constructed expected value — core.Int{V: 42}; core.String{V: \"hi\"}; core.NewVector([]core.Value{...}) for arrays (mirrors fromJSONValue, plugins/json/plugin.go:146-155); maps via core.NewHashMap() + Set(core.Keyword{V: \"k\"}, v) (mirrors plugin.go:134-145; Set not Assoc, matching production storage form)",
          "dispatch arm: ev := core.NewEvaluator(); ev.Apply(ctx, fn, []core.Value{core.String{V: payload}}, env) — exported Evaluator.Apply, core/eval.go:702, interface core/types.go:23",
          "ledger readback: core.EvalMeterFrom(ctx).Snapshot().AllocationBytes (precedent plugins/json/json_test.go:73)"
        ],
        "budgets": [
          "scalar root (Nil/Bool/Int/Float) deep bytes: 16 = MeterScalarBytes (core/metering.go:20)",
          "current duplicate through dispatch for \"42\": 32 = 16 deep + 16 ValueShallowBytes fallback (core/eval.go:886-890 with core/metering.go:654-661)",
          "string deep bytes: 16 + len (MeterStringHeaderBytes, core/metering.go:21); empty vector deep: 24 (MeterCollectionHeaderBytes, :22); empty object deep: 32 (MeterHashMapHeaderBytes, :23)",
          "reductions headroom: fixture ctx carries 1<<20 reductions against 1 reduction per GoFunc dispatch (core/eval.go:867-869)"
        ]
      },
      "redTasks": [
        "TestDecodeApplyChargesResultOnce (plugins/json/json_test.go): subtest direct-Fn asserts ledger == core.ValueDeepBytes(core.Int{V: 42}) == 16 == core.MeterScalarBytes; subtest evaluator-apply asserts the same 16 through core.NewEvaluator().Apply — RED pre-fix: the apply arm charges 32; record the observed 32 vs deep 16 as task 1.1's reproduction evidence in the red-stage notes",
        "TestDecodeApplyExactBudgetMatrix (plugins/json/json_test.go): rows scalar \"42\", string \"hi\" (JSON-quoted), empty vector \"[]\", empty object \"{}\", nested \"[1,[2,3]]\"; per row, fresh ctx with budget == deep succeeds and returns a value equal to the independently constructed expected; fresh ctx with budget == deep - 1 fails with errors.As *core.LispicoError, Code == core.CodeResourceLimit, and no value — success rows are RED pre-fix (task 1.3's exact-deep-budget + one-byte-below via fresh contexts)",
        "red stage compiles against current production symbols only (verified: core.NewEvaluator, Evaluator.Apply, WithEvalResourceLimits, EvalMeterFrom, ValueDeepBytes, MeterScalarBytes, NewVector, NewHashMap/Set all exist) — no new production symbols required"
      ],
      "codeTasks": [
        "plugins/json/plugin.go:90-92 — replace core.ChargeEvalAllocBytes(ctx, deep) with the self-accounting charge: add unexported wrapper (in plugin.go or a new plugins/json/charges.go) func chargeDecodedResult(ctx context.Context, deep int64) error { return core.ChargeGoFuncResultBytes(ctx, deep) } (pattern plugins/stdlib/charges.go:24-26) and call it in the existing if-init shape so over-budget still returns (nil, err); a literal one-line swap inside the if-init, or a two-result return res, core.ChargeGoFuncResultBytes(...), is rejected by the AST guard",
        "preserve check order: CheckConstructionDepthContextEnv (plugin.go:83) then ValueDeepBytesContext (:86) then the result charge; non-zero n always (the whole decoded result is fresh)",
        "verify seam: go test -timeout 2m ./plugins/json/ -run 'TestDecode' — new tests green, TestDecodeChargesDeepResultBytes and TestDecodeRejectsOverDeepJSON unchanged and green; AST guard green: go test -timeout 2m ./core/ -run 'TestChargeGoFuncResultBytes'"
      ]
    },
    {
      "id": "B-runtime-dispatch-regressions",
      "tasks": [
        "1.2",
        "1.3",
        "2.2"
      ],
      "summary": "Public-boundary regressions in package runtime, new file runtime/json_result_metering_test.go, reusing newJSONParityEngine (runtime/json_integer_parity_test.go:24-33) and goldenEvaluatorModes (runtime/cl_adapters_golden_test.go:94: WithTreeWalker() and WithBytecode()). Exact Go test names: TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes (both modes x scalar/string/empty-container/nested: fresh engine with WithResourceLimits MaxAllocationBytes == deep, Call(\"json/decode\", core.String payload) succeeds with value parity), TestJSON_DecodeOneByteBelowBudgetFailsClosed (MaxAllocationBytes == deep-1: *core.LispicoError, code core.CodeResourceLimit, nil value, both modes), TestJSON_DecodeLaterDispatchesKeepCharges (cumulative decodes and a later json/encode on one caller-metered ledger). Expected budgets always derived from independently constructed expected values via core.ValueDeepBytes / core.ValueShallowBytes, never from the invocation under test. Error identity: errors.As *core.LispicoError with lerr.Code == core.CodeResourceLimit; error strings are never compared across modes (precedent comment runtime/json_integer_parity_test.go:124-126).",
      "contract": {
        "states": [
          "fresh-engine",
          "exact-budget-succeeded",
          "one-below-refused",
          "cumulative-second-call-refused",
          "later-builtin-charged",
          "predecessor-parity-green"
        ],
        "transitions": [
          {
            "input": "Engine.Call(ctx, \"json/decode\", core.String{V: payload}) with WithResourceLimits(ResourceLimits{MaxAllocationBytes: deep}), tree-walker and VM modes",
            "state": "exact-budget-succeeded",
            "effect": "set",
            "evidence": "budget wiring: runtime/eval.go:627 (engine limits -> core.WithEvalResourceLimits); fields runtime/engine.go:166-177; VM lean path arms the same limits inside applyOnVM (runtime/eval.go:990-996, bytecodeEvaluator maxAllocBytes runtime/eval.go:138); tree-walker arms at runtime/eval.go:972-974; RED pre-fix: the dispatch fallback re-charges the root (core/eval.go:886-890; core/vm/vm.go:2151-2168), so an exact-deep budget is refused"
          },
          {
            "input": "same Call with MaxAllocationBytes == deep - 1, both modes",
            "state": "one-below-refused",
            "effect": "forced",
            "evidence": "spec scenario 'One byte below the result budget fails closed'; assertion precedent runtime/getin_goldens_test.go:185-188 (code core.CodeResourceLimit); green pre-fix and post-fix — pins the terminal refusal"
          },
          {
            "input": "two sequential Calls on one caller-metered ctx with meter budget == 2*deep, then with 2*deep - 1",
            "state": "cumulative-second-call-refused",
            "effect": "forced",
            "evidence": "caller-meter fixture: core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{...}) (precedent runtime/apply_pool_test.go:216-218); HasEvalMeter routes Call to the general boundary (runtime/eval.go:847-849, 930-933); each decode charges its own full deep because the marker is per-dispatch (core/metering.go:246-266) — at 2*deep both succeed, at 2*deep-1 the second fails terminally with no value; RED pre-fix (first call already needs 2*deep)"
          },
          {
            "input": "decode \"42\" then Call \"json/encode\" of the decoded value on the same metered ctx, budget == deep + core.ValueShallowBytes(expectedEncoded)",
            "state": "later-builtin-charged",
            "effect": "set",
            "evidence": "json/encode returns core.String without self-charging (plugins/json/plugin.go:54-68), so the dispatch fallback (core/eval.go:886-890 / core/vm/vm.go pendingValue) must charge its shallow bytes — proves the decode marker did not leak past its dispatch (core/metering.go:246-266 bracketing); at budget - 1 the encode call is refused"
          },
          {
            "input": "predecessor numeric payloads through both modes and both entry points (Call and Eval)",
            "state": "predecessor-parity-green",
            "effect": "no-op",
            "evidence": "runtime/json_integer_parity_test.go:73-145 TestJSON_ExactIntegersAcrossDispatchModes stays green and byte-for-byte unchanged (task 2.2)"
          }
        ],
        "forbidden": [
          "reusing one engine or one ctx across threshold cells: fresh newJSONParityEngine per case (runtime/json_integer_parity_test.go:24-33) and fresh limits/meter per cell",
          "exact-threshold assertions through Engine.Eval: the Eval arm charges reader bytes first (runtime/eval.go:638-646), so exact thresholds go through Call only",
          "deriving expected bytes from the invocation under test; budgets come only from core.ValueDeepBytes/core.ValueShallowBytes over independently constructed values",
          "asserting error message text or comparing error strings across modes; assert errors.As *core.LispicoError with Code core.CodeResourceLimit and nil value only",
          "suppressing a later dispatch's charge via any marker carry-over (the cumulative and encode cases exist to catch exactly this)"
        ],
        "seeding": [
          "fresh engine per cell: newJSONParityEngine(t, mode.opts...) — explicit clojure dialect, json plugin, no stdlib (runtime/json_integer_parity_test.go:24-33); modes from goldenEvaluatorModes (runtime/cl_adapters_golden_test.go:94)",
          "per-Call threshold budget: WithResourceLimits(ResourceLimits{MaxAllocationBytes: int(deep)}) at New (runtime/engine.go:166-177, 264-267)",
          "cumulative ledger: meteredCtx, _, _ := core.AdoptEvalStateWithMeter(context.Background(), time.Time{}, 0, core.EvalMeterSnapshot{MaxReductions: 1 << 20, MaxAllocationBytes: budget}); eng.Call(meteredCtx, ...) (precedent runtime/apply_pool_test.go:216-218)",
          "expected values built independently: core.Int{V: 42}; core.String{V: \"hi\"}; core.NewVector([]core.Value{...}) (arrays, plugins/json/plugin.go:146-155); core.NewHashMap() + Set(core.Keyword{...}, v) (objects, plugin.go:134-145); encode expectation: core.String{V: \"42\"} with shallow bytes via core.ValueShallowBytes",
          "value parity assertions via assertJSONParityValue(t, want, got, label) (runtime/json_integer_parity_test.go:56-64)"
        ],
        "budgets": [
          "scalar \"42\": deep 16; string \"hi\": deep 18 (16 header + 2); empty vector \"[]\": deep 24; empty object \"{}\": deep 32; nested: core.ValueDeepBytes(expected)",
          "duplicate root today per dispatch: + core.ValueShallowBytes(root) — scalar totals 32 pre-fix",
          "later-builtin encode of 42: core.ValueShallowBytes(core.String{V: \"42\"}) = 16 + 2 = 18",
          "reductions: engine default MaxReductions 10_000_000 (runtime/engine.go:186); metered-ctx fixture 1<<20; cost per GoFunc dispatch 1 reduction (core/eval.go:867-869, core/vm/vm.go:805-806)"
        ]
      },
      "redTasks": [
        "write runtime/json_result_metering_test.go with TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes — modes x {scalar, string, empty-container, nested}: fresh engine, MaxAllocationBytes == deep, Call succeeds, value parity against the independently built expected; RED pre-fix (tasks 1.2 + 1.3)",
        "write TestJSON_DecodeOneByteBelowBudgetFailsClosed — modes x payloads, MaxAllocationBytes == deep - 1: errors.As *core.LispicoError, Code core.CodeResourceLimit, nil value; green pre- and post-fix (pins terminal refusal, task 1.3)",
        "write TestJSON_DecodeLaterDispatchesKeepCharges — cumulative two-decode ledger at 2*deep (succeeds) and 2*deep-1 (second refused terminally), plus decode-then-encode at deep + ValueShallowBytes(expected) (succeeds) and minus one byte (refused); RED pre-fix (task 1.3 cumulative + later-builtin)",
        "all three compile against current production symbols only — confirmed: newJSONParityEngine, goldenEvaluatorModes, ResourceLimits, WithResourceLimits, WithTreeWalker, WithBytecode, core.AdoptEvalStateWithMeter, core.EvalMeterSnapshot, core.ValueDeepBytes, core.ValueShallowBytes, core.LispicoError, core.CodeResourceLimit all exist at HEAD"
      ],
      "codeTasks": [
        "task 2.2 verification after the plugin fix lands: go test -timeout 2m ./runtime/ -run 'TestJSON' (new metering tests + TestJSON_ExactIntegersAcrossDispatchModes green) and go test -timeout 2m ./runtime/ -run 'TestValueWalk' (terminal publication refusals unchanged)"
      ]
    },
    {
      "id": "C-verification-changelog",
      "tasks": [
        "3.1",
        "3.2"
      ],
      "summary": "NO-RED-WAIVER: verification-and-changelog-only seam — tasks 3.1/3.2 run the recorded floor and add the changelog entry; every observable contract is frozen by seams A and B, so there is no red stage to write. NO-TESTER-WAIVER: no separate tester stage; the zpatcher chunk runs the literal commands and records results itself. Full-floor verification and the changelog entry; no new tests. Commands recorded with results and the regression names from seams A and B. CHANGELOG.md is a hand-maintained Keep a Changelog 1.1.0 file (no changesets, no generator writing it) with an existing [Unreleased] section holding Added and Changed (CHANGELOG.md:9-33) — the entry is added directly as a new ### Fixed subsection.",
      "contract": {
        "states": [
          "unverified",
          "suite-green",
          "lint-clean",
          "changelog-entered"
        ],
        "transitions": [
          {
            "input": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\"",
            "state": "suite-green",
            "effect": "set",
            "evidence": "Makefile test -> go test $(GOTESTFLAGS) ./... (GOTESTFLAGS ?= -timeout 2m); task 3.1 requires recording commands, results, and regression names"
          },
          {
            "input": "make lint",
            "state": "lint-clean",
            "effect": "set",
            "evidence": "Makefile lint -> golangci-lint run; task 3.1"
          },
          {
            "input": "append a concise ### Fixed entry under [Unreleased] in CHANGELOG.md",
            "state": "changelog-entered",
            "effect": "set",
            "evidence": "task 3.2; Keep a Changelog 1.1.0 six-type grouping (Fixed = bug fixes); [Unreleased] sits on top (CHANGELOG.md:9)"
          }
        ],
        "forbidden": [
          "-count=1, -race, or cache-busting flags in recorded commands (task 3.1 names the exact flags above)",
          "editing released version sections or compare links",
          "commit-message-style or internal-mechanics narration in the entry; one user-visible line",
          "running the full floor while sibling edits are mid-flight; scoped package commands only until the final floor"
        ],
        "seeding": [
          "[Unreleased] section already exists with Added and Changed entries (CHANGELOG.md:9-33); add ### Fixed below them"
        ],
        "budgets": [
          "suite wall budget: -timeout 2m -p 2 -parallel 2 exactly as task 3.1 specifies",
          "changelog entry: one concise line (plus a short clarifying clause), no section sprawl"
        ]
      },
      "codeTasks": [
        "task 3.1: run and record make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" and make lint with results and the regression test names (TestDecodeApplyChargesResultOnce, TestDecodeApplyExactBudgetMatrix, TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes, TestJSON_DecodeOneByteBelowBudgetFailsClosed, TestJSON_DecodeLaterDispatchesKeepCharges, plus stay-green TestDecodeChargesDeepResultBytes, TestJSON_ExactIntegersAcrossDispatchModes, TestValueWalk_CallerPublication, TestChargeGoFuncResultBytesCalledOnlyAsReturn)",
        "task 3.2: add under [Unreleased] a ### Fixed entry to the effect: json/decode charged its decoded result's root allocation twice through public dispatch (the plugin's deep charge followed by the dispatcher's shallow root charge), so a payload fitting the allocation budget exactly could be rejected; the full deep result size is now charged exactly once per dispatch in both execution modes, with deep accounting, numeric conversion, and terminal refusals unchanged"
      ]
    }
  ],
  "requirements": [
    {
      "shall": "Every successful `json/decode` invocation SHALL charge the full constructed result's deep allocation size exactly once, including the root value.",
      "tests": [
        "TestDecodeApplyChargesResultOnce",
        "TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes",
        "TestDecodeChargesDeepResultBytes"
      ]
    },
    {
      "shall": "Public dispatch SHALL NOT add another shallow charge for the same result.",
      "tests": [
        "TestDecodeApplyChargesResultOnce",
        "TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes"
      ]
    },
    {
      "shall": "The existing deep-allocation, numeric-conversion, and terminal-refusal requirements SHALL remain in force.",
      "tests": [
        "TestDecodeRejectsOverDeepJSON",
        "TestDecodeNumericClassification",
        "TestDecodeLargeIntegers",
        "TestJSON_ExactIntegersAcrossDispatchModes",
        "TestValueWalk_CallerPublication"
      ]
    },
    {
      "shall": "When otherwise sufficient evaluation resources leave exactly the decoded result's deep allocation charge available, decoding SHALL succeed.",
      "tests": [
        "TestDecodeApplyExactBudgetMatrix",
        "TestJSON_DecodeChargesExactDeepBytesAcrossDispatchModes"
      ]
    },
    {
      "shall": "One byte less SHALL fail with a terminal `ResourceLimitError` and publish no value.",
      "tests": [
        "TestDecodeApplyExactBudgetMatrix",
        "TestJSON_DecodeOneByteBelowBudgetFailsClosed"
      ]
    },
    {
      "shall": "Result accounting for one dispatch SHALL NOT suppress the charges of later dispatches.",
      "tests": [
        "TestJSON_DecodeLaterDispatchesKeepCharges"
      ]
    }
  ],
  "testHarness": [
    "setupEnv - plugins/json/json_test.go:20 - builds a fresh core.Env with stdlib and json plugins initialized",
    "eval - plugins/json/json_test.go:34 - parses and evaluates a single form with core.NewEvaluator, asserting no error",
    "evalErr - plugins/json/json_test.go:46 - parses and evaluates a single form with core.NewEvaluator, returning the evaluation error",
    "decodeGoFunc - plugins/json/json_test.go:60 - extracts and asserts the registered json/decode core.GoFunc from the environment",
    "newJSONParityEngine - runtime/json_integer_parity_test.go:20 - builds a fresh engine under given options with Clojure dialect and json plugin loaded, registering cleanup",
    "jsonParitySrc - runtime/json_integer_parity_test.go:51 - formats a (json/decode \"%s\") source string for evaluation",
    "assertJSONParityValue - runtime/json_integer_parity_test.go:57 - non-aborting parity assertion comparing type and equality between want and got core.Value",
    "sharedChain - runtime/value_walk_publication_test.go:29 - builds a canonical self-referencing 5-level Cons hierarchy with 352 logical walk visits exceeding the 256-unit walk cap",
    "newEngine - runtime/value_walk_publication_test.go:42 - builds a fresh metering engine with stdlib and the shared symbol bound to sharedChain()",
    "requireTerminal - runtime/value_walk_publication_test.go:49 - asserts an error is a Terminal ResourceLimit and no result value was published alongside it",
    "TestChargeGoFuncResultBytesCalledOnlyAsReturn - core/chargegofuncresultbytes_usage_test.go:32 - static AST test verifying every direct ChargeGoFuncResultBytes call module-wide is the sole result of a return statement",
    "isChargeGoFuncResultBytesCall - core/chargegofuncresultbytes_usage_test.go:90 - identifies whether an AST call expression targets ChargeGoFuncResultBytes",
    "moduleRoot - core/chargegofuncresultbytes_usage_test.go:102 - discovers repository root by traversing upward until finding go.mod"
  ],
  "floor": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 1
  }
}
```
