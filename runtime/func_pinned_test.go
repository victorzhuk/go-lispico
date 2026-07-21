package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// invoker is the shared call surface PinnedFn.Call and Fn.Call expose to the
// equivalence suite — both types satisfy it so a single helper can drive both
// paths against the same observable contract.
type invoker interface {
	Call(ctx context.Context, args ...core.Value) (core.Value, error)
}

type fnInvoker struct{ fn *Fn }

func (i fnInvoker) Call(ctx context.Context, args ...core.Value) (core.Value, error) {
	return i.fn.Call(ctx, args...)
}

type pinnedInvoker struct{ p *PinnedFn }

func (i pinnedInvoker) Call(ctx context.Context, args ...core.Value) (core.Value, error) {
	return i.p.Call(ctx, args...)
}

// TestPinned_EquivalentToShared drives each observable dimension of Fn.Call
// against PinnedFn.Call and asserts the two paths produce identical results —
// the only difference between them is the VM lifecycle, not what a caller
// observes from outside.
func TestPinned_EquivalentToShared(t *testing.T) {
	t.Run("rebind visibility", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		invoke := func(inv invoker) core.Value {
			t.Helper()
			v, err := inv.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
			return v
		}

		assert.True(t, core.Int{V: 1}.Equals(invoke(fnInvoker{fn})))
		assert.True(t, core.Int{V: 1}.Equals(invoke(pinnedInvoker{pinned})))

		_, err = eng.Eval(ctx, "redef-f", "(defun f (x) :rebound)")
		require.NoError(t, err)

		assert.True(t, core.Keyword{V: "rebound"}.Equals(invoke(fnInvoker{fn})))
		assert.True(t, core.Keyword{V: "rebound"}.Equals(invoke(pinnedInvoker{pinned})))
	})

	t.Run("delete then call returns undefined", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f () :ok)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		impl := eng.(*engineImpl)
		impl.rootEnv.Delete("f")

		_, fnErr := fn.Call(ctx)
		_, pinnedErr := pinned.Call(ctx)
		require.EqualError(t, fnErr, "undefined function: f")
		require.Equal(t, fnErr.Error(), pinnedErr.Error())
	})

	t.Run("stats attribution on success and undefined", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		const success = 3
		for range success {
			_, err := fn.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
			_, err = pinned.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
		}
		stats := eng.Stats()
		assert.Equal(t, int64(success*2), stats.PluginCallCounts["f"])

		impl := eng.(*engineImpl)
		impl.rootEnv.Delete("f")

		_, _ = fn.Call(ctx)
		_, _ = pinned.Call(ctx)

		stats = eng.Stats()
		assert.Equal(t, int64(success*2+2), stats.PluginCallCounts["f"], "undefined path still bumps the same shared counter")
	})

	t.Run("callback events match between Fn and Pinned", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		var ticks atomic.Int64
		restore := nowFunc
		nowFunc = func() time.Time { return time.Unix(0, ticks.Add(1)) }
		t.Cleanup(func() { nowFunc = restore })

		var mu sync.Mutex
		var events []PluginCallEvent
		eng.OnPluginCall(func(e PluginCallEvent) {
			mu.Lock()
			defer mu.Unlock()
			if e.Function == "f" {
				events = append(events, e)
			}
		})

		const calls = 2
		for range calls {
			_, err := fn.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
		}
		for range calls {
			_, err := pinned.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
		}

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, events, calls*2, "Fn and Pinned paths must each fire OnPluginCall %d times for f", calls)
		for _, e := range events {
			assert.Equal(t, "f", e.Function)
			assert.Greater(t, e.Duration, time.Duration(0))
		}
	})

	t.Run("callback fires on undefined path too", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		var ticks atomic.Int64
		restore := nowFunc
		nowFunc = func() time.Time { return time.Unix(0, ticks.Add(1)) }
		t.Cleanup(func() { nowFunc = restore })

		var mu sync.Mutex
		var undefinedEvents []PluginCallEvent
		eng.OnPluginCall(func(e PluginCallEvent) {
			mu.Lock()
			defer mu.Unlock()
			if e.Function == "f" && e.Duration == 0 {
				undefinedEvents = append(undefinedEvents, e)
			}
		})

		impl := eng.(*engineImpl)
		impl.rootEnv.Delete("f")

		_, _ = fn.Call(ctx)
		_, _ = pinned.Call(ctx)

		mu.Lock()
		defer mu.Unlock()
		assert.Len(t, undefinedEvents, 2, "both Fn.Call and PinnedFn.Call must fire the undefined-callback event with Duration=0")
	})

	t.Run("deadline firing", func(t *testing.T) {
		eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(15*time.Millisecond))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-slow", "(defn slow [] (loop [n 100000000] (if (= n 0) n (recur (- n 1)))))")
		require.NoError(t, err)
		bindBuiltin(t, eng, "=")
		bindBuiltin(t, eng, "-")

		fn, err := eng.Func("slow")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		_, fnErr := fn.Call(context.Background())
		require.Error(t, fnErr)
		assert.True(t, errors.Is(fnErr, context.DeadlineExceeded), "Fn.Call: %v", fnErr)

		_, pinnedErr := pinned.Call(context.Background())
		require.Error(t, pinnedErr)
		assert.True(t, errors.Is(pinnedErr, context.DeadlineExceeded), "PinnedFn.Call: %v", pinnedErr)
	})

	t.Run("re-entrant budget sharing", func(t *testing.T) {
		lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 6, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
		eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(lim))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		deep, err := clojure.Dialect().ReadWithMaxDepth(deepVector(6), 200)
		require.NoError(t, err)
		require.Len(t, deep, 1)
		deepForm := deep[0]

		require.NoError(t, eng.Bind("reenter", core.GoFunc{
			Name: "reenter",
			Fn: func(ctx context.Context, eval core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
				return eval.Eval(ctx, deepForm, env)
			},
		}))

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-bare", "(defn bare [] (reenter))")
		require.NoError(t, err)
		_, err = eng.Eval(ctx, "def-wrapped", "(defn wrapped [] [(reenter)])")
		require.NoError(t, err)

		bare, err := eng.Func("bare")
		require.NoError(t, err)
		wrapped, err := eng.Func("wrapped")
		require.NoError(t, err)

		barePinned := bare.Pin()
		wrappedPinned := wrapped.Pin()
		require.NotNil(t, barePinned)
		require.NotNil(t, wrappedPinned)

		_, err = bare.Call(context.Background())
		require.NoError(t, err, "bare on Fn at depth 6 == limit must succeed")
		_, err = barePinned.Call(context.Background())
		require.NoError(t, err, "bare on PinnedFn at depth 6 == limit must succeed")

		_, err = wrapped.Call(context.Background())
		require.Error(t, err, "wrapped on Fn (1+6) > 6 must reject")
		assert.True(t, isResourceLimit(t, err), "Fn wrapped: %v", err)
		_, err = wrappedPinned.Call(context.Background())
		require.Error(t, err, "wrapped on PinnedFn (1+6) > 6 must reject")
		assert.True(t, isResourceLimit(t, err), "PinnedFn wrapped: %v", err)
	})
}

