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
	cache              map[cacheKey]*cacheEntry
	cacheHead          *cacheEntry
	cacheTail          *cacheEntry
	cacheBytes         int64
	cacheNodes         int
	dialectFP          string
	vmPool             sync.Pool
	maxStructuralDepth int
	maxCollectionLen   int
	maxCacheEntries    int
	maxCacheBytes      int
	maxCacheNodes      int
	maxReductions      int
	maxAllocBytes      int
	engineMeter        Meter
}

type cacheEntry struct {
	key   cacheKey
	chunk *vm.Chunk
	prev  *cacheEntry
	next  *cacheEntry
}

func newBytecodeEvaluator(globals *core.Env, maxDepth int, timeout time.Duration, limits ResourceLimits, treeWalker core.Evaluator, dialect core.Dialect, meter Meter) *bytecodeEvaluator {
	be := &bytecodeEvaluator{
		globals:            globals,
		maxDepth:           maxDepth,
		timeout:            timeout,
		maxStructuralDepth: limits.MaxStructuralDepth,
		maxCollectionLen:   limits.MaxCollectionLen,
		maxCacheEntries:    limits.MaxCacheEntries,
		maxCacheBytes:      limits.MaxCacheBytes,
		maxCacheNodes:      limits.MaxCacheNodes,
		maxReductions:      limits.MaxReductions,
		maxAllocBytes:      limits.MaxAllocationBytes,
		engineMeter:        meter,
		macro:              treeWalker.(macroExpander),
		tree:               treeWalker,
		dialect:            dialect,
		dialectFP:          dialect.Fingerprint(),
		cache:              make(map[cacheKey]*cacheEntry),
	}
	be.vmPool = sync.Pool{
		New: func() any {
			return vm.New(globals, vm.WithMaxDepth(maxDepth), vm.WithEvaluator(be), vm.WithMaxStructuralDepth(be.maxStructuralDepth))
		},
	}
	return be
}

func (be *bytecodeEvaluator) cacheGetLocked(key cacheKey) (*vm.Chunk, bool) {
	entry, ok := be.cache[key]
	if !ok {
		return nil, false
	}
	be.moveCacheEntryToHeadLocked(entry)
	return entry.chunk, true
}

func (be *bytecodeEvaluator) cacheAdmitLocked(key cacheKey, chunk *vm.Chunk) bool {
	if !be.cacheFitsAlone(chunk) {
		return false
	}
	if be.engineMeter != nil {
		if err := be.engineMeter.ChargeRetained(chunk.DeepBytes, 1); err != nil {
			return false
		}
	}
	entry := &cacheEntry{key: key, chunk: chunk}
	be.cache[key] = entry
	be.insertCacheEntryHeadLocked(entry)
	be.cacheBytes += chunk.DeepBytes
	be.cacheNodes += chunk.NodeCount
	for be.cacheOverLimitLocked() {
		be.evictCacheEntryLocked(be.cacheTail)
	}
	return true
}

func (be *bytecodeEvaluator) cacheFitsAlone(chunk *vm.Chunk) bool {
	if chunk == nil {
		return false
	}
	if be.maxCacheEntries < 1 {
		return false
	}
	if int64(be.maxCacheBytes) < chunk.DeepBytes {
		return false
	}
	return be.maxCacheNodes >= chunk.NodeCount
}

func (be *bytecodeEvaluator) cacheOverLimitLocked() bool {
	return len(be.cache) > be.maxCacheEntries || be.cacheBytes > int64(be.maxCacheBytes) || be.cacheNodes > be.maxCacheNodes
}

func (be *bytecodeEvaluator) insertCacheEntryHeadLocked(entry *cacheEntry) {
	entry.prev = nil
	entry.next = be.cacheHead
	if be.cacheHead != nil {
		be.cacheHead.prev = entry
	} else {
		be.cacheTail = entry
	}
	be.cacheHead = entry
}

func (be *bytecodeEvaluator) moveCacheEntryToHeadLocked(entry *cacheEntry) {
	if entry == be.cacheHead {
		return
	}
	be.unlinkCacheEntryLocked(entry)
	be.insertCacheEntryHeadLocked(entry)
}

func (be *bytecodeEvaluator) unlinkCacheEntryLocked(entry *cacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		be.cacheHead = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		be.cacheTail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

func (be *bytecodeEvaluator) evictCacheEntryLocked(entry *cacheEntry) {
	if entry == nil {
		return
	}
	delete(be.cache, entry.key)
	be.unlinkCacheEntryLocked(entry)
	be.cacheBytes -= entry.chunk.DeepBytes
	be.cacheNodes -= entry.chunk.NodeCount
	if be.cacheBytes < 0 {
		be.cacheBytes = 0
	}
	if be.cacheNodes < 0 {
		be.cacheNodes = 0
	}
	if be.engineMeter != nil {
		be.engineMeter.ReleaseRetained(entry.chunk.DeepBytes, 1)
	}
}

func (be *bytecodeEvaluator) flushCacheEpochLocked(epoch int) {
	for entry := be.cacheTail; entry != nil; {
		prev := entry.prev
		if entry.key.macroEpoch != epoch {
			be.evictCacheEntryLocked(entry)
		}
		entry = prev
	}
}

func (be *bytecodeEvaluator) flushCache() {
	be.mu.Lock()
	defer be.mu.Unlock()
	for be.cacheTail != nil {
		be.evictCacheEntryLocked(be.cacheTail)
	}
}

func (be *bytecodeEvaluator) cacheStats() CacheStats {
	be.mu.Lock()
	defer be.mu.Unlock()
	return CacheStats{
		Entries: len(be.cache),
		Bytes:   be.cacheBytes,
		Nodes:   be.cacheNodes,
		Epoch:   fmt.Sprintf("%s:%d", be.dialectFP, be.globals.MacroEpoch()),
	}
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
		var top bool
		top, err = core.StartEval(ctx)
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
		var top bool
		top, err = core.StartEval(ctx)
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
	vm.WithCallDepthCounter(core.EvalCallCounter(ctx))(v)
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
		vm.WithCallDepthCounter(core.EvalCallCounter(ctx))(v)
		v.SetDeadline(core.EvalDeadlineFrom(ctx))
		v.SetEvalMeter(core.EvalMeterFrom(ctx))
	} else {
		v.SetTimeout(timeout)
		v.SetResourceLimits(be.maxReductions, be.maxAllocBytes)
	}
	return v.ApplyPooled(ctx, fn, args, env)
}

