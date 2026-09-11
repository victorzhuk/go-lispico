package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type chargeFailingMeter struct {
	mu       sync.Mutex
	failOn   int
	charges  int
	releases int
	returns  int

	chargedBytes       int64
	chargedSlots       int64
	releasedBytes      int64
	releasedSlots      int64
	returnedReductions int64
	returnedAllocBytes int64
}

func (m *chargeFailingMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m *chargeFailingMeter) ReturnEval(reductions, allocBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.returns++
	m.returnedReductions += reductions
	m.returnedAllocBytes += allocBytes
}

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
		failOn:             m.failOn,
		charges:            m.charges,
		releases:           m.releases,
		returns:            m.returns,
		chargedBytes:       m.chargedBytes,
		chargedSlots:       m.chargedSlots,
		releasedBytes:      m.releasedBytes,
		releasedSlots:      m.releasedSlots,
		returnedReductions: m.returnedReductions,
		returnedAllocBytes: m.returnedAllocBytes,
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

// panicReleaseMeter is a host meter that panics from ReleaseRetained, the way a
// buggy embedder implementation would. It records the release before panicking,
// so a test can tell a release that was never attempted from one that was.
type panicReleaseMeter struct {
	chargeFailingMeter
}

func (m *panicReleaseMeter) ReleaseRetained(bytes, slots int64) {
	m.chargeFailingMeter.ReleaseRetained(bytes, slots)
	panic("retained release failure")
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

const (
	settleLeaseReductions int64 = 5
	settleLeaseAllocBytes int64 = 7
)

// settleRetainedCell binds one retained cell in its own scope and returns the
// env and cell a pendingCellAlloc entry needs.
func settleRetainedCell(t *testing.T, name string, val Value) (*Env, *Cell) {
	t.Helper()

	env := NewEnvWithRetainedLimits(nil, 0, 0)
	if err := env.Set(name, val); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
	cell, ok := env.CellLocal(name)
	if !ok {
		t.Fatalf("missing cell %s", name)
	}
	return env, cell
}

// settleRetainedRebuiltCell is settleRetainedCell for a cell the scope has since
// compacted away, which is what sends the cell's pending charge down
// settleRetained's release path instead of finalizing it onto the cell.
func settleRetainedRebuiltCell(t *testing.T, name string, val Value) (*Env, *Cell) {
	t.Helper()

	env, cell := settleRetainedCell(t, name, val)
	env.Delete(name)
	env.Rebuild()
	if !cell.rebuilt {
		t.Fatalf("cell %s survived Rebuild unmarked; the case needs settleRetained's release path", name)
	}
	return env, cell
}

// assertSettlementClosedOut checks the guarantees a release panic must leave
// intact: the failure is reported as a panic cause, the pending retained ledger
// is clean, and the evaluation lease went back exactly once.
func assertSettlementClosedOut(t *testing.T, st *evalState, lease *chargeFailingMeter, err error) {
	t.Helper()

	var lerr *LispicoError
	if !errors.As(err, &lerr) || lerr.Code != CodePanic {
		t.Fatalf("finishEval error = %v, want a %s cause; a panicking release must be reported, not swallowed", err, CodePanic)
	}
	if len(st.pendingCellAllocs) != 0 || st.retainedBytes != 0 || st.retainedSlots != 0 {
		t.Fatalf("pending retained ledger left after a release panic: pending=%d bytes=%d slots=%d",
			len(st.pendingCellAllocs), st.retainedBytes, st.retainedSlots)
	}
	snap := lease.snapshot()
	if snap.returns != 1 || snap.returnedReductions != settleLeaseReductions || snap.returnedAllocBytes != settleLeaseAllocBytes {
		t.Fatalf("ReturnEval = %d calls (%d,%d), want 1 call (%d,%d); the evaluation lease goes back exactly once",
			snap.returns, snap.returnedReductions, snap.returnedAllocBytes, settleLeaseReductions, settleLeaseAllocBytes)
	}
}

// TestSettleRetained_PanicMidCompensationReleasesLaterCharges pins that the
// compensating release a denied charge triggers is not cut short by one broken
// meter: a meter panicking from ReleaseRetained must not leave the meters behind
// it in that loop charged for retained state the settlement gave up on.
func TestSettleRetained_PanicMidCompensationReleasesLaterCharges(t *testing.T) {
	st := newEvalState()
	first := &chargeFailingMeter{}
	panicking := &panicReleaseMeter{}
	last := &chargeFailingMeter{}
	denying := &chargeFailingMeter{failOn: 1}

	envFirst, cellFirst := settleRetainedCell(t, "first", Int{V: 1})
	envPanic, cellPanic := settleRetainedCell(t, "panicking", Int{V: 2})
	envLast, cellLast := settleRetainedCell(t, "last", Int{V: 3})
	envDeny, cellDeny := settleRetainedCell(t, "denying", Int{V: 4})

	bytesFirst, bytesPanic, bytesLast, bytesDeny := int64(11), int64(17), int64(23), int64(29)
	st.pendingCellAllocs = []pendingCellAlloc{
		{env: envFirst, cell: cellFirst, meter: first, bytes: bytesFirst, slots: 1},
		{env: envPanic, cell: cellPanic, meter: panicking, bytes: bytesPanic, slots: 1},
		{env: envLast, cell: cellLast, meter: last, bytes: bytesLast, slots: 1},
		{env: envDeny, cell: cellDeny, meter: denying, bytes: bytesDeny, slots: 1},
	}
	st.retainedBytes = bytesFirst + bytesPanic + bytesLast + bytesDeny
	st.retainedSlots = 4
	st.evalDepth.Store(1)
	st.setMeter(first)
	st.leasedReductions, st.leasedAllocBytes = settleLeaseReductions, settleLeaseAllocBytes

	err := st.finishEval()
	assertSettlementClosedOut(t, st, first, err)

	if snap := first.snapshot(); snap.charges != 1 || snap.releases != 1 || snap.releasedBytes != bytesFirst || snap.releasedSlots != 1 {
		t.Fatalf("meter before the panicking one: %d charges, %d releases (%d,%d), want 1 charge and 1 release (%d,1)",
			snap.charges, snap.releases, snap.releasedBytes, snap.releasedSlots, bytesFirst)
	}
	if snap := last.snapshot(); snap.charges != 1 || snap.releases != 1 || snap.releasedBytes != bytesLast || snap.releasedSlots != 1 {
		t.Fatalf("meter after the panicking one: %d charges, %d releases (%d,%d), want 1 charge and 1 release (%d,1); a meter panicking from ReleaseRetained must not cost a later meter its compensating release",
			snap.charges, snap.releases, snap.releasedBytes, snap.releasedSlots, bytesLast)
	}
	if snap := denying.snapshot(); snap.charges != 1 || snap.releases != 0 {
		t.Fatalf("denying meter charges/releases = %d/%d, want 1/0", snap.charges, snap.releases)
	}
	if cellFirst.retainedMeter != nil || cellPanic.retainedMeter != nil || cellLast.retainedMeter != nil || cellDeny.retainedMeter != nil {
		t.Fatal("settleRetained finalized cells after a denied charge")
	}
}

// TestSettleRetained_PanicMidRebuiltReleaseReleasesLaterCells pins the same
// guarantee for the releases a settled charge owes cells the scope compacted
// away while the evaluation ran. The pending ledger is dropped immediately after
// that loop, so a release it skips is charged for good, with nothing to retry from.
func TestSettleRetained_PanicMidRebuiltReleaseReleasesLaterCells(t *testing.T) {
	st := newEvalState()
	first := &chargeFailingMeter{}
	panicking := &panicReleaseMeter{}
	last := &chargeFailingMeter{}

	envFirst, cellFirst := settleRetainedRebuiltCell(t, "first", Int{V: 1})
	envPanic, cellPanic := settleRetainedRebuiltCell(t, "panicking", Int{V: 2})
	envLast, cellLast := settleRetainedRebuiltCell(t, "last", Int{V: 3})

	bytesFirst, bytesPanic, bytesLast := int64(11), int64(17), int64(23)
	st.pendingCellAllocs = []pendingCellAlloc{
		{env: envFirst, cell: cellFirst, meter: first, bytes: bytesFirst, slots: 1},
		{env: envPanic, cell: cellPanic, meter: panicking, bytes: bytesPanic, slots: 1},
		{env: envLast, cell: cellLast, meter: last, bytes: bytesLast, slots: 1},
	}
	st.retainedBytes = bytesFirst + bytesPanic + bytesLast
	st.retainedSlots = 3
	st.evalDepth.Store(1)
	st.setMeter(first)
	st.leasedReductions, st.leasedAllocBytes = settleLeaseReductions, settleLeaseAllocBytes

	err := st.finishEval()
	assertSettlementClosedOut(t, st, first, err)

	if snap := first.snapshot(); snap.charges != 1 || snap.releases != 1 || snap.releasedBytes != bytesFirst || snap.releasedSlots != 1 {
		t.Fatalf("meter before the panicking one: %d charges, %d releases (%d,%d), want 1 charge and 1 release (%d,1)",
			snap.charges, snap.releases, snap.releasedBytes, snap.releasedSlots, bytesFirst)
	}
	if snap := last.snapshot(); snap.charges != 1 || snap.releases != 1 || snap.releasedBytes != bytesLast || snap.releasedSlots != 1 {
		t.Fatalf("meter after the panicking one: %d charges, %d releases (%d,%d), want 1 charge and 1 release (%d,1); a rebuilt cell's release must survive a sibling meter panicking",
			snap.charges, snap.releases, snap.releasedBytes, snap.releasedSlots, bytesLast)
	}
	if cellFirst.retainedMeter != nil || cellPanic.retainedMeter != nil || cellLast.retainedMeter != nil {
		t.Fatal("settleRetained finalized rebuilt cells; their charge is owed back, not recorded on the cell")
	}
}

// TestAbortRefundsOwnedRetainedCapacity pins the counter side of an aborted
// registration: every op-owned entry Abort removes refunds the bytes and slot
// its binding reserved, entry by entry, so capacity the operation does not
// own, such as host bindings made before or during the op, keeps its
// reservation.
func TestAbortRefundsOwnedRetainedCapacity(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	root.SetRetainedMeter(&chargeFailingMeter{})
	hostVal := Int{V: 7}
	if err := root.Set("host", hostVal); err != nil {
		t.Fatalf("set host: %v", err)
	}

	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	opX, opY := Int{V: 1}, Int{V: 2}
	if err := reg.Env().Set("x", opX); err != nil {
		t.Fatalf("set x: %v", err)
	}
	if err := reg.Env().Set("y", opY); err != nil {
		t.Fatalf("set y: %v", err)
	}
	// A host binding made while the operation runs is not the operation's;
	// its reservation must survive the rollback untouched, which is what
	// separates a per-entry refund from restoring saved totals.
	hostLate := Int{V: 9}
	if err := root.Set("late", hostLate); err != nil {
		t.Fatalf("set late: %v", err)
	}

	wantBytes := retainedBindingBytes("host", hostVal) + retainedBindingBytes("late", hostLate)
	wantOpBytes := retainedBindingBytes("x", opX) + retainedBindingBytes("y", opY)
	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != wantBytes+wantOpBytes || gotSlots != 4 {
		t.Fatalf("RetainedUsage before Abort = (%d,%d), want (%d,4)", gotBytes, gotSlots, wantBytes+wantOpBytes)
	}

	reg.Abort()

	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != wantBytes || gotSlots != 2 {
		t.Fatalf("TestAbortRefundsOwnedRetainedCapacity: RetainedUsage after Abort = (%d,%d), want (%d,2); each removed op-owned binding must refund its reserved bytes and slot without touching host capacity",
			gotBytes, gotSlots, wantBytes)
	}
	if _, ok := root.Get("x"); ok {
		t.Fatal("TestAbortRefundsOwnedRetainedCapacity: binding x survived Abort")
	}
	if _, ok := root.Get("y"); ok {
		t.Fatal("TestAbortRefundsOwnedRetainedCapacity: binding y survived Abort")
	}
	if _, ok := root.Get("late"); !ok {
		t.Fatal("TestAbortRefundsOwnedRetainedCapacity: host binding late was dropped by Abort")
	}
}

// TestAbortReleasesSettledChargeOnce pins the meter side of an aborted
// registration: a removed op-owned cell whose charge was settled
// (cell.retainedMeter set) is released exactly once, with its exact charged
// amounts, and the charge backing a surviving host binding is never
// released.
func TestAbortReleasesSettledChargeOnce(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &chargeFailingMeter{}
	root.SetRetainedMeter(meter)
	hostVal := Int{V: 7}
	if err := root.Set("host", hostVal); err != nil {
		t.Fatalf("set host: %v", err)
	}

	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	opX, opY := Int{V: 1}, Int{V: 2}
	if err := reg.Env().Set("x", opX); err != nil {
		t.Fatalf("set x: %v", err)
	}
	if err := reg.Env().Set("y", opY); err != nil {
		t.Fatalf("set y: %v", err)
	}

	bytesHost := retainedBindingBytes("host", hostVal)
	bytesX := retainedBindingBytes("x", opX)
	bytesY := retainedBindingBytes("y", opY)
	before := meter.snapshot()
	if before.charges != 3 || before.releases != 0 || before.chargedBytes != bytesHost+bytesX+bytesY || before.chargedSlots != 3 {
		t.Fatalf("meter before Abort: %d charges, %d releases (%d,%d), want 3 charges, 0 releases (%d,3)",
			before.charges, before.releases, before.chargedBytes, before.chargedSlots, bytesHost+bytesX+bytesY)
	}

	reg.Abort()

	snap := meter.snapshot()
	if snap.charges != before.charges {
		t.Fatalf("TestAbortReleasesSettledChargeOnce: charges went from %d to %d across Abort; rollback must not charge the meter",
			before.charges, snap.charges)
	}
	if snap.releases != 2 {
		t.Fatalf("TestAbortReleasesSettledChargeOnce: ReleaseRetained calls = %d, want 2, exactly one per removed op-owned settled cell",
			snap.releases)
	}
	if snap.releasedBytes != bytesX+bytesY || snap.releasedSlots != 2 {
		t.Fatalf("TestAbortReleasesSettledChargeOnce: ReleaseRetained amounts = (%d,%d), want (%d,2), the exact charged amounts and nothing of the surviving host charge",
			snap.releasedBytes, snap.releasedSlots, bytesX+bytesY)
	}
}

// TestAbortKeepsAdoptedCellCharge pins the adoption decision: a key a host
// write took over from the operation keeps that write and its charge. Abort
// neither refunds the binding's reserved capacity nor releases the meter
// charge backing the surviving host binding.
func TestAbortKeepsAdoptedCellCharge(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &chargeFailingMeter{}
	root.SetRetainedMeter(meter)

	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if err := reg.Env().Set("x", Int{V: 1}); err != nil {
		t.Fatalf("set x: %v", err)
	}
	adopted := Int{V: 2}
	if err := root.Set("x", adopted); err != nil {
		t.Fatalf("host set x: %v", err)
	}

	usageBytes, usageSlots := root.RetainedUsage()

	reg.Abort()

	if got, ok := root.Get("x"); !ok || got != adopted {
		t.Fatalf("TestAbortKeepsAdoptedCellCharge: x after Abort = (%v,%v), want the host write (%v,true)",
			got, ok, adopted)
	}
	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != usageBytes || gotSlots != usageSlots {
		t.Fatalf("TestAbortKeepsAdoptedCellCharge: RetainedUsage changed across Abort: (%d,%d) -> (%d,%d); an adopted entry must not be refunded",
			usageBytes, usageSlots, gotBytes, gotSlots)
	}
	if snap := meter.snapshot(); snap.releases != 0 {
		t.Fatalf("TestAbortKeepsAdoptedCellCharge: ReleaseRetained calls = %d, want 0; an adopted entry's charge must not be released",
			snap.releases)
	}
}

// TestAbortRestoredBindingKeepsCharge pins that a binding Abort restores
// rather than removes keeps its charge: the meter charge backing the
// surviving prior binding is never released and its reserved capacity is
// never refunded.
func TestAbortRestoredBindingKeepsCharge(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &chargeFailingMeter{}
	root.SetRetainedMeter(meter)
	prior := Int{V: 7}
	if err := root.Set("x", prior); err != nil {
		t.Fatalf("set x: %v", err)
	}

	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if err := reg.Env().Set("x", Int{V: 1}); err != nil {
		t.Fatalf("op set x: %v", err)
	}

	usageBytes, usageSlots := root.RetainedUsage()

	reg.Abort()

	if got, ok := root.Get("x"); !ok || got != prior {
		t.Fatalf("TestAbortRestoredBindingKeepsCharge: x after Abort = (%v,%v), want the prior binding (%v,true)",
			got, ok, prior)
	}
	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != usageBytes || gotSlots != usageSlots {
		t.Fatalf("TestAbortRestoredBindingKeepsCharge: RetainedUsage changed across Abort: (%d,%d) -> (%d,%d); a restored entry must not be refunded",
			usageBytes, usageSlots, gotBytes, gotSlots)
	}
	if snap := meter.snapshot(); snap.charges != 1 || snap.releases != 0 {
		t.Fatalf("TestAbortRestoredBindingKeepsCharge: meter = %d charges, %d releases, want 1 charge (the prior binding's) and 0 releases; a restored binding keeps its charge",
			snap.charges, snap.releases)
	}
}

// reentrantReleaseMeter is a host meter that re-enters the environment from
// ReleaseRetained, the way an embedder callback does. A rollback that runs
// the release while the env lock is held deadlocks in here.
type reentrantReleaseMeter struct {
	chargeFailingMeter
	env *Env
}

func (m *reentrantReleaseMeter) ReleaseRetained(bytes, slots int64) {
	m.chargeFailingMeter.ReleaseRetained(bytes, slots)
	_, _ = m.env.Get("x")
	_ = m.env.Set("after-release", Int{V: 1})
}

// TestAbortNoMeterCallUnderEnvLock pins that Abort never reaches the meter
// while the env lock is held: a release whose meter re-enters the env must
// be able to take the lock, so Abort has to collect releases under the lock
// and run them after releasing it.
func TestAbortNoMeterCallUnderEnvLock(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &reentrantReleaseMeter{env: root}
	root.SetRetainedMeter(meter)

	reg, err := root.BeginRegistration()
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if err := reg.Env().Set("x", Int{V: 1}); err != nil {
		t.Fatalf("set x: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		reg.Abort()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TestAbortNoMeterCallUnderEnvLock: Abort blocked for the 2s guard; ReleaseRetained ran while the env lock was held")
	}
	if _, ok := root.Get("x"); ok {
		t.Fatal("TestAbortNoMeterCallUnderEnvLock: binding x survived Abort")
	}
}
