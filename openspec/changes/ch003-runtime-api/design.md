# Design Document: Runtime API

**Change ID:** 003-runtime-api  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Engine Architecture

### Engine Structure

```go
package runtime

import (
    "context"
    "fmt"
    "io"
    "log/slog"
    "os"
    "path/filepath"
    "sort"
    "sync"
    "time"
    
    "github.com/victorzhuk/go-lispico/core"
)

type Engine struct {
    mu            sync.RWMutex
    rootEnv       *core.Env
    registry      *core.Registry
    evaluator     *core.Evaluator
    logger        *slog.Logger
    config        engineConfig
    
    // Hot reload
    watcher       *fileWatcher
    watchCtx      context.Context
    watchCancel   context.CancelFunc
    
    // Stats
    stats         Stats
    evalCallbacks []func(EvalEvent)
    pluginCallbacks []func(PluginCallEvent)
}

type engineConfig struct {
    maxEvalDepth int
    timeout      time.Duration
    hotReloadDir string
    bytecode     bool   // enable bytecode VM (ch008); default: tree-walker
    cacheDir     string // bytecode cache directory; empty = no disk cache
}

type EngineOption func(*engineConfig)

func WithMaxEvalDepth(depth int) EngineOption {
    return func(c *engineConfig) {
        c.maxEvalDepth = depth
    }
}

func WithTimeout(timeout time.Duration) EngineOption {
    return func(c *engineConfig) {
        c.timeout = timeout
    }
}

// WithBytecode enables the bytecode VM backend (ch008). Default: tree-walker.
func WithBytecode() EngineOption {
    return func(cfg *engineConfig) { cfg.bytecode = true }
}

// WithBytecodeCache sets the directory for compiled bytecode cache.
// Cache key = SHA256(content); stale entries are never served.
func WithBytecodeCache(dir string) EngineOption {
    return func(cfg *engineConfig) { cfg.cacheDir = dir }
}

// TODO: Capability enforcement is deferred to a future change (ch-capabilities).

```

### Engine Construction

```go
func New(log *slog.Logger, opts ...EngineOption) (*Engine, error) {
    cfg := engineConfig{
        maxEvalDepth: 1000,
        timeout:      30 * time.Second,
    }

    for _, opt := range opts {
        opt(&cfg)
    }

    logger := log
    if logger == nil {
        logger = slog.New(slog.NewTextHandler(io.Discard, nil))
    }

    rootEnv := core.NewEnv(nil)
    evaluator := core.NewEvaluator()
    evaluator.MaxDepth = cfg.maxEvalDepth

    e := &Engine{
        rootEnv:   rootEnv,
        registry:  core.NewRegistry(),
        evaluator: evaluator,
        logger:    logger,
        config:    cfg,
        stats: Stats{
            startTime: time.Now(),
        },
    }

    logger.Info("engine created",
        "maxEvalDepth", cfg.maxEvalDepth,
        "timeout", cfg.timeout)

    return e, nil
}

// stopWatcher cancels the watcher context without acquiring e.mu.
// Safe to call from Close() since cancel funcs are atomic.
func (e *Engine) stopWatcher() {
    if e.watcher != nil {
        e.watcher.Stop()
    }
}

func (e *Engine) Close() error {
    // Cancel watcher outside the lock — cancel func is atomic.
    e.stopWatcher()

    e.mu.Lock()
    defer e.mu.Unlock()

    e.watcher = nil

    e.logger.Info("engine closed")
    return nil
}
```

---

## 2. Plugin Management

