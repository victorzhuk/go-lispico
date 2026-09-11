# Architecture

This document describes the architecture of go-lispico, a zero-dependency, pluggable Lisp interpreter for Go.

## Overview

go-lispico is designed as an embeddable scripting kernel with three layers:

```
┌─────────────────────────────────────────────────────────────┐
│                        CLI                                  │
│  cmd/lispico (interactive REPL binary, golang.org/x/term)   │
├─────────────────────────────────────────────────────────────┤
│                        PLUGINS                              │
│  stdlib  agent  llm  lio  net  exec  json  fsm              │
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

#### Value Representation

`List` and `Vector` are hybrid: a flat slice below a size threshold, a
persistent tree above it. Both thresholds are 32 elements, tuned
independently (`listFlatThreshold`, `vectorFlatThreshold` in `types.go`).

- **List**: at or below `listFlatThreshold` it is a flat slice — cheap random
  access, matching reader output and other small, short-lived forms. Above
  it, `Cons` switches to a shared-tail node chain: each `Cons` allocates one
  node pointing at the existing tail, so prepend is O(1) instead of O(n) and
  every list sharing that tail can alias it safely. `NewList` builds
  whichever form matches the input length up front.
- **Vector**: `NewVector` always stores flat, regardless of length — bulk
  construction (reader output, literals) never promotes, because a vector
  that's never `Conj`'d again gains nothing from sharing and would only pay
  for it. `Conj` is the only path that promotes: crossing
  `vectorFlatThreshold` splits the vector into a bit-partitioned trie
  (32-way fan-out, 5 bits of the index per level) holding the bulk of the
  elements plus a tail buffer of 0–32 pending elements not yet folded into
  the trie. Growing the trie copies only the path to the new leaf, sharing
  every other subtree.

Both types keep exactly one representation field set (flat XOR
shared/root), and there is no demotion — a `List.Rest()` or `Vector` that
drops back at or below the threshold stays in its promoted form. Sharing is
sound because:

- **Immutability**: nodes are never mutated after construction; every
  operation that grows a structure allocates new nodes and reuses old ones
  by reference, never in place.
- **No demotion**: once shared/trie, always shared/trie, so a live alias
  into an older node can never be invalidated by a later operation on a
  different value.
- **Promotion is representation-invisible**: equality (`boundedEquals`),
  ordering, printing (`boundedString`), and both evaluators all go through
  `Len()`/`At()`/`ToSlice()`/`each()`/a cursor — never through the flat or
  shared/root fields directly — so a flat and a promoted value of equal
  contents compare, print, and iterate identically. The tree-walker and the
  bytecode VM share the same `List`/`Vector` values and accessors, so
  promotion is invisible across evaluators too.

#### Environment

Environments form a chain for lexical scoping:

```go
type Env struct {
    mu     sync.RWMutex
    parent *Env
    vars   map[string]*Cell
    funcs  map[string]*Cell // function cell; nil until first SetFunc (Lisp-2 only)
    // + retained-capacity counters, macro epoch, lazy plugin layer
}
```

Each environment:

- Holds bindings for its scope, each in a `*Cell`, so a closure or a cached
  chunk keeps one binding alive instead of pinning the whole scope
- Has optional parent for lookup chain
- Carries a second, function-cell namespace used only by Lisp-2 dialects
- Tracks its own retained bytes and slots (ADR 0012), released only by
  `Rebuild()`
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

Both stages are one scan each over the source, and the guarded entry point
(`Dialect.ReadWithContextStats`) charges every scanned byte of both to the
evaluation's ledger, settling at a 128-unit checkpoint that also reads the
armed deadline and caller cancellation. Storage is admitted before it is
obtained, never after: the token plan is reserved once when counting finishes,
the parser workspace is charged for its whole new logical capacity before each
doubling, each output node is charged before the node value is built, and
collection construction — flat copy-out, linked list cells, small-map entry
buffer, persistent-map nodes — is admitted before its storage is allocated. A
numeric token pays its conversion storage before `strconv` is entered, on
success and on failure, and an invalid-number diagnostic renders at most 128
bytes of the offending source. The charge table and its terms are ADR 0011.

The context-free entry points (`core.Read`, `core.ReadOne`, `Dialect.Read`,
`Dialect.ReadWithMaxDepth`, `Dialect.ReadWithMaxDepthStats`) install no budget
and behave exactly as before: depth ceiling only.

The reader pools its scratch — token slice, node stack, parser state — across
reads. Release clears only the reference-bearing slots the finished read wrote,
in batches of at most 128 charged slots; a scratch whose retained capacity
exceeds the current read's allocation ceiling is dropped instead of pooled, and
a read that ends terminally drops its scratch rather than walking it.

#### Evaluator

1. **Bytecode VM** (`vm/`): compiled execution — the default path
2. **Tree-walking** (`eval.go`): direct AST traversal for forms that do not compile in the VM

The compiler folds a `Vector`, `HashMap`, or list literal whose elements are
(recursively) all compile-time constants into one prebuilt value in the chunk's
constant pool, loaded by a charge-carrying constant reference instead of element
pushes plus `OpMakeVector`/`OpMakeMap`/`OpMakeList`. Sharing one instance across
executions is sound for the same reason promotion is: the values are immutable
and compared structurally. Literals containing a symbol or a nested call compile
unchanged.

A form the compiler refuses — a `defmacro` nested inside a larger form, a
`def`/`defn` inside a lexical scope (a `fn` body, a `let`/`let*`/`loop` body,
a `catch` handler), or `unquote-splicing` — falls back whole: the runtime
tree-walks the entire top-level form before any of its bytecode executes, so
the form's side effects run exactly once and lexical definitions bind in their
scope. This refusal keeps the closure architecture flat: unaffected compiled
functions keep slot locals and captures, and the VM allocates no per-call
lexical environment to mirror definitions.

Tail-call optimization is explicit: `loop`/`recur` iterate without growing the Go
stack (Clojure-style). Ordinary self-recursion is not auto-optimized; it is
bounded by the configured max eval depth.

#### VM Frame Layout

Each call frame reserves its `chunk.Locals` slots as a contiguous region at
the bottom of the frame's value-stack slice (`reserveLocals`, run at frame
entry): frame-local storage lives at `[base, base+Locals)` and temporary
operands always push at or above `base+Locals`, so `OpGetLocal`/`OpSetLocal`
and operand traffic can never touch each other's slots. `computeMaxStack`
sizes the frame as `Locals + peak operand height`, so one bound covers both
regions.

The binding forms (`let`, `let*`, the `catch` binding of `try`, the
initializers of `loop`) compile each initializer, store it into its reserved
slot, and immediately `OpPop` the operand — the initializer's value is
consumed and never left standing where the enclosing form (a vector element,
a call argument or target) would pick it up as data.

`loop` bindings have a scoped lifetime. Each slot is reserved before its
initializer compiles but stays nameless and invisible to resolution until all
initializers have bound, so loop initializers evaluate against the enclosing
scope (parallel binding, matching the tree-walker, with shadowed outer names
still visible), and the bindings leave the local scope when the loop form
ends. `recur` compiles its arguments above the loop-entry operand height,
then writes each into its slot followed by `OpPop`, so every iteration
re-enters the body at the same operand height it had at loop entry.

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
| `cond`       | Multi-way conditional |
| `loop`       | Loop with recur       |
| `recur`      | Tail recursion        |
| `try`        | Exception handling    |
| `catch`      | Catch exception       |
| `throw`      | Raise exception       |
| `and`, `or`  | Short-circuit logic   |
| `not`        | Boolean negation      |

The names above are the kernel special-form names. Under the default CL dialect they are renamed: `do`→`progn`, `set!`→`setq`, etc.

### cl/

The Common Lisp dialect package. Exports `Dialect()` which returns a
non-identity composition over `core.FullDialect` with Lisp-2 name resolution,
CL reader flags, and vocabulary-renamed function names. `nil` and `false` are
falsy under every dialect.
```
cl/
└── cl.go    # Dialect() constructor
```

### clojure/

The Clojure dialect package. Exports `Dialect()` which returns the identity
dialect (`core.FullDialect`) — Lisp-1, bracket literals enabled, no
vocabulary map. Compatible with the bytecode VM.

```
clojure/
├── clojure.go      # Dialect() constructor
└── clojure_test.go # Dialect tests
```

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

// LoadScope evaluates source with bindings and returns the child scope
// so the embedder owns its lifecycle (ADR 0012).
_, scope, err := eng.LoadScope(ctx, "rule.lisp", map[string]core.Value{
    "x": core.Int{V: 42},
})
defer scope.Rebuild() // compact dead backing when done
```

