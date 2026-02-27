package runtime

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/victorzhuk/go-lispico/core"
)

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
	eng, err := New(log,
		WithMaxEvalDepth(500),
		WithTimeout(10*time.Second),
		WithHotReloadDir("/tmp/scripts"),
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
