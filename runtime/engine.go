// Package runtime is the public Go embedding API for the Lispico interpreter.
package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/vm"
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
	// LoadScope evaluates source with additional bindings and returns the child scope.
	LoadScope(ctx context.Context, source string, bindings map[string]core.Value) (core.Value, *core.Env, error)
	// LoadDir loads all .lisp files from a directory.
	LoadDir(dir string) error
	// Call invokes a named function with arguments.
	Call(ctx context.Context, name string, args ...core.Value) (core.Value, error)
	// Func resolves a named function once and returns a reusable call handle.
	Func(name string) (*Fn, error)
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
	callCache         callCache
	bindings          map[string]map[string]struct{} // per-plugin names to delete on unload/reload; lazy-init in Use
	evalCallbacks     []func(EvalEvent)
	pluginCallbacks   []func(PluginCallEvent)
	callbacksActive   atomic.Bool // one-way: no unregister API, so once true it stays true
	// fastPath precomputes the lean-boundary condition: bytecode evaluator,
	// no engine meter, no callbacks. Read once per call at entry; recomputed
	// under e.mu at the only mutation sites (callback registration). The
	// bytecode evaluator and engine meter are immutable after New, so the
	// flag can only ever flip true→false.
	fastPath atomic.Bool
	// rootEnvPtr mirrors rootEnv for lock-free reads on the call hot path.
	// rootEnv is assigned exactly once in New and never swapped — Rebuild and
	// hot-reload mutate the env's contents in place, preserving its identity —
	// so the snapshot never needs refreshing after construction.
	rootEnvPtr atomic.Pointer[core.Env]
	// vmSlot is a per-engine VM claimed via vmSlotInUse (CAS) on the lean
	// path, mirroring PinnedFn's private-VM shape; losers fall back to the
	// bytecode pool. Built lazily on the first claim (under CAS ownership,
	// so the init is race-free and New pays nothing); nil on tree-walker
	// engines and until the first lean call.
	vmSlot           *vm.VM
	vmSlotInUse      atomic.Bool
	lazyMaterializer *stdlibLazyMaterializer
	vocab            *vocabShape
}

// vocabShape is the part of a Dialect's vocabulary a plugin operation needs,
// resolved once per dialect so neither New nor Use copies the vocabulary map.
type vocabShape struct {
	present  bool
	adapters []vocabAdapter
}

// vocabAdapter is one adapter binding of the dialect's vocabulary.
type vocabAdapter struct {
	name  string
	value core.Value
}

// vocabShapes caches vocabShape by dialect fingerprint; a Dialect is
// immutable, so an entry never goes stale.
var vocabShapes struct {
	mu   sync.RWMutex
	byFP map[string]*vocabShape
}

func vocabShapeOf(d core.Dialect) *vocabShape {
	fp := d.Fingerprint()
	vocabShapes.mu.RLock()
	vs, ok := vocabShapes.byFP[fp]
	vocabShapes.mu.RUnlock()
	if ok {
		return vs
	}

	vocab := d.Vocab()
	vs = &vocabShape{present: vocab != nil}
	for name, entry := range vocab {
		if entry.Adapter != nil {
			vs.adapters = append(vs.adapters, vocabAdapter{name: name, value: entry.Adapter})
		}
	}
	slices.SortFunc(vs.adapters, func(a, b vocabAdapter) int { return strings.Compare(a.name, b.name) })

	vocabShapes.mu.Lock()
	defer vocabShapes.mu.Unlock()
	if cached, ok := vocabShapes.byFP[fp]; ok {
		return cached
	}
	if vocabShapes.byFP == nil {
		vocabShapes.byFP = make(map[string]*vocabShape)
	}
	vocabShapes.byFP[fp] = vs
	return vs
}

