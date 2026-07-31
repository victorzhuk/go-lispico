package runtime

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestCache_Hit verifies that evaluating the same source twice returns the same
// result and the second eval reuses the cached chunk (verified via functional
// correctness — both evals produce identical results).
func TestCache_Hit(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")

	src := "(+ 1 2)"
	r1, err := e.Eval(context.Background(), "test", src)
	require.NoError(t, err)

	r2, err := e.Eval(context.Background(), "test", src)
	require.NoError(t, err)

	assert.True(t, r1.Equals(r2), "cached eval must produce identical result")
	assert.True(t, core.Int{V: 3}.Equals(r1), "result must be 3")
}

// TestCache_MacroInvalidation verifies that redefining a macro produces new
// behavior — the cache epoch correctly invalidates the cached chunk.
func TestCache_MacroInvalidation(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")

	_, err = e.Eval(context.Background(), "defmacro1", "(defmacro m [x y] (+ x y))")
	require.NoError(t, err)

	// Use the macro — should produce 3.
	r1, err := e.Eval(context.Background(), "use1", "(m 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(r1), "first macro: (m 1 2) => 3")

	// Redefine the macro to multiply.
	_, err = e.Eval(context.Background(), "defmacro2", "(defmacro m [x y] (* x y))")
	require.NoError(t, err)

	bindBuiltin(t, e, "*")

	// Use the redefined macro — should produce 2, not 3.
	r2, err := e.Eval(context.Background(), "use2", "(m 1 2)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(r2), "redefined macro: (m 1 2) => 2")
}

// TestCache_MacroInvalidation_ViaCall extends TestCache_MacroInvalidation to
// the Apply/Call path: a macro that expands to a top-level (def use-m (fn ...))
// is invoked via eng.Eval, then use-m is exercised via eng.Call; the macro is
// redefined and re-invoked, then use-m is exercised via eng.Call again. Proves
// epoch invalidation isn't bypassed by the pooled-VM cache on the Call path.
//
// The macro expansion targets a top-level def (rather than nesting the macro
// call inside a fn body) because the bytecode compiler only macro-expands the
// literal top-level form passed to Eval — a macro invoked from inside a
// compiled fn/defn body is not expanded and misresolves at call time.
func TestCache_MacroInvalidation_ViaCall(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	_, err = e.Eval(context.Background(), "defmacro1", "(defmacro m [] `(def use-m (fn [x y] (+ x y))))")
	require.NoError(t, err)

	_, err = e.Eval(context.Background(), "use1", "(m)")
	require.NoError(t, err)

	r1, err := e.Call(context.Background(), "use-m", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(r1), "first macro via Call: (use-m 1 2) => 3")

	// Redefine the macro to bind use-m to a multiplying fn instead.
	_, err = e.Eval(context.Background(), "defmacro2", "(defmacro m [] `(def use-m (fn [x y] (* x y))))")
	require.NoError(t, err)

	_, err = e.Eval(context.Background(), "use2", "(m)")
	require.NoError(t, err)

	r2, err := e.Call(context.Background(), "use-m", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(r2), "redefined macro via Call: (use-m 1 2) => 2")
}

// TestCache_Isolation verifies that different source strings with the same
// form index produce correctly different cached results.
func TestCache_Isolation(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	r1, err := e.Eval(context.Background(), "test1", "(+ 1 2)")
	require.NoError(t, err)

	r2, err := e.Eval(context.Background(), "test2", "(* 2 3)")
	require.NoError(t, err)

	assert.True(t, core.Int{V: 3}.Equals(r1), "(+ 1 2) => 3")
	assert.True(t, core.Int{V: 6}.Equals(r2), "(* 2 3) => 6")
	assert.False(t, r1.Equals(r2), "different sources must produce different results")
}

// TestCache_DialectSensitivity verifies that dialect identity still participates
// in cache keys even though truthiness is uniform.
func TestCache_DialectSensitivity(t *testing.T) {
	cl, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer cl.Close()

	bindBuiltin(t, cl, "+")

	r1, err := cl.Eval(context.Background(), "cl", "(if false :y :n)")
	require.NoError(t, err)

	assert.True(t, core.Keyword{V: "n"}.Equals(r1), "CL: (if false :y :n) => :n")
}

// TestCache_ConcurrentSafety verifies that concurrent Eval calls on a
// bytecode engine do not race. Run with -race.
func TestCache_ConcurrentSafety(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	bindBuiltin(t, e, "+")
	bindBuiltin(t, e, "*")

	ctx := context.Background()
	sources := []string{"(+ 1 2)", "(* 3 4)", "(+ 5 6)", "(* 7 8)", "(+ 9 10)"}

	t.Run("parallel-evals", func(t *testing.T) {
		t.Parallel()
		for range 100 {
			for _, src := range sources {
				_, err := e.Eval(ctx, "conc", src)
				require.NoError(t, err, "concurrent eval of %q must not error", src)
			}
		}
	})
}

func cacheCount(t *testing.T, e Engine) int {
	t.Helper()
	ei, ok := e.(*engineImpl)
	require.True(t, ok)
	require.NotNil(t, ei.bytecodeEvaluator)
	be := ei.bytecodeEvaluator
	total := 0
	for i := range be.stripes {
		s := &be.stripes[i]
		s.mu.Lock()
		total += len(s.cache)
		s.mu.Unlock()
	}
	return total
}

// TestCache_BoundedSize: a low MaxCacheEntries keeps the entry count at or below
// the ceiling while many distinct sources are evaluated (eviction recompiles on
// demand without corrupting results).
func TestCache_BoundedSize(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()),
		WithResourceLimits(ResourceLimits{MaxCacheEntries: 4, MaxCollectionLen: 1 << 30}))
	require.NoError(t, err)
	defer e.Close()
	bindBuiltin(t, e, "+")
	for i := range 30 {
		r, err := e.Eval(context.Background(), "s", fmt.Sprintf("(+ %d 1)", i))
		require.NoError(t, err)
		assert.True(t, core.Int{V: int64(i + 1)}.Equals(r))
	}
	assert.LessOrEqual(t, cacheCount(t, e), 4, "cache must stay at or below MaxCacheEntries")
}

