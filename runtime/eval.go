package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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

// vmSlotLeaseProbe, when non-nil, is invoked on every lean-path VM slot
// claim attempt with whether the engine's private VM was claimed (true) or
// the caller fell back to the pool (false). Test hook only — production
// leaves it nil, so the steady path pays one predictable nil check. Tests
// must publish it before starting goroutines and restore it after they join.
var vmSlotLeaseProbe func(claimed bool)

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

// cacheStripeThreshold and cacheStripeCount implement the chunk cache's
// adaptive striping rule: an engine whose cache holds a handful of entries
// gains nothing from partitioning and would only lose global LRU victim
// identity, so it stays a single stripe. A production-sized cache (the
// default ceiling is 4096 entries) partitions eight ways to remove
// bytecodeEvaluator's single mutex as a cross-core serialization point.
// Both values are read only through resolveCacheStripes; the stripe count
// must stay a power of two because stripeIndex routes with a mask.
const (
	cacheStripeThreshold = 64
	cacheStripeCount     = 8
)

// resolveCacheStripes applies the adaptive rule, honoring override when set
// (>0) so benchmarks can compare stripe counts within one binary.
func resolveCacheStripes(maxEntries, override int) int {
	if override > 0 {
		return override
	}
	if maxEntries < cacheStripeThreshold {
		return 1
	}
	return cacheStripeCount
}

// withCacheStripes overrides the chunk cache's stripe count instead of
// deriving it from resolveCacheStripes. Test hook only — production leaves
// it unset (0) and takes the adaptive rule; callers must pass a power of two.
func withCacheStripes(n int) EngineOption {
	return func(cfg *engineConfig) {
		cfg.cacheStripes = n
	}
}

// cacheStripe is one partition of the chunk cache: its own map and its own
// LRU list, guarded by its own mutex. bytecodeEvaluator's aggregate budget
// counters are the sole authority on total occupancy across every stripe —
// a stripe never enforces a fraction of the ceiling on its own.
type cacheStripe struct {
	mu    sync.Mutex
	cache map[cacheKey]*cacheEntry
	head  *cacheEntry
	tail  *cacheEntry
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
	stripes            []cacheStripe
	cacheEntries       atomic.Int64
	cacheBytes         atomic.Int64
	cacheNodes         atomic.Int64
	lastFlushedEpoch   atomic.Int64
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

func newBytecodeEvaluator(globals *core.Env, maxDepth int, timeout time.Duration, limits ResourceLimits, treeWalker core.Evaluator, dialect core.Dialect, meter Meter, stripeOverride int) *bytecodeEvaluator {
	// Stripe maps are lazily allocated on each stripe's first admit (see
	// cacheAdmitLocked) rather than here, so an engine that never populates
	// the chunk cache pays for the stripes slice alone, not N empty maps.
	stripes := make([]cacheStripe, resolveCacheStripes(limits.MaxCacheEntries, stripeOverride))
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
		stripes:            stripes,
	}
	be.vmPool = sync.Pool{
		New: func() any {
			return vm.New(globals, vm.WithMaxDepth(maxDepth), vm.WithEvaluator(be), vm.WithMaxStructuralDepth(be.maxStructuralDepth))
		},
	}
	return be
}

// stripeFor routes a cache key to its stripe using only sourceHash and
// formIndex — never macroEpoch, which is deliberately excluded: including it
// would scatter a source's stale-epoch entry into a different stripe than
// its fresh replacement, so a miss could not find its own stale sibling
// co-located in the stripe it is about to write. dialectFP is constant per
// engine and contributes no entropy, so it is excluded too. cacheKey itself
// stays the exact map lookup key; routing and key equality are separate concerns.
func (be *bytecodeEvaluator) stripeFor(key cacheKey) *cacheStripe {
	return &be.stripes[stripeIndex(key.sourceHash, key.formIndex, len(be.stripes))]
}

func stripeIndex(h sourceHash, formIndex, n int) int {
	if n == 1 {
		return 0
	}
	mixed := binary.LittleEndian.Uint64(h[:8]) ^ uint64(formIndex)
	return int(mixed & uint64(n-1))
}

func (s *cacheStripe) cacheGetLocked(key cacheKey) (*vm.Chunk, bool) {
	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	s.moveCacheEntryToHeadLocked(entry)
	return entry.chunk, true
}

