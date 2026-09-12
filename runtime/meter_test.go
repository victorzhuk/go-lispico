package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

type recordingMeter struct {
	mu             sync.Mutex
	leaseCalls     int
	returnCalls    int
	chargeCalls    int
	releaseCalls   int
	returnedRed    int64
	returnedAlloc  int64
	chargedBytes   int64
	chargedSlots   int64
	releasedBytes  int64
	releasedSlots  int64
	maxOutstanding int
	inLease        bool
	denyAfter      int
	chargeErr      error
}

func (m *recordingMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.leaseCalls++
	if reductions > 1024 || allocBytes > 64<<10 {
		return 0, 0, errors.New("oversized lease request")
	}
	if m.denyAfter > 0 && m.leaseCalls > m.denyAfter {
		return 0, 0, errors.New("exhausted")
	}
	if !m.inLease {
		m.inLease = true
		m.maxOutstanding = max(m.maxOutstanding, 1)
	}
	return reductions, allocBytes, nil
}

func (m *recordingMeter) ReturnEval(reductions, allocBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.returnCalls++
	m.returnedRed += reductions
	m.returnedAlloc += allocBytes
	m.inLease = false
}

func (m *recordingMeter) ChargeRetained(bytes, slots int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chargeCalls++
	m.chargedBytes += bytes
	m.chargedSlots += slots
	return m.chargeErr
}

func (m *recordingMeter) ReleaseRetained(bytes, slots int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls++
	m.releasedBytes += bytes
	m.releasedSlots += slots
}

func (m *recordingMeter) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	denyAfter, chargeErr := m.denyAfter, m.chargeErr
	m.leaseCalls = 0
	m.returnCalls = 0
	m.chargeCalls = 0
	m.releaseCalls = 0
	m.returnedRed = 0
	m.returnedAlloc = 0
	m.chargedBytes = 0
	m.chargedSlots = 0
	m.releasedBytes = 0
	m.releasedSlots = 0
	m.maxOutstanding = 0
	m.inLease = false
	m.denyAfter = denyAfter
	m.chargeErr = chargeErr
}

func (m *recordingMeter) snapshot() recordingMeter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return recordingMeter{
		leaseCalls:     m.leaseCalls,
		returnCalls:    m.returnCalls,
		chargeCalls:    m.chargeCalls,
		releaseCalls:   m.releaseCalls,
		returnedRed:    m.returnedRed,
		returnedAlloc:  m.returnedAlloc,
		chargedBytes:   m.chargedBytes,
		chargedSlots:   m.chargedSlots,
		releasedBytes:  m.releasedBytes,
		releasedSlots:  m.releasedSlots,
		maxOutstanding: m.maxOutstanding,
	}
}

func TestMeter_NoopAndLimitMeter(t *testing.T) {
	var _ Meter = NoopMeter{}
	red, alloc, err := (NoopMeter{}).LeaseEval(1<<30, 1<<30)
	if err != nil || red != 1<<30 || alloc != 1<<30 {
		t.Fatalf("NoopMeter LeaseEval = (%d, %d, %v)", red, alloc, err)
	}
	if err := (NoopMeter{}).ChargeRetained(1, 1); err != nil {
		t.Fatalf("NoopMeter ChargeRetained: %v", err)
	}

	m := NewLimitMeter(2, 128, 10, 2)
	red, alloc, err = m.LeaseEval(1, 64)
	if err != nil || red != 1 || alloc != 64 {
		t.Fatalf("LimitMeter first lease = (%d, %d, %v)", red, alloc, err)
	}
	red, alloc, err = m.LeaseEval(2, 128)
	if err != nil || red != 1 || alloc != 64 {
		t.Fatalf("LimitMeter partial lease = (%d, %d, %v)", red, alloc, err)
	}
	_, _, err = m.LeaseEval(1, 1)
	if err == nil {
		t.Fatal("LimitMeter exhausted lease succeeded")
	}
	m.ReturnEval(1, 64)
	red, alloc, err = m.LeaseEval(1, 64)
	if err != nil || red != 1 || alloc != 64 {
		t.Fatalf("LimitMeter returned lease = (%d, %d, %v)", red, alloc, err)
	}
	if err := m.ChargeRetained(8, 1); err != nil {
		t.Fatalf("ChargeRetained: %v", err)
	}
	if err := m.ChargeRetained(8, 1); err == nil {
		t.Fatal("ChargeRetained over byte limit succeeded")
	}
}

