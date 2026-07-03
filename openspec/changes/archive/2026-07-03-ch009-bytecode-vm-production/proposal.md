# Change Proposal: Bytecode VM production readiness

## Why

The bytecode VM shipped in ch008 is a prototype, not a production evaluator. It
is opt-in behind `runtime.WithBytecode()` and currently only executes a subset
of the language correctly. Several defects make it unsafe to recommend:

- **Runtime path re-executes stale code.** `runtime.New` builds one
  `compiler.NewCompiler("<runtime>")` and reuses it for every `Eval`. Each call
  appends to the *same* chunk and emits another `OpReturn`; `VM.Run` starts at
  `ip 0`, so the second and later `Eval` calls re-run earlier instructions and
  return the first form's result. Multi-call evaluation is broken.
- **`loop`/`recur` are miscompiled.** `compileLoop` aliases `compileLet` and
  `compileRecur` emits `OpTailCall` with no callable on the stack, so any real
  loop fails with a "callable" type error. The published "830× faster" loop
  benchmark never executed the loop body.
- **Nine of the twenty-two special forms are not compiled** — `defn`,
  `defmacro`, `cond`, `quasiquote`, `catch`, `throw`, `and`, `or`, `not` — so
  programs that work under the tree-walker fail with "undefined" under the VM.
- **`try`/`catch`/`throw` do nothing.** `compileTry` aliases `compileDo`; there
  is no handler mechanism or exception opcode.
- **Macros are never expanded.** `MacroExpander`/`CompileExpanded` exist but are
  not called by the runtime; `defmacro` is not compiled.
- **Variadic functions are broken.** `VM.call` binds arguments positionally and
  never packs the rest parameter into a list.
- **Not concurrency-safe.** The engine stores one `*vm.VM` with a shared stack,
  frame slice, and depth counter; concurrent `Eval` races and corrupts state.
  The tree-walker is safe here.
- **Panic surface on corrupt bytecode.** `OpGetGlobal`/`OpSetGlobal` use
  unchecked `Constants[...].(core.Symbol)` assertions that panic on a tampered
  or malformed `.lbc` cache entry.
- **Untested end to end.** No test constructs an engine with `WithBytecode()`;
  the cross-validation suite omits every form above, which is why all of this
  went unnoticed.

Making the VM production-ready lets the project honestly offer the performance
path it advertises, and removes the "experimental" caveats now in the README and
architecture docs.

## What Changes

- Fix the runtime evaluation path so each `Eval` compiles into a fresh chunk and
  runs it once, with no cross-call state leakage.
- Compile all twenty-two special forms with results identical to the
  tree-walking evaluator, including real `loop`/`recur` tail iteration and a
  working `try`/`catch`/`throw` via new exception opcodes.
- Expand macros at compile time by running a macro-expansion pass before
  compilation, so `defmacro` and macro use behave as they do in the tree-walker.
- Pack variadic rest parameters into a list in `VM.call`, matching
  `Env.ChildVariadic`.
- Make the bytecode evaluator safe for concurrent `Eval` on one engine (fresh
  per-evaluation VM state, or an equivalent isolation strategy chosen in design).
- Replace unchecked type assertions in the VM with graceful errors so malformed
  or corrupt bytecode never panics.
- Add a cross-validation corpus that runs the same programs through both
  evaluators and asserts identical results, plus runtime integration tests that
  exercise `WithBytecode()` end to end (including the cache and hot-reload).
- Update `runtime.WithBytecode`/`WithBytecodeCache` godoc, the README, CLAUDE.md,
  and ARCHITECTURE.md to drop the "experimental/incomplete" language once the
  contract holds.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: promotes the evaluator from an experimental subset to a
  verified, concurrency-safe, full-language execution path at parity with the
  tree-walking evaluator.

## Impact

- **Code:** `core/compiler/compiler.go` (all special forms, macro pass, variadic
  params), `core/vm/vm.go` (exception opcodes, variadic packing, per-eval state,
  assertion hardening, scope consistency), `core/vm/opcode.go` and
  `core/vm/chunk.go` (new opcodes), `runtime/engine.go` and `runtime/eval.go`
  (per-eval compiler/VM wiring, macro-expansion hook), `core/vm/crossval_test.go`
  and `runtime/*_test.go` (coverage).
- **Public API:** no signature changes; `WithBytecode`/`WithBytecodeCache` keep
  their shape. Behavior changes from "partial" to "full parity"; the
  experimental caveat is removed from docs.
- **Performance:** honest benchmarks replace the current invalid loop benchmark;
  the ≥10x arithmetic-loop target is re-established against a loop body that
  actually runs.
- **Dependencies:** none. Core stays zero-dependency.
