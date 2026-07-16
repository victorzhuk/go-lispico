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
