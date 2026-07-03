# go-lispico

A zero-dependency, pluggable Lisp interpreter designed as an embeddable scripting kernel for Go applications.

## Features

- **Zero dependencies** in core package (stdlib only)
- **13 built-in types**: Nil, Bool, Int, Float, String, Symbol, Keyword, List, Vector, HashMap, GoFunc, Lambda, Macro
- **22 special forms**: if, def, defn, defmacro, fn, let, let*, do, quote, quasiquote, set!, when, unless, cond, loop, recur, try, catch, throw, and, or, not
- **Tree-walking evaluator** with `loop`/`recur` tail-call optimization
- **Bytecode VM** (experimental — see [Bytecode VM](#bytecode-vm-experimental))
- **Hot-reload** with file watching
- **Plugin system** for extending functionality

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"

    "github.com/victorzhuk/go-lispico/plugins/stdlib"
    "github.com/victorzhuk/go-lispico/runtime"
)

func main() {
    log := slog.New(slog.NewTextHandler(os.Stdout, nil))

    eng, err := runtime.New(log)
    if err != nil {
        panic(err)
    }
    defer eng.Close()

    // Load the standard library so +, map, str, etc. are available
    if err := eng.Use(stdlib.New()); err != nil {
        panic(err)
    }

    // Eval(ctx, source, input): source is a label for logs/stats, input is the code
    result, err := eng.Eval(context.Background(), "example", "(+ 1 2 3)")
    if err != nil {
        panic(err)
    }
    fmt.Println(result) // 6
}
```

## Installation

```bash
go get github.com/victorzhuk/go-lispico
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        PLUGINS                              │
│  stdlib  agent  llm  lio  net  exec  data  fsm             │
│   (each with optional external dependencies)                │
├─────────────────────────────────────────────────────────────┤
│                      RUNTIME                                │
│  Engine (embedding API) + REPL + Watch + Stats              │
├─────────────────────────────────────────────────────────────┤
│                        CORE                                 │
│  Types → Env → Reader → Eval → Plugin (zero deps)           │
│                    ↓                                        │
│              Compiler → VM (bytecode)                       │
└─────────────────────────────────────────────────────────────┘
```

## Plugins

| Plugin | Description |
|--------|-------------|
| `stdlib` | Standard library (arithmetic, collections, strings) |
| `llm` | LLM API bindings (OpenAI, etc.) |
| `agent` | Agent orchestration |
| `lio` | File I/O and environment |
| `net` | HTTP client |
| `exec` | Shell execution and crypto |
| `data` | Data structures (JSON parsing) |
| `fsm` | Finite state machines |

## Bytecode VM (experimental)

The tree-walking evaluator (`runtime.New` default) is the complete, supported
execution path. A bytecode compiler and VM are also present behind
`runtime.WithBytecode()` / `runtime.WithBytecodeCache(dir)`, but the VM path is
**experimental and incomplete**: `loop`/`recur` and several special forms
(`defn`, `defmacro`, `cond`, `quasiquote`, `try`/`catch`/`throw`, `and`/`or`,
`not`) are not yet compiled. Use the default evaluator for anything beyond simple
expressions.

## Status

**Alpha** — Core functionality complete, API subject to change.

## License

[Apache License 2.0](LICENSE)