func TestPinned_ConcurrentMisuseReturnsTypedError(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	// Bind a barrier GoFunc so the first goroutine parks inside its Call with
	// inUse=true until we explicitly release it. Without a barrier the per-call
	// cost is too low for two goroutines to reliably overlap on the CAS.
	arrived := make(chan struct{})
	var arrivedOnce sync.Once
	release := make(chan struct{})
	require.NoError(t, eng.Bind("barrier", core.GoFunc{
		Name: "barrier",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			arrivedOnce.Do(func() { close(arrived) })
			<-release
			return core.Int{V: 1}, nil
		},
	}))
	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-f", "(defn f [] (barrier))")
	require.NoError(t, err)
	fn, err := eng.Func("f")
	require.NoError(t, err)
	pinned := fn.Pin()
	require.NotNil(t, pinned)

	// Park the first goroutine inside a successful Call. After it signals
	// arrival at the barrier, inUse=true, and the handle rejects any concurrent
	// entry until we release.
	firstDone := make(chan error, 1)
	go func() {
		_, callErr := pinned.Call(ctx)
		firstDone <- callErr
	}()

	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("first goroutine did not reach the barrier in time")
	}

	// The handle is now busy. A concurrent Call from the test goroutine must
	// hit the CAS guard and return a typed ConcurrentUseError.
	_, err = pinned.Call(ctx)
	require.Error(t, err, "concurrent Call must be rejected with a typed error")
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T: %v", err, err)
	assert.Equal(t, core.CodeConcurrentUse, lerr.Code, "expected ConcurrentUseError, got %v", err)

	// Release the first goroutine; it must complete cleanly.
	close(release)
	select {
	case firstErr := <-firstDone:
		require.NoError(t, firstErr, "first Call must succeed once released")
	case <-time.After(2 * time.Second):
		t.Fatal("first goroutine did not complete after release")
	}

	// Handle stays usable after the race.
	v, err := pinned.Call(ctx)
	require.NoError(t, err, "after the race, the handle must remain usable")
	require.True(t, core.Int{V: 1}.Equals(v), "got %v", v)

	stats := eng.Stats()
	assert.Equal(t, int64(2), stats.PluginCallCounts["f"], "only successful calls must bump the counter; the typed-error concurrent attempt must not")
}

