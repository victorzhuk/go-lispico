package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
)

// Each scenario below runs twice: once "cold" (the mutation happens before
// any Call, so the cache never held a stale entry) and once "warm" (a Call
// runs first to populate the cache, then the mutation happens). Both runs
// must observe identical results — proving the cache never causes Call to
// see anything a fresh resolution wouldn't.

func TestCallCache_RedefineThenCall(t *testing.T) {
	run := func(t *testing.T, warmCache bool) (core.Value, error) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		ctx := t.Context()

		_, err = eng.Eval(ctx, "def-f", "(defun f (x) :first)")
		require.NoError(t, err)
		if warmCache {
			_, err = eng.Call(ctx, "f", core.Int{V: 1})
			require.NoError(t, err)
		}
		_, err = eng.Eval(ctx, "redef-f", "(defun f (x) :second)")
		require.NoError(t, err)
		return eng.Call(ctx, "f", core.Int{V: 1})
	}

	cold, coldErr := run(t, false)
	warm, warmErr := run(t, true)
	require.NoError(t, coldErr)
	require.NoError(t, warmErr)
	assert.True(t, core.Keyword{V: "second"}.Equals(cold), "cold: got %v", cold)
	assert.True(t, core.Keyword{V: "second"}.Equals(warm), "warm: got %v", warm)
}

func TestCallCache_DeleteThenCall(t *testing.T) {
	run := func(t *testing.T, warmCache bool) error {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		ctx := t.Context()

		_, err = eng.Eval(ctx, "def-f", "(defun f () :ok)")
		require.NoError(t, err)
		if warmCache {
			_, err = eng.Call(ctx, "f")
			require.NoError(t, err)
		}
		eng.(*engineImpl).rootEnv.Delete("f")
		_, callErr := eng.Call(ctx, "f")
		return callErr
	}

	coldErr := run(t, false)
	warmErr := run(t, true)
	require.EqualError(t, coldErr, "undefined function: f")
	require.EqualError(t, warmErr, "undefined function: f")
}

// TestCallCache_DeleteRebuildRedefineThenCall covers the one case that
// actually exercises the generation guard: Rebuild drops the tombstoned
// cell from the env's map, so a later redefine allocates a brand-new *Cell.
// A cache entry taken before Rebuild points at the orphaned cell, which
// Rebuild leaves permanently tombstoned — without the {env, gen} guard the
// warm run would report "undefined function: f" for a name that now exists.
// Manually verified: with the generation check in callCache.lookup commented
// out, the warm subtest below fails with exactly that error while cold still
// returns :second; restored the check afterward.
func TestCallCache_DeleteRebuildRedefineThenCall(t *testing.T) {
	run := func(t *testing.T, warmCache bool) (core.Value, error) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		ctx := t.Context()

		_, err = eng.Eval(ctx, "def-f", "(defun f () :first)")
		require.NoError(t, err)
		if warmCache {
			_, err = eng.Call(ctx, "f")
			require.NoError(t, err)
		}
		impl := eng.(*engineImpl)
		impl.rootEnv.Delete("f")
		impl.rootEnv.Rebuild()
		_, err = eng.Eval(ctx, "redef-f", "(defun f () :second)")
		require.NoError(t, err)
		return eng.Call(ctx, "f")
	}

	cold, coldErr := run(t, false)
	warm, warmErr := run(t, true)
	require.NoError(t, coldErr)
	require.NoError(t, warmErr)
	assert.True(t, core.Keyword{V: "second"}.Equals(cold), "cold: got %v", cold)
	assert.True(t, core.Keyword{V: "second"}.Equals(warm), "warm: got %v", warm)
}

func TestCallCache_UnloadThenCall(t *testing.T) {
	run := func(t *testing.T, warmCache bool) error {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		p := &bindingPlugin{name: t.Name(), funcs: []string{"greet"}}
		require.NoError(t, eng.Use(p))
		ctx := t.Context()
		if warmCache {
			_, err = eng.Call(ctx, "greet")
			require.NoError(t, err)
		}
		require.NoError(t, eng.UnloadPlugin(t.Name()))
		_, callErr := eng.Call(ctx, "greet")
		return callErr
	}

	coldErr := run(t, false)
	warmErr := run(t, true)
	require.EqualError(t, coldErr, "undefined function: greet")
	require.EqualError(t, warmErr, "undefined function: greet")
}

