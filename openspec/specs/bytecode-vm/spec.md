# bytecode-vm Specification

## Purpose

The bytecode VM is an experimental, opt-in evaluator (`runtime.WithBytecode()`)
that compiles forms to bytecode chunks and executes them on a stack machine, with
optional on-disk caching of compiled bytecode. It currently executes a subset of
the language; the tree-walking evaluator is the supported default.
## Requirements
### Requirement: Bytecode VM execution

The bytecode VM SHALL be the Engine's default evaluator — selectable away with
`runtime.WithTreeWalker()`, selectable explicitly with `runtime.WithBytecode()`
— and SHALL produce results identical to the tree-walking evaluator for every
program it compiles. It is a documented subset: for a form it does not compile it
SHALL return a typed error, and the runtime SHALL fall back to the tree-walking
evaluator for that form — never panicking, and never producing a result that
differs from the tree-walker. Evaluations SHALL be isolated in their results:
compiled chunks MAY be cached and reused, but no stack or frame state SHALL leak
between `Eval` calls. VM instances SHALL be reused across evaluations — on both
the `Eval` path and the `Apply`/`Call` path — rather than a fresh machine being
allocated per call; a reused instance SHALL be reset before it runs the next
evaluation so no state leaks between them. Applying a closure through
`Apply`/`ApplyPooled` SHALL enter the VM's call protocol directly, without
synthesizing a per-call wrapper chunk.

Every compiled expression SHALL leave exactly one result on the stack; a
non-executed `when` body SHALL produce `nil`. Definition and mutation
SHALL have distinct semantics: a definition writes to the current scope, while
`set!` updates the scope that already owns the binding and SHALL return a typed
error when no binding exists; locals resolved to slots keep slot mutation. A catch
binding SHALL exist only in the handler scope: compiling a `try` normal body SHALL
NOT reserve or shift the catch slot, and leaving the handler SHALL restore the
previous local layout.

#### Scenario: Default engine runs the VM

- **WHEN** an Engine is constructed without evaluator options and evaluates a form inside the compiled subset
- **THEN** the form SHALL execute on the bytecode VM, and `runtime.WithTreeWalker()` SHALL select the tree-walking evaluator instead

#### Scenario: Supported forms match the tree-walker

- **WHEN** the VM evaluates a form it compiles
- **THEN** the result SHALL equal the tree-walking evaluator's result for the same form and environment, including the runtime type of a value bound by `catch`

#### Scenario: let compiles sequentially

- **WHEN** the VM compiles `(let [x 1 y x] y)` with an outer `x` bound to `99`
- **THEN** each init SHALL see the locals bound before it in the same form, and the result SHALL be `1`, matching the tree-walker

#### Scenario: Unsupported form defers to the tree-walker

- **WHEN** a program uses a form the VM does not compile (a `defmacro` nested inside a larger form, or `unquote-splicing`)
- **THEN** compilation SHALL return a typed "unsupported in bytecode" error and the runtime SHALL evaluate that form with the tree-walker, never panicking

#### Scenario: A top-level defmacro compiles

- **WHEN** a `defmacro` is the whole form being compiled
- **THEN** it SHALL compile and bind through the same path the tree-walking evaluator uses, so both agree on the macro bound and on whether the chunk cache is invalidated

#### Scenario: A defmacro inside a larger form defers

- **WHEN** a `defmacro` appears nested inside another form, such as a `do` body that also uses the macro it defines
- **THEN** compilation SHALL return the typed "unsupported in bytecode" error and the runtime SHALL evaluate that form with the tree-walker, which binds and expands in evaluation order

#### Scenario: loop/recur iterates in constant stack

- **WHEN** a `loop`/`recur` runs 10,000 iterations
- **THEN** execution SHALL complete without growing the Go stack and SHALL return the same value as the tree-walker

#### Scenario: try/catch/throw handles errors

- **WHEN** a `try` body throws a value or a called `GoFunc` returns an error, and a `catch` clause is present
- **THEN** the caught value SHALL be bound to the catch symbol with the same runtime type as under the tree-walker, and the handler result SHALL match

#### Scenario: Variadic functions bind rest arguments

- **WHEN** a variadic `fn` is applied with more arguments than fixed parameters
- **THEN** the rest arguments SHALL be bound as a list, matching `Env.ChildVariadic`

#### Scenario: Each evaluation is isolated

- **WHEN** two forms are evaluated in sequence on the same engine
- **THEN** the second evaluation SHALL return its own result, with no instructions or stack state left over from the first, whether or not its chunk came from the cache

#### Scenario: Call reuses a pooled VM

- **WHEN** `Engine.Call` invokes a function repeatedly on one engine running the VM
- **THEN** each call SHALL run on a reset, reused VM from the pool rather than a freshly allocated machine, and SHALL return the same result the tree-walker would

#### Scenario: Skipped when/unless produces nil

- **WHEN** a false-test `when` appears in a value position — a `let` binding, a `do` body, or a function body
- **THEN** the expression SHALL yield `nil` with the stack balanced, matching the tree-walker

#### Scenario: set! mutates the lexical owner

- **WHEN** a closure invoked repeatedly applies `set!` to a binding owned by an enclosing scope
- **THEN** the owning scope's binding SHALL be updated, and the state SHALL persist across invocations exactly as under the tree-walker

