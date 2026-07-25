# core-engine — delta

## ADDED Requirements

### Requirement: Sequence representation efficiency

`List` and `Vector` SHALL keep their public semantics — immutable operations,
element order, equality, deterministic printing, depth-bounded construction —
while meeting efficiency bounds: extending a sequence (`cons` onto a list, `conj`
onto a vector) SHALL allocate storage proportional to what the operation adds, not
to the length of the sequence it extends; `count` SHALL be O(1) for both types;
and indexed reads on a `Vector` SHALL be effectively constant-time. Accumulating N
elements one at a time SHALL therefore allocate O(N) in total, not O(N²).

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

## MODIFIED Requirements

### Requirement: Per-evaluation reduction and allocation counters

The core evaluator SHALL carry a per-call reduction counter and a per-call
cumulative allocation counter on the `evalState` of every evaluation, never
shared across concurrent evaluations on the same engine. Step definitions are
per-evaluator and normative: the tree-walker SHALL charge one reduction per
apply-trampoline iteration and per form dispatch; the VM SHALL charge one
reduction per instruction decode; macro expansion SHALL charge one per
expansion step; the compiler SHALL charge one per emitted instruction, and
compilation allocation SHALL charge the evaluation that triggered
compilation before any chunk is cached; GoFunc dispatch SHALL charge one
reduction plus the result's shallow allocation size at the centralized apply
sites, unless the callee has already charged the ledger for that same value, in
which case the apply site SHALL NOT charge it again. Counter values are NOT
required to be equal across evaluators.
Reduction accounting SHALL piggyback the existing batched-cancellation
countdown — no additional per-step cost and no additional clock reads.
Exceeding either ceiling SHALL raise `Code: "ResourceLimitError"`, terminal
per the non-catchability requirement, on both evaluators.

Allocation charging SHALL be incremental for structurally derived results: an
operation whose result shares substructure with one of its arguments SHALL charge
the storage it newly allocated, not a deep measure of the whole result, because
the shared substructure was charged when it was created. An operation that builds
a sequence or map from unrelated values SHALL charge the result's deep size.
Retained-state accounting SHALL keep using a deep measure, since a binding holds
its whole reachable structure alive.

#### Scenario: Shared substructure is charged once

- **WHEN** a loop extends an accumulator N times and each result shares the previous accumulator's storage
- **THEN** the total allocation charged SHALL grow linearly in N, and no iteration SHALL charge the shared prefix again

#### Scenario: A result the callee charged is not charged again at the apply site

- **WHEN** a GoFunc charges the ledger for the value it returns, and that value is then returned through a centralized apply site
- **THEN** the apply site SHALL NOT add its own shallow charge for that value, so a loop of N such calls charges O(N) in total rather than a size-proportional amount per call

#### Scenario: A result the callee did not charge is still charged at the apply site

- **WHEN** a GoFunc returns a value without charging the ledger for it
- **THEN** the apply site SHALL charge that value's shallow allocation size, and a callee's own nested evaluation SHALL NOT be mistaken for the callee having charged its result

#### Scenario: Fresh construction still charges deeply

- **WHEN** `list`, `vector`, `range`, or `json/decode` builds a result from values that share nothing with an argument
- **THEN** the evaluation SHALL be charged the result's deep allocation size

#### Scenario: Tight allocation loop fails closed

- **WHEN** an Engine runs a loop that allocates faster than it reduces, configured with `MaxAllocationBytes: 1<<20`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` before the host is exhausted, and `try`/`catch` SHALL NOT intercept the error

#### Scenario: Reduction-amplified macro recursion fails closed

- **WHEN** an Engine runs a macro-amplified recursion that exceeds `MaxReductions` before tripping `MaxDepth`
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Context observed within the reduction budget

- **WHEN** the caller's context is cancelled mid-evaluation
- **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation

#### Scenario: Per-evaluation counters are isolated

- **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race`
- **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race

#### Scenario: Evaluators agree on terminal behavior

- **WHEN** the same adversarial program is evaluated by the tree-walker and by the VM under identical limits
- **THEN** both SHALL terminate with the same terminal error class; counter values MAY differ between evaluators

#### Scenario: Direct Apply path is metered

- **WHEN** a host invokes a Lisp function via `core.Evaluator.Apply` without going through an `Engine` entry point, under tight limits
- **THEN** the evaluation SHALL be bounded by the same per-evaluation counters
