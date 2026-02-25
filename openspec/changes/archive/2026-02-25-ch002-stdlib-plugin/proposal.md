# Change Proposal: Standard Library Plugin

**Change ID:** 002-stdlib-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the standard library plugin providing essential functions that every Lisp program expects. Unlike the core engine, this plugin is implemented entirely in Go but has **no external dependencies**. It provides arithmetic, string manipulation, collection operations, higher-order functions, and control flow utilities.

**Key Characteristics:**
- First-party plugin, always loaded after core
- No external Go module dependencies
- Pure functions only (no I/O, no side effects)
- bootstrap.lisp loaded at plugin Init() time; defines threading macros using core special forms
- Thread-safe operations on immutable data structures

---

## 2. Motivation

### Problem
The core engine intentionally implements only special forms and the minimal type system. Without a standard library, users cannot:
- Perform basic arithmetic operations
- Manipulate strings and collections
- Use functional programming primitives (map, filter, reduce)
- Apply higher-order function patterns

### Solution
A comprehensive stdlib plugin that provides:
- **Arithmetic**: Full numeric tower with type promotion
- **Strings**: Concatenation, formatting, splitting, trimming
- **Collections**: Functional transforms, sorting, subsequence operations
- **Control Flow**: threading macros, conditional bindings (try/catch/throw are core special forms)
- **Higher-Order Functions**: Composition, partial application, memoization

### Success Metrics
- All stdlib functions have comprehensive test coverage
- Threading macros implemented in Lisp, not Go
- Zero panics on edge cases (empty collections, division by zero)
- Performance comparable to native Go operations (< 2x overhead)

---

## 3. Scope

### In Scope

**Arithmetic Functions**
- Basic: `+`, `-`, `*`, `/`
- Integer: `mod`, `quot` (quotient)
- Advanced: `pow`, `sqrt`, `abs`
- Predicates: `zero?`, `pos?`, `neg?`, `even?`, `odd?`
- Conversion: `int->float`, `float->int`

**String Operations**
- Concatenation: `str` (variadic, any type)
- Formatting: `format` (printf-style)
- Split/Join: `string/join`, `string/split`
- Transformation: `string/trim`, `string/upper`, `string/lower`, `string/replace`
- Predicates: `string/contains?`, `string/starts-with?`, `string/ends-with?`
- Utilities: `string/length`, `string/lines`, `string->int`, `string->float`

**Collection Operations**
- Constructors: `list`, `vector`, `hash-map`
- Access: `first`, `rest`, `last`, `nth`, `count`
- Construction: `cons`, `conj`, `append`
- Predicates: `empty?`, `seq?` (list or vector)
- Functional: `map`, `filter`, `reduce`
- Iteration: `for-each` (side effects)
- Sorting: `sort`, `sort-by`
- Subsequences: `take`, `drop`, `take-while`, `drop-while`
- Combinators: `zip`, `flatten`, `distinct`, `reverse`
- Map ops: `get`, `get-in`, `assoc`, `dissoc`, `merge`, `keys`, `vals`, `contains?`
- Coercion: `into`, `seq`

**Higher-Order Functions**
- Application: `apply`
- Composition: `comp`
- Partial: `partial`
- Caching: `memoize`
- Constants: `constantly`
- Identity: `identity`
- Multiplex: `juxt`
- Predicates: `every?`, `some`, `none?`, `not-any?`

**Control Flow**
- Assertions: `assert`
- Conditional binding: `when-let`, `if-let`
- Threading: `->` (thread-first), `->>` (thread-last), `as->` (thread-as)

Note: try/catch/throw are core special forms (implemented in ch001); stdlib provides higher-level control utilities built on top of them.

**Type System**
- Introspection: `type`
- Predicates: `nil?`, `bool?`, `int?`, `float?`, `string?`, `keyword?`, `symbol?`, `list?`, `vector?`, `map?`, `fn?`, `macro?`
- Conversion: `str->keyword`, `keyword->str`, `symbol->str`, `str->symbol`

### Out of Scope (Other Plugins)

- File I/O → Change 6a (io-plugin)
- HTTP client → Change 6b (net-plugin)
- JSON parsing → Change 6c (data-plugin)
- Regular expressions → Future plugin
- Date/time → Future plugin
- Random numbers → Future plugin (deterministic by design)

---

## 4. Functional Requirements

### Arithmetic

| ID | Requirement | Priority |
|----|-------------|----------|
| S2.1 | Numeric operations promote to float if any arg is float | P0 |
| S2.2 | Division by zero returns error (not panic) | P0 |
| S2.3 | `mod` works on integers only | P0 |
| S2.4 | `pow` handles float exponents | P0 |
| S2.5 | Predicates return boolean for any input (not error) | P0 |

### Strings

| ID | Requirement | Priority |
|----|-------------|----------|
| S2.6 | `str` concatenates any values by converting to string | P0 |
| S2.7 | `format` supports %s, %d, %f, %v, %% | P0 |
| S2.8 | String functions handle Unicode correctly | P0 |
| S2.9 | `string/split` with empty separator returns chars | P1 |

### Collections

| ID | Requirement | Priority |
|----|-------------|----------|
| S2.10 | `map`/`filter`/`reduce` work on lists and vectors | P0 |
| S2.11 | `reduce` without init uses first element | P0 |
| S2.12 | `sort` is stable | P0 |
| S2.13 | `take`/`drop` handle n > count gracefully | P0 |
| S2.14 | `flatten` flattens one level (not recursive by default) | P1 |
| S2.15 | `get` on map returns nil for missing key (not error) | P0 |

