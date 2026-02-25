# Tasks: Standard Library Plugin

**Change ID:** 002-stdlib-plugin  
**Status:** Implementation Complete  
**Created:** 2026-02-23  
**Completed:** 2026-02-25
**Estimated Effort:** 1-2 weeks  
**Depends On:** Change 1 (core-engine)

---

## Phase 1: Arithmetic Functions (Days 1-2)

### Task 1.1: Basic Arithmetic
- [x] Implement + with variadic args
- [x] Implement - (binary and negation)
- [x] Implement * with variadic args
- [x] Implement / with float promotion
- [x] Write tests
- **Acceptance**: All operations work with type promotion

### Task 1.2: Integer Operations
- [x] Implement mod
- [x] Implement quot
- [x] Handle division by zero errors
- [x] Write tests
- **Acceptance**: Integer ops work correctly

### Task 1.3: Advanced Math
- [x] Implement pow
- [x] Implement sqrt
- [x] Implement abs
- [x] Implement floor, ceil
- [x] Write tests
- **Acceptance**: Math functions accurate

### Task 1.4: Predicates
- [x] Implement zero?
- [x] Implement pos?
- [x] Implement neg?
- [x] Implement max, min
- [x] Write tests
- **Acceptance**: Predicates return correct booleans

---

## Phase 2: String Functions (Days 3-4)

### Task 2.1: Basic String Operations
- [x] Implement str (concatenation)
- [x] Implement format (printf-style)
- [x] Write tests
- **Acceptance**: String building works

### Task 2.2: String Transformation
- [x] Implement string/join
- [x] Implement string/split
- [x] Implement string/trim
- [x] Write tests
- **Acceptance**: Transformations work correctly

### Task 2.3: String Case and Replace
- [x] Implement string/upper
- [x] Implement string/lower
- [x] Implement string/replace
- [x] Write tests
- **Acceptance**: Case operations work

### Task 2.4: String Predicates
- [x] Implement string/contains?
- [x] Implement string/starts-with?
- [x] Implement string/ends-with?
- [x] Write tests
- **Acceptance**: Predicates work correctly

### Task 2.5: String Utilities
- [x] Implement string/length (rune count)
- [x] Implement string/lines
- [x] Implement string->int
- [x] Implement string->float
- [x] Write tests
- **Acceptance**: Utilities work correctly

---

## Phase 3: Collection Functions (Days 5-7)

### Task 3.1: Constructors
- [x] Implement list
- [x] Implement vector
- [x] Implement hash-map
- [x] Implement concat
- [x] Implement reverse
- [x] Write tests
- **Acceptance**: Constructors create correct types

### Task 3.2: Access Functions
- [x] Implement first
- [x] Implement rest
- [x] Implement last
- [x] Implement nth
- [x] Write tests
- **Acceptance**: Access functions work

### Task 3.3: Construction Functions
- [x] Implement cons
- [x] Implement conj
- [x] Implement count
- [x] Implement empty?
- [x] Write tests
- **Acceptance**: Construction preserves immutability

### Task 3.4: Map Operations
- [x] Implement get
- [x] Implement assoc
- [x] Implement keys
- [x] Implement vals
- [x] Write tests
- **Acceptance**: Map operations work

### Task 3.5: Functional Operations
- [x] Implement map
- [x] Implement filter
- [x] Implement reduce
- [x] Write tests
- **Acceptance**: Functional ops work on lists and vectors

---

## Phase 4: Higher-Order Functions (Days 8-9)

### Task 4.1: Application
- [x] Implement apply
- [x] Write tests
- **Acceptance**: Apply works with any function

---

## Phase 5: Control Flow (Days 10-11)

### Task 5.1: Error Handling
- [x] Implement assert
- [x] Write tests
- **Acceptance**: Error handling works

### Task 5.2: Conditional Binding
- [x] Implement when-let (as macro)
- [x] Implement if-let (as macro)
- [x] Write tests
- **Acceptance**: Conditional binding works

---

## Phase 6: Type System (Days 12)

### Task 6.1: Type Predicates
- [x] Implement type
- [x] Implement nil?, bool?, int?, float?
- [x] Implement string?, keyword?, symbol?
- [x] Implement list?, vector?, map?, fn?, macro?
- [x] Write tests
- **Acceptance**: All predicates work

### Task 6.2: Type Conversions
- [x] Implement str->keyword
- [x] Implement keyword->str
- [x] Implement int->float, float->int
- [x] Write tests
- **Acceptance**: Conversions work correctly

---

## Phase 7: Bootstrap Lisp (Days 13-14)

### Task 7.1: Threading Macros
- [x] Implement -> (thread-first)
- [x] Implement ->> (thread-last)
- [x] Implement as-> (thread-as)
- [x] Implement get-in
- [x] Write tests
- **Acceptance**: Threading macros expand correctly

### Task 7.2: Utility Macros
- [x] Implement when-let (as macro in bootstrap)
- [x] Implement if-let (as macro in bootstrap)
- [x] Write tests
- **Acceptance**: Utility macros work

---

## Acceptance Criteria

- [x] All arithmetic functions with type promotion
- [x] All string operations with Unicode support
- [x] All collection operations (list, vector, map)
- [x] All higher-order functions (map, filter, reduce, apply)
- [x] Threading macros in bootstrap.lisp
- [x] Type predicates and conversions
- [x] Test suite passes

---

## Dependencies

- Change 001-core-engine (required) ✓

---

## Implementation Notes

1. `let` has parallel bindings (all evaluated in parent env) per Clojure semantics
2. `let*` has sequential bindings (each can see previous)
3. Threading macros use `let*` internally for sequential binding access
4. `concat` and `reverse` added as core collection functions
5. `if-let` and `when-let` are macros (not functions) to allow body lazy evaluation
