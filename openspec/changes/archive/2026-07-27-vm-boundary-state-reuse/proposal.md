## Why

Every top-level `Call` whose body dispatches a `GoFunc` pays two costs that
the spec already intended to be amortized, but that the implementation resets
per call:

- One heap allocation of the re-entrant context wrapper
  (`core.AdoptEvalStateWithMeter` → `*lazyEvalStateCtx`, core/eval.go:299) —
  20.3% of the Rule benchmark's allocated bytes.
- One wall-clock read (`armDeadline` → `time.Now`, vm.go:401-411) — 8.2% of
  Rule CPU. In the profile the entire `reentrantCtx` chain is 13.6% of Rule
  time.

The cause is cache scope. `vm.reentryCtx` (vm.go:118) memoizes the wrapper
*within* one VM run — nested callbacks reuse it — but all three reset paths
(`reset`, `Reset`, `ResetIncremental`, vm.go:199/225/266) unconditionally
zero it, and `Engine.Call`/`Fn.Call` reset the pooled VM before every
dispatch. So a yagel rule invoked ten thousand times builds ten thousand
identical wrappers around the same outer context and reads the clock ten
thousand times — for host functions that never look at either.

The "Lazy re-entrant evaluation state" requirement already pushed the heavy
`*evalState` behind first use; this change pushes the remaining wrapper
allocation and the clock read behind first use too. Expected effect on the
article harness: Rule −~95 B/op and −1 alloc plus up to ~20% latency
(reentrant chain + clock), Callback similar in absolute terms (318ns today,
~50% of its allocations are this wrapper per the round-3 profile). Combined
with `compiler-constant-literal-folding` this takes Rule decisively past
GopherLua (711ns) and toward the zero-allocation goal.

## What Changes

- The re-entrant context wrapper becomes VM-owned storage reused across runs:
  resetting a VM does not discard it, and a subsequent run passing the same
  outer context reuses it after re-arming its per-evaluation fields (budget
  counters, deadline slot, generation). A run entering with a *different*
  outer context rebuilds it — correctness never depends on the cache hitting.
- Retention safety via a generation counter: the wrapper carries the VM run
  generation it was issued for. A host function that illegally retains the
  context past its call observes a stale generation and gets fail-safe
  behavior (no access to a recycled run's internals), preserving the existing
  snapshot-consistency requirement.
- The deadline clock read moves from wrapper construction (`armDeadline` at
  first GoFunc dispatch) to first *observation*: the wrapper computes
  `now + timeout` only when something actually asks — a `Deadline()` call, a
  poll checkpoint crossing the budget, or re-entry that must inherit the
  deadline. Host functions that never look at the deadline never trigger the
  read. This is the posture the "Boundary call efficiency" requirement
  already states ("armed lazily at the first in-evaluation checkpoint");
  today's code arms earlier than the spec requires.

## Capabilities

### Modified Capabilities

- `runtime-api`: "Boundary call efficiency" — tightens the per-call allowance
  from "MAY allocate at most one evaluation-state value" per call to
  amortized-zero across repeated calls with the same outer context, and
  extends the no-clock-read guarantee to GoFunc-dispatching bodies whose
  host functions never observe the deadline.
- `bytecode-vm`: "Lazy re-entrant evaluation state" — the wrapper itself
  becomes reusable VM-owned state with generation-guarded retention safety;
  the deadline read becomes observation-lazy.

## Impact

- Code: `core/vm/vm.go` (reentryCtx lifecycle, reset paths, armDeadline),
  `core/eval.go` (`lazyEvalStateCtx` re-arm + generation), tests.
- Risk — retained contexts: a GoFunc that stores its ctx and uses it after
  returning currently sees a dead-but-consistent snapshot; under reuse it
  would see a re-armed wrapper. The generation guard is the load-bearing
  mitigation and gets adversarial tests (retain-and-read, retain-and-reenter,
  retain-across-engine-close). This is the same class of hazard the pooled-VM
  design already manages for stacks and frames.
- Risk — deadline drift: computing `now + timeout` at first observation
  instead of first dispatch starts the clock strictly later, never earlier —
  a timeout can only get *more* generous by nanoseconds, and only for calls
  that observe deadlines at all. The existing spec text already blesses
  lazy arming; parity with the tree-walker's deadline behavior is pinned by
  existing deadline-ownership tests.
- Concurrency: wrapper reuse is confined to a single VM, which is already
  single-run-at-a-time; `-race` over concurrent `Call`s exercises the pool
  handoff.
