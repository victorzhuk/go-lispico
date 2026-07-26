## 1. Establish the fact

- [x] 1.1 Compile `(defmacro m (x) x)` and `(do (defmacro m (x) x))` directly
      through `NewCompiler().Compile(...)`. Both return
      `BytecodeUnsupported: defmacro is not supported by the bytecode compiler`.
      Taken from the compiler rather than inferred from runtime behaviour.
- [x] 1.2 Locate the cause: the rejection is a case in `compileList`'s
      special-form switch (`core/compiler/compiler.go:289`), reached for any
      list form at any position — nothing about it is nesting-specific.

## 2. Correct every statement of it

- [x] 2.1 `openspec/specs/bytecode-vm/spec.md` — the `Unsupported form defers
      to the tree-walker` scenario. This is the one that matters most: a wrong
      spec is the reason the wrong claim propagated.
- [x] 2.2 `CLAUDE.md` — the status line agents read first.
- [x] 2.3 `README.md`.
- [x] 2.4 `docs/adr/0002-bytecode-vm-disposition.md`.
- [x] 2.5 `docs/adr/0013-bytecode-default-authorized-by-the-gold-set.md`.
- [x] 2.6 `core/compiler/compiler.go` — the `CodeUnsupported` doc comment.
- [x] 2.7 `runtime/eval.go` — the `isUnsupportedInBytecode` doc comment.
- [x] 2.8 Leave archived changes under `openspec/changes/archive/` untouched.
      They are a record of what was believed at the time; rewriting history to
      match present knowledge would destroy the thing that makes an archive
      useful.

## 3. Pin it

- [x] 3.1 A compiler test asserting both a top-level and a nested `defmacro`
      return the typed unsupported error, so the narrower claim cannot come
      back without a failing test.

## 4. Verify

- [x] 4.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `golangci-lint run`
      clean — 0 issues.
- [x] 4.2 `go test ./... -count=1` 2443 passed.
- [x] 4.3 `openspec validate --specs --strict` — 14 passed, 0 failed.
- [x] 4.4 No benchmark or gate run: this change compiles no differently and
      allocates no differently. Running the gate would only add noise to the
      record.