#### Scenario: set! on an undefined binding errors

- **WHEN** `set!` targets a symbol with no existing binding in any enclosing scope
- **THEN** the VM SHALL return a typed error and SHALL NOT create a new binding

#### Scenario: Locals after try/catch keep correct slots

- **WHEN** a function binds locals after a `try`/`catch` form, on both the normal path and the error path
- **THEN** those locals SHALL hold their own values, with no slot-layout corruption from the catch binding

#### Scenario: Apply enters the call protocol directly

- **WHEN** `Engine.Call` applies a compiled closure on an engine running the VM
- **THEN** the VM SHALL execute the closure through its call protocol without compiling or allocating a per-call wrapper chunk, and the result SHALL match the tree-walker's

### Requirement: Bytecode VM concurrency safety

The bytecode evaluator SHALL support concurrent `Eval` calls on a single engine
without data races or cross-call state corruption. The same SHALL hold for the
`Apply`/`Call` path: distinct closures with separate environments running
concurrently on one shared engine SHALL return correct results with no data race.

#### Scenario: Concurrent evaluation

- **WHEN** multiple goroutines call `Eval` concurrently on one `WithBytecode()` engine
- **THEN** each SHALL return the correct result and `go test -race` SHALL report no data race

#### Scenario: Concurrent distinct closures through Call

- **WHEN** multiple goroutines invoke distinct closures with separate environments through `Call` on one shared `WithBytecode()` engine
- **THEN** each SHALL return its own correct result and `go test -race` SHALL report no data race

### Requirement: Bytecode VM robustness

The bytecode VM SHALL never panic on any input — valid source, a malformed form, or
a structurally malformed chunk; it SHALL return an error instead. Every error the
VM returns SHALL be a `*core.LispicoError`. For every special form the Compiler
handles, arity and shape SHALL be validated before any operand is indexed, so no
malformed special form can panic compilation. Structural validation of a chunk —
constant indices, symbol-constant types, jump and loop targets, and sub-chunk
references — SHALL happen once when the chunk is constructed or enters the chunk
cache; a chunk that fails validation SHALL be rejected there with a typed error
and SHALL never execute. Execution of a validated chunk SHALL NOT re-validate
these properties per instruction and SHALL still never panic.

#### Scenario: Empty-body function

- **WHEN** an empty-body function such as `((fn []))` or an empty-body `defn` is evaluated under `WithBytecode()`
- **THEN** the VM SHALL return an error, never panic

#### Scenario: Malformed chunk

- **WHEN** a chunk contains an opcode referencing an out-of-range stack slot, constant index, jump target, handler target, or a non-symbol where a symbol constant is required
- **THEN** it SHALL be rejected with a `*core.LispicoError` before any instruction runs, never indexing out of range and never panicking

#### Scenario: Max call depth is a typed error

- **WHEN** VM execution exceeds the maximum call depth
- **THEN** the returned error SHALL satisfy `errors.As(err, &lispicoErr)` like every other VM error

#### Scenario: Malformed special form is a typed error

- **WHEN** any compiled special form is given too few, too many, or wrongly shaped operands and evaluated through `Engine.Eval` under `WithBytecode()`
- **THEN** the result SHALL be a typed error from validation performed before operand indexing, never a panic

### Requirement: Bytecode VM tree-walker parity verification

A cross-validation corpus SHALL exercise both evaluators on the same programs and
assert identical results, and the runtime SHALL be tested end to end through
`WithBytecode()`.

#### Scenario: Cross-validation corpus passes

- **WHEN** the cross-validation corpus (all compiled special forms, closures, variadics, macros, `loop`/`recur`, `try`/`catch`/`throw` with a non-String throw, and empty-body functions) runs through both evaluators
- **THEN** every program SHALL produce equal results or the same class of error under both

#### Scenario: Runtime integration through WithBytecode

- **WHEN** the corpus is driven through `runtime.New(..., WithBytecode())`, including sequential and concurrent (`-race`) evaluation
- **THEN** all cases SHALL pass with no data race

### Requirement: Native arithmetic and comparison opcodes

The VM SHALL execute `+`, `-`, `*`, `/`, `<`, `>`, `<=`, `>=`, `=` through
dedicated opcodes operating on stack slots, with semantics identical to the stdlib
builtins including int/float promotion and division-by-zero errors. The VM MAY
specialize execution for a subset of argument shapes — for example two arguments
that are both integers — but a specialized path SHALL be indistinguishable from
the general path in every observable respect: the same result value, the same
error, and the same allocation charged to the evaluation ledger. In particular
integer overflow SHALL wrap identically on both paths, since the general path
relies on Go's wrapping arithmetic and a specialized path that instead reported
an error or saturated would change observable behavior. The compiler
SHALL emit these opcodes for a canonical native operator whether or not a dialect
is configured — a configured dialect (the shipped runtime path) SHALL NOT suppress
native-opcode emission for an operator that is not a special form and not locally
shadowed. The operator head SHALL be resolved through the cell the active dialect
uses for head position — the value cell for a Lisp-1 dialect, the function cell for
a Lisp-2 dialect (the default CL surface) — so that a rebind through that cell is
observed, and the resolution SHALL occur before argument evaluation, freezing both
the canonical decision and, when non-canonical, the operator value, so a rebind
during argument evaluation affects neither. Resolving a canonical operator head
SHALL NOT materialize the operator value onto the value stack — the canonical
path SHALL touch only the argument slots. When the operator symbol is locally
shadowed or its head-cell binding is no longer the canonical stdlib builtin,
execution SHALL fall back to the ordinary call path over the head-time-resolved
value. Canonical status SHALL be determined through the operator's resolved
binding, not re-derived by a per-execution environment walk, and a canonical
operator SHALL take the native path on every execution — not intermittently.

