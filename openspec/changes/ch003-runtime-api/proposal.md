# Change Proposal: Runtime API

**Change ID:** 003-runtime-api  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the high-level embedding API and runtime services: Engine lifecycle management, file loading, hot-reload, and REPL. This is the public Go API that host applications use to integrate go-lispico.

**Key Characteristics:**
- Public Go API (`runtime` package — `github.com/victorzhuk/go-lispico/runtime`)
- Context-aware (respects Go context cancellation)
- Hot-reload with filesystem polling
- Embeddable REPL over any io.Reader/Writer
- Thread-safe for concurrent evaluation
- Structured logging with slog

---

## 2. Motivation

### Problem
The core engine (Change 1) and stdlib (Change 2) provide evaluation primitives but lack:
- High-level API for host applications
- File loading and directory scanning
- Hot-reload for development workflows
- REPL for interactive development
- Context propagation for cancellation/timeouts
- Observability hooks

### Solution
A runtime layer that bridges the low-level core with production use cases:
- **Engine**: Factory pattern with options
- **File Loading**: Directory scanning, alphabetical ordering
- **Hot Reload**: Polling watcher with error isolation
- **REPL**: Embeddable over any transport
- **Observability**: Stats, events, structured logging

### Success Metrics
- Hot-reload completes in < 50ms
- Zero dropped evaluations during reload
- Context cancellation works within 10ms
- REPL usable over TCP socket, Unix socket, stdin
- File load errors don't crash interpreter

---

## 3. Scope

### In Scope

**Engine API**
- Constructor: `New(log *slog.Logger, opts ...EngineOption)`
- Configuration: `WithMaxEvalDepth`, `WithTimeout`
- Lifecycle: `Close()` for cleanup
- Plugin management: `Use()`, `UnloadPlugin()`, `ReloadPlugin()`
- Introspection: `ListPlugins()`, `Stats()`

**Evaluation API**
- `Eval(ctx context.Context, source, input string)` - evaluate expression
- `EvalFile(path string)` - load and evaluate file
- `EvalValue(v Value)` - evaluate pre-parsed value
- `Call(ctx, name, args...)` - call named function
- `Bind(name, value)` - bind value in root env

**File Operations**
- `LoadDir(path string)` - load all .lisp files alphabetically
- `LoadFile(path string)` - load single file
- File filtering (only .lisp extension)

**Hot Reload**
- `Watch(ctx, path string)` - start filesystem watcher
- `Stop()` - stop watcher cleanly
- Polling interval: configurable (default 1s)
- mtime-based change detection
- Error isolation: syntax errors don't crash

**REPL**
- `REPL(reader io.Reader, writer io.Writer)` - embeddable REPL
- Read-eval-print loop with history
- Multi-line expression support
- Error recovery (continue after error)
- ANSI color support (optional)

**Observability**
- `Stats()` - engine statistics
- `OnEval(callback)` - evaluation events
- `OnPluginCall(callback)` - plugin call events
- Structured logging with slog

### Out of Scope (Future)

- LSP server implementation
- Debugger with breakpoints
- Profiler integration
- Memory usage tracking per evaluation
- Capability registry and enforcement → Future change (ch-capabilities)

---

## 4. Functional Requirements

### Engine Lifecycle

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.1 | Engine created with zero or more options | P0 |
| R3.2 | Options validated at creation time | P0 |
| R3.3 | Close() releases all resources, stops watchers | P0 |
| R3.4 | Multiple Engine instances are independent | P0 |

### Plugin Management

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.5 | Use() calls Init() and registers plugin namespace | P0 |
| R3.6 | UnloadPlugin calls plugin shutdown callback | P0 |
| R3.7 | ReloadPlugin is atomic (unload + load) | P0 |
| R3.8 | If reload fails, previous version remains active | P0 |
| R3.9 | ListPlugins returns metadata for all registered | P1 |

### Evaluation

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.10 | Eval parses and evaluates expression string | P0 |
| R3.11 | EvalFile uses filename for error reporting | P0 |
| R3.12 | Call respects context cancellation | P0 |
| R3.13 | EvalWithBindings creates isolated child scope | P0 |
| R3.14 | Bindings do not escape to global env | P0 |

