# runtime-api — delta

## MODIFIED Requirements

### Requirement: Boundary call efficiency

On an engine running the bytecode evaluator, a repeated `Call` or handle call of
an already-defined function SHALL NOT allocate per-call boundary scaffolding: no
derived context or timer, no synthesized chunk, and no fresh VM. When the
function body dispatches no re-entrant call, the call SHALL additionally
allocate no evaluation-state value, leaving per-call allocations limited to
argument/result value representation. When the body dispatches a `GoFunc` that
may re-enter the evaluator, the evaluation-state wrapper SHALL be amortized:
repeated calls through one VM with the same outer context SHALL reuse one
wrapper rather than allocating per call, so steady-state per-call allocations
remain limited to argument/result value representation. A call entering with a
different outer context MAY allocate one fresh wrapper for that context. A
re-entrant `Call` — a `GoFunc` invoking `Call` again on the same engine with
the context it received — SHALL share the enclosing call's structural-depth
and deadline budget rather than starting a fresh one. When no `OnPluginCall`
or `OnEval` callback is registered, a boundary call SHALL NOT read the wall
clock except as required to enforce an observed evaluation deadline; the
engine deadline SHALL be computed lazily at its first observation — a
deadline query, a checkpoint comparison, or re-entrant adoption — so a call
whose host functions never observe the deadline reads no clock at all, even
when the body dispatches `GoFunc`s. `Stats()` SHALL remain accurate whether
or not callbacks are registered, and registered `OnPluginCall`/`OnEval`
callbacks SHALL keep firing with durations as today.

When the engine has no meter, no registered callbacks, and the entry context
carries no evaluation state — a condition the engine SHALL precompute rather
than re-derive per call — the boundary SHALL additionally avoid repeated
synchronization on the steady path: engine-root and cached-callee reads
SHALL NOT take a per-call lock (a versioned snapshot read suffices, falling
back to locked resolution on any version change or tombstone), evaluation-
state bookkeeping SHALL NOT run, and per-call atomic operations SHALL be
limited to stats attribution and VM acquisition. Panic recovery SHALL remain
in force on this path — a panicking `GoFunc` or internal fault is still
returned as an error, never propagated. Registering a callback, attaching a
meter, or passing an evaluation-state context SHALL route the very next call
through the general path with today's exact behavior.

#### Scenario: Non-dispatching Call allocates only value representation

- **WHEN** `Call` repeatedly invokes a compiled function whose body dispatches no further call (a selector or leaf body) on a bytecode engine with no callbacks registered
- **THEN** per-call allocations SHALL be limited to argument/result value representation, with no context, timer, eval-state, chunk, or VM allocation

#### Scenario: Repeated dispatching calls amortize the wrapper

- **WHEN** `Call` repeatedly invokes a compiled function whose body dispatches a `GoFunc`, passing the same outer context every time
- **THEN** the evaluation-state wrapper SHALL be allocated at most once for that context and reused across the calls, and steady-state per-call allocations SHALL be limited to argument/result value representation

#### Scenario: Re-entrant body shares one evaluation-state

- **WHEN** a compiled function whose body dispatches a `GoFunc` that re-enters the evaluator is invoked through `Call`
- **THEN** at most one evaluation-state value SHALL be allocated for that `Call` and reused for its remainder, and the `GoFunc`'s re-entry SHALL enforce the same structural-depth and deadline budget as the enclosing `Call`

#### Scenario: Nested Call shares the enclosing resource budget

- **WHEN** a `GoFunc` invoked during a `Call` itself invokes `Call` on the same engine, forwarding the context it received
- **THEN** the nested `Call` SHALL count structural depth against the enclosing call's budget rather than a fresh one, so the combined nesting still trips the configured `MaxStructuralDepth`

#### Scenario: Unobserved calls read no clock

- **WHEN** no callback is registered and a call — including one whose body dispatches `GoFunc`s that never read their context's deadline — completes without any deadline observation
- **THEN** the boundary SHALL perform no wall-clock read for that call, and the engine deadline SHALL still bound longer evaluations once computed at its first observation

#### Scenario: Stats stay accurate without callbacks

- **WHEN** `Call` runs N times with no callbacks registered
- **THEN** `Stats()` SHALL report N calls for that function

#### Scenario: Callbacks unchanged when registered

- **WHEN** an `OnPluginCall` callback is registered and `Call` runs
- **THEN** the callback SHALL fire with the function name and a measured duration, as before

#### Scenario: Steady path takes no per-call lock

- **WHEN** `Call` repeatedly invokes a cached, canonical function on a fast-condition engine (no meter, no callbacks, plain context)
- **THEN** the steady-state call SHALL perform no mutex acquisition for root or callee resolution, and redefinition, tombstoning, or hot-reload SHALL still be observed by the next call exactly as by per-call resolution

#### Scenario: Fast-path panics are still recovered

- **WHEN** a `GoFunc` dispatched during a fast-condition `Call` panics
- **THEN** `Call` SHALL return the same recovered panic error as the general path, and the engine SHALL remain usable

#### Scenario: Condition transitions route the next call correctly

- **WHEN** a callback is registered (or a meter attached, or an evaluation-state context passed) after a sequence of fast-condition calls
- **THEN** the next call SHALL take the general path — the callback fires, the meter is drawn, the shared budget is honored — with no stale fast-path behavior