In addition, the compiler MAY fuse a native operator's canonicality-freeze
and its two operands into a single instruction when both operands are a
local slot or a compile-time constant (local×local or local×const; an
arbitrary sub-expression operand — "stack×stack" — is out of scope, since it
can execute code and rebind the operator mid-evaluation, which the freeze
protocol exists to prevent):

- a native comparison whose boolean result feeds a conditional branch, and
  a native arithmetic op, SHALL both be emittable this way — one fused
  instruction that resolves the operator, reads both operands, and pushes
  the single result the unfused sequence would have produced. The
  conditional branch (or whatever consumes an arithmetic result) SHALL
  remain a separate, ordinary instruction, emitted exactly as it is today:
  a rebind to a VM-compiled closure pushes a new call frame and returns
  asynchronously, so no single instruction can both compute a result and
  branch on one that may not exist until a later frame returns.

A fused instruction SHALL preserve, bit-for-bit, the semantics of the
sequence it replaces: the same canonicality freeze point, the same
non-canonical fallback (including a fallback callee that is itself a
VM-compiled closure), the same numeric edge behavior (division by zero,
float promotion), and the same allocation-ledger charge as the unfused fused
native op. Shapes not covered by a fusion SHALL compile exactly as before.
Chunk validation SHALL verify every fused instruction's operand indices
before the chunk runs, preserving the validated-chunk invariant that the
dispatch loop performs no per-instruction bounds checks.

#### Scenario: Hot loop avoids builtin dispatch

- **WHEN** a `loop` body evaluates `(+ acc 1)` under the VM
- **THEN** the addition SHALL execute as an opcode without a `GoFunc` invocation, and the loop result SHALL equal the tree-walker's

#### Scenario: Native opcodes emitted under a configured dialect

- **WHEN** `(+ a b)` or `(< a b)` is compiled with a configured dialect (the default CL dialect or clojure) and run on a `WithBytecode()` engine
- **THEN** the operator SHALL compile to and execute as its native opcode with no `GoFunc` dispatch, matching the tree-walker's result

#### Scenario: Canonical operator adds no stack traffic

- **WHEN** a compiled body applies a canonical native operator under the VM
- **THEN** execution SHALL push and pop only the operator's arguments and result — no operator value SHALL transit the value stack

#### Scenario: Promotion parity

- **WHEN** `(+ 1 2.5)` and `(< 1 1.5)` run under the VM
- **THEN** results SHALL equal the stdlib builtins' results (`3.5`, `true`)

#### Scenario: Specialized and general paths agree

- **WHEN** the same operator is applied to two integers, including values at the `int64` limits where the result wraps, and to argument shapes the specialization does not cover
- **THEN** results, errors, and charged allocation SHALL be identical to what the general path produces for the same arguments

#### Scenario: Rebound operator falls back

- **WHEN** a program rebinds `+` to a custom function and then calls `(+ 1 2)` under the VM
- **THEN** the custom function SHALL be called, matching tree-walker behavior

#### Scenario: Mid-argument rebind does not flip the decision

- **WHEN** an argument expression of a canonical `(+ ...)` application rebinds `+` while the arguments are being evaluated
- **THEN** that application SHALL complete under the decision and value resolved before argument evaluation, matching the tree-walker's head-then-arguments order

#### Scenario: Lisp-2 function-cell rebind falls back

- **WHEN** under the CL dialect a program rebinds `+` in the function cell (`(defun + (a b) (- a b))`) and then calls `(+ 5 3)` under the VM
- **THEN** the rebound function SHALL be called (result `2`), matching the tree-walker — the native opcode SHALL NOT execute over the stale canonical value-cell binding

#### Scenario: Recursive calls keep the native path

- **WHEN** a recursive function's body applies canonical `+`, `-`, and `<` across nested self-calls under the VM
- **THEN** every application SHALL execute as a native opcode, with no fallback to `GoFunc` dispatch for canonical bindings

#### Scenario: Fused compare matches unfused semantics

- **WHEN** `(if (< n 2) a b)` executes with `<` canonical, and separately after `<` has been rebound to a builtin, a tree-walker closure, and a VM-compiled closure
- **THEN** the result SHALL be identical to the tree-walker in every case — the fused instruction takes the native path only under the same conditions the unfused sequence would, and the trailing conditional branch consumes whichever result the fused instruction or its fallback call eventually produced

#### Scenario: Fused arithmetic matches unfused semantics

- **WHEN** `(- n 1)` compiles to a fused local/const instruction and executes, including division-by-zero and float-operand edge cases for the other arithmetic ops
- **THEN** results and errors SHALL be identical to the unfused native-op sequence, and the allocation ledger SHALL be charged identically

