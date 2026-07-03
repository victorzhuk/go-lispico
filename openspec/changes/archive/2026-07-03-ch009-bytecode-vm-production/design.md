# Design: Bytecode VM production readiness

## Context

The VM (`core/vm`) and compiler (`core/compiler`) execute a subset of the
language. The runtime wires them in `runtime/engine.go`: when `WithBytecode()`
is set it constructs one `vm.New(rootEnv, bc, WithCompiler(compiler.NewCompiler("<runtime>")))`
and stores it as `engineImpl.evaluator`. `vmEvaluator.Eval` calls
`compiler.Compile(form)`, appends `OpReturn`, and runs `compiler.Chunk()`.

Two structural problems follow from that wiring. The compiler is created once
and its chunk accumulates across calls, so `VM.Run` (which always starts at
`ip 0`) re-executes earlier instructions on every subsequent `Eval`. And the
single VM holds a shared `stack`, `frames`, and `depth`, so concurrent `Eval`
calls corrupt each other. On top of that, the compiler is missing nine special
forms, miscompiles `loop`/`recur`, treats `try` as `do`, never expands macros,
and never packs variadic rest parameters.

The tree-walking evaluator (`core/eval.go`) is the reference semantics. "Parity"
below means: for a program that the tree-walker accepts, the VM produces an
equal `core.Value` (by `Equals`) or the same class of error.

## Goals / Non-Goals

**Goals:**

- Correct, single-shot evaluation per `Eval` call with no cross-call leakage.
- Full parity with the tree-walker for all 22 special forms, including
  `loop`/`recur`, `try`/`catch`/`throw`, `cond`, `and`/`or`/`not`, `defn`,
  `defmacro`, `quasiquote`, and variadic functions.
- Macro expansion at compile time.
- Safe concurrent `Eval` on a single engine.
- No panics on malformed or corrupt bytecode.
- A shared cross-validation corpus that both evaluators must pass, plus runtime
  integration tests through `WithBytecode()`.

**Non-Goals:**

- Changing the public runtime API surface.
- Beating the tree-walker on every workload; the target is the ≥10x
  arithmetic-loop win with a loop body that actually runs, not universal speedup.
- A new bytecode file format beyond bumping `cacheVersion` when encoding changes.
- Implicit tail-call optimization of ordinary function self-recursion — the
  tree-walker does not do this (only `loop`/`recur` is O(1) stack), and the VM
  must match, so deep non-`recur` recursion stays bounded by max depth.

## Decisions

### 1. Per-evaluation compiler and VM state

`runtime.Eval` (and `EvalFile`/`EvalWithBindings`) will compile each top-level
form with a fresh `compiler.NewCompiler` into a fresh chunk, and run it on VM
state that is not shared across calls or goroutines. Options weighed:

- **(a) Fresh VM per evaluation** — allocate a `*vm.VM` (or reset stack/frames)
  per `Eval`. Simple, matches the tree-walker's per-call state model, naturally
  concurrency-safe. Allocation is a few slices; negligible next to compilation.
- **(b) `sync.Pool` of VMs** — reuse VM buffers across calls to cut allocation.
  More moving parts; defer unless profiling shows (a) is a bottleneck.
- **(c) One VM guarded by a mutex** — serializes all evaluation; loses the
  concurrency the tree-walker already offers. Rejected.

Decision: **(a)** for correctness and concurrency, with **(b)** as a later
optimization if benchmarks justify it. The `BytecodeCache` stays shared (it is
disk-backed and content-addressed); its in-process access is made safe
independently.

### 2. `loop`/`recur` via a back-jump

`compileLoop` establishes the binding locals (as `let` does) and records the
instruction index of the loop body start plus the local slots of the loop
variables. `compileRecur` compiles each new value, stores it into the
corresponding loop local slot, then emits a new `OpLoop` (unconditional backward
jump) to the recorded start. `OpLoop` decrements `ip` by its operand, so
iteration reuses one frame and one stack region — O(1) stack, matching
`evalLoop`. `recur` outside a loop is a compile-time error, mirroring the
tree-walker's runtime guard.

The current `OpTailCall`-from-`recur` path is removed. `OpTailCall` may later be
emitted for genuine tail-position *function* calls as an optimization, but that
is out of scope here since the tree-walker does not optimize those either.

