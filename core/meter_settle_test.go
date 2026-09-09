package core

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type chargeFailingMeter struct {
	mu       sync.Mutex
	failOn   int
	charges  int
	releases int

	chargedBytes  int64
	chargedSlots  int64
	releasedBytes int64
	releasedSlots int64
}

func (m *chargeFailingMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m *chargeFailingMeter) ReturnEval(reductions, allocBytes int64) {}

func (m *chargeFailingMeter) ChargeRetained(bytes, slots int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.charges++
	m.chargedBytes += bytes
	m.chargedSlots += slots
	if m.failOn > 0 && m.charges >= m.failOn {
		return errors.New("retained denied")
	}
	return nil
}

func (m *chargeFailingMeter) ReleaseRetained(bytes, slots int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.releases++
	m.releasedBytes += bytes
	m.releasedSlots += slots
}

func (m *chargeFailingMeter) snapshot() chargeFailingMeter {
	m.mu.Lock()
	defer m.mu.Unlock()

	return chargeFailingMeter{
		failOn:        m.failOn,
		charges:       m.charges,
		releases:      m.releases,
		chargedBytes:  m.chargedBytes,
		chargedSlots:  m.chargedSlots,
		releasedBytes: m.releasedBytes,
		releasedSlots: m.releasedSlots,
	}
}

func TestSettleRetained_PartialFailureRollsBackCharges(t *testing.T) {
	st := newEvalState()
	meterA := &chargeFailingMeter{}
	meterB := &chargeFailingMeter{failOn: 1}
	envA := NewEnvWithRetainedLimits(nil, 0, 0)
	envB := NewEnvWithRetainedLimits(nil, 0, 0)

	if err := envA.Set("a", Int{V: 1}); err != nil {
		t.Fatalf("set envA a: %v", err)
	}
	if err := envB.Set("b", Int{V: 2}); err != nil {
		t.Fatalf("set envB b: %v", err)
	}
	cellA, ok := envA.CellLocal("a")
	if !ok {
		t.Fatal("missing envA cell a")
	}
	cellB, ok := envB.CellLocal("b")
	if !ok {
		t.Fatal("missing envB cell b")
	}

	bytesA := int64(11)
	bytesB := int64(17)
	st.pendingCellAllocs = []pendingCellAlloc{
		{env: envA, cell: cellA, meter: meterA, bytes: bytesA, slots: 1},
		{env: envB, cell: cellB, meter: meterB, bytes: bytesB, slots: 1},
	}
	st.retainedBytes = bytesA + bytesB
	st.retainedSlots = 2

	err := st.settleRetained()
	if err == nil {
		t.Fatal("settleRetained succeeded, want retained charge error")
	}
	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodeResourceLimit {
		t.Fatalf("settleRetained error = %v, want %s", err, CodeResourceLimit)
	}

	snapA := meterA.snapshot()
	if snapA.charges != 1 || snapA.releases != 1 {
		t.Fatalf("meterA charges/releases = %d/%d, want 1/1", snapA.charges, snapA.releases)
	}
	if snapA.releasedBytes != bytesA || snapA.releasedSlots != 1 {
		t.Fatalf("meterA ReleaseRetained = (%d,%d), want (%d,1)", snapA.releasedBytes, snapA.releasedSlots, bytesA)
	}
	snapB := meterB.snapshot()
	if snapB.charges != 1 || snapB.releases != 0 {
		t.Fatalf("meterB charges/releases = %d/%d, want 1/0", snapB.charges, snapB.releases)
	}

	if cellA.retainedMeter != nil || cellB.retainedMeter != nil {
		t.Fatalf("settleRetained finalized cells after failure: A=%v B=%v", cellA.retainedMeter, cellB.retainedMeter)
	}
	if gotBytes, gotSlots := envA.RetainedUsage(); gotBytes != retainedBindingBytes("a", Int{V: 1}) || gotSlots != 1 {
		t.Fatalf("envA RetainedUsage = (%d,%d), want unchanged", gotBytes, gotSlots)
	}
	if gotBytes, gotSlots := envB.RetainedUsage(); gotBytes != retainedBindingBytes("b", Int{V: 2}) || gotSlots != 1 {
		t.Fatalf("envB RetainedUsage = (%d,%d), want unchanged", gotBytes, gotSlots)
	}
	if len(st.pendingCellAllocs) != 0 || st.retainedBytes != 0 || st.retainedSlots != 0 {
		t.Fatalf("settleRetained state not reset: pending=%d bytes=%d slots=%d", len(st.pendingCellAllocs), st.retainedBytes, st.retainedSlots)
	}
}

// panicChargeMeter is a host meter that panics from ChargeRetained instead of
// denying the charge, the way a buggy embedder implementation would.
type panicChargeMeter struct {
	chargeFailingMeter
}

func (m *panicChargeMeter) ChargeRetained(bytes, slots int64) error {
	_ = m.chargeFailingMeter.ChargeRetained(bytes, slots)
	panic("retained charge failure")
}

// panicLeaseMeter grants one reduction per lease so every reduction charge
// reaches it, and panics once the initial lease has been drawn.
type panicLeaseMeter struct {
	chargeFailingMeter
	leases     int
	panicAfter int
}

func (m *panicLeaseMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	m.mu.Lock()
	m.leases++
	drawn := m.leases
	m.mu.Unlock()
	if drawn > m.panicAfter {
		panic("reduction charge failure")
	}
	return min(reductions, 1), allocBytes, nil
}

func catchSettlementPanic(fn func()) (escaped any) {
	defer func() { escaped = recover() }()
	fn()
	return nil
}