func TestPinned_ErrorPathFullReset(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-throw", `(defun throw-once () (error "boom"))`)
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-id", "(defun id (x) x)")
	require.NoError(t, err)

	throwFn, err := eng.Func("throw-once")
	require.NoError(t, err)
	idFn, err := eng.Func("id")
	require.NoError(t, err)

	throwPinned := throwFn.Pin()
	idPinned := idFn.Pin()
	require.NotNil(t, throwPinned)
	require.NotNil(t, idPinned)

	_, err = throwPinned.Call(ctx)
	require.Error(t, err, "throw-once must surface an error")

	got, err := idPinned.Call(ctx, core.Int{V: 7})
	require.NoError(t, err)
	require.True(t, core.Int{V: 7}.Equals(got), "after a thrown error the next pinned call must succeed; got %v", got)

	got, err = idPinned.Call(ctx, core.Int{V: 8})
	require.NoError(t, err)
	require.True(t, core.Int{V: 8}.Equals(got), "second pinned call after throw must succeed; got %v", got)

	// Same-handle recovery: rebind throw-once to a non-throwing body and call
	// the SAME pinned handle again. Proves throwPinned's inUse was released and
	// its VM was fully reset (not just incremented) so the next call observes
	// a clean VM with the new binding.
	_, err = eng.Eval(ctx, "redef-throw", `(defun throw-once (x) (* x 2))`)
	require.NoError(t, err)
	bindBuiltin(t, eng, "*")

	got, err = throwPinned.Call(ctx, core.Int{V: 21})
	require.NoError(t, err, "the same pinned handle must recover after rebind; got err=%v", err)
	require.True(t, core.Int{V: 42}.Equals(got), "after rebind the same pinned call must succeed; got %v", got)
}

func TestPinned_PanicRecovery(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("boom", core.GoFunc{
		Name: "boom",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			panic("kaboom from boom")
		},
	}))
	_, err = eng.Eval(t.Context(), "def-call-boom", "(defun call-boom () (boom))")
	require.NoError(t, err)
	_, err = eng.Eval(t.Context(), "def-id", "(defun id (x) x)")
	require.NoError(t, err)

	callBoom, err := eng.Func("call-boom")
	require.NoError(t, err)
	pinned := callBoom.Pin()
	require.NotNil(t, pinned)

	require.NotPanics(t, func() {
		_, err = pinned.Call(t.Context())
	}, "panicking GoFunc must NOT propagate out of PinnedFn.Call")
	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T: %v", err, err)
	assert.Equal(t, core.CodePanic, lerr.Code)
	assert.Contains(t, lerr.Message, "kaboom from boom")

	id, err := eng.Func("id")
	require.NoError(t, err)
	idPinned := id.Pin()
	require.NotNil(t, idPinned)

	got, err := idPinned.Call(t.Context(), core.Int{V: 42})
	require.NoError(t, err, "after a panicking pinned call the handle must be usable again")
	require.True(t, core.Int{V: 42}.Equals(got), "got %v", got)

	// Same-handle recovery: rebind call-boom to a non-panicking body and call
	// the SAME pinned handle again. Proves the panicking call's inUse was
	// released and full Reset (not ResetIncremental) ran, so the next apply
	// observes a clean VM with the new binding.
	_, err = eng.Eval(t.Context(), "redef-call-boom", `(defun call-boom (x) (* x 2))`)
	require.NoError(t, err)
	bindBuiltin(t, eng, "*")

	got, err = pinned.Call(t.Context(), core.Int{V: 21})
	require.NoError(t, err, "the same pinned handle must recover after the panicking call; got err=%v", err)
	require.True(t, core.Int{V: 42}.Equals(got), "after panic-recovery the same pinned call must succeed; got %v", got)
}

