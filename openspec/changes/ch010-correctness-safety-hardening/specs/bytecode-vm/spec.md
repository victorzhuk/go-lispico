# Technical Specification

## MODIFIED Requirements

### Requirement: Bytecode VM execution

The bytecode VM SHALL be an opt-in evaluator, selectable with
`runtime.WithBytecode()`, that produces results identical to the tree-walking
evaluator for every program it compiles. It is a documented subset: for a form it
does not compile it SHALL return a typed error, and the runtime SHALL fall back to
the tree-walking evaluator for that form — never panicking, and never producing a
result that differs from the tree-walker. Each `Eval` call SHALL compile and run in
isolation, with no state carried over from a prior call. The VM has no on-disk
bytecode cache.

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
- **THEN** the second evaluation SHALL return its own result, with no instructions or stack state left over from the first

### Requirement: Bytecode VM robustness

The bytecode VM SHALL never panic on any input — valid source, a malformed form, or
a structurally malformed chunk; it SHALL return an error instead.

#### Scenario: Empty-body function

- **WHEN** an empty-body function such as `((fn []))` or an empty-body `defn` is evaluated under `WithBytecode()`
- **THEN** the VM SHALL return an error, never panic

#### Scenario: Malformed chunk

- **WHEN** an opcode references an out-of-range stack slot or constant index
- **THEN** the VM SHALL return a `*core.LispicoError`, never index out of range

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
