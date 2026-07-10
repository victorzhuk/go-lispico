# REPL Binary

## Why

The REPL exists only as a library method over `io.Reader`/`io.Writer` — no binary ships, and there is no line editing, history, or interrupt handling, so the project's own language has no usable interactive surface. The grill resolved: a `cmd/lispico` binary built on `golang.org/x/term`, keeping zero CGO and no third-party dependencies.

## What Changes

- New `cmd/lispico` binary: interactive REPL with raw-mode line editing (cursor movement, kill/yank basics), persistent history, multiline continuation via the reader's balance rule, Ctrl-C cancels the current input, Ctrl-D exits.
- Flags: `-dialect` (`cl` default, `clojure`), `-bytecode` opt-in; positional file arguments evaluate in order then exit (REPL only when no files given).
- Dependency: `golang.org/x/term` (binary only — `core/` stays zero-import).
- Library `Engine.REPL` stays io-based and unchanged for embedding and tests; the binary layers terminal handling on top.

## Capabilities

### New Capabilities

- `repl-cli`: the interactive `lispico` binary — terminal handling, history, dialect selection, file execution.

### Modified Capabilities

None — the library REPL contract is untouched.

## Impact

- Code: new `cmd/lispico/`; `go.mod` gains `golang.org/x/term`.
- Docs: README usage section for the binary; binary output lands in `bin/` per project convention.
- Depends on the `review-bugfix-batch` fix to comment-aware balancing (shared rule with the library REPL).
- Out of scope: completion, syntax highlighting, third-party readline libs.
