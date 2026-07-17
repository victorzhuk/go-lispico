## 1. when/unless single result (red → green)

- [x] 1.1 Failing test: false `when` and true `unless` return `nil` in value positions — inside `let` bindings, `do` bodies, and function bodies — under `WithBytecode()`, matching the tree-walker.
- [x] 1.2 Compile the skipped branch to push `nil`, so every compiled expression leaves exactly one stack result.

## 2. set! lexical-owner semantics (red → green)

- [x] 2.1 Failing test: `set!` from an inner scope mutates the existing lexical owner (closure state persists across invocations); `set!` on an undefined binding returns a typed error under both paths.
- [x] 2.2 Emit distinct definition and mutation bytecode: definition writes the current scope; `set!` resolves the owning scope and errors when none exists; resolved local slots keep slot mutation.

## 3. try/catch slot layout (red → green)

- [x] 3.1 Failing test: locals bound after a `try`/`catch` form — on both the normal and the error path — read correct values, with no slot-index errors.
- [x] 3.2 Reserve the catch binding only in the handler scope; restore the previous local layout on handler exit.

## 4. Malformed-form validation (red → green)

- [x] 4.1 Failing tests: for every compiled special form, wrong-arity and wrong-shape inputs return typed errors, never panic — driven through public `Engine.Eval` under `WithBytecode()`.
- [x] 4.2 Validate arity and shape before the Compiler indexes operands, for every compiled special form.

## 5. Verify

- [x] 5.1 Extend the cross-validation corpus with all four defect families.
- [x] 5.2 `go test ./...` and `go test -race ./runtime/...` green; `golangci-lint run` clean.
