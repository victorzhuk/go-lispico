## Context

See `proposal.md` for the failures at commit `3bcf9c1`. VM calls and native operations route nonterminal errors through `VM.throw`, but ordinary lookup and assignment opcodes return directly. `OpThrow` returns a missing-handler `TypeError` when no handler exists. The tree-walker preserves a thrown value and wraps a `LispicoError` with `Code: "ThrowError"`.

`VM.throw` already restores the handler's frame, operand, freeze and structural-depth state. `core.IsTerminalEvalError` owns cancellation, deadline and resource-limit classification. The archived `eval-noncatchable-terminal-errors` design requires classification by Go error identity, never by a thrown value's text.

## Goals / Non-Goals

**Goals:** Consistent routing for errors raised while executing valid bytecode; preserved host error identity; clean state after handled and unhandled failures.

**Non-Goals:** Catch compile-time syntax errors, execute malformed bytecode, alter terminal classes, or replace message-string catch values for ordinary Go errors with structured objects.

## Decisions

1. Give runtime opcode failures one routing policy. A terminal error bypasses handlers; an ordinary evaluation error becomes the same message string as under `evalTry`; an explicit throw retains its original Lisp value. Apply the policy to global/function lookup, lexical assignment, runtime map construction and existing call/native paths. Structural bytecode validation remains before execution.
2. Preserve the original error when no handler exists. For an uncaught explicit throw, return a typed error carrying `ThrowError` and the value rendering used by `evalThrow`, without substituting a missing-handler error. Reuse or share the existing throw representation only as needed to preserve identity across evaluator re-entry; no public signature change is required.
3. Save the correct resume position before transferring to a handler, then reload the selected frame. Unwind through the existing handler restoration path, updated to the frame layout established by `vm-local-stack-scope`. Check nested frames and native freeze records as well as operand height.
4. Settle pending metering charges before exposing an error. A terminal settlement failure keeps its existing precedence. Never turn resource-limit failures into catchable ordinary errors or classify a thrown string by its spelling.
5. Keep cleanup at the same public boundaries. Handled errors continue in the same evaluation; unhandled failures leave no state that affects pooled `Eval` or `Apply` reuse.

## Risks / Trade-offs

- Catch routing can swallow terminal errors → test wrapped cancellation/deadline errors and resource ceilings by error identity, with handler side effects asserted absent.
- Unwind can leave stale freeze or depth state → test nested handlers, an error during native argument evaluation, and successful reuse after failure.
- Thrown values can be lost at evaluator boundaries → test String and non-String throws directly and through closure/host re-entry paths already supported by the runtime.
- A wider routing path can alter charged work before failure → retain exact settlement behavior and existing terminal-meter tests.

## Migration Plan

Implement and archive `vm-local-stack-scope` first. Add regressions, unify runtime routing and uncaught throw handling, run bounded state/error tests, then update existing architecture and changelog text. No storage migration. `WithTreeWalker()` is the rollback path.

## Plan appendix