```go
func (e *Engine) Use(plugin core.Plugin) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    // Register with core registry
    if err := e.registry.Register(plugin); err != nil {
        return fmt.Errorf("load plugin: %w", err)
    }
    
    // Initialize plugin
    if err := plugin.Init(e.rootEnv); err != nil {
        e.registry.Unregister(plugin.Name())
        return fmt.Errorf("init plugin %s: %w", plugin.Name(), err)
    }
    
    e.logger.Info("plugin loaded",
        "name", plugin.Name(),
        "version", meta.Version)
    
    e.stats.ActivePlugins++
    return nil
}

func (e *Engine) UnloadPlugin(name string) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    plugin, ok := e.registry.Get(name)
    if !ok {
        return fmt.Errorf("plugin not found: %s", name)
    }
    
    // Note: Plugin interface doesn't have Shutdown(), 
    // could be added for cleanup
    
    e.registry.Unregister(name)
    
    e.logger.Info("plugin unloaded", "name", name)
    e.stats.ActivePlugins--
    return nil
}

// ReloadPlugin atomically replaces a plugin. Holds lock for the entire operation.
func (e *Engine) ReloadPlugin(plugin core.Plugin) error {
    e.mu.Lock()
    defer e.mu.Unlock()

    name := plugin.Name()

    // Unregister old version while holding the lock
    e.registry.Unregister(name)

    // Register new version
    if err := e.registry.Register(plugin); err != nil {
        return fmt.Errorf("reload plugin register: %w", err)
    }

    // Re-initialize
    if err := plugin.Init(e.rootEnv); err != nil {
        e.registry.Unregister(name)
        return fmt.Errorf("reload plugin init %s: %w", name, err)
    }

    meta := plugin.Metadata()
    e.logger.Info("plugin reloaded", "name", name, "version", meta.Version)
    return nil
}

func (e *Engine) ListPlugins() []PluginStatus {
    e.mu.RLock()
    defer e.mu.RUnlock()
    
    var statuses []PluginStatus
    for _, name := range e.registry.Namespaces() {
        if plugin, ok := e.registry.Get(name); ok {
            meta := plugin.Metadata()
            statuses = append(statuses, PluginStatus{
                Name:     name,
                Version:  meta.Version,
                Status:   "active",
                LoadedAt: time.Now(),
            })
        }
    }
    
    sort.Slice(statuses, func(i, j int) bool {
        return statuses[i].Name < statuses[j].Name
    })
    
    return statuses
}

type PluginStatus struct {
    Name     string
    Version  string
    Status   string
    LoadedAt time.Time
}
```

---

## 3. Evaluation API

```go
// Eval evaluates all top-level forms in input, returning the last result.
// ctx is required for cancellation — hot-reload loops and long computations can be interrupted.
func (e *Engine) Eval(ctx context.Context, source, input string) (core.Value, error) {
    start := time.Now()

    // Tokenize
    reader := core.NewReader(input)
    tokens, err := reader.Tokenize()
    if err != nil {
        e.recordEval(source, start, err)
        return nil, fmt.Errorf("tokenize: %w", err)
    }

    // Parse and evaluate all forms
    parser := core.NewParser(tokens)
    var result core.Value = core.Nil{}

    for {
        form, err := parser.Parse()
        if err != nil {
            // EOF is expected end condition — check via error message
            if err.Error() == "unexpected EOF" {
                break
            }
            e.recordEval(source, start, err)
            return nil, fmt.Errorf("parse: %w", err)
        }
        if form == nil {
            break
        }

        result, err = e.evaluator.Eval(ctx, form, e.rootEnv)
        if err != nil {
            e.recordEval(source, start, err)
            return nil, fmt.Errorf("eval: %w", err)
        }
    }

    e.recordEval(source, start, nil)
    return result, nil
}

func (e *Engine) EvalFile(path string) (core.Value, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read file %s: %w", path, err)
    }
    
    e.logger.Info("loading file", "path", path)

    result, err := e.Eval(context.Background(), path, string(data))
    if err != nil {
        e.logger.Error("failed to load file",
            "path", path,
            "error", err)
        return nil, err
    }
    
    e.logger.Info("file loaded", "path", path)
    return result, nil
}

func (e *Engine) LoadDir(dir string) error {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return fmt.Errorf("read dir %s: %w", dir, err)
    }
    
    // Filter .lisp files and sort
    var files []string
    for _, entry := range entries {
        if !entry.IsDir() && filepath.Ext(entry.Name()) == ".lisp" {
            files = append(files, entry.Name())
        }
    }
    sort.Strings(files)
    
    // Load in order
    for _, file := range files {
        path := filepath.Join(dir, file)
        if _, err := e.EvalFile(path); err != nil {
            return fmt.Errorf("load %s: %w", file, err)
        }
    }
    
    return nil
}

func (e *Engine) Call(ctx context.Context, name string, args ...core.Value) (core.Value, error) {
    // Create timeout context if configured
    if e.config.timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, e.config.timeout)
        defer cancel()
    }
    
    // Look up function
    fn, ok := e.rootEnv.Get(name)
    if !ok {
        return nil, fmt.Errorf("undefined function: %s", name)
    }
    
    // Check if context cancelled before execution
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    
    start := time.Now()
    
    // Apply function — context cancellation is handled inside Apply via ctx
    result, err := e.evaluator.Apply(ctx, fn, args, e.rootEnv)
    e.recordPluginCall(name, start)
    return result, err
}

func (e *Engine) Bind(name string, v core.Value) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    // Check if name conflicts with plugin namespace
    if e.registry.HasPrefix(name) {
        return fmt.Errorf("cannot bind %s: conflicts with plugin namespace", name)
    }
    
    e.rootEnv.Set(name, v)
    return nil
}

func (e *Engine) EvalWithBindings(source string, bindings map[string]core.Value) (core.Value, error) {
    // Create isolated child environment
    child := e.rootEnv.Child()
    
    for name, val := range bindings {
        child.Set(name, val)
    }
    
    reader := core.NewReader(source)
    tokens, err := reader.Tokenize()
    if err != nil {
        return nil, err
    }
    
    parser := core.NewParser(tokens)
    form, err := parser.Parse()
    if err != nil {
        return nil, err
    }
    
    return e.evaluator.Eval(form, child)
}
```