// TestSettleRetained_PanicMidChargeReleasesEarlierCharges pins the compensation
// a panicking meter must not skip: a later meter that panics instead of denying
// the charge leaves the earlier meters charged, so the release the denial path
// performs has to happen here too, with the exact charged amounts.
func TestSettleRetained_PanicMidChargeReleasesEarlierCharges(t *testing.T) {
	st := newEvalState()
	meterA := &chargeFailingMeter{}
	meterB := &panicChargeMeter{}
	envA := NewEnvWithRetainedLimits(nil, 0, 0)
	envB := NewEnvWithRetainedLimits(nil, 0, 0)

	if err := envA.Set("a", Int{V: 1}); err != nil {
		t.Fatalf("set envA a: %v", err)
	}
	if err := envB.Set("b", Int{V: 2}); err != nil {
		t.Fatalf("set envB b: %v", err)
	}
	cellA, ok := envA.CellLocal("a")
	if !ok {
		t.Fatal("missing envA cell a")
	}
	cellB, ok := envB.CellLocal("b")
	if !ok {
		t.Fatal("missing envB cell b")
	}

	bytesA := int64(11)
	bytesB := int64(17)
	st.pendingCellAllocs = []pendingCellAlloc{
		{env: envA, cell: cellA, meter: meterA, bytes: bytesA, slots: 1},
		{env: envB, cell: cellB, meter: meterB, bytes: bytesB, slots: 1},
	}
	st.retainedBytes = bytesA + bytesB
	st.retainedSlots = 2

	_ = catchSettlementPanic(func() { _ = st.settleRetained() })

	snapA := meterA.snapshot()
	if snapA.charges != 1 || snapA.releases != 1 {
		t.Fatalf("meterA charges/releases = %d/%d, want 1/1; a meter that panics mid-charge must still release the meters already charged",
			snapA.charges, snapA.releases)
	}
	if snapA.releasedBytes != bytesA || snapA.releasedSlots != 1 {
		t.Fatalf("meterA ReleaseRetained = (%d,%d), want (%d,1); the compensating release must use the exact charged amounts",
			snapA.releasedBytes, snapA.releasedSlots, bytesA)
	}
	if cellA.retainedMeter != nil || cellB.retainedMeter != nil {
		t.Fatalf("settleRetained finalized cells after a charge panic: A=%v B=%v", cellA.retainedMeter, cellB.retainedMeter)
	}
	if len(st.pendingCellAllocs) != 0 || st.retainedBytes != 0 || st.retainedSlots != 0 {
		t.Fatalf("settleRetained state not reset after a charge panic: pending=%d bytes=%d slots=%d",
			len(st.pendingCellAllocs), st.retainedBytes, st.retainedSlots)
	}
}

// TestFinishEval_FlushPanicDoesNotLeakRetainedIntoNextEval pins what a panic in
// the reduction flush must not cost the next evaluation. The eval state lives on
// the caller's context and is reused, so pending retained charges the panicking
// evaluation accrued must be settled or dropped there and never reach the meter
// of the evaluation that follows.
func TestFinishEval_FlushPanicDoesNotLeakRetainedIntoNextEval(t *testing.T) {
	st := newEvalState()
	base := context.WithValue(context.Background(), evalStateKey{}, st)
	first := &panicLeaseMeter{panicAfter: 1}
	second := &chargeFailingMeter{}

	ctx := WithEvalMeter(base, first)
	top, err := StartEval(ctx)
	if err != nil || !top {
		t.Fatalf("StartEval = (%v, %v), want (true, nil)", top, err)
	}
	env := NewEnvWithRetainedLimits(nil, 0, 0)
	if err := env.SetWithContext(ctx, "kept", Int{V: 1}); err != nil {
		t.Fatalf("set kept: %v", err)
	}
	if len(st.pendingCellAllocs) != 1 {
		t.Fatalf("pending retained allocs = %d, want 1; the case must leave settlement a charge to lose", len(st.pendingCellAllocs))
	}
	st.budget.Store(1)

	err = FinishEval(ctx, top)
	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodePanic {
		t.Fatalf("FinishEval error = %v, want %s cause", err, CodePanic)
	}
	if len(st.pendingCellAllocs) != 0 || st.retainedBytes != 0 || st.retainedSlots != 0 {
		t.Fatalf("flush panic left pending retained state on the eval state: pending=%d bytes=%d slots=%d; the next evaluation reuses it",
			len(st.pendingCellAllocs), st.retainedBytes, st.retainedSlots)
	}

	nextCtx := WithEvalMeter(base, second)
	nextTop, err := StartEval(nextCtx)
	if err != nil || !nextTop {
		t.Fatalf("second StartEval = (%v, %v), want (true, nil)", nextTop, err)
	}
	nextEnv := NewEnvWithRetainedLimits(nil, 0, 0)
	if err := nextEnv.SetWithContext(nextCtx, "fresh", Int{V: 2}); err != nil {
		t.Fatalf("set fresh: %v", err)
	}
	if err := FinishEval(nextCtx, nextTop); err != nil {
		t.Fatalf("second FinishEval: %v", err)
	}

	if snap := first.snapshot(); snap.charges != 0 {
		t.Fatalf("the panicking evaluation's meter took %d retained charges, want 0; a flush panic must not charge it from a later evaluation",
			snap.charges)
	}
	wantBytes := retainedBindingBytes("fresh", Int{V: 2})
	snap := second.snapshot()
	if snap.charges != 1 || snap.chargedBytes != wantBytes || snap.chargedSlots != 1 {
		t.Fatalf("second evaluation ChargeRetained = %d calls (%d,%d), want 1 call (%d,1); it must pay only its own retained state",
			snap.charges, snap.chargedBytes, snap.chargedSlots, wantBytes)
	}
}
