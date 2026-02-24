# Tasks: Core Engine Foundation

**Change ID:** 001-core-engine  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 2-3 weeks

---

## Phase 1: Type System (Days 1-3)

### Task 1.1: Value Interface and Nil/Bool
- [ ] Define Value interface in `types.go`
- [ ] Implement Nil type
- [ ] Implement Bool type
- [ ] Write unit tests for Nil/Bool
- **Acceptance**: All tests pass, 100% coverage for new code

### Task 1.2: Numeric Types
- [ ] Implement Int type with int64 backing
- [ ] Implement Float type with float64 backing
- [ ] Add Type() and Equals() methods
- [ ] Write unit tests
- **Acceptance**: Numeric operations work correctly, edge cases handled

### Task 1.3: String and Symbol Types
- [ ] Implement String type with proper escaping
- [ ] Implement Symbol type
- [ ] Implement Keyword type (self-evaluating)
- [ ] Write unit tests
- **Acceptance**: String quoting works, symbols/keywords distinct

### Task 1.4: Collection Types
- [ ] Implement List type (slice-based)
- [ ] Implement Vector type
- [ ] Implement HashMap type
- [ ] Implement Assoc() and Get() for HashMap
- [ ] Write unit tests
- **Acceptance**: Collections immutable, proper Equals() behavior

### Task 1.5: Function Types
- [ ] Implement GoFunc for native functions
- [ ] Implement Lambda for closures
- [ ] Implement Macro for syntax transformers
- [ ] Write unit tests
- **Acceptance**: Functions have proper type identity

### Task 1.6: Go Interop Helpers
- [ ] Implement FromGoValue() converter
- [ ] Implement ToGoValue() converter
- [ ] Handle all supported Go types
- [ ] Write unit tests
- **Acceptance**: Round-trip conversion works

---

## Phase 2: Environment (Days 4-5)

### Task 2.1: Basic Environment
- [ ] Define Env struct with RWMutex
- [ ] Implement Set() and Get()
- [ ] Implement parent chain traversal
- [ ] Write unit tests
- **Acceptance**: Variable lookup works across scope chain

### Task 2.2: Child Scope Creation
- [ ] Implement Child() method
- [ ] Implement ChildVariadic() for function binding
- [ ] Test parameter binding
- [ ] Write unit tests
- **Acceptance**: Child scopes isolate bindings

### Task 2.3: Symbol Resolution
- [ ] Implement Find() for locating defining scope
- [ ] Test shadowing behavior
- [ ] Test concurrent reads
- [ ] Write unit tests
- **Acceptance**: Symbol resolution thread-safe

---

## Phase 3: Reader (Days 6-9)

### Task 3.1: Tokenizer Foundation
- [ ] Define token types
- [ ] Implement basic tokenizer structure
- [ ] Handle parentheses, brackets, braces
- [ ] Write unit tests
- **Acceptance**: Basic tokenization works