---

## 4. Hot Reload

### File Watcher

```go
type fileWatcher struct {
    engine    *Engine
    dir       string
    interval  time.Duration
    files     map[string]time.Time
    ctx       context.Context
    cancel    context.CancelFunc
}

func newFileWatcher(engine *Engine, dir string, interval time.Duration) *fileWatcher {
    return &fileWatcher{
        engine:   engine,
        dir:      dir,
        interval: interval,
        files:    make(map[string]time.Time),
    }
}

func (w *fileWatcher) Start(ctx context.Context) {
    w.ctx, w.cancel = context.WithCancel(ctx)
    
    go w.watchLoop()
}

func (w *fileWatcher) Stop() {
    if w.cancel != nil {
        w.cancel()
    }
}

func (w *fileWatcher) watchLoop() {
    ticker := time.NewTicker(w.interval)
    defer ticker.Stop()
    
    // Initial scan
    w.scan()
    
    for {
        select {
        case <-ticker.C:
            w.scan()
        case <-w.ctx.Done():
            w.engine.logger.Info("file watcher stopped")
            return
        }
    }
}

func (w *fileWatcher) scan() {
    entries, err := os.ReadDir(w.dir)
    if err != nil {
        w.engine.logger.Error("failed to scan directory",
            "dir", w.dir,
            "error", err)
        return
    }
    
    for _, entry := range entries {
        if entry.IsDir() || filepath.Ext(entry.Name()) != ".lisp" {
            continue
        }
        
        info, err := entry.Info()
        if err != nil {
            continue
        }
        
        path := filepath.Join(w.dir, entry.Name())
        mtime := info.ModTime()
        
        // Check if modified
        if lastMod, ok := w.files[path]; !ok || mtime.After(lastMod) {
            w.files[path] = mtime
            w.reloadFile(path)
        }
    }
}

func (w *fileWatcher) reloadFile(path string) {
    start := time.Now()
    w.engine.logger.Info("reloading file", "path", path)
    
    // Read file
    data, err := os.ReadFile(path)
    if err != nil {
        w.engine.logger.Error("failed to read file",
            "path", path,
            "error", err)
        return
    }

    // Hot-reload execution path (selected by engine config):
    //
    //   ii.  If bytecode VM enabled (WithBytecode):
    //        a. Compute cache key = sha256(content)
    //        b. Cache hit  → run cached chunks directly (~0.5ms)
    //        c. Cache miss → MacroExpand → compile AST → chunks,
    //                        store to disk async, run
    //  iii.  If tree-walker (default): evaluate directly (path below)

    // Try to parse first (catch syntax errors)
    reader := core.NewReader(string(data))
    tokens, err := reader.Tokenize()
    if err != nil {
        w.engine.logger.Error("syntax error in file",
            "path", path,
            "error", err)
        return
    }
    
    parser := core.NewParser(tokens)

    // Evaluate in child env for error isolation; merge to root on success
    child := w.engine.rootEnv.Child()
    ctx := context.Background()

    for {
        form, parseErr := parser.Parse()
        if parseErr != nil {
            if parseErr.Error() == "unexpected EOF" {
                break
            }
            w.engine.logger.Error("parse error in file",
                "path", path,
                "error", parseErr)
            return
        }
        if form == nil {
            break
        }

        if _, evalErr := w.engine.evaluator.Eval(ctx, form, child); evalErr != nil {
            w.engine.logger.Error("eval error in file",
                "path", path,
                "error", evalErr)
            return
        }
    }

    // Merge child bindings into root env atomically
    child.MergeInto(w.engine.rootEnv)
    
    duration := time.Since(start)
    w.engine.logger.Info("file reloaded",
        "path", path,
        "duration_ms", duration.Milliseconds())
}
```

