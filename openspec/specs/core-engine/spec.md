# core-engine Specification

## Purpose

The core-engine capability provides the core interpreter functionality for the system, registered and made ready for use when the system initializes.
## Requirements
### Requirement: core-engine implementation
The system SHALL implement the core-engine functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the core-engine SHALL be ready for use

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
carries one. An uncaught `throw` SHALL surface as a `*core.LispicoError`, not an
untyped error.

#### Scenario: errors.As recovers a typed error

- **WHEN** an evaluation fails on arity, type, an undefined symbol, or a general eval error
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and `lispicoErr.Code` SHALL classify the failure

#### Scenario: Uncaught throw is typed

- **WHEN** `(throw "boom")` is evaluated with no enclosing `try`
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and the error SHALL carry the thrown value's rendering

### Requirement: Literal element evaluation

Evaluating a vector `[...]` or map `{...}` literal SHALL evaluate each element,
producing a new immutable value.

#### Scenario: Vector and map literals evaluate elements

- **WHEN** `[1 x]` or `{:a x}` is evaluated with `x` bound to `99`
- **THEN** the result SHALL be `[1 99]` or `{:a 99}` respectively

#### Scenario: Quasiquote expands inside maps

- **WHEN** `` `{:a ~x} `` is evaluated with `x` bound to `99`
- **THEN** the result SHALL be `{:a 99}`

### Requirement: Reader errors carry token positions

Reader errors SHALL report the line and column of the offending token whenever the
tokenizer recorded one — including invalid numeric literals and unexpected EOF —
never a placeholder `0,0`.

#### Scenario: Invalid number reports its position

- **WHEN** parsing source containing an invalid numeric literal on line 3
- **THEN** the returned `*core.LispicoError` SHALL carry `Line: 3` and the token's column

#### Scenario: Unexpected EOF reports the end position

- **WHEN** parsing source that ends mid-form
- **THEN** the returned error SHALL carry the EOF token's recorded line and column

### Requirement: Structural recursion is bounded

The reader and the evaluator SHALL bound structural recursion so that no input can
exhaust the Go stack. The reader SHALL enforce a nesting-depth ceiling while
parsing lists, vectors, and maps; the evaluator SHALL enforce a structural-depth
ceiling while descending `Vector` and `HashMap` literals and expanding quasiquote.
Exceeding either ceiling SHALL return a `*core.LispicoError`, never a Go panic and
never a fatal stack overflow. The reader ceiling SHALL be fixed at parser
construction (the reader carries no `context`); the evaluator ceiling SHALL be
tracked per evaluation, not on a shared engine field, consistent with the
concurrent-evaluation contract.

#### Scenario: Deeply nested source fails closed instead of crashing

- **WHEN** source consisting of millions of unbalanced opening delimiters is read
- **THEN** `Read` SHALL return a `*core.LispicoError` reporting the depth limit, and the process SHALL NOT abort with a fatal stack overflow

#### Scenario: Deeply nested literal is bounded during evaluation

- **WHEN** a vector, map, or quasiquote literal nested past the structural-depth ceiling is evaluated
- **THEN** evaluation SHALL return a `*core.LispicoError` reporting the depth limit, not a panic or a fatal stack overflow

#### Scenario: Structural depth does not leak across goroutines

- **WHEN** two goroutines evaluate deeply nested literals concurrently on one engine
- **THEN** each SHALL be bounded by its own per-evaluation structural-depth counter and `go test -race` SHALL report no data race

