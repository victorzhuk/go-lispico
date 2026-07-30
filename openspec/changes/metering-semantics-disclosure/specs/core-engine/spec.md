# core-engine — delta

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
required to be equal across evaluators, and are NOT required to be equal
across compiler versions for the same evaluator and the same source: because
the VM charges one reduction per instruction decode, a compiler change that
alters how many instructions a given form compiles to (a superinstruction
fusing what were previously several instructions into one, for example)
changes the reductions charged for evaluating that form, even with the
evaluator and the source both unchanged. A resource-limit ceiling a program
previously crossed at some iteration count MAY no longer be crossed at that
count after such a compiler change; this is expected, not a defect, but SHALL
be disclosed wherever a compiler change is documented as altering instruction
count.
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

#### Scenario: A compiler change may shift where a reduction ceiling is crossed

- **WHEN** a compiler version change alters how many VM instructions a given form compiles to (e.g. fusing several instructions into one), and the same source is evaluated against the same `MaxReductions` ceiling on both compiler versions
- **THEN** the iteration count at which the ceiling is crossed MAY differ between the two versions, and this difference SHALL NOT be treated as a counter-consistency defect