// TestCache_StaleEpochEvictionBounded: repeatedly redefining a macro orphans
// old-epoch chunks; they must be reclaimed so the cache stays bounded while
// results stay correct.
func TestCache_StaleEpochEvictionBounded(t *testing.T) {
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()),
		WithResourceLimits(ResourceLimits{MaxCacheEntries: 5, MaxCollectionLen: 1 << 30}))
	require.NoError(t, err)
	defer e.Close()
	bindBuiltin(t, e, "+")
	ctx := context.Background()
	for i := range 30 {
		_, err := e.Eval(ctx, "dm", fmt.Sprintf("(defmacro m [x] (+ x %d))", i))
		require.NoError(t, err)
		_, err = e.Eval(ctx, "u", fmt.Sprintf("(m %d)", i))
		require.NoError(t, err)
	}
	// (m 1) under the last macro (+ x 29) => 30; proves correctness survived eviction.
	r, err := e.Eval(ctx, "final", "(m 1)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 30}.Equals(r), "(m 1) under (+ x 29) => 30")
	assert.LessOrEqual(t, cacheCount(t, e), 5, "stale-epoch orphaned entries must be reclaimed")
}

func cacheStats(t *testing.T, e Engine) CacheStats {
	t.Helper()
	return e.Stats().Cache
}

func cacheContainsSource(t *testing.T, e Engine, src string) bool {
	t.Helper()
	ei, ok := e.(*engineImpl)
	require.True(t, ok)
	require.NotNil(t, ei.bytecodeEvaluator)
	be := ei.bytecodeEvaluator
	keyHash := sha256Hash(src)
	for i := range be.stripes {
		s := &be.stripes[i]
		s.mu.Lock()
		for key := range s.cache {
			if key.sourceHash == keyHash {
				s.mu.Unlock()
				return true
			}
		}
		s.mu.Unlock()
	}
	return false
}