func TestMeter_ContextOverridesEngineMeter(t *testing.T) {
	engineMeter := &recordingMeter{}
	ctxMeter := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(engineMeter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	engineMeter.reset()

	got, err := eng.Eval(WithMeter(t.Context(), ctxMeter), "ctx", "1")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if !got.Equals(core.Int{V: 1}) {
		t.Fatalf("Eval result = %v, want 1", got)
	}
	if ctxMeter.snapshot().leaseCalls == 0 {
		t.Fatal("ctx meter saw no lease")
	}
	if engineMeter.snapshot().leaseCalls != 0 {
		t.Fatalf("engine meter lease calls = %d, want 0", engineMeter.snapshot().leaseCalls)
	}
}

func TestMeter_WithMeterOverridesReusedEvalState(t *testing.T) {
	meterA := &recordingMeter{}
	meterB := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	ctx := core.EnsureEvalState(WithMeter(t.Context(), meterA))

	if _, err := eng.Eval(ctx, "meter-a", "(def from-a [1])"); err != nil {
		t.Fatalf("Eval meter A: %v", err)
	}
	snapA := meterA.snapshot()
	if snapA.leaseCalls == 0 || snapA.chargeCalls != 1 {
		t.Fatalf("meterA lease/charge calls = %d/%d, want lease > 0 and charge 1", snapA.leaseCalls, snapA.chargeCalls)
	}

	if _, err := eng.Eval(WithMeter(ctx, meterB), "meter-b", "(def from-b [2])"); err != nil {
		t.Fatalf("Eval meter B: %v", err)
	}
	snapB := meterB.snapshot()
	if snapB.leaseCalls == 0 || snapB.chargeCalls != 1 {
		t.Fatalf("meterB lease/charge calls = %d/%d, want lease > 0 and charge 1", snapB.leaseCalls, snapB.chargeCalls)
	}
	snapA = meterA.snapshot()
	if snapA.leaseCalls != 1 || snapA.chargeCalls != 1 {
		t.Fatalf("meterA calls after override = lease %d charge %d, want unchanged 1/1", snapA.leaseCalls, snapA.chargeCalls)
	}
}

func TestMeter_EngineMeterFallbackAndLeaseBounds(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.Use(stdlib.New()); err != nil {
		t.Fatalf("Use stdlib: %v", err)
	}
	m.reset()

	_, err = eng.Eval(t.Context(), "loop", "(loop [i 0] (if (= i 3000) i (recur (+ i 1))))")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	snap := m.snapshot()
	if snap.leaseCalls < 2 {
		t.Fatalf("lease calls = %d, want re-lease", snap.leaseCalls)
	}
	if snap.returnCalls != 1 {
		t.Fatalf("ReturnEval calls = %d, want exactly 1 on eval end", snap.returnCalls)
	}
	if snap.maxOutstanding != 1 {
		t.Fatalf("maxOutstanding = %d, want 1", snap.maxOutstanding)
	}
}

func TestMeter_LeaseExhaustionIsTerminalResourceLimit(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.Use(stdlib.New()); err != nil {
		t.Fatalf("Use stdlib: %v", err)
	}
	m.mu.Lock()
	m.denyAfter = 1
	m.mu.Unlock()
	m.reset()

	_, err = eng.Eval(t.Context(), "loop", "(loop [i 0] (if (= i 3000) i (recur (+ i 1))))")
	if err == nil {
		t.Fatal("Eval succeeded, want resource limit")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Eval error = %v, want %s", err, core.CodeResourceLimit)
	}
	if !core.IsTerminalEvalError(err) {
		t.Fatalf("Eval error is not terminal: %v", err)
	}
}

func TestMeter_RetainedSettlementAndRebuildRelease(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	_, scope, err := eng.LoadScope(t.Context(), "(def x [1 2 3])", nil)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	bytes, slots := scope.RetainedUsage()
	snap := m.snapshot()
	if snap.chargeCalls != 1 || snap.chargedBytes != bytes || snap.chargedSlots != slots {
		t.Fatalf("ChargeRetained = calls %d (%d,%d), want 1 (%d,%d)", snap.chargeCalls, snap.chargedBytes, snap.chargedSlots, bytes, slots)
	}
	scope.Delete("x")
	scope.Rebuild()
	snap = m.snapshot()
	if snap.releaseCalls != 1 || snap.releasedBytes != bytes || snap.releasedSlots != slots {
		t.Fatalf("ReleaseRetained = calls %d (%d,%d), want 1 (%d,%d)", snap.releaseCalls, snap.releasedBytes, snap.releasedSlots, bytes, slots)
	}
}

func TestMeter_ContextMeteredLoadScopeRebuildRelease(t *testing.T) {
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	ctxMeter := &recordingMeter{}
	_, scope, err := eng.LoadScope(WithMeter(t.Context(), ctxMeter), "(def y [4 5 6])", nil)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	bytes, slots := scope.RetainedUsage()
	snap := ctxMeter.snapshot()
	if snap.chargeCalls != 1 || snap.chargedBytes != bytes || snap.chargedSlots != slots {
		t.Fatalf("ctxMeter ChargeRetained = calls %d (%d,%d), want 1 (%d,%d)", snap.chargeCalls, snap.chargedBytes, snap.chargedSlots, bytes, slots)
	}
	scope.Delete("y")
	scope.Rebuild()
	snap = ctxMeter.snapshot()
	if snap.releaseCalls != 1 || snap.releasedBytes != bytes || snap.releasedSlots != slots {
		t.Fatalf("ctxMeter ReleaseRetained = calls %d (%d,%d), want 1 (%d,%d)", snap.releaseCalls, snap.releasedBytes, snap.releasedSlots, bytes, slots)
	}
}

func TestMeter_ContextRootDefsChargeDistinctMeters(t *testing.T) {
	engineMeter := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(engineMeter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	meterA := &recordingMeter{}
	meterB := &recordingMeter{}

	_, err = eng.Eval(WithMeter(t.Context(), meterA), "rootA", "(def x [1 2 3])")
	if err != nil {
		t.Fatalf("Eval A: %v", err)
	}
	snapA := meterA.snapshot()
	if snapA.chargeCalls != 1 || snapA.chargedBytes == 0 {
		t.Fatalf("meterA ChargeRetained = calls %d, bytes %d", snapA.chargeCalls, snapA.chargedBytes)
	}

	_, err = eng.Eval(WithMeter(t.Context(), meterB), "rootB", "(def y [4 5 6])")
	if err != nil {
		t.Fatalf("Eval B: %v", err)
	}
	snapB := meterB.snapshot()
	if snapB.chargeCalls != 1 || snapB.chargedBytes == 0 {
		t.Fatalf("meterB ChargeRetained = calls %d, bytes %d", snapB.chargeCalls, snapB.chargedBytes)
	}

	rootEnv := eng.RootEnv()
	rootEnv.Delete("x")
	freedBytes, freedSlots := rootEnv.Rebuild()
	if freedBytes != snapA.chargedBytes || freedSlots != snapA.chargedSlots {
		t.Fatalf("rootEnv.Rebuild x freed = (%d, %d), want meterA charge (%d, %d)", freedBytes, freedSlots, snapA.chargedBytes, snapA.chargedSlots)
	}
	if meterA.snapshot().releaseCalls != 1 {
		t.Fatalf("meterA release calls = %d, want 1", meterA.snapshot().releaseCalls)
	}
	if meterB.snapshot().releaseCalls != 0 {
		t.Fatalf("meterB release calls = %d, want 0 before y rebuild", meterB.snapshot().releaseCalls)
	}
	if engineMeter.snapshot().releaseCalls != 0 {
		t.Fatalf("engine meter release calls = %d, want 0", engineMeter.snapshot().releaseCalls)
	}

	rootEnv.Delete("y")
	freedBytes, freedSlots = rootEnv.Rebuild()
	if freedBytes != snapB.chargedBytes || freedSlots != snapB.chargedSlots {
		t.Fatalf("rootEnv.Rebuild y freed = (%d, %d), want meterB charge (%d, %d)", freedBytes, freedSlots, snapB.chargedBytes, snapB.chargedSlots)
	}
	if meterB.snapshot().releaseCalls != 1 {
		t.Fatalf("meterB release calls = %d, want 1", meterB.snapshot().releaseCalls)
	}
	if engineMeter.snapshot().releaseCalls != 0 {
		t.Fatalf("engine meter release calls = %d, want 0", engineMeter.snapshot().releaseCalls)
	}
}

func TestMeter_MergeIntoDistinctMetersRebuildRelease(t *testing.T) {
	engineMeter := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(engineMeter))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	meterA := &recordingMeter{}
	meterB := &recordingMeter{}
	meterC := &recordingMeter{}

	if _, err := eng.Eval(WithMeter(t.Context(), meterA), "rootA", "(def x [1 2 3])"); err != nil {
		t.Fatalf("Eval A: %v", err)
	}
	snapA := meterA.snapshot()
	if snapA.chargeCalls != 1 || snapA.chargedBytes == 0 || snapA.chargedSlots != 1 {
		t.Fatalf("meterA ChargeRetained = calls %d (%d,%d), want 1 positive/1", snapA.chargeCalls, snapA.chargedBytes, snapA.chargedSlots)
	}

	if _, err := eng.Eval(WithMeter(t.Context(), meterB), "rootB", "(def y [4 5 6])"); err != nil {
		t.Fatalf("Eval B: %v", err)
	}
	snapB := meterB.snapshot()
	if snapB.chargeCalls != 1 || snapB.chargedBytes == 0 || snapB.chargedSlots != 1 {
		t.Fatalf("meterB ChargeRetained = calls %d (%d,%d), want 1 positive/1", snapB.chargeCalls, snapB.chargedBytes, snapB.chargedSlots)
	}

	_, scope, err := eng.LoadScope(WithMeter(t.Context(), meterC), "(def z [7 8 9])", nil)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	snapC := meterC.snapshot()
	if snapC.chargeCalls != 1 || snapC.chargedBytes == 0 || snapC.chargedSlots != 1 {
		t.Fatalf("meterC ChargeRetained = calls %d (%d,%d), want 1 positive/1", snapC.chargeCalls, snapC.chargedBytes, snapC.chargedSlots)
	}

	if err := scope.MergeInto(eng.RootEnv()); err != nil {
		t.Fatalf("MergeInto: %v", err)
	}
	if engineMeter.snapshot().chargeCalls != 0 {
		t.Fatalf("engine meter charge calls = %d, want 0", engineMeter.snapshot().chargeCalls)
	}

	rootEnv := eng.RootEnv()
	rootEnv.Delete("x")
	freedBytes, freedSlots := rootEnv.Rebuild()
	if freedBytes != snapA.chargedBytes || freedSlots != snapA.chargedSlots {
		t.Fatalf("rootEnv.Rebuild x freed = (%d,%d), want meterA charge (%d,%d)", freedBytes, freedSlots, snapA.chargedBytes, snapA.chargedSlots)
	}
	if meterA.snapshot().releaseCalls != 1 || meterB.snapshot().releaseCalls != 0 || meterC.snapshot().releaseCalls != 0 {
		t.Fatalf("release after x = A:%d B:%d C:%d, want 1/0/0", meterA.snapshot().releaseCalls, meterB.snapshot().releaseCalls, meterC.snapshot().releaseCalls)
	}

	rootEnv.Delete("y")
	freedBytes, freedSlots = rootEnv.Rebuild()
	if freedBytes != snapB.chargedBytes || freedSlots != snapB.chargedSlots {
		t.Fatalf("rootEnv.Rebuild y freed = (%d,%d), want meterB charge (%d,%d)", freedBytes, freedSlots, snapB.chargedBytes, snapB.chargedSlots)
	}
	if meterA.snapshot().releaseCalls != 1 || meterB.snapshot().releaseCalls != 1 || meterC.snapshot().releaseCalls != 0 {
		t.Fatalf("release after y = A:%d B:%d C:%d, want 1/1/0", meterA.snapshot().releaseCalls, meterB.snapshot().releaseCalls, meterC.snapshot().releaseCalls)
	}

	rootEnv.Delete("z")
	freedBytes, freedSlots = rootEnv.Rebuild()
	if freedBytes != snapC.chargedBytes || freedSlots != snapC.chargedSlots {
		t.Fatalf("rootEnv.Rebuild z freed = (%d,%d), want meterC charge (%d,%d)", freedBytes, freedSlots, snapC.chargedBytes, snapC.chargedSlots)
	}
	if meterA.snapshot().releaseCalls != 1 || meterB.snapshot().releaseCalls != 1 || meterC.snapshot().releaseCalls != 1 {
		t.Fatalf("release after z = A:%d B:%d C:%d, want 1/1/1", meterA.snapshot().releaseCalls, meterB.snapshot().releaseCalls, meterC.snapshot().releaseCalls)
	}
	if engineMeter.snapshot().releaseCalls != 0 {
		t.Fatalf("engine meter release calls = %d, want 0", engineMeter.snapshot().releaseCalls)
	}
}

func TestMeter_MergeIntoUpdatesTargetRetainedUsage(t *testing.T) {
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	_, scope, err := eng.LoadScope(t.Context(), "(def z [7 8 9])", nil)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	scopeBytes, scopeSlots := scope.RetainedUsage()
	rootBytes, rootSlots := eng.RootEnv().RetainedUsage()

	if err := scope.MergeInto(eng.RootEnv()); err != nil {
		t.Fatalf("MergeInto: %v", err)
	}

	gotBytes, gotSlots := eng.RootEnv().RetainedUsage()
	if gotBytes != rootBytes+scopeBytes || gotSlots != rootSlots+scopeSlots {
		t.Fatalf("RootEnv RetainedUsage = (%d,%d), want (%d,%d)", gotBytes, gotSlots, rootBytes+scopeBytes, rootSlots+scopeSlots)
	}
}

func TestMeter_MergeIntoOverwriteReleasesPreviousOwner(t *testing.T) {
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	meterA := &recordingMeter{}
	meterB := &recordingMeter{}

	if _, err := eng.Eval(WithMeter(t.Context(), meterA), "rootA", "(def x [1 2 3])"); err != nil {
		t.Fatalf("Eval A: %v", err)
	}
	snapA := meterA.snapshot()

	_, scope, err := eng.LoadScope(WithMeter(t.Context(), meterB), "(def x [4 5 6])", nil)
	if err != nil {
		t.Fatalf("LoadScope: %v", err)
	}
	snapB := meterB.snapshot()

	rootBytes, rootSlots := eng.RootEnv().RetainedUsage()
	if err := scope.MergeInto(eng.RootEnv()); err != nil {
		t.Fatalf("MergeInto: %v", err)
	}

	if snap := meterA.snapshot(); snap.releaseCalls != 1 || snap.releasedBytes != snapA.chargedBytes || snap.releasedSlots != snapA.chargedSlots {
		t.Fatalf("meterA ReleaseRetained = calls %d (%d,%d), want 1 (%d,%d)", snap.releaseCalls, snap.releasedBytes, snap.releasedSlots, snapA.chargedBytes, snapA.chargedSlots)
	}
	if snap := meterB.snapshot(); snap.releaseCalls != 0 {
		t.Fatalf("meterB release calls = %d, want 0", snap.releaseCalls)
	}
	gotBytes, gotSlots := eng.RootEnv().RetainedUsage()
	if gotBytes != rootBytes || gotSlots != rootSlots {
		t.Fatalf("RootEnv RetainedUsage = (%d,%d), want unchanged (%d,%d)", gotBytes, gotSlots, rootBytes, rootSlots)
	}

	eng.RootEnv().Delete("x")
	freedBytes, freedSlots := eng.RootEnv().Rebuild()
	if freedBytes != snapB.chargedBytes || freedSlots != snapB.chargedSlots {
		t.Fatalf("RootEnv.Rebuild x freed = (%d,%d), want meterB charge (%d,%d)", freedBytes, freedSlots, snapB.chargedBytes, snapB.chargedSlots)
	}
	if snap := meterB.snapshot(); snap.releaseCalls != 1 || snap.releasedBytes != snapB.chargedBytes || snap.releasedSlots != snapB.chargedSlots {
		t.Fatalf("meterB ReleaseRetained = calls %d (%d,%d), want 1 (%d,%d)", snap.releaseCalls, snap.releasedBytes, snap.releasedSlots, snapB.chargedBytes, snapB.chargedSlots)
	}
}

func TestMeter_ConcurrentEvaluations(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.Use(stdlib.New()); err != nil {
		t.Fatalf("Use stdlib: %v", err)
	}
	var wg sync.WaitGroup
	const numGoroutines = 10
	errCh := make(chan error, numGoroutines)
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := eng.Eval(t.Context(), "concurrent", "(+ 1 2 3)")
			if err != nil {
				errCh <- err
				return
			}
			if !res.Equals(core.Int{V: 6}) {
				errCh <- fmt.Errorf("result = %v, want 6", res)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent eval error: %v", err)
	}

	snap := m.snapshot()
	if snap.leaseCalls < numGoroutines {
		t.Fatalf("lease calls = %d, want at least %d", snap.leaseCalls, numGoroutines)
	}
	if snap.returnCalls != snap.leaseCalls {
		t.Fatalf("return calls = %d, want %d", snap.returnCalls, snap.leaseCalls)
	}
}

func TestMeter_ConcurrentRootDefsWithDistinctMeters(t *testing.T) {
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	const n = 16
	meters := make([]*recordingMeter, n)
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		meters[i] = &recordingMeter{}
		name := fmt.Sprintf("root%d", i)
		source := fmt.Sprintf("(def %s [%d])", name, i)
		meter := meters[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := eng.Eval(WithMeter(t.Context(), meter), name, source); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent root def: %v", err)
	}

	var chargedSlots int64
	for i, meter := range meters {
		name := fmt.Sprintf("root%d", i)
		snap := meter.snapshot()
		wantBytes := core.RetainedBindingBytes(name, core.NewVector([]core.Value{core.Int{V: int64(i)}}))
		if snap.chargeCalls != 1 || snap.chargedBytes != wantBytes || snap.chargedSlots != 1 {
			t.Fatalf("meter %d ChargeRetained = calls %d (%d,%d), want 1 (%d,1)", i, snap.chargeCalls, snap.chargedBytes, snap.chargedSlots, wantBytes)
		}
		chargedSlots += snap.chargedSlots
	}
	if chargedSlots != n {
		t.Fatalf("charged slots = %d, want %d", chargedSlots, n)
	}
}

type setupPlugin struct{}

func (setupPlugin) Name() string              { return "setup" }
func (setupPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: "test"} }
func (setupPlugin) Init(env *core.Env) error  { return env.Set("setup/value", core.Int{V: 1}) }

