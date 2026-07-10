# Tasks — vm-first-staged

## 1. Baseline

- [x] 1.1 Record current benchmarks (one-shot eval, file load, arithmetic loop, fib) with `-benchmem` into the change dir for stage-by-stage comparison

## 2. Stage 1: native opcodes

- [x] 2.1 Add arithmetic/comparison opcodes (`OpAdd`, `OpSub`, `OpMul`, `OpDiv`, `OpLt`, `OpGt`, `OpLe`, `OpGe`, `OpEq`) with stdlib promotion semantics and division-by-zero errors
- [x] 2.2 Compiler emits opcodes only when the operator is not locally shadowed; opcode falls back to call path when the global binding is not the canonical builtin (pointer identity)
- [x] 2.3 Extend parity corpus: promotion, division-by-zero, comparison chains, rebound `+`
- [x] 2.4 Benchmarks: arithmetic loop allocs/iter and ns/iter vs baseline

## 3. Stage 1: slot-resident locals

- [x] 3.1 Compiler capture analysis post-macro-expansion; conservative full-env fallback for unanalyzable frames
- [x] 3.2 Uncaptured locals: slot-only, drop the `Env` mirror in `OpSetLocal`/call setup
- [x] 3.3 Parity tests: closures capturing loop vars, escaping closures, nested `fn`
- [x] 3.4 Benchmarks: bytes/call on `BenchmarkSimpleArithmetic_VM` vs tree-walker

## 4. Stage 2: dialect axes

- [x] 4.1 Normalization step: resolved dialect's visible→canonical table rewrites head symbols before compilation; removed forms rejected at resolution
- [x] 4.2 VM consumes truthiness predicate in conditional opcodes
- [x] 4.3 Lisp-2: head-position resolution via function cell (`OpGetFunc` or equivalent)
- [x] 4.4 Remove the `IsIdentity()` gate in `runtime.New`; update `TestRuntime_DefaultCL_RejectsBytecode` to assert success
- [x] 4.5 Parity corpus runs under CL default, clojure, and a fail-closed restricted dialect with `WithBytecode()`
- [x] 4.6 Update ADR 0005 consequence 4 wording if it drifted; note delivery in ADR 0006
## 5. Stage 3: chunk cache

- [x] 5.1 Macro epoch counter bumped on `defmacro` at the root env
- [x] 5.2 Per-Engine chunk cache keyed by (source hash, dialect fingerprint, macro epoch), mutex-guarded
- [x] 5.3 VM reset/reuse in `runtime/eval.go`; stacks/frames cleared between runs; concurrent `Eval` gets separate instances (`-race` test)
- [x] 5.4 Tests: cache hit skips recompile, macro redefinition invalidates, results isolated across cached runs
- [x] 5.5 Benchmarks: repeated file load VM vs tree-walker — the ADR 0002/0006 end-to-end comparison

## 6. Close out

- [x] 6.1 `go build ./... && go test ./... -race && golangci-lint run` clean
- [x] 6.2 Record final stage benchmarks; state whether the default-flip condition (VM wins end-to-end incl. one-shot) is met — flip itself stays a separate change
- [x] 6.3 Update CONTEXT.md VM glossary entry to match the new reality (cached chunks, all-dialect eligibility, still opt-in)
- [x] 6.4 `openspec validate vm-first-staged` passes
