package perfgate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// notComparable is the caller-supplied reason a cell's two B/op figures cannot
// be compared: the two runs were measured on different runners, so the figures
// carry different iteration counts.
const notComparable = "runner identities differ: AMD EPYC 7763 vs INTEL(R) XEON(R) PLATINUM 8573C"

// TestEvaluate_BytesNotComparable pins the narrowing of the bytes axis to
// runs whose identities match. Where they differ, the axis is undecided: it
// converts into neither a pass nor a failure, and it must not short-circuit
// ahead of the allocs check, which stays exact across runners.
func TestEvaluate_BytesNotComparable(t *testing.T) {
	t.Parallel()

	t.Run("inconclusive rather than failing", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:               "Goldset/eval-simple",
			Latency:            MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			Bytes:              MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			Allocs:             MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			BytesAllowanceBOp:  8,
			BytesNotComparable: notComparable,
		}
		res := Evaluate(cell, TierDataDominated, ModeNonRegression)
		assert.Equal(t, VerdictInconclusive, res.Verdict, "got %s", res.Reason)
		assert.Contains(t, res.Reason, "bytes not comparable: "+notComparable)
		assert.NotContains(t, res.Reason, "bytes increased")
	})

	t.Run("allocs still decided", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:               "Goldset/eval-simple",
			Latency:            MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			Bytes:              MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			Allocs:             MetricResult{Old: 2, New: 3, DeltaPct: 50, Significant: true, N: 10},
			BytesAllowanceBOp:  8,
			BytesNotComparable: notComparable,
		}
		res := Evaluate(cell, TierDataDominated, ModeNonRegression)
		assert.Equal(t, VerdictFail, res.Verdict, "got %s", res.Reason)
		assert.Contains(t, res.Reason, "allocs increased by 50.00%")
	})

	t.Run("both undecided axes reported", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:               "Goldset/eval-simple",
			Latency:            MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
			Bytes:              MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			Allocs:             MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			BytesAllowanceBOp:  8,
			BytesNotComparable: notComparable,
		}
		res := Evaluate(cell, TierDataDominated, ModeNonRegression)
		assert.Equal(t, VerdictInconclusive, res.Verdict, "got %s", res.Reason)
		assert.Equal(t, "bytes not comparable: "+notComparable+"; latency delta not statistically significant", res.Reason)
	})

	t.Run("startup tier does not reach the absolute bound", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:               "Goldset/rule-load",
			Latency:            MetricResult{Old: 0.0001, New: 0.0001, DeltaPct: 0, Significant: true, N: 10},
			Bytes:              MetricResult{Old: 100_000, New: 400_000, DeltaPct: 300, Significant: true, N: 10},
			Allocs:             MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			BytesAllowanceBOp:  8,
			BytesNotComparable: notComparable,
		}
		res := Evaluate(cell, TierStartup, ModeNonRegression)
		assert.Equal(t, VerdictInconclusive, res.Verdict, "got %s", res.Reason)
		assert.Contains(t, res.Reason, "bytes not comparable: "+notComparable)
		assert.NotContains(t, res.Reason, "absolute overhead")
	})

	t.Run("within-tolerance tier is narrowed too", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:               "Goldset/eval-simple",
			Latency:            MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			Bytes:              MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			Allocs:             MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			BytesAllowanceBOp:  8,
			BytesNotComparable: notComparable,
		}
		res := Evaluate(cell, TierDataDominated, ModeFirstAuthorization)
		assert.Equal(t, VerdictInconclusive, res.Verdict, "got %s", res.Reason)
		assert.Contains(t, res.Reason, "bytes not comparable: "+notComparable)
	})

	t.Run("concurrent tier is narrowed too", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:               "Goldset/dispatch",
			Latency:            MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			Bytes:              MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			Allocs:             MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			RaceClean:          true,
			BytesAllowanceBOp:  8,
			BytesNotComparable: notComparable,
		}
		res := Evaluate(cell, TierConcurrent, ModeNonRegression)
		assert.Equal(t, VerdictInconclusive, res.Verdict, "got %s", res.Reason)
		assert.Contains(t, res.Reason, "bytes not comparable: "+notComparable)
	})

	// The unset field is the enforced case, and stays enforced: only a stated
	// reason excuses the bytes bound.
	t.Run("unset field enforces the bytes bound", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:              "Goldset/eval-simple",
			Latency:           MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			Bytes:             MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			Allocs:            MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			BytesAllowanceBOp: 8,
		}
		res := Evaluate(cell, TierDataDominated, ModeNonRegression)
		assert.Equal(t, VerdictFail, res.Verdict, "got %s", res.Reason)
		assert.Contains(t, res.Reason, "bytes increased by 100.00% (+100 B/op against a 8 B/op allowance)")
	})
}
