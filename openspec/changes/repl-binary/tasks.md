# Tasks — repl-binary

## 1. Shared balance rule

- [ ] 1.1 Share the comment-aware balance check between library REPL and binary (depends on review-bugfix-batch task 3.1)

## 2. Binary skeleton

- [ ] 2.1 `cmd/lispico/main.go`: flag parsing (`-dialect`, `-bytecode`), engine construction, stdlib+data plugins loaded
- [ ] 2.2 File-execution mode: evaluate positional args in order, exit codes and stderr per spec
- [ ] 2.3 Non-TTY fallback through the io-based REPL path

## 3. Terminal REPL

- [ ] 3.1 Raw-mode session on `x/term.Terminal` with deferred restore; Ctrl-C discards input, Ctrl-D exits
- [ ] 3.2 Multiline continuation prompt using the shared balance rule
- [ ] 3.3 History load/save at `${XDG_STATE_HOME:-~/.local/state}/lispico/history`, non-fatal on error

## 4. Build and docs

- [ ] 4.1 Build target producing `bin/lispico`; `go.mod` gains `golang.org/x/term`
- [ ] 4.2 README usage section (interactive, flags, file mode)

## 5. Verify

- [ ] 5.1 Tests: piped input, file mode success/failure with position, unknown dialect error; manual TTY session check
- [ ] 5.2 `go build ./... && go test ./... && golangci-lint run` clean
- [ ] 5.3 `openspec validate repl-binary` passes
