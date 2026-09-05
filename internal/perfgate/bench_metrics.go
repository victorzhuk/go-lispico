package perfgate

import "io"

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
func (m SampleMetrics) MedianBytesPerOp() float64 { panic("not implemented") }

// MedianAllocsPerOp is the allocs/op counterpart of MedianBytesPerOp.
func (m SampleMetrics) MedianAllocsPerOp() float64 { panic("not implemented") }

// ReadBenchmarkMetrics reads raw `go test -bench` output, keyed the way
// ParseBenchstatCSV keys its map: the "Benchmark" prefix stripped, the
// -<GOMAXPROCS> suffix kept. Sample counts may differ between two files, so
// nothing here may assume the two sides carry the same number of runs.
func ReadBenchmarkMetrics(r io.Reader) (map[string]SampleMetrics, error) { panic("not implemented") }

// CompareSamples derives one cell's bytes and allocs comparison from the
// baseline and candidate sample sets. Byte and allocation counts are exact, so
// every derived result is significant; a zero DeltaPct on a moved median would
// pass nonIncreasing unconditionally. A zero baseline median yields +Inf on a
// positive candidate rather than a division by zero.
func CompareSamples(baseline, candidate SampleMetrics) (bytes, allocs MetricResult) {
	panic("not implemented")
}
