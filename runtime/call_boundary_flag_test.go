package runtime

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
)

// TestCallBoundary_FlagTransitionsRouteNextCall walks one engine through the
// condition transitions of the lean-boundary flag and verifies each routes
// the very next call correctly: fast calls with exact stats and no callback
// traffic, general calls once a callback is registered (flag one-way), a
// ctx-borne meter that routes general without touching the engine flag, and
// a plain context that resumes the fast path.
func TestCallBoundary_FlagTransitionsRouteNextCall(t *testing.T) {
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	impl := eng.(*engineImpl)
	ctx := t.Context()

	_, err = eng.Eval(ctx, "def-pick", "(defun pick (a b) a)")
	require.NoError(t, err)

	// Phase 1: fast condition — flag set, no callbacks, exact stats.
	require.True(t, impl.fastPath.Load())
	for range 3 {
		got, callErr := eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
		require.NoError(t, callErr)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
	}
	assert.Equal(t, int64(3), eng.Stats().PluginCallCounts["pick"])

	// Phase 2: registering a callback flips the flag; the next call takes
	// the general path and fires it, stats cumulative.
	var mu sync.Mutex
	var events []PluginCallEvent
	eng.OnPluginCall(func(ev PluginCallEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	require.False(t, impl.fastPath.Load(), "callback registration must disable the fast path")
	for range 2 {
		got, callErr := eng.Call(ctx, "pick", core.Int{V: 1}, core.Int{V: 2})
		require.NoError(t, callErr)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
	}
	assert.Equal(t, int64(5), eng.Stats().PluginCallCounts["pick"])
	mu.Lock()
	require.Len(t, events, 2)
	mu.Unlock()
	assert.Equal(t, "pick", events[0].Function)
	assert.Equal(t, "pick", events[1].Function)
	assert.GreaterOrEqual(t, events[0].Duration, time.Duration(0))

	// Phase 3: a ctx-borne meter routes the general path (the meter is
	// drawn) WITHOUT touching the engine flag.
	eng2, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng2.Close() })
	impl2 := eng2.(*engineImpl)
	_, err = eng2.Eval(ctx, "def-pick2", "(defun pick (a b) a)")
	require.NoError(t, err)
	require.True(t, impl2.fastPath.Load())

	meter := &recordingMeter{}
	got, callErr := eng2.Call(WithMeter(ctx, meter), "pick", core.Int{V: 7}, core.Int{V: 8})
	require.NoError(t, callErr)
	assert.True(t, core.Int{V: 7}.Equals(got), "got %v", got)
	assert.Positive(t, meter.snapshot().leaseCalls, "the metered call must take the general path and draw the meter")
	require.True(t, impl2.fastPath.Load(), "a ctx meter must not flip the engine flag")

	// Phase 4: a plain context resumes the fast path — the meter is untouched.
	leases := meter.snapshot().leaseCalls
	got, callErr = eng2.Call(ctx, "pick", core.Int{V: 7}, core.Int{V: 8})
	require.NoError(t, callErr)
	assert.True(t, core.Int{V: 7}.Equals(got), "got %v", got)
	assert.Equal(t, leases, meter.snapshot().leaseCalls, "the fast path must not touch the ctx meter")

	// Phase 5: a fresh engine starts in the fast condition.
	eng3, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng3.Close() })
	assert.True(t, eng3.(*engineImpl).fastPath.Load())

	// Phase 6: handles observe the same transition — a fast Fn.Call and
	// PinnedFn.Call before registration fire nothing; after registration the
	// very next handle call takes the general path and fires the callback.
	eng4, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng4.Close() })
	_, err = eng4.Eval(ctx, "def-pick4", "(defun pick (a b) a)")
	require.NoError(t, err)
	fn4, err := eng4.Func("pick")
	require.NoError(t, err)
	pinned4 := fn4.Pin()
	require.NotNil(t, pinned4)
	require.True(t, eng4.(*engineImpl).fastPath.Load())

	var mu4 sync.Mutex
	var events4 []PluginCallEvent

	for range 2 {
		got, callErr := fn4.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
		require.NoError(t, callErr)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
		got, callErr = pinned4.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
		require.NoError(t, callErr)
		assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
	}
	mu4.Lock()
	require.Empty(t, events4, "fast handle calls must not fire callbacks")
	mu4.Unlock()
	assert.Equal(t, int64(4), eng4.Stats().PluginCallCounts["pick"])

	eng4.OnPluginCall(func(ev PluginCallEvent) {
		mu4.Lock()
		events4 = append(events4, ev)
		mu4.Unlock()
	})
	require.False(t, eng4.(*engineImpl).fastPath.Load(), "callback registration must disable the fast path")

	got, callErr = fn4.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, callErr)
	assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
	got, callErr = pinned4.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
	require.NoError(t, callErr)
	assert.True(t, core.Int{V: 1}.Equals(got), "got %v", got)
	mu4.Lock()
	require.Len(t, events4, 2, "the next handle calls must fire the callback")
	mu4.Unlock()
	assert.Equal(t, "pick", events4[0].Function)
	assert.Equal(t, "pick", events4[1].Function)
	assert.Equal(t, int64(6), eng4.Stats().PluginCallCounts["pick"])
}
