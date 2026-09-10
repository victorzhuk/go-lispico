---
status: accepted
---

# Reduction and allocation metering use a fixed deterministic ledger

Per-evaluation metering now complements ADR 0007's structural-depth, reader-depth, collection-length, and cache-entry ceilings. Every evaluation carries two more hard ceilings: reductions and cumulative allocation bytes. The ledger is deterministic by construction: the same source under the same engine configuration charges the same units regardless of Go version, allocator behavior, or host architecture.

## Reduction model

Reductions are evaluator-local work units, not wall-clock time and not cross-evaluator comparable counters.

- Tree-walker: one reduction per form dispatch, plus one per apply-trampoline `GoFunc` dispatch.
- Macro expansion: one reduction per expansion step.
- Bytecode VM: one reduction per decoded instruction, plus one per `GoFunc` dispatch.
- Compiler: one reduction per emitted instruction.
- Builtin Go function (`GoFunc`): logical work accrues locally via `core.NewBuiltinWorkBudget(ctx)`, with `Step()` recording one unit and synchronizing reductions and caller cancellation with the shared eval state every 128 pending units. At most 127 units remain unsynchronized; the remainder flushes before return. Ordinary `Step()`/`Flush()` calls read an armed engine deadline every eight nonempty synchronizations per evaluation, bounding observation during continuing work to 1,024 local units plus any single opaque phase's execution time. Deadline installation and freshly materialized eval state make the next synchronization read the clock. The first sync error is latched and replayed by every subsequent `Step`/`Flush` call. Before returning a nonterminal error, `Finish(err)` charges only actual pending reductions, once, then checks the armed deadline and caller cancellation even when no units remain pending; a terminal synchronization error wins. An already-terminal input retains its identity. A latched synchronization error prevents further checks and charges.

The hot loops do not increment a shared atomic on every step. Both evaluators already keep a 128-step cancellation budget, so metering piggybacks that countdown and flushes consumed work at the existing sync points. This keeps the context-observation bound comfortably inside the required 1,024-reduction window while avoiding a new per-step branch or atomic write.

The Builtin work budget flushes on `Flush()` and at return from `GoFunc.Fn`, ensuring host-visible totals are exact at every observation point. The VM installs the run's resolved absolute deadline (`vm.deadline` via `reentrantCtx` armDeadline + `InstallReentrantDeadline`) before the first `GoFunc` dispatch — an earlier non-zero outer deadline wins; no fresh `now+timeout` is derived. This means a builtin observing its deadline at any point during execution sees the same pre-resolved instant the VM committed at dispatch, not a recomputed bound. On rearm of a retained re-entrant context across run boundaries, the new run's deadline is installed **before** the new generation is published (core/vm/vm.go `installReentrantDeadline` precedes the `RearmReentrantEvalState` generation store): a retained-context reader that materializes state in between must never govern the new run by the prior run's deadline. `TestCallReentrancy_RearmInstallsNewRunDeadlineBeforeGeneration` pins this ordering.

Reduction counts are compilation dependent, not just source dependent: because the VM charges per decoded instruction, a change to lispico's own bytecode compiler that alters how many instructions a form compiles to changes the reductions the same source charges, even though the ledger itself is unchanged. A `MaxReductions` boundary that a given source used to trip at some iteration count can move to a different count after such a change. This is expected — the determinism requirement below binds one lispico build's charges across hosts and Go versions; it does not bind one source's charges across lispico's own compiler revisions.

## Allocation model

Allocation charging is shallow and deterministic. It counts the produced value's own container cost, not a recursive deep walk of already-existing children. Values built incrementally in Lisp are therefore charged incrementally at each construction site; values materialized inside a Go builtin are charged once by their shallow result size.

### Fixed size table

