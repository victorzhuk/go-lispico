package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestFunc_RebindVisible(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
	require.NoError(t, err)
	fn, err := eng.Func("f")
	require.NoError(t, err)

	got, err := fn.Call(ctx, core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)

	_, err = eng.Eval(ctx, "redef-f", "(defun f (x) :rebound)")
	require.NoError(t, err)
	got, err = fn.Call(ctx, core.Int{V: 1})
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "rebound"}.Equals(got), "got %v", got)
}

func TestFunc_DeleteAfterResolution(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-f", "(defun f () :ok)")
	require.NoError(t, err)
	fn, err := eng.Func("f")
	require.NoError(t, err)

	impl := eng.(*engineImpl)
	impl.rootEnv.Delete("f")

	_, fnErr := fn.Call(ctx)
	require.EqualError(t, fnErr, "undefined function: f")
	_, callErr := eng.Call(ctx, "f")
	require.EqualError(t, callErr, fnErr.Error())
}

func TestFunc_Concurrent(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-id", "(defun id (x) x)")
	require.NoError(t, err)
	fn, err := eng.Func("id")
	require.NoError(t, err)

	const workers = 8
	const iterations = 100
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			want := core.Int{V: int64(id)}
			for j := range iterations {
				got, err := fn.Call(ctx, want)
				if err != nil {
					t.Errorf("worker %d iter %d: %v", id, j, err)
					return
				}
				if !want.Equals(got) {
					t.Errorf("worker %d iter %d: expected %v, got %v", id, j, want, got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestFunc_StatsAndCallbacks(t *testing.T) {
	t.Run("callbacks", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)

		var ticks atomic.Int64
		restore := nowFunc
		nowFunc = func() time.Time { return time.Unix(0, ticks.Add(1)) }
		t.Cleanup(func() { nowFunc = restore })

		var mu sync.Mutex
		var events []PluginCallEvent
		eng.OnPluginCall(func(e PluginCallEvent) {
			mu.Lock()
			events = append(events, e)
			mu.Unlock()
		})

		const calls = 3
		for range calls {
			_, err := fn.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
		}

		stats := eng.Stats()
		assert.Equal(t, int64(calls), stats.PluginCallCounts["f"])
		mu.Lock()
		require.Len(t, events, calls)
		for _, event := range events {
			assert.Equal(t, "f", event.Function)
			assert.Greater(t, event.Duration, time.Duration(0))
		}
		mu.Unlock()
	})

	t.Run("no callbacks", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)

		const calls = 4
		for range calls {
			_, err := fn.Call(ctx, core.Int{V: 1})
			require.NoError(t, err)
		}

		stats := eng.Stats()
		assert.Equal(t, int64(calls), stats.PluginCallCounts["f"])
	})
}

func TestFunc_DialectResolution(t *testing.T) {
	t.Run("lisp2 defun function cell", func(t *testing.T) {
		eng, err := New(nil, WithBytecode())
		require.NoError(t, err)
		t.Cleanup(func() { _ = eng.Close() })

		ctx := t.Context()
		_, err = eng.Eval(ctx, "def-f", "(defun f (x) x)")
		require.NoError(t, err)
		fn, err := eng.Func("f")
		require.NoError(t, err)
		got, err := fn.Call(ctx, core.Int{V: 9})
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
		got, err := fn.Call(t.Context(), core.Int{V: 7})
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
