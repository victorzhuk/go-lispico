package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

func TestEngineDeadline_BytecodeLazyTimeoutStillFires(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithTimeout(10*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-slow", "(defn slow [] (loop [n 100000000] (if (= n 0) n (recur (- n 1)))))")
	require.NoError(t, err)
	bindBuiltin(t, eng, "=")
	bindBuiltin(t, eng, "-")

	_, err = eng.Call(context.Background(), "slow")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
}

func TestEngineDeadline_UnobservedBoundaryReadsNoClock(t *testing.T) {
	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	ctx := t.Context()
	_, err = eng.Eval(ctx, "def-pick", "(defn pick [a b] a)")
	require.NoError(t, err)
	fn, err := eng.Func("pick")
	require.NoError(t, err)

	var ticks atomic.Int64
	restore := nowFunc
	nowFunc = func() time.Time { return time.Unix(0, ticks.Add(1)) }
	t.Cleanup(func() { nowFunc = restore })

	_, err = eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	_, err = fn.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(0), ticks.Load())

	eng.OnPluginCall(func(PluginCallEvent) {})
	_, err = eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, err)
	assert.Greater(t, ticks.Load(), int64(0))
}
