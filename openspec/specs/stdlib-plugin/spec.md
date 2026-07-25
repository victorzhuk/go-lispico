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

`assoc` SHALL charge the evaluation allocation ledger for the storage its result
newly allocates, together with the length check applied to `conj`'s map branch and
`merge`, rather than relying on the shallow post-return `GoFunc` result charge.
Where the result shares substructure with the input map, the shared part SHALL NOT
be charged again — it was charged when it was created; where `assoc` inserts a
value that is new to the ledger, that value's deep size SHALL be charged. A
chained or large-valued `assoc` whose newly allocated storage exceeds the
remaining `MaxAllocationBytes` SHALL fail with a `ResourceLimitError`. Ordinary
`assoc` within budget SHALL return the updated map unchanged in value.

#### Scenario: assoc with large nested values is charged for its real size

- **WHEN** `assoc` inserts a large nested value into a map under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the inserted value plus the map storage the operation allocated, not the map's shallow entry count

#### Scenario: Chained assoc does not re-charge the shared map

- **WHEN** `assoc` is applied repeatedly to the result of the previous `assoc` in a loop
- **THEN** the total charge SHALL grow with the values and nodes added, not with the accumulated map size per iteration

#### Scenario: Over-budget assoc fails closed

- **WHEN** an `assoc` result's newly allocated storage would exceed the remaining `MaxAllocationBytes`
- **THEN** it SHALL return a `ResourceLimitError` rather than an uncharged large map

#### Scenario: Ordinary assoc is unchanged

- **WHEN** `assoc` runs within the allocation budget
- **THEN** it SHALL return the same updated map it returns today

### Requirement: Sequence extension builds its result in linear cost

`cons`, `conj`, and the sequence-extending path of `concat` SHALL extend their
argument without copying it per call: allocated bytes and allocation count SHALL
grow roughly linearly with the number of elements added across a loop, not
quadratically with the accumulated length. Charging SHALL follow the incremental
rule — the storage the operation newly allocated, not a deep measure of a result
whose bulk is shared with the argument.

Observable semantics SHALL be unchanged: inputs stay immutable, element order and
equality are unaffected, `count` stays exact, collection-length and construction
depth limits still apply, and non-collection arguments keep their existing type
errors.

#### Scenario: Accumulation completes under the default ledger

- **WHEN** a loop conses 100,000 elements onto an accumulator with default resource limits
- **THEN** it SHALL return the accumulated sequence rather than a `ResourceLimitError`

#### Scenario: Semantics preserved

- **WHEN** `cons`/`conj` run on empty, small, and large sequences, on `nil`, and on a non-collection argument
- **THEN** results, immutability of the inputs, and the existing type errors SHALL be unchanged

#### Scenario: Fresh builders keep deep charging

- **WHEN** `list`, `vector`, or `range` constructs a result from unrelated values
- **THEN** the evaluation SHALL still be charged the result's deep allocation size

