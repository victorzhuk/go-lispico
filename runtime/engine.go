package runtime

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

// Engine is the public API for the Lispico interpreter.
type Engine interface {
	// Eval evaluates source code with an optional input value.
	Eval(ctx context.Context, source, input string) (core.Value, error)
	// EvalFile evaluates a source file.
	EvalFile(path string) (core.Value, error)
	// EvalWithBindings evaluates source with additional bindings.
	EvalWithBindings(ctx context.Context, source string, bindings map[string]core.Value) (core.Value, error)
	// LoadDir loads all .lisp files from a directory.
	LoadDir(dir string) error
	// Call invokes a named function with arguments.
	Call(ctx context.Context, name string, args ...core.Value) (core.Value, error)
	// Bind binds a value to a name in the global environment.
	Bind(name string, v core.Value) error
	// Use registers and initializes a plugin.
	Use(p core.Plugin) error
	// UnloadPlugin removes a plugin by name.
	UnloadPlugin(name string) error
	// ReloadPlugin reloads a plugin.
	ReloadPlugin(p core.Plugin) error
	// ListPlugins returns status of all loaded plugins.
	ListPlugins() []PluginStatus
	// Watch starts watching a directory for file changes.
	Watch(ctx context.Context, dir string) error
	// Stop stops the watcher.
	Stop() error
	// Close releases resources.
	Close() error
	// REPL starts an interactive REPL.
	REPL(r io.Reader, w io.Writer) error
	// RootEnv returns the root environment.
	RootEnv() *core.Env
	// Registry returns the plugin registry.
	Registry() *core.Registry
	// Stats returns runtime statistics.
	Stats() EngineStats
	// OnEval registers a callback for evaluation events.
	OnEval(fn func(EvalEvent))
	// OnPluginCall registers a callback for plugin call events.
	OnPluginCall(fn func(PluginCallEvent))
}

type engineImpl struct {
	mu              sync.RWMutex
	rootEnv         *core.Env
	registry        *core.Registry
	evaluator       core.Evaluator
	logger          *slog.Logger
	config          engineConfig
	watcher         *fileWatcher
	watchCtx        context.Context
	watchCancel     context.CancelFunc
	stats           *Stats
	evalCallbacks   []func(EvalEvent)
	pluginCallbacks []func(PluginCallEvent)
}

type engineConfig struct {
	maxEvalDepth int
	timeout      time.Duration
	hotReloadDir string
	bytecode     bool
	cacheDir     string
}

type EngineOption func(*engineConfig)

func WithMaxEvalDepth(depth int) EngineOption {
	return func(cfg *engineConfig) {
		cfg.maxEvalDepth = depth
	}
}

func WithTimeout(timeout time.Duration) EngineOption {
	return func(cfg *engineConfig) {
		cfg.timeout = timeout
	}
}

func WithHotReloadDir(dir string) EngineOption {
	return func(cfg *engineConfig) {
		cfg.hotReloadDir = dir
	}
}

func WithBytecode() EngineOption {
	return func(cfg *engineConfig) {
		cfg.bytecode = true
	}
}

func WithBytecodeCache(dir string) EngineOption {
	return func(cfg *engineConfig) {
		cfg.cacheDir = dir
	}
}

func New(log *slog.Logger, opts ...EngineOption) (Engine, error) {
	cfg := engineConfig{
		maxEvalDepth: 1000,
		timeout:      30 * time.Second,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	rootEnv := core.NewEnv(nil)
	registry := core.NewRegistry()

	var evaluator core.Evaluator
	if cfg.bytecode {
		cacheDir := cfg.cacheDir
		if cacheDir == "" {
			cacheDir = "/tmp/lispico-cache"
		}
		bc, err := vm.NewBytecodeCache(cacheDir)
		if err != nil {
			return nil, err
		}
		evaluator = vm.New(rootEnv, bc,
			vm.WithCompiler(compiler.NewCompiler("<runtime>")),
			vm.WithMaxDepth(cfg.maxEvalDepth),
		)
		rootEnv.SetEvaluator(evaluator)
	} else {
		eval := core.NewEvaluator()
		eval.MaxDepth = cfg.maxEvalDepth
		rootEnv.SetEvaluator(eval)
		evaluator = eval
	}

	log.Debug("engine created", "maxEvalDepth", cfg.maxEvalDepth, "timeout", cfg.timeout, "bytecode", cfg.bytecode)

	return &engineImpl{
		rootEnv:   rootEnv,
		registry:  registry,
		evaluator: evaluator,
		logger:    log,
		config:    cfg,
		stats:     newStats(),
	}, nil
}

func (e *engineImpl) Close() error {
	e.stopWatcher()
	e.logger.Debug("engine closed")
	return nil
}

func (e *engineImpl) stopWatcher() {
	e.mu.Lock()
	watcher := e.watcher
	cancel := e.watchCancel
	e.watcher = nil
	e.watchCancel = nil
	e.watchCtx = nil
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if watcher != nil {
		watcher.Stop()
		e.logger.Info("stopped watcher")
	}
}

func (e *engineImpl) RootEnv() *core.Env {
	return e.rootEnv
}

func (e *engineImpl) Registry() *core.Registry {
	return e.registry
}

func (e *engineImpl) Stats() EngineStats {
	return e.stats.Snapshot()
}

func (e *engineImpl) OnEval(fn func(EvalEvent)) {
	e.mu.Lock()
	e.evalCallbacks = append(e.evalCallbacks, fn)
	e.mu.Unlock()
}

func (e *engineImpl) OnPluginCall(fn func(PluginCallEvent)) {
	e.mu.Lock()
	e.pluginCallbacks = append(e.pluginCallbacks, fn)
	e.mu.Unlock()
}

func (e *engineImpl) fireEvalCallbacks(event EvalEvent) {
	e.mu.RLock()
	callbacks := e.evalCallbacks
	e.mu.RUnlock()

	for _, cb := range callbacks {
		cb(event)
	}
}

func (e *engineImpl) firePluginCallbacks(event PluginCallEvent) {
	e.mu.RLock()
	callbacks := e.pluginCallbacks
	e.mu.RUnlock()

	for _, cb := range callbacks {
		cb(event)
	}
}
