package perfgate

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

// SampleMetrics holds one benchmark cell's exact per-op counts, one entry per
// appended sample run. The per-sample slices are exported rather than only a
// median so a later consumer reaches them without a second parser.
type SampleMetrics struct {
	Name        string
	BytesPerOp  []float64
	AllocsPerOp []float64
}

// MedianBytesPerOp is the figure a cell's bytes verdict is judged on. It must
// follow benchstat's own median convention: the two files are otherwise judged
// on numbers benchstat produced, and two conventions would disagree silently.
func (m SampleMetrics) MedianBytesPerOp() float64 { return median(m.BytesPerOp) }

// MedianAllocsPerOp is the allocs/op counterpart of MedianBytesPerOp.
func (m SampleMetrics) MedianAllocsPerOp() float64 { return median(m.AllocsPerOp) }

// median follows benchstat: over an even sample set it is the mean of the two
// middle values, not either one of them.
func median(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// ReadBenchmarkMetrics reads raw `go test -bench` output, keyed the way
// ParseBenchstatCSV keys its map: the "Benchmark" prefix stripped, the
// -<GOMAXPROCS> suffix kept. Sample counts may differ between two files, so
// nothing here may assume the two sides carry the same number of runs.
func ReadBenchmarkMetrics(r io.Reader) (map[string]SampleMetrics, error) {
	metrics := make(map[string]SampleMetrics)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		// A result line is "Benchmark<name> <iterations> <value> <unit>...";
		// everything shorter is preamble, a PASS/ok trailer, or a bare name
		// line from a benchmark that produced no samples.
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		name := strings.TrimPrefix(fields[0], "Benchmark")
		if name == "" {
			continue
		}

		m := metrics[name]
		m.Name = name
		for i := 2; i+1 < len(fields); i += 2 {
			unit := fields[i+1]
			if unit != "B/op" && unit != "allocs/op" {
				continue
			}
			value, err := strconv.ParseFloat(fields[i], 64)
			if err != nil {
				return nil, fmt.Errorf("perfgate: cell %q: parse %s value %q: %w", name, unit, fields[i], err)
			}
			if unit == "B/op" {
				m.BytesPerOp = append(m.BytesPerOp, value)
			} else {
				m.AllocsPerOp = append(m.AllocsPerOp, value)
			}
		}
		metrics[name] = m
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("perfgate: read benchmark metrics: %w", err)
	}
	return metrics, nil
}

// CompareSamples derives one cell's bytes and allocs comparison from the
// baseline and candidate sample sets. Byte and allocation counts are exact, so
// every derived result is significant; a zero DeltaPct on a moved median would
// pass nonIncreasing unconditionally. A zero baseline median yields +Inf on a
// positive candidate rather than a division by zero.
func CompareSamples(baseline, candidate SampleMetrics) (bytes, allocs MetricResult) {
	bytes = compareMedians(baseline.MedianBytesPerOp(), candidate.MedianBytesPerOp(), len(candidate.BytesPerOp))
	allocs = compareMedians(baseline.MedianAllocsPerOp(), candidate.MedianAllocsPerOp(), len(candidate.AllocsPerOp))
	return bytes, allocs
}

func compareMedians(oldMedian, newMedian float64, n int) MetricResult {
	return MetricResult{
		Old:      oldMedian,
		New:      newMedian,
		DeltaPct: deltaPct(oldMedian, newMedian),
		// nonIncreasing carries no significance gate on these axes, and a
		// false here would still let a moved median through the DeltaPct <= 0
		// shortcut, so it is never derived from the samples.
		Significant: true,
		N:           n,
	}
}

// deltaPct guards the zero baseline: an unchanged zero-byte cell has no
// increase to report, while a first allocation on one is an infinite increase
// and must fail rather than divide into NaN.
func deltaPct(oldMedian, newMedian float64) float64 {
	if oldMedian != 0 {
		return (newMedian - oldMedian) / oldMedian * 100
	}
	if newMedian == 0 {
		return 0
	}
	return math.Inf(1)
}
