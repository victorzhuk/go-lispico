# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**go-lispico** is a zero-dependency, pluggable Lisp interpreter designed as an embeddable scripting kernel for Go applications.

## Status

**Alpha** — Core functionality is complete. The project includes:
- Core interpreter with 13 types and 19 special forms
- Bytecode compiler and VM with TCO
- Runtime API with hot-reload support
- 8 plugins for common use cases

## Build & Test

```sh
go build ./...
go test ./...
go test ./core/... -run TestName        # single test
go test ./... -bench=. -benchmem        # benchmarks
golangci-lint run
```

## Architecture

```
core/           # Core interpreter (zero deps)
├── types.go    # Value interface + 13 concrete types
├── env.go      # Environment chain (lexical scope)
├── reader.go   # Tokenizer + S-expression parser
├── eval.go     # Tree-walking evaluator with TCO
├── plugin.go   # Plugin interface + registry
├── error.go    # Error types
├── compiler/   # Bytecode compiler
│   └── compiler.go
└── vm/         # Stack-based virtual machine
    ├── vm.go
    ├── chunk.go
    ├── opcode.go
    └── frame.go

runtime/        # Public Go embedding API
├── engine.go   # Engine interface (New, Eval, Call, Watch)
├── eval.go     # Evaluation helpers
├── repl.go     # Read-Eval-Print Loop
├── watch.go    # Hot-reload file watching
├── stats.go    # Runtime statistics
└── plugin.go   # Plugin loading

plugins/        # Domain plugins (opt-in deps)
├── stdlib/     # Standard library (pure Lisp + Go builtins)
├── llm/        # LLM API bindings
├── agent/      # Agent orchestration
├── lio/        # File I/O operations
├── net/        # HTTP client
├── exec/       # Shell execution + crypto
├── data/       # Data structures
└── fsm/        # Finite state machines
```

## Key Invariants

- `core/` has **zero external imports** — compiles with stdlib only
- All I/O lives in plugins, never in core
- Data structures are immutable (List, Vector, HashMap)
- Evaluation is deterministic: same input + env → same output
- No panics — all errors returned gracefully

## Plugin System

```go
type Plugin interface {
    Name() string          // namespace prefix, e.g. "llm", "lio"
    Init(env *Env) error   // registers functions into env
    Metadata() PluginMeta
}
```

Namespace convention: core functions have no prefix (`+`, `map`, `str`); plugin functions use `namespace/name` (`llm/complete`, `lio/read`).

## Value Types

13 concrete types implementing `Value`: `Nil`, `Bool`, `Int`, `Float`, `String`, `Symbol`, `Keyword`, `List`, `Vector`, `HashMap`, `GoFunc`, `Lambda`, `Macro`.

Only `nil` and `false` are falsy. Everything else is truthy.

## Special Forms

19 special forms: `if`, `def`, `defn`, `fn`, `let`, `let*`, `do`, `quote`, `quasiquote`, `set!`, `when`, `unless`, `cond`, `loop`, `recur`, `try`, `catch`, `throw`, `and`, `or`.

## TCO

Both `eval.go` (tree-walking) and `vm/vm.go` (bytecode) implement tail-call optimization. `loop`/`recur` and tail positions in `if`, `cond`, `do`, `fn` must not grow the stack.

## Performance Targets

| Operation | Target |
|-----------|--------|
| Core boot | < 1ms |
| Simple expression eval | < 10µs |
| 1000-iteration loop | < 5ms |
| Memory per Engine | < 10MB |
