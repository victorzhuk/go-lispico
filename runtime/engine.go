package runtime

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

type engine struct {
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

func New(log *slog.Logger, opts ...EngineOption) (*engine, error) {
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
	eval := core.NewEvaluator()
	eval.MaxDepth = cfg.maxEvalDepth
	rootEnv.SetEvaluator(eval)
	registry := core.NewRegistry()

	log.Debug("engine created", "maxEvalDepth", cfg.maxEvalDepth, "timeout", cfg.timeout)

	return &engine{
		rootEnv:   rootEnv,
		registry:  registry,
		evaluator: eval,
		logger:    log,
		config:    cfg,
		stats:     newStats(),
	}, nil
}

func (e *engine) Close() error {
	e.stopWatcher()
	e.logger.Debug("engine closed")
	return nil
}

func (e *engine) stopWatcher() {
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

func (e *engine) RootEnv() *core.Env {
	return e.rootEnv
}

func (e *engine) Registry() *core.Registry {
	return e.registry
}

func (e *engine) Stats() EngineStats {
	return e.stats.Snapshot()
}

func (e *engine) OnEval(fn func(EvalEvent)) {
	e.mu.Lock()
	e.evalCallbacks = append(e.evalCallbacks, fn)
	e.mu.Unlock()
}

func (e *engine) OnPluginCall(fn func(PluginCallEvent)) {
	e.mu.Lock()
	e.pluginCallbacks = append(e.pluginCallbacks, fn)
	e.mu.Unlock()
}

func (e *engine) fireEvalCallbacks(event EvalEvent) {
	e.mu.RLock()
	callbacks := e.evalCallbacks
	e.mu.RUnlock()

	for _, cb := range callbacks {
		cb(event)
	}
}

func (e *engine) firePluginCallbacks(event PluginCallEvent) {
	e.mu.RLock()
	callbacks := e.pluginCallbacks
	e.mu.RUnlock()

	for _, cb := range callbacks {
		cb(event)
	}
}
