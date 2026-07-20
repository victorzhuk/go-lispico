# runtime-api — delta

## ADDED Requirements

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

## MODIFIED Requirements

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
