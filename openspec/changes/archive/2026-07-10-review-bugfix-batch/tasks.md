# Tasks — review-bugfix-batch

## 1. Critical: bootstrap evaluator

- [x] 1.1 `plugins/stdlib/bootstrap.go`: use `env.Evaluator()`, fall back to `core.NewEvaluator()` only when nil
- [x] 1.2 Regression tests in `runtime` package through `runtime.New(nil)`: `(-> 1 (+ 2))` → `3`; all six bootstrap macros (`->`, `->>`, `as->`, `if-let`, `when-let`, `get-in`) resolve in head position

## 2. Plugin unload

- [x] 2.1 Add root-frame key listing + `Delete` to `core.Env` (minimal surface) — *expanded: `Delete` now clears both `vars` and `funcs`; `FuncNames()` added*
- [x] 2.2 `Use` snapshots root-env keys around `Init`, records the plugin's bindings — *snapshots union of `VarNames()` + `FuncNames()`*
- [x] 2.3 `UnloadPlugin` deletes recorded bindings; `ReloadPlugin` deletes before re-`Init`
- [x] 2.4 Tests: `(json/encode "hi")` → `UndefinedError` after unload; reload leaves exactly the fresh `Init` bindings

## 3. REPL balance check

- [x] 3.1 `runtime/repl.go` `isBalanced`: skip `;` to end of line (outside strings), matching `readComment`
- [x] 3.2 Test: `(+ 1 2) ; note (` evaluates to `3` without waiting for continuation

## 4. Reader positions and error typing

- [x] 4.1 Thread `tok.line`/`tok.col` into `parseNumber`; invalid number errors carry the token position
- [x] 4.2 `Parser.Parse`: unexpected-EOF error uses the EOF token's recorded line/col
- [x] 4.3 `evalThrow` returns `*LispicoError`; VM max-call-depth error returns `*LispicoError`
- [x] 4.4 Tests: position assertions for invalid number and EOF; `errors.As` for uncaught throw and VM depth error

## 5. Map construction

- [x] 5.1 Replace `Assoc`-in-loop construction with `Set` on a fresh map: `core/reader.go` map literal, `core/eval.go` literal + quasiquote, stdlib `hash-map`
- [x] 5.2 Benchmark or test guard confirming behavior unchanged (existing map tests pass)

## 6. Dead code and test gap

- [x] 6.1 Delete `compiler.MacroExpander` and `CompileExpanded`
- [x] 6.2 Table test for `format` builtin

## 7. Docs

- [x] 7.1 README: add Dialects section (`WithDialect`, `cl.Dialect()`, `clojure.Dialect()`, default-flip note)
- [x] 7.2 README/ARCHITECTURE.md/CLAUDE.md: caveat special-forms tables as kernel names; note CL renames (`do`→`progn`, `set!`→`setq`)
- [x] 7.3 Rewrite stale `cl/cl.go` doc paragraph (List params work via `paramsAsVector`)
- [x] 7.4 Add `cl/`, `clojure/` to directory trees in ARCHITECTURE.md and CLAUDE.md

## 8. Verify

- [x] 8.1 `go build ./... && go test ./... && golangci-lint run` clean
- [x] 8.2 `openspec validate review-bugfix-batch` passes
