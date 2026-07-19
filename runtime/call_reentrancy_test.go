package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestCallReentrancy_SharedStructuralDepth proves Engine.Call's fast path
// (applyBoundary, no per-call evalState) still shares ONE structural-depth
// budget across the VM -> GoFunc -> re-entrant eval boundary. A GoFunc bound
// as "reenter" evaluates a 6-deep vector literal on re-entry; called alone it
// fits a limit of 6, but wrapped in an outer VM-compiled vector literal the
// combined depth (1+6) must trip the limit — the re-entrant eval must not get
// a fresh budget.
func TestCallReentrancy_SharedStructuralDepth(t *testing.T) {
	t.Parallel()

	deep, err := clojure.Dialect().ReadWithMaxDepth(deepVector(6), 200)
	require.NoError(t, err)
	require.Len(t, deep, 1)
	deepForm := deep[0]

	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 6, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(lim))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("reenter", core.GoFunc{
		Name: "reenter",
		Fn: func(ctx context.Context, eval core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			return eval.Eval(ctx, deepForm, env)
		},
	}))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "def-bare", "(defn bare [] (reenter))")
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-wrapped", "(defn wrapped [] [(reenter)])")
	require.NoError(t, err)

	_, err = eng.Call(ctx, "bare")
	assert.NoError(t, err, "reenter alone (depth 6 == limit) must succeed")

	_, err = eng.Call(ctx, "wrapped")
	require.Error(t, err, "outer vector (1) + reenter body (6) = 7 > 6 must reject — shared counter")
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
}

// TestCallReentrancy_SharedStructuralDepthAcrossEngineCall proves the shared
// structural-depth budget also holds when a GoFunc re-enters through the public
// Engine.Call (not just eval.Eval) — the fast path must adopt an enclosing
// evalState's counter rather than starting a fresh budget per nested Call.
func TestCallReentrancy_SharedStructuralDepthAcrossEngineCall(t *testing.T) {
	t.Parallel()

	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 6, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(lim))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	_, err = eng.Eval(context.Background(), "def-inner", "(defn inner [] "+deepVector(6)+")")
	require.NoError(t, err)

	// reenter-call re-enters through the public Engine.Call, forwarding the ctx it
	// received (carrying the outer call's adopted structural-depth counter).
	require.NoError(t, eng.Bind("reenter-call", core.GoFunc{
		Name: "reenter-call",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return eng.Call(ctx, "inner")
		},
	}))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "def-bare2", "(defn bare2 [] (reenter-call))")
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-wrapped2", "(defn wrapped2 [] [(reenter-call)])")
	require.NoError(t, err)

	_, err = eng.Call(ctx, "bare2")
	assert.NoError(t, err, "inner alone (depth 6 == limit) must succeed")

	_, err = eng.Call(ctx, "wrapped2")
	require.Error(t, err, "outer vector (1) + inner Engine.Call body (6) = 7 > 6 must reject — shared counter across Engine.Call")
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
}

// TestCallReentrancy_MaxCallDepthAcrossBoundary proves recursion through the
// VM -> GoFunc -> tree-walker boundary is bounded by the engine's MaxEvalDepth
// (a typed *core.LispicoError) rather than the Go call stack. The GoFunc
// re-evaluates a form containing unquote-splicing, which the compiler rejects
// (forcing tree-walker fallback) and which calls the GoFunc again — recursing
// purely through eval.Eval on the shared evalState.
func TestCallReentrancy_MaxCallDepthAcrossBoundary(t *testing.T) {
	t.Parallel()

	recur, err := clojure.Dialect().ReadWithMaxDepth(
		"(do (quasiquote ((unquote-splicing (quote ())))) (reenter2))", 200)
	require.NoError(t, err)
	require.Len(t, recur, 1)
	recurForm := recur[0]

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithMaxEvalDepth(10))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("reenter2", core.GoFunc{
		Name: "reenter2",
		Fn: func(ctx context.Context, eval core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			return eval.Eval(ctx, recurForm, env)
		},
	}))

	_, err = eng.Call(context.Background(), "reenter2")
	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T", err)
	assert.Equal(t, "EvalError", lerr.Code)
}

// TestCallReentrancy_DeadlinePropagates proves the engine's own deadline
// (Engine.Call's fast path carries it on the VM directly, no per-call
// evalState) still fires inside a re-entrant GoFunc that busy-loops through
// eval.Eval.
func TestCallReentrancy_DeadlinePropagates(t *testing.T) {
	t.Parallel()

	const maxIter = 5_000_000

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(30*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("spin", core.GoFunc{
		Name: "spin",
		Fn: func(ctx context.Context, eval core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			for range maxIter {
				if _, err := eval.Eval(ctx, core.Int{V: 1}, env); err != nil {
					return nil, err
				}
			}
			return nil, fmt.Errorf("deadline never fired after %d iterations", maxIter)
		},
	}))

	_, err = eng.Call(context.Background(), "spin")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "expected DeadlineExceeded, got %v", err)
}

// TestCallReentrancy_ConcurrentRace runs concurrent Engine.Call invocations
// through a GoFunc that re-enters the evaluator, exercising the pooled-VM
// fast path and reentrantCtx adoption under -race.
func TestCallReentrancy_ConcurrentRace(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	nested := core.Vector{Items: []core.Value{core.Int{V: 1}, core.Vector{Items: []core.Value{core.Int{V: 2}, core.Int{V: 3}}}}}

	require.NoError(t, eng.Bind("reenter3", core.GoFunc{
		Name: "reenter3",
		Fn: func(ctx context.Context, eval core.Evaluator, _ []core.Value, env *core.Env) (core.Value, error) {
			return eval.Eval(ctx, nested, env)
		},
	}))

	const workers = 8
	const iterations = 50
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx := context.Background()
			for j := range iterations {
				res, err := eng.Call(ctx, "reenter3")
				if err != nil {
					t.Errorf("worker %d iter %d: %v", id, j, err)
					return
				}
				if !res.Equals(nested) {
					t.Errorf("worker %d iter %d: expected %v, got %v", id, j, nested, res)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
