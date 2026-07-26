package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func assertEvaluatorSelected(t *testing.T, eng Engine, wantBytecode bool) {
	t.Helper()

	impl, ok := eng.(*engineImpl)
	assert.True(t, ok)
	if !ok {
		t.FailNow()
	}

	if wantBytecode {
		assert.Same(t, impl.bytecodeEvaluator, impl.evaluator)
	} else {
		assert.Nil(t, impl.bytecodeEvaluator)
		assert.Same(t, impl.treeWalker, impl.evaluator)
	}
}

func TestNew_EvaluatorSelection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		opts         []EngineOption
		wantBytecode bool
	}{
		{
			name:         "default",
			opts:         nil,
			wantBytecode: true,
		},
		{
			name:         "with-treewalker",
			opts:         []EngineOption{WithTreeWalker()},
			wantBytecode: false,
		},
		{
			name:         "with-treewalker-before-bytecode",
			opts:         []EngineOption{WithTreeWalker(), WithBytecode()},
			wantBytecode: true,
		},
		{
			name:         "with-bytecode-before-treewalker",
			opts:         []EngineOption{WithBytecode(), WithTreeWalker()},
			wantBytecode: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := New(nil, tc.opts...)
			if !assert.NoError(t, err) {
				return
			}
			defer eng.Close()
			assertEvaluatorSelected(t, eng, tc.wantBytecode)
		})
	}
}

func TestNew_EvaluatorSelectionExecutesSelectedPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	source := "(if nil 1 2)"

	t.Run("default uses bytecode", func(t *testing.T) {
		eng, err := New(nil)
		if !assert.NoError(t, err) {
			return
		}
		defer eng.Close()

		got, err := eng.Eval(ctx, "compiled-subset", source)
		if !assert.NoError(t, err) {
			return
		}

		assert.True(t, core.Int{V: 2}.Equals(got), "got %v", got)
		assert.Equal(t, 1, cacheCount(t, eng))
	})

	t.Run("with-treewalker stays off VM", func(t *testing.T) {
		eng, err := New(nil, WithTreeWalker())
		if !assert.NoError(t, err) {
			return
		}
		defer eng.Close()

		got, err := eng.Eval(ctx, "compiled-subset", source)
		if !assert.NoError(t, err) {
			return
		}

		assert.True(t, core.Int{V: 2}.Equals(got), "got %v", got)
		impl, ok := eng.(*engineImpl)
		if assert.True(t, ok) {
			assert.Nil(t, impl.bytecodeEvaluator)
		}
	})
}

func TestNew_DefaultOptions(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)

	assert.NoError(t, err)
	assert.NotNil(t, eng)
	assert.NotNil(t, eng.RootEnv())
	assert.NotNil(t, eng.Registry())
	assert.NoError(t, eng.Close())
}

func TestNew_CustomOptions(t *testing.T) {
	t.Parallel()

	log := slog.Default()
	eng, err := New(
		log,
		WithMaxEvalDepth(500),
		WithTimeout(10*time.Second),
	)

	assert.NoError(t, err)
	assert.NotNil(t, eng)
	assert.NoError(t, eng.Close())
}

func TestNew_NilLogger(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)

	assert.NoError(t, err)
	assert.NotNil(t, eng)
	assert.NoError(t, eng.Close())
}

// TestNew_NilLoggerSharesProcessWideLogger pins task 2.1: every engine built
// with a nil logger gets the same *slog.Logger instance rather than one
// built per engine.
func TestNew_NilLoggerSharesProcessWideLogger(t *testing.T) {
	t.Parallel()

	engA, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = engA.Close() })

	engB, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = engB.Close() })

	implA, ok := engA.(*engineImpl)
	require.True(t, ok)
	implB, ok := engB.(*engineImpl)
	require.True(t, ok)

	assert.Same(t, discardLogger, implA.logger)
	assert.Same(t, implA.logger, implB.logger)
}

