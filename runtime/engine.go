// Package runtime is the public Go embedding API for the Lispico interpreter.
package runtime

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
)

// Engine is the public API for the Lispico interpreter.
type Engine interface {
	// Eval evaluates input as Lisp source, labeling the call source for
	// stats and OnEval events.
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
	mu                sync.RWMutex
	rootEnv           *core.Env
	registry          *core.Registry
	evaluator         core.Evaluator
	treeWalker        core.Evaluator
	bytecodeEvaluator *bytecodeEvaluator
	logger            *slog.Logger
	config            engineConfig
	watcher           *fileWatcher
	watchCtx          context.Context
	watchCancel       context.CancelFunc
	stats             *Stats
	bindings          map[string]map[string]struct{} // per-plugin names to delete on unload/reload; lazy-init in Use
	evalCallbacks     []func(EvalEvent)
	pluginCallbacks   []func(PluginCallEvent)
}

type engineConfig struct {
	maxEvalDepth int
	timeout      time.Duration
	bytecode     bool
	dialect      core.Dialect
}

// EngineOption configures an Engine created by New.
type EngineOption func(*engineConfig)

// WithMaxEvalDepth sets the maximum recursion depth before evaluation aborts
// with an error. Defaults to 1000.
func WithMaxEvalDepth(depth int) EngineOption {
	return func(cfg *engineConfig) {
		cfg.maxEvalDepth = depth
	}
}

// WithTimeout sets the default timeout applied to evaluations. Defaults to
// 30 seconds.
func WithTimeout(timeout time.Duration) EngineOption {
	return func(cfg *engineConfig) {
		cfg.timeout = timeout
	}
}

// WithBytecode switches the engine to the bytecode VM evaluator instead of
// the default tree-walking one.
func WithBytecode() EngineOption {
	return func(cfg *engineConfig) {
		cfg.bytecode = true
	}
}

// WithDialect selects the Dialect the Engine runs. The Dialect is resolved once
// at New and is immutable for the Engine's lifetime. Without this option the
// Engine runs the Common Lisp dialect. Select the prior Clojure-style surface
// with WithDialect(clojure.Dialect()).
func WithDialect(d core.Dialect) EngineOption {
	return func(cfg *engineConfig) {
		cfg.dialect = d
	}
}

// New creates an Engine. log may be nil, in which case logging is discarded.
func New(log *slog.Logger, opts ...EngineOption) (Engine, error) {
	cfg := engineConfig{
		maxEvalDepth: 1000,
		timeout:      30 * time.Second,
		dialect:      cl.Dialect(),
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	rootEnv := core.NewEnv(nil)
	registry := core.NewRegistry()

	treeWalker, err := core.NewEvaluatorWithDialect(cfg.dialect)
	if err != nil {
		return nil, err
	}
	treeWalker.MaxDepth = cfg.maxEvalDepth

	var evaluator core.Evaluator
	e := &engineImpl{
		rootEnv:  rootEnv,
		registry: registry,
		logger:   log,
		config:   cfg,
		stats:    newStats(),
	}

	if cfg.bytecode {
		be := newBytecodeEvaluator(rootEnv, cfg.maxEvalDepth, treeWalker, cfg.dialect)
		rootEnv.SetEvaluator(be)
		evaluator = be
		e.bytecodeEvaluator = be
	} else {
		rootEnv.SetEvaluator(treeWalker)
		evaluator = treeWalker
	}
	e.evaluator = evaluator
	e.treeWalker = treeWalker

	log.Debug("engine created", "maxEvalDepth", cfg.maxEvalDepth, "timeout", cfg.timeout, "bytecode", cfg.bytecode)

	return e, nil
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

// applyVocabulary reconciles the root environment with the configured Dialect's
// vocabulary map. It is invoked after each plugin's Init so plugin-registered
// GoFuncs are then renamed, exposed under adapter wrappers, or stripped
// according to the Dialect's base and vocab.
//
// On a FullDialect with a vocab, the operation is purely additive: every
// registered GoFunc remains, and the vocab entries either rename a canonical
// name to a visible name or bind a visible name to a GoFunc adapter.
//
// On an EmptyDialect, the vocabulary is an allowlist. Every GoFunc whose name
// is not in the vocab is removed from the env, and the vocab entries are then
// applied. Macros, Lambdas, and any non-GoFunc values are left alone so
// bootstrap macros survive the allowlist pass. A snapshot of every GoFunc is
// taken before the strip so the apply phase can resolve renames whose
// canonical name is absent from the allowlist and would otherwise be deleted.
func (e *engineImpl) applyVocabulary() {
	vocab := e.config.dialect.Vocab()
	if vocab == nil {
		return
	}

	goFuncs := make(map[string]core.Value)
	for _, name := range e.rootEnv.VarNames() {
		v, ok := e.rootEnv.Get(name)
		if !ok {
			continue
		}
		if _, isGoFunc := v.(core.GoFunc); isGoFunc {
			goFuncs[name] = v
		}
	}

	if e.config.dialect.IsBaseEmpty() {
		for name := range goFuncs {
			if _, allowed := vocab[name]; !allowed {
				e.rootEnv.Delete(name)
			}
		}
	}

	for visibleName, entry := range vocab {
		if entry.Adapter != nil {
			e.rootEnv.Set(visibleName, entry.Adapter)
			continue
		}
		if val, ok := goFuncs[entry.Canonical]; ok {
			e.rootEnv.Set(visibleName, val)
		}
	}

	// Bridge every GoFunc to the function cell under Lisp-2 so they are
	// callable in head position. Without this, head lookup of a GoFunc (e.g.
	// `(* x x)`, `(car '...)`) returns undefined because the value cell is
	// not consulted for head resolution in Lisp-2.
	if e.config.dialect.IsLisp2() {
		for _, name := range e.rootEnv.VarNames() {
			v, ok := e.rootEnv.Get(name)
			if !ok {
				continue
			}
			if _, isGoFunc := v.(core.GoFunc); isGoFunc {
				e.rootEnv.SetFunc(name, v)
			}
		}
	}
}
