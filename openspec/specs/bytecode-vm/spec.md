# bytecode-vm Specification

## Purpose

The bytecode VM is an experimental, opt-in evaluator (`runtime.WithBytecode()`)
that compiles forms to bytecode chunks and executes them on a stack machine, with
optional on-disk caching of compiled bytecode. It currently executes a subset of
the language; the tree-walking evaluator is the supported default.
## Requirements
### Requirement: Bytecode VM execution

The bytecode VM SHALL be an opt-in evaluator, selectable with
`runtime.WithBytecode()`, that produces results identical to the tree-walking
evaluator for every program it compiles. It is a documented subset: for a form it
does not compile it SHALL return a typed error, and the runtime SHALL fall back to
the tree-walking evaluator for that form — never panicking, and never producing a
result that differs from the tree-walker. Evaluations SHALL be isolated in their
results: compiled chunks MAY be cached and reused, but no stack or frame state
SHALL leak between `Eval` calls. VM instances SHALL be reused across evaluations —
on both the `Eval` path and the `Apply`/`Call` path — rather than a fresh machine
being allocated per call; a reused instance SHALL be reset before it runs the next
evaluation so no state leaks between them.

Every compiled expression SHALL leave exactly one result on the stack; a
non-executed `when` or `unless` body SHALL produce `nil`. Definition and mutation
SHALL have distinct semantics: a definition writes to the current scope, while
`set!` updates the scope that already owns the binding and SHALL return a typed
error when no binding exists; locals resolved to slots keep slot mutation. A catch
binding SHALL exist only in the handler scope: compiling a `try` normal body SHALL
NOT reserve or shift the catch slot, and leaving the handler SHALL restore the
previous local layout.

#### Scenario: Supported forms match the tree-walker

- **WHEN** the VM evaluates a form it compiles
- **THEN** the result SHALL equal the tree-walking evaluator's result for the same form and environment, including the runtime type of a value bound by `catch`

#### Scenario: Unsupported form defers to the tree-walker

- **WHEN** a program uses a form the VM does not compile (a `defmacro` nested in a body, or `unquote-splicing`)
- **THEN** compilation SHALL return a typed "unsupported in bytecode" error and the runtime SHALL evaluate that form with the tree-walker, never panicking

#### Scenario: loop/recur iterates in constant stack

- **WHEN** a `loop`/`recur` runs 10,000 iterations
- **THEN** execution SHALL complete without growing the Go stack and SHALL return the same value as the tree-walker

#### Scenario: try/catch/throw handles errors

- **WHEN** a `try` body throws a value or a called `GoFunc` returns an error, and a `catch` clause is present
- **THEN** the caught value SHALL be bound to the catch symbol with the same runtime type as under the tree-walker, and the handler result SHALL match

#### Scenario: Variadic functions bind rest arguments

- **WHEN** a variadic `fn` is applied with more arguments than fixed parameters
- **THEN** the rest arguments SHALL be bound as a list, matching `Env.ChildVariadic`

#### Scenario: Each evaluation is isolated

- **WHEN** two forms are evaluated in sequence on the same engine
- **THEN** the second evaluation SHALL return its own result, with no instructions or stack state left over from the first, whether or not its chunk came from the cache

#### Scenario: Call reuses a pooled VM

- **WHEN** `Engine.Call` invokes a function repeatedly on one `WithBytecode()` engine
- **THEN** each call SHALL run on a reset, reused VM from the pool rather than a freshly allocated machine, and SHALL return the same result the tree-walker would

#### Scenario: Skipped when/unless produces nil

- **WHEN** a false-test `when` or true-test `unless` appears in a value position — a `let` binding, a `do` body, or a function body
- **THEN** the expression SHALL yield `nil` with the stack balanced, matching the tree-walker

#### Scenario: set! mutates the lexical owner

- **WHEN** a closure invoked repeatedly applies `set!` to a binding owned by an enclosing scope
- **THEN** the owning scope's binding SHALL be updated, and the state SHALL persist across invocations exactly as under the tree-walker

#### Scenario: set! on an undefined binding errors

- **WHEN** `set!` targets a symbol with no existing binding in any enclosing scope
- **THEN** the VM SHALL return a typed error and SHALL NOT create a new binding

#### Scenario: Locals after try/catch keep correct slots

- **WHEN** a function binds locals after a `try`/`catch` form, on both the normal path and the error path
- **THEN** those locals SHALL hold their own values, with no slot-layout corruption from the catch binding

### Requirement: Bytecode VM concurrency safety

The bytecode evaluator SHALL support concurrent `Eval` calls on a single engine
without data races or cross-call state corruption.

#### Scenario: Concurrent evaluation

- **WHEN** multiple goroutines call `Eval` concurrently on one `WithBytecode()` engine
- **THEN** each SHALL return the correct result and `go test -race` SHALL report no data race

### Requirement: Bytecode VM robustness

The bytecode VM SHALL never panic on any input — valid source, a malformed form, or
a structurally malformed chunk; it SHALL return an error instead. Every error the
VM returns SHALL be a `*core.LispicoError`. For every special form the Compiler
handles, arity and shape SHALL be validated before any operand is indexed, so no
malformed special form can panic compilation.

#### Scenario: Empty-body function

- **WHEN** an empty-body function such as `((fn []))` or an empty-body `defn` is evaluated under `WithBytecode()`
- **THEN** the VM SHALL return an error, never panic

#### Scenario: Malformed chunk

- **WHEN** an opcode references an out-of-range stack slot or constant index
- **THEN** the VM SHALL return a `*core.LispicoError`, never index out of range

#### Scenario: Max call depth is a typed error

- **WHEN** VM execution exceeds the maximum call depth
- **THEN** the returned error SHALL satisfy `errors.As(err, &lispicoErr)` like every other VM error

