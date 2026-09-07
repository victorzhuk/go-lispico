## 0. Scope and baseline

- [x] 0.1 Confirm existing-service-strict scope against `design.md`, current frame/capture code and both binding-shape specs; record that sequential `let`/`let*` and enclosing-scope loop initialization remain the reference behavior.
- [x] 0.2 Review current `ARCHITECTURE.md`, `CHANGELOG.md` and archived `align-clojure-dialect-surface` decisions; verify the two MODIFIED requirement blocks preserve every unaffected scenario before implementation.

## 1. Regressions before implementation

- [x] 1.1 Add new compiler/VM tests `TestVMLocalOperandParity` and `TestVMLoopScopeParity` covering the exact vector, native-call, ordinary-call and three loop repros in the delta; assert pinned expected results and confirm failures under the current VM while tree-walker controls pass.
- [x] 1.2 Add new runtime test `TestRuntimeLocalScopeParity` for cold/repeated `Eval` and closure application, using Clojure vectors and CL list-pair bindings; verify expected values through both evaluator modes, including sequential `let`/`let*` controls.
- [x] 1.3 Add new VM test `TestVMLoopOperandBound` with a fixed loop body containing nested binding scopes; measure live operand height at deterministic iteration counts and verify the current implementation accumulates discarded operands before the fix. Include nested recur targets, simultaneous replacement values and escaping captures in the regression corpus.

Targeted command for this phase, with no package-selecting project wrapper:

```sh
timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core/compiler ./core/vm ./runtime -run 'Test(VMLocalOperandParity|VMLoopScopeParity|RuntimeLocalScopeParity|VMLoopOperandBound)$'
```

## 2. Frame storage and lexical lifetime

- [x] 2.1 Reserve frame-local storage separately from temporary operands at `Run`, `apply` and `call`, including variadic/captured parameters; verify the call and vector regressions pass without per-call environment mirroring.
- [x] 2.2 Make initializer stores consume their operand while preserving the value of `set!`; update `emitBind`, capture rewriting, `computeMaxStack` and chunk validation together, then verify compiler validation plus existing captured-variable and native-fusion tests pass.
- [x] 2.3 Restore loop compiler scope after the body and compile loop initializers against the enclosing scope; verify every `TestVMLoopScopeParity` case passes while sequential `let`/`let*` controls remain unchanged.
- [x] 2.4 Restore loop-entry operand height on `recur` after evaluating replacement arguments; verify `TestVMLoopOperandBound`, nested-loop targets, simultaneous replacement and per-iteration capture tests pass without increasing live operand height with iteration count.

## 3. Integration and documentation

- [x] 3.1 Run `timeout 10m go test -timeout 2m -p 2 -parallel 2 ./core/... ./runtime ./internal/goldset`; record command, test names and results, including existing gold-set correctness and allocation checks. Do not weaken assertions or limits to accommodate a regression.
- [x] 3.2 Run `timeout 10m go test -race -timeout 2m -p 2 -parallel 2 ./core/vm ./runtime`; verify captures, re-entry and pooled execution remain race-free and isolate subsequent calls.
- [x] 3.4 Run `timeout 10m make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'` and `timeout 5m make lint`; record results and classify any unrelated workspace failures without changing test discovery in this change.
- [x] 3.5 Run `openspec validate vm-local-stack-scope --strict --json`, then archive only after implementation checks pass; verify the archived sequential-binding requirements before either successor starts implementation.
