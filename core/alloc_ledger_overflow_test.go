package core

import (
	"errors"
	"math"
	"sync/atomic"
	"testing"
)

// allocLedgerCeiling is the allocation limit every case below runs under. Every
// charge under test is far above it, so what a case observes is the ledger's
// state after a refusal rather than a budget that happened to absorb the charge.
const allocLedgerCeiling = int64(1) << 20

// allocLedgerFollowUp is an ordinary allocation made after the refused one. It
// is far under the ceiling, so a ledger that still knows it is over budget
// refuses it on that ground alone; only a ledger that lost its own total can
// admit it.
const allocLedgerFollowUp = int64(64)

func isLedgerLimitError(err error) bool {
	var lerr *LispicoError
	return errors.As(err, &lerr) && lerr.Code == CodeResourceLimit
}

// TestAllocLedger_RefusedChargeCannotAdmitTheNext pins the invariant every
// allocation limit rests on: a charge the ledger cannot fit must fail closed
// and must leave the counter in a state that still refuses what comes after it.
// chargeAllocBytes adds before it compares, so a charge near the int64 ceiling
// wraps the total negative; the comparison then passes and the rest of the
// evaluation allocates unchecked.
//
// A charge above the whole budget never reaches that add, so the wrap is only
// reachable on the follow-up, and only from a counter already within one
// follow-up of the int64 ceiling. seed is the refused charge that puts it
// there, and the reachability check below fails the case rather than let it
// pass without exercising the overflow it names.
func TestAllocLedger_RefusedChargeCannotAdmitTheNext(t *testing.T) {
	cases := []struct {
		name  string
		seed  int64
		bytes int64
	}{
		{"terabyte", math.MaxInt64 - (1 << 40), 1 << 40},
		{"exabyte", math.MaxInt64 - (1 << 62), 1 << 62},
		{"maxint64_minus_one", 0, math.MaxInt64 - 1},
		{"maxint64", 0, math.MaxInt64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newEvalStateWithLimits(DefaultMaxReductions, allocLedgerCeiling)

			if tc.seed > 0 {
				if err := st.chargeAllocBytes(tc.seed); !isLedgerLimitError(err) {
					t.Fatalf("seed chargeAllocBytes(%d) under a %d-byte ceiling = %v, want %s",
						tc.seed, allocLedgerCeiling, err, CodeResourceLimit)
				}
			}

			err := st.chargeAllocBytes(tc.bytes)
			if !isLedgerLimitError(err) {
				t.Fatalf("chargeAllocBytes(%d) under a %d-byte ceiling = %v, want %s",
					tc.bytes, allocLedgerCeiling, err, CodeResourceLimit)
			}
			if got := st.allocBytes.Load(); got < 0 {
				t.Fatalf("ledger = %d after a refused %d-byte charge, want a non-negative total: a negative used is not a budget, and every later charge is compared against it",
					got, tc.bytes)
			}
			if got := st.allocBytes.Load(); got <= math.MaxInt64-allocLedgerFollowUp {
				t.Fatalf("ledger = %d after a refused %d-byte charge, want a total within %d of the int64 ceiling: a %d-byte follow-up cannot overflow from here, so this case would assert the plain over-budget refusal and observe nothing about the wrap",
					got, tc.bytes, allocLedgerFollowUp, allocLedgerFollowUp)
			}

			err = st.chargeAllocBytes(allocLedgerFollowUp)
			if !isLedgerLimitError(err) {
				t.Fatalf("chargeAllocBytes(%d) after a refused %d-byte charge = %v, want %s: the refused charge left the ledger able to admit the next allocation, so the limit is bypassed for the rest of the evaluation",
					allocLedgerFollowUp, tc.bytes, err, CodeResourceLimit)
			}
			if got := st.allocBytes.Load(); got < 0 {
				t.Fatalf("ledger = %d after a refused %d-byte charge and a %d-byte follow-up, want a non-negative total",
					got, tc.bytes, allocLedgerFollowUp)
			}
		})
	}
}

// TestAddCharge_OversizedChargeNeverLandsOnTheCounter pins the bound every other
// charge rests on: a charge larger than the whole budget is refused without
// being added, so no add on the counter can move it by more than one budget.
// That bound is what leaves an overflowing add far below zero instead of at a
// small positive residue a concurrent charge would read as a fresh budget.
func TestAddCharge_OversizedChargeNeverLandsOnTheCounter(t *testing.T) {
	cases := []struct {
		name string
		used int64
		n    int64
	}{
		{"fresh_counter", 0, allocLedgerCeiling + 1},
		{"counter_holding_a_refused_charge", 1 << 40, 1 << 62},
		{"counter_at_the_int64_ceiling", math.MaxInt64, math.MaxInt64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var counter atomic.Int64
			counter.Store(tc.used)

			if _, exact := addCharge(&counter, allocLedgerCeiling, tc.n); exact {
				t.Fatalf("addCharge(%d) against a %d-byte budget reported an exact total, want it refused: the charge is larger than the whole budget",
					tc.n, allocLedgerCeiling)
			}
			if got := counter.Load(); got != tc.used {
				t.Fatalf("counter = %d after a refused %d-byte charge from %d, want it untouched: an add above the budget can carry the counter anywhere, including back under the budget, and only saturateCounter may write a total the add cannot reach safely",
					got, tc.n, tc.used)
			}
		})
	}
}

// TestAllocLedger_RefusedOversizedChargeIsRecordedInFull pins what the ledger
// keeps for a charge it refused for being larger than the whole budget: the
// charge's own amount, and math.MaxInt64 once the total would pass it. Dropping
// the amount would under-report what the evaluation asked for; letting the total
// pass the ceiling would wrap the counter negative and admit everything after.
func TestAllocLedger_RefusedOversizedChargeIsRecordedInFull(t *testing.T) {
	cases := []struct {
		name    string
		charges []int64
		want    int64
	}{
		{"records the charge in full", []int64{1 << 40}, 1 << 40},
		{"pins at the int64 ceiling", []int64{math.MaxInt64, 1 << 40}, math.MaxInt64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newEvalStateWithLimits(DefaultMaxReductions, allocLedgerCeiling)

			for _, n := range tc.charges {
				if err := st.chargeAllocBytes(n); !isLedgerLimitError(err) {
					t.Fatalf("chargeAllocBytes(%d) under a %d-byte ceiling = %v, want %s",
						n, allocLedgerCeiling, err, CodeResourceLimit)
				}
			}

			if got := st.allocBytes.Load(); got != tc.want {
				t.Fatalf("ledger = %d after refusing %v under a %d-byte ceiling, want %d",
					got, tc.charges, allocLedgerCeiling, tc.want)
			}
		})
	}
}