func TestMeter_EngineSetupUseIsMetered(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	if err := eng.Use(setupPlugin{}); err != nil {
		t.Fatalf("Use: %v", err)
	}
	snap := m.snapshot()
	if snap.leaseCalls == 0 {
		t.Fatal("Use saw no setup lease")
	}
	if snap.chargeCalls != 1 || snap.chargedSlots == 0 || snap.chargedBytes == 0 {
		t.Fatalf("Use retained charge = calls %d (%d,%d), want positive", snap.chargeCalls, snap.chargedBytes, snap.chargedSlots)
	}
}

func TestMeter_EngineMeterPreservesNativeEvaluator(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(
		nil,
		WithDialect(clojure.Dialect()),
		WithResourceLimits(ResourceLimits{MaxReaderDepth: 200, MaxCollectionLen: 5}),
		WithEngineMeter(m),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	if _, ok := eng.RootEnv().Evaluator().(*bytecodeEvaluator); !ok {
		t.Fatalf("RootEnv evaluator = %T, want *bytecodeEvaluator", eng.RootEnv().Evaluator())
	}
	limiter, ok := eng.RootEnv().Evaluator().(core.CollectionLimiter)
	if !ok {
		t.Fatalf("RootEnv evaluator = %T, want CollectionLimiter", eng.RootEnv().Evaluator())
	}
	if got := limiter.CollectionLimit(); got != 5 {
		t.Fatalf("CollectionLimit = %d, want 5", got)
	}
	if err := eng.Use(stdlib.New()); err != nil {
		t.Fatalf("Use stdlib: %v", err)
	}
	_, err = eng.Eval(t.Context(), "range-limit", "(range 0 6)")
	if err == nil {
		t.Fatal("Eval range succeeded, want collection limit")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Eval range error = %v, want %s", err, core.CodeResourceLimit)
	}
}

func TestMeter_EvalRetainedChargeErrorIsTerminal(t *testing.T) {
	m := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	_, err = eng.Eval(t.Context(), "retained-denied", "(def denied [1 2 3])")
	if err == nil {
		t.Fatal("Eval succeeded, want retained charge error")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Eval error = %v, want %s", err, core.CodeResourceLimit)
	}
	if !core.IsTerminalEvalError(err) {
		t.Fatalf("Eval error is not terminal: %v", err)
	}
	if _, ok := eng.RootEnv().Get("denied"); !ok {
		t.Fatal("denied binding missing; Eval retained charge is charge-after-write")
	}
}

func TestMeter_UseRollsBackPluginOnRetainedChargeError(t *testing.T) {
	m := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	err = eng.Use(setupPlugin{})
	if err == nil {
		t.Fatal("Use succeeded, want retained charge error")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Use error = %v, want %s", err, core.CodeResourceLimit)
	}
	if _, ok := eng.Registry().Get("setup"); ok {
		t.Fatal("plugin remained registered after retained charge failure")
	}
	if _, ok := eng.RootEnv().Get("setup/value"); ok {
		t.Fatal("plugin binding remained after retained charge failure")
	}

	m.chargeErr = nil
	m.reset()
	if err := eng.Use(setupPlugin{}); err != nil {
		t.Fatalf("Use after rollback: %v", err)
	}
}

// ---- Plugin retained-settlement rollback (failed Use / ReloadPlugin) ----
//
// A failed plugin operation must never leave a meter charge behind for a cell
// the journal abort removed, must keep charges for cells that survive it
// (host-created or adopted), must release settled op-owned charges exactly
// once when the operation fails after settlement, and must return its unused
// compute lease exactly once with the operation's own error taking
// precedence over any settlement error.

func TestUseFailedInitLeavesNoRetainedCharge(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	initErr := errors.New("fli: deliberate init failure")
	err = eng.Use(&rpPlugin{name: "fli", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("fli/state", core.Int{V: 1}); err != nil {
			return err
		}
		return initErr
	}})
	if !errors.Is(err, initErr) {
		t.Fatalf("Use error = %v, want init failure", err)
	}
	if _, ok := eng.RootEnv().Get("fli/state"); ok {
		t.Fatal("fli/state remained after failed Use")
	}
	if _, ok := eng.Registry().Get("fli"); ok {
		t.Fatal("plugin remained registered after failed Use")
	}
	snap := m.snapshot()
	if snap.chargeCalls != 0 || snap.releaseCalls != 0 {
		t.Fatalf("charge/release calls = %d/%d, want 0/0: settlement must not charge a cell the abort's removal already decided",
			snap.chargeCalls, snap.releaseCalls)
	}
	if netBytes, netSlots := snap.chargedBytes-snap.releasedBytes, snap.chargedSlots-snap.releasedSlots; netBytes != 0 || netSlots != 0 {
		t.Fatalf("net retained charge after failed Use = (%d, %d), want (0, 0)", netBytes, netSlots)
	}
}

