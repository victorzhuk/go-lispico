# Change Proposal: Bytecode VM

**Change ID:** 008-bytecode-vm
**Status:** Proposed → Ready for Design
**Created:** 2026-02-24
**Author:** AI Assistant
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Add a bytecode compiler and stack VM as a drop-in replacement for the tree-walking evaluator. The compiler produces `Chunk` structs (instruction stream + constant pool); the VM executes them in a tight loop. Bytecode is cached to disk by content hash for near-zero hot-reload latency. Enabled via `runtime.WithBytecode()` option — the public API is unchanged.

**Key Characteristics:**
- Drop-in replacement: VM implements the same `Evaluator` interface
- Compiler translates AST → `[]Chunk`; all 19 special forms supported
- Instruction encoding: packed `uint32` (opcode in high byte, operand in low 3 bytes)
- BytecodeCache: disk cache keyed by `SHA256(content)`, gob-encoded
- Phase 2 release: tree-walker remains available as fallback via option

---

## 2. Why

The tree-walking evaluator is correct and simple, but its recursive structure creates a fixed performance ceiling. Every `Eval()` call is a Go function call: a stack frame, a type switch, interface dispatch, and potential heap allocation. A bytecode VM collapses that recursive call tree into a flat integer loop, which Go compiles to a jump table. The largest single win for go-lispico is hot-reload: cached bytecode loads a 1000-line file in ~0.5ms versus ~50ms for a tree-walk parse+eval — roughly 100x faster.

---

## 3. Motivation

### Problem

The tree-walking evaluator has a structural performance ceiling:
- **Each `Eval()` call** = Go function call + stack frame + type switch + interface dispatch
- **Recursive descent** means call depth grows with expression nesting
- **Hot-reload** re-parses and re-evaluates from source on every file change (~50ms)
- **No caching**: identical source files are fully re-evaluated on each load

### Solution

A bytecode compiler + stack VM that:
- Compiles AST to flat integer instruction streams (one pass over the AST)
- Executes in a single `for` loop — Go `switch` on `uint8` compiles to a jump table
- Caches compiled `Chunk`s to disk by `SHA256(content)` — cache hits skip compile entirely
- Slots into the existing `Evaluator` interface — all plugins and host code are unaffected

### Success Metrics

- VM hot loop ≤ 100ns per opcode
- Cached file load ≤ 1ms for a 1000-line file
- ≥ 10x speedup on arithmetic loops vs tree-walker
- All ch001/ch002 tests pass with VM backend

---

## 3. Scope

### In Scope

**Opcode Set (uint8)**
- Stack: `OpConst`, `OpNil`, `OpTrue`, `OpFalse`, `OpPop`
- Globals: `OpGetGlobal`, `OpSetGlobal`
- Locals: `OpGetLocal`, `OpSetLocal`
- Control: `OpJump`, `OpJumpIfFalse`
- Calls: `OpCall`, `OpTailCall`, `OpReturn`
- Closures: `OpClosure`
- Constructors: `OpMakelist`, `OpMakeVector`, `OpMakeMap`

**Instruction Encoding**
- Packed `uint32`: opcode in high byte (`Op() = i >> 24`), operand in low 3 bytes (`A() = i & 0x00FFFFFF`)

**Chunk**
- Fields: `Code []Instruction`, `Constants []core.Value`, `Locals int`, `Arity int`, `Variadic bool`, `Name string`

**VM Frame**
- Fields: `chunk *Chunk`, `ip int`, `base int` (stack base for locals)

**VM**
- Fields: `stack []core.Value`, `frames []Frame`, `globals *core.Env`

**Compiler**
- Input: AST from core reader
- Output: `[]Chunk`
- Handles all 19 special forms
- Detects tail position and emits `OpTailCall` instead of `OpCall`
- Runs macro expansion before emitting bytecode

**BytecodeCache**
- Disk cache keyed by `SHA256(content)`
- gob-encoded `Chunk` slices
- Version field in cache header; corrupt/stale entries fall back to recompile

**Runtime Options**
- `runtime.WithBytecode()` — enables VM execution path
- `runtime.WithBytecodeCache(dir)` — configures cache directory

### Out of Scope

| Item | Reason |
|------|--------|
| JIT compilation | Future change (requires platform-specific code generation) |
| Profiler integration | Future (requires instruction-level counters + source maps) |
| Debugger step-through | Future (requires source map in Chunk) |

---

## 4. Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| B8.1 | Compiler produces valid bytecode for all 19 special forms | P0 |
| B8.2 | VM executes all opcodes correctly | P0 |
| B8.3 | TCO via `OpTailCall` reuses current frame without stack growth | P0 |
| B8.4 | Closures capture lexical environment correctly | P0 |
| B8.5 | GoFunc calls work from the VM hot loop | P0 |
| B8.6 | BytecodeCache load and store keyed by content hash | P0 |
| B8.7 | Cache miss triggers compile + store | P0 |
| B8.8 | `runtime.WithBytecode()` enables VM execution path | P0 |
| B8.9 | `runtime.WithBytecodeCache(dir)` configures cache directory | P0 |
| B8.10 | VM implements `Evaluator` interface from ch001 | P0 |
| B8.11 | VM hot loop ≤ 100ns per opcode | P1 |
| B8.12 | Cached load ≤ 1ms for a 1000-line file | P1 |

