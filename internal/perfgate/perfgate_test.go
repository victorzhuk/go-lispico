package perfgate

import (
	"fmt"
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

// TestEvaluate_Startup_AbsoluteOverheadException: the absolute "1 ms / 256
// KiB" overhead escape excuses the LATENCY percentage bound only — a cell
// deep under the absolute latency/bytes floor still passes despite a 500%
// latency delta. Bytes and allocs are unchanged (not increased) here on
// purpose: that escape does not extend to the byte/alloc non-increasing
// bounds, which evaluateStartup checks the same way every other tier does.
func TestEvaluate_Startup_AbsoluteOverheadException(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "LoadRules",
		Latency: MetricResult{Old: 0.0001, New: 0.0006, DeltaPct: 500, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
	}
	res := Evaluate(cell, TierStartup, ModeNonRegression)
	assert.Equal(t, VerdictPass, res.Verdict, "got %s", res.Reason)
}

// TestEvaluate_Startup_BytesOrAllocsIncreaseFails covers the non-increasing
// bound on both latency paths evaluateStartup can take before it — a cell
// safely within the 5% latency tolerance, and one that only survives on the
// absolute 1ms/256KiB overhead escape — and under both gate modes (the next
// hosted run is first-authorization; a later release compares non-regression
// once a stored baseline exists). The "absolute-overhead escape" cases are
// the ones that matter: they are the combination the pre-fix evaluateStartup
// got wrong, returning VerdictPass as soon as the escape cleared latency
// without ever checking bytes or allocs.
func TestEvaluate_Startup_BytesOrAllocsIncreaseFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		latency    MetricResult
		bytes      MetricResult
		allocs     MetricResult
		wantReason string
	}{
		{
			name:       "within tolerance, bytes increased",
			latency:    MetricResult{Old: 0.0001, New: 0.0001, DeltaPct: 0, Significant: true, N: 10},
			bytes:      MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			allocs:     MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			wantReason: "bytes increased",
		},
		{
			name:       "within tolerance, allocs increased",
			latency:    MetricResult{Old: 0.0001, New: 0.0001, DeltaPct: 0, Significant: true, N: 10},
			bytes:      MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			allocs:     MetricResult{Old: 2, New: 3, DeltaPct: 50, Significant: true, N: 10},
			wantReason: "allocs increased",
		},
		{
			// Latency delta is 500%, well outside the 5% tolerance, but New
			// (0.6ms/200B) clears the absolute 1ms/256KiB floor, so the
			// escape rescues latency. Bytes still increased 100% — the case
			// Blocker 1 named: the escape must not also rescue allocation.
			name:       "absolute-overhead escape, bytes increased",
			latency:    MetricResult{Old: 0.0001, New: 0.0006, DeltaPct: 500, Significant: true, N: 10},
			bytes:      MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
			allocs:     MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			wantReason: "bytes increased",
		},
		{
			name:       "absolute-overhead escape, allocs increased",
			latency:    MetricResult{Old: 0.0001, New: 0.0006, DeltaPct: 500, Significant: true, N: 10},
			bytes:      MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
			allocs:     MetricResult{Old: 2, New: 3, DeltaPct: 50, Significant: true, N: 10},
			wantReason: "allocs increased",
		},
	}
	for _, tt := range tests {
		for _, mode := range []Mode{ModeFirstAuthorization, ModeNonRegression} {
			t.Run(fmt.Sprintf("%s/%s", tt.name, modeName(mode)), func(t *testing.T) {
				t.Parallel()
				cell := CellComparison{Name: "Goldset/rule-load", Latency: tt.latency, Bytes: tt.bytes, Allocs: tt.allocs}
				res := Evaluate(cell, TierStartup, mode)
				assert.Equal(t, VerdictFail, res.Verdict)
				assert.Contains(t, res.Reason, tt.wantReason)
			})
		}
	}
}

// TestEvaluate_Startup_Fail_BothPathsExceeded covers evaluateStartup's real
// VerdictFail branch — latency exceeds the tolerance AND the absolute
// overhead bound — which had no test on master before this change touched
// the function.
func TestEvaluate_Startup_Fail_BothPathsExceeded(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModeFirstAuthorization, ModeNonRegression} {
		t.Run(modeName(mode), func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:    "Goldset/rule-load",
				Latency: MetricResult{Old: 0.0001, New: 0.002, DeltaPct: 1900, Significant: true, N: 10},
				Bytes:   MetricResult{Old: 100, New: 200, DeltaPct: 100, Significant: true, N: 10},
				Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			}
			res := Evaluate(cell, TierStartup, mode)
			assert.Equal(t, VerdictFail, res.Verdict)
			assert.Contains(t, res.Reason, "exceeds")
			assert.Contains(t, res.Reason, "tolerance")
			assert.Contains(t, res.Reason, "absolute overhead")
		})
	}
}

func modeName(m Mode) string {
	if m == ModeFirstAuthorization {
		return "first-authorization"
	}
	return "non-regression"
}