### Engine Methods

```go
func (e *Engine) Watch(ctx context.Context, dir string) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    if e.watcher != nil {
        return fmt.Errorf("already watching")
    }
    
    e.watcher = newFileWatcher(e, dir, time.Second)
    e.watcher.Start(ctx)
    
    e.logger.Info("started watching directory", "dir", dir)
    return nil
}

func (e *Engine) Stop() error {
    e.stopWatcher()

    e.mu.Lock()
    defer e.mu.Unlock()

    e.watcher = nil
    return nil
}
```

> **Note:** `Env.MergeInto(target *Env)` must be added to the core `Env` type. It copies all bindings from the receiver into target under a write lock.

---

## 5. REPL

```go
func (e *Engine) REPL(reader io.Reader, writer io.Writer) error {
    bufReader := bufio.NewReader(reader)
    
    fmt.Fprintln(writer, "go-lispico REPL")
    fmt.Fprintln(writer, "Type (exit) or Ctrl+D to exit")
    fmt.Fprintln(writer)
    
    for {
        // Print prompt
        fmt.Fprint(writer, "lispico> ")
        
        // Read input
        line, err := bufReader.ReadString('\n')
        if err != nil {
            if err == io.EOF {
                fmt.Fprintln(writer)
                return nil
            }
            return err
        }
        
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        
        // Check for special commands
        if line == "(exit)" || line == ",quit" {
            return nil
        }
        
        // Handle multi-line input
        input := line
        for !isBalanced(input) {
            fmt.Fprint(writer, "       ")
            more, err := bufReader.ReadString('\n')
            if err != nil {
                return err
            }
            input += "\n" + more
        }
        
        // Evaluate
        result, err := e.Eval(context.Background(), "repl", input)
        if err != nil {
            fmt.Fprintf(writer, "Error: %v\n", err)
            continue
        }
        
        // Print result
        fmt.Fprintln(writer, result.String())
    }
}

func isBalanced(input string) bool {
    depth := 0
    inString := false
    escape := false
    
    for _, ch := range input {
        if escape {
            escape = false
            continue
        }
        
        if ch == '\\' {
            escape = true
            continue
        }
        
        if ch == '"' {
            inString = !inString
            continue
        }
        
        if inString {
            continue
        }
        
        switch ch {
        case '(', '[', '{':
            depth++
        case ')', ']', '}':
            depth--
            if depth < 0 {
                return false // Unbalanced
            }
        }
    }
    
    return depth == 0
}
```

---

## 6. Observability

### Stats

```go
type Stats struct {
    mu              sync.RWMutex
    TotalEvals      int64
    TotalErrors     int64
    PluginCallCounts map[string]int64
    TotalEvalNs     int64
    ActivePlugins   int
    startTime       time.Time
}

func (s *Stats) recordEval(duration time.Duration, err error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    s.TotalEvals++
    s.TotalEvalNs += duration.Nanoseconds()
    if err != nil {
        s.TotalErrors++
    }
}

func (s *Stats) recordPluginCall(name string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    if s.PluginCallCounts == nil {
        s.PluginCallCounts = make(map[string]int64)
    }
    s.PluginCallCounts[name]++
}

func (s *Stats) AvgEvalNs() int64 {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    if s.TotalEvals == 0 {
        return 0
    }
    return s.TotalEvalNs / s.TotalEvals
}

func (s *Stats) Uptime() time.Duration {
    return time.Since(s.startTime)
}

type EngineStats struct {
    TotalEvals       int64
    TotalErrors      int64
    PluginCallCounts map[string]int64
    AvgEvalNs        int64
    ActivePlugins    int
    Uptime           time.Duration
}

func (e *Engine) Stats() EngineStats {
    e.stats.mu.RLock()
    defer e.stats.mu.RUnlock()
    
    return EngineStats{
        TotalEvals:       e.stats.TotalEvals,
        TotalErrors:      e.stats.TotalErrors,
        PluginCallCounts: copyMap(e.stats.PluginCallCounts),
        AvgEvalNs:        e.stats.AvgEvalNs(),
        ActivePlugins:    e.stats.ActivePlugins,
        Uptime:           e.stats.Uptime(),
    }
}

func copyMap(m map[string]int64) map[string]int64 {
    if m == nil {
        return nil
    }
    result := make(map[string]int64, len(m))
    for k, v := range m {
        result[k] = v
    }
    return result
}
```