| Unit | Charge |
| --- | ---: |
| Scalar value (`nil`, `bool`, `int`, `float`) | 16 bytes |
| String / symbol / keyword header | 16 bytes |
| String / symbol / keyword payload | `len(utf8 bytes)` |
| List header | 24 bytes |
| Vector header | 24 bytes |
| Collection element slot | 16 bytes per element |
| Hash map header | 32 bytes |
| Hash map entry | 64 bytes per key/value pair |
| Closure header | 64 bytes |
| Closure capture slot | 8 bytes per capture |
| Bytecode instruction | 4 bytes |
| Fused operation descriptor | 40 bytes per `chunk.Fused` entry |
| Reader node | 32 bytes per parsed node |
| Reader byte payload | `len(source bytes copied into values)` |
| Reader token plan unit | 32 bytes per planned token, EOF token included |
| Reader decoded string payload | `len(decoded)` per escaped string token |
| Reader numeric-conversion storage | `2*len(token) + 256` bytes per numeric token |
| Reader list cell | 32 bytes per linked cell, past `listFlatThreshold` only |
| Reader workspace slot | 16 bytes per logical slot |
| Reader map entry slot | 64 bytes per logical slot, below and above `hashMapSmallLimit` alike |
| Reader promoted-map construction header | 24 bytes, once per promoted map literal |
| Evaluator persistent-map node | 24 bytes + 64 per entry + `MeterTrieChildBytes` (8) per child |

The promoted-map construction header is `MeterCollectionHeaderBytes` (24) for the storage the reader's map builder obtains; it is not the `MeterHashMapHeaderBytes` (32) output term of `HashMapShallowBytes`, and the two are separate charges for separate storage rather than one charge counted twice. The persistent-map node row is an evaluator charge: `hamtNodeBytes`, applied through `hamtSizeBytes` on the `Assoc`/`Dissoc` path. No reader path builds a trie node, so no reader path charges it.

### Why these values are conservative

- `16` bytes per value slot matches the project baseline that a boxed Lisp value occupies one interface slot on supported 64-bit targets; keeping it fixed avoids platform drift.
- `24` bytes for list/vector headers corresponds to one slice header; element storage is charged separately through slots.
- `64` bytes per hash-map pair intentionally over-counts both the small sorted form and the promoted Go-map form. The exact in-memory shape differs by path; the ledger must stay simple and fail closed.
- `64 + 8*caps` for closures over-counts small closures a little, but it captures the closure object plus capture-array growth without consulting runtime layout.
- `32` bytes per reader node intentionally prices parse-tree shape higher than the minimum object footprint. Reader metering must reject wide flat literals before evaluation starts, even though the exact token/value mix varies.
- The reader's own units budget the buffers the read fills; they are not measurements of the Go structs behind them. `32` per planned token prices the token slice the second pass fills; `2*len(token) + 256` prices the two token copies a numeric conversion makes plus its bounded diagnostic; `16` per workspace slot and `64` per small-map entry slot reuse the table's slot and hash-map-pair prices for buffers that grow by doubling.

## Determinism requirement

The ledger MUST NOT depend on `unsafe.Sizeof`, allocator classes, pointer width, map bucket layout, or any other runtime-specific measurement. Those values vary across architectures and Go releases; a metering ceiling tied to them would make the same source pass on one host and fail on another. The published table is therefore normative even when the real heap footprint is smaller.

## Charge sites

The fixed table is applied only at evaluator-owned construction boundaries:

- reader work and reader storage, admitted inside the guarded read before the storage they describe is obtained (see *Guarded reader admission*); the context-free reader entry points install no budget and charge nothing;
- tree-walker collection literals and quasiquote construction;
- VM `OpMakeList`, `OpMakeVector`, `OpMakeMap`, and `OpClosure`;
- compiler-emitted bytecode and constant pools, charged before a compiled chunk is cached;
- shallow `GoFunc` results at the centralized apply sites, unless the callee already charged the ledger for that same value.

This keeps the meter complete without trying to instrument every composite literal or every Go allocation in the process.

### Guarded reader admission

`Dialect.ReadWithContextStats` is the guarded entry point. It installs a
per-read budget on the reader's scratch and admits every storage term below
*before* the storage it describes is obtained, so a source that cannot fit the
evaluation's remaining allowance is refused during the read rather than after
it. The context-free entry points — `core.Read`, `core.ReadOne`,
`Dialect.Read`, `Dialect.ReadWithMaxDepth`, `Dialect.ReadWithMaxDepthStats` —
install no budget: every guarded site is a no-op without one, and their forms,
stats and errors are unchanged (`TestReadWithContextStats_LegacyParity`).

