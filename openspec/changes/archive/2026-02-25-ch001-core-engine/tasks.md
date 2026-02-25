# Tasks: Core Engine Foundation

**Change ID:** 001-core-engine  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 2-3 weeks

---

## Phase 1: Type System (Days 1-3)

### Task 1.1: Value Interface and Nil/Bool
- [x] Define Value interface in `types.go`
- [x] Implement Nil type
- [x] Implement Bool type
- [x] Write unit tests for Nil/Bool
- **Acceptance**: All tests pass, 100% coverage for new code

### Task 1.2: Numeric Types
- [x] Implement Int type with int64 backing
- [x] Implement Float type with float64 backing
- [x] Add Type() and Equals() methods
- [x] Write unit tests
- **Acceptance**: Numeric operations work correctly, edge cases handled

### Task 1.3: String and Symbol Types
- [x] Implement String type with proper escaping
- [x] Implement Symbol type
- [x] Implement Keyword type (self-evaluating)
- [x] Write unit tests
- **Acceptance**: String quoting works, symbols/keywords distinct

### Task 1.4: Collection Types
- [x] Implement List type (slice-based)
- [x] Implement Vector type
- [x] Implement HashMap type
- [x] Implement Assoc() and Get() for HashMap
- [x] Write unit tests
- **Acceptance**: Collections immutable, proper Equals() behavior

### Task 1.5: Function Types
- [x] Implement GoFunc for native functions
- [x] Implement Lambda for closures
- [x] Implement Macro for syntax transformers
- [x] Write unit tests
- **Acceptance**: Functions have proper type identity

### Task 1.6: Go Interop Helpers
- [x] Implement FromGoValue() converter
- [x] Implement ToGoValue() converter
- [x] Handle all supported Go types
- [x] Write unit tests
- **Acceptance**: Round-trip conversion works

---

## Phase 2: Environment (Days 4-5)

### Task 2.1: Basic Environment
- [x] Define Env struct with RWMutex
- [x] Implement Set() and Get()
- [x] Implement parent chain traversal
- [x] Write unit tests
- **Acceptance**: Variable lookup works across scope chain

### Task 2.2: Child Scope Creation
- [x] Implement Child() method
- [x] Implement ChildVariadic() for function binding
- [x] Test parameter binding
- [x] Write unit tests
- **Acceptance**: Child scopes isolate bindings

### Task 2.3: Symbol Resolution
- [x] Implement Find() for locating defining scope
- [x] Test shadowing behavior
- [x] Test concurrent reads
- [x] Write unit tests
- **Acceptance**: Symbol resolution thread-safe

---

## Phase 3: Reader (Days 6-9)

### Task 3.1: Tokenizer Foundation
- [x] Define token types
- [x] Implement basic tokenizer structure
- [x] Handle parentheses, brackets, braces
- [x] Write unit tests
- **Acceptance**: Basic tokenization works