func TestUseFailedInitNetRetainedUsageEqualsPreOp(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	if _, err := eng.Eval(t.Context(), "seed", "(def keep/seed [1 2 3])"); err != nil {
		t.Fatalf("Eval seed: %v", err)
	}
	pre := m.snapshot()

	initErr := errors.New("nru: deliberate init failure")
	err = eng.Use(&rpPlugin{name: "nru", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("nru/state", core.Int{V: 1}); err != nil {
			return err
		}
		return initErr
	}})
	if !errors.Is(err, initErr) {
		t.Fatalf("Use error = %v, want init failure", err)
	}
	if _, ok := eng.RootEnv().Get("keep/seed"); !ok {
		t.Fatal("pre-op seed binding lost by failed Use")
	}
	post := m.snapshot()
	if got, want := post.chargedBytes-post.releasedBytes, pre.chargedBytes-pre.releasedBytes; got != want {
		t.Fatalf("net retained bytes after failed Use = %d, want pre-op %d", got, want)
	}
	if got, want := post.chargedSlots-post.releasedSlots, pre.chargedSlots-pre.releasedSlots; got != want {
		t.Fatalf("net retained slots after failed Use = %d, want pre-op %d", got, want)
	}
}

func TestUseCapacityRejectionMidInitLeavesNoCharge(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m),
		WithResourceLimits(ResourceLimits{MaxRetainedSlotsPerEnv: 1}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	err = eng.Use(&rpPlugin{name: "cap", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("cap/one", core.Int{V: 1}); err != nil {
			return err
		}
		return env.Set("cap/two", core.Int{V: 2})
	}})
	if err == nil {
		t.Fatal("Use succeeded, want per-env capacity rejection")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Use error = %v, want %s", err, core.CodeResourceLimit)
	}
	if _, ok := eng.RootEnv().Get("cap/one"); ok {
		t.Fatal("cap/one remained after capacity-rejected Use")
	}
	if _, ok := eng.Registry().Get("cap"); ok {
		t.Fatal("plugin remained registered after capacity-rejected Use")
	}
	snap := m.snapshot()
	if netBytes, netSlots := snap.chargedBytes-snap.releasedBytes, snap.chargedSlots-snap.releasedSlots; netBytes != 0 || netSlots != 0 {
		t.Fatalf("net retained charge after mid-init capacity rejection = (%d, %d), want (0, 0)", netBytes, netSlots)
	}
}