Scan work is charged per byte across both passes — token counting and
parsing — and settled at every 128-unit checkpoint, where the ledger, the armed
engine deadline and caller cancellation are all observed
(`TestGuardedRead_ChargesEveryScannedByte`,
`TestGuardedRead_SynchronizesWithinBound`,
`TestGuardedRead_CancellationAndDeadline`). A terminal state outranks a syntax
error the same read would otherwise report
(`TestGuardedRead_TerminalStateOutranksSyntaxError`).

Six storage terms, each admitted at a fixed moment:

| Term | Charge | Admitted |
| --- | --- | --- |
| T1 token plan | `32 * tokens`, EOF token included | after counting completes, before the token buffer obtains storage |
| T2 decoded string payload | `len(decoded)` per escaped string token | in the same reservation as T1, before the second pass decodes anything |
| T3 numeric-conversion storage | `2*len(token) + 256` per numeric token | before `strconv` is entered |
| T4 output node | 32 plus that node's payload bytes | before the node value is constructed |
| T5 parser workspace | 16 per logical slot | before each growth, for the whole new logical capacity |
| T6 construction storage | three subterms, below | before the collection's storage is allocated |

- **T1** is reserved once by `readerBudget.reservePlan`. Counting itself
  obtains no storage: it refuses, it never charges. Scratch reuse from the pool
  never waives the charge (`TestGuardedRead_TokenPlanAdmission`,
  `TestGuardedRead_PoolReuseKeepsPayingThePlan`).
- **T2** covers escaped strings only. A token that aliases the input reserves
  nothing, and its node admits `32 + len(val)` under T4 as usual
  (`TestGuardedRead_EscapedPayloadAdmission`).
- **T3** uses checked arithmetic and is admitted on success and failure alike;
  a failed conversion is never refunded
  (`TestGuardedRead_NumericConversionStorage`,
  `TestGuardedRead_FailedConversionKeepsItsNodeCharge`). The conversion is
  opaque to per-byte work accounting, so it carries its own work bound: a
  numeric token longer than `MaxReductions/3` is refused before conversion is
  entered (`TestGuardedRead_NumericConversionAdmission`). The invalid-number
  diagnostic renders at most 128 bytes of source, truncation marker included,
  so an unbounded token cannot produce an unbounded error message
  (`TestGuardedRead_InvalidNumberDiagnosticIsBounded`).
- **T4** admits, then charges work, then updates stats. The payload term is
  skipped at source when the node comes from a token whose payload was already
  prepaid under T2. The admitted output total for a read equals
  `ReaderAllocationBytes(stats)` exactly
  (`TestGuardedRead_AdmitsOutputStorage`,
  `TestGuardedRead_PrepaidPayloadKeepsOutputTotals`). Generated nodes are
  included with no special case: a reader macro admits its two extra nodes and
  its head symbol's payload bytes.
- **T5** is a fresh-per-read doubling schedule — logical capacity 1, then
  doubling — charging the whole new logical capacity before each growth,
  because the old and new buffers coexist during the copy: 1→16, 2→48, 3→112,
  4→112, 5→240, 40→2032. Two buffers ride it: the parser's shared
  mark/truncate child scratch, one schedule for the whole read whose high-water
  spans all nesting rather than resetting per collection, and the top-level
  forms slice (`TestGuardedRead_WorkspaceHighWaterSpansNesting`). The schedule
  is logical, not physical: retained capacity avoids the Go allocation, never
  the charge.
- **T6** has three subterms: the flat collection copy-out, `ValueSlotsBytes(n)`;
  linked list cells, `32 * n`, past `listFlatThreshold` only, so a 32-child
  list links none and a 33-child list admits `33*32`; and the map entry buffer
  at 64 bytes per logical slot on T5's doubling schedule, charged only when
  inserting a new key grows the buffer — a key the map already holds charges no
  growth. The entry-buffer term runs on the same schedule below and above
  `hashMapSmallLimit`: a literal that promotes keeps growing the one buffer and
  adds `MeterCollectionHeaderBytes` (24) exactly once for the promotion. No
  reader path builds a persistent-map node, so none charges one
  (`TestGuardedRead_ExactCharges`, `TestGuardedRead_ConstructionStorage`,
  `TestGuardedRead_MapConstructionContracts`, which pins the builder form
  `large.m != nil && large.root == nil`).

