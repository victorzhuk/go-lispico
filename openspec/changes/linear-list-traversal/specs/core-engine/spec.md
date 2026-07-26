# core-engine — delta

## MODIFIED Requirements

### Requirement: Sequence representation efficiency

`List` and `Vector` SHALL keep their public semantics — immutable operations,
element order, equality, deterministic printing, depth-bounded construction —
while meeting efficiency bounds: extending a sequence (`cons` onto a list, `conj`
onto a vector) SHALL allocate storage proportional to what the operation adds, not
to the length of the sequence it extends; `count` SHALL be O(1) for both types;
and indexed reads on a `Vector` SHALL be effectively constant-time. Accumulating N
elements one at a time SHALL therefore allocate O(N) in total, not O(N²).

Indexed reads on a `List` are deliberately not held to that bound — a list past
the flat threshold is a shared chain, where reading position i costs i steps.
The obligation this places on the engine is that it SHALL NOT walk a `List` by
position: any evaluator, dialect, or builtin traversal of a list SHALL cost time
linear in its length, not quadratic, whichever representation backs it.

Representation SHALL be semantically invisible: a small sequence and a
structurally shared sequence with the same elements SHALL be equal, print
identically, iterate identically, and be equally immutable, in both evaluators.

#### Scenario: Accumulation is linear

- **WHEN** a loop conses 100,000 elements onto an accumulator under default resource limits
- **THEN** it SHALL complete in both execution modes without a `ResourceLimitError`

#### Scenario: Extension does not copy

- **WHEN** `cons` extends a list of N elements, or `conj` extends a vector of N elements
- **THEN** the operation SHALL NOT allocate N element slots, and the source sequence SHALL be unchanged

#### Scenario: Promotion is invisible

- **WHEN** a sequence grows past the small-representation threshold and is then compared, printed, iterated, and read by index
- **THEN** results SHALL be identical to a same-elements sequence built below the threshold, in both evaluators

#### Scenario: Traversing a shared list stays linear

- **WHEN** the engine traverses a list longer than the flat threshold — expanding a quasiquoted form, splicing a sequence into one, or normalizing a multi-expression clause body
- **THEN** the cost SHALL grow in proportion to the list's length rather than to its square, and the allocations performed SHALL be the same as for the equivalent list below the threshold