func TestUseConcurrentHostBindingKeepsCharge(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	root := eng.RootEnv()
	m.reset()

	b := newRPBarrier()
	hostErr := make(chan error, 1)
	initErr := errors.New("hgt: deliberate init failure")
	p := &rpPlugin{name: "hgt", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("hgt/op-one", core.Int{V: 1}); err != nil {
			return err
		}
		if err := b.pause(); err != nil {
			return err
		}
		if err := env.Set("hgt/op-two", core.Int{V: 2}); err != nil {
			return err
		}
		return initErr
	}}
	done := rpGo(func() error { return eng.Use(p) })

	rpWait(t, b.entered, "Init entry")
	hostErr <- root.Set("host/keep", core.Int{V: 7})
	if err := <-hostErr; err != nil {
		t.Fatalf("host Set: %v", err)
	}
	close(b.release)

	if err := rpResult(t, done, "Use"); !errors.Is(err, initErr) {
		t.Fatalf("Use error = %v, want init failure", err)
	}
	rpWantInt(t, root, "host/keep", 7)
	rpWantAbsent(t, root, "hgt/op-one")
	rpWantAbsent(t, root, "hgt/op-two")
	snap := m.snapshot()
	wantBytes := core.RetainedBindingBytes("host/keep", core.Int{V: 7})
	if netBytes, netSlots := snap.chargedBytes-snap.releasedBytes, snap.chargedSlots-snap.releasedSlots; netBytes != wantBytes || netSlots != 1 {
		t.Fatalf("net retained charge after failed Use = (%d, %d), want surviving host binding only (%d, 1)", netBytes, netSlots, wantBytes)
	}
	if snap.releaseCalls != 0 {
		t.Fatalf("ReleaseRetained calls = %d, want 0: the surviving host binding keeps its charge", snap.releaseCalls)
	}
}

