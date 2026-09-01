# go-lispico

A zero-dependency, pluggable Lisp interpreter designed as an embeddable scripting kernel for Go applications.

## Features

- **Zero dependencies** in core package (stdlib only)
- **13 built-in types**: Nil, Bool, Int, Float, String, Symbol, Keyword, List, Vector, HashMap, GoFunc, Lambda, Macro
- **21 special forms**: if, def, defn, defmacro, fn, let, let*, do, quote, quasiquote, set!, when, cond, loop, recur, try, catch, throw, and, or, not

  The names above are the kernel special-form names. Under the default
  CL dialect they are renamed: `do`→`progn`, `set!`→`setq`, etc.

- **Bytecode VM** (default) — compiled execution with per-form tree-walker fallback
- **Tree-walking evaluator** with `loop`/`recur` tail-call optimization for fallback path
- **Hot-reload** via `eng.Watch(ctx, dir)`
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
  value cells, treats `nil` and `false` as falsy, disables bracket literals
  (`[...]`) in source, and renames many special forms and builtins for CL
  familiarity (`defun`→`defn`, `setq`→`set!`, `progn`→`do`,
  `car`→`first`, etc.).
- `clojure.Dialect()` — Clojure / Lisp-1 identity dialect. Single namespace,
  bracket literals enabled. Compatible with the bytecode VM.

The default engine uses `cl.Dialect()`. Pass `WithDialect(clojure.Dialect())`
to opt in to the Clojure surface.

### Common Lisp collections

The CL dialect adapts `nth`, `mapcar`, and `sort` to their Common Lisp
argument shapes. The `car`-family aliases (`first`, `rest`, ...) come from
the standard vocabulary renaming; the collection adapters are registered
under fixed semantic IDs `cl/nth@1`, `cl/mapcar@1`, and `cl/sort@1`.

`nth` takes the index first and the sequence second. Indexing beyond the end
of a list, or into `nil`, yields `nil` rather than an error:

```lisp
(nth 1 '(10 20 30))
```

evaluates to `20`; `(nth 5 '(1 2))` and `(nth 0 nil)` both evaluate to
`nil`. A wrong argument count fails with `ArityError`, a negative index or
an unknown option with `EvalError`, and a non-integer index or a sequence
that is neither list nor `nil` with `TypeError`.

`mapcar` accepts one function followed by any number of sequences. The
shortest sequence terminates the traversal, so the result has that length:

```lisp
(mapcar #'+ '(1 2 3) '(10 20))
```

evaluates to the list `(11 22)`.

`sort` returns a new sorted sequence of the same type as its input — list in,
list out — and leaves the input untouched. This deviates deliberately from
the Common Lisp standard, where `sort` is permitted to destroy its argument;
Lispico values are immutable, so the result is a fresh sequence:

```lisp
(sort '(3 1 2) #'<)
```

evaluates to `(1 2 3)`. The predicate is a two-argument function applied to
pairs; its truthiness (any non-`nil` value, including keywords) decides
order, and the sort is stable: equivalent elements keep their input order.
Key functions run exactly once per element, in original order, before any
comparison. A `:key` option projects each element before comparison:

```lisp
(sort '("bb" "a" "ccc") #'< :key #'length)
```

evaluates to `("a" "bb" "ccc")`. A keyword other than `:key`, or a repeated
`:key`, fails with `EvalError`; a leftover option without a value fails with
`ArityError`. Callback errors stop the traversal immediately: the first
error propagates unchanged and no callback or predicate call follows it.
Terminal errors — resource limits and deadlines — keep their terminal
precedence through the adapter and abort the enclosing evaluation.


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
- `-bytecode` — bytecode VM execution (already the default; explicit opt-in)
- `-tree-walker` — tree-walk-only execution (rollback path)

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

## Plugins

go-lispico is a language kernel first: the embedding host is expected to
register its own IO primitives, so the pure-computation plugins (`stdlib`,
`json`) are the actively developed surface. The world-touching plugins are
**frozen** — security and correctness fixes only
(see `docs/adr/0004-kernel-first-mission.md`).