### Task 3.2: String Tokenization
- [x] Implement string literal parsing
- [x] Handle escape sequences (\n, \t, \", \\)
- [x] Report unterminated string errors
- [x] Write unit tests
- **Acceptance**: All escape sequences work

### Task 3.3: Number Tokenization
- [x] Implement integer parsing
- [x] Implement float parsing
- [x] Handle negative numbers
- [x] Write unit tests
- **Acceptance**: Numbers parse correctly

### Task 3.4: Symbol and Keyword Tokenization
- [x] Implement symbol parsing
- [x] Implement keyword parsing (colon prefix)
- [x] Define valid symbol characters
- [x] Write unit tests
- **Acceptance**: Symbols and keywords parse correctly

### Task 3.5: Quote Syntax
- [x] Implement quote (')
- [x] Implement quasiquote (`)
- [x] Implement unquote (~)
- [x] Implement unquote-splicing (~@)
- [x] Write unit tests
- **Acceptance**: All quote forms work

### Task 3.6: Comment Handling
- [x] Implement line comment (;)
- [x] Skip comments in tokenization
- [x] Preserve line numbers for errors
- [x] Write unit tests
- **Acceptance**: Comments don't affect parsing

### Task 3.7: Parser
- [x] Implement parseForm() dispatcher
- [x] Implement parseList()
- [x] Implement parseVector()
- [x] Implement parseHashMap()
- [x] Write unit tests
- **Acceptance**: All forms parse correctly

### Task 3.8: Reader Integration
- [x] Integrate tokenizer and parser
- [x] Implement Read() function
- [x] Handle multiple forms in input
- [x] Write unit tests
- **Acceptance**: Full read cycle works

---

## Phase 4: Evaluator (Days 10-16)

### Task 4.1: Evaluator Foundation
- [x] Define Evaluator struct
- [x] Implement Eval() dispatcher
- [x] Handle self-evaluating types
- [x] Handle symbol lookup
- [x] Write unit tests
- **Acceptance**: Basic evaluation works

### Task 4.2: Special Forms - Definitions
- [x] Implement def
- [x] Implement defn
- [x] Implement defmacro
- [x] Implement fn (lambda)
- [x] Write unit tests
- **Acceptance**: Definitions work correctly

### Task 4.3: Special Forms - Conditionals
- [x] Implement if
- [x] Implement cond
- [x] Implement and/or/not
- [x] Write unit tests
- **Acceptance**: Conditionals evaluate correctly

### Task 4.4: Special Forms - Binding
- [x] Implement let (parallel binding)
- [x] Implement let* (sequential binding)
- [x] Implement set!
- [x] Write unit tests
- **Acceptance**: Bindings work correctly

### Task 4.5: Special Forms - Control
- [x] Implement do
- [x] Implement quote
- [x] Implement quasiquote expansion
- [x] Write unit tests
- **Acceptance**: Control forms work

### Task 4.5b: Special Forms - Error Handling
- [x] Implement try (evaluate body, catch errors)
- [x] Implement catch (bind error, evaluate handler)
- [x] Implement throw (raise error from Lisp)
- [x] Implement when (short-circuit conditional, TCO on body)
- [x] Implement unless (negated when)
- [x] Write unit tests for all error handling forms
- **Acceptance**: try/catch works, throw raises catchable errors, when/unless evaluate correctly

### Task 4.6: Tail Call Optimization
- [x] Design tail call structure
- [x] Implement TCO for if branches
- [x] Implement TCO for do bodies
- [x] Implement TCO for fn bodies
- [x] Write unit tests
- **Acceptance**: Deep recursion doesn't overflow stack

### Task 4.7: Loop and Recur
- [x] Implement loop special form
- [x] Implement recur special form
- [x] Ensure O(1) stack space
- [x] Write unit tests
- **Acceptance**: Loop/recur works without stack growth

### Task 4.8: Macro System
- [x] Implement macroexpand
- [x] Implement macro expansion in eval
- [x] Add depth limit (100)
- [x] Write unit tests
- **Acceptance**: Macros expand correctly

---

## Phase 5: Plugin Interface (Days 17-18)

### Task 5.1: Plugin Interface
- [x] Define Plugin interface
- [x] Define PluginMeta struct
- [x] Implement Registry
- [x] Write unit tests
- **Acceptance**: Plugins can register

### Task 5.2: Registry Operations
- [x] Implement Register()
- [x] Implement Get()
- [x] Implement Namespaces()
- [x] Handle duplicate registration
- [x] Write unit tests
- **Acceptance**: Registry operations work

---

## Phase 6: Error Handling (Days 19-20)

### Task 6.1: Error Types
- [x] Define LispicoError struct
- [x] Implement Error() and Unwrap()
- [x] Define error constructors
- [x] Write unit tests
- **Acceptance**: Errors carry context

### Task 6.2: Error Propagation
- [x] Add error context in Reader
- [x] Add error context in Evaluator
- [x] Ensure line/column info preserved
- [x] Write unit tests
- **Acceptance**: Errors have source location

---

## Phase 7: Integration and Testing (Days 21-23)

### Task 7.1: Integration Tests
- [x] Test full read-eval-print cycle
- [x] Test recursive functions
- [x] Test macro expansion
- [x] Test concurrent evaluation
- **Acceptance**: All integration tests pass

### Task 7.2: Property-Based Tests
- [x] Test round-trip parse/print
- [x] Test determinism
- [x] Test immutability
- **Acceptance**: Properties hold

### Task 7.3: Performance Benchmarks
- [x] Benchmark simple expressions
- [x] Benchmark recursive functions
- [x] Benchmark macro expansion
- **Acceptance**: Performance targets met

### Task 7.4: Documentation
- [x] Document public API
- [x] Add examples
- [x] Document error types
- **Acceptance**: Documentation complete

---

## Acceptance Criteria

- [x] All 13 Value types implemented and tested
- [x] All 22 special forms (including try/catch/throw, when/unless, and/or/not) working
- [x] Reader parses all syntax correctly
- [x] TCO verified with deep recursion
- [x] Macro expansion with depth limit
- [x] Zero external dependencies in go.mod
- [x] Test coverage ≥ 90%
- [x] No panics on malformed input
- [x] Binary size < 2MB when compiled standalone
- [x] Simple expression eval < 10µs

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
