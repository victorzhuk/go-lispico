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
	"sync/atomic"
	"time"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

var nowFunc = time.Now

type macroExpander interface {
	MacroExpand(ctx context.Context, form core.Value, env *core.Env) (core.Value, error)
}

type sourceHash [sha256.Size]byte

// cacheKey uniquely identifies a compiled chunk in the bytecode cache.
type cacheKey struct {
	sourceHash sourceHash
	formIndex  int
	dialectFP  string
	macroEpoch int
}

// bytecodeEvaluator runs Lisp forms through the bytecode VM with chunk caching
// and VM pool reuse for concurrent safety and reduced allocation.
type bytecodeEvaluator struct {
	globals            *core.Env
	maxDepth           int
	timeout            time.Duration
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
	maxReductions      int
	maxAllocBytes      int
	engineMeter        Meter
}

func newBytecodeEvaluator(globals *core.Env, maxDepth int, timeout time.Duration, limits ResourceLimits, treeWalker core.Evaluator, dialect core.Dialect, meter Meter) *bytecodeEvaluator {
	be := &bytecodeEvaluator{
		globals:            globals,
		maxDepth:           maxDepth,
		timeout:            timeout,
		maxStructuralDepth: limits.MaxStructuralDepth,
		maxCollectionLen:   limits.MaxCollectionLen,
		maxCacheEntries:    limits.MaxCacheEntries,
		maxReductions:      limits.MaxReductions,
		maxAllocBytes:      limits.MaxAllocationBytes,
		engineMeter:        meter,
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

func (be *bytecodeEvaluator) treeFallbackCtx(ctx context.Context) context.Context {
	if be.timeout <= 0 || !core.EvalDeadlineFrom(ctx).IsZero() {
		return ctx
	}
	bound := nowFunc().Add(be.timeout)
	if d, ok := ctx.Deadline(); ok && !d.After(bound) {
		return ctx
	}
	return core.WithEvalDeadline(ctx, bound)
}

func (be *bytecodeEvaluator) Eval(ctx context.Context, form core.Value, env *core.Env) (result core.Value, err error) {
	ctx = be.evalResourceContext(ctx)
	if core.HasEvalMeter(ctx) {
		top, err := core.StartEval(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
			}
		}()
	}
	if err := core.PollEvalState(ctx); err != nil {
		return nil, err
	}
	expanded, err := be.macro.MacroExpand(ctx, form, env)
	if err != nil {
		return nil, fmt.Errorf("macro expand: %w", err)
	}
	comp := compiler.NewCompilerWithDialect("<eval>", &be.dialect)
	comp.SetEvalMeter(core.EvalMeterFrom(ctx))
	if err := comp.Compile(expanded); err != nil {
		if isUnsupportedInBytecode(err) {
			return be.tree.Eval(be.treeFallbackCtx(ctx), expanded, env)
		}
		return nil, fmt.Errorf("compile: %w", err)
	}
	if err := comp.EmitReturn(); err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	comp.MarkCaptures()
	chunk := comp.Chunk()
	if err := chunk.Validate(); err != nil {
		return nil, err
	}
	if err := chargeCompiledChunk(ctx, chunk); err != nil {
		return nil, err
	}
	// A one-shot eval is never reused, so its global reads resolve through the
	// chain walk rather than paying to build a site cache that never gets a hit.
	return be.runVM(ctx, chunk, env)
}

func (be *bytecodeEvaluator) Apply(ctx context.Context, fn core.Value, args []core.Value, env *core.Env) (result core.Value, err error) {
	ctx = be.evalResourceContext(ctx)
	if core.HasEvalMeter(ctx) {
		top, err := core.StartEval(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
			}
		}()
	}
	if err := core.PollEvalState(ctx); err != nil {
		return nil, err
	}
	if _, ok := fn.(core.Lambda); ok {
		result, err := be.tree.Apply(ctx, fn, args, env)
		if err == nil {
			err = core.FlushEvalState(ctx)
		}
		return result, err
	}
	v := be.vmPool.Get().(*vm.VM)
	v.Reset()
	v.SetGlobals(env)
	v.SetDeadline(core.EvalDeadlineFrom(ctx))
	v.SetEvalMeter(core.EvalMeterFrom(ctx))
	vm.WithStructuralDepthCounter(core.EvalStructCounter(ctx))(v)
	result, err = v.ApplyPooled(ctx, fn, args, env)
	be.vmPool.Put(v)
	return result, err
}

