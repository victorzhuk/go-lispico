package runtime

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

type macroExpander interface {
	MacroExpand(ctx context.Context, form core.Value, env *core.Env) (core.Value, error)
}

// cacheKey uniquely identifies a compiled chunk in the bytecode cache.
type cacheKey struct {
	sourceHash string
	formIndex  int
	dialectFP  string
	macroEpoch int
}

// bytecodeEvaluator runs Lisp forms through the bytecode VM with chunk caching
// and VM pool reuse for concurrent safety and reduced allocation.
type bytecodeEvaluator struct {
	globals            *core.Env
	maxDepth           int
	macro              macroExpander
	tree               core.Evaluator
	dialect            core.Dialect
	mu                 sync.Mutex
	cache              map[cacheKey]*vm.Chunk
	dialectFP          string
	vmPool             sync.Pool
	maxStructuralDepth int
	maxCollectionLen   int
	maxCacheEntries    int
}

func newBytecodeEvaluator(globals *core.Env, maxDepth int, limits ResourceLimits, treeWalker core.Evaluator, dialect core.Dialect) *bytecodeEvaluator {
	be := &bytecodeEvaluator{
		globals:            globals,
		maxDepth:           maxDepth,
		maxStructuralDepth: limits.MaxStructuralDepth,
		maxCollectionLen:   limits.MaxCollectionLen,
		maxCacheEntries:    limits.MaxCacheEntries,
		macro:              treeWalker.(macroExpander),
		tree:               treeWalker,
		dialect:            dialect,
		dialectFP:          dialect.Fingerprint(),
		cache:              make(map[cacheKey]*vm.Chunk),
	}
	be.vmPool = sync.Pool{
		New: func() any {
			return vm.New(globals, vm.WithMaxDepth(maxDepth), vm.WithEvaluator(be), vm.WithMaxStructuralDepth(be.maxStructuralDepth))
		},
	}
	return be
}

func (be *bytecodeEvaluator) Eval(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	ctx = core.EnsureEvalState(ctx)
	expanded, err := be.macro.MacroExpand(ctx, form, env)
	if err != nil {
		return nil, fmt.Errorf("macro expand: %w", err)
	}
	comp := compiler.NewCompilerWithDialect("<eval>", &be.dialect)
	if err := comp.Compile(expanded); err != nil {
		if isUnsupportedInBytecode(err) {
			return be.tree.Eval(ctx, expanded, env)
		}
		return nil, fmt.Errorf("compile: %w", err)
	}
	comp.Chunk().Emit(vm.OpReturn, 0)
	comp.MarkCaptures()
	chunk := comp.Chunk()
	if err := chunk.Validate(); err != nil {
		return nil, err
	}
	// A one-shot eval is never reused, so its global reads resolve through the
	// chain walk rather than paying to build a site cache that never gets a hit.
	return be.runVM(ctx, chunk, env)
}

func (be *bytecodeEvaluator) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (core.Value, error) {
	ctx = core.EnsureEvalState(ctx)
	v := be.vmPool.Get().(*vm.VM)
	v.Reset()
	v.SetGlobals(env)
	v.SetDeadline(core.EvalDeadlineFrom(ctx))
	vm.WithStructuralDepthCounter(core.EvalStructCounter(ctx))(v)
	result, err := v.ApplyPooled(ctx, fn, args, env)
	be.vmPool.Put(v)
	return result, err
}

func (be *bytecodeEvaluator) CollectionLimit() int { return be.maxCollectionLen }

// EvalCached evaluates form with caching: macro-expands, checks the chunk cache
// by key (sourceHash, formIndex, dialectFP, macroEpoch), compiles on miss, and
// runs via a pooled VM.
func (be *bytecodeEvaluator) EvalCached(ctx context.Context, form core.Value, env *core.Env, sourceHash string, formIndex int) (core.Value, error) {
	ctx = core.EnsureEvalState(ctx)
	expanded, err := be.macro.MacroExpand(ctx, form, env)
	if err != nil {
		return nil, fmt.Errorf("macro expand: %w", err)
	}

	key := cacheKey{
		sourceHash: sourceHash,
		formIndex:  formIndex,
		dialectFP:  be.dialectFP,
		macroEpoch: be.globals.MacroEpoch(),
	}

	be.mu.Lock()
	chunk, hit := be.cache[key]
	be.mu.Unlock()

	if !hit {
		currentEpoch := be.globals.MacroEpoch()
		be.mu.Lock()
		for k := range be.cache {
			if k.macroEpoch != currentEpoch {
				delete(be.cache, k)
			}
		}
		be.mu.Unlock()

		comp := compiler.NewCompilerWithDialect("<eval>", &be.dialect)
		if err := comp.Compile(expanded); err != nil {
			if isUnsupportedInBytecode(err) {
				return be.tree.Eval(ctx, expanded, env)
			}
			return nil, fmt.Errorf("compile: %w", err)
		}
		comp.Chunk().Emit(vm.OpReturn, 0)
		comp.MarkCaptures()
		chunk = comp.Chunk()
		if err := chunk.Validate(); err != nil {
			return nil, err
		}

		be.mu.Lock()
		if cached, dup := be.cache[key]; dup {
			chunk = cached
		} else {
			be.cache[key] = chunk
			for len(be.cache) > be.maxCacheEntries {
				for k := range be.cache {
					delete(be.cache, k)
					break
				}
			}
		}
		be.mu.Unlock()
	}

	// The site cache pays off only across repeated runs, so build it the first
	// time a cached chunk is reused (a hit) — a compile-once/run-once form
	// (e.g. a body that bumps the macro epoch every eval) never builds it.
	if hit {
		chunk.EnsureSites()
	}
	return be.runVM(ctx, chunk, env)
}