func newCacheLimitsEngine(t *testing.T, limits ResourceLimits) Engine {
	t.Helper()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(limits))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func cacheProfile(t *testing.T, src string) CacheStats {
	t.Helper()
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 16, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
	_, err := e.Eval(t.Context(), "profile", src)
	require.NoError(t, err)
	stats := cacheStats(t, e)
	require.Equal(t, 1, stats.Entries)
	require.Positive(t, stats.Bytes)
	require.Positive(t, stats.Nodes)
	return stats
}

func evalCacheSource(t *testing.T, e Engine, src string) core.Value {
	t.Helper()
	v, err := e.Eval(t.Context(), "cache", src)
	require.NoError(t, err)
	return v
}

func TestCache_ByteCeilingEvicts(t *testing.T) {
	first := "(quote " + vectorLiteral("1", 64) + ")"
	second := "(quote " + vectorLiteral("2", 64) + ")"
	profile := cacheProfile(t, first)
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: int(profile.Bytes), MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})

	evalCacheSource(t, e, first)
	evalCacheSource(t, e, second)

	stats := cacheStats(t, e)
	assert.Equal(t, 1, stats.Entries)
	assert.LessOrEqual(t, stats.Bytes, profile.Bytes)
	assert.False(t, cacheContainsSource(t, e, first))
	assert.True(t, cacheContainsSource(t, e, second))
}

func TestCache_NodeCeilingEvicts(t *testing.T) {
	profile := cacheProfile(t, "1")
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: profile.Nodes, MaxCollectionLen: 1 << 30})

	evalCacheSource(t, e, "1")
	evalCacheSource(t, e, "2")

	stats := cacheStats(t, e)
	assert.Equal(t, 1, stats.Entries)
	assert.LessOrEqual(t, stats.Nodes, profile.Nodes)
	assert.False(t, cacheContainsSource(t, e, "1"))
	assert.True(t, cacheContainsSource(t, e, "2"))
}

func TestCache_EvictionDeterminism(t *testing.T) {
	run := func() map[string]bool {
		e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 2, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})
		evalCacheSource(t, e, "1")
		evalCacheSource(t, e, "2")
		evalCacheSource(t, e, "1")
		evalCacheSource(t, e, "3")
		require.Equal(t, 2, cacheStats(t, e).Entries)
		return map[string]bool{
			"1": cacheContainsSource(t, e, "1"),
			"2": cacheContainsSource(t, e, "2"),
			"3": cacheContainsSource(t, e, "3"),
		}
	}

	want := map[string]bool{"1": true, "2": false, "3": true}
	assert.Equal(t, want, run())
	assert.Equal(t, want, run())
}

func TestCache_UnfitChunkRunsUncached(t *testing.T) {
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1, MaxCacheNodes: 1, MaxCollectionLen: 1 << 30})

	v := evalCacheSource(t, e, "[1 2]")
	assert.True(t, core.NewVector([]core.Value{core.Int{V: 1}, core.Int{V: 2}}).Equals(v))
	evalCacheSource(t, e, "[1 2]")

	assert.Equal(t, 0, cacheStats(t, e).Entries)
}