func TestUsePublishConflictReleasesSettledCharges(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	root := eng.RootEnv()
	m.reset()

	b := newRPBarrier()
	p := &rpPlugin{name: "pc", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("pc/state", core.Int{V: 1}); err != nil {
			return err
		}
		return b.pause()
	}}
	done := rpGo(func() error { return eng.Use(p) })

	rpWait(t, b.entered, "Init entry")
	eng.Registry().RegisterNoCheck(&rpHost{name: "pc"})
	close(b.release)

	rpWantCode(t, rpResult(t, done, "Use"), core.CodeRegistryConflict)
	if _, ok := eng.Registry().Get("pc"); !ok {
		t.Fatal("host registry entry lost after publish conflict")
	}
	rpWantAbsent(t, root, "pc/state")
	snap := m.snapshot()
	if snap.chargeCalls != 1 {
		t.Fatalf("ChargeRetained calls = %d, want 1 settled charge", snap.chargeCalls)
	}
	if snap.releasedBytes != snap.chargedBytes || snap.releasedSlots != snap.chargedSlots {
		t.Fatalf("ReleaseRetained after publish conflict = (%d, %d), want settled charges (%d, %d) released exactly once",
			snap.releasedBytes, snap.releasedSlots, snap.chargedBytes, snap.chargedSlots)
	}
}

func TestReloadPluginFailedInitSettlesRetainedOnce(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	root := eng.RootEnv()
	m.reset()

	if err := eng.Use(&rpPlugin{name: "rl", version: "1.0.0", init: func(env *core.Env) error {
		return env.Set("rl/old", core.Int{V: 1})
	}}); err != nil {
		t.Fatalf("Use v1: %v", err)
	}
	m.reset()

	reloadErr := errors.New("rl: deliberate reload failure")
	err = eng.ReloadPlugin(&rpPlugin{name: "rl", version: "2.0.0", init: func(env *core.Env) error {
		if err := env.Set("rl/new", core.Int{V: 2}); err != nil {
			return err
		}
		return reloadErr
	}})
	if !errors.Is(err, reloadErr) {
		t.Fatalf("ReloadPlugin error = %v, want init failure", err)
	}
	rpWantInt(t, root, "rl/old", 1)
	rpWantAbsent(t, root, "rl/new")
	snap := m.snapshot()
	if netBytes, netSlots := snap.chargedBytes-snap.releasedBytes, snap.chargedSlots-snap.releasedSlots; netBytes != 0 || netSlots != 0 {
		t.Fatalf("net retained charge after failed reload = (%d, %d), want (0, 0)", netBytes, netSlots)
	}
	if snap.releaseCalls != 0 {
		t.Fatalf("ReleaseRetained calls = %d, want 0: the restored old binding keeps its charge", snap.releaseCalls)
	}
}

