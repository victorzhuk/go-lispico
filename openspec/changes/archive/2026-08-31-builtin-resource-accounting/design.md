## Context

Evaluator and VM dispatch already charge one Reduction per GoFunc call. The
current `PollEvalState` call decrements an atomic counter on every invocation, so
calling it once per element is not the batched Builtin primitive required by ADR
0011. Direct ledger charging has the same defect, while `ctx.Err()` cannot see an
Engine-owned deadline stored only in evaluation state.

The VM also owns an absolute deadline for the current run. Its re-entry context
currently reuses an existing evaluation state unchanged or lazily computes
`now + timeout`; either behavior can omit or reset the VM's already armed
deadline when a GoFunc starts after substantial bytecode work.

Centralized apply sites charge the shallow size of every Builtin result unless
the callee marks the result as already accounted. Passing `0` through that marker
is the existing mechanism for a result that borrows all of its storage, but the
contract does not currently name this case and accessors do not use it uniformly.

## Goals / Non-Goals

**Goals:**

- Bound scalable uninterrupted Builtin work with the same Terminal errors as the
  rest of evaluation.
- Keep polling and Reduction accounting batched.
- Charge allocation only for storage constructed by the current evaluation.
- Establish an auditable primitive and ownership rules that later Builtin changes
  can apply without duplicating metering architecture.

**Non-Goals:**

- Make Reduction counts equal between Evaluator and VM.
- Meter blocking external I/O beyond the caller context contract.
- Estimate real Go heap usage or replace the fixed Size table.
- Add checkpoints to constant/bounded Builtin work or double-charge loops whose
  per-element work already re-enters an execution path.
- Migrate every currently registered stdlib Builtin; that bounded audit is
  `stdlib-builtin-resource-migration`, after the semantic changes settle.

## Decisions

### Accrue Builtin work locally and synchronize in fixed batches

Add `core.NewBuiltinWorkBudget(ctx context.Context) *BuiltinWorkBudget`.
`Step() error` records one semantic unit in a plain local counter. At 128 pending
units it synchronizes that batch with the shared evaluation state, which charges
Reductions and checks the resolved Evaluation deadline and caller cancellation.
`Flush() error` synchronizes the remainder and is mandatory on every success,
short-circuit, validation-error, callback-error, and Terminal-error return path.
The budget is single-goroutine and single-call; it latches the first sync error,
and later `Step`/`Flush` calls return that same error without more work. A
successful `Flush` with no pending units is idempotent. No result may be published
after a failed final flush.

A Builtin phase whose iteration count scales with evaluated input and which does
not re-enter the Evaluator or VM calls `Step` once per semantic unit: one visited
item, emitted item, comparison scheduling decision, or lookup step as assigned by
the consuming change. The maximum unobserved work is therefore 127 units. Neither
`Step` nor a semantic unit performs an atomic ledger operation or clock read.

A callback-driven Builtin relies on callback re-entry for callback evaluation,
but separately budgets input copying, tuple alignment, result assembly, and any
other uninterrupted phase. Direct `ChargeEvalReductions`, per-unit
`PollEvalState`, and `ctx.Err()` were rejected: the first two perform shared
counter work every iteration and the last cannot observe the Engine deadline.
One dispatch regardless of input size was rejected because a single Builtin call
could then monopolize the host past configured limits.

### Preserve the VM's absolute deadline across Builtin re-entry

Before dispatching a GoFunc, the VM re-entry path SHALL put the absolute deadline
already resolved for that VM run into the shared evaluation state. If an outer
state already exists, the earlier non-zero deadline wins. The path SHALL NOT
derive a fresh deadline from the timeout duration at first Builtin observation.
This preserves time consumed by compilation and bytecode before the callback and
keeps nested evaluator callbacks on the same deadline.

