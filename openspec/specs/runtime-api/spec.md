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

On an engine running the bytecode evaluator, a repeated `Call` or handle call of
an already-defined function SHALL NOT allocate per-call boundary scaffolding: no
derived context or timer, no synthesized chunk, and no fresh VM. When the
function body dispatches no re-entrant call, the call SHALL additionally
allocate no evaluation-state value, leaving per-call allocations limited to
argument/result value representation. When the body dispatches a `GoFunc` that
may re-enter the evaluator, the call MAY allocate at most one evaluation-state
value, reused for the remainder of that call, whose sole purpose is to carry
the shared structural-depth and deadline budget across the boundary. A
re-entrant `Call` — a `GoFunc` invoking `Call` again on the same engine with
the context it received — SHALL share the enclosing call's structural-depth
and deadline budget rather than starting a fresh one. When no `OnPluginCall`
or `OnEval` callback is registered, a boundary call SHALL NOT read the wall
clock except as required to enforce an armed evaluation deadline; the engine
deadline SHALL be armed lazily at the first in-evaluation checkpoint, so a
call completing before that checkpoint reads no clock at all. `Stats()` SHALL
remain accurate whether or not callbacks are registered, and registered
`OnPluginCall`/`OnEval` callbacks SHALL keep firing with durations as today.

#### Scenario: Non-dispatching Call allocates only value representation

- **WHEN** `Call` repeatedly invokes a compiled function whose body dispatches no further call (a selector or leaf body) on a bytecode engine with no callbacks registered
- **THEN** per-call allocations SHALL be limited to argument/result value representation, with no context, timer, eval-state, chunk, or VM allocation

#### Scenario: Re-entrant body shares one evaluation-state

- **WHEN** a compiled function whose body dispatches a `GoFunc` that re-enters the evaluator is invoked through `Call`
- **THEN** at most one evaluation-state value SHALL be allocated for that `Call` and reused for its remainder, and the `GoFunc`'s re-entry SHALL enforce the same structural-depth and deadline budget as the enclosing `Call`

#### Scenario: Nested Call shares the enclosing resource budget

- **WHEN** a `GoFunc` invoked during a `Call` itself invokes `Call` on the same engine, forwarding the context it received
- **THEN** the nested `Call` SHALL count structural depth against the enclosing call's budget rather than a fresh one, so the combined nesting still trips the configured `MaxStructuralDepth`

#### Scenario: Unobserved calls read no clock

- **WHEN** no callback is registered and a short call completes before the first cancellation checkpoint
- **THEN** the boundary SHALL perform no wall-clock read for that call, and the engine deadline SHALL still bound longer evaluations once armed at a checkpoint

#### Scenario: Stats stay accurate without callbacks

- **WHEN** `Call` runs N times with no callbacks registered
- **THEN** `Stats()` SHALL report N calls for that function

#### Scenario: Callbacks unchanged when registered

- **WHEN** an `OnPluginCall` callback is registered and `Call` runs
- **THEN** the callback SHALL fire with the function name and a measured duration, as before

### Requirement: Default evaluator selection

An Engine constructed without evaluator options SHALL run the bytecode
evaluator with per-form tree-walker fallback. `WithTreeWalker()` SHALL select
the tree-walking evaluator as the sole execution path; `WithBytecode()` SHALL
select the bytecode evaluator explicitly. When both options are passed, the
last one in argument order SHALL win. Both evaluators SHALL remain available
and fully supported; selecting either SHALL NOT change any evaluation result,
per the VM parity contract.

#### Scenario: Default is bytecode

- **WHEN** an Engine is constructed via `runtime.New(nil)` with no evaluator option
- **THEN** compiled-subset forms SHALL execute on the bytecode VM

#### Scenario: Tree-walker opt-out

- **WHEN** an Engine is constructed with `WithTreeWalker()`
- **THEN** all evaluation SHALL run on the tree-walking evaluator and the bytecode VM SHALL NOT be entered

#### Scenario: Last option wins

