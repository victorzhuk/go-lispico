# Technical Specification

## ADDED Requirements

### Requirement: Concurrent evaluation safety

The core evaluator SHALL support concurrent `Eval` and `Apply` calls on a single
engine without data races or cross-call state corruption. Per-evaluation state —
macro-expansion depth, call depth, and the `recur`/loop counter — SHALL be scoped
to a single evaluation, not shared across goroutines.

#### Scenario: Concurrent evaluation is race-free

- **WHEN** multiple goroutines evaluate on one engine concurrently
- **THEN** each SHALL return the correct result and `go test -race` SHALL report no data race

#### Scenario: recur state does not leak across goroutines

- **WHEN** one goroutine runs a `loop` while another evaluates a bare `(recur ...)` outside any loop
- **THEN** the bare `recur` SHALL always error "recur outside loop", regardless of the other goroutine's loop

### Requirement: Typed evaluation errors

Evaluation failures SHALL be reported as `*core.LispicoError` carrying a `Code`,
and SHALL include a source position (`Line`, `Col`, `Source`) when the failing form
carries one.

#### Scenario: errors.As recovers a typed error

- **WHEN** an evaluation fails on arity, type, an undefined symbol, or a general eval error
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and `lispicoErr.Code` SHALL classify the failure

### Requirement: Literal element evaluation

Evaluating a vector `[...]` or map `{...}` literal SHALL evaluate each element,
producing a new immutable value.

#### Scenario: Vector and map literals evaluate elements

- **WHEN** `[1 x]` or `{:a x}` is evaluated with `x` bound to `99`
- **THEN** the result SHALL be `[1 99]` or `{:a 99}` respectively

#### Scenario: Quasiquote expands inside maps

- **WHEN** `` `{:a ~x} `` is evaluated with `x` bound to `99`
- **THEN** the result SHALL be `{:a 99}`