#### Options

- `WithMaxEvalDepth(n)` — Cap evaluation call depth
- `WithTimeout(d)` — Per-eval timeout applied to `Eval` and `Call`
- `WithBytecode()` — Force bytecode VM execution (default)
- `WithTreeWalker()` — Force tree-walker-only execution for rollback
- `WithDialect(d)` — Select a custom dialect; the default is the Common Lisp
  dialect (`cl.Dialect()`). Select the Clojure-style surface with
  `WithDialect(clojure.Dialect())`.
- `WithEngineMeter(m)` — Bind an embedder `Meter` for every evaluation on this
  engine: reduction/allocation leases and retained-capacity charges settle
  against it. `runtime.WithMeter(ctx, m)` overrides it for a single call.
- `WithResourceLimits(l)` — Set resource ceilings (`MaxReaderDepth`,
  `MaxStructuralDepth`, `MaxCollectionLen`, `MaxCacheEntries`,
  `MaxCacheBytes`, `MaxCacheNodes`, `MaxReductions`, `MaxAllocationBytes`,
  `MaxRetainedBytesPerEnv`, `MaxRetainedSlotsPerEnv`), applied once at
  `New` and immutable
  afterward. A non-positive field selects a conservative default; there is no
  "unlimited". Exceeding a ceiling returns a `*core.LispicoError` with
  `Code: "ResourceLimitError"`.