- **WHEN** an Engine is constructed with `WithTreeWalker()` followed by `WithBytecode()`, and another with the reverse order
- **THEN** the first SHALL run the bytecode evaluator and the second the tree-walker

#### Scenario: Results identical across evaluators

- **WHEN** the same program runs on a default Engine and on a `WithTreeWalker()` Engine
- **THEN** results and error shapes SHALL be identical

### Requirement: Function handles

The Engine SHALL provide `Func(name)` returning a callable handle that resolves
the named function once at handle creation. `Func` SHALL return an error for an
undefined name. `(*Fn).Call(ctx, args...)` SHALL invoke the current binding of
the resolved name: a rebind after handle creation SHALL be visible to the next
call, and a deletion SHALL make the call return the same undefined-function
error `Engine.Call` returns. Handles SHALL be safe for concurrent use.
`Stats()` SHALL count handle calls under the function's name exactly as named
`Engine.Call`s, and registered `OnPluginCall` callbacks SHALL fire for handle
calls with measured durations. A handle call SHALL NOT re-resolve the name
through a per-call map lookup.

#### Scenario: Handle calls the current binding

- **WHEN** `Func("f")` is taken, `f` is rebound, and the handle is called
- **THEN** the call SHALL invoke the new binding

#### Scenario: Undefined name fails at handle creation

- **WHEN** `Func` is called with a name that has no binding
- **THEN** it SHALL return an error and no handle

#### Scenario: Deleted binding fails at call time

- **WHEN** a handle is taken for `f` and `f` is subsequently deleted
- **THEN** the handle call SHALL return the same undefined-function error a named `Call` of `f` returns

#### Scenario: Concurrent handle calls

- **WHEN** one handle is called from many goroutines concurrently
- **THEN** every call SHALL return a correct result and `go test -race` SHALL report no data race

#### Scenario: Stats and callbacks attribute handle calls

- **WHEN** a handle for `f` is called N times with an `OnPluginCall` callback registered
- **THEN** `Stats()` SHALL report N calls for `f` and the callback SHALL fire N times with durations

### Requirement: Pinned function handles

A function handle SHALL offer a pinned variant (`(*Fn).Pin()`) that owns a
private execution machine and is documented as NOT safe for concurrent use —
one pinned handle per goroutine. A pinned call SHALL be semantically identical
to the shared handle's call: current-binding resolution, undefined-after-delete
error shape, stats attribution, callback events, deadline enforcement, and
re-entrant resource-budget sharing. A pinned call SHALL NOT borrow from or
return to any shared machine pool. Concurrent use of one pinned handle SHALL be
detected on entry and rejected with a typed error — never a panic, never
silent corruption — and SHALL leave the engine's shared state unaffected.
Pinning SHALL be per-handle: the engine and all other handles remain safe for
concurrent use.

#### Scenario: Pinned call behaves like a shared call

- **WHEN** the same function is exercised through `Fn.Call` and through a `PinnedFn` — including rebind, delete, stats, callback, and deadline cases
- **THEN** results, error shapes, stats attribution, and callback events SHALL be identical

#### Scenario: No pool traffic on the pinned path

- **WHEN** a `PinnedFn` is called repeatedly
- **THEN** no execution machine SHALL be fetched from or returned to a shared pool for those calls

#### Scenario: Concurrent misuse is rejected, not corrupted

- **WHEN** two goroutines call one `PinnedFn` concurrently
- **THEN** at least one call SHALL return a typed concurrent-use error, no call SHALL panic, `go test -race` SHALL report no data race, and subsequent single-goroutine calls SHALL behave correctly

#### Scenario: Independent pins run concurrently

- **WHEN** two `PinnedFn`s obtained from the same `Fn` are called from two goroutines
- **THEN** both SHALL execute correctly and independently

### Requirement: Deferred plugin binding materialization

