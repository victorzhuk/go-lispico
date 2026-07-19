# runtime-api Specification

## Purpose

The runtime-api capability provides the public Go embedding API functionality for the system, registered and made ready for use when the system initializes.
## Requirements
### Requirement: runtime-api implementation
The system SHALL implement the runtime-api functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the runtime-api SHALL be ready for use

### Requirement: Configuration options behave as documented

Runtime options SHALL take effect as their documentation states, or SHALL be
removed rather than shipped as no-ops. `WithTimeout` SHALL bound `Eval` and
`EvalWithBindings`, not only `Call`. `Watch` SHALL stop when the context passed to
it is cancelled. Options that cannot be honored — an inert `WithBytecodeCache`, a
`WithHotReloadDir` that never watches — SHALL be removed.

#### Scenario: WithTimeout bounds Eval

- **WHEN** an engine is built with `WithTimeout(d)` and an `Eval` runs longer than `d`
- **THEN** the evaluation SHALL be cancelled with a deadline error

#### Scenario: Watch honors its context

- **WHEN** `Watch(ctx, dir)` is called and `ctx` is later cancelled
- **THEN** the watcher SHALL stop without a separate `Stop()` call

#### Scenario: No option is a silent no-op

- **WHEN** the public option set is enumerated
- **THEN** every option SHALL either change behavior as documented or be absent

### Requirement: WithDialect Engine option
The runtime SHALL provide a `WithDialect` construction option that selects the Dialect an Engine runs. The option SHALL be applied once at `New` and SHALL compose with the existing construction options, including the bytecode evaluator: any resolvable Dialect SHALL be accepted alongside `WithBytecode()`.

#### Scenario: Selecting a Dialect at construction
- **WHEN** an Engine is created with `WithDialect` set to a given Dialect
- **THEN** the Engine SHALL evaluate source using that Dialect's effective special-form table

#### Scenario: Omitting the option
- **WHEN** an Engine is created without `WithDialect`
- **THEN** the Engine SHALL run the default Dialect, preserving prior behavior until the default is changed by a later change

#### Scenario: Bytecode composes with any Dialect
- **WHEN** an Engine is created with both `WithBytecode()` and a non-identity Dialect (the default CL, or a restricted dialect)
- **THEN** construction SHALL succeed and evaluation SHALL honor the Dialect's forms and axes on the bytecode path

#### Scenario: Unresolvable Dialect fails construction
- **WHEN** an Engine is created with a Dialect whose delta references a canonical form absent from the kernel
- **THEN** construction SHALL return an error rather than a partially-resolved Engine

### Requirement: Default dialect is Common Lisp
An Engine created via `runtime.New()` without a `WithDialect` option SHALL run the Common Lisp dialect. Embedders requiring the prior surface SHALL select it explicitly with `WithDialect(clojure.Dialect())`.

#### Scenario: Zero-config Engine speaks Common Lisp
- **WHEN** an Engine is created with no dialect option
- **THEN** it SHALL evaluate source using the Common Lisp dialect

#### Scenario: Prior surface available by explicit selection
- **WHEN** an Engine is created with `WithDialect(clojure.Dialect())`
- **THEN** it SHALL reproduce the interpreter's behavior prior to the default flip

### Requirement: UnloadPlugin removes the plugin's bindings

`UnloadPlugin` SHALL delete every binding the plugin registered into the root
environment, in addition to unregistering it from the registry. `ReloadPlugin`
SHALL clear the old bindings before re-running `Init`.

#### Scenario: Unloaded function becomes undefined

- **WHEN** a plugin registering `json/encode` is loaded, then `UnloadPlugin` is called for it, then `(json/encode "hi")` is evaluated
- **THEN** evaluation SHALL fail with an `UndefinedError`

#### Scenario: Reload does not stack stale bindings

- **WHEN** `ReloadPlugin` is called for a loaded plugin
- **THEN** the environment SHALL contain exactly the bindings from the fresh `Init`, with no leftovers from the previous load

### Requirement: REPL input balancing ignores comments

The REPL's continuation check SHALL treat `;` to end of line as a comment, per the
reader's comment rule, when deciding whether input is a complete form.

#### Scenario: Trailing comment with unbalanced paren

- **WHEN** the REPL receives the line `(+ 1 2) ; note (`
- **THEN** it SHALL evaluate the form and print `3` instead of waiting for a closing paren

### Requirement: ResourceLimits Engine option

The runtime SHALL provide a construction option that sets a `ResourceLimits` value
carrying the reader nesting-depth, evaluator structural-depth, collection-length,
and chunk-cache-size ceilings. The option SHALL be applied once at `New` and SHALL
be immutable for the Engine's lifetime, so evaluated code cannot raise its own
ceilings. When the option is omitted, or a field is left at its zero value, the
Engine SHALL apply a conservative built-in default for that ceiling — the absence
of a limit SHALL mean "use the default", never "unlimited". The limits SHALL be
carried into the reader, the evaluator, and the stdlib so each enforcement point
uses the Engine's configured value.

#### Scenario: Configured limit takes effect