// ---- Keep-green pins: behavior that already holds and must stay ----

func TestUseSettlementDenialReleasesExactlyOnce(t *testing.T) {
	meterB := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	err = eng.Use(evaluatorSetupPlugin{ctx: WithMeter(t.Context(), meterB)})
	if err == nil {
		t.Fatal("Use succeeded, want retained charge denial")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Use error = %v, want %s", err, core.CodeResourceLimit)
	}
	if _, ok := eng.RootEnv().Get("setup/evaluator-value"); ok {
		t.Fatal("denied binding remained after failed Use")
	}
	if _, ok := eng.Registry().Get("setup-evaluator"); ok {
		t.Fatal("plugin remained registered after settlement denial")
	}
	snap := meterB.snapshot()
	if snap.chargeCalls != 1 {
		t.Fatalf("ChargeRetained calls = %d, want 1", snap.chargeCalls)
	}
	if snap.releaseCalls != 0 {
		t.Fatalf("ReleaseRetained calls = %d, want 0: denial with no earlier successful charge releases nothing", snap.releaseCalls)
	}

	meterB.chargeErr = nil
	meterB.reset()
	if err := eng.Use(setupPlugin{}); err != nil {
		t.Fatalf("Use after denial cleared: %v", err)
	}
}

func TestUseHostMeterDenialReleasesExactlyOnce(t *testing.T) {
	m := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	err = eng.Use(evaluatorSetupPlugin{ctx: t.Context()})
	if err == nil {
		t.Fatal("Use succeeded, want retained charge denial")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Use error = %v, want %s", err, core.CodeResourceLimit)
	}
	if _, ok := eng.RootEnv().Get("setup/evaluator-value"); ok {
		t.Fatal("denied binding remained after failed Use")
	}
	if _, ok := eng.Registry().Get("setup-evaluator"); ok {
		t.Fatal("plugin remained registered after settlement denial")
	}
	snap := m.snapshot()
	if snap.chargeCalls != 1 {
		t.Fatalf("ChargeRetained calls = %d, want 1", snap.chargeCalls)
	}
	if snap.releaseCalls != 0 {
		t.Fatalf("ReleaseRetained calls = %d, want 0: denial with no earlier successful charge releases nothing", snap.releaseCalls)
	}

	m.chargeErr = nil
	m.reset()
	if err := eng.Use(setupPlugin{}); err != nil {
		t.Fatalf("Use after denial cleared: %v", err)
	}
}

func TestUseAdoptedPendingCellSettlesNormally(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	root := eng.RootEnv()
	m.reset()

	b := newRPBarrier()
	p := &rpPlugin{name: "adopt", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("adopt/cell", core.Int{V: 1}); err != nil {
			return err
		}
		return b.pause()
	}}
	done := rpGo(func() error { return eng.Use(p) })

	rpWait(t, b.entered, "Init entry")
	if err := root.Set("adopt/cell", core.Int{V: 2}); err != nil {
		t.Fatalf("host rebind: %v", err)
	}
	close(b.release)

	if err := rpResult(t, done, "Use"); err != nil {
		t.Fatalf("Use: %v", err)
	}
	rpWantInt(t, root, "adopt/cell", 2)
	snap := m.snapshot()
	wantBytes := core.RetainedBindingBytes("adopt/cell", core.Int{V: 1})
	if snap.chargedBytes != wantBytes || snap.chargedSlots != 1 {
		t.Fatalf("ChargeRetained = (%d, %d), want adopted cell creation charge (%d, 1)",
			snap.chargedBytes, snap.chargedSlots, wantBytes)
	}
	if snap.releaseCalls != 0 {
		t.Fatalf("ReleaseRetained calls = %d, want 0: an adopted cell settles normally and keeps its charge", snap.releaseCalls)
	}
}

func TestUseFailedOpReturnsLeaseExactlyOnce(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	initErr := errors.New("lease-once: deliberate init failure")
	err = eng.Use(&rpPlugin{name: "lease-once", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("lease-once/state", core.Int{V: 1}); err != nil {
			return err
		}
		return initErr
	}})
	if !errors.Is(err, initErr) {
		t.Fatalf("Use error = %v, want init failure", err)
	}
	snap := m.snapshot()
	if snap.leaseCalls != 1 || snap.returnCalls != 1 {
		t.Fatalf("lease calls/returns = %d/%d, want 1/1: the unused compute lease is returned exactly once", snap.leaseCalls, snap.returnCalls)
	}
	if snap.returnedRed != 1024 || snap.returnedAlloc != 64<<10 {
		t.Fatalf("returned lease = (%d, %d), want granted (1024, %d)", snap.returnedRed, snap.returnedAlloc, 64<<10)
	}
}

