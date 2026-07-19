# runtime-api — delta

## ADDED Requirements

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