#### Scenario: Validation covers fused operands

- **WHEN** a chunk containing fused instructions with out-of-range slot, constant, or operator-site operands is loaded
- **THEN** `Validate` SHALL reject it before execution

#### Scenario: Uncovered shapes compile as before

- **WHEN** an operand of a native comparison or arithmetic op is neither a local slot nor a compile-time constant (a nested call or other sub-expression)
- **THEN** the compiler SHALL emit the existing unfused sequence unchanged

### Requirement: Slot-resident locals

The compiler SHALL determine which locals are captured by inner closures; locals
that are not captured SHALL live only in stack slots, with no per-call `Env`
allocation or write-mirroring for them, and their access cost SHALL be
unaffected by this requirement's capture provisions. A captured local SHALL be
cell-resident: one shared storage cell allocated at its binding site, written
through on every mutation, with **no** per-mutation environment mirroring. A
closure SHALL capture direct references to exactly the variables it uses —
not its defining environment chain — and captured-variable semantics SHALL be
unchanged from the tree-walker: every closure over a variable and its defining
scope observe one shared binding, before and after the defining frame returns.

#### Scenario: Uncaptured locals allocate no environment

- **WHEN** a function whose locals are never captured is called in a hot loop under the VM
- **THEN** the call SHALL not allocate an `Env` map for those locals

#### Scenario: Captured variable still works

- **WHEN** a closure captures a local and is called after the defining frame returns
- **THEN** the captured value SHALL be correct, matching the tree-walker

#### Scenario: Mutating a captured local mirrors nothing

- **WHEN** a loop body repeatedly mutates a local that an inner closure captures
- **THEN** each mutation SHALL write the shared cell only, with no environment write and no allocation per mutation

#### Scenario: Sibling closures alias one binding

- **WHEN** two closures capture the same local and one applies `set!` to it
- **THEN** the other closure and the defining scope SHALL observe the new value, matching the tree-walker

#### Scenario: Defining-scope mutation is visible to the closure

- **WHEN** the defining scope mutates a captured local after the closure was created
- **THEN** a subsequent call of the closure SHALL observe the new value, matching the tree-walker

#### Scenario: Loop-iteration capture matches the tree-walker

- **WHEN** a `loop`/`recur` body creates a closure over a loop binding on each iteration
- **THEN** each closure SHALL observe exactly the binding instance the tree-walker gives it (fresh or shared per iteration), verified by cross-validation

#### Scenario: Transitive capture through nested closures

- **WHEN** a closure nested two levels deep references a local of the outermost function
- **THEN** the reference SHALL read and write the same shared binding at every level, matching the tree-walker

### Requirement: Compiled-chunk cache

The runtime SHALL cache compiled chunks per Engine, keyed by source, dialect, and
macro-definition epoch. A cache hit SHALL skip macro expansion and compilation.
Macro expansion SHALL therefore be performed at most once per cached chunk, not
once per evaluation: an expander body is ordinary evaluated code, so re-running
it to reach a chunk that already embeds its result would repeat any effect it
has. Defining or redefining a macro SHALL invalidate affected entries, so a
stale chunk never runs an outdated expansion. The cache SHALL be bounded: its
entry count SHALL NOT grow without limit over the Engine's lifetime. Entries
orphaned by a macro-epoch bump SHALL be reclaimed, and the cache SHALL enforce
the Engine's configured chunk-cache-size ceiling, so a long-lived Engine that
evaluates many distinct sources or repeatedly redefines macros stays within its
memory budget.

Binding a macro name to a definition indistinguishable from the one already bound
there SHALL NOT count as a redefinition for invalidation: no cached expansion can
differ, so no entry is affected. Indistinguishable means the same name, the same
defining environment, the same parameters and variadic tail, and an equal body.
The comparison SHALL fail closed — where equality cannot be decided, including a
body deeper than the structural-depth bound, the binding SHALL be treated as a
redefinition and invalidate as before. Correctness outranks reuse here: serving a
stale expansion is a defect, recompiling something that need not be recompiled is
only a cost.

Additionally, plugin-load compilation SHALL be reusable across Engines within a
process: loading identical plugin source under an identical dialect fingerprint
into a second Engine SHALL NOT repeat macro expansion and compilation, provided
the source's expansion is fully determined by the dialect and the source itself.
This process-level tier SHALL be bounded, SHALL share only immutable compiled
artifacts, and SHALL NOT share per-engine resolution state: each Engine resolves
globals against its own environments, and no binding, macro, or canonical flag
SHALL leak between Engines through the shared artifacts.

#### Scenario: Repeated evaluation reuses the chunk

- **WHEN** the same source is evaluated twice on one Engine under the VM
- **THEN** the second evaluation SHALL not recompile and SHALL return the same result

#### Scenario: A cache hit does not re-run the expander

- **WHEN** a source using a macro whose expander body has an observable effect is evaluated repeatedly on one Engine under the VM, with the macro unchanged
- **THEN** that effect SHALL be observed once for the compilation, not once per evaluation

#### Scenario: Re-evaluating a source that defines a macro reuses its chunks

