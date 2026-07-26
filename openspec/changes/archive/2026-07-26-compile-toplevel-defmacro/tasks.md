## 1. Find the real boundary before widening it

- [x] 1.1 Establish why `defmacro` was refused at all, rather than assuming the
      refusal was arbitrary. Compiling it everywhere broke
      `TestBytecodeRuntime_NestedDefmacroFallsBackToTreeWalker` and
      `TestDialect_Lisp2_MacroDispatchesFromFunctionCell`, both
      `TypeError: expected callable, got core.Macro`, both of the shape
      `(do (defmacro m …) (m …))`.
- [x] 1.2 Diagnose: macro expansion is a pre-pass over the whole form, so a
      macro defined inside a form is unbound when a sibling use is expanded.
      The hazard is exactly the nested case — which is what the documentation
      said before `correct-fallback-scope` recorded the over-broad
      implementation.
- [x] 1.3 Confirm the criterion is sound rather than convenient: at
      `compileDepth == 1` the `defmacro` *is* the form, so no sibling exists to
      mis-expand. Confirm one-form-per-chunk holds in production —
      `EvalCached` compiles and runs a single form at a time, and `CompileAll`
      has no production callers.

## 2. Share the bind path

- [x] 2.1 Extract `core.BindMacro`: identical-rebind check, bind through the
      dialect's cell, conditional epoch bump.
- [x] 2.2 `evalDefmacro` delegates to it; verified behaviour-neutral before any
      opcode existed (990 tests in `core` and `runtime`).
- [x] 2.3 This is the load-bearing decision, not tidiness: a compiled
      `defmacro` that bumped the epoch unconditionally would evict its own
      chunk every evaluation and the change would cancel itself out. Sharing
      the path means the VM inherits the rule instead of re-deriving it.

## 3. Opcodes, with their validation

- [x] 3.1 `OpDefMacro` and `OpDefMacroFunc` appended to the opcode block, so no
      existing opcode value shifts. Two because the VM carries no dialect.
- [x] 3.2 `Chunk.Validate` cases added **in the same step** — constant index in
      range and constant is a `Macro`. This repo has twice shipped an opcode
      with a constant operand and no `Validate` case, which panics on an
      already-validated chunk.
- [x] 3.3 VM exec copies the prototype by value before assigning `Env`, so
      macros cannot share a defining scope; `Macro` being a value type makes
      that structural rather than a discipline.
- [x] 3.4 The opcode pushes the macro: every compiled expression must leave
      exactly one result on the stack, and nothing pre-pushed it.

## 4. Compiler

- [x] 4.1 `compileDefmacro` builds the prototype and emits the cell-appropriate
      opcode; the body travels as data because it is evaluated at expansion
      time, not compiled.
- [x] 4.2 Guard on `compileDepth > 1` returning the typed unsupported error,
      with the WHY comment naming the sibling-use hazard.

## 5. Tests

- [x] 5.1 `TestCompiler_Defmacro_Unsupported` retargeted to
      `TestCompiler_Defmacro_TopLevelCompiles`, now also validating the chunk.
      It initially failed on "chunk does not end in a control-transfer
      instruction" — a test artifact, since the real path calls `EmitReturn`.
- [x] 5.2 `TestUnsupported_NestedDefmacro` pins both sides: top-level compiles,
      nested and `unquote-splicing` return the typed error.
- [x] 5.3 The two runtime tests that caught the over-broad version stay
      unmodified and green — they are the regression guard for the hazard.

## 6. Documentation

- [x] 6.1 The six places `correct-fallback-scope` touched return to naming the
      nested case. Recorded in the proposal that the earlier correction was
      accurate about the code and wrong about the intent, rather than quietly
      reverting it.

## 7. Verify

- [x] 7.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`.
- [x] 7.2 `go test ./... -count=1` 2446 passed; `-race` 2448 passed. One race
      run tripped `TestDecodeHashMap_Scaling` (3.396 against its 3.0 bound);
      confirmed unrelated rather than assumed — it exercises `fromJSONValue`
      directly, never the compiler, and passes 4/4 in isolation under `-race`
      with ratios 1.86–1.98. Third occurrence today of the filed flake.
- [x] 7.3 Crossval `TestVMVsTreeWalker` 218 passed — the shared bind path means
      a divergence here would show the two engines disagreeing on the macro
      bound or on cache invalidation.
- [x] 7.4 `go test ./internal/goldset/ -count=1` green, both modes.
- [x] 7.5 `twice-macro` 57 → 50 allocs/op (−12.28%, p=0.000); every other cell
      `p=1.000`. Report the corrected payoff: the earlier 29-allocation figure
      measured the cost of having the form, not of the fallback, so only part
      of it was ever recoverable.
- [x] 7.6 `cmd/perfgate`: 22 PASS, 4 FAIL, none a regression. Allocations move
      only on `twice-macro`; every other cell is `p=1.000`. Two FAILs are this
      change's own improvement (`twice-macro` −14.18%) and noise
      (`GoldsetParse/text-render` −7.58%). `registry-fold` at **+23.44%** was
      far outside the usual noise band and was investigated rather than
      dismissed: a paired branch-against-master capture at `-count=12` returned
      `~ (p=0.242)`, with `route-decision` `~ (p=0.066)` and `twice-macro`
      −6.77% (p=0.012). The gate's figure came from comparing two separately
      captured files — the fifth time today a gate FAIL has evaporated under
      paired measurement.
