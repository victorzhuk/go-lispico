package core

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
)

// The publication of a wrapped total lasts from addCharge's add until
// saturateCounter pins the counter back, so the load has to be heavy enough
// that a reader lands inside that window rather than around it. The same load
// is charged twice: once under budget, where every charge is admitted and the
// counter climbs through real totals, and once past the ceiling, where every
// add wraps.
const (
	meterSnapshotChargers = 8
	meterSnapshotReaders  = 4
	meterSnapshotCharges  = 100_000
)

// meterSnapshotClimb is the counter value the admitted phase leaves behind. It
// has to stay under allocLedgerCeiling or the phase would start refusing
// charges and stop being a climb.
const meterSnapshotClimb = int64(meterSnapshotChargers * meterSnapshotCharges)

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
// The two charging phases are what make both guarantees reachable. The first
// runs wholly under the budget, so readers watch the counter climb through
// ordinary admitted totals and the monotonicity assertion has real values to
// compare; a run that only ever sampled a counter pinned at the ceiling would
// see one value forever and could not tell a monotone reading from any other.
// The seed charge between the phases is larger than the whole budget, so
// saturateCounter records it at math.MaxInt64 and every ordinary charge in the
// second phase overflows the add.
//
// The climb, the overflows and the sample count are asserted at the end rather
// than assumed, so a case that never sampled, never observed the counter at
// more than one total below the ceiling, or never had a charge overflow while
// the readers were sampling fails instead of passing on a window it did not
// exercise. Ending at the ceiling proves none of that: the seed alone leaves
// the counter there. Only a wrapping add can leave the counter below zero -
// saturateCounter never stores a negative value, and the seed is recorded
// straight at the ceiling - so a reader that catches it negative caught a real
// overflow mid-window.
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

			var negative, decreasing, samples, belowCeiling, wrapped atomic.Int64
			done := make(chan struct{})
			stop := sync.OnceFunc(func() { close(done) })

			var reading sync.WaitGroup
			// A t.Fatalf below leaves the readers spinning forever unless the
			// channel is closed on the way out, so the stop runs before the wait.
			defer reading.Wait()
			defer stop()

			for range meterSnapshotReaders {
				reading.Add(1)
				go func() {
					defer reading.Done()
					meter := EvalMeter{st: st}
					var prev, neg, dec, seen, distinct, wrap int64
					last := int64(-1)
					for {
						select {
						case <-done:
							negative.Add(neg)
							decreasing.Add(dec)
							samples.Add(seen)
							wrapped.Add(wrap)
							storeMaxObserved(&belowCeiling, distinct)
							return
						default:
						}
						if tc.counter(st).Load() < 0 {
							wrap++
						}
						got := tc.observe(meter.Snapshot())
						seen++
						if got < 0 {
							neg++
						}
						if got < prev {
							dec++
						}
						if got >= 0 && got != math.MaxInt64 && got != last {
							distinct++
							last = got
						}
						prev = got
					}
				}()
			}

			chargeAll := func() {
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
			}

			chargeAll()
			if got := tc.counter(st).Load(); got != meterSnapshotClimb {
				t.Fatalf("counter = %d after %d charges of 1 under a %d ceiling, want %d: the charges were not all admitted, so this case never published the climbing totals it names",
					got, meterSnapshotClimb, allocLedgerCeiling, meterSnapshotClimb)
			}

			if err := tc.charge(st, math.MaxInt64); !isLedgerLimitError(err) {
				t.Fatalf("seed charge of %d under a %d ceiling = %v, want %s",
					int64(math.MaxInt64), allocLedgerCeiling, err, CodeResourceLimit)
			}
			chargeAll()

			stop()
			reading.Wait()

			if got := samples.Load(); got == 0 {
				t.Fatalf("%d readers took %d snapshots: the case observed nothing and cannot answer for what Snapshot publishes",
					meterSnapshotReaders, got)
			}
			if got := wrapped.Load(); got == 0 {
				t.Fatalf("no reader caught the %s counter below zero across %d samples: only an add that overflowed leaves it there - the seed charge is recorded at the ceiling without one - so no charge wrapped while the readers were sampling and this case never reached the window it names",
					tc.name, samples.Load())
			}
			if got := belowCeiling.Load(); got < 2 {
				t.Fatalf("no reader observed more than %d distinct %s total below the int64 ceiling out of %d samples: the readers only ever saw the counter pinned, so the monotonicity assertion below compared one value with itself and asserted nothing",
					got, tc.name, samples.Load())
			}

			if neg := negative.Load(); neg > 0 {
				t.Errorf("Snapshot() published a wrapped %s: of %d samples taken while %d goroutines charged, %d read a negative total; a public running total may never be observed negative",
					tc.name, samples.Load(), meterSnapshotChargers, neg)
			}
			if dec := decreasing.Load(); dec > 0 {
				t.Errorf("Snapshot() published a %s below the value the same reader saw before it: of %d samples taken while %d goroutines charged, %d read a total below the previous one across %d distinct totals under the ceiling; a public running total may never decrease",
					tc.name, samples.Load(), meterSnapshotChargers, dec, belowCeiling.Load())
			}
		})
	}
}

func storeMaxObserved(dst *atomic.Int64, v int64) {
	for {
		cur := dst.Load()
		if v <= cur || dst.CompareAndSwap(cur, v) {
			return
		}
	}
}