| Plugin   | Status | Description                                                      |
| -------- | ------ | ---------------------------------------------------------------- |
| `stdlib` | active | Standard library (arithmetic, comparison, collections, strings) |
| `json`   | active | JSON encode/decode (`plugins/json`)                              |
| `fsm`    | idle   | Finite state machines (pure, no consumer)                        |
| `llm`    | frozen | LLM API bindings (OpenAI, etc.)                                  |
| `agent`  | frozen | Agent orchestration                                              |
| `lio`    | frozen | File I/O and environment                                         |
| `net`    | frozen | HTTP client                                                      |
| `exec`   | frozen | Shell execution and crypto                                       |

### Map lookup

The `stdlib` plugin provides `get` and `get-in` for reading values out of
maps. Both take an optional trailing default. The examples below run on the
default engine, whose CL dialect disables bracket literals, so maps and key
paths are built with `hash-map`, `list`, and `vector`.

`get` takes a map and a key:

```lisp
(get (hash-map :a 1 :b 2) :a)
```

evaluates to `1`. Lookup is map-only, with `nil` read as an empty map:
`(get nil :a)` evaluates to `nil` and `(get nil :a 0)` to `0`. Lists,
vectors, and strings are not lookup subjects — `(get (vector 10 20) 0)` and
`(get "abc" 0)` both fail with `TypeError`, and a wrong argument count fails
with `ArityError`.

A missing key yields `nil`, or the default when one is supplied. A key that
is present but holds `nil` is a hit, so the default is not substituted:

```lisp
(get (hash-map :a nil) :a 0)
```

evaluates to `nil`, where `(get (hash-map :a 1) :missing 0)` evaluates to
`0`. A key that cannot be hashed — a list, vector, or map used as a key —
counts as missing rather than as an error, so
`(get (hash-map :a 1) (list 1 2) 0)` also evaluates to `0`.

`get-in` walks a path of keys through nested maps. The path is a list, a
vector, or `nil`:

```lisp
(get-in (hash-map :a (hash-map :b 1)) (list :a :b))
```

evaluates to `1`. An empty path — `nil` or an empty sequence — returns the
subject unchanged and never consults the default, so
`(get-in (hash-map :a 1) nil 99)` evaluates to `{:a 1}`.

A miss short-circuits the rest of the path: an absent key, or a `nil` with
keys still to walk, makes the whole lookup missing. So
`(get-in (hash-map :a nil) (list :a :b) 0)` evaluates to `0`, while
`(get-in (hash-map :a nil) (list :a) 0)` evaluates to `nil` — a `nil` at the
final key is a successful lookup, as it is for `get`.

Errors are never replaced by the default. A non-map value with keys still to
walk fails with `TypeError`, so `(get-in (hash-map :a 1) (list :a :b) 0)`
fails rather than yielding `0`. A path that is neither list, vector, nor
`nil` fails with `TypeError`, and a wrong argument count with `ArityError`.
Both lookups report these as `*core.LispicoError` carrying those codes.

Under `clojure.Dialect()` bracket and map literals read, so the same lookup
can be written directly:

```lisp
(get-in {:a {:b 1}} [:a :b])
```

## Bytecode VM

`runtime.New()` defaults to bytecode VM execution. The VM compiles supported forms and defers unsupported forms to the tree-walking evaluator form-by-form (namely a `defmacro` nested inside a larger form, and `unquote-splicing`).

A `Vector`, `HashMap`, or list literal whose elements are all compile-time
constants is folded into a single shared chunk constant, so a function returning
literal config allocates nothing per call. The folded value is shared across
evaluations: in-language this is invisible (values are immutable and compared by
`Equals`), but a Go embedder comparing pointers will see the same instance
instead of equal fresh ones.

Evaluator control:

- `runtime.WithTreeWalker()` — force tree-walk-only execution (rollback).
- `runtime.WithBytecode()` — force VM execution.
- `runtime.New()` options are last-wins; the later evaluator option controls mode.

## Resource limits

`runtime.WithResourceLimits(runtime.ResourceLimits{…})` configures ten
resource ceilings before construction:

