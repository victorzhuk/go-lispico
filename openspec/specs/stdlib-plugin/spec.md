# stdlib-plugin Specification

## Purpose

The stdlib-plugin capability provides standard library functionality for the system, registered and made ready for use when the system initializes.
## Requirements
### Requirement: stdlib-plugin implementation
The system SHALL implement the stdlib-plugin functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the stdlib-plugin SHALL be ready for use

### Requirement: Builtins have a single shared implementation
Each stdlib operation SHALL have exactly one implementation, reusable across Dialects under different visible names. The stdlib SHALL NOT provide duplicate implementations that differ only by the name a Dialect exposes.

#### Scenario: One implementation serves multiple dialect names
- **WHEN** two Dialects expose the same operation under different names
- **THEN** both names SHALL resolve to the one shared implementation

#### Scenario: Adding a dialect name does not add an implementation
- **WHEN** a Dialect adds a new visible name for an existing operation
- **THEN** no new implementation of that operation SHALL be introduced in the stdlib

### Requirement: Bootstrap macros bind through the engine's evaluator

The stdlib bootstrap SHALL define its Lisp-source macros and functions through the
environment's own evaluator, so definitions land where the engine's dialect axes
(Lisp-2 function cell, truthiness) place them — never through a separately
constructed evaluator with different axes.

#### Scenario: Threading macros work under the default CL dialect

- **WHEN** `runtime.New(nil)` loads the stdlib plugin and evaluates `(-> 1 (+ 2))`
- **THEN** the result SHALL be `3`, not an `UndefinedError`

#### Scenario: All bootstrap macros resolve in head position under Lisp-2

- **WHEN** a Lisp-2 engine evaluates each of `->`, `->>`, `as->`, `if-let`, `when-let`, `get-in` in head position
- **THEN** every form SHALL resolve and evaluate without `UndefinedError`

### Requirement: range is bounded and cancellable

The `range` builtin SHALL NOT build an unbounded result. It SHALL cap its result
length at the Engine's configured collection-length ceiling, returning a
`*core.LispicoError` when the requested range would exceed it, and it SHALL check
`ctx` cooperatively while building so a `WithTimeout` or cancelled context stops it
promptly instead of running to completion or exhausting memory.

#### Scenario: Oversized range fails closed

- **WHEN** `(range 0 n)` is evaluated with `n` greater than the collection-length ceiling
- **THEN** evaluation SHALL return a `*core.LispicoError` reporting the length limit, and no oversized slice SHALL be allocated

#### Scenario: range honors cancellation

- **WHEN** a `range` over a large span is evaluated under a context that is cancelled or times out mid-build
- **THEN** the evaluation SHALL stop with the context error rather than continuing to allocate

### Requirement: merge builds its result in linear cost

`merge` SHALL construct its fresh result map without copying the accumulated map
per key: allocated bytes and allocation count SHALL grow roughly linearly with the
total number of entries merged, not quadratically. Its observable semantics SHALL
be unchanged: input maps stay immutable, iteration of the result stays
deterministic, the right-most map wins on duplicate keys, `(merge)` and nil
arguments keep their current behavior, and non-map arguments keep the existing
type error.

#### Scenario: Semantics preserved

- **WHEN** `merge` is called with zero maps, nil arguments, overlapping keys, and a non-map argument
- **THEN** results and errors SHALL be identical to the prior implementation, and the input maps SHALL be unchanged

#### Scenario: Growth is no longer quadratic

- **WHEN** `merge` is benchmarked over increasing map sizes
- **THEN** `B/op` and `allocs/op` SHALL grow roughly linearly with entry count

### Requirement: Amplifying builtins charge output allocation eagerly

A builtin that can construct output disproportionate to its input SHALL charge
the evaluation allocation ledger for its constructed output before performing
the amplifying allocation, rather than relying on the shallow post-return
`GoFunc` result charge. `format` SHALL estimate its output size from the parsed
width/precision specifiers and charge that estimate before calling into Go's
formatter; when the estimate exceeds the remaining allocation budget it SHALL
return a `ResourceLimitError` without building the string. Builtins whose output
is proportional to their input (for example string concatenation, whose result
length equals the sum of inputs) MAY continue to rely on the shallow result
charge. The single governing knob remains `MaxAllocationBytes`.

#### Scenario: format fails closed before amplifying allocation

- **WHEN** `format` is called with specifiers whose estimated output exceeds the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` and SHALL NOT build the oversized string

#### Scenario: Ordinary format still succeeds

- **WHEN** `format` produces output within the allocation budget
- **THEN** it SHALL return the formatted string and charge the ledger for it

#### Scenario: Non-amplifying builtins are unaffected

- **WHEN** a builtin whose output is proportional to its input runs under the ledger
- **THEN** it SHALL behave as before, charged by its shallow result size

### Requirement: assoc charges constructed allocation deeply

`assoc` SHALL charge the evaluation allocation ledger for its constructed result
using the same deep-bytes charge and length check applied to `conj`'s map branch
and `merge`, rather than relying on the shallow post-return `GoFunc` result
charge. A chained or large-valued `assoc` whose result exceeds the remaining
`MaxAllocationBytes` SHALL fail with a `ResourceLimitError`. Ordinary `assoc`
within budget SHALL return the updated map unchanged in value.

#### Scenario: assoc with large nested values is charged for its real size

- **WHEN** `assoc` inserts a large nested value into a map under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the result, not the map's shallow entry count

#### Scenario: Over-budget assoc fails closed

- **WHEN** an `assoc` result would exceed the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` rather than an uncharged large map

#### Scenario: Ordinary assoc is unchanged

- **WHEN** `assoc` runs within the allocation budget
- **THEN** it SHALL return the same updated map it returns today

