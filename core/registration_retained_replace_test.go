package core

import (
	"context"
	"testing"
)

// transferMeter is a host meter that records the exact ChargeRetained and
// ReleaseRetained amounts and always grants leases, so a ReplaceCell ownership
// transfer can be pinned against the bytes and slots a charge actually carries.
type transferMeter struct {
	charges       int
	releases      int
	chargedBytes  int64
	chargedSlots  int64
	releasedBytes int64
	releasedSlots int64
}

func (m *transferMeter) LeaseEval(reductions, allocBytes int64) (int64, int64, error) {
	return reductions, allocBytes, nil
}

func (m *transferMeter) ReturnEval(reductions, allocBytes int64) {}

func (m *transferMeter) ChargeRetained(bytes, slots int64) error {
	m.charges++
	m.chargedBytes += bytes
	m.chargedSlots += slots
	return nil
}

func (m *transferMeter) ReleaseRetained(bytes, slots int64) {
	m.releases++
	m.releasedBytes += bytes
	m.releasedSlots += slots
}

// transferPendingEvalCtx builds the registration-bound evaluation state the
// runtime installs inside an eval: the first view write records its retained
// charge on st.pendingCellAllocs through the bound eval instead of charging the
// meter at write time.
func transferPendingEvalCtx(m *transferMeter) (context.Context, *evalState) {
	st := newEvalState()
	return WithEvalMeter(context.WithValue(context.Background(), evalStateKey{}, st), m), st
}

// TestRegistration_ReplaceCellDropsReplacedPendingCharge pins the pending
// ownership transfer: an op Set that left its retained charge pending, followed
// by a pinned ReplaceCell, must repoint the pending ledger to the replacement
// cell. DropPendingCharges then marks the replacement dropped (the Abort that
// follows would remove it), settlement charges nothing, and Abort refunds the
// reserved capacity — no meter call may anchor on the retired op cell.
func TestRegistration_ReplaceCellDropsReplacedPendingCharge(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &transferMeter{}
	reg := abortBegin(t, root)
	view := reg.Env()
	ctx, st := transferPendingEvalCtx(meter)
	reg.BindPendingEval(ctx)
	counterTry(t, "view.SetWithContext(x)", func() error { return view.SetWithContext(ctx, "x", Int{V: 1}) })
	counterTry(t, "view.ReplaceCellWithContext(x)", func() error { return view.ReplaceCellWithContext(ctx, "x", Int{V: 2}) })

	reg.DropPendingCharges()
	if err := st.settleRetained(); err != nil {
		t.Fatalf("settleRetained: %v", err)
	}
	reg.Abort()

	if meter.charges != 0 || meter.releases != 0 {
		t.Fatalf("TestRegistration_ReplaceCellDropsReplacedPendingCharge: meter = %d charges, %d releases, want 0/0; ReplaceCell must repoint the op-owned pending charge to the replacement cell so DropPendingCharges drops it, and no meter may be charged for the retired op cell",
			meter.charges, meter.releases)
	}
	abortWantAbsent(t, root, "x", "x after Abort")
	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != 0 || gotSlots != 0 {
		t.Fatalf("TestRegistration_ReplaceCellDropsReplacedPendingCharge: RetainedUsage after Abort = (%d,%d), want (0,0); the removed binding's reservation must be refunded once",
			gotBytes, gotSlots)
	}
}

// TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce pins the settled
// ownership transfer: an op-owned binding whose charge was already applied to
// the meter (a final write settling cell.retainedMeter) must carry that anchor
// across a pinned ReplaceCell to the replacement cell, so the Abort that
// removes the replacement releases the exact charge exactly once. The retired
// op cell must not keep an anchor that a later removal path could release
// again, and the replace itself must not re-charge the meter.
func TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &transferMeter{}
	root.SetRetainedMeter(meter)
	valX := Int{V: 1}
	bytesX := retainedBindingBytes("x", valX)

	reg := abortBegin(t, root)
	view := reg.Env()
	counterTry(t, "view.Set(x)", func() error { return view.Set("x", valX) })
	counterTry(t, "view.ReplaceCell(x)", func() error { return view.ReplaceCell("x", Int{V: 2}) })

	if meter.charges != 1 || meter.chargedBytes != bytesX || meter.chargedSlots != 1 {
		t.Fatalf("TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce: meter before Abort = %d charges (%d,%d), want 1 charge (%d,1); ReplaceCell must not charge the meter again for capacity the op-owned binding already reserved and charged",
			meter.charges, meter.chargedBytes, meter.chargedSlots, bytesX)
	}

	reg.Abort()

	if meter.releases != 1 {
		t.Fatalf("TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce: ReleaseRetained calls = %d, want exactly 1; ReplaceCell must transfer the settled charge anchor from the retired op cell to the replacement so Abort's removal releases it once",
			meter.releases)
	}
	if meter.releasedBytes != bytesX || meter.releasedSlots != 1 {
		t.Fatalf("TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce: ReleaseRetained amounts = (%d,%d), want (%d,1), the exact charged amounts",
			meter.releasedBytes, meter.releasedSlots, bytesX)
	}
	if meter.charges != 1 {
		t.Fatalf("TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce: charges across Abort = %d, want 1; rollback must not charge the meter", meter.charges)
	}
	abortWantAbsent(t, root, "x", "x after Abort")
	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != 0 || gotSlots != 0 {
		t.Fatalf("TestRegistration_ReplaceCellAbortReleasesSettledChargeOnce: RetainedUsage after Abort = (%d,%d), want (0,0)", gotBytes, gotSlots)
	}
}

// TestRegistration_ReplaceCellSettlesChargeOnReplacementCell pins where a
// transferred pending anchor settles: after a pinned ReplaceCell the operation
// completes, so the charge must finalize on the live replacement binding — and
// the completed root's Delete+Rebuild of the replacement then releases that
// exact charge exactly once.
func TestRegistration_ReplaceCellSettlesChargeOnReplacementCell(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &transferMeter{}
	reg := abortBegin(t, root)
	view := reg.Env()
	ctx, st := transferPendingEvalCtx(meter)
	reg.BindPendingEval(ctx)
	valA := Int{V: 1}
	bytesA := retainedBindingBytes("x", valA)
	counterTry(t, "view.SetWithContext(x)", func() error { return view.SetWithContext(ctx, "x", valA) })
	cellA, ok := root.CellLocal("x")
	if !ok || cellA == nil {
		t.Fatal("op Set did not leave a cell for x")
	}
	counterTry(t, "view.ReplaceCellWithContext(x)", func() error { return view.ReplaceCellWithContext(ctx, "x", Int{V: 2}) })
	cellB, ok := root.CellLocal("x")
	if !ok || cellB == nil || cellB == cellA {
		t.Fatal("ReplaceCell did not install a fresh replacement cell for x")
	}
	reg.Complete()
	if err := st.settleRetained(); err != nil {
		t.Fatalf("settleRetained: %v", err)
	}

	if meter.charges != 1 || meter.chargedBytes != bytesA || meter.chargedSlots != 1 {
		t.Fatalf("TestRegistration_ReplaceCellSettlesChargeOnReplacementCell: meter after settle = %d charges (%d,%d), want 1 charge (%d,1)",
			meter.charges, meter.chargedBytes, meter.chargedSlots, bytesA)
	}

	root.Delete("x")
	if freedBytes, freedSlots := root.Rebuild(); freedBytes != bytesA || freedSlots != 1 {
		t.Fatalf("TestRegistration_ReplaceCellSettlesChargeOnReplacementCell: Rebuild freed (%d,%d), want (%d,1); the compacted-away replacement cell carried the binding's reservation",
			freedBytes, freedSlots, bytesA)
	}
	if meter.releases != 1 || meter.releasedBytes != bytesA || meter.releasedSlots != 1 {
		t.Fatalf("TestRegistration_ReplaceCellSettlesChargeOnReplacementCell: ReleaseRetained = %d calls totaling (%d,%d), want exactly 1 call of (%d,1)",
			meter.releases, meter.releasedBytes, meter.releasedSlots, bytesA)
	}
}

