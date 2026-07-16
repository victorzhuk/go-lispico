# bytecode-vm — delta

## ADDED Requirements

### Requirement: Keyword application parity

VM application SHALL support Keyword values as callables with semantics identical
to the Evaluator: `(:key m)` looks up `:key` in map `m`, a missing key yields the
default argument or `nil`, wrong arity is a typed error, and a non-map argument
behaves exactly as under the tree-walker. This SHALL hold on both the `Eval` and
the `Apply`/`Call` paths.

#### Scenario: Keyword lookup hits and misses

- **WHEN** `(:key m)` is evaluated under `WithBytecode()` with the key present, absent, and absent with a default argument
- **THEN** the results SHALL equal the tree-walker's (value, `nil`, default)

#### Scenario: Keyword misuse matches the Evaluator

- **WHEN** a Keyword is applied with wrong arity or to a non-map value under the VM
- **THEN** the outcome (typed error or value) SHALL equal the tree-walker's for the same input

### Requirement: Structural-depth state hygiene

VM structural-depth accounting SHALL be restored on every exit path — normal
return, thrown error, ceiling breach, and malformed input — including when the VM
instance is reused from the pool. One failed evaluation SHALL NOT reduce the
structural-depth budget available to any later evaluation on the same Engine.

#### Scenario: Failed evaluation does not poison the next

- **WHEN** a VM evaluation fails for any reason and a subsequent well-formed evaluation runs on the same `WithBytecode()` Engine
- **THEN** the subsequent evaluation SHALL see the full configured structural-depth budget and succeed

#### Scenario: Pooled reuse restores depth state

- **WHEN** a pooled VM instance that previously exited through an error path is reused for a new evaluation
- **THEN** its structural-depth accounting SHALL start fresh, with no carry-over from the failed run

## MODIFIED Requirements

### Requirement: Bytecode VM concurrency safety

The bytecode evaluator SHALL support concurrent `Eval` calls on a single engine
without data races or cross-call state corruption. The same SHALL hold for the
`Apply`/`Call` path: distinct closures with separate environments running
concurrently on one shared engine SHALL return correct results with no data race.

#### Scenario: Concurrent evaluation

- **WHEN** multiple goroutines call `Eval` concurrently on one `WithBytecode()` engine
- **THEN** each SHALL return the correct result and `go test -race` SHALL report no data race

#### Scenario: Concurrent distinct closures through Call

- **WHEN** multiple goroutines invoke distinct closures with separate environments through `Call` on one shared `WithBytecode()` engine
- **THEN** each SHALL return its own correct result and `go test -race` SHALL report no data race