func TestCallCache_HotReloadThenCall(t *testing.T) {
	run := func(t *testing.T, warmCache bool) (core.Value, error) {
		dir := t.TempDir()
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })
		ctx := t.Context()

		_, err = eng.Eval(ctx, "def-f", "(defun f () :first)")
		require.NoError(t, err)
		if warmCache {
			_, err = eng.Call(ctx, "f")
			require.NoError(t, err)
		}

		impl := eng.(*engineImpl)
		w := newFileWatcher(impl, dir, 10*time.Millisecond)
		w.ctx = t.Context()
		file := filepath.Join(dir, "reload.lisp")
		require.NoError(t, os.WriteFile(file, []byte("(defun f () :second)"), 0o644))
		w.reloadFile(file)

		return eng.Call(ctx, "f")
	}

	cold, coldErr := run(t, false)
	warm, warmErr := run(t, true)
	require.NoError(t, coldErr)
	require.NoError(t, warmErr)
	assert.True(t, core.Keyword{V: "second"}.Equals(cold), "cold: got %v", cold)
	assert.True(t, core.Keyword{V: "second"}.Equals(warm), "warm: got %v", warm)
}

// TestCallCache_Lisp2FunctionCellReachable proves the newly-reachable case:
// under CL (Lisp-2), "f" is bound with defun ONLY — the function cell — so a
// successful Call here can only have resolved through resolveCallCell's
// FuncCell branch, not the old env.Get value-cell walk.
func TestCallCache_Lisp2FunctionCellReachable(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
	require.NoError(t, err)

	got, err := eng.Call(ctx, "f", core.Int{V: 9})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 9}.Equals(got), "got %v", got)
}

// TestCallCache_Lisp2PrecedenceMatchesHeadPosition binds "f" both ways under
// CL: def in the value cell, defun in the function cell. Call must reach the
// defun binding, exactly matching what (f 1) resolves to in head position
// (core/eval.go resolveHead: function cell first).
func TestCallCache_Lisp2PrecedenceMatchesHeadPosition(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-f", "(def f (fn (x) :value-cell))")
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "defun-f", "(defun f (x) :function-cell)")
	require.NoError(t, err)

	viaCall, err := eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)
	viaHeadPosition, err := eng.Eval(ctx, "head-call", "(f 1)")
	require.NoError(t, err)

	assert.True(t, core.Keyword{V: "function-cell"}.Equals(viaCall), "Call must resolve the function cell, got %v", viaCall)
	assert.True(t, viaHeadPosition.Equals(viaCall), "Call must match head-position resolution exactly, got %v vs %v", viaCall, viaHeadPosition)
}

// TestCallCache_StatsExactnessAcrossForcedMiss drives "f" through a cache
// miss, a hit, a Rebuild-forced miss (NameGen bump invalidates the entry),
// and a hit again — the shared counter (decision 4) must accumulate every
// call regardless of which path served it.
func TestCallCache_StatsExactnessAcrossForcedMiss(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
	require.NoError(t, err)

	_, err = eng.Call(ctx, "f", core.Int{V: 1}) // miss, populates
	require.NoError(t, err)
	_, err = eng.Call(ctx, "f", core.Int{V: 1}) // hit
	require.NoError(t, err)

	eng.(*engineImpl).rootEnv.Rebuild() // bumps NameGen: next Call on f is a forced miss

	_, err = eng.Call(ctx, "f", core.Int{V: 1}) // forced miss, re-resolves
	require.NoError(t, err)
	_, err = eng.Call(ctx, "f", core.Int{V: 1}) // hit again
	require.NoError(t, err)

	stats := eng.Stats()
	assert.Equal(t, int64(4), stats.PluginCallCounts["f"], "same counter must accumulate across hit and forced-miss paths")
}

// TestCallCache_BoundedFlushOnOverflow calls more than maxCallCacheEntries
// distinct names, which forces at least one flush-all during the first
// pass. The second pass re-calls every name, including ones evicted by the
// flush, and every one must still resolve correctly.
func TestCallCache_BoundedFlushOnOverflow(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	const n = maxCallCacheEntries + 50
	names := make([]string, n)
	var src strings.Builder
	for i := range n {
		names[i] = fmt.Sprintf("f%d", i)
		fmt.Fprintf(&src, "(defun %s (x) x) ", names[i])
	}
	_, err = eng.Eval(ctx, "def-many", src.String())
	require.NoError(t, err)

	for _, name := range names {
		_, err := eng.Call(ctx, name, core.Int{V: 1})
		require.NoError(t, err, "name %s must resolve on first call", name)
	}

	for _, name := range names {
		got, err := eng.Call(ctx, name, core.Int{V: 7})
		require.NoError(t, err, "name %s must still resolve after a cache overflow flush", name)
		assert.True(t, core.Int{V: 7}.Equals(got), "name %s: got %v", name, got)
	}
}