// TestRegistration_ReplaceCellAfterRebuildDropsReplacedCharge pins that the
// pending transfer survives scope compaction: a live op cell that Rebuild keeps
// in place is still the journal's last write, so the ReplaceCell that follows
// must still repoint its pending charge to the replacement, and the
// DropPendingCharges + settle + Abort sequence must leave the meter untouched
// and the capacity refunded.
func TestRegistration_ReplaceCellAfterRebuildDropsReplacedCharge(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &transferMeter{}
	reg := abortBegin(t, root)
	view := reg.Env()
	ctx, st := transferPendingEvalCtx(meter)
	reg.BindPendingEval(ctx)
	counterTry(t, "view.SetWithContext(x)", func() error { return view.SetWithContext(ctx, "x", Int{V: 1}) })
	root.Rebuild()
	counterTry(t, "view.ReplaceCellWithContext(x)", func() error { return view.ReplaceCellWithContext(ctx, "x", Int{V: 2}) })

	reg.DropPendingCharges()
	if err := st.settleRetained(); err != nil {
		t.Fatalf("settleRetained: %v", err)
	}
	reg.Abort()

	if meter.charges != 0 || meter.releases != 0 {
		t.Fatalf("TestRegistration_ReplaceCellAfterRebuildDropsReplacedCharge: meter = %d charges, %d releases, want 0/0; the pending charge on a pinned cell that Rebuild kept in place must still transfer to the replacement so DropPendingCharges drops it before any meter runs",
			meter.charges, meter.releases)
	}
	abortWantAbsent(t, root, "x", "x after Abort")
	if gotBytes, gotSlots := root.RetainedUsage(); gotBytes != 0 || gotSlots != 0 {
		t.Fatalf("TestRegistration_ReplaceCellAfterRebuildDropsReplacedCharge: RetainedUsage after Abort = (%d,%d), want (0,0)", gotBytes, gotSlots)
	}
}

// TestRegistration_HostRebaseBeforeReplaceKeepsCharge pins the ownership
// boundary: a host write that re-based the key after the op's last write leaves
// the journal not pinning the old cell, so a later ReplaceCell through the view
// transfers nothing and charges nothing — the host binding and its single
// charge survive the failed operation untouched, with no meter release.
func TestRegistration_HostRebaseBeforeReplaceKeepsCharge(t *testing.T) {
	root := NewEnvWithRetainedLimits(nil, 0, 0)
	meter := &transferMeter{}
	root.SetRetainedMeter(meter)
	reg := abortBegin(t, root)
	view := reg.Env()
	counterTry(t, "view.Set(x)", func() error { return view.Set("x", Int{V: 1}) })
	host := Int{V: 7}
	counterSeed(t, "host Set(x)", root.Set("x", host))
	counterTry(t, "view.ReplaceCell(x)", func() error { return view.ReplaceCell("x", Int{V: 2}) })

	reg.Abort()

	if got, ok := root.Get("x"); !ok || got != host {
		t.Fatalf("TestRegistration_HostRebaseBeforeReplaceKeepsCharge: x after Abort = (%v,%v), want the host binding (%v,true); a view ReplaceCell after a host re-base must not remove the host binding",
			got, ok, host)
	}
	if meter.charges != 1 || meter.releases != 0 {
		t.Fatalf("TestRegistration_HostRebaseBeforeReplaceKeepsCharge: meter = %d charges, %d releases, want 1/0; the host binding keeps its single charge and pins false must block any ownership transfer or release",
			meter.charges, meter.releases)
	}
}