#### Scenario: Malformed special form is a typed error

- **WHEN** any compiled special form is given too few, too many, or wrongly shaped operands and evaluated through `Engine.Eval` under `WithBytecode()`
- **THEN** the result SHALL be a typed error from validation performed before operand indexing, never a panic

### Requirement: Bytecode VM tree-walker parity verification

A cross-validation corpus SHALL exercise both evaluators on the same programs and
assert identical results, and the runtime SHALL be tested end to end through
`WithBytecode()`.

#### Scenario: Cross-validation corpus passes

- **WHEN** the cross-validation corpus (all compiled special forms, closures, variadics, macros, `loop`/`recur`, `try`/`catch`/`throw` with a non-String throw, and empty-body functions) runs through both evaluators
- **THEN** every program SHALL produce equal results or the same class of error under both

#### Scenario: Runtime integration through WithBytecode

- **WHEN** the corpus is driven through `runtime.New(..., WithBytecode())`, including sequential and concurrent (`-race`) evaluation
- **THEN** all cases SHALL pass with no data race

### Requirement: Native arithmetic and comparison opcodes

The VM SHALL execute `+`, `-`, `*`, `/`, `<`, `>`, `<=`, `>=`, `=` through
dedicated opcodes operating on stack slots, with semantics identical to the stdlib
builtins including int/float promotion and division-by-zero errors. When the
operator symbol is locally shadowed or its global binding is no longer the
canonical stdlib builtin, execution SHALL fall back to the ordinary call path.

#### Scenario: Hot loop avoids builtin dispatch

- **WHEN** a `loop` body evaluates `(+ acc 1)` under the VM
- **THEN** the addition SHALL execute as an opcode without a `GoFunc` invocation, and the loop result SHALL equal the tree-walker's

#### Scenario: Promotion parity

- **WHEN** `(+ 1 2.5)` and `(< 1 1.5)` run under the VM
- **THEN** results SHALL equal the stdlib builtins' results (`3.5`, `true`)

#### Scenario: Rebound operator falls back

- **WHEN** a program rebinds `+` to a custom function and then calls `(+ 1 2)` under the VM
- **THEN** the custom function SHALL be called, matching tree-walker behavior

### Requirement: Slot-resident locals

The compiler SHALL determine which locals are captured by inner closures; locals
that are not captured SHALL live only in stack slots, with no per-call `Env`
allocation or write-mirroring for them. Captured variables SHALL remain visible to
their closures with unchanged semantics.

#### Scenario: Uncaptured locals allocate no environment

- **WHEN** a function whose locals are never captured is called in a hot loop under the VM
- **THEN** the call SHALL not allocate an `Env` map for those locals

#### Scenario: Captured variable still works

- **WHEN** a closure captures a local and is called after the defining frame returns
- **THEN** the captured value SHALL be correct, matching the tree-walker

### Requirement: Compiled-chunk cache

The runtime SHALL cache compiled chunks per Engine, keyed by source, dialect, and
macro-definition epoch. A cache hit SHALL skip macro expansion and compilation.
Defining or redefining a macro SHALL invalidate affected entries, so a stale chunk
never runs an outdated expansion. The cache SHALL be bounded: its entry count SHALL
NOT grow without limit over the Engine's lifetime. Entries orphaned by a macro-epoch
bump SHALL be reclaimed, and the cache SHALL enforce the Engine's configured
chunk-cache-size ceiling, so a long-lived Engine that evaluates many distinct
sources or repeatedly redefines macros stays within its memory budget.

#### Scenario: Repeated evaluation reuses the chunk

- **WHEN** the same source is evaluated twice on one Engine under the VM
- **THEN** the second evaluation SHALL not recompile and SHALL return the same result

#### Scenario: Macro redefinition invalidates

- **WHEN** source using macro `m` is evaluated, `m` is redefined, and the same source is evaluated again
- **THEN** the second evaluation SHALL reflect the new definition of `m`

#### Scenario: Cache does not grow without bound

- **WHEN** an Engine repeatedly evaluates distinct sources and redefines macros far beyond the chunk-cache-size ceiling
- **THEN** the cache entry count SHALL stay at or below the configured ceiling, and results SHALL remain correct for whatever is evaluated next

### Requirement: Dialect-axis execution

The VM SHALL honor the Engine's dialect: form names normalized to canonical kernel
forms before compilation, truthiness decided through the dialect's truthiness rule,
head-position symbol resolution through the function cell under Lisp-2, and special
forms with a dialect-owned Form-shape rule (`cond` clause shape first) compiled from
the same canonical structure the Evaluator dispatches on. Any resolvable dialect
SHALL be VM-eligible.

#### Scenario: CL dialect runs on the VM

- **WHEN** an Engine is created with the default CL dialect and `WithBytecode()`, and evaluates `(progn (setq x 1) (if nil 2 x))`
- **THEN** construction SHALL succeed and the result SHALL be `1`, matching the tree-walker

#### Scenario: Truthiness axis honored

- **WHEN** a nil-only-falsy dialect evaluates `(if false 1 2)` under the VM
- **THEN** the result SHALL be `1`, because `false` is truthy on that axis

#### Scenario: Restricted dialect runs on the VM

- **WHEN** a fail-closed dialect built from the empty base with a form subset runs a program using only its forms under the VM
- **THEN** the program SHALL evaluate correctly, and forms outside the subset SHALL remain undefined

#### Scenario: Both cond clause shapes compile

- **WHEN** a Clojure-dialect Engine compiles a flat-pair `cond` and a CL-dialect Engine compiles a nested-clause `cond` under `WithBytecode()`
- **THEN** both SHALL compile from the dialect's canonical clauses and return results equal to the tree-walker's

