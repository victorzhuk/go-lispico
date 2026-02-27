# go-lispico

A zero-dependency, pluggable Lisp interpreter designed as an embeddable scripting kernel for Go applications.

## Features

- **Zero dependencies** in core package (stdlib only)
- **13 built-in types**: Nil, Bool, Int, Float, String, Symbol, Keyword, List, Vector, HashMap, GoFunc, Lambda, Macro
- **19 special forms**: if, def, defn, fn, let, let*, do, quote, quasiquote, set!, when, unless, cond, loop, recur, try, catch, throw, and, or
- **Bytecode VM** with tail-call optimization
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
    
    "github.com/victorzhuk/go-lispico/core"
    "github.com/victorzhuk/go-lispico/runtime"
)

func main() {
    log := slog.New(slog.NewTextHandler(os.Stdout, nil))
    
    // Create engine with bytecode VM
    eng, err := runtime.New(log, 
        runtime.WithBytecode(),
        runtime.WithBytecodeCache(".cache"),
    )
    if err != nil {
        panic(err)
    }
    defer eng.Close()
    
    // Evaluate code
    result, err := eng.Eval(context.Background(), "(+ 1 2 3)", "")
    if err != nil {
        panic(err)
    }
    fmt.Println(result) // Int{V:6}
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

## Status

**Alpha** — Core functionality complete, API subject to change.

## License

[MIT License](LICENSE)