// cacheAdmitLocked admits chunk into stripe s. The aggregate ceiling holds
// exactly, with no tolerance term: cacheFitsAlone checks the true undivided
// limits (never divided by stripe count), bounded local pre-eviction makes
// room by evicting only from s's own tail, and the CAS-charge against
// bytecodeEvaluator's global counters is the only way an entry becomes
// visible — refusal is the sole failure mode, nothing is ever inserted
// speculatively.
func (be *bytecodeEvaluator) cacheAdmitLocked(s *cacheStripe, key cacheKey, chunk *vm.Chunk) bool {
	if !be.cacheFitsAlone(chunk) {
		return false
	}
	// s.tail != nil bounds the loop: one big LRU rarely empties mid-eviction,
	// but an N-way split one does routinely once other stripes hold the budget.
	for s.tail != nil && be.cacheBudgetExceeded(chunk) {
		be.evictCacheEntryLocked(s, s.tail)
	}
	if err := be.chargeCacheBudget(chunk); err != nil {
		return false
	}
	if be.engineMeter != nil {
		if err := be.engineMeter.ChargeRetained(chunk.DeepBytes, 1); err != nil {
			be.releaseCacheBudget(chunk.DeepBytes, chunk.NodeCount)
			return false
		}
	}
	if s.cache == nil {
		s.cache = make(map[cacheKey]*cacheEntry)
	}
	entry := &cacheEntry{key: key, chunk: chunk}
	s.cache[key] = entry
	s.insertCacheEntryHeadLocked(entry)
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

// cacheBudgetExceeded reports whether admitting chunk would push any of the
// three global counters past its ceiling — the same used+n>max test
// chargeCacheBudget applies, checked ahead of time so pre-eviction can make
// room before the charge is attempted.
func (be *bytecodeEvaluator) cacheBudgetExceeded(chunk *vm.Chunk) bool {
	return be.cacheEntries.Load()+1 > int64(be.maxCacheEntries) ||
		be.cacheBytes.Load()+chunk.DeepBytes > int64(be.maxCacheBytes) ||
		be.cacheNodes.Load()+int64(chunk.NodeCount) > int64(be.maxCacheNodes)
}

// chargeCacheBudget CAS-charges the three global counters in order,
// rolling back what already succeeded when a later charge fails — the same
// bytes-then-slots rollback shape as limitMeter.ChargeRetained in meter.go.
func (be *bytecodeEvaluator) chargeCacheBudget(chunk *vm.Chunk) error {
	if err := chargeCounter(&be.cacheEntries, int64(be.maxCacheEntries), 1); err != nil {
		return err
	}
	if err := chargeCounter(&be.cacheBytes, int64(be.maxCacheBytes), chunk.DeepBytes); err != nil {
		returnCounter(&be.cacheEntries, 1)
		return err
	}
	if err := chargeCounter(&be.cacheNodes, int64(be.maxCacheNodes), int64(chunk.NodeCount)); err != nil {
		returnCounter(&be.cacheBytes, chunk.DeepBytes)
		returnCounter(&be.cacheEntries, 1)
		return err
	}
	return nil
}

func (be *bytecodeEvaluator) releaseCacheBudget(bytes int64, nodes int) {
	returnCounter(&be.cacheEntries, 1)
	returnCounter(&be.cacheBytes, bytes)
	returnCounter(&be.cacheNodes, int64(nodes))
}

func (s *cacheStripe) insertCacheEntryHeadLocked(entry *cacheEntry) {
	entry.prev = nil
	entry.next = s.head
	if s.head != nil {
		s.head.prev = entry
	} else {
		s.tail = entry
	}
	s.head = entry
}

func (s *cacheStripe) moveCacheEntryToHeadLocked(entry *cacheEntry) {
	if entry == s.head {
		return
	}
	s.unlinkCacheEntryLocked(entry)
	s.insertCacheEntryHeadLocked(entry)
}

func (s *cacheStripe) unlinkCacheEntryLocked(entry *cacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		s.head = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		s.tail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

func (be *bytecodeEvaluator) evictCacheEntryLocked(s *cacheStripe, entry *cacheEntry) {
	if entry == nil {
		return
	}
	delete(s.cache, entry.key)
	s.unlinkCacheEntryLocked(entry)
	be.releaseCacheBudget(entry.chunk.DeepBytes, entry.chunk.NodeCount)
	if be.engineMeter != nil {
		be.engineMeter.ReleaseRetained(entry.chunk.DeepBytes, 1)
	}
}

// flushCacheEpochLocked reclaims entries older than epoch. The test is < and
// not != because a sweep for an older epoch can still be walking stripes when
// a redefinition bumps past it: != would let that in-flight sweep delete an
// entry a newer epoch just admitted into a stripe it had not yet reached.
func (be *bytecodeEvaluator) flushCacheEpochLocked(s *cacheStripe, epoch int) {
	for entry := s.tail; entry != nil; {
		prev := entry.prev
		if entry.key.macroEpoch < epoch {
			be.evictCacheEntryLocked(s, entry)
		}
		entry = prev
	}
}

// flushStaleEpoch reclaims stale-epoch entries across every stripe, gated by
// one CAS on lastFlushedEpoch so exactly one caller per epoch bump pays the
// O(total entries) sweep; losers skip straight to their own admit.
// Correctness never depends on the sweep running promptly, or at all: epoch
// is part of cacheKey, so a probe for the current epoch can never return a
// stale-epoch entry regardless of whether reclamation has caught up. The
// sweep is reclamation only, never hit/miss correctness.
func (be *bytecodeEvaluator) flushStaleEpoch(epoch int64) {
	for {
		prev := be.lastFlushedEpoch.Load()
		if prev >= epoch {
			return
		}
		if !be.lastFlushedEpoch.CompareAndSwap(prev, epoch) {
			continue
		}
		for i := range be.stripes {
			st := &be.stripes[i]
			st.mu.Lock()
			be.flushCacheEpochLocked(st, int(epoch))
			st.mu.Unlock()
		}
		return
	}
}

func (be *bytecodeEvaluator) flushCache() {
	for i := range be.stripes {
		s := &be.stripes[i]
		s.mu.Lock()
		for s.tail != nil {
			be.evictCacheEntryLocked(s, s.tail)
		}
		s.mu.Unlock()
	}
}

func (be *bytecodeEvaluator) cacheStats() CacheStats {
	return CacheStats{
		Entries: int(be.cacheEntries.Load()),
		Bytes:   be.cacheBytes.Load(),
		Nodes:   int(be.cacheNodes.Load()),
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

	s := be.stripeFor(key)

	s.mu.Lock()
	chunk, hit := s.cacheGetLocked(key)
	s.mu.Unlock()

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
		be.flushStaleEpoch(int64(currentEpoch))

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

		s.mu.Lock()
		if cached, dup := s.cacheGetLocked(key); dup {
			chunk = cached
			hit = true
		} else if be.cacheAdmitLocked(s, key, chunk) {
			hit = false
		}
		s.mu.Unlock()
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
// "unsupported in bytecode" error (a defmacro nested inside a larger form, unquote-splicing),
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

	// The lean-boundary condition is derived ONCE here and threaded down:
	// callBoundary/applyOnVM never re-probe it on the fast path.
	fast := e.fastPath.Load() && !core.HasEvalState(ctx) && !core.HasEvalMeter(ctx)

	var env *core.Env
	if fast {
		env = e.rootEnvPtr.Load()
	} else {
		e.mu.RLock()
		env = e.rootEnv
		e.mu.RUnlock()
	}

	entry := e.callCache.lookup(name, env)
	var fn core.Value
	var live bool
	if entry != nil {
		if fast {
			// Lock-free versioned read: every cell mutation bumps version
			// under the env write lock, so a match proves the cached value
			// is still live. Mismatch (redefinition, tombstone, hot-reload)
			// falls through to the locked re-read below.
			if entry.cell.Version() == entry.cellVer && entry.value != nil {
				fn, live = entry.value, true
			}
		} else {
			fn, live, _ = env.ReadCell(entry.cell)
		}
	}
	if entry == nil || !live {
		// A cache hit whose cell has since gone tombstoned is not
		// necessarily undefined: under Lisp-2 a different, still-live cell
		// (the value-cell fallback) may now be the right answer, exactly as
		// fresh resolution would find. Drop the dead entry and re-resolve in
		// full before giving up.
		if entry != nil {
			e.callCache.drop(name)
		}
		var ok bool
		if entry, ok = e.resolveCallEntry(env, name); !ok {
			return nil, e.reportUndefinedCall(name, e.stats.counterFor(name))
		}
		if fn, live, _ = env.ReadCell(entry.cell); !live {
			return nil, e.reportUndefinedCall(name, entry.counter)
		}
	}
	if be := e.bytecodeEvaluator; be != nil {
		if fast {
			v, slot := e.acquireCallVM(be)
			return e.callBoundaryLean(ctx, name, fn, env, entry.counter, args, v, be, slot)
		}
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
		return e.callBoundary(ctx, name, fn, env, entry.counter, args, v, false)
	}
	return e.callBoundary(ctx, name, fn, env, entry.counter, args, nil, false)
}

// reportUndefinedCall is Call's single undefined-function tail: bump the
// counter, fire the undefined-callback event, and return the error string
// both the cache-miss and cache-hit-but-tombstoned paths report identically.
func (e *engineImpl) reportUndefinedCall(name string, counter *atomic.Int64) error {
	counter.Add(1)
	if e.callbacksActive.Load() {
		e.firePluginCallbacks(PluginCallEvent{Function: name, Duration: 0})
	}
	return fmt.Errorf("undefined function: %s", name)
}

// callBoundary is the unified Engine.Call fast path: it arms timing, dispatches
// to the bytecode applyOnVM (or the tree-walker when bytecode is off), and
// fires stats/callbacks around the apply. The caller supplies v — a pool-
// acquired VM for Engine.Call and Fn.Call, or a private handle-owned VM for
// PinnedFn.Call — so the per-call VM lifecycle stays in the caller and a
// single apply step is shared across every entry point.
func (e *engineImpl) callBoundary(ctx context.Context, name string, fn core.Value, env *core.Env, counter *atomic.Int64, args []core.Value, v *vm.VM, fast bool) (result core.Value, err error) {
	needsEvalState := !fast && (core.HasEvalState(ctx) || core.HasEvalMeter(ctx) || e.config.engineMeter != nil)
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

	active := !fast && e.callbacksActive.Load()
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

// callBoundaryLean is the lean spine for fast-condition calls: one
// recover-defer covering VM release, panic→NewPanicError, and the stats
// bump. No StartEval/FinishEval, no clock reads, no callbacks — the fast
// flag guarantees none are attached and the entry check guarantees the
// context carries no eval state or meter, so applyOnVM takes its plain
// timeout/limits arm exactly as the general path would.
func (e *engineImpl) callBoundaryLean(ctx context.Context, name string, fn core.Value, env *core.Env, counter *atomic.Int64, args []core.Value, v *vm.VM, be *bytecodeEvaluator, slot bool) (result core.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = core.NewPanicError(name, r)
		}
		e.releaseCallVM(v, be, slot, err)
		counter.Add(1)
	}()
	return be.applyOnVM(v, ctx, fn, args, env, e.config.timeout)
}

// acquireCallVM claims the engine's private VM slot for a lean call; when
// the slot is busy (concurrent Call in flight) the caller falls back to a
// fully-reset pool VM. Mirrors PinnedFn's CAS inUse claim.
func (e *engineImpl) acquireCallVM(be *bytecodeEvaluator) (v *vm.VM, slot bool) {
	if e.vmSlotInUse.CompareAndSwap(false, true) {
		if e.vmSlot == nil {
			e.vmSlot = vm.New(be.globals, vm.WithMaxDepth(be.maxDepth), vm.WithEvaluator(be), vm.WithMaxStructuralDepth(be.maxStructuralDepth))
		}
		if probe := vmSlotLeaseProbe; probe != nil {
			probe(true)
		}
		return e.vmSlot, true
	}
	if probe := vmSlotLeaseProbe; probe != nil {
		probe(false)
	}
	v = be.vmPool.Get().(*vm.VM)
	v.Reset()
	return v, false
}

// releaseCallVM returns a lean-call VM: a clean exit from the engine slot
// keeps only the incremental reset (the run/apply loop already restored the
// stacks), while an error or panic — or any invariant violation — gets a
// full Reset before release. An invariant violation falls back to a full
// Reset without surfacing an error: the call already succeeded, unlike
// PinnedFn, whose handle contract reports it via NewVMStateError. Pool VMs
// always return fully reset on error, matching the general path.
func (e *engineImpl) releaseCallVM(v *vm.VM, be *bytecodeEvaluator, slot bool, callErr error) {
	if !slot {
		if callErr != nil {
			v.Reset()
		}
		be.vmPool.Put(v)
		return
	}
	if callErr != nil {
		v.Reset()
	} else if resetErr := v.ResetIncremental(); resetErr != nil {
		v.Reset()
	}
	e.vmSlotInUse.Store(false)
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
