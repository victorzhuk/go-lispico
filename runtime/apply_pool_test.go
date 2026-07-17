package runtime

import (
	"errors"
	"fmt"
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

// TestBytecodeRuntime_CallSequenceNoStateLeak verifies a sequence of pooled
// Call invocations, including a call that errors on the VM's max call depth,
// leaves no stack/frame/depth state behind for the next call. Each result
// must depend only on its own argument, not on the outcome or depth of prior
// calls.
func TestBytecodeRuntime_CallSequenceNoStateLeak(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithMaxEvalDepth(20))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()

	bindBuiltin(t, eng, "+")
	bindBuiltin(t, eng, "-")
	bindBuiltin(t, eng, "=")

	_, err = eng.Eval(ctx, "def-count-down",
		"(defn count-down [n] (if (= n 0) 0 (+ 1 (count-down (- n 1)))))")
	require.NoError(t, err)

	cases := []struct {
		n       int64
		wantErr bool
	}{
		{n: 3},
		{n: 30, wantErr: true}, // exceeds max depth 20
		{n: 5},                 // must not inherit depth/frame state from the failed call above
		{n: 2},
		{n: 25, wantErr: true}, // errors again
		{n: 1},                 // must not inherit state from the error immediately before it
		{n: 3},
	}

	for i, c := range cases {
		res, err := eng.Call(ctx, "count-down", core.Int{V: c.n})
		if c.wantErr {
			require.Error(t, err, "case %d: n=%d", i, c.n)
			var lispicoErr *core.LispicoError
			require.True(t, errors.As(err, &lispicoErr), "case %d: expected *core.LispicoError, got %T", i, err)
			require.Equal(t, "EvalError", lispicoErr.Code, "case %d", i)
			continue
		}
		require.NoError(t, err, "case %d: n=%d", i, c.n)
		assert.True(t, core.Int{V: c.n}.Equals(res), "case %d: count-down(%d) => %d, got %v", i, c.n, c.n, res)
	}
}

// TestBytecodeRuntime_CallConcurrentDistinctClosures verifies concurrent
// Calls to distinct closures — each capturing its own separate environment —
// through the pooled VM produce correct, non-cross-contaminated results.
func TestBytecodeRuntime_CallConcurrentDistinctClosures(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()

	bindBuiltin(t, eng, "+")

	_, err = eng.Eval(ctx, "def-make-adder", "(def make-adder (fn [n] (fn [x] (+ x n))))")
	require.NoError(t, err)

	const workers = 8
	const iterations = 25

	for i := range workers {
		name := fmt.Sprintf("adder%d", i)
		_, err := eng.Eval(ctx, "def-"+name, fmt.Sprintf("(def %s (make-adder %d))", name, i))
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("adder%d", id)
			want := core.Int{V: int64(100 + id)}
			for j := range iterations {
				res, err := eng.Call(ctx, name, core.Int{V: 100})
				if err != nil {
					t.Errorf("worker %d iter %d: %v", id, j, err)
					return
				}
				if !res.Equals(want) {
					t.Errorf("worker %d iter %d: expected %v, got %v", id, j, want, res)
					return
				}
			}
		}(i)
	}

	wg.Wait()
}
