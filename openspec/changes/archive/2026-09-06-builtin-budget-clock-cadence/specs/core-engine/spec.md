# core-engine — delta

## MODIFIED Requirements

### Requirement: Per-evaluation reduction and allocation counters

The core evaluator SHALL carry a per-call reduction counter and a per-call
cumulative allocation counter on the `evalState` of every evaluation, never
shared across concurrent evaluations on the same engine. Step definitions are
per-execution-path and normative: the Evaluator SHALL charge one reduction per
apply-trampoline iteration and per form dispatch; the VM SHALL charge one
reduction per instruction decode; macro expansion SHALL charge one per
expansion step; the compiler SHALL charge one per emitted instruction, and
compilation allocation SHALL charge the evaluation that triggered
compilation before any chunk is cached; GoFunc dispatch SHALL charge one
reduction plus the result's shallow allocation size at the centralized apply
sites, unless the callee has already accounted for that same value, including a
zero-byte account for a wholly borrowed result, in which case the apply site
SHALL NOT charge it again.

The core SHALL provide a local work budget for Builtin phases whose uninterrupted
Go work scales with evaluated input and does not otherwise re-enter the Evaluator
or VM for that unit. A phase using this facility SHALL accrue one local step per
semantic unit. The budget SHALL synchronize with shared evaluation state every
128 pending units and SHALL flush any remainder before every return.
Synchronization SHALL charge the logical Reductions and observe the Engine-owned
Evaluation deadline and caller cancellation. Observing the deadline SHALL NOT
require a wall-clock read at every synchronization: the core MAY read the clock
at a reduced, fixed multiple of the synchronization interval. That cadence SHALL
be carried by the evaluation rather than by the budget, because a budget is
confined to one GoFunc call and a per-budget cadence would read the clock once
per call however short the call is. The interval between a deadline passing and
a Builtin terminating SHALL be bounded and documented: no more than a small fixed
number of synchronizations, plus any single opaque phase's own execution time as
today. Caller cancellation and the Reduction charge SHALL still occur at every
synchronization. Installing a deadline on an evaluation SHALL make the next
synchronization read the clock, so no evaluation inherits a cadence position from
an earlier one. No atomic ledger operation or clock read SHALL occur per local
step. A consumer SHALL NOT replace the facility with a
direct per-unit ledger charge, per-unit evaluation-state poll, or caller-context
check, and SHALL NOT double-charge callback execution already accounted by
re-entry. When a consumer assigns a budget to a callback-driven operation,
separate uninterrupted copying, traversal, and result-construction phases SHALL
retain their own ownership.

The Go API SHALL expose `NewBuiltinWorkBudget(context.Context)`,
`(*BuiltinWorkBudget).Step() error`, `(*BuiltinWorkBudget).Flush() error`, and
`(*BuiltinWorkBudget).Finish(error) error`.
A budget SHALL be confined to one GoFunc call and goroutine, SHALL latch and
replay its first synchronization error, and SHALL make an empty successful flush
idempotent. If a pending non-Terminal error and a Terminal flush error coexist,
the Terminal error SHALL win; otherwise the original validation/callback error
SHALL be preserved.

Settling a pending non-Terminal error through `Finish` SHALL check the armed
Evaluation deadline and caller cancellation even between scheduled clock reads
or when no local work remains pending. A reduction-limit failure SHALL retain
precedence over that check. Settling a nil error SHALL retain ordinary `Flush`
behavior; an existing Terminal input error SHALL retain its identity. Consumers
SHALL settle pending validation/callback errors through this operation before
returning them. Forced error settlement SHALL NOT charge local work twice.

Before a VM dispatches a GoFunc, its re-entry context SHALL carry the absolute
deadline already resolved for that VM run. An earlier non-zero deadline from an
outer evaluation SHALL win. Starting a Builtin or nested evaluator callback
SHALL NOT compute a fresh `now + timeout` deadline or otherwise restore time
already consumed by compilation or bytecode execution.

Every consumer migrated to this contract SHALL assign an accounting owner to each
reachable helper phase that scales with user input. An opaque scalable
library/helper call SHALL be replaced by an interruptible budgeted kernel,
rejected before entry by a deterministic input/work ceiling, or documented as a
bounded exception with its proof and maximum work. A check only before and after
opaque work SHALL NOT count as bounded interruption.

Calls into methods of host-provided Go `Value` implementations SHALL be recorded
as trusted-host boundaries and are outside the core-owned interruption guarantee.
Core-owned `Value` formatting, equality, hashing, and traversal receive no such
exception and SHALL satisfy the interruptible/bounded rule.

Builtin kernels and result validators SHALL obtain collection-length and
construction-depth limits from the active evaluator passed to the GoFunc. They
SHALL NOT use `env.Evaluator()` as dynamic policy because a child lexical
environment need not own the evaluator currently executing it.

Counter values are NOT required to be equal across evaluators, and are NOT
required to be equal across compiler versions for the same evaluator and the
same source: because the VM charges one reduction per instruction decode, a
compiler change that alters how many instructions a given form compiles to (a
superinstruction fusing what were previously several instructions into one, for
example) changes the reductions charged for evaluating that form, even with the
evaluator and the source both unchanged. A resource-limit ceiling a program
previously crossed at some iteration count MAY no longer be crossed at that count
after such a compiler change; this is expected, not a defect, but SHALL be
disclosed wherever a compiler change is documented as altering instruction count.

