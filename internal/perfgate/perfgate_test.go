package perfgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
// the function. Bytes and allocs are held non-increasing on purpose: bytes
// and allocs are now checked before this branch, so a cell failing both
// would report "bytes increased" instead, which is a different case (see
// TestEvaluate_Startup_BytesOrAllocsIncreaseFails).
func TestEvaluate_Startup_Fail_BothPathsExceeded(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModeFirstAuthorization, ModeNonRegression} {
		t.Run(modeName(mode), func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:    "Goldset/rule-load",
				Latency: MetricResult{Old: 0.0001, New: 0.002, DeltaPct: 1900, Significant: true, N: 10},
				Bytes:   MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
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
	assert.Equal(t, TierEngineSensitive, tiers["BenchmarkApply"].Tier)
	assert.Equal(t, TierDataDominated, tiers["BenchmarkFormat"].Tier)
	assert.Zero(t, tiers["BenchmarkFormat"].BytesAllowanceBOp, "a cell absent from bytesAllowanceBOp gets no allowance")
}

func TestLoadTierConfig_UnknownTier(t *testing.T) {
	t.Parallel()

	const config = `{"cells": {"BenchmarkApply": "bogus"}}`

	_, err := LoadTierConfig(strings.NewReader(config))
	require.Error(t, err)
}

// TestLoadTierConfig_BytesAllowance guards tiers.json's bytesAllowanceBOp map:
// a listed cell gets its allowance, an unlisted one defaults to 0.
func TestLoadTierConfig_BytesAllowance(t *testing.T) {
	t.Parallel()

	const config = `{"cells": {"Goldset/guard-nil": "data-dominated", "Goldset/pipeline": "data-dominated"}, "bytesAllowanceBOp": {"Goldset/guard-nil": 4}}`

	tiers, err := LoadTierConfig(strings.NewReader(config))
	require.NoError(t, err)
	assert.Equal(t, 4.0, tiers["Goldset/guard-nil"].BytesAllowanceBOp)
	assert.Zero(t, tiers["Goldset/pipeline"].BytesAllowanceBOp)
}

// TestLoadTierConfig_BytesAllowance_UnknownCell guards against a typo'd
// bytesAllowanceBOp key silently reverting to the exact bound instead of
// surfacing as a load error.
func TestLoadTierConfig_BytesAllowance_UnknownCell(t *testing.T) {
	t.Parallel()

	const config = `{"cells": {"Goldset/guard-nil": "data-dominated"}, "bytesAllowanceBOp": {"Goldset/gaurd-nil": 4}}`

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

// TestEvaluate_Ordering_BytesAllocsBeforeSignificance guards the fix to
// evaluateNonRegression, evaluateWithinTolerance, and evaluateStartup: a
// non-significant latency delta must not hide a real bytes or allocs
// regression behind an INCONCLUSIVE that Resolve later collapses to PASS.
// Before the fix, every case below returned INCONCLUSIVE.
func TestEvaluate_Ordering_BytesAllocsBeforeSignificance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tier Tier
		mode Mode
	}{
		{name: "non-regression", tier: TierDataDominated, mode: ModeNonRegression},
		{name: "within-tolerance", tier: TierDataDominated, mode: ModeFirstAuthorization},
		{name: "startup/first-authorization", tier: TierStartup, mode: ModeFirstAuthorization},
		{name: "startup/non-regression", tier: TierStartup, mode: ModeNonRegression},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/bytes increased", func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:    "Format",
				Latency: MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
				Bytes:   MetricResult{Old: 100, New: 105, DeltaPct: 5, Significant: true, N: 10},
				Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: true, N: 10},
			}
			res := Evaluate(cell, tt.tier, tt.mode)
			assert.Equal(t, VerdictFail, res.Verdict)
			assert.Contains(t, res.Reason, "bytes increased")
		})
		t.Run(tt.name+"/allocs increased", func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:    "Format",
				Latency: MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
				Bytes:   MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: true, N: 10},
				Allocs:  MetricResult{Old: 2, New: 3, DeltaPct: 50, Significant: true, N: 10},
			}
			res := Evaluate(cell, tt.tier, tt.mode)
			assert.Equal(t, VerdictFail, res.Verdict)
			assert.Contains(t, res.Reason, "allocs increased")
		})
	}
}

