package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

const reentrantCallDepthMapSource = `(defn deep (n) (if (= n 0) 0 (first (map (fn [x] (deep (- n 1))) (list 1)))))`

func newReentrantCallDepthEngine(t testing.TB, opts ...EngineOption) Engine {
	t.Helper()
	all := []EngineOption{WithDialect(clojure.Dialect())}
	all = append(all, opts...)
	eng, err := New(nil, all...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	return eng
}

func assertCallDepthExceeded(t testing.TB, err error) {
	t.Helper()
	require.Error(t, err)
	var lispicoErr *core.LispicoError
	require.True(t, errors.As(err, &lispicoErr), "expected *core.LispicoError, got %T", err)
	assert.Equal(t, "EvalError", lispicoErr.Code)
	assert.Contains(t, lispicoErr.Error(), "maximum call depth exceeded")
}

func TestVM_ReentrantCallDepth_Map(t *testing.T) {
	t.Parallel()

	src := reentrantCallDepthMapSource + ` (deep 50)`
	ctx := t.Context()

	vmEng := newReentrantCallDepthEngine(t, WithBytecode(), WithMaxEvalDepth(10))
	_, err := vmEng.Eval(ctx, "vm-map", src)
	assertCallDepthExceeded(t, err)

	treeEng := newReentrantCallDepthEngine(t, WithTreeWalker(), WithMaxEvalDepth(10))
	_, err = treeEng.Eval(ctx, "tree-map", src)
	assertCallDepthExceeded(t, err)
}

func TestVM_ReentrantCallDepth_HigherOrderBuiltins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		def  string
	}{
		{name: "map", def: reentrantCallDepthMapSource},
		{name: "filter", def: `(defn deep (n) (if (= n 0) 0 (first (filter (fn [x] (do (deep (- n 1)) true)) (list 1)))))`},
		{name: "reduce", def: `(defn deep (n) (if (= n 0) 0 (reduce (fn [a x] (deep (- n 1))) 0 (list 1))))`},
		{name: "apply", def: `(defn deep (n) (if (= n 0) 0 (apply (fn [x] (deep (- n 1))) (list 1))))`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eng := newReentrantCallDepthEngine(t, WithBytecode(), WithMaxEvalDepth(10))
			_, err := eng.Eval(t.Context(), "vm-"+tt.name, tt.def+` (deep 50)`)
			assertCallDepthExceeded(t, err)
		})
	}
}

func TestVM_ReentrantCallDepth_UnderLimit(t *testing.T) {
	t.Parallel()

	eng := newReentrantCallDepthEngine(t, WithBytecode(), WithMaxEvalDepth(10))
	ctx := t.Context()
	src := reentrantCallDepthMapSource + ` (deep 10)`

	for i := range 3 {
		got, err := eng.Eval(ctx, "under-limit", src)
		require.NoError(t, err, "run %d", i)
		assert.True(t, (core.Int{V: 0}).Equals(got), "run %d: got %v", i, got)
	}
}

func TestVM_ReentrantCallDepth_ConcurrentBounded(t *testing.T) {
	t.Parallel()

	eng := newReentrantCallDepthEngine(t, WithBytecode())
	ctx := t.Context()

	const workers = 8
	var wg sync.WaitGroup
	for id := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := eng.Eval(core.DetachEvalState(ctx), "under-limit-concurrent", reentrantCallDepthMapSource+` (deep 50)`)
			if err != nil {
				t.Errorf("worker %d: %v", id, err)
				return
			}
			if !(core.Int{V: 0}).Equals(got) {
				t.Errorf("worker %d: got %v", id, got)
			}
		}()
	}
	wg.Wait()
}

func TestVM_ReentrantCallDepth_ConcurrentIsolatedBounds(t *testing.T) {
	t.Parallel()

	eng := newReentrantCallDepthEngine(t, WithBytecode())
	ctx := t.Context()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, err := eng.Eval(core.DetachEvalState(ctx), "over-limit-concurrent", reentrantCallDepthMapSource+` (deep 1200)`)
		assertCallDepthExceeded(t, err)
	}()

	go func() {
		defer wg.Done()
		got, err := eng.Eval(core.DetachEvalState(ctx), "under-limit-concurrent", reentrantCallDepthMapSource+` (deep 50)`)
		require.NoError(t, err)
		assert.True(t, (core.Int{V: 0}).Equals(got), "got %v", got)
	}()

	wg.Wait()
}

func TestVM_ReentrantCallDepth_DecrementOnPanic(t *testing.T) {
	t.Parallel()

	eng := newReentrantCallDepthEngine(t, WithBytecode(), WithMaxEvalDepth(10))
	ctx := core.EnsureEvalState(t.Context())
	require.NoError(t, eng.Bind("boom", core.GoFunc{
		Name: "boom",
		Fn: func(context.Context, core.Evaluator, []core.Value, *core.Env) (core.Value, error) {
			panic("boom")
		},
	}))

	_, err := eng.Eval(ctx, "panic", `(defn boom-callback [] (first (map (fn [x] (boom)) (list 1))))`)
	require.NoError(t, err)

	_, err = eng.Call(ctx, "boom-callback")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "boom"), "got %v", err)

	got, err := eng.Eval(ctx, "after-panic", reentrantCallDepthMapSource+` (deep 10)`)
	require.NoError(t, err)
	assert.True(t, (core.Int{V: 0}).Equals(got), "got %v", got)
}
