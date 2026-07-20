package runtime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
