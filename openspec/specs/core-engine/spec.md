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
per-evaluator and normative: the tree-walker SHALL charge one reduction per
apply-trampoline iteration and per form dispatch; the VM SHALL charge one
reduction per instruction decode; macro expansion SHALL charge one per
expansion step; the compiler SHALL charge one per emitted instruction, and
compilation allocation SHALL charge the evaluation that triggered
compilation before any chunk is cached; GoFunc dispatch SHALL charge one
reduction plus the result's shallow allocation size at the centralized apply
sites. Counter values are NOT required to be equal across evaluators.
Reduction accounting SHALL piggyback the existing batched-cancellation
countdown — no additional per-step cost and no additional clock reads.
Exceeding either ceiling SHALL raise `Code: "ResourceLimitError"`, terminal
per the non-catchability requirement, on both evaluators.

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