One linked list cell has two unit prices, and both are correct. The reader
admits `readerListCellBytes` (32) for a cell it links while building a fresh
list from its own workspace; `List.Cons` charges `ListShallowBytes(1)` (40) for
a cell it prepends to an existing list. Each owner charges the storage it
actually obtains at its own site — the reader's cells come out of one planned
construction whose header and slots are already admitted, `Cons` allocates a
standalone shallow list — and neither price is derived from the other. They are
not reconciled into a single unit, because doing so would make one of the two
sites over- or under-charge for storage it does not obtain.

Worked totals under the default limits, for orientation:

| source | admitted |
| --- | ---: |
| `a` | 113 |
| `'a` | 246 |
| `(a b c)` | 499 |
| `"ab"` (zero-copy) | 114 |
| `"a\nb"` (escaped) | 115 |
| `12345` | 378 |

#### No duplicate charge

`ReaderStats` values are unchanged: they still describe output only, never
workspace, conversion, construction or scan work
(`TestGuardedRead_StatsUnchanged`).

Before this change the runtime charged the reader's output total once,
post-parse, after `Read` returned. That same total is now admitted
incrementally, before each allocation, inside the read, and the post-parse
charge is gone. The net total across a read is unchanged; what changed is when
it is admitted, and that direct callers of the guarded entry point are charged
too.

There is no credit path. A payload prepaid under T2 is not refunded when its
node is built — the node simply omits the payload term. Totals are
byte-for-byte identical either way, and the ledger keeps the invariant that
`AllocationBytes` is monotonically non-decreasing for the lifetime of one
evaluation (`TestGuardedRead_AdmittedBytesNeverDecrease`).

#### Scratch release

`readerScratch.release` clears the reference-bearing scratch slots that the
read being released actually wrote, in batches of at most 128, charging one
work unit per cleared slot. It does not clear the pooled buffer's full
capacity: the pool preserves capacity across reads, so clearing to capacity
billed a small read for slots a previous, larger read had grown — a
cross-engine, unbounded charge. The invariant that makes the narrower clear
safe is that slots above the previous read's high-water are already zero. A
retained buffer larger than the current read's allocation ceiling is dropped
rather than pooled, and on terminal failure the scratch is dropped rather than
traversed (`TestGuardedRead_ScratchReleaseDropsOversizedCapacity`,
`TestGuardedRead_FailedReadReleasesScratch`,
`TestGuardedRead_RetainedASTSurvivesScratchReuse`).

### The apply-site fallback charge and its opt-out

The apply site's shallow `GoFunc` result charge (`ValueShallowBytes(result)`,
in both `core/eval.go`'s tree-walker apply loop and `core/vm/vm.go`'s
`OpCall` `GoFunc` case) exists to catch results the fixed table has no other
charge site for. Left unconditional, it double-charges any builtin whose
result derives structurally from one of its own arguments — `cons`, `conj`,
`concat`, and friends on a shared `List`/`Vector` allocate O(1) new storage
but their *result* is still the whole accumulated structure, so a
shallow-size charge on every call turns an O(1) structural update into an
O(n) charge, and repeated calls into the same quadratic-charging defect this
change removed from accumulation.

`core.ChargeGoFuncResultBytes(ctx, n)` is the opt-out: a builtin that already
knows its own incremental cost calls it with that cost immediately before
returning the value it describes, and the apply site's fallback charge is
skipped for that call. Contract:

- Call exactly once per `GoFunc.Fn` invocation, immediately before returning
  the value `n` describes — the value returned must be the same value `n`
  was computed for.
- `BeginGoFuncDispatch`/`EndGoFuncDispatch` bracket each dispatch so the
  callee-charged marker is visible only to that call's own apply-site
  fallback check, not to an outer frame's — required because a `GoFunc` like
  `map` re-enters `apply`/VM `call` once per element on the same
  `evalState`, and a naive marker would mistake an inner element-lambda's
  charge for `map`'s own result already being billed.
- Both evaluators enforce the same rule off the same marker, so a builtin
  that opts out charges identically under the tree-walker and the VM.

A zero-byte charge (`n == 0`) marks a wholly borrowed result — an existing
argument, stored member, or caller-supplied default returned as-is. The
apply site skips the fallback shallow charge without adding any bytes. A
non-zero `n` charges exactly that many bytes; mixed results combining fresh
and borrowed components must pass only the fresh delta. The normative charge
site for `GoFunc` results is now the centralized apply site *unless* the
callee opted out via `ChargeGoFuncResultBytes`; builtins that return
structurally derived or wholly borrowed results are the primary opt-out
consumers.

