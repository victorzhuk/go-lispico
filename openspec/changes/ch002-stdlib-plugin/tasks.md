# Tasks: Standard Library Plugin

**Change ID:** 002-stdlib-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 1-2 weeks  
**Depends On:** Change 1 (core-engine)

---

## Phase 1: Arithmetic Functions (Days 1-2)

### Task 1.1: Basic Arithmetic
- [ ] Implement + with variadic args
- [ ] Implement - (binary and negation)
- [ ] Implement * with variadic args
- [ ] Implement / with float promotion
- [ ] Write tests
- **Acceptance**: All operations work with type promotion

### Task 1.2: Integer Operations
- [ ] Implement mod
- [ ] Implement quot
- [ ] Handle division by zero errors
- [ ] Write tests
- **Acceptance**: Integer ops work correctly

### Task 1.3: Advanced Math
- [ ] Implement pow
- [ ] Implement sqrt
- [ ] Implement abs
- [ ] Implement floor, ceil
- [ ] Write tests
- **Acceptance**: Math functions accurate

### Task 1.4: Predicates
- [ ] Implement zero?
- [ ] Implement pos?
- [ ] Implement neg?
- [ ] Write tests
- **Acceptance**: Predicates return correct booleans

---

## Phase 2: String Functions (Days 3-4)

### Task 2.1: Basic String Operations
- [ ] Implement str (concatenation)
- [ ] Implement format (printf-style)
- [ ] Write tests
- **Acceptance**: String building works

### Task 2.2: String Transformation
- [ ] Implement string/join
- [ ] Implement string/split
- [ ] Implement string/trim
- [ ] Write tests
- **Acceptance**: Transformations work correctly

### Task 2.3: String Case and Replace
- [ ] Implement string/upper
- [ ] Implement string/lower
- [ ] Implement string/replace
- [ ] Write tests
- **Acceptance**: Case operations work

### Task 2.4: String Predicates
- [ ] Implement string/contains?
- [ ] Implement string/starts-with?
- [ ] Implement string/ends-with?
- [ ] Write tests
- **Acceptance**: Predicates work correctly

### Task 2.5: String Utilities
- [ ] Implement string/length (rune count)
- [ ] Implement string/lines
- [ ] Implement string->int
- [ ] Implement string->float
- [ ] Write tests
- **Acceptance**: Utilities work correctly

---

## Phase 3: Collection Functions (Days 5-7)

### Task 3.1: Constructors
- [ ] Implement list
- [ ] Implement vector
- [ ] Implement hash-map
- [ ] Write tests
- **Acceptance**: Constructors create correct types

### Task 3.2: Access Functions
- [ ] Implement first
- [ ] Implement rest
- [ ] Implement last
- [ ] Implement nth
- [ ] Write tests
- **Acceptance**: Access functions work

### Task 3.3: Construction Functions
- [ ] Implement cons
- [ ] Implement conj
- [ ] Implement count
- [ ] Write tests
- **Acceptance**: Construction preserves immutability

### Task 3.4: Map Operations
- [ ] Implement get
- [ ] Implement assoc
- [ ] Implement keys
- [ ] Implement vals
- [ ] Write tests
- **Acceptance**: Map operations work

### Task 3.5: Functional Operations
- [ ] Implement map
- [ ] Implement filter
- [ ] Implement reduce
- [ ] Write tests
- **Acceptance**: Functional ops work on lists and vectors

---

## Phase 4: Higher-Order Functions (Days 8-9)

### Task 4.1: Application
- [ ] Implement apply
- [ ] Write tests
- **Acceptance**: Apply works with any function

### Task 4.2: Composition
- [ ] Implement comp
- [ ] Implement partial
- [ ] Write tests
- **Acceptance**: Composition works correctly

### Task 4.3: Utilities
- [ ] Implement memoize
- [ ] Implement constantly
- [ ] Implement identity
- [ ] Write tests
- **Acceptance**: Utilities work correctly

### Task 4.4: Predicates
- [ ] Implement every?
- [ ] Implement some
- [ ] Implement none?
- [ ] Write tests
- **Acceptance**: Predicates work on collections

---

## Phase 5: Control Flow (Days 10-11)

### Task 5.1: Error Handling
- [ ] Implement try/catch
- [ ] Implement throw
- [ ] Implement assert
- [ ] Write tests
- **Acceptance**: Error handling works

### Task 5.2: Conditional Binding
- [ ] Implement when-let
- [ ] Implement if-let
- [ ] Write tests
- **Acceptance**: Conditional binding works

---

## Phase 6: Type System (Days 12)

### Task 6.1: Type Predicates
- [ ] Implement type
- [ ] Implement nil?, bool?, int?, float?
- [ ] Implement string?, keyword?, symbol?
- [ ] Implement list?, vector?, map?, fn?, macro?
- [ ] Write tests
- **Acceptance**: All predicates work

### Task 6.2: Type Conversions
- [ ] Implement str->keyword
- [ ] Implement keyword->str
- [ ] Implement int->float, float->int
- [ ] Write tests
- **Acceptance**: Conversions work correctly

---

## Phase 7: Bootstrap Lisp (Days 13-14)

### Task 7.1: Threading Macros
- [ ] Implement -> (thread-first)
- [ ] Implement ->> (thread-last)
- [ ] Implement as-> (thread-as)
- [ ] Write tests
- **Acceptance**: Threading macros expand correctly

### Task 7.2: Utility Macros
- [ ] Implement when
- [ ] Implement when-not
- [ ] Implement when-let
- [ ] Implement if-let
- [ ] Write tests
- **Acceptance**: Utility macros work

---

## Acceptance Criteria

- [ ] All arithmetic functions with type promotion
- [ ] All string operations with Unicode support
- [ ] All collection operations (list, vector, map)
- [ ] All higher-order functions
- [ ] Threading macros in bootstrap.lisp
- [ ] Error handling (try/catch/throw)
- [ ] Type predicates and conversions
- [ ] Test coverage ≥ 90%
- [ ] Property-based tests for edge cases
- [ ] Benchmark suite with baselines

---

## Dependencies

- Change 001-core-engine (required)

---

**Begin implementation after Change 1 is complete**
