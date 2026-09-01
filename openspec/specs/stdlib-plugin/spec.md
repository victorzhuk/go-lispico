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

The stdlib bootstrap SHALL define its trusted Lisp-source macros and functions
through `core.BootstrapDefiner` on the evaluator owned by the target environment,
so definitions land where the Engine's Dialect axes place them. The capability
SHALL expose `DefineBootstrap(context.Context, string, *core.Env)
(core.Value, error)` and SHALL be implemented by both the core tree evaluator and
the runtime bytecode evaluator. The bytecode implementation SHALL delegate to its
dialect-configured tree evaluator's definition path rather than construct an
identity evaluator.

The capability SHALL use the trusted full bootstrap reader and accept exactly
one top-level proper list headed by `defn` or `defmacro`. Only that definition
dispatch MAY use the full kernel when the running Dialect removes the form;
namespace mode, truthiness, evaluation state, limits, and target environment
SHALL remain those of the installed owner. Eager loading and lazy first-touch
materialization SHALL use this same operation and publication rules.

Host-installed Go plugins are trusted and MAY discover the exported capability
through a structural interface assertion. The capability SHALL NOT be registered
as a Lisp value or Special form and SHALL NOT be reachable from evaluated Lisp
code. The system does not claim isolation among Go plugins.

If stdlib is initialized directly on an environment that has no evaluator, the
loader SHALL install the default Evaluator on that environment before defining
source. It SHALL NOT evaluate bootstrap source through an unowned temporary
Evaluator or replace an evaluator already installed by an Engine or embedder.

#### Scenario: Threading macros work under the default CL dialect

- **WHEN** `runtime.New(nil)` loads the stdlib plugin and evaluates `(-> 1 (+ 2))`
- **THEN** the result SHALL be `3`, not an `UndefinedError`

#### Scenario: All bootstrap macros resolve in head position under Lisp-2

- **WHEN** a Lisp-2 Engine evaluates each of `->`, `->>`, `as->`, `if-let`, and `when-let` in head position
- **THEN** every form SHALL resolve and evaluate without `UndefinedError`

#### Scenario: Eager and lazy definitions use the installed owner

- **WHEN** the same bootstrap name is defined during eager startup and lazy first touch with an evaluator already installed on the environment
- **THEN** both paths SHALL invoke that evaluator's bootstrap-definition operation and SHALL publish equivalent bindings

#### Scenario: Restricted Dialect does not lose trusted definitions

- **WHEN** an empty-base Dialect removes user access to `defn` and `defmacro` while stdlib bootstrap source is loaded
- **THEN** trusted definitions SHALL still be installed without making either removed Special form callable by user code

#### Scenario: Trusted reader is independent of CL reader flags

- **WHEN** bootstrap source containing vector parameter or binding syntax is loaded for a Dialect whose public reader disables brackets
- **THEN** the trusted definition SHALL load while the same bracket syntax submitted as user source remains rejected

#### Scenario: Bootstrap input is definition-only

- **WHEN** trusted bootstrap input contains multiple forms or a top-level form other than `defn` or `defmacro`
- **THEN** the operation SHALL fail with a typed error without evaluating that input

#### Scenario: Go capability is absent from Lisp

- **WHEN** evaluated code inspects and invokes every name available in a full-base or empty-base Dialect
- **THEN** no name or first-class value SHALL expose `DefineBootstrap`, while trusted host Go code MAY assert `core.BootstrapDefiner`

#### Scenario: Standalone initialization adopts its evaluator

- **WHEN** stdlib initializes directly on an environment with no evaluator
- **THEN** the environment SHALL own the default Evaluator before the first bootstrap definition is evaluated

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

`dissoc` SHALL follow the same incremental rule for the storage its result
allocates.

#### Scenario: assoc with large nested values is charged for its real size

- **WHEN** `assoc` inserts a large nested value into a map under the ledger
- **THEN** the ledger SHALL be charged approximately the deep size of the inserted value plus the map storage the operation allocated, not the map's shallow entry count

#### Scenario: Chained assoc does not re-charge the shared map

- **WHEN** `assoc` is applied repeatedly to the result of the previous `assoc` in a loop
- **THEN** the total charge SHALL grow with the values and nodes added, not with the accumulated map size per iteration

#### Scenario: Map accumulation completes under the default ledger

- **WHEN** a loop `assoc`s 20,000 distinct keys into an accumulating map with default resource limits
- **THEN** it SHALL return the accumulated map rather than a `ResourceLimitError`