Plugin load MAY defer binding creation behind a process-level template: names
materialize into the engine's root environment on first resolution — value
cell, function cell, or macro lookup — with results, errors, canonical-operator
status, and macro expansion byte-identical to an eagerly loaded engine.
Materialization SHALL be at-most-once per name per engine and safe under
concurrent first resolution, including definitions whose execution resolves
other unmaterialized names. Surfaces that enumerate bindings SHALL observe the
full plugin surface (forcing materialization as needed). `UnloadPlugin` SHALL
remove both the deferred template and every binding it materialized. A user
definition shadowing a deferred name SHALL win exactly as it wins over an
eager binding, and deleting bindings SHALL behave as it would on an eagerly
loaded engine — deferral SHALL never resurrect a deleted name.

#### Scenario: First use is identical to eager load

- **WHEN** a fresh engine with deferred stdlib evaluates a program using arithmetic, collection functions, and a stdlib macro
- **THEN** results, errors, and native-operator execution SHALL be identical to the same engine loaded eagerly

#### Scenario: Concurrent first touch materializes once

- **WHEN** many goroutines concurrently first-resolve the same deferred name and names whose definitions depend on other deferred names
- **THEN** each name SHALL materialize exactly once, no evaluation SHALL deadlock, and `go test -race` SHALL report no data race

#### Scenario: Enumeration sees the full surface

- **WHEN** a surface that lists plugin bindings runs on an engine where nothing has been resolved yet
- **THEN** it SHALL report the same bindings an eagerly loaded engine reports

#### Scenario: Unload removes deferred and materialized bindings

- **WHEN** `UnloadPlugin` runs after some deferred names were materialized and others were not
- **THEN** neither materialized nor unmaterialized plugin names SHALL resolve afterwards

#### Scenario: Deletion is not undone by deferral

- **WHEN** a deferred stdlib name is shadowed by a user definition which is then deleted
- **THEN** subsequent resolution SHALL behave exactly as on an eagerly loaded engine after the same operations

### Requirement: Evaluation reductions and cumulative allocation are metered

The runtime SHALL extend `ResourceLimits` with `MaxReductions` and
`MaxAllocationBytes` fields. Each SHALL default to a conservative value
(10,000,000 reductions and 64 MiB per evaluation) when left at zero — never
unlimited. Each evaluation SHALL carry a per-call reduction counter and a
per-call cumulative allocation counter, threaded through the same `evalState`
used for structural depth, so every evaluation path — including direct
`core.Evaluator.Apply` calls that bypass `Engine` methods — is metered.
Reader output SHALL be charged to the evaluation ledger after reading and
before the first form evaluates. Exceeding either ceiling SHALL raise a
`*core.LispicoError` with `Code: "ResourceLimitError"` that is terminal per
the core-engine non-catchability requirement. The evaluator SHALL observe the
caller's context cancellation at least every 1,024 reductions. Ceiling
enforcement MAY be batched with the cancellation countdown (bounded slack of
one batch, at most 128 reductions). Counters SHALL NOT be shared across
concurrent evaluations on the same engine. Allocation charges SHALL use a
fixed, architecture-independent, documented per-type size table.

#### Scenario: Tight allocation loop fails closed

- **WHEN** an Engine runs a loop that allocates faster than it reduces, configured with `MaxAllocationBytes: 1<<20`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` before the host is exhausted, and `try`/`catch` SHALL NOT intercept the error

#### Scenario: Reduction-amplified macro recursion fails closed

- **WHEN** an Engine runs a macro-amplified recursion that exceeds `MaxReductions` before tripping `MaxDepth`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: GoFunc-built values are charged

- **WHEN** a loop concatenates strings through a stdlib GoFunc until shallow result sizes exceed `MaxAllocationBytes`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` without per-plugin instrumentation

#### Scenario: Reader output is charged before evaluation

- **WHEN** source containing a flat literal whose parsed size exceeds `MaxAllocationBytes` is evaluated
- **THEN** the call SHALL fail with `Code: "ResourceLimitError"` before the first form's evaluation begins

#### Scenario: Context observed within the reduction budget

- **WHEN** the caller's context is cancelled mid-evaluation
- **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation

