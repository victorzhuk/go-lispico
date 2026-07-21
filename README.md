# go-lispico

A zero-dependency, pluggable Lisp interpreter designed as an embeddable scripting kernel for Go applications.

## Features

- **Zero dependencies** in core package (stdlib only)
- **13 built-in types**: Nil, Bool, Int, Float, String, Symbol, Keyword, List, Vector, HashMap, GoFunc, Lambda, Macro
- **22 special forms**: if, def, defn, defmacro, fn, let, let*, do, quote, quasiquote, set!, when, unless, cond, loop, recur, try, catch, throw, and, or, not

  The names above are the kernel special-form names. Under the default
  CL dialect they are renamed: `do`→`progn`, `set!`→`setq`, etc.

- **Bytecode VM** (default) — compiled execution with per-form tree-walker fallback
- **Tree-walking evaluator** with `loop`/`recur` tail-call optimization for fallback path
- **Hot-reload** via `eng.Watch(ctx, dir)`
- **Plugin system** for extending functionality


The names above are the kernel special-form names. Under the default
CL dialect they are renamed: `do`→`progn`, `set!`→`setq`, etc.
## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"

    "github.com/victorzhuk/go-lispico/core"
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

    _, err = eng.Eval(context.Background(), "setup", "(defun add (a b) (+ a b))")
    if err != nil {
        panic(err)
    }
    add, err := eng.Func("add")
    if err != nil {
        panic(err)
    }
    sum, err := add.Call(context.Background(), core.Int{V: 2}, core.Int{V: 3})
    if err != nil {
        panic(err)
    }
    fmt.Println(sum) // 5
}
```

## Dialects

Build an engine with a specific dialect via `runtime.WithDialect(d)`. Two
dialects ship with the interpreter:

- `cl.Dialect()` — Common Lisp / Lisp-2 (default). Separates function and
  value cells, uses nil-only falsiness, disables bracket literals (`[...]`)
  in source, and renames many special forms and builtins for CL familiarity
  (`defun`→`defn`, `setq`→`set!`, `progn`→`do`, `car`→`first`, etc.).
- `clojure.Dialect()` — Clojure / Lisp-1 identity dialect. Single namespace,
  `nil`+`false` falsiness, bracket literals enabled. Compatible with the
  bytecode VM.

The default engine uses `cl.Dialect()`. Pass `WithDialect(clojure.Dialect())`
to opt in to the Clojure surface.

## Installation

```bash
go get github.com/victorzhuk/go-lispico
```


## REPL Binary

Build the interactive REPL:

```bash
make build    # produces bin/lispico
```

Interactive session with line editing, history, and multiline support:

```bash
./bin/lispico
```

Flags:

- `-dialect cl|clojure` — select dialect (default: `cl`)
- `-bytecode` — force bytecode VM mode (default)

File execution — evaluate file(s) in order, then exit:

```bash
./bin/lispico prog.lisp
./bin/lispico -dialect clojure prog.lisp
```

Piped input:

```bash
echo '(+ 1 2)' | ./bin/lispico   # prints 3, exits 0
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        PLUGINS                              │
│  stdlib  agent  llm  lio  net  exec  data  fsm              │
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

go-lispico is a language kernel first: the embedding host is expected to
register its own IO primitives, so the pure-computation plugins (`stdlib`,
`data`) are the actively developed surface. The world-touching plugins are
**frozen** — security and correctness fixes only
(see `docs/adr/0004-kernel-first-mission.md`).

| Plugin   | Status | Description                                                      |
| -------- | ------ | ---------------------------------------------------------------- |
| `stdlib` | active | Standard library (arithmetic, comparison, collections, strings) |
| `data`   | active | Data structures (JSON parsing)                                   |
| `fsm`    | idle   | Finite state machines (pure, no consumer)                        |
| `llm`    | frozen | LLM API bindings (OpenAI, etc.)                                  |
| `agent`  | frozen | Agent orchestration                                              |
| `lio`    | frozen | File I/O and environment                                         |
| `net`    | frozen | HTTP client                                                      |
| `exec`   | frozen | Shell execution and crypto                                       |

## Bytecode VM

`runtime.New()` defaults to bytecode VM execution. The VM compiles supported forms and defers unsupported forms to the tree-walking evaluator form-by-form (for example `defmacro` nested in a body, `unquote-splicing`).

Evaluator control:

- `runtime.WithTreeWalker()` — force tree-walk-only execution (rollback).
- `runtime.WithBytecode()` — force VM execution.
- `runtime.New()` options are last-wins; the later evaluator option controls mode.

## Resource limits

`runtime.WithResourceLimits(runtime.ResourceLimits{…})` configures six
resource ceilings before construction:

| Field | Default | Effect |
| --- | ---: | --- |
| `MaxReaderDepth` | 1024 | Maximum nesting depth of s-expressions |
| `MaxStructuralDepth` | 1024 | Maximum structural depth of evaluated / compiled `Vector`, `HashMap`, and quasiquote `List` literals |
| `MaxCollectionLen` | 10,000,000 | Maximum length of `range`-produced lists |
| `MaxCacheEntries` | 4096 | Maximum bytecode chunk cache entries |
| `MaxReductions` | 10,000,000 | Maximum reductions charged to one evaluation across reader bridge, macro expansion, compiler, evaluator, and `GoFunc` dispatch |
| `MaxAllocationBytes` | 64 MiB | Maximum shallow allocation bytes charged to one evaluation |

A non-positive field selects the default. Ceilings are immutable after `New`.
Exceeding one returns a `*core.LispicoError` with `Code: "ResourceLimitError"`.
Structural depth is enforced at evaluation time in both evaluators so
a dead-branch over-limit literal (`(if false <deep> 1)`) is not rejected.
Reductions piggyback the existing 128-step cancellation budget in both
evaluators, and allocation charging uses the fixed deterministic size table in
ADR 0011. Reader output is charged before the first form runs; VM/tree-walker
work and `GoFunc` re-entry share one per-evaluation ledger.

## Status

**Alpha** — Core functionality complete, API subject to change.

## License

[Apache License 2.0](LICENSE)
