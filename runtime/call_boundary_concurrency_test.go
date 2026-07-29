package runtime

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func newStormEngine(t *testing.T) Engine {
	t.Helper()
	eng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))
	_, err = eng.Eval(t.Context(), "def-add", "(defun add (a b) (+ a b))")
	require.NoError(t, err)
	return eng
}

// TestCallBoundary_ConcurrentStorm_NeverDoubleLeasesVM hammers Engine.Call
// from many goroutines while the lease probe widens each claim window, so
// contention is guaranteed to occur: every claim attempt is observed, the
// peak of concurrent slot holders must never exceed one, and contended
// callers must take the pool fallback rather than sharing the slot VM.
func TestCallBoundary_ConcurrentStorm_NeverDoubleLeasesVM(t *testing.T) {
	eng := newStormEngine(t)
	ctx := t.Context()

	var active, peak, claims, fallbacks atomic.Int64
	restore := vmSlotLeaseProbe
	vmSlotLeaseProbe = func(claimed bool) {
		if claimed {
			claims.Add(1)
			n := active.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(50 * time.Microsecond)
			active.Add(-1)
		} else {
			fallbacks.Add(1)
		}
	}
	t.Cleanup(func() { vmSlotLeaseProbe = restore })

	const goroutines, calls = 8, 100
	var wg sync.WaitGroup
	var bad atomic.Int64
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range calls {
				got, err := eng.Call(ctx, "add", core.Int{V: 1}, core.Int{V: 2})
				if err != nil || !(core.Int{V: 3}).Equals(got) {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	assert.Zero(t, bad.Load(), "every concurrent call must succeed with the right value")
	assert.LessOrEqual(t, peak.Load(), int64(1), "the VM slot was double-leased")
	assert.Equal(t, int64(goroutines*calls), claims.Load()+fallbacks.Load(),
		"every lean call must report exactly one lease attempt")
	assert.Positive(t, claims.Load())
	assert.Positive(t, fallbacks.Load(), "contended callers must fall back to the pool")
}

// TestCallBoundary_ConcurrentStorm_NoRace runs the same storm without the
// probe and pins stats attribution under concurrency; it is the -race
// witness for the lean path's lock-free reads and CAS slot. A shared Fn
// handle joins the storm on the same engine: its lean calls race the same
// slot, so losers must take the pool fallback without error.
func TestCallBoundary_ConcurrentStorm_NoRace(t *testing.T) {
	eng := newStormEngine(t)
	ctx := t.Context()

	fn, err := eng.Func("add")
	require.NoError(t, err)

	const goroutines, calls = 8, 250
	var wg sync.WaitGroup
	var bad atomic.Int64
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range calls {
				got, err := eng.Call(ctx, "add", core.Int{V: 1}, core.Int{V: 2})
				if err != nil || !(core.Int{V: 3}).Equals(got) {
					bad.Add(1)
				}
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range calls {
				got, err := fn.Call(ctx, core.Int{V: 1}, core.Int{V: 2})
				if err != nil || !(core.Int{V: 3}).Equals(got) {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	assert.Zero(t, bad.Load())
	assert.Equal(t, int64(2*goroutines*calls), eng.Stats().PluginCallCounts["add"])
}
