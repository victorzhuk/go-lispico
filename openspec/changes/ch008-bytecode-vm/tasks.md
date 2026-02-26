# Tasks: Bytecode VM

**Change ID:** 008-bytecode-vm  
**Status:** Ready for Implementation  
**Created:** 2026-02-24  
**Estimated Effort:** 2-3 weeks  
**Depends On:** Changes 1, 3 (core-engine, runtime-api)

---

## Phase 1: Opcode & Chunk (Days 1-2)

### Task 1.1: Opcode Definitions
- [ ] Define `Opcode` type (`uint8`)
- [ ] Define all opcode constants (`OpConst` through `OpFalse`)
- [ ] Define `opNames` array
- [ ] Implement `String()` method
- [ ] Write tests for `String()` on all defined opcodes
- **Acceptance**: All opcodes defined, `String()` returns human-readable names

### Task 1.2: Instruction Encoding
- [ ] Define `Instruction` type (`uint32`)
- [ ] Implement `Encode(op Opcode, a int) Instruction`
- [ ] Implement `Op()` method (high byte extraction)
- [ ] Implement `A()` method (low 24-bit extraction)
- [ ] Implement `String()` method
- [ ] Write round-trip tests for `Encode`/`Op`/`A`
- **Acceptance**: Encode/decode round-trips correctly for all opcodes and operand values

### Task 1.3: Chunk Type
- [ ] Define `Chunk` struct (`Name`, `Arity`, `Variadic`, `Locals`, `Code`, `Constants`, `SubChunks`)
- [ ] Implement `AddConstant` with deduplication
- [ ] Implement `Emit(op, a)` returning offset
- [ ] Implement `EmitJump(op)` with placeholder operand
- [ ] Implement `PatchJump(offset)` for back-patching
- [ ] Write tests for constant deduplication and `PatchJump` offset correctness
- **Acceptance**: Chunk emits and patches instructions; duplicate constants are interned

---

## Phase 2: Compiler (Days 3-6)

### Task 2.1: Compiler Scaffold
- [ ] Define `Compiler` struct with scope tracking
- [ ] Implement local variable resolution
- [ ] Implement scope push/pop for `let*` and `fn` bodies
- [ ] Implement `Chunk()` accessor
- [ ] Write tests for scope tracking
- **Acceptance**: Compiler tracks locals across nested scopes

### Task 2.2: Literal Compilation
- [ ] Compile `Nil{}` → `OpNil`
- [ ] Compile `Bool{}` → `OpTrue` / `OpFalse`
- [ ] Compile `Int{}` → `OpConst`
- [ ] Compile `Float{}` → `OpConst`
- [ ] Compile `String{}` → `OpConst`
- [ ] Compile `Keyword{}` → `OpConst`
- [ ] Write per-literal-type tests
- **Acceptance**: Each literal type emits correct opcode(s)

### Task 2.3: Special Forms
- [ ] Compile `if` with and without else branch (`OpJumpIfFalse`, `OpJump`)
- [ ] Compile `do` (sequence of forms, last is result)
- [ ] Compile `let*` (local slot allocation, scoped binding)
- [ ] Compile `def` (`OpSetGlobal`)
- [ ] Compile `set!` (`OpSetGlobal` / `OpSetLocal`)
- [ ] Compile `quote` (`OpConst` with quoted value)
- [ ] Write per-form tests
- **Acceptance**: All six special forms compile and produce correct bytecode

### Task 2.4: Function Compilation
- [ ] Compile `fn` → sub-chunk creation + `OpClosure`
- [ ] Compile `defn` as sugar for `def` + `fn`
- [ ] Detect tail position and emit `OpTailCall` instead of `OpCall`
- [ ] Handle variadic parameters
- [ ] Write tests for fn, defn, tail detection, and variadic
- **Acceptance**: Functions compile to sub-chunks; tail calls emit `OpTailCall`

### Task 2.5: Collection Constructors
- [ ] Compile list literals → `OpMakeList`
- [ ] Compile vector literals → `OpMakeVector`
- [ ] Compile map literals → `OpMakeMap`
- [ ] Write tests for each collection type
- **Acceptance**: Collection constructors emit correct opcodes with correct arity

### Task 2.6: Macro Expansion
- [ ] Implement `CompileExpanded(expander, form)` method
- [ ] Define `MacroExpander` interface
- [ ] Ensure `defmacro` forms are evaluated by tree-walker, not compiled
- [ ] Write tests for macro expansion before compilation
- **Acceptance**: Macros expand at compile time; expanded forms compile correctly

---

## Phase 3: VM (Days 7-10)

### Task 3.1: VM Scaffold
- [ ] Define `VM` struct (`stack`, `frames`, `globals`, `cache`)
- [ ] Define `Frame` struct (`chunk`, `ip`, `base`)
- [ ] Implement `push`, `pop`, `peek` stack operations
- [ ] Implement `New(globals, cache)` constructor
- [ ] Write tests for stack operations
- **Acceptance**: VM initializes and stack push/pop/peek work correctly

### Task 3.2: Core Opcodes
- [ ] Implement `OpConst` — push from constant pool
- [ ] Implement `OpNil`, `OpTrue`, `OpFalse` — push literals
- [ ] Implement `OpPop` — discard top
- [ ] Implement `OpGetLocal`, `OpSetLocal` — frame-relative access
- [ ] Implement `OpGetGlobal`, `OpSetGlobal` — env lookup/mutation
- [ ] Write per-opcode tests in isolation
- **Acceptance**: Each core opcode executes correctly