```json
{
  "v": 2,
  "change": "vm-runtime-error-parity",
  "baseSha": "4b8fb7507e347bcc7bbbd63f4c2b1c67a41e5b41",
  "generatedAt": "2026-09-07T16:10:29.618Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "floor": "timeout 10m make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && timeout 5m make lint",
  "chunks": [
    {
      "id": "preflight",
      "taskIds": [
        "0.1",
        "0.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "preflight-contract",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [
        "./core/vm",
        "./core"
      ],
      "sites": [
        {
          "task": "0.1",
          "file": "core/vm/local_operand_parity_test.go",
          "symbol": "TestVMLocalOperandParity",
          "anchor": "func TestVMLocalOperandParity(t *testing.T) {",
          "change": "Verify vm-local-stack-scope predecessor archived; inspect Frame layout and stack isolation contract; verify local operand and loop scope regressions pass."
        },
        {
          "task": "0.2",
          "file": "core/eval_terminal_test.go",
          "symbol": "TestEvalTry_DeadlineEvasionLoopTerminates",
          "anchor": "func TestEvalTry_DeadlineEvasionLoopTerminates(t *testing.T) {",
          "change": "Confirm existing terminal error identity, metering settlement and uncatchable errors; verify syntax and bytecode validation stay unrouted."
        }
      ],
      "contract": {
        "states": [
          "vm-local-stack-scope predecessor is archived at commit c6de454 and frame locals remain below temporary operands",
          "existing coverage inventory names ordinary errors, explicit throws, terminal identity, metering settlement, reuse, compile refusal, and bytecode validation"
        ],
        "transitions": [
          {
            "input": "task 0.1: archived predecessor plus its local/loop regression suite",
            "state": "settled frame/operand baseline",
            "effect": "set",
            "evidence": "git log c6de454; core/vm/frame.go:5-14; core/vm/local_operand_parity_test.go:14-19; core/vm/loop_operand_bound_test.go:123-128"
          },
          {
            "input": "task 0.2: existing-service-strict coverage audit",
            "state": "coverage inventory",
            "effect": "set",
            "evidence": "core/vm/crossval_test.go:558-638,641-724,853-881,1299-1345; core/vm/vm_terminal_test.go:192-265; core/compiler/malformed_test.go:11-59; core/vm/validate_test.go:13-120; runtime/apply_pool_test.go:113-158"
          }
        ],
        "forbidden": [
          "Treat compiler CodeCompileError or Chunk.Validate refusal as a runtime opcode error.",
          "Seed VM locals or handler snapshots by direct field mutation in sealed behavior tests."
        ],
        "seeding": [
          "Frame/operand baseline: compile and execute the Lisp sources used by TestVMLocalOperandParity, TestVMLoopScopeParity, and TestVMLoopOperandBound.",
          "Compile refusal: malformed Lisp forms through compiler.NewCompiler.Compile.",
          "Bytecode refusal: malformed vm.Chunk through (*vm.Chunk).Validate before VM.Run."
        ],
        "budgets": [
          "Predecessor regression run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "TestVMLoopOperandBound samples 10, 100, and 1000 iterations and permits at most +8 live operands over the 10-iteration baseline."
        ]
      },
      "redTasks": [],
      "codeTasks": [],
      "redTests": [],
      "redRun": "",
      "verify": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run 'Test(VMLocalOperandParity|VMVsTreeWalker_TerminalErrorNotCaught|EvalTry_DeadlineEvasionLoopTerminates)$'",
      "coder": "zpatcher"
    },
    {
      "id": "routing",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "ordinary-opcode-routing",
      "shard": "",
      "pkgDirs": [
        "core/vm"
      ],
      "pkgs": [
        "./core/vm"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/vm/crossval_test.go",
          "symbol": "TestVMOrdinaryErrorCatchParity",
          "anchor": "func TestVMVsTreeWalker_TryCatch(t *testing.T) {",
          "change": "Add test TestVMOrdinaryErrorCatchParity: (try missing (catch e 42)), undefined mutation, nested closure errors, runtime collection construction errors; assert exact handler values through both evaluators; confirm current VM skips handlers."
        },
        {
          "task": "2.1",
          "file": "core/vm/vm.go",
          "symbol": "VM.run",
          "anchor": "case OpGetGlobal:",
          "change": "Route errors raised by valid runtime opcodes through a common policy using core.IsTerminalEvalError and existing handler unwinding; lookup, assignment, construction, call and native-operation cases reach the expected handler; terminal tests still bypass it."
        }
      ],
      "contract": {
        "states": [
          "active handler snapshot: addr, frameDepth, stackDepth, freezeDepth, structDepth",
          "ordinary error remains its original error object until handler selection",
          "handled ordinary error becomes core.String{V: err.Error()}",
          "terminal error remains unhandled and preserves identity",
          "undefined lexical assignment leaves the environment unbound"
        ],
        "transitions": [
          {
            "input": "task 1.1/2.1: undefined value/function lookup from valid OpGetGlobal, OpGetFunc, OpFreezeNative, OpFreezeNativeFunc, or fused resolution with active handler",
            "state": "nearest handler receives core.String value exactly \"UndefinedError: undefined: missing\" for symbol missing",
            "effect": "set",
            "evidence": "R1 undefined lookup caught; core/error.go:22-27,106-108; core/eval.go:1958-1974; core/vm/vm.go:1044-1051,1109-1116,1454-1470"
          },
          {
            "input": "task 1.1/2.1: (set! missing 1) reaches OpSetLexical with active handler",
            "state": "nearest handler receives core.String value and missing remains unbound",
            "effect": "set",
            "evidence": "R1 undefined mutation caught without creating binding; core/vm/vm.go:1095-1107"
          },
          {
            "input": "task 1.1/2.1: nested closure raises ordinary lookup/call error inside outer try",
            "state": "outer nearest handler resumes on its saved frame",
            "effect": "set",
            "evidence": "R1 closure error reaches enclosing handler; core/vm/vm.go:1161-1175,1966-1991"
          },
          {
            "input": "task 1.1/2.1: runtime OpMakeMap rejects an evaluated unhashable key",
            "state": "nearest handler receives the tree-walker-equivalent rendered message",
            "effect": "set",
            "evidence": "R1 runtime collection construction error; core/vm/vm.go:1249-1270; runtime/vm_map_literal_error_test.go:17-49"
          },
          {
            "input": "task 1.1/2.1: callable/type/arity failure from OpCall, OpTailCall, native opcode, or OpFusedNativeOp",
            "state": "nearest handler receives core.String{V: err.Error()}",
            "effect": "set",
            "evidence": "R1 call and native-operation cases; core/vm/vm.go:1161-1192,1335-1365,1994-2088"
          },
          {
            "input": "task 1.1/2.1: same ordinary runtime error with no active handler",
            "state": "host receives original error object and *core.LispicoError Code such as \"UndefinedError\", \"TypeError\", or \"EvalError\"",
            "effect": "no-op",
            "evidence": "R1 original error class reaches host; core/error.go:12-30"
          },
          {
            "input": "task 1.1/2.1: wrapped context.Canceled, wrapped context.DeadlineExceeded, or *core.LispicoError Code core.CodeResourceLimit",
            "state": "handler is not entered and terminal error identity reaches caller",
            "effect": "forced",
            "evidence": "R1 terminal bypass; core/error.go:32-47; core/vm/crossval_test.go:641-724"
          },
          {
            "input": "task 1.1/2.1: malformed source or structurally invalid chunk",
            "state": "compiler/Chunk.Validate refusal occurs before VM runtime routing",
            "effect": "no-op",
            "evidence": "R1 compile-time syntax and validation outside routing; core/compiler/malformed_test.go:11-59; core/vm/validate_test.go:13-120; runtime/eval.go:430-612"
          }
        ],
        "forbidden": [
          "A terminal error executes any catch-body side effect.",
          "An unhandled ordinary error is replaced by a core.String or a new error class.",
          "A failed OpSetLexical creates missing in any environment.",
          "BytecodeError from malformed structural bytecode enters Lisp catch routing.",
          "ResourceLimitError from construction depth is treated as the ordinary OpMakeMap unhashable-key failure."
        ],
        "seeding": [
          "Active handler: compile (try <body> (catch e <handler>)) through compiler.CompileAll; never append handler records directly.",
          "Undefined lookup: (try missing (catch e e)); exact control value comes from core.NewUndefinedError(\"missing\").Error().",
          "Undefined mutation: (try (set! missing 1) (catch e e)), then evaluate missing outside the catch and require UndefinedError.",
          "Nested frame: (try ((fn [] missing)) (catch e e)).",
          "Runtime construction: bind vector k, then evaluate (try {k 3} (catch e e)) so parsing succeeds and OpMakeMap rejects at execution.",
          "Call/native: use (try (1) (catch e e)) and (try (+ 1 \"a\") (catch e e)) with the same bindings in both evaluators.",
          "Terminal side effects: bind a GoFunc marker used only in catch; inject fmt.Errorf(\"wrapped: %w\", context.Canceled), fmt.Errorf(\"wrapped: %w\", context.DeadlineExceeded), and core.NewResourceLimitError(\"limit\"); require marker count 0."
        ],
        "budgets": [
          "Red run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "Handler selection is nearest-active and linear only in stale handler/frame entries already present; no new per-opcode scan or allocation on successful instructions."
        ]
      },
      "redTasks": [
        "Add sealed TestVMOrdinaryErrorCatchParity in core/vm/crossval_test.go with named rows for lookup, mutation, nested closure, map construction, call, native op, unhandled original class, and terminal side-effect absence; demonstrate current VM skips ordinary handlers. (Review note: the native-op row already routes through vm.throw at core/vm/vm.go:1335-1345 and passes pre-change; the red signal must come from the lookup/mutation/closure/map-construction/call rows that currently return directly.)"
      ],
      "codeTasks": [
        "In core/vm/vm.go centralize runtime-error transfer and replace direct returns for valid runtime opcode failures; save current frame ip, settle charges, test core.IsTerminalEvalError, transfer err.Error() as core.String only when a handler exists, reload the selected frame, and preserve the original error when unhandled."
      ],
      "redTests": [
        "TestVMOrdinaryErrorCatchParity"
      ],
      "redRun": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$'",
      "verify": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$' && go build ./core ./core/vm && go vet ./core ./core/vm && golangci-lint run ./core ./core/vm",
      "coder": "go-coder"
    },
    {
      "id": "throw-identity",
      "taskIds": [
        "1.2",
        "2.2"
      ],
      "prev": "routing",
      "sharedPkg": "./core/vm",
      "parallel": false,
      "seam": "throw-value-identity",
      "shard": "",
      "pkgDirs": [
        "core/vm",
        "core"
      ],
      "pkgs": [
        "./core/vm",
        "./core"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "core/vm/crossval_test.go",
          "symbol": "TestVMUncaughtThrowParity",
          "anchor": "func TestVMVsTreeWalker_NonStringThrow(t *testing.T) {",
          "change": "Add test TestVMUncaughtThrowParity: (throw \"boom\"), non-String throw, rethrow; assert errors.As, ThrowError, equal rendering, caught-value runtime type; verify current VM uncaught throws fail those assertions."
        },
        {
          "task": "2.2",
          "file": "core/vm/vm.go",
          "symbol": "VM.run",
          "anchor": "case OpThrow:",
          "change": "Return the original ordinary error when unhandled; preserve ThrowError plus rendering for uncaught explicit throw; TestVMUncaughtThrowParity, existing structured-throw cases and supported evaluator re-entry paths retain value and error identity (shared with core/error.go)."
        },
        {
          "task": "2.2",
          "file": "core/eval.go",
          "symbol": "evalThrow",
          "anchor": "func evalThrow(ctx context.Context, e *engine, args []Value, env *Env) (Value, error) {",
          "change": "Share the throw value-carrier protocol with the VM: evalTry recognizes the exported ThrownValue() method; rendering parity for uncaught throws."
        }
      ],
      "contract": {
        "states": [
          "explicit throw carrier retains original core.Value",
          "errors.As reaches *core.LispicoError",
          "LispicoError.Code is exactly \"ThrowError\"",
          "string throw rendering is String.V without quotes",
          "non-String throw rendering is fmt.Sprintf(\"%v\", value)",
          "caught explicit throw binds original runtime type",
          "ordinary errors bind rendered core.String instead"
        ],
        "transitions": [
          {
            "input": "task 1.2/2.2: (throw \"boom\") with no handler through tree-walker, VM.Run, runtime.Engine.Eval, and callable host boundary",
            "state": "errors.As exposes *core.LispicoError{Code: \"ThrowError\", Message: \"boom\"}; err.Error rendering matches tree-walker modulo runtime boundary prefix",
            "effect": "set",
            "evidence": "R2 uncaught string throw; core/eval.go:1982-2005; core/error.go:12-30; core/eval_throw_test.go:8-24"
          },
          {
            "input": "task 1.2/2.2: uncaught Int or HashMap throw",
            "state": "ThrowError Message and raw carrier Error equal fmt.Sprintf(\"%v\", original value)",
            "effect": "set",
            "evidence": "R2 uncaught non-String rendering; core/eval.go:2001-2005"
          },
          {
            "input": "task 1.2/2.2: caught HashMap throw",
            "state": "catch binding is *core.HashMap, not core.String",
            "effect": "set",
            "evidence": "R2 caught structured value; core/eval.go:1958-1974; core/eval_test.go:537-543"
          },
          {
            "input": "task 1.2/2.2: catch e then (throw e)",
            "state": "new explicit throw preserves e runtime type, ThrowError classification, and rendering",
            "effect": "set",
            "evidence": "R2 rethrow and original value; task 1.2"
          },
          {
            "input": "task 1.2/2.2: explicit throw crosses VM -> GoFunc/evaluator -> tree try or tree -> evaluator -> VM try",
            "state": "receiving evaluator recognizes value carrier and binds original core.Value",
            "effect": "set",
            "evidence": "design decision 2; runtime evaluator re-entry paths core/vm/vm.go:523-558,1994-2046"
          },
          {
            "input": "task 1.2/2.2: (throw \"context deadline exceeded\") inside try",
            "state": "ordinary explicit throw is caught and text is bound unchanged",
            "effect": "set",
            "evidence": "R2 terminal-looking text remains catchable; core/eval_terminal_test.go:61-67; core/error_test.go:59-61"
          },
          {
            "input": "task 1.2/2.2: OpThrow has no active handler",
            "state": "typed ThrowError carrier reaches host",
            "effect": "forced",
            "evidence": "R2 throwing never becomes missing-handler TypeError; current incorrect substitution core/vm/vm.go:1324-1332"
          }
        ],
        "forbidden": [
          "Uncaught OpThrow returns core.NewTypeError(\"handler\", core.Nil{}).",
          "A structured thrown value is stringified before an active catch binds it.",
          "Text equal to a context or resource error message is classified terminal without core.IsTerminalEvalError identity/code evidence.",
          "Sealed tests assert the private carrier concrete type; the host contract is errors.As to *core.LispicoError plus Code \"ThrowError\" and rendering."
        ],
        "seeding": [
          "String host error: evaluate (throw \"boom\") through WithTreeWalker and WithBytecode engines and directly through compiled VM.",
          "Non-String host error: evaluate (throw 42) and (throw {:code :denied}); expected rendering is derived independently from the same core.Value String method, not copied from VM output.",
          "Structured catch: (try (throw {:code :denied}) (catch e e)); assert *core.HashMap and keyword lookup result.",
          "Rethrow: (try (throw {:code :denied}) (catch e (throw e))).",
          "Re-entry: a bound GoFunc calls the supplied core.Evaluator on a form that throws, while an outer try belongs to the other evaluator path.",
          "Terminal-looking string: (try (throw \"context deadline exceeded\") (catch e e))."
        ],
        "budgets": [
          "Red run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "Throw preservation adds no deep copy of core.Value and no string formatting on the caught explicit-throw path beyond construction of the existing typed cause."
        ]
      },
      "redTasks": [
        "Add sealed TestVMUncaughtThrowParity in core/vm/crossval_test.go: table string/non-String/structured/rethrow/re-entry paths; assert errors.As to *core.LispicoError, Code exactly \"ThrowError\", Message and Error rendering parity, and caught runtime type; demonstrate current VM returns TypeError for uncaught OpThrow."
      ],
      "codeTasks": [
        "Move the existing private throw carrier contract to a cross-package value-carrier protocol: keep private concrete carriers, add an exported ThrownValue() core.Value method, make evalTry recognize that protocol, and make VM routing preserve it across evaluator re-entry. For unhandled OpThrow construct the same *core.LispicoError Code \"ThrowError\" and String.V/fmt.Sprintf rendering. Do not add an exported type, option, sentinel, or public signature."
      ],
      "redTests": [
        "TestVMUncaughtThrowParity"
      ],
      "redRun": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$'",
      "verify": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$' && go build ./core ./core/vm ./runtime && go vet ./core ./core/vm ./runtime && golangci-lint run ./core ./core/vm ./runtime",
      "coder": "go-coder"
    },
    {
      "id": "unwind-reuse",
      "taskIds": [
        "1.3",
        "2.3"
      ],
      "prev": "throw-identity",
      "sharedPkg": "./core/vm",
      "parallel": false,
      "seam": "unwind-restoration-reuse",
      "shard": "",
      "pkgDirs": [
        "core/vm",
        "runtime"
      ],
      "pkgs": [
        "./core/vm",
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.3",
          "file": "core/vm/vm_terminal_test.go",
          "symbol": "TestVMErrorUnwindReuse",
          "anchor": "func TestVM_TerminalErrorUnwindsStacks(t *testing.T) {",
          "change": "Add test TestVMErrorUnwindReuse: error during native argument evaluation, nested handlers, successful subsequent Eval/Apply; assert no stale operands, callee freeze records, reduced structural-depth allowance. Extend existing terminal-error tests with handler side effects that must remain absent for wrapped cancellation/deadline and resource-limit errors."
        },
        {
          "task": "1.3",
          "file": "runtime/apply_pool_test.go",
          "symbol": "TestBytecodeRuntime_CallSequenceNoStateLeak",
          "anchor": "func TestBytecodeRuntime_CallSequenceNoStateLeak(t *testing.T) {",
          "change": "Add pooled Eval/Call reuse assertions after unhandled failure (fresh state, full depth allowance)."
        },
        {
          "task": "2.3",
          "file": "core/vm/vm.go",
          "symbol": "VM.throw",
          "anchor": "func (vm *VM) throw(value core.Value) bool {",
          "change": "Preserve resume positions, frame reloads, pending-charge settlement, all handler state restoration; TestVMErrorUnwindReuse and existing terminal/meter settlement tests pass without changing terminal-error precedence."
        }
      ],
      "contract": {
        "states": [
          "current frame ip saved before transfer",
          "selected handler frame is top frame and dispatch locals are reloaded",
          "operand stack length equals handler.stackDepth plus one caught value",
          "freezeStack length equals handler.freezeDepth",
          "structural depth equals handler.structDepth",
          "closure depth decremented for each unwound closure frame",
          "consumed reductions and pending allocation bytes are settled",
          "public pooled VM is clean after unhandled failure"
        ],
        "transitions": [
          {
            "input": "task 1.3/2.3: ordinary error during native argument evaluation under nested closure and try",
            "state": "nearest handler frame, saved resume address, operand snapshot, freeze snapshot, and closure depth restored",
            "effect": "set",
            "evidence": "R1 native argument failure and nested handlers; core/vm/vm.go:63-68,91-96,1161-1192,1318-1319,1966-1991"
          },
          {
            "input": "task 1.3/2.3: error interrupts OpStructEnter before OpStructLeave",
            "state": "structural depth restored to handler snapshot and full configured allowance remains for handler/later evaluation",
            "effect": "set",
            "evidence": "R1 structural-depth restoration; core/vm/vm_test.go:1986-2053; core/vm/vm.go:1277-1293,1318-1319,1976-1991"
          },
          {
            "input": "task 1.3/2.3: catch body executes after transfer",
            "state": "dispatch reload uses handler chunk/code/ip/base/env/caps/truthiness and resumes exactly once",
            "effect": "forced",
            "evidence": "design decision 3; core/vm/vm.go:609-622,1161-1175,1324-1333"
          },
          {
            "input": "task 1.3/2.3: pending reductions/allocation exist when ordinary opcode fails",
            "state": "both charges settle before handler or host observes failure; successful settlement preserves original error",
            "effect": "clear",
            "evidence": "design decision 4; core/vm/vm.go:406-432,919-940; core/vm/crossval_test.go:558-638"
          },
          {
            "input": "task 1.3/2.3: settlement raises ResourceLimitError while an ordinary error or explicit throw is pending",
            "state": "core.CodeResourceLimit terminal error replaces pending error and bypasses handler",
            "effect": "forced",
            "evidence": "design decision 4 terminal precedence; core/vm/vm.go:924-939; core/builtin_budget_test.go:323-379"
          },
          {
            "input": "task 1.3/2.3: wrapped cancellation/deadline or resource limit occurs under active handler",
            "state": "handler side-effect count stays 0 and public boundary resets VM state",
            "effect": "forced",
            "evidence": "R1 terminal cannot run handler; core/error.go:32-47; runtime/eval.go:900-1010"
          },
          {
            "input": "task 1.3/2.3: unhandled Eval or Apply failure followed by successful pooled Eval/Apply",
            "state": "later result and available structural allowance equal a fresh engine/VM result",
            "effect": "clear",
            "evidence": "R1 failed evaluation does not poison reuse; core/vm/vm.go:234-324,891-916; runtime/apply_pool_test.go:113-158; runtime/eval.go:523-612,900-1010"
          }
        ],
        "forbidden": [
          "A caught error leaves an aborted frame, operand, native freeze record, or structural-depth increment live.",
          "Handler dispatch continues with stale chunk/code/ip/base/env/caps locals.",
          "An ordinary pending error outranks a terminal error raised while settling prior work.",
          "A terminal path runs catch side effects.",
          "A failed public Eval or Apply returns a dirty VM to a pool or reusable engine slot."
        ],
        "seeding": [
          "Freeze/operand failure: freeze canonical +, then evaluate an argument that raises UndefinedError inside a nested closure; catch rebinds + and invokes it, proving stale freeze and operands are absent.",
          "Nearest handler/resume: nested try forms with distinct marker results, followed by a continuation value; assert only inner handler executes and continuation executes once.",
          "Structural depth: enter a collection expression that fails before OpStructLeave; catch constructs a value exactly at configured MaxStructuralDepth, then run another exact-limit construction.",
          "Settlement: execute two scalar native results to accumulate pending bytes, then raise an ordinary GoFunc error; use EvalMeter snapshot and a ceiling one byte below settled total for terminal-wins row.",
          "Pooled reuse: on one bytecode engine, fail Eval, then Eval 42; define a callable, fail Call/Apply, then Call successfully. Exercise both general vmPool and lean engine-slot release paths.",
          "Terminal absence: catch body calls a counter GoFunc; inject wrapped canceled, wrapped deadline, and core.NewResourceLimitError through call/native paths; require counter 0."
        ],
        "budgets": [
          "Red run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "Resource settlement case uses an exact deterministic allocation ceiling derived from core.MeterScalarBytes; no timing tolerance.",
          "Structural case uses exact configured MaxStructuralDepth and requires the full allowance after unwind/reuse.",
          "Race floor: wall timeout 10m; Go test timeout 2m; package workers 2; test parallelism 2."
        ]
      },
      "redTasks": [
        "Add sealed TestVMErrorUnwindReuse in core/vm/vm_terminal_test.go for nested handler/resume, operand/freeze/structural restoration, settlement precedence, and successful later ApplyPooled; extend terminal cases with wrapped-error handler counters. Add public pooled Eval/Call reuse assertions in runtime/apply_pool_test.go under the same fixtures."
      ],
      "codeTasks": [
        "In core/vm/vm.go make the common routing path save frame.ip, settle consumed reductions and pending allocation bytes, give any settlement terminal error precedence, call existing throw restoration only for non-terminal handled errors, reload frame locals, and leave Run/ApplyPooled plus runtime release paths responsible for unhandled cleanup. Preserve handler snapshots from OpSetupTry and vm-local-stack-scope frame bases."
      ],
      "redTests": [
        "TestVMErrorUnwindReuse"
      ],
      "redRun": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$'",
      "verify": "timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm ./runtime -run 'Test(VMOrdinaryErrorCatchParity|VMUncaughtThrowParity|VMErrorUnwindReuse)$' && go build ./core ./core/vm ./runtime && go vet ./core ./core/vm ./runtime && golangci-lint run ./core ./core/vm ./runtime",
      "coder": "go-coder"
    },
    {
      "id": "docs",
      "taskIds": [
        "3.3"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "documentation",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.3",
          "file": "ARCHITECTURE.md",
          "symbol": "Terminal errors",
          "anchor": "### Terminal errors",
          "change": "Update existing ARCHITECTURE.md error semantics and [Unreleased] in existing CHANGELOG.md; ordinary opcode errors described as catchable, terminal classes excluded, uncaught throws retain rendering and classification."
        },
        {
          "task": "3.3",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Add [Unreleased] entry: ordinary VM opcode errors catchable, terminal classes excluded, uncaught throws keep rendering and ThrowError classification."
        }
      ],
      "contract": {
        "states": [
          "ARCHITECTURE.md error semantics match runtime contract",
          "CHANGELOG.md [Unreleased] contains one curated Fixed entry"
        ],
        "transitions": [
          {
            "input": "task 3.3: update Error Handling and Terminal errors",
            "state": "docs state ordinary valid-bytecode opcode errors are catchable strings, explicit throws retain values in catches and ThrowError/rendering at host, terminal classes bypass catches",
            "effect": "set",
            "evidence": "ARCHITECTURE.md:559-619,659-680; R1 and R2"
          },
          {
            "input": "task 3.3: update [Unreleased]",
            "state": "Fixed entry describes VM/tree-walker parity and host-visible throw correction",
            "effect": "set",
            "evidence": "CHANGELOG.md:8-18; hand-maintained Keep a Changelog layout"
          }
        ],
        "forbidden": [
          "Claim compile-time or bytecode-validation failures are catchable.",
          "Describe ResourceLimitError, cancellation, or deadlines as catchable.",
          "Document private helper names or implementation mechanics as public API."
        ],
        "seeding": [
          "Use existing ARCHITECTURE.md Error Handling/Error codes/Terminal errors sections.",
          "Use CHANGELOG.md [Unreleased] Fixed heading; no release cut."
        ],
        "budgets": [
          "One concise [Unreleased] Fixed bullet; no new ADR or dependency documentation."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "Modify ARCHITECTURE.md and CHANGELOG.md only after behavior is green."
      ],
      "redTests": [],
      "redRun": "",
      "verify": "git diff --check -- ARCHITECTURE.md CHANGELOG.md && golangci-lint run ./core/... ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "scoped-race-floor",
      "taskIds": [
        "3.1",
        "3.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "targeted-and-race-floor",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [
        "./core/...",
        "./runtime",
        "./internal/goldset",
        "./core/vm"
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "Makefile",
          "symbol": "test",
          "anchor": "test:\n\tgo test $(GOTESTFLAGS) ./...",
          "change": "Run timeout 10m go test -timeout 2m -p 2 -parallel 2 ./core/... ./runtime ./internal/goldset; record names/commands/results."
        },
        {
          "task": "3.2",
          "file": "Makefile",
          "symbol": "GOTESTFLAGS",
          "anchor": "GOTESTFLAGS ?= -timeout 2m",
          "change": "Run timeout 10m go test -race -timeout 2m -p 2 -parallel 2 ./core/vm ./runtime; race-free shared counters, handlers, reused VM state."
        }
      ],
      "contract": {
        "states": [
          "core/runtime/goldset slice green",
          "VM/runtime race slice green"
        ],
        "transitions": [
          {
            "input": "task 3.1: scoped service suite",
            "state": "ordinary routing, throw, terminal, metering, reuse, and goldset regressions pass",
            "effect": "set",
            "evidence": "tasks.md 3.1"
          },
          {
            "input": "task 3.2: race slice",
            "state": "shared counters, handlers, and reused VM state are race-free",
            "effect": "set",
            "evidence": "tasks.md 3.2"
          }
        ],
        "forbidden": [
          "Add -count=1 to cache-preserving developer or race commands.",
          "Run race detection in the red inner loop."
        ],
        "seeding": [
          "Run after implementation, docs, targeted red command, per-chunk build/vet/lint, and with no skipped sealed tests."
        ],
        "budgets": [
          "Each command wall timeout 10m; Go test timeout 2m; package workers 2; test parallelism 2."
        ]
      },
      "redTasks": [],
      "codeTasks": [],
      "redTests": [],
      "redRun": "",
      "verify": "timeout 10m go test -timeout 2m -p 2 -parallel 2 ./core/... ./runtime ./internal/goldset && timeout 10m go test -race -timeout 2m -p 2 -parallel 2 ./core/vm ./runtime",
      "coder": "zpatcher"
    },
    {
      "id": "repo-floor",
      "taskIds": [
        "3.4"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "repository-floor",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.4",
          "file": "Makefile",
          "symbol": "lint",
          "anchor": "lint:\n\tgolangci-lint run",
          "change": "Run timeout 10m make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\", timeout 5m make lint, openspec validate vm-runtime-error-parity --strict --json."
        }
      ],
      "contract": {
        "states": [
          "repository test floor green",
          "repository lint floor green",
          "strict change validation green"
        ],
        "transitions": [
          {
            "input": "task 3.4: make test with bounded GOTESTFLAGS",
            "state": "complete repository tests pass",
            "effect": "set",
            "evidence": "Makefile:14-17; tasks.md 3.4"
          },
          {
            "input": "task 3.4: make lint",
            "state": "golangci-lint passes",
            "effect": "set",
            "evidence": "Makefile:19-20; tasks.md 3.4"
          },
          {
            "input": "task 3.4: strict validation",
            "state": "vm-runtime-error-parity artifact validates",
            "effect": "set",
            "evidence": "tasks.md 3.4"
          }
        ],
        "forbidden": [
          "Drop timeout, -p 2, or -parallel 2 limits.",
          "Substitute a narrowed suite for make test.",
          "Add -count=1."
        ],
        "seeding": [
          "Run only after all code, tests, and docs are complete."
        ],
        "budgets": [
          "make test wall timeout 10m with Go timeout 2m, package workers 2, test parallelism 2; make lint wall timeout 5m."
        ]
      },
      "redTasks": [],
      "codeTasks": [],
      "redTests": [],
      "redRun": "",
      "verify": "timeout 10m make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2' && timeout 5m make lint && openspec validate vm-runtime-error-parity --strict --json",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "preflight-contract",
      "summary": "NO-RED-WAIVER: verification-only prerequisite; it confirms the settled frame contract and inventories existing strict regressions before any new red test is sealed.",
      "tasks": [
        "0.1",
        "0.2"
      ],
      "redTasks": [],
      "codeTasks": [],
      "contract": {
        "states": [
          "vm-local-stack-scope predecessor is archived at commit c6de454 and frame locals remain below temporary operands",
          "existing coverage inventory names ordinary errors, explicit throws, terminal identity, metering settlement, reuse, compile refusal, and bytecode validation"
        ],
        "transitions": [
          {
            "input": "task 0.1: archived predecessor plus its local/loop regression suite",
            "state": "settled frame/operand baseline",
            "effect": "set",
            "evidence": "git log c6de454; core/vm/frame.go:5-14; core/vm/local_operand_parity_test.go:14-19; core/vm/loop_operand_bound_test.go:123-128"
          },
          {
            "input": "task 0.2: existing-service-strict coverage audit",
            "state": "coverage inventory",
            "effect": "set",
            "evidence": "core/vm/crossval_test.go:558-638,641-724,853-881,1299-1345; core/vm/vm_terminal_test.go:192-265; core/compiler/malformed_test.go:11-59; core/vm/validate_test.go:13-120; runtime/apply_pool_test.go:113-158"
          }
        ],
        "forbidden": [
          "Treat compiler CodeCompileError or Chunk.Validate refusal as a runtime opcode error.",
          "Seed VM locals or handler snapshots by direct field mutation in sealed behavior tests."
        ],
        "seeding": [
          "Frame/operand baseline: compile and execute the Lisp sources used by TestVMLocalOperandParity, TestVMLoopScopeParity, and TestVMLoopOperandBound.",
          "Compile refusal: malformed Lisp forms through compiler.NewCompiler.Compile.",
          "Bytecode refusal: malformed vm.Chunk through (*vm.Chunk).Validate before VM.Run."
        ],
        "budgets": [
          "Predecessor regression run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "TestVMLoopOperandBound samples 10, 100, and 1000 iterations and permits at most +8 live operands over the 10-iteration baseline."
        ]
      }
    },
    {
      "id": "ordinary-opcode-routing",
      "summary": "Route every ordinary error produced while executing valid bytecode to the nearest active catch; bind err.Error() only after a handler is selected, otherwise return the original error unchanged.",
      "tasks": [
        "1.1",
        "2.1"
      ],
      "redTasks": [
        "Add sealed TestVMOrdinaryErrorCatchParity in core/vm/crossval_test.go with named rows for lookup, mutation, nested closure, map construction, call, native op, unhandled original class, and terminal side-effect absence; demonstrate current VM skips ordinary handlers."
      ],
      "codeTasks": [
        "In core/vm/vm.go centralize runtime-error transfer and replace direct returns for valid runtime opcode failures; save current frame ip, settle charges, test core.IsTerminalEvalError, transfer err.Error() as core.String only when a handler exists, reload the selected frame, and preserve the original error when unhandled."
      ],
      "contract": {
        "states": [
          "active handler snapshot: addr, frameDepth, stackDepth, freezeDepth, structDepth",
          "ordinary error remains its original error object until handler selection",
          "handled ordinary error becomes core.String{V: err.Error()}",
          "terminal error remains unhandled and preserves identity",
          "undefined lexical assignment leaves the environment unbound"
        ],
        "transitions": [
          {
            "input": "task 1.1/2.1: undefined value/function lookup from valid OpGetGlobal, OpGetFunc, OpFreezeNative, OpFreezeNativeFunc, or fused resolution with active handler",
            "state": "nearest handler receives core.String value exactly \"UndefinedError: undefined: missing\" for symbol missing",
            "effect": "set",
            "evidence": "R1 undefined lookup caught; core/error.go:22-27,106-108; core/eval.go:1958-1974; core/vm/vm.go:1044-1051,1109-1116,1454-1470"
          },
          {
            "input": "task 1.1/2.1: (set! missing 1) reaches OpSetLexical with active handler",
            "state": "nearest handler receives core.String value and missing remains unbound",
            "effect": "set",
            "evidence": "R1 undefined mutation caught without creating binding; core/vm/vm.go:1095-1107"
          },
          {
            "input": "task 1.1/2.1: nested closure raises ordinary lookup/call error inside outer try",
            "state": "outer nearest handler resumes on its saved frame",
            "effect": "set",
            "evidence": "R1 closure error reaches enclosing handler; core/vm/vm.go:1161-1175,1966-1991"
          },
          {
            "input": "task 1.1/2.1: runtime OpMakeMap rejects an evaluated unhashable key",
            "state": "nearest handler receives the tree-walker-equivalent rendered message",
            "effect": "set",
            "evidence": "R1 runtime collection construction error; core/vm/vm.go:1249-1270; runtime/vm_map_literal_error_test.go:17-49"
          },
          {
            "input": "task 1.1/2.1: callable/type/arity failure from OpCall, OpTailCall, native opcode, or OpFusedNativeOp",
            "state": "nearest handler receives core.String{V: err.Error()}",
            "effect": "set",
            "evidence": "R1 call and native-operation cases; core/vm/vm.go:1161-1192,1335-1365,1994-2088"
          },
          {
            "input": "task 1.1/2.1: same ordinary runtime error with no active handler",
            "state": "host receives original error object and *core.LispicoError Code such as \"UndefinedError\", \"TypeError\", or \"EvalError\"",
            "effect": "no-op",
            "evidence": "R1 original error class reaches host; core/error.go:12-30"
          },
          {
            "input": "task 1.1/2.1: wrapped context.Canceled, wrapped context.DeadlineExceeded, or *core.LispicoError Code core.CodeResourceLimit",
            "state": "handler is not entered and terminal error identity reaches caller",
            "effect": "forced",
            "evidence": "R1 terminal bypass; core/error.go:32-47; core/vm/crossval_test.go:641-724"
          },
          {
            "input": "task 1.1/2.1: malformed source or structurally invalid chunk",
            "state": "compiler/Chunk.Validate refusal occurs before VM runtime routing",
            "effect": "no-op",
            "evidence": "R1 compile-time syntax and validation outside routing; core/compiler/malformed_test.go:11-59; core/vm/validate_test.go:13-120; runtime/eval.go:430-612"
          }
        ],
        "forbidden": [
          "A terminal error executes any catch-body side effect.",
          "An unhandled ordinary error is replaced by a core.String or a new error class.",
          "A failed OpSetLexical creates missing in any environment.",
          "BytecodeError from malformed structural bytecode enters Lisp catch routing.",
          "ResourceLimitError from construction depth is treated as the ordinary OpMakeMap unhashable-key failure."
        ],
        "seeding": [
          "Active handler: compile (try <body> (catch e <handler>)) through compiler.CompileAll; never append handler records directly.",
          "Undefined lookup: (try missing (catch e e)); exact control value comes from core.NewUndefinedError(\"missing\").Error().",
          "Undefined mutation: (try (set! missing 1) (catch e e)), then evaluate missing outside the catch and require UndefinedError.",
          "Nested frame: (try ((fn [] missing)) (catch e e)).",
          "Runtime construction: bind vector k, then evaluate (try {k 3} (catch e e)) so parsing succeeds and OpMakeMap rejects at execution.",
          "Call/native: use (try (1) (catch e e)) and (try (+ 1 \"a\") (catch e e)) with the same bindings in both evaluators.",
          "Terminal side effects: bind a GoFunc marker used only in catch; inject fmt.Errorf(\"wrapped: %w\", context.Canceled), fmt.Errorf(\"wrapped: %w\", context.DeadlineExceeded), and core.NewResourceLimitError(\"limit\"); require marker count 0."
        ],
        "budgets": [
          "Red run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "Handler selection is nearest-active and linear only in stale handler/frame entries already present; no new per-opcode scan or allocation on successful instructions."
        ]
      }
    },
    {
      "id": "throw-value-identity",
      "summary": "Represent explicit throw as an error carrying the original core.Value and a *core.LispicoError with Code \"ThrowError\"; use one value-carrier protocol across VM and tree-walker re-entry without adding a public signature or error-code constant.",
      "tasks": [
        "1.2",
        "2.2"
      ],
      "redTasks": [
        "Add sealed TestVMUncaughtThrowParity in core/vm/crossval_test.go: table string/non-String/structured/rethrow/re-entry paths; assert errors.As to *core.LispicoError, Code exactly \"ThrowError\", Message and Error rendering parity, and caught runtime type; demonstrate current VM returns TypeError for uncaught OpThrow."
      ],
      "codeTasks": [
        "Move the existing private throw carrier contract to a cross-package value-carrier protocol: keep private concrete carriers, add an exported ThrownValue() core.Value method, make evalTry recognize that protocol, and make VM routing preserve it across evaluator re-entry. For unhandled OpThrow construct the same *core.LispicoError Code \"ThrowError\" and String.V/fmt.Sprintf rendering. Do not add an exported type, option, sentinel, or public signature."
      ],
      "contract": {
        "states": [
          "explicit throw carrier retains original core.Value",
          "errors.As reaches *core.LispicoError",
          "LispicoError.Code is exactly \"ThrowError\"",
          "string throw rendering is String.V without quotes",
          "non-String throw rendering is fmt.Sprintf(\"%v\", value)",
          "caught explicit throw binds original runtime type",
          "ordinary errors bind rendered core.String instead"
        ],
        "transitions": [
          {
            "input": "task 1.2/2.2: (throw \"boom\") with no handler through tree-walker, VM.Run, runtime.Engine.Eval, and callable host boundary",
            "state": "errors.As exposes *core.LispicoError{Code: \"ThrowError\", Message: \"boom\"}; err.Error rendering matches tree-walker modulo runtime boundary prefix",
            "effect": "set",
            "evidence": "R2 uncaught string throw; core/eval.go:1982-2005; core/error.go:12-30; core/eval_throw_test.go:8-24"
          },
          {
            "input": "task 1.2/2.2: uncaught Int or HashMap throw",
            "state": "ThrowError Message and raw carrier Error equal fmt.Sprintf(\"%v\", original value)",
            "effect": "set",
            "evidence": "R2 uncaught non-String rendering; core/eval.go:2001-2005"
          },
          {
            "input": "task 1.2/2.2: caught HashMap throw",
            "state": "catch binding is *core.HashMap, not core.String",
            "effect": "set",
            "evidence": "R2 caught structured value; core/eval.go:1958-1974; core/eval_test.go:537-543"
          },
          {
            "input": "task 1.2/2.2: catch e then (throw e)",
            "state": "new explicit throw preserves e runtime type, ThrowError classification, and rendering",
            "effect": "set",
            "evidence": "R2 rethrow and original value; task 1.2"
          },
          {
            "input": "task 1.2/2.2: explicit throw crosses VM -> GoFunc/evaluator -> tree try or tree -> evaluator -> VM try",
            "state": "receiving evaluator recognizes value carrier and binds original core.Value",
            "effect": "set",
            "evidence": "design decision 2; runtime evaluator re-entry paths core/vm/vm.go:523-558,1994-2046"
          },
          {
            "input": "task 1.2/2.2: (throw \"context deadline exceeded\") inside try",
            "state": "ordinary explicit throw is caught and text is bound unchanged",
            "effect": "set",
            "evidence": "R2 terminal-looking text remains catchable; core/eval_terminal_test.go:61-67; core/error_test.go:59-61"
          },
          {
            "input": "task 1.2/2.2: OpThrow has no active handler",
            "state": "typed ThrowError carrier reaches host",
            "effect": "forced",
            "evidence": "R2 throwing never becomes missing-handler TypeError; current incorrect substitution core/vm/vm.go:1324-1332"
          }
        ],
        "forbidden": [
          "Uncaught OpThrow returns core.NewTypeError(\"handler\", core.Nil{}).",
          "A structured thrown value is stringified before an active catch binds it.",
          "Text equal to a context or resource error message is classified terminal without core.IsTerminalEvalError identity/code evidence.",
          "Sealed tests assert the private carrier concrete type; the host contract is errors.As to *core.LispicoError plus Code \"ThrowError\" and rendering."
        ],
        "seeding": [
          "String host error: evaluate (throw \"boom\") through WithTreeWalker and WithBytecode engines and directly through compiled VM.",
          "Non-String host error: evaluate (throw 42) and (throw {:code :denied}); expected rendering is derived independently from the same core.Value String method, not copied from VM output.",
          "Structured catch: (try (throw {:code :denied}) (catch e e)); assert *core.HashMap and keyword lookup result.",
          "Rethrow: (try (throw {:code :denied}) (catch e (throw e))).",
          "Re-entry: a bound GoFunc calls the supplied core.Evaluator on a form that throws, while an outer try belongs to the other evaluator path.",
          "Terminal-looking string: (try (throw \"context deadline exceeded\") (catch e e))."
        ],
        "budgets": [
          "Red run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "Throw preservation adds no deep copy of core.Value and no string formatting on the caught explicit-throw path beyond construction of the existing typed cause."
        ]
      }
    },
    {
      "id": "unwind-restoration-reuse",
      "summary": "Settle already-incurred charges before routing, then restore the selected handler's frame, operand, freeze, and structural-depth snapshot; terminal settlement failure wins and public pooled boundaries reset every unhandled failure.",
      "tasks": [
        "1.3",
        "2.3"
      ],
      "redTasks": [
        "Add sealed TestVMErrorUnwindReuse in core/vm/vm_terminal_test.go for nested handler/resume, operand/freeze/structural restoration, settlement precedence, and successful later ApplyPooled; extend terminal cases with wrapped-error handler counters. Add public pooled Eval/Call reuse assertions in runtime/apply_pool_test.go under the same fixtures."
      ],
      "codeTasks": [
        "In core/vm/vm.go make the common routing path save frame.ip, settle consumed reductions and pending allocation bytes, give any settlement terminal error precedence, call existing throw restoration only for non-terminal handled errors, reload frame locals, and leave Run/ApplyPooled plus runtime release paths responsible for unhandled cleanup. Preserve handler snapshots from OpSetupTry and vm-local-stack-scope frame bases."
      ],
      "contract": {
        "states": [
          "current frame ip saved before transfer",
          "selected handler frame is top frame and dispatch locals are reloaded",
          "operand stack length equals handler.stackDepth plus one caught value",
          "freezeStack length equals handler.freezeDepth",
          "structural depth equals handler.structDepth",
          "closure depth decremented for each unwound closure frame",
          "consumed reductions and pending allocation bytes are settled",
          "public pooled VM is clean after unhandled failure"
        ],
        "transitions": [
          {
            "input": "task 1.3/2.3: ordinary error during native argument evaluation under nested closure and try",
            "state": "nearest handler frame, saved resume address, operand snapshot, freeze snapshot, and closure depth restored",
            "effect": "set",
            "evidence": "R1 native argument failure and nested handlers; core/vm/vm.go:63-68,91-96,1161-1192,1318-1319,1966-1991"
          },
          {
            "input": "task 1.3/2.3: error interrupts OpStructEnter before OpStructLeave",
            "state": "structural depth restored to handler snapshot and full configured allowance remains for handler/later evaluation",
            "effect": "set",
            "evidence": "R1 structural-depth restoration; core/vm/vm_test.go:1986-2053; core/vm/vm.go:1277-1293,1318-1319,1976-1991"
          },
          {
            "input": "task 1.3/2.3: catch body executes after transfer",
            "state": "dispatch reload uses handler chunk/code/ip/base/env/caps/truthiness and resumes exactly once",
            "effect": "forced",
            "evidence": "design decision 3; core/vm/vm.go:609-622,1161-1175,1324-1333"
          },
          {
            "input": "task 1.3/2.3: pending reductions/allocation exist when ordinary opcode fails",
            "state": "both charges settle before handler or host observes failure; successful settlement preserves original error",
            "effect": "clear",
            "evidence": "design decision 4; core/vm/vm.go:406-432,919-940; core/vm/crossval_test.go:558-638"
          },
          {
            "input": "task 1.3/2.3: settlement raises ResourceLimitError while an ordinary error or explicit throw is pending",
            "state": "core.CodeResourceLimit terminal error replaces pending error and bypasses handler",
            "effect": "forced",
            "evidence": "design decision 4 terminal precedence; core/vm/vm.go:924-939; core/builtin_budget_test.go:323-379"
          },
          {
            "input": "task 1.3/2.3: wrapped cancellation/deadline or resource limit occurs under active handler",
            "state": "handler side-effect count stays 0 and public boundary resets VM state",
            "effect": "forced",
            "evidence": "R1 terminal cannot run handler; core/error.go:32-47; runtime/eval.go:900-1010"
          },
          {
            "input": "task 1.3/2.3: unhandled Eval or Apply failure followed by successful pooled Eval/Apply",
            "state": "later result and available structural allowance equal a fresh engine/VM result",
            "effect": "clear",
            "evidence": "R1 failed evaluation does not poison reuse; core/vm/vm.go:234-324,891-916; runtime/apply_pool_test.go:113-158; runtime/eval.go:523-612,900-1010"
          }
        ],
        "forbidden": [
          "A caught error leaves an aborted frame, operand, native freeze record, or structural-depth increment live.",
          "Handler dispatch continues with stale chunk/code/ip/base/env/caps locals.",
          "An ordinary pending error outranks a terminal error raised while settling prior work.",
          "A terminal path runs catch side effects.",
          "A failed public Eval or Apply returns a dirty VM to a pool or reusable engine slot."
        ],
        "seeding": [
          "Freeze/operand failure: freeze canonical +, then evaluate an argument that raises UndefinedError inside a nested closure; catch rebinds + and invokes it, proving stale freeze and operands are absent.",
          "Nearest handler/resume: nested try forms with distinct marker results, followed by a continuation value; assert only inner handler executes and continuation executes once.",
          "Structural depth: enter a collection expression that fails before OpStructLeave; catch constructs a value exactly at configured MaxStructuralDepth, then run another exact-limit construction.",
          "Settlement: execute two scalar native results to accumulate pending bytes, then raise an ordinary GoFunc error; use EvalMeter snapshot and a ceiling one byte below settled total for terminal-wins row.",
          "Pooled reuse: on one bytecode engine, fail Eval, then Eval 42; define a callable, fail Call/Apply, then Call successfully. Exercise both general vmPool and lean engine-slot release paths.",
          "Terminal absence: catch body calls a counter GoFunc; inject wrapped canceled, wrapped deadline, and core.NewResourceLimitError through call/native paths; require counter 0."
        ],
        "budgets": [
          "Red run: wall timeout 5m; Go test timeout 2m; package workers 2; test parallelism 2.",
          "Resource settlement case uses an exact deterministic allocation ceiling derived from core.MeterScalarBytes; no timing tolerance.",
          "Structural case uses exact configured MaxStructuralDepth and requires the full allowance after unwind/reuse.",
          "Race floor: wall timeout 10m; Go test timeout 2m; package workers 2; test parallelism 2."
        ]
      }
    },
    {
      "id": "documentation",
      "summary": "NO-TESTER-WAIVER: documentation-only seam; behavior is proven by the three sealed tests and existing terminal regressions.",
      "tasks": [
        "3.3"
      ],
      "redTasks": [],
      "codeTasks": [
        "Modify ARCHITECTURE.md and CHANGELOG.md only after behavior is green."
      ],
      "contract": {
        "states": [
          "ARCHITECTURE.md error semantics match runtime contract",
          "CHANGELOG.md [Unreleased] contains one curated Fixed entry"
        ],
        "transitions": [
          {
            "input": "task 3.3: update Error Handling and Terminal errors",
            "state": "docs state ordinary valid-bytecode opcode errors are catchable strings, explicit throws retain values in catches and ThrowError/rendering at host, terminal classes bypass catches",
            "effect": "set",
            "evidence": "ARCHITECTURE.md:559-619,659-680; R1 and R2"
          },
          {
            "input": "task 3.3: update [Unreleased]",
            "state": "Fixed entry describes VM/tree-walker parity and host-visible throw correction",
            "effect": "set",
            "evidence": "CHANGELOG.md:8-18; hand-maintained Keep a Changelog layout"
          }
        ],
        "forbidden": [
          "Claim compile-time or bytecode-validation failures are catchable.",
          "Describe ResourceLimitError, cancellation, or deadlines as catchable.",
          "Document private helper names or implementation mechanics as public API."
        ],
        "seeding": [
          "Use existing ARCHITECTURE.md Error Handling/Error codes/Terminal errors sections.",
          "Use CHANGELOG.md [Unreleased] Fixed heading; no release cut."
        ],
        "budgets": [
          "One concise [Unreleased] Fixed bullet; no new ADR or dependency documentation."
        ]
      }
    },
    {
      "id": "targeted-and-race-floor",
      "summary": "NO-RED-WAIVER: verification-only seam; it records scoped and race evidence after all behavior chunks are green.",
      "tasks": [
        "3.1",
        "3.2"
      ],
      "redTasks": [],
      "codeTasks": [],
      "contract": {
        "states": [
          "core/runtime/goldset slice green",
          "VM/runtime race slice green"
        ],
        "transitions": [
          {
            "input": "task 3.1: scoped service suite",
            "state": "ordinary routing, throw, terminal, metering, reuse, and goldset regressions pass",
            "effect": "set",
            "evidence": "tasks.md 3.1"
          },
          {
            "input": "task 3.2: race slice",
            "state": "shared counters, handlers, and reused VM state are race-free",
            "effect": "set",
            "evidence": "tasks.md 3.2"
          }
        ],
        "forbidden": [
          "Add -count=1 to cache-preserving developer or race commands.",
          "Run race detection in the red inner loop."
        ],
        "seeding": [
          "Run after implementation, docs, targeted red command, per-chunk build/vet/lint, and with no skipped sealed tests."
        ],
        "budgets": [
          "Each command wall timeout 10m; Go test timeout 2m; package workers 2; test parallelism 2."
        ]
      }
    },
    {
      "id": "repository-floor",
      "summary": "NO-RED-WAIVER: verification-only final floor; it validates the complete repository and strict change artifact after targeted evidence is green.",
      "tasks": [
        "3.4"
      ],
      "redTasks": [],
      "codeTasks": [],
      "contract": {
        "states": [
          "repository test floor green",
          "repository lint floor green",
          "strict change validation green"
        ],
        "transitions": [
          {
            "input": "task 3.4: make test with bounded GOTESTFLAGS",
            "state": "complete repository tests pass",
            "effect": "set",
            "evidence": "Makefile:14-17; tasks.md 3.4"
          },
          {
            "input": "task 3.4: make lint",
            "state": "golangci-lint passes",
            "effect": "set",
            "evidence": "Makefile:19-20; tasks.md 3.4"
          },
          {
            "input": "task 3.4: strict validation",
            "state": "vm-runtime-error-parity artifact validates",
            "effect": "set",
            "evidence": "tasks.md 3.4"
          }
        ],
        "forbidden": [
          "Drop timeout, -p 2, or -parallel 2 limits.",
          "Substitute a narrowed suite for make test.",
          "Add -count=1."
        ],
        "seeding": [
          "Run only after all code, tests, and docs are complete."
        ],
        "budgets": [
          "make test wall timeout 10m with Go timeout 2m, package workers 2, test parallelism 2; make lint wall timeout 5m."
        ]
      }
    }
  ],
  "requirements": [
    {
      "shall": "The handler SHALL receive the same message-string value as under the tree-walker.",
      "tests": [
        "TestVMOrdinaryErrorCatchParity"
      ]
    },
    {
      "shall": "Without a handler, the original error class SHALL reach the host. Compile-time",
      "tests": [
        "TestVMOrdinaryErrorCatchParity",
        "TestVMMapLiteral_UnhashableKeyIsTypedEvalError",
        "TestCompiler_MalformedForms",
        "TestChunkValidate_RejectsMalformed"
      ]
    },
    {
      "shall": "Cancellation, deadline expiry and resource-limit errors SHALL retain their",
      "tests": [
        "TestVMOrdinaryErrorCatchParity",
        "TestVMVsTreeWalker_TerminalErrorNotCaught",
        "TestVM_CanceledThroughOpCallNotCaught",
        "TestVM_ResourceLimitInClosureNotCaught",
        "TestVMErrorUnwindReuse"
      ]
    },
    {
      "shall": "- **THEN** both evaluators SHALL return `42`",
      "tests": [
        "TestVMOrdinaryErrorCatchParity"
      ]
    },
    {
      "shall": "- **THEN** both evaluators SHALL return `42` and SHALL NOT create the missing binding",
      "tests": [
        "TestVMOrdinaryErrorCatchParity"
      ]
    },
    {
      "shall": "- **THEN** the outer handler SHALL run with the same caught value and result as the tree-walker",
      "tests": [
        "TestVMOrdinaryErrorCatchParity"
      ]
    },
    {
      "shall": "- **THEN** the handler and later call SHALL use their own operands and callee bindings, with no stale native-call state",
      "tests": [
        "TestVMErrorUnwindReuse",
        "TestVMVsTreeWalker_NativeOpThrowCatchSlotReuse"
      ]
    },
    {
      "shall": "- **THEN** no handler SHALL run and the same terminal error class SHALL reach the host under both evaluators",
      "tests": [
        "TestVMVsTreeWalker_TerminalErrorNotCaught",
        "TestVMOrdinaryErrorCatchParity"
      ]
    },
    {
      "shall": "- **THEN** the later operation SHALL see fresh execution state and the full configured depth allowance",
      "tests": [
        "TestVMErrorUnwindReuse",
        "TestBytecodeRuntime_CallSequenceNoStateLeak"
      ]
    },
    {
      "shall": "An explicit throw caught in Lisp SHALL deliver the original thrown value,",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestEvalThrow_TypedError",
        "TestEval_TryCatch_BindsThrownValue"
      ]
    },
    {
      "shall": "Throwing a value SHALL NOT become a missing-handler type error. A value whose",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestEvalThrow_ErrorLookingStringStaysCatchable"
      ]
    },
    {
      "shall": "- **THEN** the host SHALL recover a typed error with code `ThrowError` and message `boom` under both evaluator modes",
      "tests": [
        "TestVMUncaughtThrowParity"
      ]
    },
    {
      "shall": "- **THEN** both evaluator modes SHALL return the same map value rather than a rendered string",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestVMVsTreeWalker_NonStringThrow"
      ]
    },
    {
      "shall": "- **THEN** both evaluator modes SHALL expose `ThrowError` with equal thrown-value rendering",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestVMVsTreeWalker_NonStringThrow"
      ]
    },
    {
      "shall": "- **THEN** both evaluators SHALL return the thrown string successfully",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestEvalThrow_ErrorLookingStringStaysCatchable"
      ]
    },
    {
      "shall": "An ordinary evaluation error raised while executing valid bytecode SHALL reach",
      "tests": [
        "TestVMOrdinaryErrorCatchParity"
      ]
    },
    {
      "shall": "existing terminal classification and SHALL bypass all handlers. Unwinding SHALL",
      "tests": [
        "TestVMOrdinaryErrorCatchParity",
        "TestVMVsTreeWalker_TerminalErrorNotCaught",
        "TestVM_CanceledThroughOpCallNotCaught",
        "TestVM_ResourceLimitInClosureNotCaught"
      ]
    },
    {
      "shall": "failure SHALL not affect a later evaluation through a reused VM.",
      "tests": [
        "TestVMErrorUnwindReuse",
        "TestBytecodeRuntime_CallSequenceNoStateLeak"
      ]
    },
    {
      "shall": "including its runtime type. An uncaught throw SHALL expose a typed error with",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestEvalThrow_TypedError"
      ]
    },
    {
      "shall": "text resembles a terminal error SHALL remain an ordinary catchable throw.",
      "tests": [
        "TestVMUncaughtThrowParity",
        "TestEvalThrow_ErrorLookingStringStaysCatchable"
      ]
    }
  ],
  "testHarness": [
    "newCrossValEnv — core/vm/crossval_test.go:22 — builds shared cross-evaluation environment with core symbols",
    "newTestEnv — core/eval_test.go:19 — builds pristine core environment for evaluator testing",
    "NewEngine — internal/goldset/goldset.go:39 — builds configured runtime.Engine with Clojure dialect and stdlib for eval or vm mode",
    "terminalTryCallChunk — core/vm/vm_terminal_test.go:12 — builds chunk with OpSetupTry calling a target function",
    "terminalTryClosureCallChunk — core/vm/vm_terminal_test.go:34 — builds chunk with OpSetupTry executing a nested closure call",
    "throwingStringChunk — core/vm/vm_terminal_test.go:167 — builds chunk throwing string \"context deadline exceeded\"",
    "compare — core/vm/crossval_test.go:354 — helper executing a form on both tree-walker and VM and asserting result equality (compareDialect — :382 — same for a dialect renaming)"
  ],
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 1,
    "warnings": 5
  }
}
```
