# runtime-api — delta

## ADDED Requirements

### Requirement: Boundary call efficiency

On a `WithBytecode()` engine, a repeated `Call` of an already-defined function
SHALL NOT allocate per-call scaffolding: no derived context or timer, no
per-call evaluation-state context value, no synthesized chunk, and no fresh VM.
Steady-state allocations per `Call` SHALL be limited to argument/result value
representation. `Stats()` SHALL remain accurate whether or not callbacks are
registered, and registered `OnPluginCall`/`OnEval` callbacks SHALL keep firing
with durations as today.

#### Scenario: Steady-state Call allocates only value representation

- **WHEN** `Call` invokes the same compiled two-argument function repeatedly on a `WithBytecode()` engine with no callbacks registered
- **THEN** per-call allocations SHALL be limited to argument/result value representation, with no context, timer, eval-state, chunk, or VM allocation

#### Scenario: Stats stay accurate without callbacks

- **WHEN** `Call` runs N times with no callbacks registered
- **THEN** `Stats()` SHALL report N calls for that function

#### Scenario: Callbacks unchanged when registered

- **WHEN** an `OnPluginCall` callback is registered and `Call` runs
- **THEN** the callback SHALL fire with the function name and a measured duration, as before