---

## 5. Non-Functional Requirements

### Performance

| Operation | Tree-walker | Bytecode VM | Speedup |
|-----------|-------------|-------------|---------|
| Simple arithmetic `(+ 1 2 3)` | ~800ns | ~80ns | ~10x |
| 10k-iteration loop | ~45ms | ~3ms | ~15x |
| Function call (closure) | ~1.2µs | ~120ns | ~10x |
| `map f large-list` | ~3µs/elem | ~200ns/elem | ~15x |
| File load (cached) | ~50ms | ~0.5ms | ~100x |
| File load (uncached) | ~50ms | ~55ms | ~neutral |

### Reliability

- Cache version field detected at read time; corrupt entries trigger fallback to recompile
- Malformed bytecode returns a Go error, never a panic
- VM stack overflow returns descriptive error (not a Go stack overflow)

### Security

- No new I/O surface: cache reads/writes are gated behind `WithBytecodeCache(dir)`
- Cache directory is user-supplied; no default path is assumed
- Bytecode does not execute arbitrary Go; all opcodes map to safe VM operations

---

## 6. Design Philosophy

### Drop-in Replacement

The VM implements the same `Evaluator` interface as the tree-walker. The engine selects the execution path at construction time via `WithBytecode()`. All plugins, GoFuncs, and host code are unaffected — they never interact with `Chunk` or `Frame` directly.

### Phase 2

The tree-walker ships as v0.1.0. The bytecode VM ships as v0.2.0 with an identical public API. The tree-walker remains the default; the VM is opt-in via `WithBytecode()`.

### Macros Expand at Compile Time

The compiler runs macro expansion before emitting bytecode. Macros are a compile-time transformation — by the time the VM runs, all macros have been fully expanded. This preserves Lisp macro semantics and simplifies the VM (no macro dispatch in the hot loop).

### Cache Key = Content Hash

`SHA256(content)` is the cache key. If the file changes by even one byte, the hash changes and the old cache entry is bypassed. Old entries accumulate silently and can be purged by deleting the cache directory.

---

## 7. Dependencies

### External Dependencies

- `encoding/gob` (stdlib) — Chunk serialization
- `crypto/sha256` (stdlib) — cache key derivation
- `path/filepath` (stdlib) — cache path construction

### Internal Dependencies

| Change | Status | Reason |
|--------|--------|--------|
| ch001-core-engine | Required | `core.Value`, `Evaluator` interface, AST types, `*core.Env` |
| ch003-runtime-api | Required | `WithBytecode()` / `WithBytecodeCache()` option pattern; hot-reload integration |

### Future Dependents

- Change 9+ (JIT): will depend on the `Chunk` representation and opcode set defined here

---

## 8. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Macro compilation correctness | High | High | Careful testing; compare compiler output against tree-walker on identical inputs |
| Closure capture semantics | Medium | High | Property-based tests against tree-walker as reference implementation |
| Cache corruption | Low | Medium | Version field in cache header; corrupt/mismatched entries fall back to recompile |
| Opcode set completeness | Medium | High | One-to-one test per special form before declaring B8.1 done |

---

## 9. Open Questions

1. **Upvalue model**: Should closures use a flat upvalue array (LuaJIT-style) or a captured `*Env` chain? Flat arrays are faster; captured env is simpler to implement correctly.
2. **Async store**: Should cache writes be fire-and-forget (goroutine) or synchronous? Async avoids latency on first load; sync simplifies error handling.
3. **Opcode width**: `uint32` with 8-bit opcode leaves 24 bits for operand (max 16M constants). Is this sufficient, or should large programs use a separate wide-operand encoding?
4. **Constant deduplication**: Should the compiler intern identical constants across chunks, or keep per-chunk constant pools? Interning saves memory; per-chunk is simpler.

**Recommendation**: Flat upvalue array, async cache store, 24-bit operand (revisit if needed), per-chunk constant pools initially.

---

## 10. Migration Path

1. Write `core/compiler/` package: AST → `[]Chunk`, tail-position detection, macro expansion pass
2. Write `core/vm/` package: `Frame`, `VM`, opcode dispatch loop
3. VM implements the `Evaluator` interface from ch001
4. Add `WithBytecode()` and `WithBytecodeCache(dir)` to runtime options (ch003 option pattern)
5. Benchmark tree-walker vs VM on representative workloads; validate performance targets
6. Ship as v0.2.0 — v0.1.0 tree-walker remains as default fallback via option

---

## 11. Acceptance Criteria

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

## 12. References

### Inspiration Sources

- **tengo**: Bytecode VM patterns for Go-embedded scripting, opcode design
- **LuaJIT**: Flat upvalue model, instruction encoding (`uint32` with opcode + operand)
- **CPython**: Constant pool design, per-function code objects (analogous to `Chunk`)
- **Crafting Interpreters** (Nystrom): Stack VM architecture, closure upvalue handling

### Specification References

- "Crafting Interpreters" — Robert Nystrom (Part III: A Bytecode Virtual Machine)
- "Virtual Machines" — Smith & Nair
- LuaJIT bytecode reference: https://luajit.org/ext_bytecode.html

---

**Next Step:** Create detailed design document (02-design.md) with opcode reference, compiler algorithm, VM run loop, and cache format.