### 3. `try`/`catch`/`throw` via a handler stack

Add `OpSetupTry(handlerAddr)`, `OpPopTry`, and `OpThrow`. `compileTry` compiles
the body wrapped in `OpSetupTry`/`OpPopTry` and the catch clause at
`handlerAddr`, binding the caught value to the catch symbol's local slot.
`throw` compiles its argument then `OpThrow`.

The VM keeps a per-frame-region handler stack. Any error surfaced while a
handler is active — an `OpThrow`, or a Go `error` returned from `call()`/an
opcode — unwinds the stack to the handler's saved depth, converts the error to a
catchable `core.Value` (the thrown value for `OpThrow`; a wrapped error value
for Go errors, consistent with how `evalTry`/`evalCatch` expose it), binds it,
and jumps to `handlerAddr`. With no active handler the error propagates as
today.

### 4. Macro expansion before compilation

The runtime runs a macro-expansion pass over each form before compiling it,
reusing the existing `core.MacroExpand` against the engine's shared `*core.Env`.
`defmacro` is evaluated through the tree-walker at compile time to register the
`Macro` in the env (the VM and tree-walker share one env), so subsequent forms
expand correctly. This reuses proven expansion logic rather than reimplementing
it in the compiler, and it is what `compiler.CompileExpanded` +
`MacroExpander` were scaffolded for but never wired to.

### 5. Remaining special forms

`cond`, `and`, `or`, `not`, `defn`, and `quasiquote` compile directly:

- `cond` → chained test/branch with `OpJumpIfFalse`/`OpJump`, like nested `if`.
- `and`/`or` → short-circuit jump chains leaving the deciding value on the stack.
- `not` → compile the operand then a truthiness inversion (a small opcode or a
  compiled `if`).
- `defn` → desugar to `def` + `fn`, matching `evalDefn`.
- `quasiquote` → compile the template with `unquote`/`unquote-splicing` handling
  that mirrors `evalQuasiquote`.

### 6. Variadic packing and scope consistency

`VM.call` binds the first `Arity` positional args to locals and, when
`Chunk.Variadic`, packs the remaining args into a `core.List` bound to the rest
slot — mirroring `Env.ChildVariadic`. `def`/global get and set are made
consistent (both against the same environment the tree-walker's `evalDef`
targets) so nested `def` behaves identically under both evaluators.

### 7. No panics on bad bytecode

The unchecked `Constants[...].(core.Symbol)` assertions in `OpGetGlobal`/
`OpSetGlobal` (and any peers) become checked conversions that return a
`core.LispicoError` on mismatch, so a corrupt or version-skewed `.lbc` entry
yields a graceful error rather than a process panic. `cacheVersion` is bumped
whenever the opcode set or encoding changes in this work.

### 8. Verification

A single cross-validation corpus of programs (covering every special form,
nested scopes, closures, variadics, macros, `loop`/`recur`, and
`try`/`catch`/`throw`) is run through both evaluators and asserted equal. The
same corpus is driven through `runtime.New(..., WithBytecode())` end to end,
including a cache-hit path and a hot-reload path, so the runtime wiring is
exercised the way an embedder uses it. `go test -race` covers concurrent `Eval`.

## Risks / Trade-offs

- **Go-error / handler-stack integration is the hardest part.** Turning Go
  `error` returns into catchable values without leaking VM stack state needs
  careful unwinding and tests for nested `try`, `throw` across a `GoFunc`
  boundary, and re-throw. Mitigation: model it directly on `evalTry`/`evalCatch`
  and cross-validate.
- **Per-eval VM allocation** adds work versus the current shared VM. Expected to
  be dominated by compilation and cache I/O; if not, `sync.Pool` (decision 1b)
  recovers it.
- **Shared env between VM and tree-walker for macros** keeps a dependency from
  the compile path onto the tree-walker. This is acceptable — the tree-walker is
  the reference semantics and already lives in `core` — and avoids duplicating
  macro logic.
- **Scope-consistency change** may alter today's (incorrect) nested-`def`
  behavior under the VM. That is intended; parity with the tree-walker is the
  contract, and cross-validation locks it in.