type engineConfig struct {
	maxEvalDepth int
	timeout      time.Duration
	bytecode     bool
	dialect      core.Dialect
	limits       ResourceLimits
	engineMeter  Meter
	cacheStripes int
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
//
// When the caller's context already carries a deadline at or before what
// this timeout would produce, the Engine leaves it alone instead of wrapping
// it in a second timer — the caller's deadline governs alone. A later
// caller deadline is still bounded by this tighter timeout.
//
// WithTimeout(0) disables the Engine's own deadline entirely, leaving the
// caller's context as the only bound. Use this only once the embedder
// applies a deadline to every evaluation lifecycle itself (ADR 0010) —
// otherwise a caller context without a deadline runs unbounded.
func WithTimeout(timeout time.Duration) EngineOption {
	return func(cfg *engineConfig) {
		cfg.timeout = timeout
	}
}

// WithBytecode explicitly selects the bytecode VM evaluator.
//
// The VM executes supported forms; unsupported forms still fall back to the
// tree-walker for correctness.
func WithBytecode() EngineOption {
	return func(cfg *engineConfig) {
		cfg.bytecode = true
	}
}

// WithTreeWalker explicitly selects the tree-walking evaluator.
//
// Pass this after WithBytecode() to opt back out of bytecode.
func WithTreeWalker() EngineOption {
	return func(cfg *engineConfig) {
		cfg.bytecode = false
	}
}

// ResourceLimits configures resource ceilings for an Engine. All fields are
// immutable after construction. A non-positive field is replaced with its default.
type ResourceLimits struct {
	MaxReaderDepth         int
	MaxStructuralDepth     int
	MaxCollectionLen       int
	MaxCacheEntries        int
	MaxCacheBytes          int
	MaxCacheNodes          int
	MaxReductions          int
	MaxAllocationBytes     int
	MaxRetainedBytesPerEnv int
	MaxRetainedSlotsPerEnv int
}

const (
	defaultMaxReaderDepth         = 1024
	defaultMaxStructuralDepth     = 1024
	defaultMaxCollectionLen       = 10_000_000
	defaultMaxCacheEntries        = 4096
	defaultMaxCacheBytes          = 64 << 20
	defaultMaxCacheNodes          = 1_000_000
	defaultMaxReductions          = 10_000_000
	defaultMaxAllocationBytes     = 64 << 20
	defaultMaxRetainedBytesPerEnv = 32 << 20
	defaultMaxRetainedSlotsPerEnv = 100_000
)

func resolveLimits(l ResourceLimits) ResourceLimits {
	if l.MaxReaderDepth <= 0 {
		l.MaxReaderDepth = defaultMaxReaderDepth
	}
	if l.MaxStructuralDepth <= 0 {
		l.MaxStructuralDepth = defaultMaxStructuralDepth
	}
	if l.MaxCollectionLen <= 0 {
		l.MaxCollectionLen = defaultMaxCollectionLen
	}
	if l.MaxCacheEntries <= 0 {
		l.MaxCacheEntries = defaultMaxCacheEntries
	}
	if l.MaxCacheBytes <= 0 {
		l.MaxCacheBytes = defaultMaxCacheBytes
	}
	if l.MaxCacheNodes <= 0 {
		l.MaxCacheNodes = defaultMaxCacheNodes
	}
	if l.MaxReductions <= 0 {
		l.MaxReductions = defaultMaxReductions
	}
	if l.MaxAllocationBytes <= 0 {
		l.MaxAllocationBytes = defaultMaxAllocationBytes
	}
	if l.MaxRetainedBytesPerEnv <= 0 {
		l.MaxRetainedBytesPerEnv = defaultMaxRetainedBytesPerEnv
	}
	if l.MaxRetainedSlotsPerEnv <= 0 {
		l.MaxRetainedSlotsPerEnv = defaultMaxRetainedSlotsPerEnv
	}
	return l
}

// WithResourceLimits sets resource ceilings for the Engine. Passed to New.
func WithResourceLimits(limits ResourceLimits) EngineOption {
	return func(cfg *engineConfig) {
		cfg.limits = limits
	}
}

// WithDialect selects the Dialect the Engine runs. The Dialect is resolved and
// validated by core.NewDialect and is immutable for the Engine's lifetime; New
// only reads its frozen dispatch table. Without this option the
// Engine runs the Common Lisp dialect. Select the prior Clojure-style surface
// with WithDialect(clojure.Dialect()).
func WithDialect(d core.Dialect) EngineOption {
	return func(cfg *engineConfig) {
		cfg.dialect = d
	}
}

// discardLogger backs every engine constructed with a nil logger. It has no
// per-engine state and slog.Logger is safe for concurrent use, so sharing
// one instance across engines is indistinguishable from each engine building
// its own; slog.DiscardHandler.Enabled always reports false, so calls on a
// default-logger engine skip attribute formatting instead of building and
// discarding it. Nothing may reassign engineImpl.logger after construction
// (the only writes are here and at the assignment below) or this sharing
// would leak between engines.
var discardLogger = slog.New(slog.DiscardHandler)

// New creates an Engine. log may be nil, in which case logging is discarded.
//
// Default evaluator is the bytecode VM with per-form tree-walker fallback on
// unsupported bytecode forms.
func New(log *slog.Logger, opts ...EngineOption) (Engine, error) {
	cfg := engineConfig{
		maxEvalDepth: 1000,
		timeout:      30 * time.Second,
		bytecode:     true,
		dialect:      cl.Dialect(),
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	cfg.limits = resolveLimits(cfg.limits)
	if cfg.limits.MaxCollectionLen > math.MaxInt32 {
		return nil, core.NewResourceLimitError(fmt.Sprintf("MaxCollectionLen %d exceeds the vector length cap %d", cfg.limits.MaxCollectionLen, math.MaxInt32))
	}
	if cfg.engineMeter != nil {
		grantedRed, grantedAlloc, err := cfg.engineMeter.LeaseEval(1, 0)
		if err != nil {
			if grantedRed > 0 || grantedAlloc > 0 {
				cfg.engineMeter.ReturnEval(grantedRed, grantedAlloc)
			}
			return nil, core.NewResourceLimitError(fmt.Sprintf("engine setup meter: %v", err))
		}
		cfg.engineMeter.ReturnEval(grantedRed, grantedAlloc)
	}

	if log == nil {
		log = discardLogger
	}

	rootEnv := core.NewEnvWithRetainedLimits(nil, int64(cfg.limits.MaxRetainedBytesPerEnv), int64(cfg.limits.MaxRetainedSlotsPerEnv))
	registry := core.NewRegistry()

	treeWalker, err := core.NewEvaluatorWithDialect(cfg.dialect)
	if err != nil {
		return nil, err
	}
	treeWalker.MaxDepth = cfg.maxEvalDepth
	treeWalker.MaxStructuralDepth = cfg.limits.MaxStructuralDepth
	treeWalker.MaxCollectionLen = cfg.limits.MaxCollectionLen
	var evaluator core.Evaluator
	e := &engineImpl{
		rootEnv:  rootEnv,
		registry: registry,
		logger:   log,
		config:   cfg,
		stats:    newStats(),
	}
	e.rootEnvPtr.Store(rootEnv)
	e.vocab = vocabShapeOf(cfg.dialect)

	if cfg.bytecode {
		be := newBytecodeEvaluator(rootEnv, cfg.maxEvalDepth, cfg.timeout, cfg.limits, treeWalker, cfg.dialect, cfg.engineMeter, cfg.cacheStripes)
		rootEnv.SetEvaluator(be)
		evaluator = be
		e.bytecodeEvaluator = be
	} else {
		rootEnv.SetEvaluator(treeWalker)
		evaluator = treeWalker
	}
	e.evaluator = evaluator
	e.treeWalker = treeWalker
	if cfg.engineMeter != nil {
		rootEnv.SetRetainedMeter(cfg.engineMeter)
	}
	e.fastPath.Store(e.bytecodeEvaluator != nil && cfg.engineMeter == nil)
	// root env. With no template registered its miss-path consult is a no-op.
	installLazyLayer(e)

	log.Debug("engine created", "maxEvalDepth", cfg.maxEvalDepth, "timeout", cfg.timeout, "bytecode", cfg.bytecode)

	return e, nil
}

func (e *engineImpl) Close() error {
	e.stopWatcher()
	if e.bytecodeEvaluator != nil {
		e.bytecodeEvaluator.flushCache()
	}
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
	stats := e.stats.Snapshot()
	if e.bytecodeEvaluator != nil {
		stats.Cache = e.bytecodeEvaluator.cacheStats()
	}
	return stats
}

func (e *engineImpl) OnEval(fn func(EvalEvent)) {
	e.mu.Lock()
	e.evalCallbacks = append(e.evalCallbacks, fn)
	e.recomputeFastPathLocked()
	e.mu.Unlock()
	e.callbacksActive.Store(true)
}

func (e *engineImpl) OnPluginCall(fn func(PluginCallEvent)) {
	e.mu.Lock()
	e.pluginCallbacks = append(e.pluginCallbacks, fn)
	e.recomputeFastPathLocked()
	e.mu.Unlock()
	e.callbacksActive.Store(true)
}

// recomputeFastPathLocked refreshes the lean-boundary condition under e.mu.
// Callback registration is the only runtime mutation site: the bytecode
// evaluator and engine meter are immutable after New. The callback slices
// are the source of truth here — callbacksActive is stored only after the
// lock is released, so reading it under the lock would miss the in-flight
// registration.
func (e *engineImpl) recomputeFastPathLocked() {
	noCallbacks := len(e.evalCallbacks) == 0 && len(e.pluginCallbacks) == 0
	e.fastPath.Store(e.bytecodeEvaluator != nil && e.config.engineMeter == nil && noCallbacks)
}

// fireEvalCallbacks hands the settled outcome to the embedder's observers. The
// evaluation has already settled by then, so a panicking observer must neither
// reach the caller nor become the evaluation's outcome: it is contained per
// callback — one failing observer must not starve the ones registered after
// it — and the settled result and error stand as published.
func (e *engineImpl) fireEvalCallbacks(event EvalEvent) {
	if !e.callbacksActive.Load() {
		return
	}

	e.mu.RLock()
	callbacks := e.evalCallbacks
	e.mu.RUnlock()

	for _, cb := range callbacks {
		e.deliverEvalEvent(cb, event)
	}
}

func (e *engineImpl) deliverEvalEvent(cb func(EvalEvent), event EvalEvent) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Warn("eval observer panic", "source", event.Source, "panic", r)
		}
	}()
	cb(event)
}