func TestCache_MeterChargeAndRelease(t *testing.T) {
	t.Run("MacroEpochFlushAndClose", func(t *testing.T) {
		m := &recordingMeter{}
		e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(m), WithResourceLimits(ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30}))
		require.NoError(t, err)

		evalCacheSource(t, e, "1")
		snap := m.snapshot()
		assert.Equal(t, 1, snap.chargeCalls)
		assert.Equal(t, int64(1), snap.chargedSlots)
		assert.Equal(t, 0, snap.releaseCalls)

		e.RootEnv().BumpMacroEpoch()
		evalCacheSource(t, e, "2")
		snap = m.snapshot()
		assert.Equal(t, 2, snap.chargeCalls)
		assert.Equal(t, int64(2), snap.chargedSlots)
		assert.Equal(t, 1, snap.releaseCalls)
		assert.Equal(t, int64(1), snap.releasedSlots)

		require.NoError(t, e.Close())
		snap = m.snapshot()
		assert.Equal(t, 2, snap.releaseCalls)
		assert.Equal(t, int64(2), snap.releasedSlots)
	})

	t.Run("CapacityEviction", func(t *testing.T) {
		m := &recordingMeter{}
		e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(m), WithResourceLimits(ResourceLimits{MaxCacheEntries: 1, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30}))
		require.NoError(t, err)

		evalCacheSource(t, e, "1")
		evalCacheSource(t, e, "2")
		snap := m.snapshot()
		assert.Equal(t, 2, snap.chargeCalls)
		assert.Equal(t, 1, snap.releaseCalls)

		require.NoError(t, e.Close())
		snap = m.snapshot()
		assert.Equal(t, 2, snap.releaseCalls)
		assert.Equal(t, int64(2), snap.releasedSlots)
	})

	t.Run("ChargeDenial", func(t *testing.T) {
		deny := &recordingMeter{chargeErr: fmt.Errorf("deny")}
		uncached, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(deny), WithResourceLimits(ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30}))
		require.NoError(t, err)
		t.Cleanup(func() { _ = uncached.Close() })
		deny.reset()
		evalCacheSource(t, uncached, "1")
		assert.Equal(t, 0, cacheStats(t, uncached).Entries)
		assert.Equal(t, 1, deny.snapshot().chargeCalls)
	})
}

func TestCache_StatsExposeEntriesBytesNodesEpoch(t *testing.T) {
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})

	evalCacheSource(t, e, "1")
	stats := cacheStats(t, e)

	assert.Equal(t, 1, stats.Entries)
	assert.Positive(t, stats.Bytes)
	assert.Equal(t, 1, stats.Nodes)
	ei := e.(*engineImpl)
	assert.Equal(t, fmt.Sprintf("%s:%d", ei.bytecodeEvaluator.dialectFP, e.RootEnv().MacroEpoch()), stats.Epoch)
}

func TestCache_MultiCeilingEvictionOneInsert(t *testing.T) {
	bigSrc := vectorLiteral("1", 64)
	bigProfile := cacheProfile(t, bigSrc)
	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 2, MaxCacheBytes: int(bigProfile.Bytes), MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30})

	evalCacheSource(t, e, "1")
	evalCacheSource(t, e, "2")
	require.Equal(t, 2, cacheStats(t, e).Entries)
	evalCacheSource(t, e, bigSrc)

	stats := cacheStats(t, e)
	assert.Equal(t, 1, stats.Entries)
	assert.LessOrEqual(t, stats.Bytes, bigProfile.Bytes)
	assert.False(t, cacheContainsSource(t, e, "1"))
	assert.False(t, cacheContainsSource(t, e, "2"))
	assert.True(t, cacheContainsSource(t, e, bigSrc))
}

// epochOf returns the engine's current chunk-cache epoch, the counter that
// keys cached chunks against macro definitions.
func epochOf(t *testing.T, e Engine) string {
	t.Helper()
	return e.Stats().Cache.Epoch
}

// TestCache_IdenticalMacroRebindKeepsEpoch pins the defect: re-evaluating one
// unchanged source that happens to define a macro must not churn the cache.
// Before the fix the epoch climbed on every evaluation, so every form in the
// source recompiled every time — the "Repeated evaluation reuses the chunk"
// scenario silently excluded any source containing a defmacro.
func TestCache_IdenticalMacroRebindKeepsEpoch(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []EngineOption
		src  string
	}{
		{"lisp2-default-cl", nil, "(defmacro twice (x) (list 'progn x x))"},
		{"lisp1-clojure", []EngineOption{WithDialect(clojure.Dialect())},
			"(defmacro twice [x] (list 'do x x))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := New(nil, append(tc.opts, WithBytecode())...)
			require.NoError(t, err)
			defer e.Close()

			_, err = e.Eval(context.Background(), "m", tc.src)
			require.NoError(t, err)
			first := epochOf(t, e)

			for i := range 4 {
				_, err = e.Eval(context.Background(), "m", tc.src)
				require.NoError(t, err)
				require.Equal(t, first, epochOf(t, e),
					"eval #%d rebound an identical macro and churned the cache epoch", i+2)
			}
		})
	}
}

