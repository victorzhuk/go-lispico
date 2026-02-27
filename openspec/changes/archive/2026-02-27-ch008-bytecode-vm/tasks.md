# Tasks: Bytecode VM

**Change ID:** 008-bytecode-vm  
**Status:** Ready for Implementation  
**Created:** 2026-02-24  
**Estimated Effort:** 2-3 weeks  
**Depends On:** Changes 1, 3 (core-engine, runtime-api)

---

## Phase 1: Opcode & Chunk (Days 1-2)

### Task 1.1: Opcode Definitions
- [x] Define `Opcode` type (`uint8`)
- [x] Define all opcode constants (`OpConst` through `OpFalse`)
- [x] Define `opNames` array
- [x] Implement `String()` method
- [x] Write tests for `String()` on all defined opcodes
- **Acceptance**: All opcodes defined, `String()` returns human-readable names

### Task 1.2: Instruction Encoding
- [x] Define `Instruction` type (`uint32`)
- [x] Implement `Encode(op Opcode, a int) Instruction`
- [x] Implement `Op()` method (high byte extraction)
- [x] Implement `A()` method (low 24-bit extraction)
- [x] Implement `String()` method
- [x] Write round-trip tests for `Encode`/`Op`/`A`
- **Acceptance**: Encode/decode round-trips correctly for all opcodes and operand values

### Task 1.3: Chunk Type
- [x] Define `Chunk` struct (`Name`, `Arity`, `Variadic`, `Locals`, `Code`, `Constants`, `SubChunks`)
- [x] Implement `AddConstant` with deduplication
- [x] Implement `Emit(op, a)` returning offset
- [x] Implement `EmitJump(op)` with placeholder operand
- [x] Implement `PatchJump(offset)` for back-patching
- [x] Write tests for constant deduplication and `PatchJump` offset correctness
- **Acceptance**: Chunk emits and patches instructions; duplicate constants are interned

---

## Phase 2: Compiler (Days 3-6)

### Task 2.1: Compiler Scaffold
- [x] Define `Compiler` struct with scope tracking
- [x] Implement local variable resolution
- [x] Implement scope push/pop for `let*` and `fn` bodies
- [x] Implement `Chunk()` accessor
- [x] Write tests for scope tracking
- **Acceptance**: Compiler tracks locals across nested scopes

### Task 2.2: Literal Compilation
- [x] Compile `Nil{}` → `OpNil`
- [x] Compile `Bool{}` → `OpTrue` / `OpFalse`
- [x] Compile `Int{}` → `OpConst`
- [x] Compile `Float{}` → `OpConst`
- [x] Compile `String{}` → `OpConst`
- [x] Compile `Keyword{}` → `OpConst`
- [x] Write per-literal-type tests
- **Acceptance**: Each literal type emits correct opcode(s)

### Task 2.3: Special Forms
- [x] Compile `if` with and without else branch (`OpJumpIfFalse`, `OpJump`)
- [x] Compile `do` (sequence of forms, last is result)
- [x] Compile `let*` (local slot allocation, scoped binding)
- [x] Compile `def` (`OpSetGlobal`)
- [x] Compile `set!` (`OpSetGlobal` / `OpSetLocal`)
- [x] Compile `quote` (`OpConst` with quoted value)
- [x] Write per-form tests
- **Acceptance**: All six special forms compile and produce correct bytecode

### Task 2.4: Function Compilation
- [x] Compile `fn` → sub-chunk creation + `OpClosure`
- [x] Compile `defn` as sugar for `def` + `fn`
- [x] Detect tail position and emit `OpTailCall` instead of `OpCall`
- [x] Handle variadic parameters
- [x] Write tests for fn, defn, tail detection, and variadic
- **Acceptance**: Functions compile to sub-chunks; tail calls emit `OpTailCall`

### Task 2.5: Collection Constructors
- [x] Compile list literals → `OpMakeList`
- [x] Compile vector literals → `OpMakeVector`
- [x] Compile map literals → `OpMakeMap`
- [x] Write tests for each collection type
- **Acceptance**: Collection constructors emit correct opcodes with correct arity

### Task 2.6: Macro Expansion
- [x] Implement `CompileExpanded(expander, form)` method
- [x] Define `MacroExpander` interface
- [x] Ensure `defmacro` forms are evaluated by tree-walker, not compiled
- [x] Write tests for macro expansion before compilation
- **Acceptance**: Macros expand at compile time; expanded forms compile correctly

---

## Phase 3: VM (Days 7-10)

### Task 3.1: VM Scaffold
- [x] Define `VM` struct (`stack`, `frames`, `globals`, `cache`)
- [x] Define `Frame` struct (`chunk`, `ip`, `base`)
- [x] Implement `push`, `pop`, `peek` stack operations
- [x] Implement `New(globals, cache)` constructor
- [x] Write tests for stack operations
- **Acceptance**: VM initializes and stack push/pop/peek work correctly

### Task 3.2: Core Opcodes
- [x] Implement `OpConst` — push from constant pool
- [x] Implement `OpNil`, `OpTrue`, `OpFalse` — push literals
- [x] Implement `OpPop` — discard top
- [x] Implement `OpGetLocal`, `OpSetLocal` — frame-relative access
- [x] Implement `OpGetGlobal`, `OpSetGlobal` — env lookup/mutation
- [x] Write per-opcode tests in isolation
- **Acceptance**: Each core opcode executes correctly

