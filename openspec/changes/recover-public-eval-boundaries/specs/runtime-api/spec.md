# runtime-api — delta

## ADDED Requirements

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