### File Loading

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.15 | LoadDir loads files in alphabetical order | P0 |
| R3.16 | LoadDir only processes .lisp files | P0 |
| R3.17 | Load failures return descriptive errors | P0 |
| R3.18 | Syntax errors include line/column information | P0 |

### Hot Reload

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.19 | Watch polls at configurable interval (default 1s) | P0 |
| R3.20 | Change detection uses file mtime | P0 |
| R3.21 | Reload is atomic per file — either all definitions from a file are updated or none are (parse + eval must both succeed) | P0 |
| R3.22 | Syntax errors log and skip, don't crash | P0 |
| R3.23 | Successful reloads log at INFO level | P1 |
| R3.24 | Stop() cancels watcher goroutine cleanly | P0 |
| R3.25 | No goroutine leaks on Stop() | P0 |

### REPL

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.26 | Works over any io.Reader/io.Writer pair | P0 |
| R3.27 | Multi-line expressions supported | P0 |
| R3.28 | Error recovery (continue after error) | P0 |
| R3.29 | History file path configurable via `WithHistoryFile` option; default `$XDG_STATE_HOME/lispico/history` or `~/.local/state/lispico/history` | P1 |
| R3.30 | ANSI colors optional (no-color mode) | P1 |

### Observability

| ID | Requirement | Priority |
|----|-------------|----------|
| R3.31 | Stats returns: totalEvals, totalErrors, pluginCallCounts, avgEvalNs | P0 |
| R3.32 | EvalEvent contains: source, durationNs, error (if any) | P0 |
| R3.33 | PluginCallEvent contains: plugin, function, durationNs | P0 |
| R3.34 | All logs use structured logging (slog) | P0 |
| R3.35 | Log levels: DEBUG (detailed), INFO (normal), WARN (recoverable), ERROR | P0 |

---

## 5. Design Philosophy

### Context-First

All operations respect Go context for cancellation and timeouts:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

result, err := engine.Call(ctx, "my-function", args)
```

### Graceful Degradation

Hot-reload failures don't crash:
- Syntax error → log warning, keep old definitions
- File deleted → log info, definitions remain until file restored
- Watch errors → retry with backoff

### Zero Downtime

In-flight evaluations complete during reload:
- New evaluations use new definitions
- Existing evaluations finish with old definitions
- No locks held during evaluation

### Embeddable by Design

REPL works over any transport:
```go
// TCP socket
listener, _ := net.Listen("tcp", ":8080")
conn, _ := listener.Accept()
go engine.REPL(conn, conn)

// Unix socket
listener, _ := net.Listen("unix", "/tmp/lispico.sock")

// Stdin/stdout
engine.REPL(os.Stdin, os.Stdout)
```

---

## 6. API Specification

### Engine Construction

```go
// Options pattern for configuration
type EngineOption func(*engineConfig)

func WithMaxEvalDepth(depth int) EngineOption
func WithTimeout(timeout time.Duration) EngineOption
// TODO: Capability enforcement is deferred to a future change (ch-capabilities).

// Construction
func New(log *slog.Logger, opts ...EngineOption) (*Engine, error)

// Lifecycle
func (e *Engine) Close() error
```

### Plugin Management

```go
func (e *Engine) Use(p Plugin) error
func (e *Engine) UnloadPlugin(name string) error
func (e *Engine) ReloadPlugin(p Plugin) error
func (e *Engine) ListPlugins() []PluginStatus

type PluginStatus struct {
    Name     string
    Version  string
    Status   string // "active", "error"
    LoadedAt time.Time
}
```

### Evaluation

```go
// All Eval methods accept context for cancellation — hot-reload loops and long computations can be interrupted.
func (e *Engine) Eval(ctx context.Context, source, input string) (Value, error)
func (e *Engine) EvalFile(path string) (Value, error)
func (e *Engine) EvalValue(v Value) (Value, error)
func (e *Engine) Call(ctx context.Context, name string, args ...Value) (Value, error)
func (e *Engine) Bind(name string, v Value) error
```

### File Operations

```go
func (e *Engine) LoadDir(path string) error
func (e *Engine) LoadFile(path string) error
```

### Hot Reload

```go
func (e *Engine) Watch(ctx context.Context, path string) error
func (e *Engine) Stop() error
```

### REPL

```go
func (e *Engine) REPL(reader io.Reader, writer io.Writer) error
```

### Observability

```go
func (e *Engine) Stats() EngineStats
type EngineStats struct {
    TotalEvals       int64
    TotalErrors      int64
    PluginCallCounts map[string]int64
    AvgEvalNs        int64
    ActivePlugins    int
    Uptime           time.Duration
}