### Task 3.3: Control Flow
- [ ] Implement `OpJump` — unconditional IP advance
- [ ] Implement `OpJumpIfFalse` — conditional branch
- [ ] Wire up `IsTruthy` for condition evaluation
- [ ] Write tests for both branch directions
- **Acceptance**: Jumps advance IP correctly; falsy values trigger conditional branch

### Task 3.4: Function Calls
- [ ] Implement `OpCall` — push new frame, dispatch to `Closure` or `GoFunc`
- [ ] Implement `OpReturn` — pop frame, push result
- [ ] Implement `GoFunc` dispatch through `vmEvaluator` adapter
- [ ] Write tests for closure calls and GoFunc callbacks
- **Acceptance**: Function calls push/pop frames; GoFuncs receive valid evaluator

### Task 3.5: Tail Call Optimization
- [ ] Implement `OpTailCall` — reuse current frame without stack growth
- [ ] Verify 1M-iteration tail-recursive loop completes without stack overflow
- [ ] Write tests for tail call frame reuse
- **Acceptance**: Tail calls reuse frame; 1M iterations succeed

### Task 3.6: Closures
- [ ] Implement `OpClosure` — create `Closure` from `SubChunks[A]` + current env
- [ ] Add `Closure` type to `core/types.go` (implements `Value` interface)
- [ ] Implement environment capture at closure creation
- [ ] Write tests for closure capture and nested closures
- **Acceptance**: Closures capture lexical environment; nested closures resolve correctly

---

## Phase 4: Cache & Integration (Days 11-13)

### Task 4.1: BytecodeCache
- [ ] Define `BytecodeCache` struct with `dir` field
- [ ] Implement `New(dir)` constructor with `os.MkdirAll`
- [ ] Implement `Load(path, content)` — SHA256 key, gob decode, version check
- [ ] Implement `Store(path, content, chunks)` — async gob encode
- [ ] Define `cacheVersion` constant
- [ ] Write tests for cache hit, miss, and version mismatch
- **Acceptance**: Cache loads/stores correctly; version mismatch triggers fallback

### Task 4.2: Closure Type in Core
- [ ] Add `Closure` struct to `core/types.go`
- [ ] Implement `Type()`, `String()`, `Equals()` for `Closure`
- [ ] Implement `NewClosure(chunk, env)` constructor
- [ ] Write tests for Value interface compliance
- **Acceptance**: `Closure` satisfies `Value` interface; equality is identity-based

### Task 4.3: vmEvaluator Adapter
- [ ] Define `vmEvaluator` struct wrapping `*VM`
- [ ] Implement `Eval(ctx, form, env)` — compile form, emit `OpReturn`, run
- [ ] Wire adapter into `VM.call` for GoFunc dispatch
- [ ] Write tests for GoFunc calling back into VM evaluator
- **Acceptance**: GoFuncs work unchanged under VM execution

### Task 4.4: Runtime Options
- [ ] Add `WithBytecode()` engine option
- [ ] Add `WithBytecodeCache(dir)` engine option
- [ ] Integrate VM selection in `Engine.New` constructor
- [ ] Wire cache into hot-reload path (`reloadFile`)
- [ ] Write tests for option selection and reload with cache
- **Acceptance**: `WithBytecode()` selects VM; cache integrates with hot-reload

---

## Phase 5: Testing & Benchmarks (Days 14-15)

### Task 5.1: Unit Tests
- [ ] Opcode `String()` for all opcodes
- [ ] Instruction `Encode`/`Op`/`A` round-trip
- [ ] Chunk `AddConstant` deduplication
- [ ] Chunk `PatchJump` offset correctness
- [ ] Compiler per-form tests (each literal, each special form)
- [ ] VM per-opcode tests (each opcode in isolation)
- **Acceptance**: All unit tests pass; no opcode or form untested

### Task 5.2: Integration Tests
- [ ] Compile + run: `(+ 1 2)` → `Int{V:3}`
- [ ] Recursive fibonacci via `OpTailCall`
- [ ] Closure creation and invocation
- [ ] GoFunc callback through `vmEvaluator`
- [ ] Cache hit: second load uses cached bytecode
- [ ] Cache miss: version mismatch triggers recompile
- [ ] Context cancellation halts VM run loop
- **Acceptance**: All integration scenarios pass end-to-end

### Task 5.3: Cross-validation
- [ ] All ch001 core-engine tests pass with VM backend
- [ ] All ch002 stdlib tests pass with VM backend
- [ ] Property test: compile+run result equals tree-walker result for same form
- **Acceptance**: VM produces identical results to tree-walker on all existing tests

### Task 5.4: Benchmarks
- [ ] Arithmetic loop (10k iterations): ≥ 10x speedup vs tree-walker
- [ ] Function call overhead: ≥ 10x speedup
- [ ] `map` over large list: ≥ 10x speedup
- [ ] File load (cached): ≤ 1ms for 1000-line file
- [ ] File load (uncached): ≤ neutral overhead vs tree-walker
- **Acceptance**: All performance targets met; benchmark results documented

---

## Acceptance Criteria

- [ ] All 19 special forms compile and execute correctly
- [ ] TCO: 1M-iteration loop without stack overflow
- [ ] Closure capture matches tree-walker semantics
- [ ] GoFunc calls work from the VM hot loop
- [ ] Cache hit avoids recompile
- [ ] Cache miss triggers compile + async store
- [ ] `WithBytecode()` enables VM; default execution path remains tree-walker
- [ ] VM implements `Evaluator` interface
- [ ] Benchmark shows ≥ 10x speedup on arithmetic loops
- [ ] All ch001/ch002 tests pass with VM backend
- [ ] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 003-runtime-api (required)

---

*Tasks generated from proposal.md and design.md for change 008-bytecode-vm.*