// TestEvaluate_EngineSensitiveImprovement_AllocsOrdering guards the one
// asymmetric hoist: allocs is checked before the significance gate here, but
// bytes stays below it because bytes on this tier is a 20%-improvement
// floor, not a non-increasing bound — hoisting it would fail "not yet
// significant" outright. The first case pins that deliberate non-hoist:
// without it, a later "make all four evaluators uniform" pass could hoist
// the bytes floor too and silently delete the rerun path for this tier.
func TestEvaluate_EngineSensitiveImprovement_AllocsOrdering(t *testing.T) {
	t.Parallel()

	t.Run("not significant, bytes and allocs both benchstat tilde", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:    "Apply",
			Latency: MetricResult{Old: 100, New: 95, DeltaPct: 0, Significant: false, N: 10},
			Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: false, N: 10},
			Allocs:  MetricResult{Old: 2, New: 2, DeltaPct: 0, Significant: false, N: 10},
		}
		res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
		assert.Equal(t, VerdictInconclusive, res.Verdict)
	})

	t.Run("not significant, allocs increased", func(t *testing.T) {
		t.Parallel()
		cell := CellComparison{
			Name:    "Apply",
			Latency: MetricResult{Old: 100, New: 95, DeltaPct: 0, Significant: false, N: 10},
			Bytes:   MetricResult{Old: 64, New: 64, DeltaPct: 0, Significant: true, N: 10},
			Allocs:  MetricResult{Old: 2, New: 3, DeltaPct: 50, Significant: true, N: 10},
		}
		res := Evaluate(cell, TierEngineSensitive, ModeFirstAuthorization)
		assert.Equal(t, VerdictFail, res.Verdict)
		assert.Contains(t, res.Reason, "allocs increased")
	})
}

// TestEvaluate_BytesAllowance guards the per-cell absolute bytes allowance
// (tiers.json's bytesAllowanceBOp): it is per-cell rather than a tier-wide
// loosening, sized in absolute B/op rather than percentage, and reaches only
// nonIncreasing. Both modes are covered because first-authorization routes
// TierDataDominated to evaluateWithinTolerance and non-regression routes it
// to evaluateNonRegression. The "within allowance" case is Goldset/guard-nil
// exactly: 1128 against 1129 B/op.
func TestEvaluate_BytesAllowance(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModeFirstAuthorization, ModeNonRegression} {
		t.Run(modeName(mode)+"/within allowance", func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:              "Goldset/guard-nil",
				Latency:           MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
				Bytes:             MetricResult{Old: 1128, New: 1129, DeltaPct: 0.09, Significant: true, N: 10},
				Allocs:            MetricResult{Old: 32, New: 32, DeltaPct: 0, Significant: true, N: 10},
				BytesAllowanceBOp: 4,
			}
			res := Evaluate(cell, TierDataDominated, mode)
			assert.Equal(t, VerdictInconclusive, res.Verdict, "got %s", res.Reason)
		})

		t.Run(modeName(mode)+"/past allowance", func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:              "Goldset/guard-nil",
				Latency:           MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
				Bytes:             MetricResult{Old: 1128, New: 1150, DeltaPct: 1.95, Significant: true, N: 10},
				Allocs:            MetricResult{Old: 32, New: 32, DeltaPct: 0, Significant: true, N: 10},
				BytesAllowanceBOp: 4,
			}
			res := Evaluate(cell, TierDataDominated, mode)
			assert.Equal(t, VerdictFail, res.Verdict)
			assert.Contains(t, res.Reason, "bytes increased")
		})

		t.Run(modeName(mode)+"/unlisted cell gets no allowance", func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:    "Goldset/guard-nil",
				Latency: MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
				Bytes:   MetricResult{Old: 1128, New: 1129, DeltaPct: 0.09, Significant: true, N: 10},
				Allocs:  MetricResult{Old: 32, New: 32, DeltaPct: 0, Significant: true, N: 10},
			}
			res := Evaluate(cell, TierDataDominated, mode)
			assert.Equal(t, VerdictFail, res.Verdict)
			assert.Contains(t, res.Reason, "bytes increased by 0.09%")
		})

		t.Run(modeName(mode)+"/allowance does not reach allocs", func(t *testing.T) {
			t.Parallel()
			cell := CellComparison{
				Name:              "Goldset/guard-nil",
				Latency:           MetricResult{Old: 100, New: 100, DeltaPct: 0, Significant: false, N: 10},
				Bytes:             MetricResult{Old: 1128, New: 1129, DeltaPct: 0.09, Significant: true, N: 10},
				Allocs:            MetricResult{Old: 32, New: 33, DeltaPct: 3.13, Significant: true, N: 10},
				BytesAllowanceBOp: 4,
			}
			res := Evaluate(cell, TierDataDominated, mode)
			assert.Equal(t, VerdictFail, res.Verdict)
			assert.Contains(t, res.Reason, "allocs increased")
		})
	}
}

// pinnedProfileRunID and pinnedProfileDir name the checked-in classification
// profile that licenses tiers.json (see its own comment and
// testdata/profile-30630796967/README.md). TestPinnedProfile ties the two
// together: without it, a tier and the profile that licenses it can drift
// apart with only a README as a warning. pinnedProfileDir is built from
// pinnedProfileRunID, and the test separately asserts that same run ID
// appears in tiers.json's own comment -- so replacing this profile with a
// newer one and forgetting to update tiers.json's provenance sentence (or
// the reverse) fails here instead of leaving two pointers to disagree
// silently.
const (
	pinnedProfileRunID = "30637802780"
	pinnedProfileDir   = "testdata/profile-" + pinnedProfileRunID
)