// TestNew_NilLoggerAllocsBudget pins task 2.1: New(nil)+Close() must not
// construct a discard logger per engine. The warm-up call settles any
// process-wide first-touch cost (e.g. discardLogger's own initialization)
// outside the measured loop. The budget is a ceiling, matching the other
// allocation tests in this package: it catches a reintroduced per-engine
// logger without failing when an unrelated construction site gets cheaper.
func TestNew_NilLoggerAllocsBudget(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	warm, err := New(nil)
	require.NoError(t, err)
	require.NoError(t, warm.Close())

	allocs := testing.AllocsPerRun(200, func() {
		eng, err := New(nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = eng.Close()
	})
	assert.LessOrEqual(t, allocs, float64(17), "engine construction+close alloc budget regressed, got %v", allocs)
}

// TestNew_DistinctLoggersStayIndependent pins task 2.3: an explicitly passed
// logger is used exactly as supplied, and two engines given distinct loggers
// never cross-emit into each other's sink.
func TestNew_DistinctLoggersStayIndependent(t *testing.T) {
	t.Parallel()

	var bufA, bufB bytes.Buffer
	logA := slog.New(slog.NewTextHandler(&bufA, nil))
	logB := slog.New(slog.NewTextHandler(&bufB, nil))

	engA, err := New(logA)
	require.NoError(t, err)
	t.Cleanup(func() { _ = engA.Close() })

	engB, err := New(logB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = engB.Close() })

	implA, ok := engA.(*engineImpl)
	require.True(t, ok)
	implB, ok := engB.(*engineImpl)
	require.True(t, ok)
	assert.Same(t, logA, implA.logger, "an explicit logger must never be replaced by the shared discard logger")
	assert.Same(t, logB, implB.logger)

	implA.logger.Info("marker-from-a")
	implB.logger.Info("marker-from-b")

	assert.Contains(t, bufA.String(), "marker-from-a")
	assert.NotContains(t, bufA.String(), "marker-from-b")
	assert.Contains(t, bufB.String(), "marker-from-b")
	assert.NotContains(t, bufB.String(), "marker-from-a")
}

func TestClose_NoWatcher(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)

	err = eng.Close()

	assert.NoError(t, err)
}

func TestStats_Initial(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)
	defer eng.Close()

	stats := eng.Stats()

	assert.Equal(t, int64(0), stats.TotalEvals)
	assert.Equal(t, int64(0), stats.TotalErrors)
	assert.Empty(t, stats.PluginCallCounts)
}

func TestStats_RecordEval(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)
	defer eng.Close()

	eng.Bind("+", core.GoFunc{
		Name: "+",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			sum := int64(0)
			for _, arg := range args {
				if i, ok := arg.(core.Int); ok {
					sum += i.V
				}
			}
			return core.Int{V: sum}, nil
		},
	})

	_, _ = eng.Eval(t.Context(), "test", "(+ 1 2)")
	_, _ = eng.Eval(t.Context(), "test", "undefined-symbol")
	_, _ = eng.Eval(t.Context(), "test", "(+ 2 3)")

	stats := eng.Stats()
	assert.Equal(t, int64(3), stats.TotalEvals)
	assert.Equal(t, int64(1), stats.TotalErrors)
}

func TestStats_RecordPluginCall(t *testing.T) {
	t.Parallel()

	eng, err := New(nil)
	assert.NoError(t, err)
	defer eng.Close()

	eng.Bind("foo", core.GoFunc{
		Name: "foo",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	})
	eng.Bind("bar", core.GoFunc{
		Name: "bar",
		Fn: func(_ context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	})

	_, _ = eng.Call(t.Context(), "foo")
	_, _ = eng.Call(t.Context(), "foo")
	_, _ = eng.Call(t.Context(), "bar")

	stats := eng.Stats()
	assert.Equal(t, int64(2), stats.PluginCallCounts["foo"])
	assert.Equal(t, int64(1), stats.PluginCallCounts["bar"])
}
