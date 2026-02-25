# Change Proposal: Core Engine Foundation

**Change ID:** 001-core-engine  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the zero-dependency Lisp interpreter kernel: types, reader, evaluator, environment, and plugin interface. The core is intentionally minimal (~900 lines target), implementing only McCarthy's seven primitives and essential special forms. All I/O, networking, and domain logic lives in separate plugins.

**Key Characteristics:**
- Pure Go, zero external dependencies in `go.mod`
- No I/O operations (filesystem, network, randomness)
- Deterministic: same input + env → same output
- Thread-safe environment with RWMutex
- Tail-call optimization (TCO) for recursion
- Macro expansion before evaluation

---

## 2. Why

This change establishes the foundation of the go-lispico interpreter. The core engine provides the essential Lisp evaluation capabilities that all other features depend on. Without this foundation, plugins cannot function, scripts cannot run, and the system has no purpose.

---

## 3. Motivation

### Problem
Existing Go scripting engines have critical limitations:
- **glisp**: 11 years stale, no active maintenance
- **joker**: No real Go interop, Clojure-focused
- **zygomys**: Complex, slow compilation, heavy standard library
- **starlark-go**: Python syntax, not Lisp
- **tengo**: Not Lisp, different paradigm

### Solution
A clean-slate Lisp interpreter designed for:
- **Embeddability**: Drop-in Go library with minimal footprint
- **Composability**: Plugin-per-domain model, import only what you need
- **Hot-reload**: Change scripts without restarting Go service
- **Macro-powered DSLs**: Homoiconic configuration and orchestration

### Success Metrics
- Core compiles with zero external dependencies
- All special forms pass comprehensive test suite
- No panics on malformed input (graceful errors)
- Binary footprint < 2MB when compiled standalone
- Evaluation performance < 10µs for simple expressions

---

## 3. Scope

### In Scope

**Type System (types.go)**
- Value interface and 13 concrete types
- Go interop: FromGoValue(), ToGoValue()
- String representation for each type

**Environment Chain (env.go)**
- Lexical scope with parent reference
- Thread-safe RWMutex for concurrent reads
- Child scope creation (let bindings)
- Symbol lookup with shadowing