### Trusted-host boundary

Host-provided `GoFunc` implementations (registered by plugins) are
trusted-host boundaries: the core-owned interruption guarantee — reduction
budgets, allocation ceilings, cooperative cancellation — applies at the
*call boundary* and at *charge sites*, but the code inside a `GoFunc.Fn`
body runs on the host's trust. The `BuiltinWorkBudget` lets well-behaved
builtins participate in cooperative metering, but core cannot enforce
metering correctness on untrusted Go code that ignores the budget API.

## Per-evaluation settlement and observation

Charges accrue during an evaluation and settle at its end. `Engine.Eval`,
`EvalWithBindings`, and `LoadScope` each reach one settlement point on every
return, panic unwind included: pending reductions flush, pending retained
charges settle, and the evaluation lease returns exactly once
(`core.FinishEval`). Statistics and the `OnEval` event are published from that
same point, after settlement.

What a host observing the meter can rely on:

- The published outcome is the returned outcome. Settlement itself can fail —
  a retained-meter denial turns an otherwise successful evaluation into a
  terminal `ResourceLimitError` — so the event's failure status and cause are
  recorded once that verdict is known. A settlement error surfaces whenever
  the evaluation otherwise succeeded; when the evaluation already failed, only
  a terminal settlement error replaces its error.
- `EvalEvent.Duration` covers the work required to produce the returned
  outcome, settlement included. It excludes the callbacks' own execution.
- Failures that never execute a form are counted like any other: a lease
  refused by the meter at `core.StartEval`, a reader refusal, a binding write
  refused before the first form. Each invocation contributes exactly one
  evaluation count and one event per registered callback.
- On the reader and evaluation failure paths the event carries the same public
  wrapped error the caller receives (`read: %w`, `eval: %w`); the cause is
  reachable with `errors.As` / `errors.Is`, not by pointer identity.

Settlement is not a rollback. A retained charge denied at settlement fails
after the evaluation's writes have already occurred: bindings the evaluation
created stay in their env, and only the meters already charged in that same
settlement are released back (`settleRetained`). The fail-closed guarantee in
ADR 0012 covers the individual write that would breach a per-env capacity
ceiling — that write does not occur — not an evaluation whose retained charge
a meter later denies.

A panic does not cross the settlement point. A recovered evaluation panic is
turned into the evaluation's error before settlement rules on the outcome, so
a terminal settlement error still replaces it. A host meter that panics from
inside the reduction flush or the retained charge is recovered in
`(*evalState).finishEval` — the single place that turns a settlement panic into
an error — and reported as a settlement error carrying the `CodePanic` cause;
the evaluation lease is returned on that path as well. That error is not
terminal, so it follows the same precedence as any other settlement error: it
surfaces when the evaluation otherwise succeeded, and an evaluation that
already failed keeps its own cause. An `OnEval` observer that panics is
contained per callback and logged at `Warn` — the settled result and error
stand as published, counted once, and every other registered callback still
receives its one event — because the evaluation is over by then and an observer
must neither become the evaluation's outcome nor cost the observers behind it
theirs.

An abandoned settlement leaves nothing behind for the next one. The eval state
outlives the evaluation on the caller's context and `StartEval` reuses it, so
`finishEval`'s recover resets the pending retained ledger
(`(*evalState).resetRetained`): a panic in the reduction flush cannot bill the
next evaluation on that context for charges this one never settled. The
compensating release is symmetric on the panic path for the same reason.
`settleRetained` releases the meters it has already charged from a `defer` that
runs unless the charge loop ran to completion, so a meter that panics part way
through leaves no meter holding a charge for a settlement that never happened,
exactly as a denial part way through does.

Two limits bound that containment:

- A meter that panics inside `ReturnEval` still unwinds into the caller. The
  recover runs before the lease is returned, which is what makes the lease
  return survive a panic on every path it does cover.
- Containment covers the source-evaluation entry points only. The
  `Engine.Call` family settles through `core.FinishEval` at `callBoundary` but
  fires its `OnPluginCall` observers uncontained, and the hot-reload path
  (`runtime/watch.go`) flushes pending state without settling through
  `core.FinishEval` and publishes no event.