`BuiltinWorkBudget` construction performs no deadline reset. Its first batch
sync, including a final flush smaller than 128, therefore observes the same
absolute bound as the running VM. A regression performs substantial VM work
before entering a long Builtin and proves the deadline is not restarted.

### Keep one shared terminal checkpoint

Builtins do not invent error precedence. They return the result of budget
synchronization immediately, allowing the core to decide whether a resource
ceiling, Evaluation deadline, or caller cancellation is observed at that point.
When a Builtin already holds a non-Terminal validation/callback error and its
mandatory flush returns a Terminal error, the Terminal error wins; otherwise the
original error is preserved. Tests pin terminal classes independently under
Evaluator and VM execution but do not assert equal counters.

### Make opaque scalable work interruptible or deterministically bounded

A wrapper around an opaque O(n) or O(n log n) operation such as
`strings.Split`, `strings.ReplaceAll`, `sort.SliceStable`, `Value.Equals`, deep
formatting, or collection flattening cannot claim bounded interruption merely by
checking before and after the call. Each inventoried opaque phase SHALL be
replaced by an interruptible kernel with budget steps, protected by a
deterministic input/work ceiling checked before entry, or listed as a reviewed
bounded exception with the proof and bound. The implementation SHALL NOT close
this change while an opaque phase that scales with user input remains
unclassified.

Host-provided Go `Value` implementations are trusted extension code, like Go
plugins. Time spent inside their `String`, `Equals`, or other methods is outside
the bounded core-owned kernel guarantee because Go offers no safe preemption or
work estimate for an arbitrary method. The inventory marks each such call site
as a trusted-host boundary. Core-owned `Value` implementations receive no such
exception: their scalable formatting, equality, hashing, and traversal must be
interruptible or deterministically bounded.

### Mark wholly borrowed results with a zero-byte callee charge

Immediately before a Builtin returns an existing argument, stored member,
caller-supplied default, or another value for which it allocated no storage, it
marks the result as callee-accounted with zero bytes. The apply site then skips
its fallback shallow charge. A result that combines borrowed and fresh structure
charges only the fresh portion through the same marker.

Returning a borrowed value unmarked was rejected because a large map/vector can
spuriously exhaust allocation budget. Disabling the centralized fallback was
rejected because ordinary Builtins still need fail-closed default accounting.

### Keep the full stdlib audit out of the foundation

This change proves the primitive with synthetic GoFuncs and the dependent
`get-in`/CL slices prove real consumers. `stdlib-builtin-resource-migration`
freezes the complete registration/helper/result inventory and migrates existing
stdlib families after lookup, adapter, and nil semantics settle. Keeping that
audit out of this prerequisite prevents a repository-wide migration from
blocking the smaller behavior changes that motivated the primitive.

## Risks / Trade-offs

- [Risk] New logical steps change Reduction thresholds → disclose the metering
  model change and test terminal behavior rather than exact cross-engine counts.
- [Risk] A loop is charged twice through both budgeting and evaluator callbacks →
  require each consuming change to assign exactly one owner to every scalable
  phase.
- [Risk] Zero-byte marking hides a real allocation → reserve it for wholly
  borrowed results and retain incremental/deep charging for constructed values.
- [Risk] Budgeting a comparator changes sort error propagation → stop immediately
  after budget sync or callback failure and preserve the first error unchanged.
- [Risk] A final partial batch is lost on early return → structure each Builtin
  so every return path flushes once, and enforce that shape with focused tests.
- [Risk] A child environment hides the active evaluator's limits → pass the
  `eval` argument supplied to the GoFunc into kernels/resource helpers; query
  `CollectionLimiter` and `ConstructionDepthEvaluator` there and never rediscover
  dynamic limits through `env.Evaluator()`.

## Migration Plan

Amend the contract/ADR, add failing synthetic batch/deadline/result tests, and
land the primitive. Dependent behavior slices then consume it, followed by the
full stdlib migration. Rollback removes the Builtin budget and borrowed-result
contract; no stored data changes.
