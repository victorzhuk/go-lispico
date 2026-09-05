package perfgate

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// samplesPerCell is the release workflow's appended-run count, and the reason
// "the median" of an even sample set is a choice rather than a lookup.
const samplesPerCell = 10

// TestReadBenchmarkMetrics_MatchesBenchstat is the measurement that licenses
// judging bytes and allocs off the raw files instead of benchstat: on the
// profile whose two arms share a runner, the raw medians must reproduce
// benchstat's own Old and New for every cell on both axes. It is also what
// pins the median convention -- several cells land on a half-integer, so a
// lower-middle or upper-middle reading fails here.
func TestReadBenchmarkMetrics_MatchesBenchstat(t *testing.T) {
	t.Parallel()

	oldMetrics := readSampleMetrics(t, "bench-evaluator.txt")
	newMetrics := readSampleMetrics(t, "bench-vm.txt")
	cells := parsePinnedBenchstat(t)

	require.Len(t, cells, 27)
	require.Len(t, oldMetrics, len(cells), "baseline raw file yields a different cell set than benchstat.csv")
	require.Len(t, newMetrics, len(cells), "candidate raw file yields a different cell set than benchstat.csv")

	for name, cell := range cells {
		om, ok := oldMetrics[name]
		require.True(t, ok, "cell %q missing from the baseline raw file", name)
		nm, ok := newMetrics[name]
		require.True(t, ok, "cell %q missing from the candidate raw file", name)

		assert.Equal(t, cell.Bytes.Old, om.MedianBytesPerOp(), "cell %q: raw bytes median must reproduce benchstat's Old", name)
		assert.Equal(t, cell.Bytes.New, nm.MedianBytesPerOp(), "cell %q: raw bytes median must reproduce benchstat's New", name)
		assert.Equal(t, cell.Allocs.Old, om.MedianAllocsPerOp(), "cell %q: raw allocs median must reproduce benchstat's Old", name)
		assert.Equal(t, cell.Allocs.New, nm.MedianAllocsPerOp(), "cell %q: raw allocs median must reproduce benchstat's New", name)

		assert.Len(t, om.BytesPerOp, samplesPerCell, "cell %q: per-sample bytes must stay reachable, not collapse to a median", name)
		assert.Len(t, om.AllocsPerOp, samplesPerCell, "cell %q: per-sample allocs must stay reachable, not collapse to a median", name)
	}
}

// TestReadBenchmarkMetrics_DeltaPctDerivation guards against a MetricResult
// built from two medians with DeltaPct and Significant left at their zero
// values: nonIncreasing returns PASS on DeltaPct <= 0 before it ever reads
// New-Old, so an underived delta passes every cell on both axes.
func TestReadBenchmarkMetrics_DeltaPctDerivation(t *testing.T) {
	t.Parallel()

	const cellName = "Goldset/counter-closure-2"

	baseline := readSampleMetrics(t, "bench-evaluator.txt")[cellName]
	candidate := readSampleMetrics(t, "bench-vm.txt")[cellName]
	want := parsePinnedBenchstat(t)[cellName]

	bytesRes, allocsRes := CompareSamples(baseline, candidate)

	assert.Equal(t, want.Bytes.Old, bytesRes.Old, "bytes Old must be the baseline median")
	assert.Equal(t, want.Bytes.New, bytesRes.New, "bytes New must be the candidate median")
	assert.InDelta(t, want.Bytes.DeltaPct, bytesRes.DeltaPct, 0.01, "bytes DeltaPct must be derived from the two medians")
	assert.True(t, bytesRes.Significant, "an exact byte count is never statistically inconclusive")

	assert.Equal(t, want.Allocs.Old, allocsRes.Old, "allocs Old must be the baseline median")
	assert.Equal(t, want.Allocs.New, allocsRes.New, "allocs New must be the candidate median")
	assert.InDelta(t, want.Allocs.DeltaPct, allocsRes.DeltaPct, 0.01, "allocs DeltaPct must be derived from the two medians")
	assert.True(t, allocsRes.Significant, "an exact allocation count is never statistically inconclusive")
}

// TestCrossRunner_ZeroBaselineBytesFails covers the same derivation where the
// baseline median is 0. GoldsetCall/call-boundary genuinely reads 0 B/op and 0
// allocs/op in the committed profile, so an unguarded (New-Old)/Old there
// produces NaN on an unchanged cell and lets a first allocation through.
func TestCrossRunner_ZeroBaselineBytesFails(t *testing.T) {
	t.Parallel()

	metrics := readSampleMetrics(t, "bench-vm.txt")
	zero := metrics["GoldsetCall/call-boundary-2"]
	positive := metrics["Goldset/guard-nil-2"]

	require.Zero(t, zero.MedianBytesPerOp(), "call-boundary is the zero-baseline cell; it no longer reads 0 B/op")

	t.Run("unchanged at zero", func(t *testing.T) {
		t.Parallel()

		bytesRes, allocsRes := CompareSamples(zero, zero)
		assert.False(t, math.IsNaN(bytesRes.DeltaPct), "a zero baseline must not divide into NaN")
		assert.Zero(t, bytesRes.DeltaPct, "an unchanged zero-byte cell has a zero delta")
		assert.Zero(t, allocsRes.DeltaPct, "an unchanged zero-alloc cell has a zero delta")

		res := Evaluate(CellComparison{Name: zero.Name, Bytes: bytesRes, Allocs: allocsRes}, TierDataDominated, ModeNonRegression)
		assert.NotEqual(t, VerdictFail, res.Verdict, "an unchanged zero-byte cell must not fail: %s", res.Reason)
	})

	t.Run("first allocation on a zero baseline", func(t *testing.T) {
		t.Parallel()

		bytesRes, allocsRes := CompareSamples(zero, positive)
		assert.False(t, math.IsNaN(bytesRes.DeltaPct), "a zero baseline must not divide into NaN")
		assert.True(t, math.IsInf(bytesRes.DeltaPct, 1), "a positive candidate against a zero baseline is an infinite increase")

		res := Evaluate(CellComparison{Name: zero.Name, Bytes: bytesRes, Allocs: allocsRes}, TierDataDominated, ModeNonRegression)
		assert.Equal(t, VerdictFail, res.Verdict, "a first allocation on a zero-byte cell must fail")
		assert.Contains(t, res.Reason, "bytes increased")
	})
}

func readSampleMetrics(t *testing.T, file string) map[string]SampleMetrics {
	t.Helper()

	f, err := os.Open(filepath.Join(pinnedProfileDir, file))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	metrics, err := ReadBenchmarkMetrics(f)
	require.NoError(t, err)
	return metrics
}

func parsePinnedBenchstat(t *testing.T) map[string]CellComparison {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(pinnedProfileDir, "benchstat.csv"))
	require.NoError(t, err)
	cells, err := ParseBenchstatCSV(data)
	require.NoError(t, err)
	return cells
}
