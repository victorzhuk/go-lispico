package runtime

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestIntegration_FullWorkflow(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default(),
		WithMaxEvalDepth(500),
		WithTimeout(5*time.Second),
	)
	require.NoError(t, err)

	p := &mockPlugin{name: "test", version: "1.0.0"}
	require.NoError(t, eng.Use(p))

	bindBuiltin(eng, "+")
	bindBuiltin(eng, "*")

	_, err = eng.Eval(context.Background(), "init", "(def x 10)")
	require.NoError(t, err)

	require.NoError(t, eng.Bind("y", core.Int{V: 20}))

	_, err = eng.Eval(context.Background(), "calc", "(+ x y)")
	require.NoError(t, err)

	_, err = eng.Eval(context.Background(), "fn", "(defn multiply [a b] (* a b))")
	require.NoError(t, err)

	result, err := eng.Call(context.Background(), "multiply", core.Int{V: 3}, core.Int{V: 4})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 12}.Equals(result))

	stats := eng.Stats()
	assert.GreaterOrEqual(t, stats.TotalEvals, int64(3))
	assert.Equal(t, 1, stats.ActivePlugins)

	require.NoError(t, eng.Close())
}

func TestIntegration_HotReloadUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	bindBuiltin(eng, "+")

	file := filepath.Join(dir, "counter.lisp")
	err = os.WriteFile(file, []byte("(def counter 0)"), 0644)
	require.NoError(t, err)

	require.NoError(t, eng.Watch(context.Background(), dir))
	time.Sleep(600 * time.Millisecond)

	var evalCount atomic.Int64
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, _ = eng.Eval(ctx, "load", "counter")
					evalCount.Add(1)
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)

	err = os.WriteFile(file, []byte("(def counter 100)"), 0644)
	require.NoError(t, err)

	time.Sleep(600 * time.Millisecond)

	cancel()
	wg.Wait()

	total := evalCount.Load()
	assert.Greater(t, total, int64(10), "should have completed many evaluations during load")

	stats := eng.Stats()
	assert.GreaterOrEqual(t, stats.TotalEvals, total)

	val, ok := eng.RootEnv().Get("counter")
	require.True(t, ok)
	assert.True(t, core.Int{V: 100}.Equals(val))
}

func TestIntegration_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	bindBuiltin(eng, "+")
	bindBuiltin(eng, "*")

	_, err = eng.Eval(context.Background(), "init", "(defn add [a b] (+ a b))")
	require.NoError(t, err)

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var evalErrs atomic.Int64
	var callErrs atomic.Int64
	var panicCount atomic.Int64

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()

			for j := 0; j < 50; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				switch j % 4 {
				case 0:
					_, err := eng.Eval(ctx, "concurrent", "42")
					if err != nil {
						evalErrs.Add(1)
					}
				case 1:
					_, err := eng.Call(ctx, "add", core.Int{V: int64(id)}, core.Int{V: int64(j)})
					if err != nil {
						callErrs.Add(1)
					}
				case 2:
					_ = eng.Bind("temp", core.Int{V: int64(id * j)})
				case 3:
					_ = eng.Stats()
				}
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int64(0), panicCount.Load(), "no panics should occur")

	stats := eng.Stats()
	assert.Greater(t, stats.TotalEvals, int64(0))
}

func TestIntegration_PluginLifecycle(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	p1 := &mockPlugin{name: "alpha", version: "1.0.0"}
	p2 := &mockPlugin{name: "beta", version: "1.0.0"}

	require.NoError(t, eng.Use(p1))
	require.NoError(t, eng.Use(p2))

	plugins := eng.ListPlugins()
	require.Len(t, plugins, 2)

	require.NoError(t, eng.UnloadPlugin("alpha"))

	plugins = eng.ListPlugins()
	require.Len(t, plugins, 1)
	assert.Equal(t, "beta", plugins[0].Name)

	p2Reloaded := &mockPlugin{name: "beta", version: "2.0.0"}
	require.NoError(t, eng.ReloadPlugin(p2Reloaded))

	plugins = eng.ListPlugins()
	require.Len(t, plugins, 1)
	assert.Equal(t, "2.0.0", plugins[0].Version)
}

func TestIntegration_Callbacks(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	bindBuiltin(eng, "+")

	var evalEvents []EvalEvent
	var evalMu sync.Mutex
	eng.OnEval(func(e EvalEvent) {
		evalMu.Lock()
		evalEvents = append(evalEvents, e)
		evalMu.Unlock()
	})

	_, err = eng.Eval(context.Background(), "test", "(+ 1 2)")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		evalMu.Lock()
		defer evalMu.Unlock()
		return len(evalEvents) >= 1
	}, time.Second, 10*time.Millisecond)

	evalMu.Lock()
	assert.GreaterOrEqual(t, len(evalEvents), 1)
	assert.Equal(t, "test", evalEvents[0].Source)
	assert.NoError(t, evalEvents[0].Error)
	evalMu.Unlock()
}

func TestIntegration_EvalWithBindingsIsolation(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	require.NoError(t, eng.Bind("global-x", core.Int{V: 1}))

	bindings := map[string]core.Value{
		"local-x": core.Int{V: 100},
	}

	result, err := eng.EvalWithBindings(context.Background(), "local-x", bindings)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 100}.Equals(result))

	_, ok := eng.RootEnv().Get("local-x")
	assert.False(t, ok, "local binding should not leak to root env")

	result, err = eng.EvalWithBindings(context.Background(), "global-x", bindings)
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(result))
}

func TestIntegration_NoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	eng, err := New(slog.Default())
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, eng.Watch(context.Background(), dir))

	time.Sleep(50 * time.Millisecond)

	require.NoError(t, eng.Close())

	time.Sleep(100 * time.Millisecond)

	after := runtime.NumGoroutine()
	assert.Equal(t, before, after, "goroutine leak detected")
}

func TestIntegration_LoadDirAndEval(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	bindBuiltin(eng, "+")
	bindBuiltin(eng, "conj")

	dir := t.TempDir()

	files := map[string]string{
		"01_config.lisp": "(def config {:env :test})",
		"02_utils.lisp":  "(defn double [x] (+ x x))",
		"03_main.lisp":   "(def result (double 21))",
	}

	for name, content := range files {
		err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
		require.NoError(t, err)
	}

	require.NoError(t, eng.LoadDir(dir))

	val, ok := eng.RootEnv().Get("result")
	require.True(t, ok)
	assert.True(t, core.Int{V: 42}.Equals(val))

	result, err := eng.Call(context.Background(), "double", core.Int{V: 5})
	require.NoError(t, err)
	assert.True(t, core.Int{V: 10}.Equals(result))
}

func TestIntegration_StatsAccuracy(t *testing.T) {
	t.Parallel()

	eng, err := New(slog.Default())
	require.NoError(t, err)
	defer eng.Close()

	bindBuiltin(eng, "+")

	initial := eng.Stats()
	assert.Equal(t, int64(0), initial.TotalEvals)
	assert.Equal(t, int64(0), initial.TotalErrors)

	for i := 0; i < 5; i++ {
		_, _ = eng.Eval(context.Background(), "test", "(+ 1 2)")
	}

	_, _ = eng.Eval(context.Background(), "test", "undefined-var")

	stats := eng.Stats()
	assert.Equal(t, int64(6), stats.TotalEvals)
	assert.Equal(t, int64(1), stats.TotalErrors)
	assert.Greater(t, stats.TotalEvalNs, int64(0))
	assert.Greater(t, stats.AvgEvalNs, int64(0))
}
