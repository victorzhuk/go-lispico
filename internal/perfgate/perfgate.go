// Package perfgate applies ADR 0008's consumer performance-gate thresholds
// (docs/adr/0008-consumer-performance-gate.md) to a benchstat comparison
// between a reference run and a candidate run. It does not run benchmarks or
// own the per-cell tier assignments — those are YAGEL's corpus and are not
// yet published (see openspec/changes/release-consumer-gate/design.md, "Open
// inputs"); this package only evaluates whatever tiered comparison it is given.
package perfgate

import (
	"fmt"
	"math"
	"time"
)

// Tier is a benchmark cell's committed classification, looked up from
// perfgate/tiers.json before candidate results exist (ADR 0008, "Thresholds").
type Tier string

const (
	TierEngineSensitive Tier = "engine-sensitive"
	TierDataDominated   Tier = "data-dominated"
	TierConcurrent      Tier = "concurrent"
	TierStartup         Tier = "startup"
)

// Mode selects which branch of the first-authorization vs later-release rule
// applies (design.md "Decision rules"). ADR 0008: the 15%/20% improvement
// thresholds apply once; later releases compare the candidate VM against the
// previous release's VM baseline as non-regression instead.
type Mode int

const (
	// ModeUnknown is the zero value: an uninitialized Mode must not silently
	// pick a threshold branch on a release gate.
	ModeUnknown Mode = iota
	ModeFirstAuthorization
	ModeNonRegression
)

// Verdict is a cell's gate outcome.
type Verdict int

const (
	// VerdictUnknown is the zero value: an uninitialized Verdict must not be
	// mistaken for VerdictPass on a release gate.
	VerdictUnknown Verdict = iota
	VerdictPass
	VerdictFail
	VerdictInconclusive
)

func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "PASS"
	case VerdictFail:
		return "FAIL"
	case VerdictInconclusive:
		return "INCONCLUSIVE"
	default:
		return "UNKNOWN"
	}
}

// MetricResult is one benchstat-compared metric (sec/op, B/op, or allocs/op)
// for a cell, old (reference) vs new (candidate).
type MetricResult struct {
	Old         float64
	New         float64
	DeltaPct    float64 // vs base; negative means New is lower than Old
	Significant bool    // false when benchstat reports "~" (p >= alpha)
	PValue      float64
	N           int
}

// CellComparison is one benchmark cell's benchstat comparison, old vs candidate.
type CellComparison struct {
	Name    string
	Latency MetricResult // sec/op. For Concurrent cells this may represent a
	// throughput-style metric instead; only the two-sided tolerance check is
	// applied to Concurrent cells, so the improvement/regression sign
	// convention below does not matter for that tier.
	Bytes  MetricResult // B/op
	Allocs MetricResult // allocs/op

	// RaceClean reports whether the separate untimed -race run was clean for
	// this cell (ADR 0008: "race detector clean in the separate untimed
	// run"). Only TierConcurrent checks it. Zero value is false so an
	// unset field fails closed rather than silently passing a release gate.
	RaceClean bool
}

const (
	// engineSensitiveLatencyImprovementPct is ADR 0008's engine-sensitive
	// floor: at least 15% lower latency to claim first-authorization improvement.
	engineSensitiveLatencyImprovementPct = 15.0
	// engineSensitiveBytesImprovementPct is ADR 0008's engine-sensitive floor:
	// at least 20% fewer allocated bytes.
	engineSensitiveBytesImprovementPct = 20.0
	// nonRegressionTolerancePct is ADR 0008's two-sided bound for
	// data/output-dominated, concurrent, and startup cells, and for
	// engine-sensitive cells once in non-regression mode.
	nonRegressionTolerancePct = 5.0
)

// startupMaxLatency and startupMaxBytes are ADR 0008's absolute-overhead
// alternative for startup/Rule-load cells, so sub-millisecond one-time work
// cannot fail on percentage alone.
const (
	startupMaxLatency = time.Millisecond
	startupMaxBytes   = 256.0 * 1024
)

// Result is a cell's gate verdict from one benchstat comparison attempt.
type Result struct {
	Verdict Verdict
	Reason  string
}

// Evaluate applies ADR 0008's per-tier thresholds to one benchstat
// comparison. A VerdictInconclusive result means the caller should rerun
// this cell once at doubled benchtime and evaluate again; if still
// inconclusive, call Resolve instead of rerunning further.
func Evaluate(cell CellComparison, tier Tier, mode Mode) Result {
	switch tier {
	case TierEngineSensitive:
		if mode == ModeFirstAuthorization {
			return evaluateEngineSensitiveImprovement(cell)
		}
		return evaluateWithinTolerance(cell, nonRegressionTolerancePct)
	case TierDataDominated:
		return evaluateWithinTolerance(cell, nonRegressionTolerancePct)
	case TierConcurrent:
		if !cell.RaceClean {
			return Result{Verdict: VerdictFail, Reason: "race detector run was not clean for this cell"}
		}
		return evaluateWithinTolerance(cell, nonRegressionTolerancePct)
	case TierStartup:
		return evaluateStartup(cell)
	default:
		return Result{Verdict: VerdictFail, Reason: fmt.Sprintf("unknown tier %q", tier)}
	}
}