- **WHEN** an Engine is created with a `ResourceLimits` that lowers the reader nesting-depth ceiling and then reads source nested past that ceiling
- **THEN** `Read`/`Eval` SHALL fail with the depth-limit error at the configured ceiling

#### Scenario: Omitted option applies safe defaults

- **WHEN** an Engine is created with no `ResourceLimits` option and is given adversarially deep input
- **THEN** the Engine SHALL still fail closed at its default ceilings rather than crashing the process

#### Scenario: Limits are immutable after construction

- **WHEN** an Engine is running and evaluated code attempts to change any ceiling
- **THEN** no evaluation path SHALL be able to raise a ceiling, and the Engine SHALL enforce the value fixed at `New`

### Requirement: Evaluation deadline ownership

The Engine SHALL apply a safe default evaluation deadline of 30 seconds to `Eval`,
`EvalWithBindings`, and `Call`. When the caller's context already carries a
deadline at or earlier than the Engine's, the Engine SHALL NOT create its own
bound — the caller's deadline governs. When the caller's deadline is later, the
Engine's tighter bound SHALL still apply. `WithTimeout(0)` SHALL disable the
Engine deadline entirely, leaving the caller's context as the only bound; this is
intended for embedders that apply a deadline to every evaluation lifecycle
themselves (ADR 0010). The Engine deadline SHALL be enforced by bounded-interval
checks during evaluation, without allocating a timer or derived context per
call; `GoFunc` implementations receive the caller's context, so a `GoFunc`
blocking on external work is bounded by the caller's context, not interrupted
mid-call by the Engine deadline.

#### Scenario: Default deadline applies

- **WHEN** an Engine is constructed without `WithTimeout` and an evaluation runs longer than 30 seconds
- **THEN** the evaluation SHALL be cancelled with a deadline error

#### Scenario: Earlier caller deadline governs alone

- **WHEN** the caller's context deadline is earlier than the Engine's configured timeout
- **THEN** the evaluation SHALL be bounded by the caller's deadline and the Engine SHALL NOT layer a second bound

#### Scenario: Later caller deadline is tightened

- **WHEN** the caller's context deadline is later than the Engine's configured timeout
- **THEN** the evaluation SHALL be bounded by the Engine's timeout

#### Scenario: Explicit disablement

- **WHEN** an Engine is constructed with `WithTimeout(0)`
- **THEN** the Engine SHALL impose no deadline of its own and the caller's context SHALL be the only cancellation source

#### Scenario: No per-call timer allocation

- **WHEN** `Eval` or `Call` runs on an Engine with the default timeout and a caller context without a deadline
- **THEN** the Engine SHALL NOT allocate a timer or a derived deadline context for that call, and the deadline SHALL still be enforced by in-evaluation checks

### Requirement: Boundary call efficiency

On a `WithBytecode()` engine, a repeated `Call` of an already-defined function
SHALL NOT allocate per-call boundary scaffolding: no derived context or timer,
no synthesized chunk, and no fresh VM. When the function body dispatches no
re-entrant call, the `Call` SHALL additionally allocate no evaluation-state
value, leaving per-call allocations limited to argument/result value
representation. When the body dispatches a `GoFunc` that may re-enter the
evaluator, the `Call` MAY allocate at most one evaluation-state value, reused
for the remainder of that `Call`, whose sole purpose is to carry the shared
structural-depth and deadline budget across the boundary. A re-entrant `Call`
— a `GoFunc` invoking `Call` again on the same engine with the context it
received — SHALL share the enclosing call's structural-depth and deadline
budget rather than starting a fresh one. `Stats()` SHALL remain accurate
whether or not callbacks are registered, and registered `OnPluginCall`/`OnEval`
callbacks SHALL keep firing with durations as today.

#### Scenario: Non-dispatching Call allocates only value representation

- **WHEN** `Call` repeatedly invokes a compiled function whose body dispatches no further call (a selector or leaf body) on a `WithBytecode()` engine with no callbacks registered
- **THEN** per-call allocations SHALL be limited to argument/result value representation, with no context, timer, eval-state, chunk, or VM allocation

#### Scenario: Re-entrant body shares one evaluation-state

- **WHEN** a compiled function whose body dispatches a `GoFunc` that re-enters the evaluator is invoked through `Call`
- **THEN** at most one evaluation-state value SHALL be allocated for that `Call` and reused for its remainder, and the `GoFunc`'s re-entry SHALL enforce the same structural-depth and deadline budget as the enclosing `Call`

#### Scenario: Nested Call shares the enclosing resource budget

- **WHEN** a `GoFunc` invoked during a `Call` itself invokes `Call` on the same engine, forwarding the context it received
- **THEN** the nested `Call` SHALL count structural depth against the enclosing call's budget rather than a fresh one, so the combined nesting still trips the configured `MaxStructuralDepth`

#### Scenario: Stats stay accurate without callbacks

- **WHEN** `Call` runs N times with no callbacks registered
- **THEN** `Stats()` SHALL report N calls for that function

#### Scenario: Callbacks unchanged when registered

- **WHEN** an `OnPluginCall` callback is registered and `Call` runs
- **THEN** the callback SHALL fire with the function name and a measured duration, as before

