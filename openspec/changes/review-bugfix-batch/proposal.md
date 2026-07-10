# Review Bugfix Batch

## Why

A full project review verified nine defects live against HEAD, headlined by a critical one: the stdlib bootstrap macros (`->`, `->>`, `as->`, `if-let`, `when-let`, `get-in`) are undefined under the default Common Lisp dialect because the bootstrap loader hardcodes a Lisp-1 evaluator. Alongside it: `UnloadPlugin` leaves unloaded functions callable, the REPL hangs on trailing comments, parse errors drop known positions, and the docs describe a pre-dialect default that no longer exists.

## What Changes

- `plugins/stdlib/bootstrap.go` uses the env's dialect-aware evaluator instead of a hardcoded `core.NewEvaluator()`, so bootstrap macros bind into the function cell under Lisp-2; regression test goes through `runtime.New`.
- `UnloadPlugin` deletes the plugin's env bindings (tracked at `Use` time); `ReloadPlugin` clears them before re-`Init`. Unloaded functions become `UndefinedError`.
- REPL `isBalanced` skips `;` comments to end of line, matching the reader's comment rule; no more hang on `(+ 1 2) ; note (`.
- Reader threads token line/col into `parseNumber` and reports the real EOF position instead of `0,0`.
- `evalThrow` and the VM max-call-depth error become `*LispicoError` like every sibling error, so `errors.As` works uniformly.
- HashMap construction paths (`reader`, `eval` literal/quasiquote, stdlib `hash-map`) build via the internal `Set` escape hatch on a fresh map instead of O(n²) `Assoc` loops.
- Dead exported compiler API (`MacroExpander`, `CompileExpanded`) deleted; `format` builtin gains a table test.
- Docs-only: README gains a Dialects section; special-forms tables caveated with CL default renames (`do`→`progn`, `set!`→`setq`); stale `cl/cl.go` doc paragraph rewritten; `cl/` and `clojure/` added to directory trees.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: bootstrap macros must be available under every dialect the engine runs, including Lisp-2 head-position lookup.
- `runtime-api`: `UnloadPlugin` removes the plugin's bindings from the environment; REPL input balancing ignores `;` comments.
- `core-engine`: reader errors carry the originating token's line/col (invalid numbers, unexpected EOF); `throw` produces a typed `LispicoError`.
- `bytecode-vm`: max-call-depth error is a typed `LispicoError`.

## Impact

- Code: `plugins/stdlib/bootstrap.go`, `runtime/plugin.go`, `runtime/repl.go`, `core/reader.go`, `core/eval.go`, `core/types.go` call sites, `core/vm/vm.go`, `core/compiler/compiler.go` (deletion), `plugins/stdlib/collections.go`, `plugins/stdlib/strings.go` tests.
- Docs: `README.md`, `ARCHITECTURE.md`, `CLAUDE.md`, `cl/cl.go` doc comment.
- No public API additions; one exported dead interface removed (`compiler.MacroExpander`, `compiler.CompileExpanded`) — unused anywhere, technically breaking for external importers.
- Explicitly out of scope: VM/compiler feature work (separate vm-first change), REPL binary (separate change).
