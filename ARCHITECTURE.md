# Architecture

This document describes the architecture of go-lispico, a zero-dependency, pluggable Lisp interpreter for Go.

## Overview

go-lispico is designed as an embeddable scripting kernel with three layers:

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

## Package Structure

### core/

The core package contains the interpreter kernel with **zero external dependencies**. It compiles with only Go's standard library.

```
core/
├── types.go      # Value interface and 13 concrete types
├── env.go        # Lexical environment chain
├── reader.go     # Tokenizer and S-expression parser
├── eval.go       # Tree-walking evaluator with TCO
├── plugin.go     # Plugin interface and registry
├── error.go      # Error types
├── compiler/     # Bytecode compiler
│   └── compiler.go
└── vm/           # Stack-based virtual machine
    ├── vm.go       # VM execution loop
    ├── chunk.go    # Bytecode chunks
    ├── opcode.go   # Instruction opcodes
    └── frame.go    # Call frames
```

#### Value Types

All values implement the `Value` interface:

```go
type Value interface {
    Type() Keyword
    String() string
    Equals(other Value) bool
}
```

13 concrete types:

| Type      | Description            | Example          |
| --------- | ---------------------- | ---------------- |
| `Nil`     | Null value             | `nil`            |
| `Bool`    | Boolean                | `true`, `false`  |
| `Int`     | 64-bit integer         | `42`             |
| `Float`   | 64-bit float           | `3.14`           |
| `String`  | UTF-8 string           | `"hello"`        |
| `Symbol`  | Identifier             | `foo`            |
| `Keyword` | Constant key           | `:key`           |
| `List`    | Linked list            | `(1 2 3)`        |
| `Vector`  | Indexed array          | `[1 2 3]`        |
| `HashMap` | Key-value map          | `{:a 1}`         |
| `GoFunc`  | Go function            | `+`, `map`       |
| `Lambda`  | User function          | `(fn [x] x)`     |
| `Macro`   | Compile-time expansion | `(defmacro ...)` |

#### Environment

Environments form a chain for lexical scoping:

```go
type Env struct {
    parent   *Env
    bindings map[Symbol]Value
    mu       sync.RWMutex
}
```

Each environment:

- Holds bindings for its scope
- Has optional parent for lookup chain
- Is thread-safe with RWMutex

#### Reader

The reader (`reader.go`) transforms source text into AST:

1. **Tokenization**: Split input into tokens
2. **Parsing**: Build S-expression tree

Supports:

- Numbers (int, float)
- Strings (with escape sequences)
- Symbols and keywords
- Lists `()`, vectors `[]`, maps `{}`
- Comments starting with `;`

#### Evaluator

Two evaluation modes:

1. **Tree-walking** (`eval.go`): direct AST traversal — the default, complete path
2. **Bytecode VM** (`vm/`): compiled execution

Tail-call optimization is explicit: `loop`/`recur` iterate without growing the Go
stack (Clojure-style). Ordinary self-recursion is not auto-optimized; it is
bounded by the configured max eval depth.

#### Special Forms

22 special forms handled directly by the evaluator:

| Form         | Purpose               |
| ------------ | --------------------- |
| `if`         | Conditional           |
| `def`        | Define variable       |
| `defn`       | Define function       |
| `defmacro`   | Define macro          |
| `fn`         | Lambda expression     |
| `let`        | Local bindings        |
| `let*`       | Sequential bindings   |
| `do`         | Sequence expressions  |
| `quote`      | Prevent evaluation    |
| `quasiquote` | Template quoting      |
| `set!`       | Mutate variable       |
| `when`       | Conditional with body |
| `unless`     | Negated conditional   |
| `cond`       | Multi-way conditional |
| `loop`       | Loop with recur       |
| `recur`      | Tail recursion        |
| `try`        | Exception handling    |
| `catch`      | Catch exception       |
| `throw`      | Raise exception       |
| `and`, `or`  | Short-circuit logic   |
| `not`        | Boolean negation      |

The names above are the kernel special-form names. Under the default
CL dialect they are renamed: `do`→`progn`, `set!`→`setq`, etc.

### cl/

The Common Lisp dialect package. Exports `Dialect()` which returns a
non-identity composition over `core.FullDialect` with Lisp-2 name resolution,
nil-only falsiness, CL reader flags, and vocabulary-renamed function names.

### clojure/

The Clojure dialect package. Exports `Dialect()` which returns the identity
dialect (`core.FullDialect`) — Lisp-1, nil+false falsiness, bracket literals
enabled, no vocabulary map. Compatible with the bytecode VM.

### runtime/

The runtime package provides the public Go embedding API:

```
runtime/
├── engine.go    # Engine struct and options
├── eval.go      # Evaluation helpers
├── repl.go      # Read-Eval-Print Loop
├── watch.go     # Hot-reload file watching
├── stats.go     # Runtime statistics
└── plugin.go    # Plugin loading
```

#### Engine

The main entry point for embedding:

