package runtime

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// lifecycleMeter is a test meter with panic hooks and a release reentry hook.
// The reentry hook runs after releaseCalls is incremented so a reentrant
// cache probe observes a consistent count.
// chargeBarrierN, when positive, blocks each ChargeRetained caller until n
// callers have arrived — forcing true duplicate-candidate concurrency.
type lifecycleMeter struct {
	mu             sync.Mutex
	chargeCalls    int
	releaseCalls   int
	panicCharge    bool
	panicRelease   bool
	onRelease      func()
	onCharge       func()
	chargeBarrierN int
	chargeArrived  chan struct{}
	chargeGate     chan struct{}
}

func (m *lifecycleMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m *lifecycleMeter) ReturnEval(_, _ int64) {}

func (m *lifecycleMeter) ChargeRetained(_, _ int64) error {
	m.mu.Lock()
	m.chargeCalls++
	barrier := m.chargeBarrierN
	m.mu.Unlock()
	if barrier > 0 {
		m.mu.Lock()
		m.chargeArrived <- struct{}{}
		if len(m.chargeArrived) == barrier && m.chargeGate != nil {
			close(m.chargeGate)
		}
		gate := m.chargeGate
		m.mu.Unlock()
		if gate != nil {
			<-gate
		}
	}
	m.mu.Lock()
	hook := m.onCharge
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.panicCharge {
		panic("charge retained boom")
	}
	return nil
}

func (m *lifecycleMeter) ReleaseRetained(_, _ int64) {
	m.mu.Lock()
	hook := m.onRelease
	m.releaseCalls++
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.panicRelease {
		panic("release retained boom")
	}
}

func (m *lifecycleMeter) snapshot() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chargeCalls, m.releaseCalls
}

func newMeteredCacheEngine(t *testing.T, m Meter, limits ResourceLimits) Engine {
	t.Helper()
	e, err := New(nil, WithBytecode(), WithDialect(clojure.Dialect()), WithEngineMeter(m), WithResourceLimits(limits))
	require.NoError(t, err)
	return e
}

func lifecycleLimits(entries int) ResourceLimits {
	return ResourceLimits{MaxCacheEntries: entries, MaxCacheBytes: 1 << 30, MaxCacheNodes: 1 << 30, MaxCollectionLen: 1 << 30}
}

func totalCacheMapEntries(be *bytecodeEvaluator) int {
	total := 0
	for i := range be.stripes {
		s := &be.stripes[i]
		s.mu.Lock()
		total += len(s.cache)
		s.mu.Unlock()
	}
	return total
}

// TestCacheLifecycle_AdmissionPanicThenEvalAndClose proves a meter panic on
// ChargeRetained rolls the unpublished reservation back, leaves the cache
// consistent, and the same stripe still admits and closes cleanly.
func TestCacheLifecycle_AdmissionPanicThenEvalAndClose(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(10))
	defer func() { _ = e.Close() }()

	m.mu.Lock()
	m.panicCharge = true
	m.mu.Unlock()

	var evalErr error
	func() {
		_, evalErr = e.Eval(context.Background(), "boom", "1")
	}()
	// engine.Eval converts admission panics to errors via its recover guard.
	require.ErrorContains(t, evalErr, "charge retained boom", "charge panic must surface from Eval")

	be := e.(*engineImpl).bytecodeEvaluator
	assert.Equal(t, int64(0), be.cacheEntries.Load(), "panicked admission must roll back its entries charge")
	assert.Equal(t, int64(0), be.cacheBytes.Load(), "panicked admission must roll back its bytes charge")
	assert.Equal(t, int64(0), be.cacheNodes.Load(), "panicked admission must roll back its nodes charge")
	assert.Equal(t, 0, totalCacheMapEntries(be))
	chargeCalls, releaseCalls := m.snapshot()
	assert.Equal(t, 1, chargeCalls)
	assert.Equal(t, 0, releaseCalls)

	m.mu.Lock()
	m.panicCharge = false
	m.mu.Unlock()

	v, err := e.Eval(context.Background(), "after", "2")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 2}.Equals(v))
	assert.Equal(t, 1, cacheStats(t, e).Entries)
	assert.Equal(t, 1, totalCacheMapEntries(be))
	require.NoError(t, e.Close())
	assert.Equal(t, int64(0), be.cacheEntries.Load())
}

