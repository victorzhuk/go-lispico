# Tasks: Correctness and safety hardening

## 1. Criticals

- [ ] 1.1 Remove `BytecodeCache` and `WithBytecodeCache`: delete the cache type, gob/version/atomic-rename machinery, the `cache` fields on `VM`/`bytecodeEvaluator`, and the nine cache tests; drop the option from `runtime`. (ADR-0002)
- [ ] 1.2 Guard empty-body `fn`/`defn` in `compileFn`: emit `OpNil` for an empty body as `compileDo` does, or reject with the tree-walker's arity error; add a stack-underflow guard in `VM.pop`. Regression: `((fn []))` and empty-body `defn` return an error under `WithBytecode()`, never panic.
- [ ] 1.3 Resolve symlinks in `plugins/lio/sandbox.go` (`filepath.EvalSymlinks` on the target, or its parent for a not-yet-existing write) before `withinRoot` and `DenyPattern`. Regression: an intermediate symlink pointing outside the root is denied for read and write.

## 2. VM parity and safety (ADR-0002)

- [ ] 2.1 Return a typed "unsupported in bytecode" error from the compiler for nested `defmacro` and `unquote-splicing`; make the bytecode `Eval` path fall back to the tree-walker for such forms instead of panicking or erroring opaquely.
- [ ] 2.2 Align `throw` so the value bound by `catch` has the same runtime type under both evaluators (tree-walker coercion is the reference). Cross-validate a non-String throw.
- [ ] 2.3 Bounds-check `OpGetLocal`/`OpSetLocal` and `OpMakeList`/`OpMakeVector`/`OpMakeMap` stack indexing; return a `*LispicoError` on a malformed chunk, never index out of range.
- [ ] 2.4 Extend the cross-validation corpus: nested `defmacro`, non-String `throw`, empty-body `fn`/`defn`.

## 3. Concurrency (ADR-0003)

- [ ] 3.1 Move `macroDepth`, `callDepth`, `loopDepth` off the shared `engine` struct into per-evaluation state threaded through `Eval`/`Apply`.
- [ ] 3.2 `-race` tests: concurrent macro expansion on one engine has no data race; a bare `(recur 1)` in one goroutine still errors "recur outside loop" while another goroutine runs a `loop`.

## 4. Error contract

- [ ] 4.1 Convert the ~51 `fmt.Errorf` sites in `core/eval.go` to `*LispicoError` with the appropriate `Code` (EvalError / TypeError / ArityError / UndefinedError).
- [ ] 4.2 Populate `Line`/`Col`/`Source` on eval-time errors where the failing form carries a position. Test that `errors.As(err, &lispicoErr)` succeeds for an eval-time error.

## 5. Literal semantics (ADR-0001)

- [ ] 5.1 Evaluate the elements of `[...]` and `{...}` in `core/eval.go` (move them out of the self-evaluating case), producing a new immutable value.
- [ ] 5.2 Add the `HashMap` case to `expandQuasiquote`.
- [ ] 5.3 Tests: `(let [x 99] [1 x])` → `[1 99]`, `{:a x}` → `{:a 99}`, `` `{:a ~x} `` expands. Record the breaking change in CHANGELOG.

## 6. Runtime API honesty

- [ ] 6.1 Apply `cfg.timeout` (`WithTimeout`) to `Eval`/`EvalWithBindings`, not just `Call`.
- [ ] 6.2 Make `WithHotReloadDir` start watching at `New`, or remove the option; ship only what works.
- [ ] 6.3 Make `Watch(ctx, dir)` honor the passed `ctx` for watcher lifetime.
- [ ] 6.4 Correct the reversed `Engine.Eval` doc comment (`source` labels the run; `input` is the code).

## 7. Targeted robustness

- [ ] 7.1 Cap the `net` response-body read with a documented limit (`io.LimitReader`); default sane.
- [ ] 7.2 `exec/pipe`: `Wait()` every started stage when a later `Start()` fails, so no zombie survives to ctx-timeout.
- [ ] 7.3 Isolate `io/env-*` per engine, or document that it mutates the process-global environment and is unsafe across concurrent scripts.

## 8. Documentation

- [ ] 8.1 Bring ARCHITECTURE.md, CLAUDE.md, and README in line with the code: the no-panics scope, typed errors, the concurrency contract, the removed `WithBytecodeCache`, and literal semantics; link the ADRs.
- [ ] 8.2 Update the `bytecode-vm`, `core-engine`, `runtime-api`, and `io-plugin` specs to this change's deltas on archive.

## Acceptance

- [ ] `((fn []))`, empty-body `defn`, and a malformed chunk return errors under `WithBytecode()`; no panic.
- [ ] A symlink escaping the sandbox root is denied for read and write.
- [ ] `WithBytecodeCache` is gone; `go build ./...` is green with no references.
- [ ] Concurrent `Eval` on one engine is correct under `-race`; the stray-`recur` cross-goroutine test passes deterministically.
- [ ] `errors.As` recovers a `*LispicoError` from an eval-time error.
- [ ] `(let [x 99] [1 x])` → `[1 99]` and `` `{:a ~x} `` expands.
- [ ] `WithTimeout` bounds an `Eval`; `Watch` stops when its `ctx` is cancelled.
- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run`, and `go test -race ./...` are green.