### Events

```go
type EvalEvent struct {
    Source   string
    Duration time.Duration
    Error    error
}

type PluginCallEvent struct {
    Plugin   string
    Function string
    Duration time.Duration
}

func (e *Engine) OnEval(fn func(EvalEvent)) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.evalCallbacks = append(e.evalCallbacks, fn)
}

func (e *Engine) OnPluginCall(fn func(PluginCallEvent)) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.pluginCallbacks = append(e.pluginCallbacks, fn)
}

func (e *Engine) recordEval(source string, start time.Time, err error) {
    duration := time.Since(start)
    
    e.stats.recordEval(duration, err)
    
    event := EvalEvent{
        Source:   source,
        Duration: duration,
        Error:    err,
    }
    
    for _, cb := range e.evalCallbacks {
        cb(event)
    }
}

func (e *Engine) recordPluginCall(name string, start time.Time) {
    duration := time.Since(start)
    
    e.stats.recordPluginCall(name)
    
    event := PluginCallEvent{
        Function: name,
        Duration: duration,
    }
    
    for _, cb := range e.pluginCallbacks {
        cb(event)
    }
}
```

---

## 7. Error Handling

### LispicoError

All interpreter errors are returned as `*LispicoError`. Callers can type-assert to inspect kind and location.

```go
type ErrorKind string

const (
    ErrSyntax      ErrorKind = "SyntaxError"
    ErrEval        ErrorKind = "EvalError"
    ErrType        ErrorKind = "TypeError"
    ErrArity       ErrorKind = "ArityError"
    ErrUndefined   ErrorKind = "UndefinedError"
    ErrCapability  ErrorKind = "CapabilityError"
)

type LispicoError struct {
    Kind    ErrorKind
    Message string
    Line    int
    Col     int
    Source  string
}

func (e *LispicoError) Error() string {
    if e.Line > 0 {
        return fmt.Sprintf("%s at %s:%d:%d: %s", e.Kind, e.Source, e.Line, e.Col, e.Message)
    }
    return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}
```

Callers pattern-match on `ErrorKind` to distinguish syntax errors from runtime errors:

```go
result, err := interp.Eval(ctx, "repl", input)
var lisErr *LispicoError
if errors.As(err, &lisErr) {
    switch lisErr.Kind {
    case ErrSyntax:
        fmt.Fprintf(w, "syntax error at line %d: %s\n", lisErr.Line, lisErr.Message)
    case ErrUndefined:
        fmt.Fprintf(w, "undefined: %s\n", lisErr.Message)
    default:
        fmt.Fprintf(w, "%v\n", lisErr)
    }
}
```

---

## 8. File Organization

```
runtime/
├── engine.go         # Main Engine implementation
├── plugin.go         # Plugin management
├── eval.go           # Evaluation API
├── watch.go          # Hot reload watcher
├── repl.go           # REPL implementation
├── stats.go          # Observability
└── runtime_test.go   # Test suite
```

---

---

## 9. Performance Requirements

| Operation                  | Target  | Notes                      |
|----------------------------|---------|----------------------------|
| Eval (simple expression)   | < 1ms   | Single form, no I/O        |
| Hot reload (tree-walker)   | < 100ms | Parse + eval all forms     |
| Hot reload (cached, VM)    | < 1ms   | Bytecode cache hit (ch008) |
| Hot reload (miss, VM)      | < 60ms  | MacroExpand + compile + run|
| Plugin load                | < 10ms  | Init + env bind            |

---

## 10. Dependencies

### Internal Dependencies

- **Change 1** (core-engine): `core.Env`, `core.Evaluator`, `core.Value`, `core.Registry`, `core.Plugin`
- **Change 8** (bytecode-vm): Optional — activated when `WithBytecode()` is used; provides `vm.Compiler` and `vm.Chunk`

### External Dependencies

- `log/slog` (stdlib)
- `sync`, `os`, `context`, `time` (stdlib)

---

**Next Step:** Create tasks document (03-tasks.md) with implementation phases and acceptance criteria.