// TestCacheLifecycle_ReservationCountersMatchEntries drives a working set
// through a tight entry ceiling so eviction fires throughout, then pins the
// invariant the stripe counters and the meter balance must both settle on:
// exactly one retained slot and one live map entry per cached chunk.
func TestCacheLifecycle_ReservationCountersMatchEntries(t *testing.T) {
	m := &recordingMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(3))
	defer func() { _ = e.Close() }()

	for i := range 10 {
		evalCacheSource(t, e, strconv.Itoa(i))
	}

	be := e.(*engineImpl).bytecodeEvaluator
	live, liveBytes, liveNodes := 0, int64(0), int64(0)
	for i := range be.stripes {
		s := &be.stripes[i]
		s.mu.Lock()
		for _, entry := range s.cache {
			live++
			liveBytes += entry.chunk.DeepBytes
			liveNodes += int64(entry.chunk.NodeCount)
		}
		s.mu.Unlock()
	}
	require.Equal(t, 3, cacheStats(t, e).Entries)
	require.Equal(t, 3, live, "budget counter must match live stripe entries")
	require.Equal(t, int64(3), be.cacheEntries.Load())
	require.Equal(t, liveBytes, be.cacheBytes.Load(), "bytes counter must match live chunk bytes")
	require.Equal(t, liveNodes, be.cacheNodes.Load(), "nodes counter must match live chunk nodes")

	snap := m.snapshot()
	require.Equal(t, snap.chargedSlots-snap.releasedSlots, int64(live),
		"meter retained balance must equal live entries after eviction")
}

// TestCacheLifecycle_ReleaseReentryDoesNotDeadlock proves a meter that
// reenters the engine from ReleaseRetained cannot deadlock: releases run
// outside every cache lock, so the reentrant Eval can take the stripe mutex.
func TestCacheLifecycle_ReleaseReentryDoesNotDeadlock(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(1))

	var reentrant atomic.Int64
	m.mu.Lock()
	m.onRelease = func() {
		if reentrant.Add(1) > 3 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = e.Eval(ctx, "reentry", "78")
	}
	m.mu.Unlock()

	type outcome struct {
		v1, v2 core.Value
		err1   error
	}
	done := make(chan outcome, 1)
	go func() {
		out := outcome{}
		out.v1, out.err1 = e.Eval(context.Background(), "r1", "1")
		if out.err1 == nil {
			out.v2, out.err1 = e.Eval(context.Background(), "r2", "2")
		}
		closeErr := e.Close()
		if out.err1 == nil && closeErr != nil {
			out.err1 = closeErr
		}
		done <- out
	}()

	select {
	case out := <-done:
		require.NoError(t, out.err1)
		assert.True(t, core.Int{V: 1}.Equals(out.v1))
		assert.True(t, core.Int{V: 2}.Equals(out.v2))
	case <-time.After(15 * time.Second):
		t.Fatal("release reentry deadlocked against the cache locks")
	}
	require.Positive(t, reentrant.Load(), "reentry hook must have run")
}

// TestCacheLifecycle_ConcurrentDuplicateAdmitsSingleEntry fires many
// concurrent candidates for one key: exactly one publishes, the losers roll
// back their own reservations, and the meter balance and stripe map settle
// on a single entry.
func TestCacheLifecycle_ConcurrentDuplicateAdmitsSingleEntry(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(100))
	defer func() { _ = e.Close() }()

	src := "42"
	const n = 16
	m.mu.Lock()
	m.chargeBarrierN = n
	m.chargeArrived = make(chan struct{}, n)
	m.chargeGate = make(chan struct{})
	m.mu.Unlock()

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = e.Eval(context.Background(), "dup", src)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "candidate %d", i)
	}

	be := e.(*engineImpl).bytecodeEvaluator
	assert.Equal(t, 1, totalCacheMapEntries(be), "exactly one entry may survive")
	assert.Equal(t, 1, cacheStats(t, e).Entries)
	chargeCalls, releaseCalls := m.snapshot()
	assert.Equal(t, n, chargeCalls, "every candidate reserves before publication")
	assert.Equal(t, n-1, releaseCalls, "every losing candidate releases only its own charge")
	assert.Equal(t, int64(1), be.cacheEntries.Load())
}