// pinnedBenchEvaluatorSHA256 and pinnedBenchVMSHA256 guard the provenance
// the profile's README asserts ("The benchmark output is committed
// verbatim"): benchstat.csv and verdict.txt are derived from these two raw
// files, but nothing forced them to stay in sync with a silent edit or a
// swap for a different run's files. Update both alongside benchstat.csv and
// verdict.txt when committing a new profile.
const (
	pinnedBenchEvaluatorSHA256 = "51d22b660eff68d34b4e6a55557bd2357b70e02a7efbd9913f42f6e76cb1bd7e"
	pinnedBenchVMSHA256        = "f50713f51da72f016c34846ff882017a86e34872eb6ac8fbef802d175236bb55"
)

// TestPinnedProfile re-evaluates every cell in the committed profile against
// the committed tiers.json and checks the verdict against the committed
// verdict.txt -- the oracle here is that file, not a hardcoded expectation,
// so replacing the profile without updating verdict.txt fails this test
// rather than passing silently. It also pins the two raw benchmark files by
// content digest and cross-checks the profile directory against tiers.json's
// own provenance sentence, so a profile swap that only touches the derived
// files (or only one of the two provenance pointers) fails here too.
func TestPinnedProfile(t *testing.T) {
	t.Parallel()

	evaluatorData, err := os.ReadFile(filepath.Join(pinnedProfileDir, "bench-evaluator.txt"))
	require.NoError(t, err)
	assert.Equal(t, pinnedBenchEvaluatorSHA256, hashHex(evaluatorData), "bench-evaluator.txt changed since it was pinned")

	vmData, err := os.ReadFile(filepath.Join(pinnedProfileDir, "bench-vm.txt"))
	require.NoError(t, err)
	assert.Equal(t, pinnedBenchVMSHA256, hashHex(vmData), "bench-vm.txt changed since it was pinned")

	csvData, err := os.ReadFile(filepath.Join(pinnedProfileDir, "benchstat.csv"))
	require.NoError(t, err)
	cells, err := ParseBenchstatCSV(csvData)
	require.NoError(t, err)

	tiersData, err := os.ReadFile("tiers.json")
	require.NoError(t, err)
	tiers, err := LoadTierConfig(bytes.NewReader(tiersData))
	require.NoError(t, err)

	var tiersFile tierConfigFile
	require.NoError(t, json.Unmarshal(tiersData, &tiersFile))
	assert.Contains(t, tiersFile.Comment, pinnedProfileRunID,
		"tiers.json's comment no longer names the profile this test pins")

	verdictData, err := os.ReadFile(filepath.Join(pinnedProfileDir, "verdict.txt"))
	require.NoError(t, err)
	wantVerdicts := parsePinnedVerdicts(t, string(verdictData))

	csvNames := make([]string, 0, len(cells))
	for name := range cells {
		csvNames = append(csvNames, TrimProcsSuffix(name))
	}
	tierNames := make([]string, 0, len(tiers))
	for name := range tiers {
		tierNames = append(tierNames, name)
	}
	verdictNames := make([]string, 0, len(wantVerdicts))
	for name := range wantVerdicts {
		verdictNames = append(verdictNames, TrimProcsSuffix(name))
	}
	assert.ElementsMatch(t, csvNames, tierNames, "benchstat.csv and tiers.json disagree on which cells exist")
	assert.ElementsMatch(t, csvNames, verdictNames, "benchstat.csv and verdict.txt disagree on which cells exist")

	for name, cell := range cells {
		ct, ok := tiers[TrimProcsSuffix(name)]
		require.True(t, ok, "no committed tier for cell %q", name)
		want, ok := wantVerdicts[name]
		require.True(t, ok, "no committed verdict for cell %q", name)

		cell.BytesAllowanceBOp = ct.BytesAllowanceBOp
		res := Evaluate(cell, ct.Tier, ModeFirstAuthorization)
		assert.Equal(t, want, res.Verdict, "cell %q (tier %s): got %s, want %s", name, ct.Tier, res.Verdict, want)
	}
}

func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// parsePinnedVerdicts parses cmd/perfgate's report format ("name: VERDICT" or
// "name: VERDICT (reason)") into a map keyed by the full cell name, suffix
// included, matching how ParseBenchstatCSV keys its own map.
func parsePinnedVerdicts(t *testing.T, data string) map[string]Verdict {
	t.Helper()

	verdicts := make(map[string]Verdict)
	for line := range strings.SplitSeq(strings.TrimSpace(data), "\n") {
		name, rest, ok := strings.Cut(line, ": ")
		require.True(t, ok, "verdict line %q: want \"name: VERDICT\"", line)
		if i := strings.Index(rest, " ("); i >= 0 {
			rest = rest[:i]
		}
		verdicts[name] = parseVerdict(t, rest)
	}
	return verdicts
}

func parseVerdict(t *testing.T, s string) Verdict {
	t.Helper()

	switch s {
	case "PASS":
		return VerdictPass
	case "FAIL":
		return VerdictFail
	case "INCONCLUSIVE":
		return VerdictInconclusive
	default:
		t.Fatalf("unknown verdict %q", s)
		return VerdictUnknown
	}
}