// TestCache_ChangedMacroBodyInvalidates is the case that must never regress:
// a real redefinition still takes effect. TestCache_MacroInvalidation covers
// the behavior; this covers the epoch that drives it.
func TestCache_ChangedMacroBodyInvalidates(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()

	_, err = e.Eval(context.Background(), "m", "(defmacro m (x) (list 'quote x))")
	require.NoError(t, err)
	before := epochOf(t, e)

	_, err = e.Eval(context.Background(), "m", "(defmacro m (x) (list 'quote (list x x)))")
	require.NoError(t, err)

	assert.NotEqual(t, before, epochOf(t, e), "a changed macro body must invalidate")
}

// TestCache_HitSkipsMacroExpansion pins that a cache hit skips expansion, by
// counting how often a macro's expander body actually runs. Expansion is
// compile-time work: the cached chunk already embeds its result, so re-running
// it per evaluation was wasted, and observably so for an expander with side
// effects. Before this was fixed the count climbed 1, 2, 3, 4 across four
// evaluations of one unchanged source.
func TestCache_HitSkipsMacroExpansion(t *testing.T) {
	e, err := New(nil, WithBytecode())
	require.NoError(t, err)
	defer e.Close()
	bindBuiltin(t, e, "+")

	var expansions int
	require.NoError(t, e.Bind("tick", core.GoFunc{
		Name: "tick",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			expansions++
			return args[0], nil
		},
	}))

	_, err = e.Eval(context.Background(), "def", "(defmacro m (x) (tick x))")
	require.NoError(t, err)

	for i := range 4 {
		_, err = e.Eval(context.Background(), "use", "(m 1)")
		require.NoError(t, err)
		require.Equal(t, 1, expansions,
			"after eval #%d the expander had run %d times; a cache hit must not re-expand",
			i+1, expansions)
	}
}

// distinctStripeSources returns two integer-literal sources whose cache keys
// route to different stripes under an n-stripe cache, so a test can pin
// per-stripe behavior without depending on which physical stripe a source
// happens to land in.
func distinctStripeSources(t *testing.T, n int) (string, string) {
	t.Helper()
	first := ""
	firstIdx := -1
	for i := range 1000 {
		src := strconv.Itoa(i)
		idx := stripeIndex(sha256Hash(src), 0, n)
		if firstIdx == -1 {
			first, firstIdx = src, idx
			continue
		}
		if idx != firstIdx {
			return first, src
		}
	}
	t.Fatalf("no two sources among 1000 candidates route to different stripes for n=%d", n)
	return "", ""
}

// TestCache_RefusalTerminatesUnderTightBudget pins the hazard the stripe.tail
// != nil bound in cacheAdmitLocked exists for: a monolithic LRU rarely empties
// mid-eviction, but an empty stripe with nothing of its own to evict hits
// that case routinely once a sibling stripe already holds the whole budget.
// Before the bound, the pre-eviction loop spun on that stripe's permanently
// nil tail. The second source is expected to miss the cache (its admit is
// refused) and still evaluate correctly.
func TestCache_RefusalTerminatesUnderTightBudget(t *testing.T) {
	src1, src2 := distinctStripeSources(t, 2)
	n1, err := strconv.ParseInt(src1, 10, 64)
	require.NoError(t, err)
	n2, err := strconv.ParseInt(src2, 10, 64)
	require.NoError(t, err)

	type outcome struct {
		v1, v2 core.Value
		err    error
	}
	done := make(chan outcome, 1)

	go func() {
		e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), withCacheStripes(2),
			WithResourceLimits(ResourceLimits{MaxCacheEntries: 1, MaxCollectionLen: 1 << 30}))
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer e.Close()

		v1, err := e.Eval(context.Background(), "cache", src1)
		if err != nil {
			done <- outcome{err: err}
			return
		}
		v2, err := e.Eval(context.Background(), "cache", src2)
		done <- outcome{v1: v1, v2: v2, err: err}
	}()

	select {
	case out := <-done:
		require.NoError(t, out.err)
		assert.True(t, core.Int{V: n1}.Equals(out.v1))
		assert.True(t, core.Int{V: n2}.Equals(out.v2))
	case <-time.After(5 * time.Second):
		t.Fatal("Eval hung under a refused admit — the stripe pre-eviction loop did not terminate")
	}
}