// applyOnVM is the shared apply step used by Engine.Call, Fn.Call, and
// PinnedFn.Call through callBoundary: it sets globals, arms the VM
// depth/deadline, and runs ApplyPooled. The caller owns v and its lifecycle
// — Apply has its own pool Get/Put path (eval.go:112-129) and does not call
// through here. WithEvalState branching is the single source of truth for
// resource-budget sharing at the Go-Lisp boundary.
func (be *bytecodeEvaluator) applyOnVM(v *vm.VM, ctx context.Context, fn core.Value, args []core.Value, env *core.Env, timeout time.Duration) (core.Value, error) {
	v.SetGlobals(env)
	if core.HasEvalState(ctx) {
		vm.WithStructuralDepthCounter(core.EvalStructCounter(ctx))(v)
		v.SetDeadline(core.EvalDeadlineFrom(ctx))
		v.SetEvalMeter(core.EvalMeterFrom(ctx))
	} else {
		v.SetTimeout(timeout)
		v.SetResourceLimits(be.maxReductions, be.maxAllocBytes)
	}
	return v.ApplyPooled(ctx, fn, args, env)
}

func (be *bytecodeEvaluator) CollectionLimit() int { return be.maxCollectionLen }

// EvalCached evaluates form with caching: macro-expands, checks the chunk cache
// runs via a pooled VM.
func (be *bytecodeEvaluator) EvalCached(ctx context.Context, form core.Value, env *core.Env, sourceHash sourceHash, formIndex int) (core.Value, error) {
	ctx = be.evalResourceContext(ctx)
	if err := core.PollEvalState(ctx); err != nil {
		return nil, err
	}
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
		comp.SetEvalMeter(core.EvalMeterFrom(ctx))
		if err := comp.Compile(expanded); err != nil {
			if isUnsupportedInBytecode(err) {
				return be.tree.Eval(be.treeFallbackCtx(ctx), expanded, env)
			}
			return nil, fmt.Errorf("compile: %w", err)
		}
		if err := comp.EmitReturn(); err != nil {
			return nil, fmt.Errorf("compile: %w", err)
		}
		comp.MarkCaptures()
		chunk = comp.Chunk()
		if err := chunk.Validate(); err != nil {
			return nil, err
		}
		if err := chargeCompiledChunk(ctx, chunk); err != nil {
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
	if deadline := core.EvalDeadlineFrom(ctx); !deadline.IsZero() {
		v.SetDeadline(deadline)
	} else {
		v.SetTimeout(be.timeout)
	}
	v.SetEvalMeter(core.EvalMeterFrom(ctx))
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

func (be *bytecodeEvaluator) evalResourceContext(ctx context.Context) context.Context {
	if !core.HasEvalMeter(ctx) && be.engineMeter != nil {
		ctx = WithMeter(ctx, be.engineMeter)
	}
	return core.WithEvalResourceLimits(ctx, be.maxReductions, be.maxAllocBytes)
}

func (e *engineImpl) evalResourceContext(ctx context.Context) context.Context {
	if !core.HasEvalMeter(ctx) && e.config.engineMeter != nil {
		ctx = WithMeter(ctx, e.config.engineMeter)
	}
	return core.WithEvalResourceLimits(ctx, e.config.limits.MaxReductions, e.config.limits.MaxAllocationBytes)
}

type retainedUsage struct {
	bytes int64
	slots int64
}

type retainedBindingCharge struct {
	meter Meter
	bytes int64
	slots int64
}

type meterEvaluator struct {
	core.Evaluator
	mu    sync.RWMutex
	meter Meter
	cells map[*core.Cell]retainedBindingCharge
}

func (me *meterEvaluator) TrackBinding(cell *core.Cell, meter Meter, bytes, slots int64) {
	if cell == nil || meter == nil || (bytes <= 0 && slots <= 0) {
		return
	}
	me.mu.Lock()
	defer me.mu.Unlock()
	if me.cells == nil {
		me.cells = make(map[*core.Cell]retainedBindingCharge)
	}
	me.cells[cell] = retainedBindingCharge{meter: meter, bytes: bytes, slots: slots}
}

func (me *meterEvaluator) ReleaseCell(cell *core.Cell) (int64, int64, bool) {
	if cell == nil {
		return 0, 0, false
	}
	me.mu.Lock()
	charge, ok := me.cells[cell]
	if ok {
		delete(me.cells, cell)
	}
	me.mu.Unlock()

	if !ok || charge.meter == nil {
		return 0, 0, false
	}
	charge.meter.ReleaseRetained(charge.bytes, charge.slots)
	return charge.bytes, charge.slots, true
}

func (me *meterEvaluator) ReleaseRetained(bytes, slots int64) {
	if me.meter != nil {
		me.meter.ReleaseRetained(bytes, slots)
	}
}

func unwrapEvaluator(ev core.Evaluator) core.Evaluator {
	if me, ok := ev.(*meterEvaluator); ok {
		return me.Evaluator
	}
	return ev
}

func (e *engineImpl) attachScopeMeter(env *core.Env, meter Meter) *meterEvaluator {
	if env == nil || meter == nil {
		return nil
	}
	baseEval := env.Evaluator()
	if me, ok := baseEval.(*meterEvaluator); ok {
		return me
	}
	if baseEval == nil {
		baseEval = e.evaluator
	}
	baseEval = unwrapEvaluator(baseEval)
	me := &meterEvaluator{Evaluator: baseEval, meter: meter}
	env.SetEvaluator(me)
	return me
}

func retainedUsageOf(env *core.Env) retainedUsage {
	if env == nil {
		return retainedUsage{}
	}
	bytes, slots := env.RetainedUsage()
	return retainedUsage{bytes: bytes, slots: slots}
}

func snapshotCellSet(env *core.Env) map[*core.Cell]struct{} {
	if env == nil {
		return nil
	}
	m := make(map[*core.Cell]struct{})
	env.ForEachCell(func(name string, cell *core.Cell) {
		m[cell] = struct{}{}
	})
	return m
}

func (e *engineImpl) settleRetained(ctx context.Context, beforeRoot retainedUsage, beforeRootCells map[*core.Cell]struct{}, scope *core.Env, beforeScope retainedUsage, beforeScopeCells map[*core.Cell]struct{}) error {
	meter := MeterFromContext(ctx)
	if meter == nil {
		meter = e.config.engineMeter
	}
	if meter == nil {
		return nil
	}
	rootAfter := retainedUsageOf(e.rootEnv)
	scopeAfter := retainedUsageOf(scope)
	bytes := max(rootAfter.bytes-beforeRoot.bytes, 0) + max(scopeAfter.bytes-beforeScope.bytes, 0)
	slots := max(rootAfter.slots-beforeRoot.slots, 0) + max(scopeAfter.slots-beforeScope.slots, 0)
	if bytes == 0 && slots == 0 {
		return nil
	}
	if err := meter.ChargeRetained(bytes, slots); err != nil {
		return core.NewResourceLimitError(fmt.Sprintf("retained meter: %v", err))
	}

	me := e.attachScopeMeter(e.rootEnv, meter)
	if me != nil && e.rootEnv != nil {
		e.rootEnv.ForEachCell(func(name string, cell *core.Cell) {
			if _, existed := beforeRootCells[cell]; !existed {
				val, _, _ := e.rootEnv.ReadCell(cell)
				me.TrackBinding(cell, meter, core.RetainedBindingBytes(name, val), 1)
			}
		})
	}
	if scope != nil {
		scopeMe := e.attachScopeMeter(scope, meter)
		if scopeMe != nil {
			scope.ForEachCell(func(name string, cell *core.Cell) {
				if _, existed := beforeScopeCells[cell]; !existed {
					val, _, _ := scope.ReadCell(cell)
					scopeMe.TrackBinding(cell, meter, core.RetainedBindingBytes(name, val), 1)
				}
			})
		}
	}
	return nil
}

func (e *engineImpl) readForms(ctx context.Context, input string) ([]core.Value, error) {
	forms, stats, err := e.config.dialect.ReadWithMaxDepthStats(input, e.config.limits.MaxReaderDepth)
	if err != nil {
		return nil, err
	}
	if err := core.ChargeEvalReader(ctx, stats); err != nil {
		return nil, err
	}
	return forms, nil
}

func chargeCompiledChunk(ctx context.Context, chunk *vm.Chunk) error {
	return core.ChargeEvalAllocBytes(ctx, compiledChunkBytes(chunk))
}

func compiledChunkBytes(chunk *vm.Chunk) int64 {
	if chunk == nil {
		return 0
	}
	bytes := int64(len(chunk.Code))*core.MeterInstructionBytes + core.ValueSlotsBytes(len(chunk.Constants))
	for _, c := range chunk.Constants {
		bytes += core.ValueShallowBytes(c)
	}
	bytes += core.ValueSlotsBytes(len(chunk.SubChunks))
	for _, sub := range chunk.SubChunks {
		bytes += compiledChunkBytes(sub)
	}
	return bytes
}

func (e *engineImpl) Eval(ctx context.Context, source, input string) (result core.Value, err error) {
	start := time.Now()

	metered := core.HasEvalMeter(ctx) || e.config.engineMeter != nil
	ctx = e.evalResourceContext(ctx)
	if metered {
		beforeRoot := retainedUsageOf(e.rootEnv)
		beforeRootCells := snapshotCellSet(e.rootEnv)
		top, err := core.StartEval(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
			}
			if rerr := e.settleRetained(ctx, beforeRoot, beforeRootCells, nil, retainedUsage{}, nil); rerr != nil {
				result = nil
				err = rerr
			}
		}()
	}
	forms, err := e.readForms(ctx, input)
	if err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, fmt.Errorf("read: %w", err)
	}

	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	if be := e.bytecodeEvaluator; be != nil {
		result = core.Nil{}
		sourceHash := sha256Hash(input)
		for i, form := range forms {
			result, err = be.EvalCached(ctx, form, env, sourceHash, i)
			if err != nil {
				if ferr := core.FlushEvalState(ctx); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
					err = ferr
				}
				dur := time.Since(start)
				e.stats.recordEval(dur, err)
				e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
				return nil, fmt.Errorf("eval: %w", err)
			}
		}
		if err := core.FlushEvalState(ctx); err != nil {
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, fmt.Errorf("eval: %w", err)
		}
		dur := time.Since(start)
		e.stats.recordEval(dur, nil)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
		e.logger.Debug("eval", "source", source, "duration", dur)
		return result, nil
	}

	ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))

	result = core.Nil{}
	for _, form := range forms {
		result, err = e.evaluator.Eval(ctx, form, env)
		if err != nil {
			if ferr := core.FlushEvalState(ctx); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				err = ferr
			}
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, fmt.Errorf("eval: %w", err)
		}
	}
	if err := core.FlushEvalState(ctx); err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, fmt.Errorf("eval: %w", err)
	}

	dur := time.Since(start)
	e.stats.recordEval(dur, nil)
	e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
	e.logger.Debug("eval", "source", source, "duration", dur)
	return result, nil
}