// Resolve collapses a still-inconclusive verdict after the one allowed
// rerun (design.md "Decision rules"; ADR 0008's burden-of-proof rule):
// improvement claims fail when unproven, non-regression claims pass when
// unrefuted. The engine-sensitive tier under first-authorization mode is the
// only improvement claim; every other tier/mode combination is non-regression.
func Resolve(tier Tier, mode Mode) Verdict {
	if tier == TierEngineSensitive && mode == ModeFirstAuthorization {
		return VerdictFail
	}
	return VerdictPass
}

func evaluateEngineSensitiveImprovement(cell CellComparison) Result {
	if !cell.Latency.Significant {
		return Result{Verdict: VerdictInconclusive, Reason: "latency improvement not statistically significant"}
	}
	if cell.Latency.DeltaPct > -engineSensitiveLatencyImprovementPct {
		return Result{Verdict: VerdictFail, Reason: fmt.Sprintf(
			"latency improved %.2f%%, need at least %.0f%% lower", -cell.Latency.DeltaPct, engineSensitiveLatencyImprovementPct)}
	}
	if cell.Bytes.DeltaPct > -engineSensitiveBytesImprovementPct {
		return Result{Verdict: VerdictFail, Reason: fmt.Sprintf(
			"bytes improved %.2f%%, need at least %.0f%% fewer", -cell.Bytes.DeltaPct, engineSensitiveBytesImprovementPct)}
	}
	if r := nonIncreasing(cell.Allocs, "allocs"); r.Verdict != VerdictPass {
		return r
	}
	return Result{Verdict: VerdictPass}
}

func evaluateWithinTolerance(cell CellComparison, tolerancePct float64) Result {
	if !cell.Latency.Significant {
		return Result{Verdict: VerdictInconclusive, Reason: "latency delta not statistically significant"}
	}
	if math.Abs(cell.Latency.DeltaPct) > tolerancePct {
		return Result{Verdict: VerdictFail, Reason: fmt.Sprintf(
			"latency delta %.2f%% exceeds %.0f%% tolerance", cell.Latency.DeltaPct, tolerancePct)}
	}
	if r := nonIncreasing(cell.Bytes, "bytes"); r.Verdict != VerdictPass {
		return r
	}
	if r := nonIncreasing(cell.Allocs, "allocs"); r.Verdict != VerdictPass {
		return r
	}
	return Result{Verdict: VerdictPass}
}

func evaluateStartup(cell CellComparison) Result {
	// ADR 0008's "at most 1 ms and 256 KiB absolute overhead" is ambiguous
	// between an absolute New value under the floor (what's implemented
	// below) and an absolute delta (New-Old) under the floor. Worth an
	// ADR/design clarification.
	if !cell.Latency.Significant {
		return Result{Verdict: VerdictInconclusive, Reason: "latency delta not statistically significant"}
	}
	withinTolerance := math.Abs(cell.Latency.DeltaPct) <= nonRegressionTolerancePct
	withinAbsoluteBound := cell.Latency.New <= startupMaxLatency.Seconds() && cell.Bytes.New <= startupMaxBytes
	if !withinTolerance && !withinAbsoluteBound {
		return Result{Verdict: VerdictFail, Reason: fmt.Sprintf(
			"latency delta %.2f%% exceeds %.0f%% tolerance and absolute overhead %.3fms/%.0fB exceeds %s/%.0fKiB",
			cell.Latency.DeltaPct, nonRegressionTolerancePct,
			cell.Latency.New*1000, cell.Bytes.New,
			startupMaxLatency, startupMaxBytes/1024)}
	}
	return Result{Verdict: VerdictPass}
}

// nonIncreasing enforces ADR 0008's "allocation count non-increasing" and
// "bytes ... non-increasing" bounds. These are exact counts in Go's
// benchmark output, not sampled statistics, so unlike latency they carry no
// significance gate here.
func nonIncreasing(m MetricResult, label string) Result {
	if m.DeltaPct > 0 {
		return Result{Verdict: VerdictFail, Reason: fmt.Sprintf("%s increased by %.2f%%", label, m.DeltaPct)}
	}
	return Result{Verdict: VerdictPass}
}