### Task 3.2: String Tokenization
- [ ] Implement string literal parsing
- [ ] Handle escape sequences (\n, \t, \", \\)
- [ ] Report unterminated string errors
- [ ] Write unit tests
- **Acceptance**: All escape sequences work

### Task 3.3: Number Tokenization
- [ ] Implement integer parsing
- [ ] Implement float parsing
- [ ] Handle negative numbers
- [ ] Write unit tests
- **Acceptance**: Numbers parse correctly

### Task 3.4: Symbol and Keyword Tokenization
- [ ] Implement symbol parsing
- [ ] Implement keyword parsing (colon prefix)
- [ ] Define valid symbol characters
- [ ] Write unit tests
- **Acceptance**: Symbols and keywords parse correctly

### Task 3.5: Quote Syntax
- [ ] Implement quote (')
- [ ] Implement quasiquote (`)
- [ ] Implement unquote (~)
- [ ] Implement unquote-splicing (~@)
- [ ] Write unit tests
- **Acceptance**: All quote forms work

### Task 3.6: Comment Handling
- [ ] Implement line comment (;)
- [ ] Skip comments in tokenization
- [ ] Preserve line numbers for errors
- [ ] Write unit tests
- **Acceptance**: Comments don't affect parsing

### Task 3.7: Parser
- [ ] Implement parseForm() dispatcher
- [ ] Implement parseList()
- [ ] Implement parseVector()
- [ ] Implement parseHashMap()
- [ ] Write unit tests
- **Acceptance**: All forms parse correctly

### Task 3.8: Reader Integration
- [ ] Integrate tokenizer and parser
- [ ] Implement Read() function
- [ ] Handle multiple forms in input
- [ ] Write unit tests
- **Acceptance**: Full read cycle works

---

## Phase 4: Evaluator (Days 10-16)

### Task 4.1: Evaluator Foundation
- [ ] Define Evaluator struct
- [ ] Implement Eval() dispatcher
- [ ] Handle self-evaluating types
- [ ] Handle symbol lookup
- [ ] Write unit tests
- **Acceptance**: Basic evaluation works

### Task 4.2: Special Forms - Definitions
- [ ] Implement def
- [ ] Implement defn
- [ ] Implement defmacro
- [ ] Implement fn (lambda)
- [ ] Write unit tests
- **Acceptance**: Definitions work correctly

### Task 4.3: Special Forms - Conditionals
- [ ] Implement if
- [ ] Implement cond
- [ ] Implement and/or/not
- [ ] Write unit tests
- **Acceptance**: Conditionals evaluate correctly

### Task 4.4: Special Forms - Binding
- [ ] Implement let (parallel binding)
- [ ] Implement let* (sequential binding)
- [ ] Implement set!
- [ ] Write unit tests
- **Acceptance**: Bindings work correctly

### Task 4.5: Special Forms - Control
- [ ] Implement do
- [ ] Implement quote
- [ ] Implement quasiquote expansion
- [ ] Write unit tests
- **Acceptance**: Control forms work

### Task 4.5b: Special Forms - Error Handling
- [ ] Implement try (evaluate body, catch errors)
- [ ] Implement catch (bind error, evaluate handler)
- [ ] Implement throw (raise error from Lisp)
- [ ] Implement when (short-circuit conditional, TCO on body)
- [ ] Implement unless (negated when)
- [ ] Write unit tests for all error handling forms
- **Acceptance**: try/catch works, throw raises catchable errors, when/unless evaluate correctly

### Task 4.6: Tail Call Optimization
- [ ] Design tail call structure
- [ ] Implement TCO for if branches
- [ ] Implement TCO for do bodies
- [ ] Implement TCO for fn bodies
- [ ] Write unit tests
- **Acceptance**: Deep recursion doesn't overflow stack

### Task 4.7: Loop and Recur
- [ ] Implement loop special form
- [ ] Implement recur special form
- [ ] Ensure O(1) stack space
- [ ] Write unit tests
- **Acceptance**: Loop/recur works without stack growth

### Task 4.8: Macro System
- [ ] Implement macroexpand
- [ ] Implement macro expansion in eval
- [ ] Add depth limit (100)
- [ ] Write unit tests
- **Acceptance**: Macros expand correctly

---

## Phase 5: Plugin Interface (Days 17-18)

### Task 5.1: Plugin Interface
- [ ] Define Plugin interface
- [ ] Define PluginMeta struct
- [ ] Implement Registry
- [ ] Write unit tests
- **Acceptance**: Plugins can register

### Task 5.2: Registry Operations
- [ ] Implement Register()
- [ ] Implement Get()
- [ ] Implement Namespaces()
- [ ] Handle duplicate registration
- [ ] Write unit tests
- **Acceptance**: Registry operations work

---

## Phase 6: Error Handling (Days 19-20)

### Task 6.1: Error Types
- [ ] Define LispicoError struct
- [ ] Implement Error() and Unwrap()
- [ ] Define error constructors
- [ ] Write unit tests
- **Acceptance**: Errors carry context

### Task 6.2: Error Propagation
- [ ] Add error context in Reader
- [ ] Add error context in Evaluator
- [ ] Ensure line/column info preserved
- [ ] Write unit tests
- **Acceptance**: Errors have source location

---

## Phase 7: Integration and Testing (Days 21-23)

### Task 7.1: Integration Tests
- [ ] Test full read-eval-print cycle
- [ ] Test recursive functions
- [ ] Test macro expansion
- [ ] Test concurrent evaluation
- **Acceptance**: All integration tests pass

### Task 7.2: Property-Based Tests
- [ ] Test round-trip parse/print
- [ ] Test determinism
- [ ] Test immutability
- **Acceptance**: Properties hold

### Task 7.3: Performance Benchmarks
- [ ] Benchmark simple expressions
- [ ] Benchmark recursive functions
- [ ] Benchmark macro expansion
- **Acceptance**: Performance targets met

### Task 7.4: Documentation
- [ ] Document public API
- [ ] Add examples
- [ ] Document error types
- **Acceptance**: Documentation complete

---

## Acceptance Criteria

- [ ] All 13 Value types implemented and tested
- [ ] All 19 special forms (including try/catch/throw, when/unless) working
- [ ] Reader parses all syntax correctly
- [ ] TCO verified with deep recursion
- [ ] Macro expansion with depth limit
- [ ] Zero external dependencies in go.mod
- [ ] Test coverage ≥ 90%
- [ ] No panics on malformed input
- [ ] Binary size < 2MB when compiled standalone
- [ ] Simple expression eval < 10µs

---

## Dependencies

- Go 1.21+ (for log/slog)
- Standard library only

---

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| TCO bugs | Extensive recursive test cases |
| Macro expansion infinite loop | Hard depth limit with error |
| Unicode handling | Document limitations, basic UTF-8 |
| Performance issues | Benchmark early, optimize critical paths |

---

## Notes

- Keep code simple and readable
- Add tests as you implement
- Document any deviations from design
- Measure LOC regularly (target < 1000)

**Begin implementation with Task 1.1**
