package perfgate

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
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
		assert.Equal(t, samplesPerCell, om.LatencyN, "cell %q: every pinned run must carry a counted timed sample", name)
		assert.Equal(t, samplesPerCell, nm.LatencyN, "cell %q: every pinned run must carry a counted timed sample", name)
	}
}

// TestReadBenchmarkMetrics_Presence pins the distinction between an absent
// metric and a measured zero: a cell entry exists once a line names it, but
// each axis records presence through its own sample count. A literal 0 B/op or
// 0 allocs/op is a positive-length sample, not an empty one.
func TestReadBenchmarkMetrics_Presence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		latencyN  int
		bytesN    int
		allocsN   int
		wantError bool
	}{
		{
			name:     "time only",
			line:     "BenchmarkTimeOnly-8   100   5223 ns/op",
			latencyN: 1,
		},
		{
			name:   "bytes only",
			line:   "BenchmarkBytesOnly-8   100   1970 B/op",
			bytesN: 1,
		},
		{
			name:    "allocs only",
			line:    "BenchmarkAllocsOnly-8   100   58 allocs/op",
			allocsN: 1,
		},
		{
			name:     "complete positive",
			line:     "BenchmarkComplete-8   28188   8495 ns/op   1970 B/op   58 allocs/op",
			latencyN: 1,
			bytesN:   1,
			allocsN:  1,
		},
		{
			name:     "complete measured zero",
			line:     "BenchmarkMeasuredZero-8   100   1000000 ns/op   0 B/op   0 allocs/op",
			latencyN: 1,
			bytesN:   1,
			allocsN:  1,
		},
		{
			name:      "malformed timed value",
			line:      "BenchmarkBadTime-8   100   abc ns/op",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metrics, err := ReadBenchmarkMetrics(strings.NewReader(tt.line + "\n"))
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, metrics, 1)

			m, ok := metrics[strings.TrimPrefix(strings.Fields(tt.line)[0], "Benchmark")]
			require.True(t, ok)
			assert.Equal(t, tt.latencyN, m.LatencyN)
			assert.Len(t, m.BytesPerOp, tt.bytesN)
			assert.Len(t, m.AllocsPerOp, tt.allocsN)
		})
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

	bytesRes, allocsRes, err := CompareSamples(baseline, candidate)
	require.NoError(t, err)

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

		bytesRes, allocsRes, err := CompareSamples(zero, zero)
		require.NoError(t, err)
		assert.False(t, math.IsNaN(bytesRes.DeltaPct), "a zero baseline must not divide into NaN")
		assert.Zero(t, bytesRes.DeltaPct, "an unchanged zero-byte cell has a zero delta")
		assert.Zero(t, allocsRes.DeltaPct, "an unchanged zero-alloc cell has a zero delta")

		res := Evaluate(CellComparison{Name: zero.Name, Bytes: bytesRes, Allocs: allocsRes}, TierDataDominated, ModeNonRegression)
		assert.NotEqual(t, VerdictFail, res.Verdict, "an unchanged zero-byte cell must not fail: %s", res.Reason)
	})

	t.Run("first allocation on a zero baseline", func(t *testing.T) {
		t.Parallel()

		bytesRes, allocsRes, err := CompareSamples(zero, positive)
		require.NoError(t, err)
		assert.False(t, math.IsNaN(bytesRes.DeltaPct), "a zero baseline must not divide into NaN")
		assert.True(t, math.IsInf(bytesRes.DeltaPct, 1), "a positive candidate against a zero baseline is an infinite increase")

		res := Evaluate(CellComparison{Name: zero.Name, Bytes: bytesRes, Allocs: allocsRes}, TierDataDominated, ModeNonRegression)
		assert.Equal(t, VerdictFail, res.Verdict, "a first allocation on a zero-byte cell must fail")
		assert.Contains(t, res.Reason, "bytes increased")
	})
}