#### Scenario: Per-evaluation counters are isolated

- **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race`
- **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race

#### Scenario: Defaults match the embedder contract

- **WHEN** an Engine is constructed with no `MaxReductions` / `MaxAllocationBytes` and adversarial input runs
- **THEN** the defaults (10M reductions / 64 MiB allocation per evaluation) SHALL apply, never "unlimited"

### Requirement: Retained state is charged by owned capacity

`ResourceLimits` SHALL carry `MaxRetainedBytesPerEnv` and
`MaxRetainedSlotsPerEnv` fields, defaulting to 32 MiB and 100,000 when left
at zero. Every env on the engine path SHALL carry an owned-capacity counter
(bytes + slots) with limit configuration inherited from its parent chain.
Binding a new name SHALL charge the fixed-table backing cost plus the value's
shallow size and one slot, and SHALL raise a
`*core.LispicoError{Code: "ResourceLimitError"}` — leaving prior bindings
intact — if the new total would exceed either ceiling. Rebinding through an
existing binding and reviving a tombstoned binding SHALL NOT charge. Deleting
a binding SHALL tombstone the slot without releasing backing or decrementing
counters. The runtime SHALL provide `(*Env).RetainedUsage() (bytes, slots
int64)` and `(*Env).Rebuild()`; `Rebuild` SHALL compact in place — same
`*Env` identity, live `*Cell` pointers preserved, tombstoned cells dropped,
counters recomputed, name generation bumped — and is the only path that
releases dead backing. The runtime SHALL provide `Engine.LoadScope(ctx,
source, bindings) (core.Value, *core.Env, error)` returning the retained
child scope with `EvalWithBindings` evaluation semantics. Capturing an env
through a closure SHALL NOT transfer or double-count ownership; values charge
shallow backing only.

#### Scenario: Slot ceiling fails closed

- **WHEN** an env with `MaxRetainedSlotsPerEnv: 5` receives a sixth new binding
- **THEN** the write SHALL fail with `Code: "ResourceLimitError"` and the prior five bindings SHALL remain intact

#### Scenario: Byte ceiling fails closed

- **WHEN** an env's retained bytes would exceed `MaxRetainedBytesPerEnv` on a new binding
- **THEN** the write SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Rebind does not charge

- **WHEN** an existing binding is rebound to a new value
- **THEN** the env's slot and byte counters SHALL NOT increase

#### Scenario: Delete tombstones but does not release

- **WHEN** a binding is deleted and `RetainedUsage` is then read
- **THEN** the counters SHALL be unchanged from before the delete

#### Scenario: Rebuild releases dead capacity and preserves live cells

- **WHEN** an env has had many bindings added, most deleted, and `Rebuild` is called while a caller holds a `*Cell` for a live binding
- **THEN** `RetainedUsage` SHALL equal the live binding set's backing, the old maps SHALL be garbage-collectable, and the held cell SHALL still serve the live binding

#### Scenario: LoadScope returns the retained scope

- **WHEN** a host calls `LoadScope` with source that defines handler closures
- **THEN** the returned `*core.Env` SHALL be the scope those closures captured, and `RetainedUsage` on it SHALL report the load's retained backing

#### Scenario: Closure capture does not double-count

- **WHEN** a `Lambda` captures an env and the env's counters are later inspected
- **THEN** the captured env's counters SHALL be the same as before the capture

### Requirement: Meter interface with engine-side lease amortization

The runtime SHALL define a `Meter` interface — `LeaseEval(reductions,
allocBytes int64) (int64, int64, error)`, `ReturnEval(reductions, allocBytes
int64)`, `ChargeRetained(bytes, slots int64) error`, `ReleaseRetained(bytes,
slots int64)` — that the engine consumes and the embedder implements. The
engine SHALL NOT define ledger ranks or composition; it SHALL draw compute
credit in leases of at most 1,024 reductions and 64 KiB per call, hold at
most one active compute lease per evaluation, re-lease when a grant is
exhausted, and return the unconsumed remainder exactly once on evaluation end
including error unwind. A zero grant with error SHALL terminate the
evaluation with `Code: "ResourceLimitError"` (terminal). A meter SHALL be
attachable per evaluation via `runtime.WithMeter(ctx, m)` — honored on every
evaluation path including direct `core.Evaluator.Apply` — and per engine via
the `runtime.WithEngineMeter(m)` option, which also meters engine setup
(`New` dialect bootstrap and `Use` plugin bootstrap); a ctx meter SHALL
override the engine meter. At evaluation end the engine SHALL settle
persistent-scope retained deltas through `ChargeRetained` and SHALL credit
`Rebuild`-freed capacity through `ReleaseRetained`. Meter state SHALL NOT be
observable from evaluated Lisp code. Absent any meter, behavior SHALL be
unchanged. The runtime SHALL ship a no-op meter and a flat threshold meter
(`NewLimitMeter`); `Meter` implementations MUST be safe for concurrent use.

#### Scenario: Exhausted meter fails closed mid-evaluation

- **WHEN** a meter's compute credit runs out while an evaluation is drawing its next lease
- **THEN** the evaluation SHALL fail with `Code: "ResourceLimitError"`, `try`/`catch` SHALL NOT intercept it, and the engine SHALL NOT retry the lease

#### Scenario: Unconsumed credit returns on end and on unwind

- **WHEN** an evaluation granted N reductions consumes M < N and then completes — normally or by error
- **THEN** the meter SHALL receive exactly one `ReturnEval` crediting the unconsumed remainder

#### Scenario: Context meter overrides engine meter

- **WHEN** an engine has `WithEngineMeter(a)` and a call carries `WithMeter(ctx, b)`
- **THEN** the evaluation SHALL draw from `b` only

#### Scenario: Engine setup is metered

- **WHEN** an engine is constructed with `WithEngineMeter` and `Use` loads a plugin whose bootstrap exceeds the meter's credit
- **THEN** `Use` SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Retained delta settles at evaluation end

- **WHEN** a metered `LoadScope` call defines bindings that retain bytes and slots
- **THEN** the meter SHALL receive one `ChargeRetained` with the load scope's retained delta at evaluation end

#### Scenario: Direct Apply path draws from the ctx meter

- **WHEN** a host invokes a handler via `core.Evaluator.Apply` with a ctx carrying `WithMeter`
- **THEN** the evaluation SHALL draw leases from that meter

#### Scenario: Meter is invisible to rules

- **WHEN** evaluated code enumerates its environment and probes for meter state
- **THEN** no binding, form, or plugin SHALL expose budgets, grants, or counters

#### Scenario: Absent meter preserves existing behavior

- **WHEN** an engine entry point is called with no ctx meter and no engine meter
- **THEN** charges SHALL flow to the per-evaluation `evalState` ledger exactly as without this change

#### Scenario: Concurrent evaluations share a meter race-free

- **WHEN** multiple goroutines evaluate under one engine meter concurrently
- **THEN** every lease, return, and settlement SHALL be accounted and `go test -race` SHALL report no data race

### Requirement: Bytecode cache byte and node bounds with deterministic LRU

`ResourceLimits` SHALL carry `MaxCacheBytes` and `MaxCacheNodes` fields,
defaulting to 64 MiB and 1,000,000 when left at zero. Each compiled `Chunk`
SHALL publish a deep byte size (fixed size table; boxed constant payloads
measured structurally; `SubChunks` recursive) and the node count of its
macro-expanded source form, captured at compile time. The per-engine chunk
cache SHALL be a deterministic LRU: recency SHALL update on hit and insert,
and eviction SHALL be strictly least-recently-used, reproducible for
identical operation sequences. Admission SHALL enforce `MaxCacheEntries`,
`MaxCacheBytes`, and `MaxCacheNodes` atomically on insert, evicting LRU
entries until all three hold; one insertion MAY evict multiple entries. A
chunk that alone exceeds any ceiling SHALL NOT be admitted and its
evaluation SHALL proceed uncached without error. When an engine meter is
configured, the cache SHALL charge `ChargeRetained(deepBytes, 1)` on insert
and `ReleaseRetained` on evict, macro-epoch flush, and `Close`; a denied
retained charge SHALL cause the chunk to run uncached, never fail the
evaluation. `EngineStats` SHALL expose cache entries, bytes, nodes, and the
cache epoch. The process-level stdlib bootstrap artifact cache is exempt
from these ceilings.

#### Scenario: Byte ceiling evicts

- **WHEN** the cache is filled with chunks whose combined deep bytes exceed `MaxCacheBytes` while entry count is at or below `MaxCacheEntries`
- **THEN** the cache SHALL evict least-recently-used entries until combined bytes are under `MaxCacheBytes`

#### Scenario: Node ceiling evicts

- **WHEN** the cache holds chunks whose combined node count exceeds `MaxCacheNodes`
- **THEN** the cache SHALL evict least-recently-used entries until combined nodes are under `MaxCacheNodes`

#### Scenario: All three ceilings enforced on one insert

- **WHEN** inserting a chunk whose addition crosses more than one ceiling
- **THEN** the cache SHALL evict enough least-recently-used entries to bring all three back under their ceilings in one insertion

#### Scenario: Eviction is deterministic

- **WHEN** two identical sequences of compile/hit/insert operations run against two engines with identical limits
- **THEN** both caches SHALL evict the same keys in the same order and retain identical entry sets

#### Scenario: Unfit chunk runs uncached

- **WHEN** a single chunk's deep bytes or node count alone exceeds a ceiling
- **THEN** its evaluation SHALL succeed, the chunk SHALL NOT enter the cache, and no entries SHALL be evicted for it

#### Scenario: Cache charges the engine meter

- **WHEN** an engine constructed with `WithEngineMeter` inserts and later evicts a chunk
- **THEN** the meter SHALL receive `ChargeRetained` with the chunk's deep bytes on insert and a matching `ReleaseRetained` on evict

#### Scenario: Constant payloads count

- **WHEN** a chunk with tiny code embeds a quoted structure of large structural size
- **THEN** its published deep bytes SHALL reflect the structure's measured size, not merely constant-slot headers

### Requirement: GoFunc panics are recovered at evaluation boundaries

A panic raised inside a `GoFunc` (a shipped builtin or an embedder-registered
function) SHALL be recovered at every public evaluation boundary — `Engine.Eval`,
`Engine.Call`, and `Fn.Call` — and returned as a `*core.LispicoError`, never
propagated to the caller's goroutine and never aborting the process. The
recovered error SHALL carry the panic value. After a recovered panic the engine
and any `Fn` handle SHALL remain usable, and no pooled VM SHALL be returned to
the pool in a post-panic state. A recovered panic is a host-facing failure, not
a terminal eval error and not a value observable by `try`/`catch`.

#### Scenario: Panic in a GoFunc surfaces as a typed error, not a crash

- **WHEN** a registered `GoFunc` panics and is invoked through `Engine.Eval`, `Engine.Call`, or `Fn.Call` on either evaluator
- **THEN** the call SHALL return a `*core.LispicoError` reporting the panic and the process SHALL NOT abort

#### Scenario: Engine stays usable after a recovered panic

- **WHEN** a call recovers a GoFunc panic and a subsequent evaluation is issued on the same engine and the same `Fn` handle
- **THEN** the subsequent evaluation SHALL succeed, observing no corruption from the pooled VM that unwound the panic

#### Scenario: Recovered panic matches a returned error to the embedder

- **WHEN** an embedder observes engine stats and `OnEval` callbacks across a call that recovered a panic
- **THEN** the failed evaluation SHALL be recorded on the same path as any other errored evaluation

### Requirement: Function redefinitions survive plugin loading

Loading a plugin with `Use()` SHALL NOT revert a user redefinition of an
existing binding. Under a Lisp-2 dialect the engine bridges value-cell
`GoFunc`s into the function cell so they are callable in head position; this
bridge SHALL NOT overwrite a function-cell binding that differs from the
value-cell `GoFunc` it would install. A binding first established by the bridge,
or absent, MAY be (re)bridged; a binding a program has redefined SHALL be left
untouched. The guarantee holds regardless of how many plugins are loaded and in
what order, preserving the deterministic-evaluation contract.

#### Scenario: Operator redefinition survives an unrelated Use

- **WHEN** a program redefines a canonical operator with `defun` and a later, unrelated `Use()` loads another plugin
- **THEN** the operator SHALL keep the redefined behavior, and no error or silent revert SHALL occur

#### Scenario: Non-operator builtin redefinition survives

- **WHEN** a program redefines a non-operator builtin (for example `map`) with `defun` and a later `Use()` loads another plugin
- **THEN** the redefinition SHALL persist

#### Scenario: Newly loaded plugin functions are still callable in head position

- **WHEN** a plugin is loaded and its functions have not been redefined by the program
- **THEN** those functions SHALL be callable in head position, and un-redefined canonical operators SHALL still take the VM native-op fast path

#### Scenario: Reloading the owning plugin resets its bindings

- **WHEN** the plugin that owns a binding is reloaded with `ReloadPlugin`
- **THEN** that binding MAY be restored to the plugin's definition, since reloading the owning plugin is the sanctioned reset path

### Requirement: Every public evaluation entry point recovers GoFunc panics

Every public path that evaluates user code SHALL recover a panic raised by a
`GoFunc` and return it as a `PanicError`, so no panic escapes the engine to the
host. This SHALL hold for `EvalWithBindings` and `LoadScope` exactly as it
already holds for `Eval`, `Call`, and `Fn.Call`. On a recovered panic the call
SHALL return a nil result (and, for `LoadScope`, a nil scope) together with the
`PanicError`, and any pooled VM used by the call SHALL be reset before it
returns to the pool.

#### Scenario: EvalWithBindings recovers a panicking GoFunc

- **WHEN** a bound `GoFunc` panics during `EvalWithBindings`
- **THEN** the call SHALL return a `PanicError` and SHALL NOT propagate a raw panic to the caller

#### Scenario: LoadScope recovers a panicking GoFunc

- **WHEN** a `GoFunc` panics during `LoadScope`
- **THEN** the call SHALL return a nil scope and a `PanicError`, not a raw panic

### Requirement: Background hot-reload never crashes the host on a panic

A `GoFunc` panic during a `Watch` background reload SHALL be recovered, converted
to an error, and surfaced through the watcher's reload-error path — the same path
an ordinary reload evaluation error uses — rather than terminating the host
process. The watcher SHALL continue watching after a recovered panic; a single
failing file version SHALL NOT stop the watch loop.

#### Scenario: Watched file whose evaluation panics does not kill the process

- **WHEN** a watched file is reloaded and its evaluation triggers a `GoFunc` panic
- **THEN** the reload SHALL report the failure as an error and the host process SHALL keep running
- **AND** the watcher SHALL remain active for subsequent file changes

### Requirement: ListPlugins reports real lifecycle status

`ListPlugins()` SHALL report each registered plugin's real lifecycle status —
active, idle, or frozen — rather than a hardcoded value. The reported status
SHALL match the plugin's documented ADR-0004 lifecycle: `stdlib` and `data`
active, `fsm` idle, and `llm`/`agent`/`lio`/`net`/`exec` frozen. A plugin that
does not declare a lifecycle SHALL default to active, preserving behavior for
third-party plugins.

#### Scenario: Frozen and idle plugins are reported correctly

- **WHEN** an engine has loaded plugins spanning active, idle, and frozen lifecycles and `ListPlugins()` is called
- **THEN** each plugin's reported status SHALL match its ADR-0004 lifecycle, not a uniform "active"

#### Scenario: Third-party plugin without a declared lifecycle

- **WHEN** a plugin does not declare a lifecycle status
- **THEN** `ListPlugins()` SHALL report it as active