Evaluator options are last-wins for mode selection.

### cmd/

The `cmd/lispico/` binary is the interactive REPL. It layers terminal handling
(`golang.org/x/term`) on top of `runtime.Engine` without modifying the Engine
contract. The binary owns flag parsing (`-dialect`, `-bytecode`,
`-tree-walker`), file
execution mode, and raw-mode terminal sessions with history persistence.

### plugins/

Domain-specific plugins extend functionality. Each plugin:

- Lives in its own package
- May have external dependencies
- Registers functions in a namespace

The pure-computation plugins (`stdlib`, `json`) are the actively developed
surface; the world-touching plugins are frozen — security and correctness
fixes only (see `docs/adr/0004-kernel-first-mission.md`).

```
plugins/
├── stdlib/    # Standard library (pure Lisp + Go builtins)
├── json/      # JSON encode/decode
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

### Resource Limits

Resource ceilings protect host availability against adversarial or accidental
input. The reader bounds parser nesting (`MaxReaderDepth`) at parse time, so a
deeply nested source returns a typed error instead of a fatal stack overflow.
On the runtime path the reader admits its own work and storage into the
evaluation's allocation ledger *during* the read, before each allocation, and
observes caller cancellation and the armed engine deadline at the same
checkpoints; there is no separate post-parse charge. The evaluator bounds structural descent into
`Vector`/`HashMap` literals and quasiquote (`MaxStructuralDepth`); this counter
lives on the per-evaluation `evalState` carried in `context.Context`, so it is
shared by BOTH the tree-walker and the bytecode VM and stays continuous across
evaluator callbacks (`map`/`filter`/`reduce` invoke `eval.Apply`, and VM
`GoFunc` re-entry adopts the same ledger). Enforcement is lazy — only
structure that is actually evaluated/executed is counted, so a dead-branch
over-limit literal is not rejected and the two evaluators agree. A folded
all-constant literal carries its deep bytes and structural depth computed once
at compile time, so loading it charges the ledger and checks the depth ceiling
in O(1) without re-walking the value — the charge model is identical to the
tree-walker's, only the walk is gone. `range` caps
its result length (`MaxCollectionLen`) and checks `ctx` cooperatively; the
per-engine bytecode chunk cache is bounded by entry, deep-byte, and
expanded-node ceilings (`MaxCacheEntries`, `MaxCacheBytes`, `MaxCacheNodes`) and
reclaims entries orphaned by a macro-epoch bump. `MaxReductions` counts macro
expansion, compiler emission, evaluator work, and `GoFunc` dispatch via the
existing 128-step cancellation budget, while `MaxAllocationBytes` charges the
fixed deterministic size table from ADR 0011 at reader, compiler, VM,
tree-walker, and `GoFunc` result boundaries. `MaxRetainedBytesPerEnv` and
`MaxRetainedSlotsPerEnv` cap per-environment retained binding capacity
(ADR 0012): every `Env` on the engine path carries owned counters
(bytes + slots), charged on new-slot binding writes using the same fixed
size table. A breach does not occur; the write is rejected with a
terminal `ResourceLimitError`. Dead backing is released only through
`(*Env).Rebuild()`, which compacts in place with live cell pointers
preserved. `(*Env).RetainedUsage()` probes the current counters.
Resource-limit breaches are a hard safety boundary and are not catchable
by `try`/`catch`.

### Plugin Loading Flow

```
Engine.Use(plugin) / ReloadPlugin(plugin)
    │
    ▼
