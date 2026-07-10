# Tasks — review-bugfix-batch

## 1. Critical: bootstrap evaluator

- [ ] 1.1 `plugins/stdlib/bootstrap.go`: use `env.Evaluator()`, fall back to `core.NewEvaluator()` only when nil
- [ ] 1.2 Regression tests in `runtime` package through `runtime.New(nil)`: `(-> 1 (+ 2))` → `3`; all six bootstrap macros (`->`, `->>`, `as->`, `if-let`, `when-let`, `get-in`) resolve in head position

## 2. Plugin unload

- [ ] 2.1 Add root-frame key listing + `Delete` to `core.Env` (minimal surface)
- [ ] 2.2 `Use` snapshots root-env keys around `Init`, records the plugin's bindings
- [ ] 2.3 `UnloadPlugin` deletes recorded bindings; `ReloadPlugin` deletes before re-`Init`
- [ ] 2.4 Tests: `(json/encode "hi")` → `UndefinedError` after unload; reload leaves exactly the fresh `Init` bindings

## 3. REPL balance check

- [ ] 3.1 `runtime/repl.go` `isBalanced`: skip `;` to end of line (outside strings), matching `readComment`
- [ ] 3.2 Test: `(+ 1 2) ; note (` evaluates to `3` without waiting for continuation

## 4. Reader positions and error typing

- [ ] 4.1 Thread `tok.line`/`tok.col` into `parseNumber`; invalid number errors carry the token position
- [ ] 4.2 `Parser.Parse`: unexpected-EOF error uses the EOF token's recorded line/col
- [ ] 4.3 `evalThrow` returns `*LispicoError`; VM max-call-depth error returns `*LispicoError`
- [ ] 4.4 Tests: position assertions for invalid number and EOF; `errors.As` for uncaught throw and VM depth error

## 5. Map construction

- [ ] 5.1 Replace `Assoc`-in-loop construction with `Set` on a fresh map: `core/reader.go` map literal, `core/eval.go` literal + quasiquote, stdlib `hash-map`
- [ ] 5.2 Benchmark or test guard confirming behavior unchanged (existing map tests pass)

## 6. Dead code and test gap

- [ ] 6.1 Delete `compiler.MacroExpander` and `CompileExpanded`
- [ ] 6.2 Table test for `format` builtin

## 7. Docs

- [ ] 7.1 README: add Dialects section (`WithDialect`, `cl.Dialect()`, `clojure.Dialect()`, default-flip note)
- [ ] 7.2 README/ARCHITECTURE.md/CLAUDE.md: caveat special-forms tables as kernel names; note CL renames (`do`→`progn`, `set!`→`setq`)
- [ ] 7.3 Rewrite stale `cl/cl.go` doc paragraph (List params work via `paramsAsVector`)
- [ ] 7.4 Add `cl/`, `clojure/` to directory trees in ARCHITECTURE.md and CLAUDE.md

## 8. Verify

- [ ] 8.1 `go build ./... && go test ./... && golangci-lint run` clean
- [ ] 8.2 `openspec validate review-bugfix-batch` passes