- **WHEN** a source containing a `defmacro` is evaluated repeatedly on one Engine under the VM, with the macro's definition unchanged between evaluations
- **THEN** the cache epoch SHALL be the same after every evaluation as after the first, and no form in that source SHALL be recompiled

#### Scenario: Macro redefinition invalidates

- **WHEN** source using macro `m` is evaluated, `m` is redefined, and the same source is evaluated again
- **THEN** the second evaluation SHALL reflect the new definition of `m`

#### Scenario: An identical body in a different scope still invalidates

- **WHEN** a macro name is rebound to a body equal to its current one but closing over a different defining environment
- **THEN** the binding SHALL invalidate as a redefinition, since the expansion it produces may differ

#### Scenario: Cache does not grow without bound

- **WHEN** an Engine repeatedly evaluates distinct sources and redefines macros far beyond the chunk-cache-size ceiling
- **THEN** the cache entry count SHALL stay at or below the configured ceiling, and results SHALL remain correct for whatever is evaluated next

#### Scenario: Second engine skips plugin recompilation

- **WHEN** a second Engine with the same dialect loads the same stdlib plugin source in one process
- **THEN** the load SHALL reuse the process-level compiled artifacts without repeating expansion or compilation, and every stdlib definition SHALL behave identically to a freshly compiled load

#### Scenario: Shared artifacts leak no engine state

- **WHEN** two Engines built from shared plugin artifacts each define new bindings and one unloads the plugin
- **THEN** neither Engine SHALL observe the other's bindings or unload, and `go test -race` SHALL report no data race across concurrent engine construction

### Requirement: Dialect-axis execution

The VM SHALL honor the Engine's dialect: form names normalized to canonical kernel
forms before compilation, truthiness decided through the dialect's truthiness rule,
head-position symbol resolution through the function cell under Lisp-2, and special
forms with a dialect-owned Form-shape rule (`cond` clause shape first) compiled from
the same canonical structure the Evaluator dispatches on. Any resolvable dialect
SHALL be VM-eligible.

#### Scenario: CL dialect runs on the VM

- **WHEN** an Engine is created with the default CL dialect and `WithBytecode()`, and evaluates `(progn (setq x 1) (if nil 2 x))`
- **THEN** construction SHALL succeed and the result SHALL be `1`, matching the tree-walker

#### Scenario: Truthiness axis honored

- **WHEN** a nil-only-falsy dialect evaluates `(if false 1 2)` under the VM
- **THEN** the result SHALL be `1`, because `false` is truthy on that axis

#### Scenario: Restricted dialect runs on the VM

- **WHEN** a fail-closed dialect built from the empty base with a form subset runs a program using only its forms under the VM
- **THEN** the program SHALL evaluate correctly, and forms outside the subset SHALL remain undefined

#### Scenario: Both cond clause shapes compile

- **WHEN** a Clojure-dialect Engine compiles a flat-pair `cond` and a CL-dialect Engine compiles a nested-clause `cond` under `WithBytecode()`
- **THEN** both SHALL compile from the dialect's canonical clauses and return results equal to the tree-walker's

### Requirement: Keyword application parity

VM application SHALL support Keyword values as callables with semantics identical
to the Evaluator: `(:key m)` looks up `:key` in map `m`, a missing key yields
`nil`, wrong arity (anything other than exactly one argument) is a typed error,
and a non-map argument behaves exactly as under the tree-walker. This SHALL hold
on both the `Eval` and the `Apply`/`Call` paths.

#### Scenario: Keyword lookup hits and misses

- **WHEN** `(:key m)` is evaluated under `WithBytecode()` with the key present and absent
- **THEN** the results SHALL equal the tree-walker's (value, `nil`)

#### Scenario: Keyword misuse matches the Evaluator

- **WHEN** a Keyword is applied with wrong arity or to a non-map value under the VM
- **THEN** the outcome (typed error or value) SHALL equal the tree-walker's for the same input

### Requirement: Structural-depth state hygiene

VM structural-depth accounting SHALL be restored on every exit path — normal
return, thrown error, ceiling breach, and malformed input — including when the VM
instance is reused from the pool. One failed evaluation SHALL NOT reduce the
structural-depth budget available to any later evaluation on the same Engine.

Depth counters SHALL use atomic access whenever the counter is shared with an
evaluation state or a re-entrant context; a counter private to one VM MAY be a
plain field. The choice SHALL be made from the counter's identity at arm time,
not re-derived per operation, and SHALL NOT change what a limit breach reports
or when it trips.

#### Scenario: Failed evaluation does not poison the next

- **WHEN** a VM evaluation fails for any reason and a subsequent well-formed evaluation runs on the same `WithBytecode()` Engine
- **THEN** the subsequent evaluation SHALL see the full configured structural-depth budget and succeed

#### Scenario: Pooled reuse restores depth state

- **WHEN** a pooled VM instance that previously exited through an error path is reused for a new evaluation
- **THEN** its structural-depth accounting SHALL start fresh, with no carry-over from the failed run

#### Scenario: Shared depth counters still enforce limits

- **WHEN** a host `GoFunc` re-enters the evaluator so the call-depth counter is shared across the boundary
- **THEN** combined nesting SHALL still trip the configured depth limit, and `go test -race` SHALL report no data race