read Registry.Generation
    │
    ▼
Env.BeginRegistration() → registration view
    │
    ▼
plugin.Init(view)        # reload also deletes the old plugin's names first
    │
    ├─► vocabulary application (through the view)
    │
    ▼
fence in-flight lazy materialization started through the view
    │
    ├─ error ──────────────► Registration.Abort()
    │
    ▼
settle
    │
    ├─ error ──────────────► Registration.Abort()
    │
    ▼
Registry.PublishIf(generation) → registry entry
    │
    ├─ conflict (CodeRegistryConflict) ──► Registration.Abort(), host entry kept
    │
    ▼
Registration.Complete() → ownership, lazy activation,
                           Stats().ActivePlugins all publish here
    │
    ▼
Engine ready
```

`Init` receives the registration view returned by `Env.BeginRegistration`
(`Registration.Env()`), not the root environment directly — its pointer
identity no longer equals `RootEnv()`. The view forwards every read and
write to the same root immediately, so a reader resolving through the root
sees plugin definitions as `Init` writes them, with no isolation between an
in-progress registration and ordinary evaluation; closures and retained
references keep seeing root updates after `Use`/`ReloadPlugin` succeeds. No
`Plugin` method or signature changes.

On an `Init`, vocabulary, or settlement error, or a `Registry.PublishIf`
conflict, the operation's journal runs `Registration.Abort()`: it reverts
only the writes the operation owns — value/function cells, canonical
status, per-engine lazy installed/tombstone state. A host write racing the
operation always wins over the rollback: a write to the same name between
two plugin writes, a delete, `ReplaceCell`, `Rebuild`, or an unrelated
first-touch materialization are all kept as the host left them. A registry
conflict — a host edit of the registry entry during the operation — fails
`PublishIf` with a `*core.LispicoError` of code `CodeRegistryConflict`
(`NewRegistryConflictError`); the host's registry entry stands and the
operation aborts.

The registry entry (`Registry.PublishIf`), then ownership bookkeeping, lazy
activation, and `Stats().ActivePlugins` (`Registration.Complete()`) publish
only once the whole operation succeeds; the root environment identity never
changes, and existing `Fn`/`PinnedFn` handles keep working across a reload.
A successful `UnloadPlugin` keeps its own last-writer ownership semantics —
it is not a registration abort.

The guarantee is post-return, not during: readers may observe intermediate
plugin definitions while `Init` is still running (no read isolation), and a
host lookup racing the abort itself can briefly miss a name the operation
is in the middle of deleting.

Out of rollback scope: plugin Go objects, external effects, writes through
unrelated retained environment references, and ordinary `Eval` effects.
Retained-meter charges for cells an abort removes still leak until the
follow-up change `plugin-retained-rollback` lands.

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

Failures are reported as `*core.LispicoError` carrying a `Code` that identifies
the error class. `Unwrap` exposes the cause for `errors.Is`/`errors.As`.

### Error codes

| Code | Exported constant | Reported for |
| --- | --- | --- |
| `ReadError` | — | a tokenizer or parser failure |
| `CompileError` | `compiler.CodeCompileError` | a form the bytecode compiler rejects |
| `ArityError` | — | a call with the wrong number of arguments |
| `TypeError` | — | an argument of the wrong runtime type |
| `EvalError` | — | a correctly typed argument outside the operation's domain |
| `UndefinedError` | — | a reference to an unbound symbol |
| `ThrowError` | — | an explicit `throw` that reaches the host uncaught |
| `ResourceLimitError` | `core.CodeResourceLimit` | a resource ceiling exceeded |
| `PanicError` | `core.CodePanic` | a panic recovered from an embedded `GoFunc` at a runtime boundary |
| `ConcurrentUseError` | `core.CodeConcurrentUse` | a `PinnedFn` entered concurrently or re-entered from its own execution |
| `VMStateError` | `core.CodeVMState` | a pinned call that left its private VM dirty; a full reset was applied |

Only the last four have exported constants. The rest are string literals inside
their constructors (`core.NewReadError`, `core.NewArityError`,
`core.NewTypeError`, `core.NewEvalError`, `core.NewUndefinedError`), so a host
switching on them writes the literal.

The `Code` is the contract; the `Message` is not. Message wording tracks the
operation it describes and must never be matched on.

`Error()` renders as `"<Code>: <Message>"`, or
`"<Code> at <Source>:<Line>:<Col>: <Message>"` when a position is set.
Positions come from the reader alone: `Line` and `Col` are filled in by
tokenizer and parser failures and by the reader depth ceiling. `core.Value`
carries no position, so an error raised while evaluating a form has none.

An uncaught explicit `throw` carries `ThrowError`; its message is the thrown
string verbatim, without quotes, or the `%v` rendering of any other thrown
value. Both evaluators produce this carrier identically at the host boundary.

### Classifying a stdlib failure

Every evaluation failure originated by an active stdlib Builtin or a CL adapter
is recoverable as a `*core.LispicoError`:

- Wrong argument count — `ArityError`.
- Wrong runtime type — `TypeError`.
- A correctly typed value outside the operation's domain — an out-of-range
  index, a zero divisor, malformed format syntax, incomparable operands —
  `EvalError`, unless a more specific code already governs the failure.

Errors that only pass through a Builtin keep their original type and code: a
callback error raised inside a higher-order function, a failure surfaced by the
shared evaluation-state checkpoint, and a resource-helper failure all propagate
unchanged. A terminal error is never rewritten into `EvalError`.

The tree-walking evaluator and the VM report the same `Code`, with equivalent
diagnostic meaning, for the same invalid call. Parity extends to propagation:
an ordinary runtime failure raised while executing valid bytecode — an unbound
symbol, a `set!` whose target resolves nowhere, a map-literal build, a call,
or a native operation — unwinds to the nearest active `try`/`catch` handler,
which binds what the tree-walker's handler would bind, and an unhandled
ordinary failure reaches the host as the original typed error. Compile-time
refusals are not runtime errors: a form the bytecode compiler rejects never
produces a catchable failure, because the runtime tree-walks the whole
enclosing top-level form instead.

### Recovering an error in the host

Errors reaching the embedder are wrapped, so recover the typed error with
`errors.As` rather than a type assertion:

```go
var le *core.LispicoError
if errors.As(err, &le) {
    switch le.Code {
    case "ArityError", "TypeError":
        return fmt.Errorf("malformed call: %w", err)
    case "EvalError":
        return fmt.Errorf("argument outside the operation's domain: %w", err)
    case core.CodeResourceLimit:
        return fmt.Errorf("resource ceiling exceeded: %w", err)
    }
}
```

Calls that reach each of those arms:

```go
_, arity := eng.Eval(ctx, "host", "(get (hash-map :a 1))")
_, typ := eng.Eval(ctx, "host", "(get (vector 10 20) 0)")
_, domain := eng.Eval(ctx, "host", "(nth -1 (list 1 2))")