Reduction accounting, including synchronized Builtin batches, SHALL use the
existing evaluation state. Exceeding either ceiling SHALL raise
`Code: "ResourceLimitError"`, terminal per the non-catchability requirement, on
both execution paths. A failed full or partial batch flush SHALL be returned
before a result is published.

Allocation charging SHALL be incremental for structurally derived results: an
operation whose result shares substructure with one of its arguments SHALL charge
the storage it newly allocated, not a deep measure of the whole result, because
the shared substructure was charged when it was created. The core SHALL support a
zero-byte callee disposition meaning that the returned argument, stored member,
caller-supplied default, or other value is wholly borrowed; the centralized apply
site SHALL not charge that declared storage again. An operation that builds a
sequence or map from unrelated values SHALL charge the result's deep size.
Retained-state accounting SHALL keep using a deep measure, since a binding holds
its whole reachable structure alive.

#### Scenario: Shared substructure is charged once

- **WHEN** a loop extends an accumulator N times and each result shares the previous accumulator's storage
- **THEN** the total allocation charged SHALL grow linearly in N, and no iteration SHALL charge the shared prefix again

#### Scenario: A result the callee charged is not charged again at the apply site

- **WHEN** a GoFunc charges the ledger for the value it returns, and that value is then returned through a centralized apply site
- **THEN** the apply site SHALL NOT add its own shallow charge for that value, so a loop of N such calls charges O(N) in total rather than a size-proportional amount per call

#### Scenario: Borrowed results have zero allocation charge

- **WHEN** a Builtin returns an existing argument, stored collection, or caller-supplied default without allocating result storage
- **THEN** it SHALL mark zero result-allocation bytes and the apply site SHALL NOT charge the borrowed value's shallow size

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

#### Scenario: Scalable Builtin work is bounded

- **WHEN** a Builtin performs a long uninterrupted input-sized loop under an exhausted Reduction budget
- **THEN** its next bounded budget synchronization SHALL stop with `Code: "ResourceLimitError"` rather than allowing one dispatch to run unbounded

#### Scenario: Local Builtin steps do not touch shared state per unit

- **WHEN** a Builtin completes 127 uninterrupted logical work units
- **THEN** those steps SHALL use only its local counter, and the final flush SHALL perform one shared synchronization for the remainder

#### Scenario: Builtin checkpoint observes the Engine deadline

- **WHEN** scalable Builtin work runs after the Engine-owned Evaluation deadline expires while the caller context remains live
- **THEN** budget synchronization SHALL return `context.DeadlineExceeded` within the documented number of synchronizations of the expiry

#### Scenario: Short Builtins do not read the clock once per call

- **WHEN** an evaluation with an armed deadline calls many short Builtins, each constructing its own work budget and flushing a small remainder on return
- **THEN** the wall-clock reads SHALL be bounded by the documented fraction of synchronizations rather than reaching one per Builtin call

#### Scenario: A newly installed deadline is read at the next synchronization

- **WHEN** an Evaluation deadline is installed and a Builtin then synchronizes its budget
- **THEN** that synchronization SHALL read the clock rather than continue a cadence position established before the deadline existed

#### Scenario: VM time before a Builtin remains consumed

- **WHEN** a VM resolves its Evaluation deadline, performs substantial bytecode work, and then enters a long Builtin
- **THEN** the Builtin SHALL observe that same absolute deadline rather than receiving a new timeout interval

#### Scenario: Per-element re-entry has one accounting owner

- **WHEN** a Builtin re-enters the Evaluator or VM once for every input element and performs no separate scalable uninterrupted phase
- **THEN** those execution steps SHALL account for callback execution, while any separate input-copying, traversal, or result-construction phase SHALL retain its own Builtin work charge

#### Scenario: Opaque scalable work cannot bypass interruption

- **WHEN** an active Builtin would call an opaque helper whose work scales with user input
- **THEN** it SHALL use an interruptible kernel, enforce a deterministic pre-entry work bound, or carry a reviewed bounded-exception proof in the frozen inventory

#### Scenario: Child environments retain active evaluator limits

- **WHEN** a Builtin executes in a child lexical environment without its own evaluator
- **THEN** its collection-length and construction-depth checks SHALL still use the limits of the evaluator that dispatched the GoFunc

#### Scenario: Context observed within the reduction budget

- **WHEN** the caller's context is cancelled mid-evaluation
- **THEN** the evaluator SHALL stop within 1,024 reductions of the cancellation

#### Scenario: Per-evaluation counters are isolated

- **WHEN** two goroutines evaluate reduction-heavy forms concurrently on one engine under `-race`
- **THEN** each SHALL be bounded by its own counter and `go test -race` SHALL report no data race

#### Scenario: Evaluators agree on terminal behavior

- **WHEN** the same adversarial program is evaluated by the Evaluator and VM under identical limits
- **THEN** both SHALL terminate with the same terminal error class; counter values MAY differ between evaluators

#### Scenario: Direct Apply path is metered

- **WHEN** a host invokes a Lisp function via `core.Evaluator.Apply` without going through an `Engine` entry point, under tight limits
- **THEN** the evaluation SHALL be bounded by the same per-evaluation counters

#### Scenario: A compiler change may shift where a reduction ceiling is crossed

- **WHEN** a compiler version change alters how many VM instructions a given form compiles to (e.g. fusing several instructions into one), and the same source is evaluated against the same `MaxReductions` ceiling on both compiler versions
- **THEN** the iteration count at which the ceiling is crossed MAY differ between the two versions, and this difference SHALL NOT be treated as a counter-consistency defect
