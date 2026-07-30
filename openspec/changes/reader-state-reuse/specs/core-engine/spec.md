# core-engine — delta

## ADDED Requirements

### Requirement: Reader working state is safely reusable across calls

The reader's per-call working storage (tokenizer state, parser state, and the
token buffer) MAY be drawn from a reusable pool rather than freshly allocated
on every `Read`, provided reuse is invisible to any observer: a value tree
returned from one `Read` SHALL be completely unaffected by any later `Read`
that reuses the same pooled storage, under both sequential and concurrent
use. Reuse SHALL NOT change any parsed value's content, structural sharing
with the source string, or observable allocation shape beyond reducing
per-call heap allocations. A collection literal's backing slice, once handed
to `List` or `Vector`, SHALL be independently owned — never an alias into
pooled scratch storage that a later `Read` call may overwrite.

#### Scenario: A prior Read's result survives a later Read reusing pooled storage

- **WHEN** one `Read` call's returned value tree is retained, and a subsequent `Read` call reuses the same pooled tokenizer/parser storage
- **THEN** the retained value tree SHALL be unchanged by the subsequent call's tokenization or parsing

#### Scenario: Concurrent Read calls sharing a pool show no data race

- **WHEN** multiple goroutines call `Read` concurrently against a shared reader-state pool
- **THEN** `go test -race` SHALL report no data race, and each call's result SHALL be correct for its own input

#### Scenario: A collection literal's elements are independently owned

- **WHEN** a list or vector literal is parsed via pooled scratch storage
- **THEN** its final backing slice SHALL be a right-sized, independently-owned copy, never an alias into storage a later `Read` call may reuse