// runVM gets a VM from the pool, resets it, runs chunk in env, and returns the VM.
func (be *bytecodeEvaluator) runVM(ctx context.Context, chunk *vm.Chunk, env *core.Env) (core.Value, error) {
	v := be.vmPool.Get().(*vm.VM)
	v.Reset()
	v.SetGlobals(env)
	v.SetDeadline(core.EvalDeadlineFrom(ctx))
	vm.WithStructuralDepthCounter(core.EvalStructCounter(ctx))(v)
	result, err := v.Run(ctx, chunk)
	be.vmPool.Put(v)
	return result, err
}

// isUnsupportedInBytecode reports whether err is the compiler's typed
// "unsupported in bytecode" error (defmacro nested in a body, unquote-splicing),
// so the caller can fall back to the tree-walker instead of failing the eval.
func isUnsupportedInBytecode(err error) bool {
	var lerr *core.LispicoError
	return errors.As(err, &lerr) && lerr.Code == compiler.CodeUnsupported
}

// evalDeadline returns the instant the Engine's own deadline fires for an
// evaluation started at start, or the zero Time when the Engine imposes none:
// timeout disabled, or the caller already holds an equal-or-earlier deadline
// its own context enforces (ADR 0010).
func (e *engineImpl) evalDeadline(ctx context.Context, start time.Time) time.Time {
	if e.config.timeout <= 0 {
		return time.Time{}
	}
	bound := start.Add(e.config.timeout)
	if d, ok := ctx.Deadline(); ok && !d.After(bound) {
		return time.Time{}
	}
	return bound
}

func (e *engineImpl) Eval(ctx context.Context, source, input string) (core.Value, error) {
	start := time.Now()

	ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))

	forms, err := e.config.dialect.ReadWithMaxDepth(input, e.config.limits.MaxReaderDepth)
	if err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, fmt.Errorf("read: %w", err)
	}

	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	// Bytecode path: use cached compilation.
	if be := e.bytecodeEvaluator; be != nil {
		var result core.Value = core.Nil{}
		sourceHash := sha256Hash(input)
		for i, form := range forms {
			result, err = be.EvalCached(ctx, form, env, sourceHash, i)
			if err != nil {
				dur := time.Since(start)
				e.stats.recordEval(dur, err)
				e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
				return nil, fmt.Errorf("eval: %w", err)
			}
		}
		dur := time.Since(start)
		e.stats.recordEval(dur, nil)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
		e.logger.Debug("eval", "source", source, "duration", dur)
		return result, nil
	}

	// Tree-walker path.
	var result core.Value = core.Nil{}
	for _, form := range forms {
		result, err = e.evaluator.Eval(ctx, form, env)
		if err != nil {
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, fmt.Errorf("eval: %w", err)
		}
	}

	dur := time.Since(start)
	e.stats.recordEval(dur, nil)
	e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
	e.logger.Debug("eval", "source", source, "duration", dur)
	return result, nil
}

// sha256Hash returns a hex-encoded SHA-256 hash of s.
func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:])
}

func (e *engineImpl) EvalFile(path string) (core.Value, error) {
	e.logger.Info("loading file", "path", path)

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	result, err := e.Eval(context.Background(), path, string(content))
	if err != nil {
		return nil, err
	}

	e.logger.Info("loaded file", "path", path)
	return result, nil
}

func (e *engineImpl) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) == ".lisp" {
			files = append(files, entry.Name())
		}
	}

	sort.Strings(files)

	for _, name := range files {
		p := filepath.Join(dir, name)
		if _, err := e.EvalFile(p); err != nil {
			return fmt.Errorf("load %s: %w", p, err)
		}
	}

	return nil
}

func (e *engineImpl) Call(ctx context.Context, name string, args ...core.Value) (core.Value, error) {
	start := time.Now()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))

	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	fn, ok := env.Get(name)
	if !ok {
		dur := time.Since(start)
		e.stats.recordPluginCall(name, dur)
		e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: dur})
		return nil, fmt.Errorf("undefined function: %s", name)
	}

	result, err := e.evaluator.Apply(ctx, fn, args, env)
	dur := time.Since(start)
	e.stats.recordPluginCall(name, dur)
	e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: dur})
	return result, err
}

func (e *engineImpl) Bind(name string, v core.Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.registry.HasPrefix(name) {
		return fmt.Errorf("bind: name %q conflicts with registered plugin namespace", name)
	}

	e.rootEnv.Set(name, v)
	// Under Lisp-2, head-position resolution goes through the function cell.
	// Mirror the value-cell binding into the function cell so embedders can
	// call bound names in head position regardless of the active dialect.
	if e.config.dialect.IsLisp2() {
		e.rootEnv.SetFunc(name, v)
	}
	e.logger.Debug("bind", "name", name)
	return nil
}

func (e *engineImpl) EvalWithBindings(ctx context.Context, source string, bindings map[string]core.Value) (core.Value, error) {
	start := time.Now()

	ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))

	forms, err := e.config.dialect.ReadWithMaxDepth(source, e.config.limits.MaxReaderDepth)
	if err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, fmt.Errorf("read: %w", err)
	}

	e.mu.RLock()
	childEnv := e.rootEnv.Child()
	e.mu.RUnlock()

	for name, val := range bindings {
		childEnv.Set(name, val)
		if e.config.dialect.IsLisp2() {
			childEnv.SetFunc(name, val)
		}
	}

	var result core.Value = core.Nil{}
	for _, form := range forms {
		result, err = e.evaluator.Eval(ctx, form, childEnv)
		if err != nil {
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, fmt.Errorf("eval: %w", err)
		}
	}

	dur := time.Since(start)
	e.stats.recordEval(dur, nil)
	e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
	return result, nil
}