limited, err := runtime.New(log, runtime.WithResourceLimits(
    runtime.ResourceLimits{MaxReaderDepth: 4}))
if err != nil {
    return err
}
defer limited.Close()
_, ceiling := limited.Eval(ctx, "host", "(list (list (list (list (list 1)))))")
```

`arity` carries `ArityError`, `typ` `TypeError`, `domain` `EvalError`, and
`ceiling` `ResourceLimitError`.

### Terminal errors

Resource-limit failures, context cancellation, and deadline expiry are
terminal: they abort the enclosing evaluation, and Lisp `try`/`catch` cannot
intercept them, under either evaluator. A `catch` clause recovers an ordinary
`TypeError`, while a `ResourceLimitError` reaches the host exactly as it would
have without the `try`. For an ordinary failure the in-language handler is
bound the rendered message string, not the code, so classification stays a
host-side concern; for an explicit `throw` the handler is bound the thrown
value itself, and that identity survives an unwind across an evaluator
boundary. In the VM, consumed resource charges are settled before an ordinary
error is transferred to a handler or the host; a limit breach surfaced by that
settlement is itself terminal — it outranks the pending error, bypasses every
handler, and reaches the host.

Test for terminal errors with `core.IsTerminalEvalError`, and test first: a
cancelled or expired evaluation returns a wrapped `context` error rather than a
`*core.LispicoError`, so `errors.As` alone does not see it.

```go
if core.IsTerminalEvalError(err) {
    return err
}
var le *core.LispicoError
if errors.As(err, &le) && le.Code == "ArityError" {
    return fmt.Errorf("malformed call: %w", err)
}
```

### Recovered panics

The runtime boundary recovers panics from embedded `GoFunc` values at
`Engine.Eval`, `Engine.Call`, `Fn.Call`, and `PinnedFn.Call`. Recovered panics
return `PanicError`, are recorded as failed eval/plugin calls, and cannot leave
a pooled or pinned VM in a dirty reusable state: shared bytecode VMs are reset
before returning to the pool or discarded when the panic escaped a low-level
apply.
