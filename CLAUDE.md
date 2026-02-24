# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**go-lispico** is a zero-dependency, pluggable Lisp interpreter designed as an embeddable scripting kernel for Go applications. Status: **pre-alpha** — specifications are complete, implementation has not started.

Specs live in `.claude/docs/` and `openspec/`. Read them before implementing anything.

## Build & Test

No build system exists yet. When created, expected commands:

```sh
go build ./...
go test ./...
go test ./core/... -run TestName        # single test
go test ./... -bench=.                  # benchmarks
golangci-lint run
```

## Architecture

```
core/        ← zero external deps, <1000 lines total
  types.go   ← Value interface + 13 concrete types
  env.go     ← lexical scope chain, thread-safe (RWMutex)
  reader.go  ← tokenizer + S-expression parser
  eval.go    ← evaluator with TCO + 19 special forms
  plugin.go  ← Plugin interface + registry

runtime/     ← public Go embedding API (Engine struct)
  engine.go  ← New(), Eval(), Call(), Watch()

plugins/     ← opt-in domain plugins (each with own go.mod)
  stdlib/    ← pure-Lisp standard library, no Go deps
  llm/       ← LLM API bindings
  agent/     ← agent orchestration
  fs/        ← filesystem
  http/      ← HTTP client
  json/      ← JSON serialization
  exec/      ← shell execution
  crypto/    ← hashing & UUID

testdata/    ← .lisp files for integration tests
```

### Core Invariants

- `core/` has **zero external imports** — compiles with stdlib only
- All I/O lives in plugins, never in core
- Data structures are immutable (List, Vector, HashMap)
- Evaluation is deterministic: same input + env → same output
- No panics — all errors returned gracefully

### Plugin System

```go
type Plugin interface {
    Name() string          // namespace prefix, e.g. "llm", "fs"
    Init(env *Env) error   // registers functions into env
    Metadata() PluginMeta
}
```

Namespace convention: core functions have no prefix (`+`, `map`, `str`); plugin functions use `namespace/name` (`llm/complete`, `fs/read`).

### Value Types

13 concrete types implementing `Value`: `Nil`, `Bool`, `Int`, `Float`, `String`, `Symbol`, `Keyword`, `List`, `Vector`, `HashMap`, `GoFunc`, `Lambda`, `Macro`.

Only `nil` and `false` are falsy. Everything else is truthy.

### TCO

`eval.go` implements tail-call optimization via a trampoline loop. `loop`/`recur` and tail positions in `if`, `cond`, `do`, `fn` must not grow the stack.

## Implementation Order

Changes are tracked in `openspec/changes/`. Implement in dependency order:

1. **ch001** — Core Engine (types → env → reader → eval → plugin)
2. **ch002** — Standard Library Plugin
3. **ch003** — Runtime API (Engine embedding API)
4. **ch004+** — Domain plugins (llm, agent, fs, http, etc.)

Never start a change before its dependencies pass tests.

## Performance Targets

| Operation | Target |
|-----------|--------|
| Core boot | < 1ms |
| Simple expression eval | < 10µs |
| 1000-iteration loop | < 5ms |
| Memory per Engine | < 10MB |

## Key Specs to Read

- `.claude/docs/go-lispico.md` — full PRD (types, special forms, reader syntax, plugin authoring)
- `openspec/changes/ch001/` — Core engine design and task breakdown
- `openspec/changes/ch001/specs/` — Component-level specs for types, env, reader, eval, plugin
