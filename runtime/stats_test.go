package runtime

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"

	"context"
	"testing"
)

func TestStats_InitialState(t *testing.T) {
	s := newStats()
	snap := s.Snapshot()

	assert.Equal(t, int64(0), snap.TotalEvals)
	assert.Equal(t, int64(0), snap.TotalErrors)
	assert.Equal(t, int64(0), snap.AvgEvalNs)
	assert.Equal(t, 0, snap.ActivePlugins)
	assert.GreaterOrEqual(t, snap.Uptime.Milliseconds(), int64(0))
	assert.Empty(t, snap.PluginCallCounts)
}

func TestStats_RecordEval_TracksCountErrorsTiming(t *testing.T) {
	s := newStats()

	s.recordEval(100*time.Microsecond, nil)
	s.recordEval(200*time.Microsecond, assert.AnError)
	s.recordEval(300*time.Microsecond, nil)

	snap := s.Snapshot()
	assert.Equal(t, int64(3), snap.TotalEvals)
	assert.Equal(t, int64(1), snap.TotalErrors)
	assert.Equal(t, int64(600*1000), snap.TotalEvalNs)
	assert.Equal(t, int64(200*1000), snap.AvgEvalNs)
}

func TestStats_AvgEvalNs_ZeroWhenNoEvals(t *testing.T) {
	s := newStats()
	snap := s.Snapshot()

	assert.Equal(t, int64(0), snap.AvgEvalNs)
}

func TestStats_Uptime_Increases(t *testing.T) {
	s := newStats()
	firstSnap := s.Snapshot()

	time.Sleep(10 * time.Millisecond)

	secondSnap := s.Snapshot()
	assert.Greater(t, secondSnap.Uptime, firstSnap.Uptime)
}

func TestStats_RecordPluginCall_PerFunction(t *testing.T) {
	s := newStats()

	s.countPluginCall("foo")
	s.countPluginCall("foo")
	s.countPluginCall("bar")

	snap := s.Snapshot()
	assert.Equal(t, int64(2), snap.PluginCallCounts["foo"])
	assert.Equal(t, int64(1), snap.PluginCallCounts["bar"])
}

func TestStats_ActivePlugins(t *testing.T) {
	s := newStats()

	s.incPlugins()
	s.incPlugins()
	s.incPlugins()
	s.decPlugins()

	assert.Equal(t, int64(2), s.activePlugins.Load())
}

func TestEngine_OnEval_CallbackFires(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eng, err := New(log)
	assert.NoError(t, err)
	defer eng.Close()

	var mu sync.Mutex
	var events []EvalEvent
	eng.OnEval(func(e EvalEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	_, err = eng.Eval(context.Background(), "test", "42")
	assert.NoError(t, err)

	mu.Lock()
	assert.Len(t, events, 1)
	assert.Equal(t, "test", events[0].Source)
	assert.NoError(t, events[0].Error)
	assert.Greater(t, events[0].Duration, time.Duration(0))
	mu.Unlock()
}

func TestEngine_OnEval_CallbackFiresWithError(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eng, err := New(log)
	assert.NoError(t, err)
	defer eng.Close()

	var mu sync.Mutex
	var events []EvalEvent
	eng.OnEval(func(e EvalEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	_, err = eng.Eval(context.Background(), "test", "(undefined-fn)")
	assert.Error(t, err)

	mu.Lock()
	assert.Len(t, events, 1)
	assert.Equal(t, "test", events[0].Source)
	assert.Error(t, events[0].Error)
	mu.Unlock()
}

func TestEngine_OnPluginCall_CallbackFires(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eng, err := New(log)
	assert.NoError(t, err)
	defer eng.Close()

	eng.Bind("test-fn", core.GoFunc{
		Name: "test-fn",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return core.Int{V: 42}, nil
		},
	})

	var mu sync.Mutex
	var events []PluginCallEvent
	eng.OnPluginCall(func(e PluginCallEvent) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	_, err = eng.Call(context.Background(), "test-fn", core.Int{V: 1})
	assert.NoError(t, err)

	mu.Lock()
	assert.Len(t, events, 1)
	assert.Equal(t, "test-fn", events[0].Function)
	assert.Greater(t, events[0].Duration, time.Duration(0))
	mu.Unlock()
}

func TestEngine_OnEval_MultipleCallbacks(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eng, err := New(log)
	assert.NoError(t, err)
	defer eng.Close()

	var mu sync.Mutex
	var callCount int
	eng.OnEval(func(e EvalEvent) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})
	eng.OnEval(func(e EvalEvent) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	_, err = eng.Eval(context.Background(), "test", "42")
	assert.NoError(t, err)

	mu.Lock()
	assert.Equal(t, 2, callCount)
	mu.Unlock()
}

func TestEngine_Stats_ReturnsSnapshot(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eng, err := New(log)
	assert.NoError(t, err)
	defer eng.Close()

	_, err = eng.Eval(context.Background(), "test", "42")
	assert.NoError(t, err)
	_, err = eng.Eval(context.Background(), "test", `"hello"`)
	assert.NoError(t, err)

	stats := eng.Stats()
	assert.Equal(t, int64(2), stats.TotalEvals)
	assert.Equal(t, int64(0), stats.TotalErrors)
	assert.Greater(t, stats.Uptime, time.Duration(0))
}

func TestEngine_Stats_TracksErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eng, err := New(log)
	assert.NoError(t, err)
	defer eng.Close()

	_, _ = eng.Eval(context.Background(), "test", "(undefined)")

	stats := eng.Stats()
	assert.Equal(t, int64(1), stats.TotalEvals)
	assert.Equal(t, int64(1), stats.TotalErrors)
}

func statsOverflowName(i int) string {
	return "overflow-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)) + "!"
}

// TestStats_OverflowKeepsLiveCountersConnected proves a flush-on-overflow does
// not detach the counters already cached by warm Engine.Call entries, Fn, and
// PinnedFn handles: post-flush calls through every cached path must still be
// visible in Snapshot while the counter map stays bounded.
func TestStats_OverflowKeepsLiveCountersConnected(t *testing.T) {
	t.Parallel()

	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-id", "(defun id (x) x)")
	require.NoError(t, err)
	fn, err := eng.Func("id")
	require.NoError(t, err)
	pinned := fn.Pin()
	require.NotNil(t, pinned)

	// Warm every path: the call-cache entry, the Fn handle, the PinnedFn handle.
	_, err = eng.Call(ctx, "id", core.Int{V: 1})
	require.NoError(t, err)
	_, err = fn.Call(ctx, core.Int{V: 2})
	require.NoError(t, err)
	_, err = pinned.Call(ctx, core.Int{V: 3})
	require.NoError(t, err)

	// Distinct undefined names push the counter map past its bound and force a flush.
	for i := range maxCallCacheEntries + 10 {
		_, err := eng.Call(ctx, statsOverflowName(i))
		require.Error(t, err)
	}

	before := eng.Stats().PluginCallCounts["id"]
	_, err = eng.Call(ctx, "id", core.Int{V: 4})
	require.NoError(t, err)
	_, err = fn.Call(ctx, core.Int{V: 5})
	require.NoError(t, err)
	_, err = pinned.Call(ctx, core.Int{V: 6})
	require.NoError(t, err)
	after := eng.Stats().PluginCallCounts["id"]

	assert.Equal(t, before+3, after,
		"calls via warm Engine.Call, Fn, and PinnedFn must remain visible after a counter flush")
	assert.LessOrEqual(t, len(eng.Stats().PluginCallCounts), maxCallCacheEntries,
		"counter map must stay bounded across overflows")
}
