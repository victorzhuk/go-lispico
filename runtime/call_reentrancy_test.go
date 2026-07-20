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

func TestCallReentrancy_SharedStructuralDepthAcrossMultipleDispatches(t *testing.T) {
	t.Parallel()

	lim := ResourceLimits{MaxReaderDepth: 200, MaxStructuralDepth: 6, MaxCollectionLen: 1 << 30, MaxCacheEntries: 4096}
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithResourceLimits(lim))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	_, err = eng.Eval(context.Background(), "def-inner-multi", "(defn inner-multi [] "+deepVector(6)+")")
	require.NoError(t, err)

	require.NoError(t, eng.Bind("arm-state", core.GoFunc{
		Name: "arm-state",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			if !core.HasEvalState(ctx) {
				return nil, errors.New("missing eval state")
			}
			if core.EvalStructCounter(ctx) == nil {
				return nil, errors.New("missing structural counter")
			}
			return core.Nil{}, nil
		},
	}))
	require.NoError(t, eng.Bind("reenter-multi", core.GoFunc{
		Name: "reenter-multi",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return eng.Call(ctx, "inner-multi")
		},
	}))

	ctx := context.Background()
	_, err = eng.Eval(ctx, "def-bare-multi", "(defn bare-multi [] (do (arm-state) (reenter-multi)))")
	require.NoError(t, err)
	_, err = eng.Eval(ctx, "def-combined-multi", "(defn combined-multi [] (do (arm-state) [(reenter-multi)]))")
	require.NoError(t, err)

	_, err = eng.Call(ctx, "bare-multi")
	assert.NoError(t, err, "inner alone (depth 6 == limit) must succeed after prior GoFunc dispatch")

	_, err = eng.Call(ctx, "combined-multi")
	require.Error(t, err, "outer vector (1) + inner Engine.Call body (6) = 7 > 6 must reject after prior GoFunc dispatch")
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
		"(do (quasiquote ((unquote-splicing (quote ())))) (reenter2))", 200,
	)
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

func TestCallReentrancy_LazyEvalStateConcurrentMaterialization(t *testing.T) {
	t.Parallel()

	deadline := time.Now().Add(time.Minute)
	ctx, wantCounter := core.AdoptEvalState(context.Background(), deadline, 3)

	const workers = 2
	counters := make(chan *atomic.Int64, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !core.HasEvalState(ctx) {
				t.Error("missing eval state")
				return
			}
			counters <- core.EvalStructCounter(ctx)
		}()
	}
	wg.Wait()
	close(counters)

	for counter := range counters {
		assert.Same(t, wantCounter, counter)
		assert.Equal(t, int64(3), counter.Load())
	}
}

func TestCallReentrancy_StashedLazyCtxSurvivesPooledVMReuse(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(time.Hour))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	var stashed context.Context
	require.NoError(t, eng.Bind("stash-ctx", core.GoFunc{
		Name: "stash-ctx",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			stashed = ctx
			return core.Nil{}, nil
		},
	}))
	var liveCounter *atomic.Int64
	require.NoError(t, eng.Bind("mutate-counter", core.GoFunc{
		Name: "mutate-counter",
		Fn: func(ctx context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			liveCounter = core.EvalStructCounter(ctx)
			liveCounter.Add(7)
			return core.Nil{}, nil
		},
	}))

	_, err = eng.Eval(context.Background(), "def-stash", "(defn stash [] (stash-ctx))")
	require.NoError(t, err)
	_, err = eng.Eval(context.Background(), "def-mutate", "(defn mutate [] (mutate-counter))")
	require.NoError(t, err)

	_, err = eng.Call(context.Background(), "stash")
	require.NoError(t, err)
	require.NotNil(t, stashed)

	_, err = eng.Call(context.Background(), "mutate")
	require.NoError(t, err)
	require.NotNil(t, liveCounter)

	stashedCounter := core.EvalStructCounter(stashed)
	stashedDeadline := core.EvalDeadlineFrom(stashed)
	require.NotZero(t, stashedDeadline)
	assert.NotSame(t, liveCounter, stashedCounter)
	assert.Equal(t, int64(0), stashedCounter.Load())
	assert.Equal(t, int64(7), liveCounter.Load())

	liveCounter.Add(11)
	assert.Equal(t, int64(0), stashedCounter.Load())
	assert.Equal(t, stashedDeadline, core.EvalDeadlineFrom(stashed))
}