| Field | Default | Effect |
| --- | ---: | --- |
| `MaxReaderDepth` | 1024 | Maximum nesting depth of s-expressions |
| `MaxStructuralDepth` | 1024 | Maximum structural depth of evaluated / compiled `Vector`, `HashMap`, and quasiquote `List` literals |
| `MaxCollectionLen` | 10,000,000 | Maximum length of `range`-produced lists; may not exceed 2,147,483,647, the `Vector` length cap |
| `MaxCacheEntries` | 4096 | Maximum bytecode chunk cache entries |
| `MaxCacheBytes` | 64 MiB | Maximum retained deep bytes across bytecode chunk cache entries |
| `MaxCacheNodes` | 1,000,000 | Maximum expanded AST nodes across bytecode chunk cache entries |
| `MaxReductions` | 10,000,000 | Maximum reductions charged to one evaluation across reader bridge, macro expansion, compiler, evaluator, and `GoFunc` dispatch |
| `MaxAllocationBytes` | 64 MiB | Maximum shallow allocation bytes charged to one evaluation |
| `MaxRetainedBytesPerEnv` | 32 MiB | Maximum retained binding backing bytes per engine-owned `Env` |
| `MaxRetainedSlotsPerEnv` | 100,000 | Maximum retained binding slots per engine-owned `Env` |

A non-positive field selects the default. Ceilings are immutable after `New`.
Exceeding one returns a `*core.LispicoError` with `Code: "ResourceLimitError"`.
Structural depth is enforced at evaluation time in both evaluators so
a dead-branch over-limit literal (`(if false <deep> 1)`) is not rejected.
Reductions piggyback the existing 128-step cancellation budget in both
evaluators, and builtin logical work accrues locally via
`core.NewBuiltinWorkBudget(ctx)` with `Step()` synchronizing every 128 units
with the shared evaluation state (reductions + engine deadline + caller
cancellation); max unobserved work is 127 units. Allocation charging uses
the fixed deterministic size table in ADR 0011. Reader output is charged
before the first form runs; VM/tree-walker work and `GoFunc` re-entry share
one per-evaluation ledger. Builtin `GoFunc` results are charged at the
centralized apply site unless the callee opted out via
`ChargeGoFuncResultBytes` — zero bytes marks a wholly borrowed result (the
apply site skips its fallback shallow charge), and mixed results charge only
the fresh delta. The full builtin resource-migration path is tracked in
`openspec/changes/stdlib-builtin-resource-migration`. Charging is
incremental: a builtin whose result derives structurally from one of its own
arguments (`cons`, `conj`, `concat`, and similarly shaped ops on `List`/
`Vector`) charges only what it newly allocated, not the whole result's
shallow size on every call; a builtin that builds its result from otherwise
unrelated values charges that result's full deep size. Retained per-`Env`
binding capacity (`MaxRetainedBytesPerEnv`/`MaxRetainedSlotsPerEnv`, ADR 0012)
is a separate measure with its own ledger, not folded into
`MaxAllocationBytes`. The per-engine bytecode chunk cache obeys the entry,
deep-byte, and expanded-node ceilings. Reduction counts and charge values are
evaluator- and compiler-version-specific; only terminal behavior is compared
across engine configurations, not raw counter values.

## Error handling

Failures raised by the interpreter and by the active stdlib are
`*core.LispicoError` values carrying a `Code`: `ArityError` for a wrong
argument count, `TypeError` for a wrong argument type, `EvalError` for a
correctly typed value outside an operation's domain, and `ResourceLimitError`
for an exceeded ceiling. They arrive wrapped, so recover them with `errors.As`
rather than a type assertion:

```go
if core.IsTerminalEvalError(err) {
    return err
}
var le *core.LispicoError
if errors.As(err, &le) && le.Code == "ArityError" {
    return fmt.Errorf("malformed call: %w", err)
}
```

`core.IsTerminalEvalError` comes first because a cancelled or expired
evaluation returns a wrapped `context` error, not a `*core.LispicoError`.
Terminal failures — resource ceilings, cancellation, deadline expiry — abort
the evaluation and are not catchable by Lisp `try`/`catch`.

Codes are the contract; message wording is not, and only reader errors carry a
source position. The full code table is in
[ARCHITECTURE.md](ARCHITECTURE.md#error-handling).

## Status

**Alpha** — Core functionality complete, API subject to change.

## License

[Apache License 2.0](LICENSE)