// TestCacheLifecycle_CloseAttemptsEveryReleaseAfterPanic proves Close with a
// panicking release attempts the retained release of every live entry before
// surfacing the first panic as Close's error.
func TestCacheLifecycle_CloseAttemptsEveryReleaseAfterPanic(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(10))

	for _, src := range []string{"11", "22", "33"} {
		evalCacheSource(t, e, src)
	}
	require.Equal(t, 3, cacheStats(t, e).Entries)

	m.mu.Lock()
	m.panicRelease = true
	m.mu.Unlock()

	err := e.Close()
	require.Error(t, err, "the first release panic must propagate as Close's error")
	assert.Contains(t, err.Error(), "release retained boom",
		"Close must surface the release panic, got: %v", err)
	_, releaseCalls := m.snapshot()
	assert.Equal(t, 3, releaseCalls, "every live entry's release must be attempted despite the first panic")

	be := e.(*engineImpl).bytecodeEvaluator
	assert.Equal(t, int64(0), be.cacheEntries.Load(), "all entries must be detached regardless of the panic")
}

// TestCacheLifecycle_DuplicateLoserPanickingReleaseReleasesOnce proves a
// duplicate-publication loser whose ReleaseRetained panics releases its
// reservation exactly once: the panic unwinds into detachPublish's recovery,
// which must not run a second cleanup, so the winner's counters and the
// meter balance stay intact.
func TestCacheLifecycle_DuplicateLoserPanickingReleaseReleasesOnce(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(100))
	defer func() { _ = e.Close() }()

	evalCacheSource(t, e, "11")
	be := e.(*engineImpl).bytecodeEvaluator
	require.Equal(t, 1, cacheStats(t, e).Entries)
	wantEntries := be.cacheEntries.Load()
	wantBytes := be.cacheBytes.Load()
	wantNodes := be.cacheNodes.Load()

	const n = 4
	m.mu.Lock()
	m.panicRelease = true
	m.chargeBarrierN = n
	m.chargeArrived = make(chan struct{}, n)
	m.chargeGate = make(chan struct{})
	m.mu.Unlock()

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	failed := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			defer func() {
				if r := recover(); r != nil {
					failed[i] = fmt.Sprint(r)
				}
			}()
			_, errs[i] = e.Eval(context.Background(), "loser", "42")
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for i := range n {
		switch {
		case errs[i] == nil && failed[i] == "":
			succeeded++
		case errs[i] != nil:
			assert.Contains(t, errs[i].Error(), "release retained boom", "candidate %d", i)
		default:
			assert.Contains(t, failed[i], "release retained boom",
				"candidate %d must surface the release panic, not vanish silently", i)
		}
	}
	assert.Equal(t, 1, succeeded, "exactly one candidate wins publication")
	chargeCalls, releaseCalls := m.snapshot()
	assert.Equal(t, 1+n, chargeCalls, "the seeded entry plus every candidate reserves")
	assert.Equal(t, n-1, releaseCalls,
		"each loser must attempt exactly one release despite the panic")
	assert.Equal(t, wantEntries+1, be.cacheEntries.Load(), "only the winner's charge may join the seeded entry")
	assert.Greater(t, be.cacheBytes.Load(), wantBytes, "winner's bytes counter must be untouched")
	assert.Greater(t, be.cacheNodes.Load(), wantNodes, "winner's nodes counter must be untouched")
	assert.Equal(t, 2, totalCacheMapEntries(be), "seeded entry plus exactly one winner may survive")
}

