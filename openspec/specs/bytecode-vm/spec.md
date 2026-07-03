# bytecode-vm Specification

## Purpose

The bytecode VM is an experimental, opt-in evaluator (`runtime.WithBytecode()`)
that compiles forms to bytecode chunks and executes them on a stack machine, with
optional on-disk caching of compiled bytecode. It currently executes a subset of
the language; the tree-walking evaluator is the supported default.
## Requirements
### Requirement: Bytecode VM execution

The bytecode VM SHALL be a full-language evaluator, selectable with
`runtime.WithBytecode()`, that produces results identical to the tree-walking
evaluator for every program the tree-walker accepts. It SHALL support all 22
special forms, variadic functions, closures, and macros. Each `Eval` call SHALL
compile and run in isolation, with no state carried over from a prior call.

#### Scenario: All special forms match the tree-walker

- **WHEN** the VM evaluates any of `if`, `def`, `defn`, `defmacro`, `fn`, `let`, `let*`, `do`, `quote`, `quasiquote`, `set!`, `when`, `unless`, `cond`, `loop`, `recur`, `try`, `catch`, `throw`, `and`, `or`, `not`
- **THEN** the result SHALL equal the tree-walking evaluator's result for the same form and environment

#### Scenario: loop/recur iterates in constant stack

- **WHEN** a `loop`/`recur` runs 10,000 iterations
- **THEN** execution SHALL complete without growing the Go stack and SHALL return the same value as the tree-walker

#### Scenario: try/catch/throw handles errors

- **WHEN** a `try` body throws a value or a called `GoFunc` returns an error, and a `catch` clause is present
- **THEN** the caught value SHALL be bound to the catch symbol and the handler result SHALL match the tree-walker

#### Scenario: Macros expand before compilation

- **WHEN** a macro is defined with `defmacro` and then used
- **THEN** the macro SHALL be expanded prior to compilation and the result SHALL match the tree-walker

#### Scenario: Variadic functions bind rest arguments

- **WHEN** a variadic `fn` is applied with more arguments than fixed parameters
- **THEN** the rest arguments SHALL be bound as a list, matching `Env.ChildVariadic`

#### Scenario: Each evaluation is isolated

- **WHEN** two forms are evaluated in sequence on the same engine
- **THEN** the second evaluation SHALL return its own result, with no instructions or stack state left over from the first

#### Scenario: Bytecode cache hit

- **WHEN** `runtime.WithBytecodeCache(dir)` is set and the same source content is compiled twice
- **THEN** the second load SHALL read version-gated cached bytecode without recompiling

### Requirement: Bytecode VM concurrency safety

The bytecode evaluator SHALL support concurrent `Eval` calls on a single engine
without data races or cross-call state corruption.

#### Scenario: Concurrent evaluation

- **WHEN** multiple goroutines call `Eval` concurrently on one `WithBytecode()` engine
- **THEN** each SHALL return the correct result and `go test -race` SHALL report no data race

### Requirement: Bytecode VM robustness

The bytecode VM SHALL never panic on malformed, corrupt, or version-mismatched
bytecode; it SHALL return an error instead.

#### Scenario: Corrupt cache entry

- **WHEN** a `.lbc` cache entry is corrupt or was written by an incompatible `cacheVersion`
- **THEN** the VM SHALL return a graceful error and fall back to recompilation, never panicking

### Requirement: Bytecode VM tree-walker parity verification

A cross-validation corpus SHALL exercise both evaluators on the same programs and
assert identical results, and the runtime SHALL be tested end to end through
`WithBytecode()`.

#### Scenario: Cross-validation corpus passes

- **WHEN** the cross-validation corpus (all special forms, closures, variadics, macros, `loop`/`recur`, `try`/`catch`/`throw`) runs through both evaluators
- **THEN** every program SHALL produce equal results or the same class of error under both

#### Scenario: Runtime integration through WithBytecode

- **WHEN** the corpus is driven through `runtime.New(..., WithBytecode())`, including cache-hit and hot-reload paths
- **THEN** all cases SHALL pass

