package core

import (
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
