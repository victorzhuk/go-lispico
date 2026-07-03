# Tasks: Bytecode VM production readiness

## 1. Runtime evaluation path

- [ ] 1.1 Compile each top-level form with a fresh `compiler.NewCompiler` into a fresh chunk in `runtime/eval.go`; never reuse a compiler across `Eval` calls.
- [ ] 1.2 Run each compiled chunk on VM state that is not shared across calls or goroutines (fresh `*vm.VM` per evaluation, or reset isolated state); keep the `BytecodeCache` shared.
- [ ] 1.3 Add a runtime regression test: two sequential `Eval` calls return their own results (not the first form's), and the shared cache still hits.
- [ ] 1.4 Add a `-race` test: concurrent `Eval` on one `WithBytecode()` engine produces correct results with no data race.

## 2. Macro expansion

- [ ] 2.1 Run `core.MacroExpand` over each form against the engine env before compiling, in the bytecode `Eval` path.
- [ ] 2.2 Evaluate `defmacro` at compile time (through the tree-walker on the shared env) so later forms expand; wire `compiler.CompileExpanded`/`MacroExpander` accordingly.
- [ ] 2.3 Cross-validate a macro definition and its use under both evaluators.

## 3. Special forms — direct compilation

- [ ] 3.1 `cond` → test/branch chain.
- [ ] 3.2 `and` / `or` → short-circuit jump chains leaving the deciding value.
- [ ] 3.3 `not` → operand + truthiness inversion.
- [ ] 3.4 `defn` → desugar to `def` + `fn`.
- [ ] 3.5 `quasiquote` → template compilation with `unquote`/`unquote-splicing`, mirroring `evalQuasiquote`.
- [ ] 3.6 Per-form cross-validation cases for each of the above.

## 4. loop / recur

- [ ] 4.1 Add `OpLoop` (unconditional backward jump) to `opcode.go`, `chunk.go`, and the VM run loop.
- [ ] 4.2 `compileLoop`: establish binding locals, record loop-start ip and loop-var slots.
- [ ] 4.3 `compileRecur`: compile new values, store into loop slots, emit `OpLoop`; remove the `OpTailCall`-from-`recur` path.
- [ ] 4.4 Compile-time error for `recur` outside a loop.
- [ ] 4.5 Cross-validate a 10,000-iteration `loop`/`recur` runs in O(1) stack and matches the tree-walker; replace the invalid loop benchmark with one whose body runs.

## 5. try / catch / throw

- [ ] 5.1 Add `OpSetupTry(handlerAddr)`, `OpPopTry`, `OpThrow` opcodes and a per-frame handler stack in the VM.
- [ ] 5.2 `compileTry`: wrap body in `OpSetupTry`/`OpPopTry`, compile the catch clause binding the caught value to the catch symbol; compile `throw` to its argument + `OpThrow`.
- [ ] 5.3 Unwind to the nearest handler on `OpThrow` and on Go `error` returns from `call()`/opcodes, converting the error to a catchable value consistent with `evalTry`/`evalCatch`.
- [ ] 5.4 Cross-validate: catch a thrown value, catch an error raised inside a `GoFunc`, nested `try`, and re-throw.

## 6. Variadics and scope consistency

- [ ] 6.1 `VM.call`: pack rest args into a `core.List` for the variadic slot, mirroring `Env.ChildVariadic`.
- [ ] 6.2 Align VM `def`/global get and set with the environment the tree-walker's `evalDef` targets; cross-validate nested `def`.

## 7. Robustness

- [ ] 7.1 Replace unchecked `Constants[...].(core.Symbol)` assertions in `OpGetGlobal`/`OpSetGlobal` (and peers) with checked conversions returning a `core.LispicoError`.
- [ ] 7.2 Bump `cacheVersion` for the new opcode set/encoding; add a test that a stale/corrupt `.lbc` entry yields a graceful error, never a panic.

## 8. Cross-validation corpus and integration

- [ ] 8.1 Build one cross-validation corpus covering every special form, closures, variadics, macros, `loop`/`recur`, and `try`/`catch`/`throw`; run each program through both evaluators and assert equal results/errors.
- [ ] 8.2 Runtime integration tests through `runtime.New(..., WithBytecode())`: happy path, cache-hit path, and hot-reload path.
- [ ] 8.3 Re-establish the ≥10x arithmetic-loop benchmark against a running loop body; record the honest number.

## 9. Documentation

- [ ] 9.1 Remove the "experimental / incomplete" language from `runtime.WithBytecode`/`WithBytecodeCache` godoc, the README Bytecode VM section, CLAUDE.md, and ARCHITECTURE.md once parity holds.
- [ ] 9.2 Update the `bytecode-vm` spec to the production contract (this change's spec delta) on archive.

## Acceptance

- [ ] All 22 special forms and variadic/macro programs match the tree-walker in the cross-validation corpus.
- [ ] `WithBytecode()` runtime integration tests pass, including sequential and concurrent (`-race`) evaluation, cache hit, and hot-reload.
- [ ] No panic on any malformed or corrupt bytecode input.
- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run`, and `go test -race ./...` are green.