func sha256Hash(s string) sourceHash {
	return sha256.Sum256([]byte(s))
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
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	e.mu.RLock()
	env := e.rootEnv
	e.mu.RUnlock()

	fn, ok := env.Get(name)
	counter := e.stats.counterFor(name)
	if !ok {
		counter.Add(1)
		if e.callbacksActive.Load() {
			e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: 0})
		}
		return nil, fmt.Errorf("undefined function: %s", name)
	}
	if be := e.bytecodeEvaluator; be != nil {
		v := be.vmPool.Get().(*vm.VM)
		v.Reset()
		defer be.vmPool.Put(v)
		return e.callBoundary(ctx, name, fn, env, counter, args, v)
	}
	return e.callBoundary(ctx, name, fn, env, counter, args, nil)
}

// callBoundary is the unified Engine.Call fast path: it arms timing, dispatches
// to the bytecode applyOnVM (or the tree-walker when bytecode is off), and
// fires stats/callbacks around the apply. The caller supplies v — a pool-
// acquired VM for Engine.Call and Fn.Call, or a private handle-owned VM for
// PinnedFn.Call — so the per-call VM lifecycle stays in the caller and a
// single apply step is shared across every entry point.
func (e *engineImpl) callBoundary(ctx context.Context, name string, fn core.Value, env *core.Env, counter *atomic.Int64, args []core.Value, v *vm.VM) (result core.Value, err error) {
	needsEvalState := core.HasEvalState(ctx) || core.HasEvalMeter(ctx) || e.config.engineMeter != nil
	if needsEvalState {
		ctx = e.evalResourceContext(ctx)
		top, beginErr := core.StartEval(ctx)
		if beginErr != nil {
			return nil, beginErr
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
			}
		}()
	}

	active := e.callbacksActive.Load()
	var start time.Time
	if active {
		start = nowFunc()
	}
	if be := e.bytecodeEvaluator; be != nil {
		result, err = be.applyOnVM(v, ctx, fn, args, env, e.config.timeout)
	} else {
		var deadline time.Time
		if e.config.timeout > 0 {
			deadlineStart := start
			if !active {
				deadlineStart = nowFunc()
			}
			deadline = e.evalDeadline(ctx, deadlineStart)
		}
		if !core.HasEvalState(ctx) {
			ctx = e.evalResourceContext(ctx)
		}
		ctx = core.WithEvalDeadline(ctx, deadline)
		result, err = e.evaluator.Apply(ctx, fn, args, env)
		if err == nil {
			err = core.FlushEvalState(ctx)
		}
	}
	counter.Add(1)
	if active {
		e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: nowFunc().Sub(start)})
	}
	return result, err
}

