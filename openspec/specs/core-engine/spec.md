# core-engine Specification

## Purpose

The core-engine capability provides the core interpreter functionality for the system, registered and made ready for use when the system initializes.

## Requirements

### Requirement: core-engine implementation
The system SHALL implement the core-engine functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the core-engine SHALL be ready for use

### Requirement: Concurrent evaluation safety

The core evaluator SHALL support concurrent `Eval` and `Apply` calls on a single
engine without data races or cross-call state corruption. Per-evaluation state —
macro-expansion depth, call depth, and the `recur`/loop counter — SHALL be scoped
to a single evaluation, not shared across goroutines.

#### Scenario: Concurrent evaluation is race-free

- **WHEN** multiple goroutines evaluate on one engine concurrently
- **THEN** each SHALL return the correct result and `go test -race` SHALL report no data race

#### Scenario: recur state does not leak across goroutines

- **WHEN** one goroutine runs a `loop` while another evaluates a bare `(recur ...)` outside any loop
- **THEN** the bare `recur` SHALL always error "recur outside loop", regardless of the other goroutine's loop

### Requirement: Typed evaluation errors

Evaluation failures SHALL be reported as `*core.LispicoError` carrying a `Code`,
and SHALL include a source position (`Line`, `Col`, `Source`) when the failing form
carries one. An uncaught `throw` SHALL surface as a `*core.LispicoError`, not an
untyped error.

#### Scenario: errors.As recovers a typed error

- **WHEN** an evaluation fails on arity, type, an undefined symbol, or a general eval error
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and `lispicoErr.Code` SHALL classify the failure

#### Scenario: Uncaught throw is typed

- **WHEN** `(throw "boom")` is evaluated with no enclosing `try`
- **THEN** `errors.As(err, &lispicoErr)` SHALL succeed and the error SHALL carry the thrown value's rendering

### Requirement: Literal element evaluation

Evaluating a vector `[...]` or map `{...}` literal SHALL evaluate each element,
producing a new immutable value.

#### Scenario: Vector and map literals evaluate elements

- **WHEN** `[1 x]` or `{:a x}` is evaluated with `x` bound to `99`
- **THEN** the result SHALL be `[1 99]` or `{:a 99}` respectively

#### Scenario: Quasiquote expands inside maps

