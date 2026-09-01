package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func TestStdlibBootstrap_TwoEnginesProduceIdenticalResults(t *testing.T) {
	first := newBytecodeStdlibEngine(t)
	firstResults := stdlibBootstrapResults(t, first)
	require.NoError(t, first.Close())

	second := newBytecodeStdlibEngine(t)
	secondResults := stdlibBootstrapResults(t, second)
	require.NoError(t, second.Close())

	require.Len(t, firstResults, len(secondResults))
	for i := range firstResults {
		assert.True(t, firstResults[i].Equals(secondResults[i]), "result %d mismatch: first=%v second=%v", i, firstResults[i], secondResults[i])
	}
}

func TestStdlibBootstrap_TwoEnginesProduceIdenticalResultsAcrossDialects(t *testing.T) {
	testCases := []struct {
		name string
		opts []EngineOption
	}{
		{name: "cl"},
		{name: "clojure", opts: []EngineOption{WithDialect(clojure.Dialect())}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			first := newBytecodeStdlibEngineWithOptions(t, tc.opts...)
			firstResults := stdlibBootstrapResults(t, first)
			require.NoError(t, first.Close())

			second := newBytecodeStdlibEngineWithOptions(t, tc.opts...)
			secondResults := stdlibBootstrapResults(t, second)
			require.NoError(t, second.Close())

			require.Len(t, firstResults, len(secondResults))
			for i := range firstResults {
				assert.True(t, firstResults[i].Equals(secondResults[i]), "result %d mismatch: first=%v second=%v", i, firstResults[i], secondResults[i])
			}
		})
	}
}

func TestStdlibBootstrap_ConcurrentEngineConstructionRaceSafe(t *testing.T) {
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
			if err != nil {
				errs <- err
				return
			}
			defer eng.Close()
			if err := eng.Use(stdlib.New()); err != nil {
				errs <- err
				return
			}
			results, err := stdlibBootstrapResultsNoFatal(eng)
			if err != nil {
				errs <- err
				return
			}
			if len(results) != 2 || !(core.Int{V: 9}).Equals(results[0]) || !(core.Int{V: 7}).Equals(results[1]) {
				errs <- fmt.Errorf("unexpected stdlib results: %v", results)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestUnloadPlugin_InvalidatesMacroCache(t *testing.T) {
	eng := newBytecodeStdlibEngine(t)
	defer eng.Close()

	_, err := eng.Eval(context.Background(), "warm", "(-> 1 (+ 2))")
	require.NoError(t, err)
	require.NoError(t, eng.UnloadPlugin(stdlib.New().Name()))

	_, err = eng.Eval(context.Background(), "after-unload", "(-> 1 (+ 2))")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined")
}

func TestReloadPlugin_InvalidatesMacroCache(t *testing.T) {
	eng := newBytecodeStdlibEngine(t)
	defer eng.Close()

	ctx := context.Background()

	_, err := eng.Eval(ctx, "override", "(defmacro -> [x & forms] '(+ 4 5))")
	require.NoError(t, err)

	beforeReload, err := eng.Eval(ctx, "before-reload", "(-> 1 (+ 2))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 9}.Equals(beforeReload), "custom macro override path: (-> 1 (+ 2)) => 9")

	require.NoError(t, eng.ReloadPlugin(stdlib.New()))

	afterReload, err := eng.Eval(ctx, "after-reload", "(-> 1 (+ 2))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(afterReload), "reloaded stdlib macro path: (-> 1 (+ 2)) => 3")
}

func TestStdlibBootstrap_SameDialectEnginesIsolateDefinitions(t *testing.T) {
	first := newBytecodeStdlibEngine(t)
	defer first.Close()
	second := newBytecodeStdlibEngine(t)
	defer second.Close()

	assertStdlibBootstrapBehavior(t, first)
	assertStdlibBootstrapBehavior(t, second)

	_, err := first.Eval(context.Background(), "define-marker", "(def isolation-marker 99)")
	require.NoError(t, err)

	got, err := first.Eval(context.Background(), "read-marker", "isolation-marker")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 99}.Equals(got))

	_, err = second.Eval(context.Background(), "other-engine-read-marker", "isolation-marker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined")
}

func TestStdlibBootstrap_UnloadPluginOneEngineLeavesOtherUsable(t *testing.T) {
	first := newBytecodeStdlibEngine(t)
	defer first.Close()
	second := newBytecodeStdlibEngine(t)
	defer second.Close()

	_, err := second.Eval(context.Background(), "bind-second", "(def retained-in-second 7)")
	require.NoError(t, err)

	require.NoError(t, first.UnloadPlugin(stdlib.New().Name()))

	_, err = second.Eval(context.Background(), "use-second", "(-> 1 (+ 2) (* 3))")
	require.NoError(t, err)
	_, err = second.Eval(context.Background(), "read-second", "retained-in-second")
	require.NoError(t, err)

	_, err = first.Eval(context.Background(), "use-first", "(-> 1 (+ 2) (* 3))")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined")
}

func TestStdlibBootstrap_DialectDefinitionsAreSeparated(t *testing.T) {
	cl := newBytecodeStdlibEngineWithOptions(t)
	defer cl.Close()
	clojureEng := newBytecodeStdlibEngineWithOptions(t, WithDialect(clojure.Dialect()))
	defer clojureEng.Close()

	assertStdlibBootstrapBehavior(t, cl)
	assertStdlibBootstrapBehavior(t, clojureEng)
}

func newBytecodeStdlibEngineWithOptions(t *testing.T, opts ...EngineOption) Engine {
	t.Helper()
	options := make([]EngineOption, 0, len(opts)+1)
	options = append(options, WithBytecode())
	options = append(options, opts...)

	eng, err := New(nil, options...)
	require.NoError(t, err)
	require.NoError(t, eng.Use(stdlib.New()))
	return eng
}

func newBytecodeStdlibEngine(t *testing.T) Engine {
	return newBytecodeStdlibEngineWithOptions(t, WithDialect(clojure.Dialect()))
}

func assertStdlibBootstrapBehavior(t *testing.T, eng Engine) {
	t.Helper()
	results := stdlibBootstrapResults(t, eng)
	assert.True(t, core.Int{V: 9}.Equals(results[0]), "thread-first result = %v", results[0])
	assert.True(t, core.Int{V: 7}.Equals(results[1]), "get-in result = %v", results[1])
}

func stdlibBootstrapResults(t *testing.T, eng Engine) []core.Value {
	t.Helper()
	ctx := context.Background()
	threaded, err := eng.Eval(ctx, "thread-first", "(-> 1 (+ 2) (* 3))")
	require.NoError(t, err)
	getIn, err := eng.Eval(ctx, "get-in", "(get-in (hash-map :a (hash-map :b 7)) (vector :a :b))")
	require.NoError(t, err)
	return []core.Value{threaded, getIn}
}

func stdlibBootstrapResultsNoFatal(eng Engine) ([]core.Value, error) {
	ctx := context.Background()
	threaded, err := eng.Eval(ctx, "thread-first", "(-> 1 (+ 2) (* 3))")
	if err != nil {
		return nil, err
	}
	getIn, err := eng.Eval(ctx, "get-in", "(get-in (hash-map :a (hash-map :b 7)) (vector :a :b))")
	if err != nil {
		return nil, err
	}
	return []core.Value{threaded, getIn}, nil
}
