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
		wantBytes := core.RetainedBindingBytes(name, core.Vector{Items: []core.Value{core.Int{V: int64(i)}}})
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

type evaluatorSetupPlugin struct {
	ctx context.Context
}

func (evaluatorSetupPlugin) Name() string { return "setup-evaluator" }

func (evaluatorSetupPlugin) Metadata() core.PluginMeta { return core.PluginMeta{Version: "test"} }

func (p evaluatorSetupPlugin) Init(env *core.Env) error {
	_, err := env.Evaluator().Eval(p.ctx, core.List{Items: []core.Value{
		core.Symbol{V: "def"},
		core.Symbol{V: "setup/evaluator-value"},
		core.Int{V: 42},
	}}, env)
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
