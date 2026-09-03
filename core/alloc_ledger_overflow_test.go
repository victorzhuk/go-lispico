package core

import (
	"errors"
	"math"
	"testing"
)

// allocLedgerCeiling is the allocation limit every case below runs under. Each
// first charge is far above it, so what the case observes is the ledger's state
// after a refusal rather than a budget that happened to absorb the charge.
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
func TestAllocLedger_RefusedChargeCannotAdmitTheNext(t *testing.T) {
	cases := []struct {
		name  string
		bytes int64
	}{
		{"terabyte", 1 << 40},
		{"exabyte", 1 << 62},
		{"maxint64_minus_one", math.MaxInt64 - 1},
		{"maxint64", math.MaxInt64},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newEvalStateWithLimits(DefaultMaxReductions, allocLedgerCeiling)

			err := st.chargeAllocBytes(tc.bytes)
			if !isLedgerLimitError(err) {
				t.Fatalf("chargeAllocBytes(%d) under a %d-byte ceiling = %v, want %s",
					tc.bytes, allocLedgerCeiling, err, CodeResourceLimit)
			}
			if got := st.allocBytes.Load(); got < 0 {
				t.Fatalf("ledger = %d after a refused %d-byte charge, want a non-negative total: a negative used is not a budget, and every later charge is compared against it",
					got, tc.bytes)
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