func (e *engineImpl) Bind(name string, v core.Value) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.registry.HasPrefix(name) {
		return fmt.Errorf("bind: name %q conflicts with registered plugin namespace", name)
	}

	if e.config.dialect.IsLisp2() {
		if err := e.rootEnv.SetBoth(name, v); err != nil {
			return err
		}
	} else if err := e.rootEnv.Set(name, v); err != nil {
		return err
	}
	e.logger.Debug("bind", "name", name)
	return nil
}

func (e *engineImpl) EvalWithBindings(ctx context.Context, source string, bindings map[string]core.Value) (core.Value, error) {
	result, _, err := e.evalWithBindingScope(ctx, source, bindings)
	return result, err
}

func (e *engineImpl) LoadScope(ctx context.Context, source string, bindings map[string]core.Value) (core.Value, *core.Env, error) {
	return e.evalWithBindingScope(ctx, source, bindings)
}

func (e *engineImpl) evalWithBindingScope(ctx context.Context, source string, bindings map[string]core.Value) (result core.Value, childEnv *core.Env, err error) {
	start := time.Now()

	metered := core.HasEvalMeter(ctx) || e.config.engineMeter != nil
	ctx = e.evalResourceContext(ctx)
	ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))
	var beforeScope retainedUsage
	var beforeScopeCells map[*core.Cell]struct{}
	if metered {
		beforeRoot := retainedUsageOf(e.rootEnv)
		beforeRootCells := snapshotCellSet(e.rootEnv)
		top, err := core.StartEval(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
			}
			if rerr := e.settleRetained(ctx, beforeRoot, beforeRootCells, childEnv, beforeScope, beforeScopeCells); rerr != nil {
				result = nil
				err = rerr
			}
		}()
	}

	forms, err := e.readForms(ctx, source)
	if err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, nil, fmt.Errorf("read: %w", err)
	}

	e.mu.RLock()
	childEnv = e.rootEnv.Child()
	e.mu.RUnlock()
	beforeScope = retainedUsageOf(childEnv)
	beforeScopeCells = snapshotCellSet(childEnv)

	for name, val := range bindings {
		if e.config.dialect.IsLisp2() {
			if err := childEnv.SetBoth(name, val); err != nil {
				return nil, childEnv, err
			}
			continue
		}
		if err := childEnv.Set(name, val); err != nil {
			return nil, childEnv, err
		}
	}
	result = core.Nil{}
	for _, form := range forms {
		result, err = e.evaluator.Eval(ctx, form, childEnv)
		if err != nil {
			if ferr := core.FlushEvalState(ctx); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				err = ferr
			}
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
			return nil, childEnv, fmt.Errorf("eval: %w", err)
		}
	}
	if err := core.FlushEvalState(ctx); err != nil {
		dur := time.Since(start)
		e.stats.recordEval(dur, err)
		e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		return nil, childEnv, fmt.Errorf("eval: %w", err)
	}

	dur := time.Since(start)
	e.stats.recordEval(dur, nil)
	e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: nil})
	return result, childEnv, nil
}
