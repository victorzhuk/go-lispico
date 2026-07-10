# core-engine — delta

## ADDED Requirements

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