// epochBumpExpander delegates to the real expander, then bumps the macro
// epoch exactly once — the deterministic window between EvalCached's key
// capture and planCacheAdmission.
type epochBumpExpander struct {
	inner   macroExpander
	globals *core.Env
	bumped  *atomic.Bool
}

func (x *epochBumpExpander) MacroExpandDeep(ctx context.Context, form core.Value, env *core.Env) (core.Value, error) {
	v, err := x.inner.MacroExpandDeep(ctx, form, env)
	if err == nil && x.bumped.CompareAndSwap(false, true) {
		x.globals.BumpMacroEpoch()
	}
	return v, err
}

// TestCacheLifecycle_StaleEpochPlanningSkipsReservation proves a stale macro
// epoch at planning refuses admission outright: no reservation, no victim
// detachment, nothing published, and the borrowed candidate still evaluates.
func TestCacheLifecycle_StaleEpochPlanningSkipsReservation(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(100))
	defer func() { _ = e.Close() }()

	be := e.(*engineImpl).bytecodeEvaluator
	bumped := &atomic.Bool{}
	be.macro = &epochBumpExpander{inner: be.macro, globals: be.globals, bumped: bumped}

	// The stub bumps the epoch during expansion, so by the time
	// planCacheAdmission runs the key's epoch is already stale.
	v := evalCacheSource(t, e, "7")
	require.True(t, bumped.Load(), "the expander stub must have bumped the epoch")
	assert.True(t, core.Int{V: 7}.Equals(v), "the borrowed candidate must still evaluate")

	chargeCalls, releaseCalls := m.snapshot()
	assert.Equal(t, 0, chargeCalls, "a planning refusal must not reserve")
	assert.Equal(t, 0, releaseCalls, "a planning refusal must not detach victims")
	assert.Equal(t, 0, totalCacheMapEntries(be), "a refused candidate must publish nothing")
	assert.Equal(t, int64(0), be.cacheEntries.Load())

	// A fresh-epoch source still admits normally.
	v = evalCacheSource(t, e, "5")
	assert.True(t, core.Int{V: 5}.Equals(v))
	chargeCalls, _ = m.snapshot()
	assert.Equal(t, 1, chargeCalls)
	assert.Equal(t, 1, cacheStats(t, e).Entries)
}

// TestCacheLifecycle_EpochBumpAfterPlanningRejectsPublication proves the
// publication-time epoch recheck still holds when the epoch is bumped between
// planning and publishing: the candidate loses cleanly, releases its own
// reservation once, and the borrowed chunk still evaluates.
func TestCacheLifecycle_EpochBumpAfterPlanningRejectsPublication(t *testing.T) {
	m := &lifecycleMeter{}
	e := newMeteredCacheEngine(t, m, lifecycleLimits(100))
	defer func() { _ = e.Close() }()

	be := e.(*engineImpl).bytecodeEvaluator
	var bumped atomic.Bool
	m.mu.Lock()
	m.onCharge = func() {
		if bumped.CompareAndSwap(false, true) {
			be.globals.BumpMacroEpoch()
		}
	}
	m.mu.Unlock()

	v := evalCacheSource(t, e, "9")
	assert.True(t, core.Int{V: 9}.Equals(v), "the borrowed candidate must still evaluate")
	chargeCalls, releaseCalls := m.snapshot()
	assert.Equal(t, 1, chargeCalls, "the candidate reserves before the raced epoch bump")
	assert.Equal(t, 1, releaseCalls, "the losing candidate releases exactly once")
	assert.Equal(t, 0, totalCacheMapEntries(be), "the stale candidate must publish nothing")
	assert.Equal(t, int64(0), be.cacheEntries.Load())

	// The hook fired on the first admission; a later fresh-epoch source
	// still publishes normally.
	v = evalCacheSource(t, e, "3")
	assert.True(t, core.Int{V: 3}.Equals(v))
	assert.Equal(t, 1, cacheStats(t, e).Entries)
}