func (be *bytecodeEvaluator) CollectionLimit() int        { return be.maxCollectionLen }
func (be *bytecodeEvaluator) ConstructionDepthLimit() int { return be.maxStructuralDepth }

// EvalCached evaluates form with caching: checks the chunk cache, macro-expands
// and compiles only on a miss, then runs via a pooled VM.
func (be *bytecodeEvaluator) EvalCached(ctx context.Context, form core.Value, env *core.Env, sourceHash sourceHash, formIndex int) (core.Value, error) {
	ctx = be.evalResourceContext(ctx)
	if err := core.PollEvalState(ctx); err != nil {
		return nil, err
	}
	key := cacheKey{
		sourceHash: sourceHash,
		formIndex:  formIndex,
		dialectFP:  be.dialectFP,
		macroEpoch: be.globals.MacroEpoch(),
	}

	be.mu.Lock()
	chunk, hit := be.cacheGetLocked(key)
	be.mu.Unlock()

	if !hit {
		// Expand only on a miss. A cached chunk was compiled from the
		// expansion, so re-expanding to reach it is work whose result is
		// thrown away — and an expander with side effects would re-run
		// them. Expansion is compile-time, not per-evaluation.
		expanded, err := be.macro.MacroExpand(ctx, form, env)
		if err != nil {
			return nil, fmt.Errorf("macro expand: %w", err)
		}

		currentEpoch := be.globals.MacroEpoch()
		be.mu.Lock()
		be.flushCacheEpochLocked(currentEpoch)
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
		if cached, dup := be.cacheGetLocked(key); dup {
			chunk = cached
			hit = true
		} else if be.cacheAdmitLocked(key, chunk) {
			hit = false
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
	vm.WithCallDepthCounter(core.EvalCallCounter(ctx))(v)
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

func (e *engineImpl) retainedMeter(ctx context.Context) Meter {
	meter := MeterFromContext(ctx)
	if meter == nil {
		meter = e.config.engineMeter
	}
	return meter
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
	if chunk.DeepBytes > 0 {
		return chunk.DeepBytes
	}
	bytes := int64(len(chunk.Code))*core.MeterInstructionBytes + core.ValueSlotsBytes(len(chunk.Constants))
	for _, name := range chunk.LocalNames {
		bytes += core.StringShallowBytes(len(name))
	}
	for _, c := range chunk.Constants {
		bytes += core.ValueDeepBytes(c)
	}
	bytes += core.ValueSlotsBytes(len(chunk.SubChunks))
	for _, sub := range chunk.SubChunks {
		bytes += compiledChunkBytes(sub)
	}
	return bytes
}

func (e *engineImpl) Eval(ctx context.Context, source, input string) (result core.Value, err error) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			panicErr := core.NewPanicError(source, r)
			result = nil
			dur := time.Since(start)
			e.stats.recordEval(dur, panicErr)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: panicErr})
			err = fmt.Errorf("eval: %w", panicErr)
		}
	}()

	metered := core.HasEvalMeter(ctx) || e.config.engineMeter != nil
	ctx = e.evalResourceContext(ctx)
	if metered {
		var top bool
		top, err = core.StartEval(ctx)
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

func (e *engineImpl) Call(ctx context.Context, name string, args ...core.Value) (result core.Value, err error) {
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
		defer func() {
			if r := recover(); r != nil {
				result = nil
				err = core.NewPanicError(name, r)
			}
			if err != nil {
				v.Reset()
			}
			be.vmPool.Put(v)
		}()
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
		var top bool
		top, err = core.StartEval(ctx)
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

	active := e.callbacksActive.Load()
	var start time.Time
	if active {
		start = nowFunc()
	}
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = core.NewPanicError(name, r)
		}
		counter.Add(1)
		if active {
			e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: nowFunc().Sub(start)})
		}
	}()
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
	defer func() {
		if r := recover(); r != nil {
			result = nil
			childEnv = nil
			err = core.NewPanicError(source, r)
			dur := time.Since(start)
			e.stats.recordEval(dur, err)
			e.fireEvalCallbacks(EvalEvent{Source: source, Duration: dur, Error: err})
		}
	}()

	metered := core.HasEvalMeter(ctx) || e.config.engineMeter != nil
	ctx = e.evalResourceContext(ctx)
	ctx = core.WithEvalDeadline(ctx, e.evalDeadline(ctx, start))
	if metered {
		var top bool
		top, err = core.StartEval(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
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
	if meter := e.retainedMeter(ctx); meter != nil {
		childEnv.SetRetainedMeter(meter)
	}

	for name, val := range bindings {
		if e.config.dialect.IsLisp2() {
			if err := childEnv.SetBothWithContext(ctx, name, val); err != nil {
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
