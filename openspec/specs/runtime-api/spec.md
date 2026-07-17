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
timer — the caller's deadline governs. When the caller's deadline is later, the
Engine's tighter bound SHALL still apply. `WithTimeout(0)` SHALL disable the
Engine deadline entirely, leaving the caller's context as the only bound; this is
intended for embedders that apply a deadline to every evaluation lifecycle
themselves (ADR 0010).

#### Scenario: Default deadline applies

- **WHEN** an Engine is constructed without `WithTimeout` and an evaluation runs longer than 30 seconds
- **THEN** the evaluation SHALL be cancelled with a deadline error

#### Scenario: Earlier caller deadline governs alone

- **WHEN** the caller's context deadline is earlier than the Engine's configured timeout
- **THEN** the evaluation SHALL be bounded by the caller's deadline and the Engine SHALL NOT layer a second timer

#### Scenario: Later caller deadline is tightened

- **WHEN** the caller's context deadline is later than the Engine's configured timeout
- **THEN** the evaluation SHALL be bounded by the Engine's timeout

#### Scenario: Explicit disablement

- **WHEN** an Engine is constructed with `WithTimeout(0)`
- **THEN** the Engine SHALL impose no deadline of its own and the caller's context SHALL be the only cancellation source

