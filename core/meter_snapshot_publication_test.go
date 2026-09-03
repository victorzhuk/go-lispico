package core

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

// The publication of a wrapped total lasts from addCharge's add until
// saturateCounter pins the counter back, so the load has to be heavy enough
// that a reader lands inside that window rather than around it.
const (
	meterSnapshotChargers = 8
	meterSnapshotReaders  = 4
	meterSnapshotCharges  = 100_000
)

// TestEvalMeter_SnapshotNeverPublishesAWrappedTotal pins what EvalMeter.Snapshot
// may report while charges are in flight from any number of goroutines.
// Reductions and AllocationBytes are running totals on a public API: neither may
// ever be observed negative, and neither may ever be observed below the value
// the same reader saw before it.
//
// addCharge commits counter.Add(n) before it can test whether the counter still
// held a plain total, so a counter already pinned at the int64 ceiling hands the
// wrapped sum to every concurrent Snapshot until saturateCounter pins it back.
// The limit still holds - nothing over budget is admitted - but the number the
// embedder reads is not a total.
//
// The seed charge is what makes that window reachable: it is larger than the
// whole budget, so saturateCounter records it at math.MaxInt64 and every
// ordinary charge after it overflows the add. The pin and the sample count are
// asserted at the end rather than assumed, so a case that never reached the
// ceiling, or never sampled, fails instead of passing on a window it did not
// exercise.
func TestEvalMeter_SnapshotNeverPublishesAWrappedTotal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		charge  func(*evalState, int64) error
		counter func(*evalState) *atomic.Int64
		observe func(EvalMeterSnapshot) int64
	}{
		{
			name:    "allocation bytes",
			charge:  (*evalState).chargeAllocBytes,
			counter: func(st *evalState) *atomic.Int64 { return &st.allocBytes },
			observe: func(s EvalMeterSnapshot) int64 { return s.AllocationBytes },
		},
		{
			name:    "reductions",
			charge:  (*evalState).chargeReductions,
			counter: func(st *evalState) *atomic.Int64 { return &st.reductions },
			observe: func(s EvalMeterSnapshot) int64 { return s.Reductions },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newEvalStateWithLimits(allocLedgerCeiling, allocLedgerCeiling)
			if err := tc.charge(st, math.MaxInt64); !isLedgerLimitError(err) {
				t.Fatalf("seed charge of %d under a %d ceiling = %v, want %s",
					int64(math.MaxInt64), allocLedgerCeiling, err, CodeResourceLimit)
			}

			var negative, decreasing, samples atomic.Int64
			done := make(chan struct{})

			var reading sync.WaitGroup
			for range meterSnapshotReaders {
				reading.Add(1)
				go func() {
					defer reading.Done()
					meter := EvalMeter{st: st}
					var prev, neg, dec, seen int64
					for {
						select {
						case <-done:
							negative.Add(neg)
							decreasing.Add(dec)
							samples.Add(seen)
							return
						default:
						}
						got := tc.observe(meter.Snapshot())
						seen++
						if got < 0 {
							neg++
						}
						if got < prev {
							dec++
						}
						prev = got
					}
				}()
			}

			var charging sync.WaitGroup
			for range meterSnapshotChargers {
				charging.Add(1)
				go func() {
					defer charging.Done()
					for range meterSnapshotCharges {
						_ = tc.charge(st, 1)
					}
				}()
			}
			charging.Wait()
			close(done)
			reading.Wait()

			if got := samples.Load(); got == 0 {
				t.Fatalf("%d readers took %d snapshots: the case observed nothing and cannot answer for what Snapshot publishes",
					meterSnapshotReaders, got)
			}
			if got := tc.counter(st).Load(); got != math.MaxInt64 {
				t.Fatalf("counter = %d after the seed charge and %d ordinary charges, want %d: the counter is off the int64 ceiling, so no add here could overflow and this case never reached the window it names",
					got, meterSnapshotChargers*meterSnapshotCharges, int64(math.MaxInt64))
			}
			if neg, dec := negative.Load(), decreasing.Load(); neg > 0 || dec > 0 {
				t.Fatalf("Snapshot() published a wrapped %s: of %d samples taken while %d goroutines charged, %d read a negative total and %d read a total below the value the same reader saw before it; a public running total may never be observed negative and may never decrease",
					tc.name, samples.Load(), meterSnapshotChargers, neg, dec)
			}
		})
	}
}
