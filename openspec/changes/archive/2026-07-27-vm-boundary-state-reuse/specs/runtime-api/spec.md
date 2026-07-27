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