// TestCallCache_ConcurrentDuringRedefineAndReload runs Callers against a
// live name while the main goroutine alternates between redefining it via
// eval and via hot-reload — the two mutation paths the cache must stay
// correct under. Race-clean under -race is the pass criterion; every Call
// must succeed since f stays bound throughout.
func TestCallCache_ConcurrentDuringRedefineAndReload(t *testing.T) {
	dir := t.TempDir()
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	w := newFileWatcher(impl, dir, 10*time.Millisecond)
	w.ctx = t.Context()
	file := filepath.Join(dir, "reload.lisp")

	stop := make(chan struct{})
	var wg sync.WaitGroup
	const callers = 8
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, callErr := eng.Call(ctx, "f", core.Int{V: 1}); callErr != nil {
					t.Errorf("unexpected Call error: %v", callErr)
					return
				}
			}
		}()
	}

	const rounds = 50
	for i := range rounds {
		_, err := eng.Eval(ctx, "redef", fmt.Sprintf("(defun f (x) %d)", i))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf("(defun f (x) %d)", i+rounds)), 0o644))
		w.reloadFile(file)
	}

	close(stop)
	wg.Wait()
}

// TestCallCache_Lisp2ValueThenFunctionBindingObserved is the blocker-2
// regression: caching a Lisp-2 value-cell fallback would let a later
// function binding for the same name go unobserved by Call forever, since
// Env.SetFuncWithContext never bumps Env.NameGen. resolveCallCell must
// report that hit as non-cacheable so the next Call re-resolves fresh.
func TestCallCache_Lisp2ValueThenFunctionBindingObserved(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-f", "(def f (fn (x) :value-cell))")
	require.NoError(t, err)

	got, err := eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "value-cell"}.Equals(got), "got %v", got)

	_, err = eng.Eval(ctx, "defun-f", "(defun f (x) :function-cell)")
	require.NoError(t, err)

	got, err = eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "function-cell"}.Equals(got), "Call must observe the new function binding, got %v", got)

	viaHeadPosition, err := eng.Eval(ctx, "head-call", "(f 1)")
	require.NoError(t, err)
	assert.True(t, viaHeadPosition.Equals(got), "Call must match head-position resolution")
}

// TestCallCache_Lisp2BindThenFunctionBindingObserved is the Engine.Bind
// variant of blocker 3, the same root cause reached through a different
// entry point: Bind under Lisp-2 calls Env.SetBoth, which only bumps
// Env.NameGen when the value cell was previously unbound, so a
// Bind-created value cell needs the same non-cacheable-fallback rule.
func TestCallCache_Lisp2BindThenFunctionBindingObserved(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-f", "(def f (fn (x) :value-cell))")
	require.NoError(t, err)
	_, err = eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)

	require.NoError(t, eng.Bind("f", core.GoFunc{
		Name: "f",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Keyword{V: "bound"}, nil
		},
	}))

	_, err = eng.Eval(ctx, "defun-f", "(defun f (x) :function-cell)")
	require.NoError(t, err)

	got, err := eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "function-cell"}.Equals(got), "Call must observe the new function binding, got %v", got)

	viaHeadPosition, err := eng.Eval(ctx, "head-call", "(f 1)")
	require.NoError(t, err)
	assert.True(t, viaHeadPosition.Equals(got), "Call must match head-position resolution")
}

// TestCallCache_TombstonedFunctionFallsBackToValue is the natural-sequence
// regression for blocker 4: a cached function-cell entry surviving until
// its cell is deleted, with a value binding then created for the same
// name, must resolve through the value cell rather than reporting
// undefined.
func TestCallCache_TombstonedFunctionFallsBackToValue(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()
	impl := eng.(*engineImpl)

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) :function-cell)")
	require.NoError(t, err)
	got, err := eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "function-cell"}.Equals(got))

	impl.rootEnv.Delete("f")
	_, err = eng.Eval(ctx, "def-f-value", "(def f (fn () :value-cell))")
	require.NoError(t, err)

	got, err = eng.Call(ctx, "f")
	require.NoError(t, err, "must fall back to the value binding, not report undefined")
	assert.True(t, core.Keyword{V: "value-cell"}.Equals(got), "got %v", got)
}

// TestCallCache_StaleCacheHitOnDeadCellFallsBack isolates the blocker-4
// code path from the generation guard alone: it plants a cache entry whose
// gen matches the CURRENT env.NameGen() (so the guard by itself would not
// catch it) but whose cell is dead, alongside an already-live value cell
// for the same name. Only the !live-on-a-cache-hit fallback — not the
// generation guard — can save this call.
func TestCallCache_StaleCacheHitOnDeadCellFallsBack(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()
	impl := eng.(*engineImpl)

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) :function-cell)")
	require.NoError(t, err)
	got, err := eng.Call(ctx, "f", core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "function-cell"}.Equals(got))

	cached := impl.callCache.lookup("f", impl.rootEnv)
	require.NotNil(t, cached, "f must be cached after a function-cell Call")
	deadCell := cached.cell

	impl.rootEnv.Delete("f")
	_, err = eng.Eval(ctx, "def-f-value", "(def f (fn () :value-cell))")
	require.NoError(t, err)

	// Replant a stale entry pointing at the dead function cell, but with
	// gen forced to match the CURRENT generation — a hit whose generation
	// guard passes yet whose cell has gone dead, independent of whatever
	// bump the (def f ...) above happened to cause.
	stale := &callCacheEntry{env: impl.rootEnv, gen: impl.rootEnv.NameGen(), cell: deadCell, counter: cached.counter}
	impl.callCache.store("f", stale)

	got, err = eng.Call(ctx, "f")
	require.NoError(t, err, "a cache hit on a tombstoned cell must fall back to a live value binding, not report undefined")
	assert.True(t, core.Keyword{V: "value-cell"}.Equals(got), "got %v", got)
}