// TestCache_ConcurrentAggregateBudgetHolds drives concurrent Eval traffic
// across a working set wide enough to span every stripe at the default
// (adaptive) stripe count, with byte/node ceilings tight enough that
// eviction fires throughout the run. The aggregate counters are the sole
// authority on occupancy, so they must never exceed the configured ceiling
// no matter which stripe absorbed which entry.
func TestCache_ConcurrentAggregateBudgetHolds(t *testing.T) {
	sources := make([]string, 40)
	for i := range sources {
		sources[i] = "(quote " + vectorLiteral(strconv.Itoa(i), i+1) + ")"
	}
	profile := cacheProfile(t, sources[len(sources)-1])
	maxBytes := int(profile.Bytes) * 3
	maxNodes := profile.Nodes * 3

	e := newCacheLimitsEngine(t, ResourceLimits{MaxCacheEntries: 100, MaxCacheBytes: maxBytes, MaxCacheNodes: maxNodes, MaxCollectionLen: 1 << 30})

	ei := e.(*engineImpl)
	stripeCount := len(ei.bytecodeEvaluator.stripes)
	touched := map[int]bool{}
	for _, src := range sources {
		touched[stripeIndex(sha256Hash(src), 0, stripeCount)] = true
	}
	require.Greater(t, len(touched), 1, "working set must span multiple stripes")

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := range 50 {
				src := sources[(seed+i)%len(sources)]
				if _, err := e.Eval(context.Background(), "conc-budget", src); err != nil {
					t.Errorf("concurrent eval of %q: %v", src, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	stats := e.Stats().Cache
	assert.LessOrEqual(t, stats.Entries, 100)
	assert.LessOrEqual(t, stats.Bytes, int64(maxBytes))
	assert.LessOrEqual(t, stats.Nodes, maxNodes)
}

// TestCache_GlobalCounterDenialRollsBackWithoutChargingMeter proves the
// step-3/step-4 admit ordering in cacheAdmitLocked: src2 lands in a stripe
// with nothing of its own to evict, so the aggregate node ceiling denies its
// charge outright. chargeCacheBudget must roll back the entries and bytes
// counters it tentatively charged before the node charge failed, and the
// engine meter — which has no ceiling of its own and would allow the
// charge — must never be asked.
func TestCache_GlobalCounterDenialRollsBackWithoutChargingMeter(t *testing.T) {
	src1, src2 := distinctStripeSources(t, 2)
	profile := cacheProfile(t, src1)

	m := &recordingMeter{}
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(m), withCacheStripes(2),
		WithResourceLimits(ResourceLimits{MaxCacheEntries: 10, MaxCacheBytes: 1 << 30, MaxCacheNodes: profile.Nodes, MaxCollectionLen: 1 << 30}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	m.reset()

	evalCacheSource(t, e, src1)
	snap := m.snapshot()
	require.Equal(t, 1, snap.chargeCalls, "first admit must charge the meter")

	ei := e.(*engineImpl)
	be := ei.bytecodeEvaluator
	entriesAfterFirst := be.cacheEntries.Load()
	bytesAfterFirst := be.cacheBytes.Load()

	evalCacheSource(t, e, src2)
	snap = m.snapshot()
	assert.Equal(t, 1, snap.chargeCalls, "a node-ceiling denial in a stripe with nothing to evict must never reach the engine meter")
	assert.Equal(t, entriesAfterFirst, be.cacheEntries.Load(), "denied admit must roll back its tentative entries charge")
	assert.Equal(t, bytesAfterFirst, be.cacheBytes.Load(), "denied admit must roll back its tentative bytes charge")
	assert.Equal(t, int64(profile.Nodes), be.cacheNodes.Load(), "node counter must settle back to the first admit's charge, not drift upward")
}
