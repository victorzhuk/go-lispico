package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestCall_StatsAccurateWithoutCallbacks proves per-function call counts stay
// correct on the lazy path (no OnPluginCall registered) — countPluginCall
// runs unconditionally even though timing and callback firing are skipped.
func TestCall_StatsAccurateWithoutCallbacks(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("noop", core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	}))

	ctx := context.Background()
	const n = 5
	for range n {
		_, err := eng.Call(ctx, "noop")
		require.NoError(t, err)
	}
	_, err = eng.Call(ctx, "missing")
	require.Error(t, err)

	stats := eng.Stats()
	assert.Equal(t, int64(n), stats.PluginCallCounts["noop"])
	assert.Equal(t, int64(1), stats.PluginCallCounts["missing"])
}

// TestCall_CallbackFiresWithDurationWhenRegistered proves OnPluginCall still
// fires with the function name and a measured duration once registered, and
// that registering a callback doesn't disturb the count.
func TestCall_CallbackFiresWithDurationWhenRegistered(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("noop", core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	}))

	var mu sync.Mutex
	var events []PluginCallEvent
	eng.OnPluginCall(func(e PluginCallEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	ctx := context.Background()
	_, err = eng.Call(ctx, "noop")
	require.NoError(t, err)
	_, err = eng.Call(ctx, "noop")
	require.NoError(t, err)

	mu.Lock()
	require.Len(t, events, 2)
	assert.Equal(t, "noop", events[0].Function)
	assert.GreaterOrEqual(t, events[0].Duration, time.Duration(0))
	mu.Unlock()

	stats := eng.Stats()
	assert.Equal(t, int64(2), stats.PluginCallCounts["noop"])
}

// TestCall_ConcurrentRegistrationAndCall exercises OnPluginCall registration
// racing against concurrent Call invocations under -race: the one-way
// callbacksActive flag and the sync.Map counter must stay consistent no
// matter when the flag flips relative to in-flight Calls.
func TestCall_ConcurrentRegistrationAndCall(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })

	require.NoError(t, eng.Bind("noop", core.GoFunc{
		Name: "noop",
		Fn: func(_ context.Context, _ core.Evaluator, _ []core.Value, _ *core.Env) (core.Value, error) {
			return core.Nil{}, nil
		},
	}))

	const workers = 8
	const iterations = 200
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			eng.OnPluginCall(func(PluginCallEvent) {})
		}
	}()

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				_, err := eng.Call(ctx, "noop")
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()

	stats := eng.Stats()
	assert.Equal(t, int64(workers*iterations), stats.PluginCallCounts["noop"])
}

// TestCall_SteadyStateAllocatesOnlyValues proves the boundary contract for
// Call: with no callbacks registered and a GoFunc-free target (no lazy
// re-entrancy evalState wrap), the only allocation on the steady-state path
// is the variadic args slice at the call site — no eval-state, chunk, or
// stats allocation. A measured value >=2 would mean one of those got
// reintroduced.
func TestCall_SteadyStateAllocatesOnlyValues(t *testing.T) {
	if raceEnabled {
		t.Skip("alloc counts are unreliable under the race detector")
	}

	eng, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()))
	require.NoError(t, err)
	defer eng.Close()

	_, err = eng.Eval(context.Background(), "setup", "(defn pick [a b] a)")
	require.NoError(t, err)

	ctx := context.Background()
	_, err = eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2}) // warm the chunk cache and the stats counter
	require.NoError(t, err)

	allocs := testing.AllocsPerRun(200, func() {
		_, _ = eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
	})
	assert.LessOrEqual(t, allocs, float64(1))
}