// TestEvaluate_Startup_NonSignificantByteDeltaPasses documents the known
// blind spot recorded in ADR 0008: benchstat's "~" comes through parse.go as
// DeltaPct=0 with Significant=false, so a real but non-significant byte/alloc
// regression is indistinguishable from no change and passes here like every
// other non-increasing tier.
func TestEvaluate_Startup_NonSignificantByteDeltaPasses(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Goldset/rule-load",
		Latency: MetricResult{Old: 0.0001, New: 0.0001, DeltaPct: 0, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 100, New: 108, DeltaPct: 0, Significant: false, N: 10},
		Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: false, N: 10},
	}
	res := Evaluate(cell, TierStartup, ModeNonRegression)
	assert.Equal(t, VerdictPass, res.Verdict, "got %s", res.Reason)
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

func TestTrimProcsSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "strips procs suffix", in: "Goldset/route-decision-24", want: "Goldset/route-decision"},
		{name: "strips single digit", in: "Apply-8", want: "Apply"},
		{name: "keeps non-numeric tail", in: "Goldset/route-decision", want: "Goldset/route-decision"},
		{name: "keeps trailing dash", in: "Cell-", want: "Cell-"},
		{name: "keeps bare name", in: "Cell", want: "Cell"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, TrimProcsSuffix(tt.in))
		})
	}
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

// TestEvaluate_NonRegression_ImprovementPasses is the case a two-sided latency
// bound got wrong: a release that makes a cell faster must not fail for it.
// ADR 0008 rejected a standing improvement gate because it "punishes Evaluator
// improvements", and a two-sided bound reintroduces exactly that.
func TestEvaluate_NonRegression_ImprovementPasses(t *testing.T) {
	t.Parallel()

	for _, tier := range []Tier{TierEngineSensitive, TierDataDominated} {
		cell := CellComparison{
			Name:    "Goldset/twice-macro",
			Latency: MetricResult{Old: 100, New: 72, DeltaPct: -28.36, Significant: true, N: 10},
			Bytes:   MetricResult{Old: 64, New: 56, DeltaPct: -12.5, Significant: true, N: 10},
			Allocs:  MetricResult{Old: 10, New: 8, DeltaPct: -20, Significant: true, N: 10},
		}
		res := Evaluate(cell, tier, ModeNonRegression)
		assert.Equal(t, VerdictPass, res.Verdict, "tier %s: improvement must pass, got %s", tier, res.Reason)
	}
}

// TestEvaluate_NonRegression_RegressionStillFails: the bound still bites in the
// direction it exists for.
func TestEvaluate_NonRegression_RegressionStillFails(t *testing.T) {
	t.Parallel()

	for _, tier := range []Tier{TierEngineSensitive, TierDataDominated} {
		cell := CellComparison{
			Name:    "Goldset/pipeline",
			Latency: MetricResult{Old: 100, New: 112, DeltaPct: 12, Significant: true, N: 10},
			Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 10},
			Allocs:  MetricResult{Old: 10, New: 10, DeltaPct: 0, Significant: true, N: 10},
		}
		res := Evaluate(cell, tier, ModeNonRegression)
		assert.Equal(t, VerdictFail, res.Verdict, "tier %s: regression must fail", tier)
	}
}

// TestEvaluate_NonRegression_BytesStillOneSided: a faster candidate that
// allocates more still fails. Latency going one-sided must not soften the
// byte and allocation checks, which were already non-increasing.
func TestEvaluate_NonRegression_BytesStillOneSided(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Goldset/pipeline",
		Latency: MetricResult{Old: 100, New: 60, DeltaPct: -40, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 80, DeltaPct: 25, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 10, New: 10, DeltaPct: 0, Significant: true, N: 10},
	}
	res := Evaluate(cell, TierDataDominated, ModeNonRegression)
	assert.Equal(t, VerdictFail, res.Verdict)
	assert.Contains(t, res.Reason, "bytes")
}

// TestEvaluate_FirstAuthorization_DataDominatedStaysTwoSided: comparing the two
// evaluators of one commit, a data-dominated cost is expected to be
// mode-invariant — the reason GoldsetParse/* cells carry this tier — so a move
// in either direction is a finding.
func TestEvaluate_FirstAuthorization_DataDominatedStaysTwoSided(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "GoldsetParse/rule-load",
		Latency: MetricResult{Old: 100, New: 80, DeltaPct: -20, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 10, New: 10, DeltaPct: 0, Significant: true, N: 10},
	}
	res := Evaluate(cell, TierDataDominated, ModeFirstAuthorization)
	assert.Equal(t, VerdictFail, res.Verdict,
		"a mode-invariant cost that moves between modes is a finding either way")
}

// TestEvaluate_Startup_NonRegressionImprovementPasses: a faster start is not a
// regression either.
func TestEvaluate_Startup_NonRegressionImprovementPasses(t *testing.T) {
	t.Parallel()

	cell := CellComparison{
		Name:    "Goldset/rule-load",
		Latency: MetricResult{Old: 0.01, New: 0.006, DeltaPct: -40, Significant: true, N: 10},
		Bytes:   MetricResult{Old: 1 << 20, New: 1 << 20, DeltaPct: 0, Significant: true, N: 10},
		Allocs:  MetricResult{Old: 10, New: 10, DeltaPct: 0, Significant: true, N: 10},
	}
	res := Evaluate(cell, TierStartup, ModeNonRegression)
	assert.Equal(t, VerdictPass, res.Verdict, "got %s", res.Reason)
}