### Task 3.3: Control Flow
- [x] Implement `OpJump` — unconditional IP advance
- [x] Implement `OpJumpIfFalse` — conditional branch
- [x] Wire up `IsTruthy` for condition evaluation
- [x] Write tests for both branch directions
- **Acceptance**: Jumps advance IP correctly; falsy values trigger conditional branch

### Task 3.4: Function Calls
- [x] Implement `OpCall` — push new frame, dispatch to `Closure` or `GoFunc`
- [x] Implement `OpReturn` — pop frame, push result
- [x] Implement `GoFunc` dispatch through `vmEvaluator` adapter
- [x] Write tests for closure calls and GoFunc callbacks
- **Acceptance**: Function calls push/pop frames; GoFuncs receive valid evaluator

### Task 3.5: Tail Call Optimization
- [x] Implement `OpTailCall` — reuse current frame without stack growth
- [x] Verify 1M-iteration tail-recursive loop completes without stack overflow
- [x] Write tests for tail call frame reuse
- **Acceptance**: Tail calls reuse frame; 1M iterations succeed

### Task 3.6: Closures
- [x] Implement `OpClosure` — create `Closure` from `SubChunks[A]` + current env
- [x] Add `Closure` type to `core/types.go` (implements `Value` interface)
- [x] Implement environment capture at closure creation
- [x] Write tests for closure capture and nested closures
- **Acceptance**: Closures capture lexical environment; nested closures resolve correctly

---

## Phase 4: Cache & Integration (Days 11-13)

### Task 4.1: BytecodeCache
- [x] Define `BytecodeCache` struct with `dir` field
- [x] Implement `New(dir)` constructor with `os.MkdirAll`
- [x] Implement `Load(path, content)` — SHA256 key, gob decode, version check
- [x] Implement `Store(path, content, chunks)` — async gob encode
- [x] Define `cacheVersion` constant
- [x] Write tests for cache hit, miss, and version mismatch
- **Acceptance**: Cache loads/stores correctly; version mismatch triggers fallback

### Task 4.2: Closure Type in Core
- [x] Add `Closure` struct to `core/types.go`
- [x] Implement `Type()`, `String()`, `Equals()` for `Closure`
- [x] Implement `NewClosure(chunk, env)` constructor
- [x] Write tests for Value interface compliance
- **Acceptance**: `Closure` satisfies `Value` interface; equality is identity-based

### Task 4.3: vmEvaluator Adapter
- [x] Define `vmEvaluator` struct wrapping `*VM`
- [x] Implement `Eval(ctx, form, env)` — compile form, emit `OpReturn`, run
- [x] Wire adapter into `VM.call` for GoFunc dispatch
- [x] Write tests for GoFunc calling back into VM evaluator
- **Acceptance**: GoFuncs work unchanged under VM execution

### Task 4.4: Runtime Options
- [x] Add `WithBytecode()` engine option
- [x] Add `WithBytecodeCache(dir)` engine option
- [x] Integrate VM selection in `Engine.New` constructor
- [x] Wire cache into hot-reload path (`reloadFile`)
- [x] Write tests for option selection and reload with cache
- **Acceptance**: `WithBytecode()` selects VM; cache integrates with hot-reload

---

## Phase 5: Testing & Benchmarks (Days 14-15)

### Task 5.1: Unit Tests
- [x] Opcode `String()` for all opcodes
- [x] Instruction `Encode`/`Op`/`A` round-trip
- [x] Chunk `AddConstant` deduplication
- [x] Chunk `PatchJump` offset correctness
- [x] Compiler per-form tests (each literal, each special form)
- [x] VM per-opcode tests (each opcode in isolation)
- **Acceptance**: All unit tests pass; no opcode or form untested

### Task 5.2: Integration Tests
- [x] Compile + run: `(+ 1 2)` → `Int{V:3}`
- [x] Recursive fibonacci via `OpTailCall`
- [x] Closure creation and invocation
- [x] GoFunc callback through `vmEvaluator`
- [x] Cache hit: second load uses cached bytecode
- [x] Cache miss: version mismatch triggers recompile
- [x] Context cancellation halts VM run loop
- **Acceptance**: All integration scenarios pass end-to-end

### Task 5.3: Cross-validation
- [x] All ch001 core-engine tests pass with VM backend
- [x] All ch002 stdlib tests pass with VM backend
- [x] Property test: compile+run result equals tree-walker result for same form
- **Acceptance**: VM produces identical results to tree-walker on all existing tests

### Task 5.4: Benchmarks
- [x] Arithmetic loop (10k iterations): ≥ 10x speedup vs tree-walker
- [x] Function call overhead: ≥ 10x speedup
- [x] `map` over large list: ≥ 10x speedup
- [x] File load (cached): ≤ 1ms for 1000-line file
- [x] File load (uncached): ≤ neutral overhead vs tree-walker
- **Acceptance**: All performance targets met; benchmark results documented

---

## Acceptance Criteria

- [x] All 19 special forms compile and execute correctly
- [x] TCO: 1M-iteration loop without stack overflow
- [x] Closure capture matches tree-walker semantics
- [x] GoFunc calls work from the VM hot loop
- [x] Cache hit avoids recompile
- [x] Cache miss triggers compile + async store
- [x] `WithBytecode()` enables VM; default execution path remains tree-walker
- [x] VM implements `Evaluator` interface
- [x] Benchmark shows ≥ 10x speedup on arithmetic loops
- [x] All ch001/ch002 tests pass with VM backend
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 003-runtime-api (required)

---

*Tasks generated from proposal.md and design.md for change 008-bytecode-vm.*
