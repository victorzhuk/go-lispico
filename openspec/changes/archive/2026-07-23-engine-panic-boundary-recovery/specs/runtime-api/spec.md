# runtime-api — delta

## ADDED Requirements

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
