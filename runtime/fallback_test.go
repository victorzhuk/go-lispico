package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// A macro whose expansion is a form the bytecode compiler cannot handle (a
// nested defmacro) must fall back to the tree-walker on the already-expanded
// form, so the macro body runs exactly once — not once during expansion and
// again during the fallback.
func TestBytecodeRuntime_UnsupportedFallbackExpandsOnce(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := context.Background()

	var ticks atomic.Int64
	require.NoError(t, eng.Bind("tick", core.GoFunc{
		Name: "tick",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			ticks.Add(1)
			return core.Nil{}, nil
		},
	}))

	_, err = eng.Eval(ctx, "macro",
		"(defmacro tricky [] (tick) (quote (do (defmacro inner [] 1) 42)))")
	require.NoError(t, err)

	res, err := eng.Eval(ctx, "use", "(tricky)")
	require.NoError(t, err)
	assert.True(t, res.Equals(core.Int{V: 42}), "tricky returns 42, got %v", res)

	assert.Equal(t, int64(1), ticks.Load(),
		"macro body must run once; ticks indicates double expansion on the unsupported fallback")
}