func TestUseFailedOpKeepsErrorPrecedence(t *testing.T) {
	m := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	root := eng.RootEnv()
	m.reset()

	initErr := errors.New("prec: deliberate init failure")
	b := newRPBarrier()
	p := &rpPlugin{name: "prec", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("prec/state", core.Int{V: 1}); err != nil {
			return err
		}
		if err := b.pause(); err != nil {
			return err
		}
		return initErr
	}}
	done := rpGo(func() error { return eng.Use(p) })

	rpWait(t, b.entered, "Init entry")
	if err := root.Set("prec/state", core.Int{V: 2}); err != nil {
		t.Fatalf("host adopt rebind: %v", err)
	}
	close(b.release)

	err = rpResult(t, done, "Use")
	if !errors.Is(err, initErr) {
		t.Fatalf("Use error = %v, want the init error to take precedence over the settlement denial", err)
	}
	var lerr *core.LispicoError
	if errors.As(err, &lerr) {
		t.Fatalf("Use error = %v, want no *core.LispicoError: the settlement denial must be swallowed while the operation error stands", err)
	}
	snap := m.snapshot()
	if snap.chargeCalls != 1 {
		t.Fatalf("ChargeRetained calls = %d, want 1: the adopted cell must reach settlement and be denied", snap.chargeCalls)
	}
	if snap.releaseCalls != 0 {
		t.Fatalf("ReleaseRetained calls = %d, want 0: a denial with no earlier successful charge releases nothing", snap.releaseCalls)
	}
	rpWantInt(t, root, "prec/state", 2)
	if _, ok := eng.Registry().Get("prec"); ok {
		t.Fatal("plugin remained registered after failed Use")
	}
}

type evaluatorSetupPlugin struct {
	ctx context.Context
}

func (evaluatorSetupPlugin) Name() string { return "setup-evaluator" }

func (evaluatorSetupPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: "test"} }

func (p evaluatorSetupPlugin) Init(env *core.Env) error {
	_, err := env.Evaluator().Eval(p.ctx, core.NewList([]core.Value{
		core.Symbol{V: "def"},
		core.Symbol{V: "setup/evaluator-value"},
		core.Int{V: 42},
	}), env)
	return err
}

func TestMeter_UseRollsBackNestedEvaluatorRetainedChargeError(t *testing.T) {
	m := &recordingMeter{chargeErr: errors.New("retained denied")}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.reset()

	err = eng.Use(evaluatorSetupPlugin{ctx: t.Context()})
	if err == nil {
		t.Fatal("Use succeeded, want retained charge error")
	}
	var lerr *core.LispicoError
	if !errors.As(err, &lerr) || lerr.Code != core.CodeResourceLimit {
		t.Fatalf("Use error = %v, want %s", err, core.CodeResourceLimit)
	}
	if _, ok := eng.Registry().Get("setup-evaluator"); ok {
		t.Fatal("plugin remained registered after nested retained charge failure")
	}
	if _, ok := eng.RootEnv().Get("setup/evaluator-value"); ok {
		t.Fatal("nested evaluator binding remained after retained charge failure")
	}
}

type rebuildDuringChargeMeter struct {
	recordingMeter
	env *core.Env
}

func (m *rebuildDuringChargeMeter) ChargeRetained(bytes, slots int64) error {
	if err := m.recordingMeter.ChargeRetained(bytes, slots); err != nil {
		return err
	}
	if m.env != nil {
		m.env.Delete("x")
		m.env.Rebuild()
	}
	return nil
}

func TestMeter_RebuildRacesPendingAllocation(t *testing.T) {
	m := &rebuildDuringChargeMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.env = eng.RootEnv()
	m.reset()

	_, err = eng.Eval(t.Context(), "pending-rebuild", "(def x [1 2 3])")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	snap := m.snapshot()
	if snap.chargeCalls != 1 || snap.releaseCalls != 1 {
		t.Fatalf("Charge/Release calls = %d/%d, want 1/1", snap.chargeCalls, snap.releaseCalls)
	}
	if snap.releasedBytes != snap.chargedBytes || snap.releasedSlots != snap.chargedSlots {
		t.Fatalf("ReleaseRetained = (%d,%d), want charged (%d,%d)", snap.releasedBytes, snap.releasedSlots, snap.chargedBytes, snap.chargedSlots)
	}
}

type reentrantReleaseMeter struct {
	recordingMeter
	env *core.Env
}

func (m *reentrantReleaseMeter) ReleaseRetained(bytes, slots int64) {
	m.recordingMeter.ReleaseRetained(bytes, slots)
	_, _ = m.env.Get("x")
	_ = m.env.Set("after-release", core.Int{V: 1})
}

func TestMeter_ReentrantMeterDuringRebuild(t *testing.T) {
	m := &reentrantReleaseMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	m.env = eng.RootEnv()
	m.reset()

	if _, err := eng.Eval(t.Context(), "bind", "(def x [1])"); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	eng.RootEnv().Delete("x")
	eng.RootEnv().Rebuild()
	if _, ok := eng.RootEnv().Get("after-release"); !ok {
		t.Fatal("after-release was not rebound by reentrant release")
	}
}

func TestMeter_InvisibleToLispBindings(t *testing.T) {
	plain, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker())
	if err != nil {
		t.Fatalf("New plain: %v", err)
	}
	t.Cleanup(func() { _ = plain.Close() })
	metered, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(&recordingMeter{}))
	if err != nil {
		t.Fatalf("New metered: %v", err)
	}
	t.Cleanup(func() { _ = metered.Close() })

	plainNames := append(plain.RootEnv().LocalNames(), plain.RootEnv().LocalFuncNames()...)
	meteredNames := append(metered.RootEnv().LocalNames(), metered.RootEnv().LocalFuncNames()...)
	if len(plainNames) != len(meteredNames) {
		t.Fatalf("meter changed binding count: plain %d metered %d", len(plainNames), len(meteredNames))
	}
	for _, name := range meteredNames {
		switch name {
		case "meter", "meter?", "current-meter", "grant", "lease", "budget", "meter/lease", "runtime/meter", "*meter*":
			t.Fatalf("meter binding leaked: %s", name)
		}
	}
}
