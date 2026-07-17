package perfgate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate_Pass_EngineSensitive(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Apply",
		Latency: MetricResult{Old: 100, New: 80, DeltaPct: -20, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 48, DeltaPct: -25, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}

	res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
	assert.Equal(t, VerdictPass, res.Verdict)
}

func TestEvaluate_EngineSensitive_Inconclusive_NotSignificant(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Apply",
		Latency: MetricResult{Old: 100, New: 90, DeltaPct: -10, Significant: false, N: 10},
		Bytes:   MetricResult{Old: 64, New: 48, DeltaPct: -25, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}

	res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
	assert.Equal(t, VerdictInconclusive, res.Verdict)
}

func TestEvaluate_EngineSensitive_Fail_LatencyBelowThreshold(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Apply",
		Latency: MetricResult{Old: 100, New: 90, DeltaPct: -10, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 48, DeltaPct: -25, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}

	res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
	assert.Equal(t, VerdictFail, res.Verdict)
	assert.Contains(t, res.Reason, "latency improved 10.00%, need at least 15% lower")
}

func TestEvaluate_EngineSensitive_Fail_BytesBelowThreshold(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Apply",
		Latency: MetricResult{Old: 100, New: 80, DeltaPct: -20, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 60, DeltaPct: -6.25, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}

	res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
	assert.Equal(t, VerdictFail, res.Verdict)
	assert.Contains(t, res.Reason, "bytes improved 6.25%, need at least 20% fewer")
}

func TestEvaluate_EngineSensitive_Fail_AllocsIncreased(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Apply",
		Latency: MetricResult{Old: 100, New: 80, DeltaPct: -20, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 48, DeltaPct: -25, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 3, DeltaPct: 50, Significant: true, N: 10},
	}

	res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
	assert.Equal(t, VerdictFail, res.Verdict)
	assert.Contains(t, res.Reason, "allocs increased by 50.00%")
}

func TestEvaluate_Fail_DataDominated(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Format",
		Latency: MetricResult{Old: 100, New: 108, DeltaPct: 8, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}

	res := Evaluate(cell, TierDataDominated, ModeNonRegression)
	assert.Equal(t, VerdictFail, res.Verdict)
	assert.Contains(t, res.Reason, "8.00%")
}

func TestEvaluate_InconclusiveThenResolvedOnRerun(t *testing.T) {
	t.Parallel()

	firstAttempt := CellComparison{
		Name:    "Format",
		Latency: MetricResult{Old: 100, New: 98, Significant: false, N: 10},
		Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}
	res := Evaluate(firstAttempt, TierDataDominated, ModeNonRegression)
	require.Equal(t, VerdictInconclusive, res.Verdict)

	rerunAttempt := CellComparison{
		Name:    "Format",
		Latency: MetricResult{Old: 100, New: 97, DeltaPct: -3, Significant: true, N: 20},
		Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 20},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 20},
	}
	res = Evaluate(rerunAttempt, TierDataDominated, ModeNonRegression)
	assert.Equal(t, VerdictPass, res.Verdict)
}

func TestResolve_AfterRerun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tier Tier
		mode Mode
		want Verdict
	}{
		{
			name: "improvement tier fails when still unproven",
			tier: TierEngineSensitive,
			mode: ModeFirstAuthorization,
			want: VerdictFail,
		},
		{
			name: "non-regression tier passes when still unrefuted",
			tier: TierDataDominated,
			mode: ModeNonRegression,
			want: VerdictPass,
		},
		{
			name: "engine-sensitive tier in non-regression mode passes when still unrefuted",
			tier: TierEngineSensitive,
			mode: ModeNonRegression,
			want: VerdictPass,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Resolve(tt.tier, tt.mode))
		})
	}
}

func TestEvaluate_Concurrent_RaceNotClean(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:      "Dispatch",
		Latency:   MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
		Bytes:     MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 10},
		Allocs:    MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
		RaceClean: false,
	}
	res := Evaluate(cell, TierConcurrent, ModeNonRegression)
	assert.Equal(t, VerdictFail, res.Verdict)
}

func TestEvaluate_Startup_AbsoluteOverheadException(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "LoadRules",
		Latency: MetricResult{Old: 0.0001, New: 0.0006, DeltaPct: 500, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}
	res := Evaluate(cell, TierStartup, ModeNonRegression)
	assert.Equal(t, VerdictPass, res.Verdict)
}

func TestParseBenchstatCSV(t *testing.T) {
	t.Parallel()

	const csv = `goos: linux
goarch: amd64
,old.txt,,new.txt,,,
,sec/op,CI,sec/op,CI,vs base,P
Apply-8,1.0005e-07,1%,8.005e-08,1%,-19.99%,p=0.000 n=10
geomean,1.0004999999999995e-07,,8.004999999999987e-08,,-19.99%,

,old.txt,,new.txt,,,
,B/op,CI,B/op,CI,vs base,P
Apply-8,64,0%,48,0%,-25.00%,p=0.000 n=10
geomean,63.99999999999998,,47.999999999999986,,-25.00%,

,old.txt,,new.txt,,,
,allocs/op,CI,allocs/op,CI,vs base,P
Apply-8,2,0%,2,0%,~,p=1.000 n=10
geomean,2,,2,,+0.00%,
`

	cells, err := ParseBenchstatCSV([]byte(csv))
	require.NoError(t, err)
	require.Contains(t, cells, "Apply-8")

	cell := cells["Apply-8"]
	assert.InDelta(t, -19.99, cell.Latency.DeltaPct, 0.001)
	assert.True(t, cell.Latency.Significant)
	assert.InDelta(t, -25.00, cell.Bytes.DeltaPct, 0.001)
	assert.False(t, cell.Allocs.Significant)
}

func TestLoadTierConfig(t *testing.T) {
	t.Parallel()

	const config = `{"comment": "placeholder", "cells": {"BenchmarkApply": "engine-sensitive", "BenchmarkFormat": "data-dominated"}}`

	tiers, err := LoadTierConfig(strings.NewReader(config))
	require.NoError(t, err)
	assert.Equal(t, TierEngineSensitive, tiers["BenchmarkApply"])
	assert.Equal(t, TierDataDominated, tiers["BenchmarkFormat"])
}

func TestLoadTierConfig_UnknownTier(t *testing.T) {
	t.Parallel()

	const config = `{"cells": {"BenchmarkApply": "bogus"}}`

	_, err := LoadTierConfig(strings.NewReader(config))
	require.Error(t, err)
}
