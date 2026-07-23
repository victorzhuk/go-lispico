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
	"github.com/victorzhuk/go-lispico/core/vm"
)

func TestPanicBoundary_EvalRecoversGoFuncPanics(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts []EngineOption
	}{
		{name: "bytecode", opts: []EngineOption{WithBytecode(), WithDialect(clojure.Dialect())}},
		{name: "treewalker", opts: []EngineOption{WithTreeWalker(), WithDialect(clojure.Dialect())}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := New(nil, tt.opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })

			panicValue := fmt.Sprintf("%s panic", tt.name)
			require.NoError(t, eng.Bind("boom", core.GoFunc{
				Name: "boom",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					panic(panicValue)
				},
			}))

			var evalErr error
			require.NotPanics(t, func() {
				_, evalErr = eng.Eval(t.Context(), tt.name, "(boom)")
			})
			assertPanicError(t, evalErr, panicValue)
		})
	}
}

func TestPanicBoundary_CallAndFnCallRecoverGoFuncPanics(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts []EngineOption
	}{
		{name: "bytecode", opts: []EngineOption{WithBytecode()}},
		{name: "treewalker", opts: []EngineOption{WithTreeWalker()}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := New(nil, tt.opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })

			panicValue := fmt.Sprintf("%s panic", tt.name)
			require.NoError(t, eng.Bind("boom", core.GoFunc{
				Name: "boom",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					panic(panicValue)
				},
			}))
			fn, err := eng.Func("boom")
			require.NoError(t, err)

			var callErr error
			require.NotPanics(t, func() {
				_, callErr = eng.Call(t.Context(), "boom")
			})
			assertPanicError(t, callErr, panicValue)

			var fnErr error
			require.NotPanics(t, func() {
				_, fnErr = fn.Call(t.Context())
			})
			assertPanicError(t, fnErr, panicValue)
		})
	}
}

func TestPanicBoundary_HandlesRemainUsableAfterRecoveredPanic(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts []EngineOption
	}{
		{name: "bytecode", opts: []EngineOption{WithBytecode(), WithDialect(clojure.Dialect())}},
		{name: "treewalker", opts: []EngineOption{WithTreeWalker(), WithDialect(clojure.Dialect())}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := New(nil, tt.opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = eng.Close() })

			bindBuiltin(t, eng, "*")
			const panicValue = "dirty boundary"
			require.NoError(t, eng.Bind("boom", core.GoFunc{
				Name: "boom",
				Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
					panic(panicValue)
				},
			}))
			_, err = eng.Eval(t.Context(), "defs", "(defn call-boom [] (boom)) (defn id [x] x)")
			require.NoError(t, err)
			callBoom, err := eng.Func("call-boom")
			require.NoError(t, err)
			id, err := eng.Func("id")
			require.NoError(t, err)

			_, err = eng.Eval(t.Context(), "eval-panic", "(boom)")
			assertPanicError(t, err, panicValue)
			got, err := eng.Eval(t.Context(), "eval-ok", "42")
			require.NoError(t, err)
			assert.True(t, core.Int{V: 42}.Equals(got), "got %v", got)

			_, err = eng.Call(t.Context(), "boom")
			assertPanicError(t, err, panicValue)
			got, err = eng.Call(t.Context(), "id", core.Int{V: 7})
			require.NoError(t, err)
			assert.True(t, core.Int{V: 7}.Equals(got), "got %v", got)

			_, err = callBoom.Call(t.Context())
			assertPanicError(t, err, panicValue)
			got, err = id.Call(t.Context(), core.Int{V: 8})
			require.NoError(t, err)
			assert.True(t, core.Int{V: 8}.Equals(got), "got %v", got)
			_, err = eng.Eval(t.Context(), "redef", "(defn call-boom [x] (* x 2))")
			require.NoError(t, err)
			got, err = callBoom.Call(t.Context(), core.Int{V: 21})
			require.NoError(t, err, "the same Fn handle must recover and be usable after panic")
			assert.True(t, core.Int{V: 42}.Equals(got), "got %v", got)
		})
	}
}

func TestPanicBoundary_EvalPanicRecordsStatsAndCallbacksLikeReturnedError(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	boom := errors.New("returned boom")
	require.NoError(t, eng.Bind("fail", core.GoFunc{
		Name: "fail",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return nil, boom
		},
	}))
	require.NoError(t, eng.Bind("panic-fail", core.GoFunc{
		Name: "panic-fail",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			panic("panic boom")
		},
	}))

	var mu sync.Mutex
	var events []EvalEvent
	eng.OnEval(func(e EvalEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	_, err = eng.Eval(t.Context(), "returned", "(fail)")
	require.Error(t, err)
	statsAfterReturned := eng.Stats()

	_, err = eng.Eval(t.Context(), "panicked", "(panic-fail)")
	assertPanicError(t, err, "panic boom")
	statsAfterPanic := eng.Stats()

	assert.Equal(t, statsAfterReturned.TotalEvals+1, statsAfterPanic.TotalEvals)
	assert.Equal(t, statsAfterReturned.TotalErrors+1, statsAfterPanic.TotalErrors)

	mu.Lock()
	require.Len(t, events, 2)
	assert.Equal(t, "returned", events[0].Source)
	assert.Error(t, events[0].Error)
	assert.Equal(t, "panicked", events[1].Source)
	assertPanicError(t, events[1].Error, "panic boom")
	assert.Greater(t, events[0].Duration, time.Duration(0))
	assert.Greater(t, events[1].Duration, time.Duration(0))
	mu.Unlock()
}

func TestPanicBoundary_BytecodeEvaluatorApplyDropsPanickedVM(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	impl := eng.(*engineImpl)
	be := impl.bytecodeEvaluator
	require.NotNil(t, be)

	var created atomic.Int64
	be.vmPool = sync.Pool{
		New: func() any {
			created.Add(1)
			return vm.New(be.globals, vm.WithMaxDepth(be.maxDepth), vm.WithEvaluator(be), vm.WithMaxStructuralDepth(be.maxStructuralDepth))
		},
	}

	panicFn := core.GoFunc{
		Name: "boom",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			panic("apply panic")
		},
	}
	require.Panics(t, func() {
		_, _ = be.Apply(t.Context(), panicFn, nil, impl.rootEnv)
	})
	assert.Equal(t, int64(1), created.Load())

	safeFn := core.GoFunc{
		Name: "safe",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			return core.Int{V: 99}, nil
		},
	}
	got, err := be.Apply(t.Context(), safeFn, nil, impl.rootEnv)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 99}.Equals(got), "got %v", got)
	assert.Equal(t, int64(2), created.Load(), "panicked Apply VM must not return to the pool")
}

func assertPanicError(t *testing.T, err error, wantPanic any) {
	t.Helper()
	require.Error(t, err)
	var lerr *core.LispicoError
	require.True(t, errors.As(err, &lerr), "expected *core.LispicoError, got %T: %v", err, err)
	assert.Equal(t, core.CodePanic, lerr.Code)
	assert.Contains(t, lerr.Message, fmt.Sprint(wantPanic))
}