// TestCompareSamples_RequiresAllocationSamples pins the boundary between an
// absent sample set and measured evidence: a missing set would read as a zero
// median and a silent improvement, so only the all-present combination may
// reach the medians. A measured zero stays valid evidence.
func TestCompareSamples_RequiresAllocationSamples(t *testing.T) {
	t.Parallel()

	present := []float64{1}
	type combo struct {
		baselineBytes, baselineAllocs   []float64
		candidateBytes, candidateAllocs []float64
		missing                         []string
	}
	cases := make([]combo, 0, 17)
	add := func(bb, ba, cb, ca []float64, missing ...string) {
		cases = append(cases, combo{bb, ba, cb, ca, missing})
	}
	for _, bb := range [][]float64{nil, present} {
		for _, ba := range [][]float64{nil, present} {
			for _, cb := range [][]float64{nil, present} {
				for _, ca := range [][]float64{nil, present} {
					var missing []string
					if len(bb) == 0 {
						missing = append(missing, "baseline B/op")
					}
					if len(ba) == 0 {
						missing = append(missing, "baseline allocs/op")
					}
					if len(cb) == 0 {
						missing = append(missing, "candidate B/op")
					}
					if len(ca) == 0 {
						missing = append(missing, "candidate allocs/op")
					}
					add(bb, ba, cb, ca, missing...)
				}
			}
		}
	}
	// A non-nil empty slice is as absent as a nil one.
	add([]float64{}, []float64{}, []float64{}, []float64{},
		"baseline B/op", "baseline allocs/op", "candidate B/op", "candidate allocs/op")

	for i, tc := range cases {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			t.Parallel()

			baseline := SampleMetrics{BytesPerOp: tc.baselineBytes, AllocsPerOp: tc.baselineAllocs}
			candidate := SampleMetrics{BytesPerOp: tc.candidateBytes, AllocsPerOp: tc.candidateAllocs}

			bytesRes, allocsRes, err := CompareSamples(baseline, candidate)
			if len(tc.missing) == 0 {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, fmt.Sprintf("missing allocation samples: %s", strings.Join(tc.missing, ", ")))
			assert.Zero(t, bytesRes, "a refused comparison returns no bytes result")
			assert.Zero(t, allocsRes, "a refused comparison returns no allocs result")
		})
	}
}

// TestCompareSamples_NumericalControls pins the derived figures on inputs the
// presence gate now admits: measured zeros, a first allocation, uneven
// sample counts, and median conventions.
func TestCompareSamples_NumericalControls(t *testing.T) {
	t.Parallel()

	t.Run("zero to zero", func(t *testing.T) {
		t.Parallel()

		zero := SampleMetrics{BytesPerOp: []float64{0}, AllocsPerOp: []float64{0}}
		bytesRes, allocsRes, err := CompareSamples(zero, zero)
		require.NoError(t, err)
		assert.Zero(t, bytesRes.DeltaPct, "an unchanged zero-byte cell has a zero delta")
		assert.Zero(t, allocsRes.DeltaPct, "an unchanged zero-alloc cell has a zero delta")
		assert.True(t, bytesRes.Significant)
		assert.True(t, allocsRes.Significant)
		assert.Equal(t, 1, bytesRes.N)
		assert.Equal(t, 1, allocsRes.N)
	})

	t.Run("first allocation on zero baseline", func(t *testing.T) {
		t.Parallel()

		zero := SampleMetrics{BytesPerOp: []float64{0}, AllocsPerOp: []float64{0}}
		positive := SampleMetrics{BytesPerOp: []float64{16}, AllocsPerOp: []float64{1}}
		bytesRes, allocsRes, err := CompareSamples(zero, positive)
		require.NoError(t, err)
		assert.False(t, math.IsNaN(bytesRes.DeltaPct), "a zero baseline must not divide into NaN")
		assert.True(t, math.IsInf(bytesRes.DeltaPct, 1), "a first allocation on a zero baseline is an infinite increase")
		assert.False(t, math.IsNaN(allocsRes.DeltaPct), "a zero baseline must not divide into NaN")
		assert.True(t, math.IsInf(allocsRes.DeltaPct, 1), "a first allocation on a zero baseline is an infinite increase")
	})

	t.Run("uneven sample counts", func(t *testing.T) {
		t.Parallel()

		baseline := SampleMetrics{BytesPerOp: []float64{8, 12}, AllocsPerOp: []float64{1, 3}}
		candidate := SampleMetrics{BytesPerOp: []float64{10, 12, 14}, AllocsPerOp: []float64{2, 3, 4}}
		bytesRes, allocsRes, err := CompareSamples(baseline, candidate)
		require.NoError(t, err)
		assert.Equal(t, float64(10), bytesRes.Old)
		assert.Equal(t, float64(12), bytesRes.New)
		assert.InDelta(t, 20.0, bytesRes.DeltaPct, 0.0001)
		assert.Equal(t, 3, bytesRes.N)
		assert.Equal(t, float64(2), allocsRes.Old)
		assert.Equal(t, float64(3), allocsRes.New)
		assert.InDelta(t, 50.0, allocsRes.DeltaPct, 0.0001)
		assert.Equal(t, 3, allocsRes.N)
	})

	t.Run("independent sample counts per axis", func(t *testing.T) {
		t.Parallel()

		baseline := SampleMetrics{BytesPerOp: []float64{8, 12}, AllocsPerOp: []float64{1}}
		candidate := SampleMetrics{BytesPerOp: []float64{10, 12, 14}, AllocsPerOp: []float64{1}}
		bytesRes, allocsRes, err := CompareSamples(baseline, candidate)
		require.NoError(t, err)
		assert.Equal(t, 3, bytesRes.N, "bytes N follows the bytes sample count")
		assert.Equal(t, 1, allocsRes.N, "allocs N follows the allocs sample count")
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