func (e *engineImpl) firePluginCallbacks(event PluginCallEvent) {
	if !e.callbacksActive.Load() {
		return
	}

	e.mu.RLock()
	callbacks := e.pluginCallbacks
	e.mu.RUnlock()

	for _, cb := range callbacks {
		e.deliverPluginCallEvent(cb, event)
	}
}

// deliverPluginCallEvent contains a panicking observer at the publication
// point, mirroring deliverEvalEvent: one failing observer must neither reach
// the caller nor starve the callbacks registered after it.
func (e *engineImpl) deliverPluginCallEvent(cb func(PluginCallEvent), event PluginCallEvent) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Warn("plugin-call observer panic", "function", event.Function, "panic", r)
		}
	}()
	cb(event)
}

// applyVocabulary reconciles the cells a plugin operation wrote through env,
// its registration view, with the configured Dialect's vocabulary. It runs
// after each plugin's Init and only reads and writes the names reg recorded
// (live ones; a reload deletes through the view too), the visible renames of
// those names, and the dialect's adapters:
//
//   - a GoFunc an empty-base vocabulary does not list is deleted;
//   - each written name's own binding lands next, then every rename of it;
//   - an adapter whose value cell is absent is bound.
//
// Under Lisp-2 each GoFunc binding is mirrored into the function cell so it
// resolves in head position; a canonical binding (stdlib's native operators)
// mirrors canonically so the VM's native-op fast path still fires. A function
// cell already holding a different live value is left alone: it is a user
// defun the plugin must not revert. A rename the operation itself wrote is
// overwritten regardless, so the rename wins whatever order Init bound them
// in. Nothing outside those names is touched, so a host Bind and the cells of
// earlier plugins keep their bindings and versions.
func (e *engineImpl) applyVocabulary(env *core.Env, reg *core.Registration) error {
	d := e.config.dialect
	lisp2 := d.IsLisp2()
	if !e.vocab.present && !lisp2 {
		return nil
	}

	written := reg.Names()
	slices.Sort(written)
	var scratch [8]core.VocabBinding
	renames := scratch[:0]
	// Own bindings and deletions touch only the written name itself, so they
	// land in one pass; renames wait until every own binding is in place.
	for _, name := range written {
		v, ok, canon := env.GetMaterializedCanonical(name)
		if !ok {
			continue
		}
		start := len(renames)
		renames = d.AppendVocabBindings(renames, name, v, canon)
		if len(renames) == start || renames[start].Name != name {
			env.Delete(name)
			continue
		}
		b := renames[start]
		renames = append(renames[:start], renames[start+1:]...)
		if canon != b.Canonical || !v.Equals(b.Value) {
			if err := setVocabValue(env, b); err != nil {
				return err
			}
		}
		if b.Func {
			if err := setVocabFuncUnlessRebound(env, b); err != nil {
				return err
			}
		}
	}

	for _, b := range renames {
		if _, ours := slices.BinarySearch(written, b.Name); ours {
			if err := setVocabValue(env, b); err != nil {
				return err
			}
			if b.Func {
				if err := setVocabFunc(env, b); err != nil {
					return err
				}
			}
			continue
		}
		if cur, ok, _ := env.GetMaterializedCanonical(b.Name); !ok || cur.Equals(b.Value) {
			if err := setVocabValue(env, b); err != nil {
				return err
			}
		}
		if b.Func {
			if err := setVocabFuncUnlessRebound(env, b); err != nil {
				return err
			}
		}
	}

	for _, a := range e.vocab.adapters {
		if !env.HasLive(a.name) {
			if err := env.Set(a.name, a.value); err != nil {
				return err
			}
		}
		if _, isGoFunc := a.value.(core.GoFunc); lisp2 && isGoFunc && !env.HasLiveFunc(a.name) {
			if err := env.SetFunc(a.name, a.value); err != nil {
				return err
			}
		}
	}
	return nil
}

func setVocabValue(env *core.Env, b core.VocabBinding) error {
	if b.Canonical {
		return env.SetCanonical(b.Name, b.Value)
	}
	return env.Set(b.Name, b.Value)
}

func setVocabFunc(env *core.Env, b core.VocabBinding) error {
	if b.Canonical {
		return env.SetFuncCanonical(b.Name, b.Value)
	}
	return env.SetFunc(b.Name, b.Value)
}

func setVocabFuncUnlessRebound(env *core.Env, b core.VocabBinding) error {
	if cur, ok, _ := env.GetMaterializedFuncCanonical(b.Name); ok && !cur.Equals(b.Value) {
		return nil
	}
	return setVocabFunc(env, b)
}