### Higher-Order Functions

| ID | Requirement | Priority |
|----|-------------|----------|
| S2.16 | `comp` composes right-to-left: `(comp f g h)` = f(g(h(x))) | P0 |
| S2.17 | `partial` fixes arguments from the left | P0 |
| S2.18 | `memoize` caches by argument equality (not identity) | P0 |
| S2.19 | `juxt` applies multiple fns to same args, returns vector | P0 |
| S2.19b | `not-any?` returns true when predicate is false for every element | P0 |

### Control Flow

| ID | Requirement | Priority |
|----|-------------|----------|
| S2.20 | `try`/`catch`/`throw` are core special forms (see ch001); stdlib threading macros and conditional bindings build on them | P0 |
| S2.21 | Threading macros expand at compile time | P0 |
| S2.22 | `->` threads as first arg, `->>` as last arg | P0 |
| S2.23 | `as->` allows named binding in thread | P0 |
| S2.24 | `when-let`/`if-let` short-circuit on nil | P0 |

---

## 5. Design Philosophy

### Immutability First

All collection operations return new collections. Originals are never modified:

```lisp
(def v [1 2 3])
(conj v 4)  ; returns [1 2 3 4]
v           ; still [1 2 3]
```

### Type Coercion

Operations work across compatible types with sensible coercion:

```lisp
(+ 1 2.5)     ; 3.5 (int promoted to float)
(str 1 "a" 2) ; "1a2" (numbers coerced to strings)
(into [] '(1 2 3)) ; [1 2 3] (list to vector)
```

### Nil Handling

Nil is treated as empty collection in collection contexts:

```lisp
(first nil)  ; nil
(count nil)  ; 0
(map inc nil) ; ()
```

### Lazy vs Eager

Stdlib is **eager** (not lazy) for simplicity:
- `map` returns realized list
- `filter` returns realized list
- No lazy sequences in stdlib (could be future plugin)

---

## 6. Threading Macros (Lisp Implementation)

Threading macros are implemented in `bootstrap.lisp`, not Go:

```lisp
; Thread-first: insert as first argument
(defmacro -> [x & forms]
  (loop [x x
         forms forms]
    (if (empty? forms)
      x
      (let [form (first forms)
            next-x (if (list? form)
                     `(~(first form) ~x ~@(rest form))
                     (list form x))]
        (recur next-x (rest forms))))))

; Thread-last: insert as last argument  
(defmacro ->> [x & forms]
  (loop [x x
         forms forms]
    (if (empty? forms)
      x
      (let [form (first forms)
            next-x (if (list? form)
                     `(~(first form) ~@(rest form) ~x)
                     (list form x))]
        (recur next-x (rest forms))))))

; Thread-as: named binding through forms
(defmacro as-> [x name & forms]
  (if (empty? forms)
    x
    (loop [result x
           remaining forms]
      (if (empty? remaining)
        result
        (recur `(let [~name ~result] ~(first remaining))
               (rest remaining))))))
```

**Rationale**: Demonstrates macro power, keeps Go code minimal, user-customizable.

Note: `as->` uses loop/recur for self-contained implementation avoiding dependency on interleave/butlast which are defined later in stdlib.

---

## 7. Error Handling

### Standard Error Types

```
TypeError:     Operation on incompatible types
ArityError:    Wrong number of arguments
ValueError:    Invalid value (e.g., divide by zero)
IndexError:    Index out of bounds
KeyError:      Key not found in map (for strict lookup)
```

### Error Format

All errors are single-line, lowercase, wrapped with context:
```
TypeError: expected number, got string "hello" at (+ 1 "hello") in script.lisp:42:5
```

---

## 8. Performance Requirements

| Operation | Target | Notes |
|-----------|--------|-------|
| `(+ 1 2 3)` | < 100ns | Inline arithmetic |
| `(map inc (range 1000))` | < 1ms | List traversal |
| `(reduce + 0 (range 10000))` | < 5ms | Accumulation |
| `(sort (shuffle (range 1000)))` | < 10ms | Quick sort |
| String concat (1000 chars) | < 1µs | Builder pattern |

---

## 9. Dependencies

### External Dependencies

None. Stdlib plugin has no external Go module dependencies.

### Internal Dependencies

- **Change 1** (core-engine): Required
  - Value types
  - Environment
  - Plugin interface
  - Special forms (def, fn, loop, recur, try, catch, throw, etc.)
  - bootstrap.lisp is evaluated via the core engine at plugin Init() time

### Dependent Changes

- **Change 3** (runtime-api): Uses stdlib
- **Change 4-7**: All depend on stdlib functions

---

## 10. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Unicode string handling bugs | Medium | Medium | Test with Unicode test suite |
| Collection operation edge cases | Medium | Low | Property-based testing (empty, single, large) |
| Threading macro expansion bugs | Low | Medium | Comprehensive macro tests |
| Performance regression | Low | Medium | Benchmark suite, CI gates |

---

## 11. Acceptance Criteria

- [ ] All arithmetic functions with type promotion
- [ ] All string operations with Unicode support
- [ ] All collection operations (list, vector, map)
- [ ] All higher-order functions
- [ ] Threading macros in bootstrap.lisp
- [ ] Error handling utilities (when-let, if-let, assert) — try/catch/throw are core special forms
- [ ] Type predicates and conversions
- [ ] Test coverage ≥ 90%
- [ ] Property-based tests for edge cases
- [ ] Benchmark suite with performance baselines

---

**Next Step:** Create detailed design document (02-design.md) with function signatures, implementation notes, and bootstrap.lisp specification.