**Reader/Parser (reader.go)**
- Tokenizer for S-expressions
- Support: lists `()`, vectors `[]`, maps `{}`
- Quote `'`, quasiquote `` ` ``, unquote `~`, splice `~@`
- Keywords `:name`, strings `"..."`, numbers, symbols
- Line comments `;`

**Evaluator (eval.go)**
- 22 special forms (def, defn, defmacro, fn, if, cond, when, unless, and, or, not, let, let*, do, quote, quasiquote, set!, loop, recur, try, catch, throw)
- macroexpand is a stdlib function, not a special form
- Tail-call optimization
- Macro expansion with depth limit (100)
- Error propagation with context

**Plugin Interface (plugin.go)**
- Plugin interface definition
- Registry with namespace isolation
- Use() method for registration

### Out of Scope (Plugin Domain)

- Arithmetic, string, collection functions → Change 2 (stdlib)
- File I/O operations → Change 6a (io-plugin)
- HTTP client → Change 6b (net-plugin)
- LLM integration → Change 4 (llm-plugin)
- State machines → Change 7 (fsm-plugin)
- REPL, hot-reload → Change 3 (runtime-api)

---

## 4. Functional Requirements

### Core Evaluation

| ID | Requirement | Priority |
|----|-------------|----------|
| C1.1 | Self-evaluating atoms return themselves (nil, bool, int, float, string, keyword) | P0 |
| C1.2 | Symbols are resolved via environment chain lookup | P0 |
| C1.3 | Lists are evaluated as function calls or special forms | P0 |
| C1.4 | Macros receive unevaluated arguments, produce re-evaluated result | P0 |
| C1.5 | TCO applied to: if/when/unless branches, cond branches, do bodies, fn bodies, try/catch bodies | P0 |
| C1.6 | `recur` rebinds loop variables without stack growth | P0 |
| C1.7 | Errors include failing form context (source, line, column) | P0 |

### Environment & Concurrency

| ID | Requirement | Priority |
|----|-------------|----------|
| C1.8 | Environment supports concurrent reads (RWMutex) | P0 |
| C1.9 | Child scopes isolate bindings from parent | P0 |
| C1.10 | set! mutates only in defining scope (not global escape) | P0 |

### Plugin System

| ID | Requirement | Priority |
|----|-------------|----------|
| C1.11 | Plugins register before any Lisp evaluation | P0 |
| C1.12 | Init() is idempotent (no duplicate registrations) | P0 |
| C1.13 | Functions use namespace/fn-name convention | P0 |
| C1.14 | Plugin metadata accessible at runtime | P1 |

### Reader

| ID | Requirement | Priority |
|----|-------------|----------|
| C1.15 | Parse identical trees for semantically equivalent source | P0 |
| C1.16 | Commas treated as whitespace (Clojure-style readability) | P1 |
| C1.17 | Proper escape handling in strings (\n, \t, \", \\) | P0 |
| C1.18 | try/catch/throw provide structured error handling; catch binds the error value | P0 |

---

## 5. Non-Functional Requirements

### Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| Core boot time | < 1ms | Time to create Engine instance |
| Simple expression eval | < 10µs | `(+ 1 2)` or equivalent |
| 1000-iteration loop | < 5ms | Tail-recursive factorial |
| Memory per Engine | < 10MB | Baseline with empty env |

### Reliability

- Syntax errors caught at load time with line/column info
- Undefined symbol returns descriptive error (not nil panic)
- Division by zero returns Go error (not panic)
- Goroutine-safe for concurrent Eval() calls

### Security

- Core has no I/O capabilities
- No randomness (deterministic evaluation)
- No access to Go runtime internals
- Sandboxed by design (no way to escape)

---

## 6. Design Philosophy

### Unix KISS Principle

> "Do one thing well." — The core evaluates Lisp expressions. Nothing else.

**Good:**
- Pure function from (source + env) → Value
- Single responsibility per type
- Zero external dependencies
- try/catch/throw in core (evaluator must intercept errors)

**Bad (out of scope):**
- File I/O in core
- HTTP client in core
- Complex standard library in core
- macroexpand as special form (implement as stdlib function instead)

### Structural Simplicity

> "When in doubt, leave it out of core."

Test for inclusion: *"Can any useful Lisp program be written without this?"*
- If yes → exclude, make a plugin
- If no → consider for core

### Homoiconicity

Code is data. All constructs are S-expressions:
- Function calls: `(fn arg1 arg2)`
- Special forms: `(def name value)`
- Macros: `(defmacro name [args] body)`
- Data literals: lists, vectors, maps

---

## 7. Dependencies

### External Dependencies

None. Core must compile with `go build ./core/...` with zero external dependencies.

### Internal Dependencies

None (this is the foundation).

### Future Dependencies (Plugins)

Changes 2-7 will depend on this core package.

---

## 8. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| LOC exceeds 1000 target | Medium | Low | Measure with cloc; refactor to plugins if needed |
| TCO implementation bugs | Medium | High | Extensive test suite; property-based tests |
| Macro expansion infinite loop | Low | High | Hard depth limit (100) with clear error |
| Performance not meeting targets | Low | Medium | Benchmark early; optimize critical paths |
| Reader edge cases (Unicode) | Medium | Low | Document limitations; UTF-8 basic support |

---

## 9. Open Questions

1. **Unicode identifiers**: Should symbols support Unicode, or ASCII-only? (Joker allows Unicode with config)
2. **Big integers**: Include big.Int in core, or defer to stdlib plugin? (Joker has BigInt/BigFloat/BigRat)
3. **Floating point precision**: float64 only, or include big.Float? (Decision: float64 for core)
4. **Character type**: Include distinct char type, or use single-char strings? (Decision: no char type)
5. **GoFunc context**: GoFunc signature is `func(ctx context.Context, eval Evaluator, args []Value, env *Env) (Value, error)` — Evaluator interface must be defined in types.go to avoid circular imports

**Recommendation**: ASCII symbols, float64, no char type. Keep core minimal.

---

## 10. Acceptance Criteria

- [ ] All 13 Value types implemented with tests
- [x] All 22 special forms (including try/catch/throw, when/unless, and/or/not) implemented with tests
- [ ] Reader parses all syntax forms correctly
- [ ] TCO verified not to grow stack (deep recursion test)
- [ ] Macro expansion works with nested quasiquote
- [ ] Plugin interface allows registration
- [ ] Zero external dependencies in go.mod
- [ ] Test coverage ≥ 90%
- [ ] No panics on any malformed input
- [ ] Binary size check passes (< 2MB standalone)

---

## 11. References

### Inspiration Sources

- **glisp**: TCO, macro system, Go interop patterns
- **joker**: Clojure compatibility, namespace design, linter mode ideas
- **zygomys**: Package mechanism, sandbox mode, JSON/msgpack interop
- **starlark-go**: Thread safety, capability-based function registration
- **tengo**: Secure embedding, module system, bytecode VM patterns
- **Janet**: Minimal core philosophy, C interop patterns

### Specification References

- "Recursive Functions of Symbolic Expressions" — John McCarthy (1960)
- "The Roots of Lisp" — Paul Graham
- "Structure and Interpretation of Computer Programs" — SICP (Chapter 4)

---

**Next Step:** Create detailed design document (02-design.md) with type system, special forms specification, and evaluation rules.
