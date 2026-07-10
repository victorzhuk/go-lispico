# bytecode-vm — delta

## MODIFIED Requirements

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