### Requirement: Kernel let binding scope parity

The VM SHALL bind kernel `let` in parallel: every binding init expression SHALL
resolve names in the scope enclosing the `let`, never in bindings introduced by
the same vector — matching the tree-walking evaluator. Kernel `let*` SHALL
remain sequential: each init resolves bindings introduced earlier in the same
vector. A binding name that shadows an enclosing binding SHALL not be visible
to any sibling init in the same `let` vector.

#### Scenario: let init sees the enclosing binding, not its sibling

- **WHEN** the VM evaluates `(def a 10) (let [a 1 b a] b)`
- **THEN** the result SHALL be `10`, equal to the tree-walking evaluator's result

#### Scenario: let* init sees the earlier sibling

- **WHEN** the VM evaluates `(def a 10) (let* [a 1 b a] b)`
- **THEN** the result SHALL be `1`, equal to the tree-walking evaluator's result

### Requirement: Resolved global bindings

Repeated execution of a compiled chunk SHALL NOT re-resolve a global name
through a locked map walk on every read — in the value namespace and, on a
Lisp-2 dialect, in the function namespace alike. A call site's resolution MAY
be cached on the chunk, guarded so that a chunk running against a different
environment, or after a new name is bound into the resolution environment,
resolves afresh. The two namespaces SHALL resolve through distinct cached
entries even for the same symbol in the same chunk. Reading a global through
a cached site whose binding has not been written since resolution SHALL take
no lock and SHALL allocate nothing. Rebinding an already-bound global —
including a Lisp-2 function rebind that clears or restores a canonical
operator marking — SHALL be visible to subsequent reads through any cached
resolution, deleting it SHALL make subsequent reads report it undefined, and
concurrent execution with concurrent binds SHALL stay race-free per the
concurrency-safety requirement. Neither the read path nor the write path of
a binding SHALL allocate per operation on account of the cache.

#### Scenario: Rebind visible through a cached resolution

- **WHEN** a chunk reads global `f`, then the program rebinds `f`, then the same cached chunk executes again
- **THEN** the second execution SHALL observe the new binding, matching the tree-walker

#### Scenario: Delete visible through a cached resolution

- **WHEN** a chunk reads global `f` through a warmed cached site, then `f` is deleted, then the same chunk executes again
- **THEN** the second execution SHALL report `f` undefined, matching the tree-walker

#### Scenario: Shared chunk across environments

- **WHEN** one cached chunk executes against two engines with different root environments
- **THEN** each execution SHALL resolve globals in its own environment, with no cross-engine value leakage

#### Scenario: Concurrent bind and execute

- **WHEN** one goroutine rebinds a global while others execute chunks reading it on the same engine
- **THEN** each execution SHALL observe either the old or the new binding and `go test -race` SHALL report no data race

#### Scenario: Stable global reads are lock- and allocation-free

- **WHEN** a cached chunk repeatedly reads a global that is never rebound after the chunk warmed up
- **THEN** those reads SHALL acquire no lock and allocate nothing

#### Scenario: Function-namespace head resolution is cached

- **WHEN** a Lisp-2 chunk repeatedly calls a function whose binding is never rewritten after warm-up
- **THEN** head resolutions SHALL acquire no lock and allocate nothing, and results SHALL match the tree-walker

#### Scenario: Defun rebind of a canonical operator through a warmed site

- **WHEN** a warmed Lisp-2 chunk calls a canonical operator head, then the program rebinds it with `defun`, then the chunk executes again
- **THEN** the second execution SHALL call the user definition (not native operator semantics), and restoring the canonical binding SHALL restore native semantics, both matching the tree-walker

#### Scenario: Function cell dropped by compaction and rebound

- **WHEN** a warmed function head is deleted, the environment is compacted (`Rebuild`), and the name is then bound to a new function cell
- **THEN** the next execution through the previously warmed site SHALL resolve the new binding, matching the tree-walker

### Requirement: Batched cancellation observation

The VM SHALL observe context cancellation and the engine evaluation deadline at
bounded intervals rather than before every instruction: at most a fixed
instruction budget apart, counting every executed instruction — calls, tail
calls, and loop back-jumps included. A host `GoFunc` extends the wall-clock
observation window by its own execution time, since the VM never preempts host
code. An already-cancelled context SHALL be rejected at the evaluation boundary
before any instruction executes. Cancellation and deadline errors SHALL keep
their current error shape.

Deadline enforcement SHALL NOT require a wall-clock read at every checkpoint.
The VM MAY observe deadline expiry through an externally-armed expiry signal
(set once when the deadline passes) or through clock reads at a reduced,
fixed multiple of the checkpoint interval. In either mechanism the interval
between a deadline passing and the evaluation terminating SHALL be bounded
and documented: no more than a small fixed number of checkpoint intervals of
instruction execution (plus any single host `GoFunc`'s own execution time,
as today). Context cancellation SHALL still be checked at every checkpoint.
Arming the deadline signal SHALL remain lazy: an evaluation that completes
before its first checkpoint SHALL perform no clock read and no timer work.

#### Scenario: Loop observes cancellation within the budget

- **WHEN** the caller's context is cancelled while a `loop`/`recur` body is iterating under the VM
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget

#### Scenario: Recursion observes cancellation within the budget

- **WHEN** the caller's context is cancelled while a recursive function is descending under the VM
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget

#### Scenario: Straight-line code observes cancellation within the budget

- **WHEN** the caller's context is cancelled during a long straight-line instruction sequence
- **THEN** evaluation SHALL stop with a context error within the fixed instruction budget

#### Scenario: Cancelled context rejected at the boundary

- **WHEN** `Eval` or `Call` is invoked with a context that is already cancelled
- **THEN** the evaluation SHALL return a context error without executing any instruction

#### Scenario: Deadline crossing terminates within the documented bound

- **WHEN** the engine deadline passes while a compiled body is executing instructions
- **THEN** evaluation SHALL fail with the same deadline error shape as today, within the documented bound of checkpoint intervals

#### Scenario: Short evaluations stay clock-free

- **WHEN** an evaluation completes before its first checkpoint on an engine with a configured timeout
- **THEN** the evaluation SHALL perform no wall-clock read and SHALL create no timer

### Requirement: Lazy re-entrant evaluation state

Dispatching a host `GoFunc` from the VM SHALL NOT materialize an
evaluation-state value unless the host function actually requests it (by
re-entering the evaluator or reading the state from its context). When
requested, the state SHALL be materialized at most once per VM run and SHALL
carry the enclosing evaluation's structural-depth and deadline budget, so
re-entrant calls enforce the same resource limits as today. The context
wrapper handed to host functions SHALL be reusable VM-owned storage: a
subsequent run on the same VM with the same outer context SHALL reuse the
wrapper after re-arming its per-evaluation fields, rather than allocating a
new one, and a run with a different outer context SHALL build a fresh
wrapper. The wrapper's deadline SHALL be computed lazily at first
observation, not at dispatch, so host functions that never observe a
deadline trigger no wall-clock read. The context a `GoFunc` receives SHALL
delegate cancellation, deadline, and unrelated values to the caller's context
unchanged. State handed to a host function SHALL be generation-guarded:
retaining the context past the call SHALL NOT expose a later run's budget,
deadline, or internals — a stale-generation access SHALL behave as a context
carrying no evaluation state, delegating to the outer context and adopting a
fresh budget on re-entry.

Re-arming SHALL be proportional to what changed: when a rearm carries the
same configuration the wrapper was last armed with — same limits, timeout,
and meter posture — the wrapper MAY refresh only its generation stamp and
any per-run seeds that differ, rather than rewriting every field. Any
configuration difference SHALL take the full rearm. The observable contract
is unchanged in either case: the host function and any re-entry see exactly
the values a full rearm would have installed.

#### Scenario: Non-re-entrant host pays no state allocation

- **WHEN** a compiled body repeatedly dispatches a `GoFunc` that never re-enters the evaluator
- **THEN** no evaluation-state value SHALL be allocated for those dispatches

#### Scenario: Wrapper reused across runs with one outer context

- **WHEN** the same VM executes many top-level calls under one outer context, each dispatching a `GoFunc`
- **THEN** the context wrapper SHALL be allocated at most once and re-armed per run, and per-call wrapper allocations SHALL be zero at steady state

#### Scenario: Re-entry shares the enclosing budget

- **WHEN** a dispatched `GoFunc` re-enters the evaluator with the context it received
- **THEN** the re-entrant evaluation SHALL count structural depth against the enclosing run's budget and honor the enclosing deadline, identical to eager state adoption

#### Scenario: Caller context semantics pass through

- **WHEN** the caller's context is cancelled while a dispatched `GoFunc` is waiting on it
- **THEN** the `GoFunc` SHALL observe cancellation through the context it received exactly as it would through the caller's context

#### Scenario: Retained context is generation-guarded

- **WHEN** a `GoFunc` stores the context it received and reads state or re-enters the evaluator after its call returned and the VM has moved to a later run
- **THEN** the stale context SHALL NOT expose the later run's budget, deadline, or internals; its accesses SHALL behave as a context carrying no evaluation state

#### Scenario: Changed configuration is fully re-armed

- **WHEN** the engine's limits, timeout, or meter posture change between two calls that reuse one wrapper
- **THEN** the next dispatch SHALL observe the new configuration exactly as a freshly built wrapper would

#### Scenario: Same-configuration rearm is observably identical

- **WHEN** two adjacent calls with identical configuration each dispatch a re-entering `GoFunc`
- **THEN** the second call's re-entry SHALL observe a fresh budget and correct seeds exactly as under a full rearm

### Requirement: Fused native-op results charge the allocation ledger

The VM fused native arithmetic and comparison ops SHALL charge the evaluation
allocation ledger for their result, consistent with the charge the GoFunc
dispatch path already applies, so a heap-boxed `Float` or out-of-preboxed-range
`Int` produced by a fused `+`/`-`/`*`/`/`/comparison op is not invisible to
`MaxAllocationBytes`. The charge SHALL be a fixed scalar size computed at the
fused dispatch site (no Go allocation added). Preboxed small-int and boolean
results MAY be charged their same fixed scalar size; the intent is consistency
with the non-fused path, not a new exemption.