// TestCallCache_ConcurrentDeleteRebuildRedefineNeverSticks is the bounded
// stress detector for blocker 1: N goroutines hammer Call while one
// goroutine repeatedly deletes, rebuilds, and redefines the name — the
// sequence that lets a post-Rebuild generation get paired with the
// orphaned pre-Rebuild cell when gen is read after resolving instead of
// before. The corruption is sticky (a wrong cache entry stays wrong
// forever), so once the mutator stops, a final deterministic pass must
// observe the last definition on every call — a single miss there means
// the race got through. A detector, not a proof: the deterministic tests
// above carry the correctness weight.
func TestCallCache_ConcurrentDeleteRebuildRedefineNeverSticks(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()
	impl := eng.(*engineImpl)

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) 0)")
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	const callers = 8
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = eng.Call(ctx, "f", core.Int{V: 1})
			}
		}()
	}

	deadline := time.Now().Add(2 * time.Second)
	for i := 0; time.Now().Before(deadline) && i < 50_000; i++ {
		impl.rootEnv.Delete("f")
		impl.rootEnv.Rebuild()
		_, err := eng.Eval(ctx, "redef", fmt.Sprintf("(defun f (x) %d)", i))
		require.NoError(t, err)
	}

	close(stop)
	wg.Wait()

	_, err = eng.Eval(ctx, "final-def", "(defun f (x) :final)")
	require.NoError(t, err)
	for i := range 1000 {
		got, callErr := eng.Call(ctx, "f", core.Int{V: 1})
		require.NoError(t, callErr, "iteration %d: Call must not stick to a dead cache entry", i)
		assert.True(t, core.Keyword{V: "final"}.Equals(got), "iteration %d: got %v", i, got)
	}
}

// TestCallCache_GenerationCapturedBeforeResolve is a deterministic proof of
// blocker 1, independent of goroutine-scheduling luck: it performs the two
// halves of resolveCallEntry by hand around a real Delete+Rebuild+redefine,
// in the buggy order (resolve the cell, THEN read gen), and shows the
// resulting pairing would be a permanent zombie — a dead cell whose paired
// generation equals the env's current, live generation, so every future
// {env,gen} check on it would pass forever. It then confirms the real
// resolveCallEntry (gen captured first) never lands on that orphaned cell
// when resolving the same name after the same mutation.
func TestCallCache_GenerationCapturedBeforeResolve(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()
	impl := eng.(*engineImpl)

	_, err = eng.Eval(ctx, "def-f", "(defun f (x) :first)")
	require.NoError(t, err)

	// Buggy-order step 1: resolve the cell before the mutation lands.
	oldCell, _, ok := impl.resolveCallCell(impl.rootEnv, "f")
	require.True(t, ok)

	// The concurrent mutation that would land between the two reads.
	impl.rootEnv.Delete("f")
	impl.rootEnv.Rebuild()
	_, err = eng.Eval(ctx, "redef-f", "(defun f (x) :second)")
	require.NoError(t, err)

	// Buggy-order step 2: read gen after the mutation.
	genAfterResolve := impl.rootEnv.NameGen()

	_, liveOld, _ := impl.rootEnv.ReadCell(oldCell)
	require.False(t, liveOld, "Rebuild must permanently orphan the pre-mutation cell")
	require.Equal(t, genAfterResolve, impl.rootEnv.NameGen(),
		"a gen-after-resolve entry would pair the dead oldCell with THIS generation, "+
			"which equals the env's current live generation — so the {env,gen} guard would pass forever")

	// The real resolveCallEntry, called fresh after the same mutation,
	// must never return the orphaned cell.
	freshEntry, ok := impl.resolveCallEntry(impl.rootEnv, "f")
	require.True(t, ok)
	assert.NotSame(t, oldCell, freshEntry.cell, "fresh resolution must never return the orphaned cell")
	fn, live, _ := impl.rootEnv.ReadCell(freshEntry.cell)
	require.True(t, live)
	got, err := impl.evaluator.Apply(ctx, fn, []core.Value{core.Int{V: 1}}, impl.rootEnv)
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "second"}.Equals(got), "got %v", got)
}