func (e *Engine) OnEval(fn func(EvalEvent))
func (e *Engine) OnPluginCall(fn func(PluginCallEvent))

type EvalEvent struct {
    Source    string
    Duration  time.Duration
    Error     error
}

type PluginCallEvent struct {
    Plugin   string
    Function string
    Duration time.Duration
}
```

---

## 7. Hot Reload Algorithm

```
Watch Loop (goroutine):
  1. Scan directory for *.lisp files
  2. For each file:
     a. Check mtime against cache
     b. If changed:
        i.   Read file content
        ii.  Parse (catch syntax errors)
        iii. If parse OK:
             - Evaluate in fresh child env
             - Merge successful bindings to root
             - Update mtime cache
             - Log "Reloaded: path"
        iv.  If parse FAIL:
             - Log warning with error
             - Keep old definitions
  3. Sleep for interval (default 1s)
  4. Check context cancellation
  5. Repeat
```

**Key Design Decisions**:
- **mtime, not hash**: Faster, good enough for development
- **Parse before eval**: Catch syntax errors early
- **Child env merge**: Isolate failures
- **Atomic file reload**: Parse entire file before applying any bindings. If parse succeeds but eval fails mid-file, log the error and restore the previous bindings for that file (keep a per-file snapshot of bindings before reload). This prevents partial state from a single file.

---

## 8. REPL Behavior

```
Read Phase:
  1. Print prompt: "lispico> "
  2. Read line(s) until balanced parentheses
  3. Handle special commands:
     - ",quit" or Ctrl+D → exit
     - ",help" → show commands
     - ",reset" → reset environment

Eval Phase:
  1. Parse input
  2. If parse error → print error, continue
  3. Evaluate with root environment
  4. If eval error → print error with stack trace

Print Phase:
  1. Print result using type's String() method
  2. Pretty-print for collections

Loop:
  - Continue until exit command
  - History appended to configurable path (default `$XDG_STATE_HOME/lispico/history`)
```

---

## 9. Performance Requirements

| Operation | Target | Notes |
|-----------|--------|-------|
| Engine creation | < 5ms | With core + stdlib |
| LoadDir (10 files) | < 100ms | 100 lines each |
| Hot reload (1 file) | < 50ms | 100 lines |
| Context cancellation | < 10ms | From cancel to eval stop |
| REPL startup | < 100ms | First prompt |
| Stats() call | < 1µs | Simple counter reads |

---

## 10. Dependencies

### External Dependencies

- `log/slog` - Structured logging (Go 1.21+)
- `context` - Cancellation propagation
- `io` - REPL interface

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required for useful REPL

### Dependent Changes

- **Change 4-7**: All use runtime API

---

## 11. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Hot-reload race conditions | Medium | High | Careful locking, child env isolation |
| Goroutine leaks in Watch() | Medium | High | Context cancellation tests, leak detector |
| File system edge cases | Medium | Low | Handle deleted files, permissions |
| REPL security over network | Low | High | Document security considerations |
| Context cancellation timing | Low | Medium | Test with various timeout values |

---

## 12. Acceptance Criteria

- [ ] Engine created with functional options
- [ ] Plugins loaded/unloaded/reloaded correctly
- [ ] Eval respects context cancellation
- [ ] LoadDir loads files alphabetically
- [ ] Hot-reload detects changes and updates
- [ ] Syntax errors in reload don't crash
- [ ] REPL works over stdin/stdout
- [ ] Stats and callbacks working
- [ ] Structured logging throughout
- [ ] No goroutine leaks (race detector)
- [ ] Test coverage ≥ 85%
- [ ] LispicoError type defined with Kind, Message, Line, Col, Source fields

---

**Next Step:** Create detailed design document (02-design.md) with Engine internals, Watch implementation, and REPL state machine.