```go
eng, err := runtime.New(log)
defer eng.Close()

// Plugins are loaded after construction with Use.
if err := eng.Use(stdlib.New()); err != nil {
    return err
}

// Eval(ctx, source, input): source labels the run for logs/stats, input is code.
result, err := eng.Eval(ctx, "main.lisp", "(+ 1 2)")
```

#### Options

- `WithMaxEvalDepth(n)` — Cap evaluation call depth
- `WithTimeout(d)` — Per-eval timeout applied to `Eval` and `Call`
- `WithBytecode()` — Enable the bytecode VM
- `WithDialect(d)` — Select a custom dialect; the default is the Common Lisp
  dialect (`cl.Dialect()`). Select the Clojure-style surface with
  `WithDialect(clojure.Dialect())`.
### plugins/

Domain-specific plugins extend functionality. Each plugin:

- Lives in its own package
- May have external dependencies
- Registers functions in a namespace

The pure-computation plugins (`stdlib`, `data`) are the actively developed
surface; the world-touching plugins are frozen — security and correctness
fixes only (see `docs/adr/0004-kernel-first-mission.md`).

```
plugins/
├── stdlib/    # Standard library (pure Lisp + Go builtins)
├── data/      # Data structures (JSON)
├── fsm/       # Finite state machines (pure, idle)
├── llm/       # LLM API bindings (frozen)
├── agent/     # Agent orchestration (frozen)
├── lio/       # File I/O and environment (frozen)
├── net/       # HTTP client (frozen)
└── exec/      # Shell execution + crypto (frozen)
```

## Data Flow

### Evaluation Flow

```
Source Code
    │
    ▼
┌─────────┐
│ Reader  │ → Tokenize → Parse
└─────────┘
    │
    ▼
   AST
    │
    ├─────────────────────┐
    │                     │
    ▼                     ▼
┌─────────┐         ┌───────────┐
│  Eval   │         │ Compiler  │
│(tree)   │         │           │
└─────────┘         └───────────┘
    │                     │
    │                     ▼
    │               ┌───────────┐
    │               │  Bytecode │
    │               └───────────┘
    │                     │
    │                     ▼
    │               ┌───────────┐
    │               │    VM     │
    │               └───────────┘
    │                     │
    └─────────┬───────────┘
              │
              ▼
           Result
```

### Plugin Loading Flow

```
runtime.New()
    │
    ▼
For each plugin:
    │
    ├─► plugin.Init(env)
    │       │
    │       └─► Register functions in env
    │
    ▼
Engine ready
```

## Key Design Decisions

### 1. Zero Dependencies in Core

The `core/` package imports only Go's standard library. This ensures:

- Maximum portability
- Minimal attack surface
- Easy vendoring
- Fast compilation

### 2. Immutable Data Structures

Lists, vectors, and hash maps are immutable. Benefits:

- Thread-safe by default
- Predictable evaluation
- Easy reasoning about code

### 3. Dual Evaluation Modes

Both tree-walking and bytecode execution are supported:

- Tree-walking: Simple, fast startup, good for REPL
- Bytecode: Optimized for repeated execution

### 4. Plugin Isolation

Each plugin:

- Has its own namespace
- Can be optionally loaded
- May have its own dependencies

This allows applications to include only needed functionality.

### 5. Tail-Call Optimization

Recursive calls in tail position don't grow the stack:

```lisp
(defn factorial [n acc]
  (if (<= n 1)
    acc
    (recur (- n 1) (* n acc))))

(factorial 100000 1)  ; Won't overflow stack
```

## Adding a New Plugin

1. **Create package** in `plugins/`:

```bash
mkdir plugins/myplugin
```

2. **Implement Plugin interface**:

```go
// plugins/myplugin/plugin.go
package myplugin

import (
    "context"

    "github.com/victorzhuk/go-lispico/core"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "myplugin" }

func (p *Plugin) Init(env *core.Env) error {
    env.Set("myplugin/hello", core.GoFunc{
        Name: "myplugin/hello",
        Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
            return core.String{V: "Hello from myplugin!"}, nil
        },
    })
    return nil
}

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "My custom plugin",
        Author:      "you",
    }
}
```

3. **Use in application**:

```go
import "github.com/victorzhuk/go-lispico/plugins/myplugin"

eng, _ := runtime.New(log)
_ = eng.Use(myplugin.New())
```

## Thread Safety

- **Environments**: Protected by RWMutex
- **Values**: Immutable after creation
- **VM**: Each execution has isolated stack

Multiple goroutines can safely evaluate code in the same engine, as long as they don't mutate shared state.

## Error Handling

All errors are returned, never panicked:

```go
result, err := eng.Eval(ctx, "repl", "(invalid")
if err != nil {
    // handle read error
}
```

Failures are reported as `*core.LispicoError` with a `Code` identifying the
error class — `ReadError`, `EvalError`, `TypeError`, `ArityError`,
`UndefinedError` — plus source location (`Source`, `Line`, `Col`) when the
error can be tied to a position in the input. `Unwrap` exposes the cause for
`errors.Is`/`errors.As`.