Unlike the sequence requirement's 100,000-element cons, a chained `assoc` has a
finite ceiling by construction: each call copies a root-to-leaf path, so building
an n-key map one key at a time allocates O(n log n) bytes no matter how the
structure is arranged. The bound above is chosen an order of magnitude past the
whole-copy representation's ceiling and with margin below the measured one; it
pins the improvement without asserting an unbounded budget that no persistent map
can honestly deliver.

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

### Requirement: Map lookup preserves key presence

`get` SHALL accept two or three arguments. For a map subject, it SHALL return
the stored value when the key is present, including when that value is `nil`.
For a missing key or a `nil` subject, it SHALL return `nil` in the two-argument
form and the supplied default in the three-argument form. A non-map, non-`nil`
subject SHALL return a `*core.LispicoError` with `Code: "TypeError"`.

#### Scenario: Nil subject has no key

- **WHEN** `(get nil :k)` and `(get nil :k :missing)` are evaluated
- **THEN** the results SHALL be `nil` and `:missing`, respectively

#### Scenario: Present nil is not replaced by the default

- **WHEN** `(get (hash-map :k nil) :k :missing)` is evaluated
- **THEN** the result SHALL be `nil`

#### Scenario: Missing map key uses the default

- **WHEN** `(get (hash-map) :k :missing)` is evaluated
- **THEN** the result SHALL be `:missing`

#### Scenario: Scalar lookup remains an error

- **WHEN** `get` is called with a non-map, non-`nil` subject
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "TypeError"`

#### Scenario: Lookup arity errors are typed

- **WHEN** `get` is called with any arity other than two or three
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "ArityError"`

### Requirement: Nested lookup distinguishes an absent path from a nil value

`get-in` SHALL accept a subject, a key path, and an optional default. The key
path SHALL be a list, vector, or `nil`; a `nil` path SHALL be the empty path.
For each remaining key, the current subject SHALL be a map or `nil`. An absent
key or a `nil` subject with keys remaining SHALL make the path missing; the
two-argument form SHALL then return `nil`, and the three-argument form SHALL
return its default. A present `nil` at the terminal key SHALL be a successful
lookup and SHALL NOT be replaced by the default.

#### Scenario: Missing intermediate short-circuits traversal

- **WHEN** `(get-in (hash-map :a (hash-map)) (list :a :b :c))` is evaluated
- **THEN** the result SHALL be `nil` without attempting lookup on a non-map value

#### Scenario: Missing nested path uses the default

- **WHEN** `(get-in (hash-map :a nil) (list :a :b) :missing)` is evaluated
- **THEN** the result SHALL be `:missing`

#### Scenario: Terminal nil remains present

- **WHEN** `(get-in (hash-map :a (hash-map :b nil)) (list :a :b) :missing)` is evaluated
- **THEN** the result SHALL be `nil`

#### Scenario: Empty path returns the original subject

- **WHEN** `get-in` is called with an empty list, empty vector, or `nil` key path
- **THEN** it SHALL return the original subject and ignore any supplied default

#### Scenario: Non-map intermediate remains an error

- **WHEN** `(get-in (hash-map :a 1) (list :a :b))` is evaluated
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "TypeError"`

#### Scenario: Invalid key path remains an error

- **WHEN** `get-in` receives a key path that is not a list, vector, or `nil`
- **THEN** evaluation SHALL return a `*core.LispicoError` with `Code: "TypeError"`

#### Scenario: Engines share nested lookup behavior

- **WHEN** the same `get-in` behavior golden is evaluated by the Evaluator and VM
- **THEN** both engines SHALL return equal values or equivalent errors

#### Scenario: Long traversal is cancellable and metered

- **WHEN** `get-in` traverses a long key path under a cancelled caller context, an expired Engine-owned Evaluation deadline, or an exhausted Reduction budget
- **THEN** traversal SHALL stop with the corresponding context or resource-limit error rather than completing as an unmetered Builtin loop

#### Scenario: Borrowed lookup results do not consume allocation budget

- **WHEN** `get` returns a stored collection or default, or `get-in` returns a stored collection or the original subject for an empty path, under an otherwise sufficient tight allocation limit
- **THEN** lookup SHALL charge zero result-allocation bytes and SHALL return the borrowed value rather than a `ResourceLimitError`

#### Scenario: Empty-base Dialects do not inherit Builtin get-in

- **WHEN** an empty-base Dialect omits `get-in` from its vocabulary
- **THEN** `get-in` SHALL be undefined, while explicitly allowlisting it SHALL expose the shared Builtin

#### Scenario: Get-in has Builtin representation

- **WHEN** the `get-in` callable is printed or compared after this change
- **THEN** it SHALL have the same printed and equality behavior as other Builtins named `get-in`, not the previous Lambda behavior
