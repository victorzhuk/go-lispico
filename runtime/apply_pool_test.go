package runtime

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestBytecodeRuntime_CallWithPool verifies Engine.Call returns correct results
// when routed through the pooled bytecode VM path.
func TestBytecodeRuntime_CallWithPool(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()

	// Define functions via Eval (goes through bytecode evaluator → runVM → pool).
	_, err = eng.Eval(ctx, "def-id", "(def id-fn (fn [x] x))")
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-const", "(def const-fn (fn [] 42))")
	require.NoError(t, err)

	// Call — must return correct results through pooled VM.
	r, err := eng.Call(ctx, "const-fn")
	require.NoError(t, err)
	require.True(t, r.Equals(core.Int{V: 42}))

	r, err = eng.Eval(ctx, "post-call", "2")
	require.NoError(t, err)
	require.True(t, r.Equals(core.Int{V: 2}))

	r, err = eng.Call(ctx, "id-fn", core.Int{V: 99})
	require.NoError(t, err)
	require.True(t, r.Equals(core.Int{V: 99}))
}

// TestBytecodeRuntime_CallIsolation verifies sequential Call invocations
// return independent results without cross-contamination.
func TestBytecodeRuntime_CallIsolation(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-id", "(def id-fn (fn [x] x))")
	require.NoError(t, err)

	const n = 10
	for i := range n {
		res, err := eng.Call(ctx, "id-fn", core.Int{V: int64(i)})
		require.NoError(t, err, "iteration %d", i)
		assert.True(t, res.Equals(core.Int{V: int64(i)}), "expected %d, got %v", i, res)
	}
}

// TestBytecodeRuntime_CallConcurrent verifies concurrent Calls from multiple
// goroutines produce correct results with no races.
func TestBytecodeRuntime_CallConcurrent(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-const", "(def const-fn (fn [] 42))")
	require.NoError(t, err)

	const workers = 8
	const iterations = 25
	var wg sync.WaitGroup

	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range iterations {
				res, err := eng.Call(ctx, "const-fn")
				if err != nil {
					t.Errorf("worker %d iter %d: %v", id, j, err)
					return
				}
				if !res.Equals(core.Int{V: 42}) {
					t.Errorf("worker %d iter %d: expected 42, got %v", id, j, res)
					return
				}
			}
		}(i)
	}

	wg.Wait()
}