Charges issued by VM opcodes MAY accumulate in VM-local storage between
settlement points instead of writing to the shared evaluation-state ledger per
instruction. Settlement SHALL occur at every batched cancellation checkpoint,
at every run exit (normal return and error unwind), and before any `GoFunc`
dispatch or re-entrant evaluation adoption, so any externally observable read
of the ledger — a host function, a nested evaluation, a meter lease, or the
evaluation's own completion — sees totals identical to per-instruction
charging. Limit enforcement MAY lag by at most one unsettled batch: one
checkpoint interval's worth of whatever charges were issued in it, which is
NOT bounded to fixed-size scalar charges — a checkpoint interval that
constructs collections also accumulates list, vector, map, and closure
shallow-byte charges and charged-constant charges in the same unsettled batch,
and the slack bound SHALL be read as covering all of them, not scalars alone.
The terminal `ResourceLimitError` and its error shape SHALL be unchanged.

A compiled chunk's published `DeepBytes` SHALL include a fixed per-site charge
for each fused-operator descriptor it carries, sized independently of the
instruction count the fusion removed. Fusing operator resolution into fewer
executed instructions SHALL NOT be assumed to shrink a chunk's accounted
`DeepBytes` in proportion — the fused-descriptor charge MAY exceed the bytes
freed by the removed instructions, so a compiler change that reduces
instruction count MAY still increase the chunk's accounted size and its
likelihood of exceeding `MaxCacheBytes` on admission.

#### Scenario: Fused arithmetic result is charged

- **WHEN** a fused native op produces a heap-boxed numeric result under the ledger
- **THEN** the allocation ledger SHALL be charged for that result, matching the GoFunc-dispatch charge for the same operation

#### Scenario: Goldset allocation posture is preserved

- **WHEN** the goldset benchmark cells run in VM mode after this change
- **THEN** their allocations per operation SHALL be non-increasing (the charge is a size computation, not an allocation)

#### Scenario: Host observation sees exact totals

- **WHEN** a compiled body dispatches a `GoFunc` (or re-enters the evaluator) after executing charged opcodes
- **THEN** the ledger the host or nested evaluation observes SHALL include every charge issued before the dispatch, identical to per-instruction charging

#### Scenario: Limit crossing fails within one batch window

- **WHEN** a program's charged bytes cross `MaxAllocationBytes` (or exhaust a meter lease) between two checkpoints, whether from scalar arithmetic, collection construction, or a mix of both within the same interval
- **THEN** evaluation SHALL fail with the same terminal `ResourceLimitError` no later than the next settlement point, and the meter's draw/return accounting SHALL balance exactly as with per-instruction charging

#### Scenario: Fusion may increase accounted chunk size despite fewer instructions

- **WHEN** a chunk compiles with fused native-op sites that reduce its total instruction count relative to an equivalent unfused compilation
- **THEN** its published `DeepBytes` MAY be larger than the unfused compilation's, and that increase SHALL be attributable to the fixed per-site fused-descriptor charge rather than to a measurement defect

### Requirement: All-constant collection literals compile to shared constants

A collection literal — `Vector`, `HashMap`, or list literal — whose elements
(recursively, including map keys and values) are all compile-time constants
SHALL compile to a single reference into the chunk constant pool, built once
at compile time, rather than to per-execution element pushes and construction
opcodes. Nested all-constant literals SHALL fold into their parent. A literal
containing any non-constant element SHALL compile exactly as before. Repeated
execution SHALL return the shared constant value; this is unobservable
in-language because the value is immutable and comparison is by `Equals`.

Resource enforcement SHALL be preserved by precomputation, not skipped: the
folded constant's construction charge and structural depth SHALL be computed
once at compile time, and each execution SHALL charge the per-evaluation
allocation ledger by the precomputed bytes and check the precomputed depth
against the running engine's `MaxStructuralDepth` in O(1), raising the same
terminal `ResourceLimitError` as the construction path. The allocation ledger SHALL
therefore observe the same charges under the bytecode VM as under the
tree-walking evaluator for the same program. No engine-specific limit SHALL
be baked into the compiled chunk. The folded constant SHALL be covered by the
chunk's compile-time retained-bytes accounting, and chunk validation SHALL
verify the new instruction's operands before the chunk runs.

#### Scenario: Rule-shaped literal stops allocating per call

- **WHEN** a compiled function returning `{:model :large :tools [:read :grep]}` is called repeatedly on a bytecode engine
- **THEN** per-call allocations SHALL NOT include construction of the map or its nested vector, and the returned value SHALL equal the tree-walker's result

#### Scenario: Mixed literals still construct

- **WHEN** a function returning `{:model m}` (a non-constant element) is called
- **THEN** the literal SHALL be constructed per execution exactly as before this change

#### Scenario: Allocation ledger is evaluator-independent

- **WHEN** a program whose folded literals exceed a small configured `MaxAllocationBytes` runs under the bytecode VM and under the tree-walker
- **THEN** both SHALL fail with the same terminal `ResourceLimitError`

#### Scenario: Depth limit still enforced on folded constants

- **WHEN** a folded literal's structural depth exceeds the engine's configured `MaxStructuralDepth`
- **THEN** execution SHALL fail with the same terminal `ResourceLimitError` the construction path raises, under both evaluators