- **WHEN** `` `{:a ~x} `` is evaluated with `x` bound to `99`
- **THEN** the result SHALL be `{:a 99}`

### Requirement: Reader errors carry token positions

Reader errors SHALL report the line and column of the offending token whenever the
tokenizer recorded one — including invalid numeric literals and unexpected EOF —
never a placeholder `0,0`.

#### Scenario: Invalid number reports its position

- **WHEN** parsing source containing an invalid numeric literal on line 3
- **THEN** the returned `*core.LispicoError` SHALL carry `Line: 3` and the token's column

#### Scenario: Unexpected EOF reports the end position

- **WHEN** parsing source that ends mid-form
- **THEN** the returned error SHALL carry the EOF token's recorded line and column

### Requirement: Structural recursion is bounded

The reader and the evaluator SHALL bound structural recursion so that no input can
exhaust the Go stack. The reader SHALL enforce a nesting-depth ceiling while
parsing lists, vectors, and maps; the evaluator SHALL enforce a structural-depth
ceiling while descending `Vector` and `HashMap` literals and expanding quasiquote.
Exceeding either ceiling SHALL return a `*core.LispicoError`, never a Go panic and
never a fatal stack overflow. The reader ceiling SHALL be fixed at parser
construction (the reader carries no `context`); the evaluator ceiling SHALL be
tracked per evaluation, not on a shared engine field, consistent with the
concurrent-evaluation contract.

#### Scenario: Deeply nested source fails closed instead of crashing

- **WHEN** source consisting of millions of unbalanced opening delimiters is read
- **THEN** `Read` SHALL return a `*core.LispicoError` reporting the depth limit, and the process SHALL NOT abort with a fatal stack overflow

#### Scenario: Deeply nested literal is bounded during evaluation

- **WHEN** a vector, map, or quasiquote literal nested past the structural-depth ceiling is evaluated
- **THEN** evaluation SHALL return a `*core.LispicoError` reporting the depth limit, not a panic or a fatal stack overflow

#### Scenario: Structural depth does not leak across goroutines

- **WHEN** two goroutines evaluate deeply nested literals concurrently on one engine
- **THEN** each SHALL be bounded by its own per-evaluation structural-depth counter and `go test -race` SHALL report no data race

### Requirement: Global binding cells

`Env` SHALL expose a stable binding cell per bound name: the cell created when a
name is first bound in a scope SHALL remain the write-through target for every
later rebind of that name in that scope, so a holder of the cell observes rebinds
and deletions without re-walking the scope chain. Cell reads SHALL be safe under
concurrent binds, guarded by a short read lock (not the full chain walk),
preserving the concurrent evaluation safety requirement.

#### Scenario: Cell observes rebind

- **WHEN** a caller resolves the cell for a bound name and the name is subsequently rebound in the same scope
- **THEN** reading through the held cell SHALL return the new value

#### Scenario: Cell observes deletion

- **WHEN** a caller resolves the cell for a bound name and the name is subsequently deleted from that scope
- **THEN** reading through the held cell SHALL report the name unbound rather than returning the stale value

#### Scenario: Reads race-free with writes

- **WHEN** goroutines read through held cells while another goroutine rebinds the same names
- **THEN** each read SHALL return either the prior or the new value and `go test -race` SHALL report no data race

### Requirement: Tree-walker batched cancellation observation

The tree-walking evaluator SHALL observe context cancellation and the engine
evaluation deadline on the same bounded budget as the bytecode VM: at most a
fixed node budget apart, and unconditionally at every `apply` trampoline
iteration, so loops and recursion observe cancellation within one iteration or
call. Error shape SHALL be unchanged.

#### Scenario: Tree-walker loop observes cancellation

- **WHEN** the caller's context is cancelled while a `loop`/`recur` body iterates on the tree-walker
- **THEN** evaluation SHALL stop with a context error no later than the next trampoline iteration

#### Scenario: Tree-walker straight-line budget

- **WHEN** the caller's context is cancelled during evaluation of a long form sequence on the tree-walker
- **THEN** evaluation SHALL stop with a context error within the fixed node budget

### Requirement: Map representation efficiency

`HashMap` SHALL keep its public semantics — immutable operations, key domain,
`Int`/`Float` key distinctness, deterministic iteration — while meeting
efficiency bounds: a map operation SHALL NOT format a key into a string;
iterating a map SHALL NOT allocate or re-sort per call for maps at or below the
small-map threshold; and constructing, reading, or copying a small map SHALL
allocate O(1) objects. Promotion between the small and large representations
SHALL be semantically invisible: equality, iteration order rules, printing, and
immutability are identical at both representations.

Above the small-map threshold an immutable update SHALL NOT copy the whole map:
`Assoc` and `Dissoc` SHALL share the untouched majority of the structure with the
receiver, so that the storage a single update allocates is bounded by the depth of
the structure rather than by its entry count, and extending a map n times costs
O(n log n) in total rather than O(n²). The receiver SHALL be unaffected by an
update derived from it.

The hash backing that structure SHALL be derived from fixed constants rather than
a per-process random seed. A randomized seed would make the structure's shape, and
anything derived from it, differ across restarts for identical input, which
contradicts the determinism this requirement states.

#### Scenario: Small-map operations are allocation-bounded

- **WHEN** a map literal with at most the threshold number of keys is built, read with `Get`, extended with `Assoc`, and iterated
- **THEN** `Get` and iteration SHALL allocate nothing and `Assoc` SHALL allocate only the new map's storage

#### Scenario: Numeric keys never format

- **WHEN** `Get`, `Set`, `Assoc`, or `Dissoc` runs with an `Int` or `Float` key
- **THEN** the operation SHALL NOT allocate a formatted string representation of the key

#### Scenario: Promotion is invisible

- **WHEN** a map grows past the small-map threshold via `Assoc` and later shrinks via `Dissoc`
- **THEN** equality with a same-pairs map, iteration determinism, and immutability SHALL hold identically before and after promotion

#### Scenario: Iteration order is deterministic

- **WHEN** the same map value is iterated or printed repeatedly, at either representation
- **THEN** the order SHALL be identical on every iteration and identical across both evaluators

#### Scenario: Extending a large map does not copy it

- **WHEN** a map above the small-map threshold is extended by `Assoc` at sizes spanning two orders of magnitude
- **THEN** the bytes and allocations a single call charges SHALL stay bounded as the map grows rather than rising in proportion to its entry count, and the receiver SHALL remain unchanged and independently readable

#### Scenario: Colliding keys stay retrievable

- **WHEN** a large map holds distinct keys whose hashes agree in every bit position the structure discriminates on
- **THEN** each key SHALL resolve to its own value, `Dissoc` of one SHALL leave the others intact, and `Len` SHALL count them separately

#### Scenario: Structure shape does not vary across processes

- **WHEN** the same sequence of map operations runs in separate processes
- **THEN** the resulting map SHALL print identically and iterate identically in every run

### Requirement: Terminal errors are not catchable

`try`/`catch` SHALL NOT intercept terminal errors in either evaluator. The
terminal classes are: `context.Canceled` and `context.DeadlineExceeded`
(matched by `errors.Is`, including wrapped forms) and `*core.LispicoError`
with `Code: "ResourceLimitError"` (matched by `errors.As`). A terminal error
SHALL unwind every active handler, frame, and freeze record and surface to the
host boundary unchanged in class. Values raised by the `throw` special form
SHALL remain catchable regardless of their content — the filter applies to Go
error classes, never to thrown Lisp values.

#### Scenario: Deadline evasion loop terminates

- **WHEN** `(loop [] (try body (catch e nil)))` runs under an expired engine deadline or cancelled context on either evaluator
- **THEN** evaluation SHALL stop with an error satisfying `errors.Is(err, context.DeadlineExceeded)` or `errors.Is(err, context.Canceled)`, and the `catch` handler SHALL NOT observe it

#### Scenario: Nested resource-limit error is not caught by the VM

- **WHEN** a resource ceiling trips inside a closure or GoFunc invoked under an enclosing `try` on the bytecode VM
- **THEN** evaluation SHALL fail with `Code: "ResourceLimitError"` and the enclosing handler SHALL NOT run

#### Scenario: Explicit throws stay catchable

- **WHEN** a program evaluates `(try (throw "context deadline exceeded") (catch e e))` on either evaluator
- **THEN** the handler SHALL receive the thrown value and evaluation SHALL succeed

#### Scenario: Evaluators agree on terminal class

- **WHEN** the same terminal-error-producing program runs on the tree-walker and on the VM under identical limits
- **THEN** both SHALL surface the same terminal error class to the host

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
Evaluation deadline and caller cancellation. No atomic ledger operation or clock
read SHALL occur per local step. A consumer SHALL NOT replace the facility with a
direct per-unit ledger charge, per-unit evaluation-state poll, or caller-context
check, and SHALL NOT double-charge callback execution already accounted by
re-entry. When a consumer assigns a budget to a callback-driven operation,
separate uninterrupted copying, traversal, and result-construction phases SHALL
retain their own ownership.

The Go API SHALL expose `NewBuiltinWorkBudget(context.Context)`,
`(*BuiltinWorkBudget).Step() error`, and `(*BuiltinWorkBudget).Flush() error`.
A budget SHALL be confined to one GoFunc call and goroutine, SHALL latch and
replay its first synchronization error, and SHALL make an empty successful flush
idempotent. If a pending non-Terminal error and a Terminal flush error coexist,
the Terminal error SHALL win; otherwise the original validation/callback error
SHALL be preserved.

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
- **THEN** the next bounded budget synchronization SHALL return `context.DeadlineExceeded`

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

### Requirement: Env owned-capacity accounting

The core evaluator SHALL charge an env's owned-capacity counter on every new
binding write — `def` / `let` / `fn` params / `defn` / `defmacro` in the
tree-walker and per-call frame env writes in the VM — and SHALL raise
`Code: "ResourceLimitError"` (terminal) when a new binding would exceed the
env's configured byte or slot ceiling, leaving the env unmodified. Rebinding
through an existing `Cell` and reviving a tombstoned `Cell` SHALL NOT charge.
Deleting a binding SHALL tombstone without decrementing. `Rebuild` SHALL
preserve `*Env` identity and live `*Cell` identity, drop tombstoned cells,
recompute counters, and bump the name generation so cached resolutions of
dropped cells invalidate. Counters SHALL be uniform across persistent and
transient envs; a transient env's counter dies with the env. Capturing an env
through a closure SHALL NOT transfer or double-count ownership.

#### Scenario: Slot ceiling fails closed

- **WHEN** an env with `MaxRetainedSlotsPerEnv: 5` receives a sixth new binding
- **THEN** the write SHALL fail with `Code: "ResourceLimitError"` and the prior five bindings SHALL remain intact

#### Scenario: Byte ceiling fails closed

- **WHEN** an env's retained bytes would exceed `MaxRetainedBytesPerEnv` on a new binding
- **THEN** the write SHALL fail with `Code: "ResourceLimitError"`

#### Scenario: Rebind does not charge

- **WHEN** an existing binding is rebound to a new value
- **THEN** the env's slot and byte counters SHALL NOT increase

#### Scenario: Delete tombstones but does not release

- **WHEN** a binding is deleted and the env's retained counters are read
- **THEN** the counters SHALL be unchanged from before the delete

#### Scenario: Rebuild preserves live cell identity

- **WHEN** `Rebuild` runs while a VM site cache holds a resolution for a live binding and another for a deleted one
- **THEN** the live resolution SHALL keep serving the current value and the deleted one SHALL observe the binding unbound, with no stale value served

#### Scenario: Transient frame envs are counted but vanish

- **WHEN** a VM call allocates a frame env, binds locals, and returns
- **THEN** the bindings SHALL have charged that env's own counter, the per-env ceiling SHALL apply, and no persistent counter SHALL retain the charge after return

#### Scenario: Closure capture does not double-count

- **WHEN** a `Lambda` captures an env and the env's counters are later inspected
- **THEN** the captured env's counters SHALL be the same as before the capture

### Requirement: Call recursion is bounded across re-entrant apply

Both evaluators SHALL bound call-stack recursion by the shared per-evaluation
`MaxDepth` ceiling, including recursion that re-enters the evaluator through a
higher-order builtin (`map`, `filter`, `reduce`, `apply`). The call-depth
counter SHALL be shared across the re-entrant apply boundary, not reset per
pooled VM instance, so no script can drive unbounded native-stack recursion by
laundering self-recursion through a higher-order function. Exceeding the ceiling
SHALL return a `*core.LispicoError`, never a Go panic and never a fatal stack
overflow. The counter SHALL be tracked per evaluation, not on a shared engine
field, consistent with the concurrent-evaluation contract.

#### Scenario: Higher-order recursion is bounded on the bytecode VM

- **WHEN** a function recurses through `map` past `MaxDepth` on the bytecode VM
- **THEN** evaluation SHALL stop with a `*core.LispicoError` reporting the call-depth limit, and the process SHALL NOT abort with a fatal stack overflow

#### Scenario: Both evaluators bound the same recursion identically

- **WHEN** the same higher-order-recursion program runs on the tree-walker and on the bytecode VM under identical limits
- **THEN** both SHALL fail closed at the call-depth ceiling with the same error class

#### Scenario: Under-limit higher-order recursion still completes

- **WHEN** a bounded computation recurses through `map` to a depth below `MaxDepth`
- **THEN** it SHALL complete normally, with no false trip from counter state left over by a prior top-level evaluation

#### Scenario: Depth counter does not leak across goroutines

- **WHEN** two goroutines run bounded higher-order recursion concurrently on one engine
- **THEN** each SHALL be bounded by its own per-evaluation call-depth counter and `go test -race` SHALL report no data race

### Requirement: let/let*/loop accept List or Vector bindings

The binding forms `let`, `let*`, and `loop` SHALL accept their binding list as
either a `Vector` of flat alternating name/value elements (`[n0 v0 n1 v1 …]`) or
a `List` of two-element `(name value)` binding pairs (`((n0 v0) (n1 v1) …)`).
Both surface shapes SHALL produce identical bindings and identical evaluation
behavior, and the tree-walker and the bytecode compiler SHALL parse them
identically. This makes the forms usable under a bracket-less dialect (for
example the default Common Lisp dialect, which has no `Vector` reader syntax).
An empty binding list SHALL be valid in either shape. A malformed binding list —
a Vector of odd length, or a List element that is not a two-element pair headed
by a symbol — SHALL be a compile/eval error naming both accepted shapes.
Binding semantics (parallel `let`, sequential `let*`, `loop`/`recur` targets)
SHALL be unchanged.

#### Scenario: Common Lisp list-pair bindings under the default dialect

- **WHEN** `(let ((a 1) (b 2)) (+ a b))` is evaluated under the `cl` dialect
- **THEN** it SHALL bind `a` and `b` and return `3`, with no read or compile error

#### Scenario: Clojure vector bindings still work

- **WHEN** `(let [a 1 b 2] (+ a b))` is evaluated under the `clojure` dialect
- **THEN** it SHALL behave exactly as before

#### Scenario: Both evaluators agree on the list form

- **WHEN** the same `let`/`let*`/`loop` List-form source is run through the tree-walker and the bytecode compiler
- **THEN** both SHALL produce the same result (crossval parity)

#### Scenario: Malformed bindings are rejected clearly

- **WHEN** a `let` binding list is neither a valid flat Vector nor a list of two-element pairs
- **THEN** evaluation SHALL fail with an error naming both accepted binding shapes

### Requirement: loop/recur gives per-iteration binding identity for captured variables

When a closure is created inside a `loop` body and captures a loop variable, the
closure SHALL observe the value that variable held at the closure's own
iteration; a subsequent `recur` SHALL NOT change what an earlier iteration's
closure observes. A `recur` that rebinds a captured loop slot SHALL install a
fresh binding cell for that slot, so each iteration's closure holds a distinct
cell. Loop variables that are not captured by any closure MAY continue to use
in-place rebinding and SHALL NOT incur additional allocation. An explicit `set!`
of a captured loop variable within an iteration SHALL still be visible to
closures created in that same iteration (ordinary write-through), independent of
the fresh-cell-per-iteration behavior of `recur`. The tree-walker and the
bytecode compiler SHALL implement this identically.

#### Scenario: Closures in a loop capture per-iteration values

- **WHEN** `(loop [i 0 acc []] (if (< i 3) (recur (+ i 1) (conj acc (fn [] i))) acc))` is evaluated and each returned closure is called
- **THEN** the closures SHALL return `0`, `1`, `2` respectively — not `3`, `3`, `3`

#### Scenario: Both evaluators agree

- **WHEN** the closures-in-loop program is run through the tree-walker and the bytecode compiler
- **THEN** both SHALL produce `(0 1 2)` (crossval parity)

#### Scenario: Non-capturing loops allocate nothing extra

- **WHEN** a `loop` that never captures its loop variables (for example an accumulate-and-sum loop) runs under the allocation gate
- **THEN** its per-iteration allocation SHALL be unchanged from before this change

#### Scenario: set! within an iteration stays visible

- **WHEN** a loop body captures a variable in a closure and also `set!`s it later in the same iteration before creating the closure
- **THEN** the closure SHALL observe the `set!`-updated value for that iteration

### Requirement: Value construction is depth-bounded

Value construction that can increase nesting depth — the VM `OpMakeList`,
`OpMakeVector`, `OpMakeMap` opcodes and the stdlib `list`/`cons`/`vector`/
`conj`/`assoc`/`merge` builders and `json/decode` — SHALL reject a result whose
nesting depth exceeds `MaxStructuralDepth` (default 1024) with a terminal
`ResourceLimitError` (`CodeResourceLimit`). The depth check SHALL be bounded so
it cannot itself overflow the Go stack (descend at most `MaxStructuralDepth + 1`
levels). Value *breadth* (a wide flat collection) SHALL NOT be limited by this
requirement.

Enforcing the bound SHALL NOT cost time proportional to the collection being
extended. Extending an already-checked collection can only exceed the limit
through the element being added, so the check for `cons` and `conj` SHALL be
bounded by that element rather than by the accumulated result — otherwise a
loop accumulating collections is quadratic in time while allocating linearly.
This is a bound on the enforcement cost, not a relaxation of the bound itself:
the same constructions are rejected, at the same limit.

#### Scenario: Deeply nested construction fails with a terminal error

- **WHEN** a script builds a value whose nesting exceeds `MaxStructuralDepth` (for example via `loop`/`recur` wrapping `list` repeatedly, or `json/decode` of deeply nested input)
- **THEN** construction SHALL return a terminal `ResourceLimitError`, not crash the process, and the error SHALL NOT be catchable by in-script `try`/`catch`

#### Scenario: Escalating nesting through cons or conj still fails

- **WHEN** a loop repeatedly wraps its accumulator as the added element, so each step nests one level deeper, past `MaxStructuralDepth`
- **THEN** construction SHALL return a terminal `ResourceLimitError`, whether the wrapping is done with `cons` or with `conj`

#### Scenario: Accumulating collections stays linear

- **WHEN** a loop conses a small collection onto a growing accumulator at sizes spanning several doublings
- **THEN** the time SHALL grow in proportion to the number of elements added rather than to its square, matching the allocation growth for the same loop

#### Scenario: Wide flat collections are unaffected

- **WHEN** a script builds a shallow collection with many elements
- **THEN** it SHALL succeed, bounded only by the allocation ledger, not by the depth limit

### Requirement: Value-tree walks cannot crash on pathological depth

`String`, `Equals`, `ValueDeepBytes`, and `ValueNodeCount` SHALL be depth-bounded
so that a value exceeding `MaxStructuralDepth` degrades safely — a truncation
marker for `String`, a defined result for `Equals`, a capped count for the
byte/node walks — rather than recursing until the Go stack overflows. Ordinary
values within the depth limit SHALL be walked exactly as before.

#### Scenario: Stringifying an over-deep value does not crash

- **WHEN** `String()` (or an `Equals`/deep-bytes walk) is called on a value deeper than `MaxStructuralDepth`
- **THEN** it SHALL return a bounded result and SHALL NOT trigger a Go stack overflow

### Requirement: Bytecode compilation is depth-bounded

`Compiler.Compile` SHALL compare its compile depth against a limit (default 1024)
and return a terminal `ResourceLimitError` when exceeded, so a deeply nested form
— including one produced by macro expansion after the reader's own depth cap —
cannot overflow the Go stack during compilation. `literalDepth()` SHALL be
guarded the same way.

#### Scenario: Macro-expanded deep form fails to compile safely

- **WHEN** a macro expands to a form nested beyond the compile depth limit and that expansion is compiled
- **THEN** compilation SHALL return a terminal `ResourceLimitError`, not crash the process

### Requirement: Env merge is atomic against concurrent writes

`MergeInto` and `MergeIntoCanonical` SHALL be atomic with respect to concurrent
`Set`/`SetCanonical`/`Bind`-driven writes on the target env: a write that races
the merge SHALL NOT be silently overwritten by a stale precomputed merge value.
The implementation SHALL either hold the target lock for the whole merge or
re-validate each target cell's version before committing and skip/recompute on a
version change. The documented locking guarantee and the implementation SHALL
agree.

#### Scenario: Concurrent write during merge is not lost

- **WHEN** a `Set` on a name lands concurrently with a `MergeInto` touching the same name
- **THEN** the final value SHALL reflect one of the two writes under a defined order, never a stale merge value that discards the concurrent write, and the `-race` detector SHALL report no race

### Requirement: Retained aggregate stays consistent across overwrites

An env's retained byte and slot aggregate SHALL remain equal to the sum of its
live cells' retained backing across arbitrary merge/overwrite sequences. The
`MergeInto` overwrite branch SHALL adjust the aggregate by the difference between
the new and old cell backing, not only on first insertion. This aggregate gates
`MaxRetainedBytesPerEnv`.

#### Scenario: Repeated overwrite merges do not drift the aggregate

- **WHEN** the same names are merged into an env repeatedly with changing values (iterative hot-reload)
- **THEN** the env retained aggregate SHALL equal the true sum of live cell retained bytes, with no accumulated drift

### Requirement: Multi-meter retained settlement is all-or-nothing

`settleRetained` SHALL charge meters in a deterministic order and, if any charge
fails, SHALL unwind the charges already applied in that settlement before
returning the error, leaving no charge applied without its owning cell recorded.
A partial failure SHALL NOT leave charged-but-unowned backing that a later
`Rebuild()` cannot release.

#### Scenario: Partial multi-meter charge failure rolls back

- **WHEN** a settlement spanning more than one meter fails on a later meter's charge
- **THEN** the earlier successful charges SHALL be released before returning the error, and no cell SHALL be left charged without its `retainedMeter` recorded

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

### Requirement: Reader allocation cost scales with input, not with hidden constant factors

`core.Read` and its underlying tokenizer SHALL size their working storage from
the input rather than growing it through unbounded incremental append, so that
allocation count and bytes for tokenizing and parsing a source string grow
proportionally to that string's size rather than carrying an unsized-growth
constant-factor penalty on top. Internal token representation SHALL favor
compact field widths over convenience-sized machine words where the value
range does not require them, provided reader-error position reporting loses
no precision a caller can observe today for realistic source sizes. A
zero-copy substring fast path (as already used for numbers, symbols, and
keywords) MAY be extended to other token kinds where the token's text is
never mutated after tokenization; where doing so extends how long the source
string stays reachable through a parsed value, that extension SHALL be a
documented, deliberate choice, not an incidental side effect.

`ReaderStats` (node count, deterministic byte accounting) SHALL remain
unaffected by any internal allocation-efficiency change to the reader — the
ledger this feeds (`ChargeEvalReader`) is evaluator-independent per ADR 0011,
and an allocation-shape change to the reader's own internals SHALL NOT alter
what it reports.

#### Scenario: Small-literal parsing allocates close to its content size

- **WHEN** a short source string such as `(1 2 3)` is parsed
- **THEN** the allocation and allocation-count cost SHALL NOT be dominated by unsized slice growth, and SHALL be measurably lower than an implementation that starts token storage from a zero-capacity slice

#### Scenario: A zero-copy token extension is a stated decision

- **WHEN** a token kind's text is served as a substring of the original source rather than an independent copy
- **THEN** that choice, and any resulting extension of the source string's reachability through parsed values, SHALL be documented rather than left as an unstated side effect of an allocation-efficiency change

#### Scenario: Reader stats are unaffected by internal allocation changes

- **WHEN** the reader's internal token or buffer representation changes for allocation efficiency
- **THEN** `ReaderStats.Nodes` and `ReaderStats.Bytes` for a given source SHALL be identical to their values before the change

#### Scenario: Error position precision is preserved

- **WHEN** a reader error is produced for source within any realistic file's line and column range
- **THEN** the reported line and column SHALL be exact, not truncated or wrapped by a narrower internal representation

### Requirement: Reader working state is safely reusable across calls

The reader's per-call working storage (tokenizer state, parser state, and the
token buffer) MAY be drawn from a reusable pool rather than freshly allocated
on every `Read`, provided reuse is invisible to any observer: a value tree
returned from one `Read` SHALL be completely unaffected by any later `Read`
that reuses the same pooled storage, under both sequential and concurrent
use. Reuse SHALL NOT change any parsed value's content, structural sharing
with the source string, or observable allocation shape beyond reducing
per-call heap allocations. A collection literal's backing slice, once handed
to `List` or `Vector`, SHALL be independently owned — never an alias into
pooled scratch storage that a later `Read` call may overwrite.

#### Scenario: A prior Read's result survives a later Read reusing pooled storage

- **WHEN** one `Read` call's returned value tree is retained, and a subsequent `Read` call reuses the same pooled tokenizer/parser storage
- **THEN** the retained value tree SHALL be unchanged by the subsequent call's tokenization or parsing

#### Scenario: Concurrent Read calls sharing a pool show no data race

- **WHEN** multiple goroutines call `Read` concurrently against a shared reader-state pool
- **THEN** `go test -race` SHALL report no data race, and each call's result SHALL be correct for its own input

#### Scenario: A collection literal's elements are independently owned

- **WHEN** a list or vector literal is parsed via pooled scratch storage
- **THEN** its final backing slice SHALL be a right-sized, independently-owned copy, never an alias into storage a later `Read` call may reuse

### Requirement: Boxed value memory layout is size-class-aware

Concrete `Value` struct layouts MAY be tuned for Go allocator size-class
efficiency — field width and ordering chosen so a boxed value's memory
footprint lands in a smaller size class — provided the change is justified
by measurement on a workload that actually exercises the affected
representation at scale, not applied speculatively. Any such layout change
SHALL leave observable semantics, equality, printing, and public field
ranges (or their documented reduction, e.g. a narrowed maximum length)
unchanged for realistic inputs, and SHALL NOT alter any value's accounted
allocation-ledger size: the ledger's fixed size table (ADR 0011) is
independent of a Go struct's actual memory layout, and a layout tuning change
SHALL NOT be allowed to move it.

#### Scenario: A layout change is measurement-justified

- **WHEN** a `Value` type's struct layout is changed for size-class efficiency
- **THEN** the change SHALL be accompanied by a benchmark demonstrating a measurable improvement on a workload exercising that representation at a realistic scale, not merely a theoretical size-class crossing

#### Scenario: Accounted ledger size is unaffected by layout tuning

- **WHEN** a `Value` type's Go struct layout changes size for allocator efficiency
- **THEN** that type's accounted allocation-ledger size (as computed from ADR 0011's fixed size table) SHALL remain exactly what it was before the layout change

#### Scenario: A narrowed field range is documented, not silent

- **WHEN** a layout change narrows a field's representable range (for example, capping a count field's width)
- **THEN** the resulting limit SHALL be documented, and behavior at or beyond that limit SHALL fail closed rather than silently wrap or corrupt

### Requirement: Trusted hosts can define bootstrap source through the owning evaluator

The core SHALL expose `BootstrapDefiner` with
`DefineBootstrap(context.Context, string, *Env) (Value, error)`. Both the core
tree evaluator and runtime bytecode evaluator SHALL implement the capability;
the bytecode implementation SHALL delegate to its dialect-configured tree
evaluator rather than create an identity evaluator.

The operation SHALL parse with the trusted full bootstrap reader and accept
exactly one top-level proper list headed by `defn` or `defmacro`. Only dispatch of
that top-level definition MAY use the full kernel. Namespace mode, truthiness,
evaluation state, resource limits, and target environment SHALL remain those of
the owning evaluator. Read/evaluation failures SHALL remain typed.

Host-installed Go plugins are trusted and MAY discover this exported structural
interface. The capability SHALL NOT be registered as a Lisp value or Special
form and SHALL NOT be reachable from evaluated Lisp code; no isolation is claimed
among Go plugins.

#### Scenario: Both execution evaluators implement the capability

- **WHEN** a host inspects the core tree evaluator or runtime bytecode evaluator installed on an environment
- **THEN** each SHALL satisfy `core.BootstrapDefiner`, and the bytecode path SHALL retain its dialect-configured tree owner

#### Scenario: Trusted definition grammar fails closed

- **WHEN** bootstrap input contains multiple forms or a top-level form other than `defn` or `defmacro`
- **THEN** `DefineBootstrap` SHALL return a typed error without evaluating the input

#### Scenario: Lisp cannot invoke the host capability

- **WHEN** code executes under a full-base or empty-base Dialect
- **THEN** no Lisp name, Special form, reflected object, or first-class value SHALL expose `DefineBootstrap`