func TestPinned_ReentrantCallReturnsTypedError(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	var p *PinnedFn
	require.NoError(t, eng.Bind("reenter-self", core.GoFunc{
		Name: "reenter-self",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			if p == nil {
				return nil, fmt.Errorf("self-call handle not yet wired")
			}
			return p.Call(ctx, core.Int{V: 9})
		},
	}))
	_, err = eng.Eval(ctx, "def-outer", "(defun outer () (reenter-self))")
	require.NoError(t, err)
	fn, err := eng.Func("outer")
	require.NoError(t, err)
	p = fn.Pin()
	require.NotNil(t, p)
	_, err = p.Call(ctx)
	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T: %v", err, err)
	assert.Equal(t, core.CodeConcurrentUse, lerr.Code, "re-entrant p.Call must return ConcurrentUseError, got %v", err)
}

func TestPinned_TwoPinsFromOneFnIndependent(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
	require.NoError(t, err)

	fn, err := eng.Func("f")
	require.NoError(t, err)
	a := fn.Pin()
	b := fn.Pin()
	require.NotNil(t, a)
	require.NotNil(t, b)

	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range iterations {
			v, callErr := a.Call(ctx, core.Int{V: int64(i)})
			require.NoError(t, callErr)
			require.True(t, core.Int{V: int64(i)}.Equals(v))
		}
	}()
	go func() {
		defer wg.Done()
		for i := range iterations {
			v, callErr := b.Call(ctx, core.Int{V: int64(-i)})
			require.NoError(t, callErr)
			require.True(t, core.Int{V: int64(-i)}.Equals(v))
		}
	}()
	wg.Wait()

	stats := eng.Stats()
	assert.Equal(t, int64(iterations*2), stats.PluginCallCounts["f"], "shared counter must accumulate both Pins")
}

func TestPinned_GoFuncThenPureCall(t *testing.T) {
	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 6, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(lim), WithTimeout(15*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-inner", "(defn inner [] "+deepVector(6)+")")
	require.NoError(t, err)
	bindBuiltin(t, eng, "=")
	bindBuiltin(t, eng, "-")

	require.NoError(t, eng.Bind("reenter-call", core.GoFunc{
		Name: "reenter-call",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return eng.Call(ctx, "inner")
		},
	}))
	_, err = eng.Eval(ctx, "def-outer", "(defn outer [] [(reenter-call)])")
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-slow", "(defn slow [] (loop [n 100000000] (if (= n 0) n (recur (- n 1)))))")
	require.NoError(t, err)

	outer, err := eng.Func("outer")
	require.NoError(t, err)
	slow, err := eng.Func("slow")
	require.NoError(t, err)

	outerPinned := outer.Pin()
	require.NotNil(t, outerPinned)

	_, err = outerPinned.Call(context.Background())
	require.Error(t, err, "outer on pinned: outer (1) + inner (6) = 7 > 6 must reject")
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError from the dirty call, got %v", err)

	slowPinned := slow.Pin()
	require.NotNil(t, slowPinned)

	_, err = slowPinned.Call(context.Background())
	require.Error(t, err, "deadline must fire on the second pinned call; if structDepth/deadline were not reset incrementally the call would either return instantly or corrupt budget accounting")
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "expected DeadlineExceeded, got %v", err)
}

func TestPinned_DialectResolution(t *testing.T) {
	t.Run("lisp2 defun function cell", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		got, err := pinned.Call(ctx, core.Int{V: 9})
		require.NoError(t, err)
		assert.True(t, core.Int{V: 9}.Equals(got), "got %v", got)
	})

	t.Run("lisp1 bind value cell", func(t *testing.T) {
		eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		require.NoError(t, eng.Bind("id", core.GoFunc{
			Name: "id",
			Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				return args[0], nil
			},
		}))
		fn, err := eng.Func("id")
		require.NoError(t, err)
		pinned := fn.Pin()
		require.NotNil(t, pinned)

		got, err := pinned.Call(t.Context(), core.Int{V: 7})
		require.NoError(t, err)
		assert.True(t, core.Int{V: 7}.Equals(got), "got %v", got)
	})

	t.Run("lisp2 value-only closure is not a function binding", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		_, err = eng.Eval(t.Context(), "value-only", "(def f (fn (x) x))")
		require.NoError(t, err)
		_, err = eng.Func("f")
		require.EqualError(t, err, "undefined function: f")
	})
}
