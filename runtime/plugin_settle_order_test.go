package runtime

import (
	"errors"
	"testing"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// TestUseFailedOpDropsPendingsBeforeSettlement pins the settlement-order
// contract: the failed operation must not charge the meter for the cell its
// abort removes. A charge-then-release ordering nets to the same meter
// reading but passes the settlement's ChargeRetained to the host meter of a
// binding that is about to be deleted, so the contract is zero charges and
// zero releases, with the reserved capacity refunded exactly once and the
// unused compute lease returned exactly once.
func TestUseFailedOpDropsPendingsBeforeSettlement(t *testing.T) {
	m := &recordingMeter{}
	eng, err := New(nil, WithDialect(clojure.Dialect()), WithTreeWalker(), WithEngineMeter(m))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	root := eng.RootEnv()
	m.reset()
	preBytes, preSlots := root.RetainedUsage()

	b := newRPBarrier()
	initErr := errors.New("dps: deliberate init failure")
	p := &rpPlugin{name: "dps", version: "1.0.0", init: func(env *core.Env) error {
		if err := env.Set("dps/one", core.Int{V: 1}); err != nil {
			return err
		}
		if err := b.pause(); err != nil {
			return err
		}
		return initErr
	}}
	done := rpGo(func() error { return eng.Use(p) })

	rpWait(t, b.entered, "Init entry")
	close(b.release)

	if err := rpResult(t, done, "Use"); !errors.Is(err, initErr) {
		t.Fatalf("Use error = %v, want init failure", err)
	}
	rpWantAbsent(t, root, "dps/one")
	snap := m.snapshot()
	if snap.chargeCalls != 0 || snap.releaseCalls != 0 {
		t.Fatalf("charge/release calls = %d/%d, want 0/0: settlement must not charge a cell the abort's removal already decided",
			snap.chargeCalls, snap.releaseCalls)
	}
	if postBytes, postSlots := root.RetainedUsage(); postBytes != preBytes || postSlots != preSlots {
		t.Fatalf("retained usage after failed Use = (%d, %d), want pre-op (%d, %d): capacity refunded exactly once",
			postBytes, postSlots, preBytes, preSlots)
	}
	if snap.leaseCalls != 1 || snap.returnCalls != 1 {
		t.Fatalf("lease/return calls = %d/%d, want 1/1: settlement unwinds exactly once across the pending filter",
			snap.leaseCalls, snap.returnCalls)
	}
}
