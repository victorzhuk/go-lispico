package perfgate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// longerBenchtimeProfileDir is the 400ms rerun (its README.md records 57092
// iterations against the pinned profile's 28188). B/op is a total divided by
// an iteration count, so twice the iterations halves the rounding granularity:
// this profile is a directional check on the pinned one, never an allowance
// source. GoldsetCall/call-boundary is absent from it.
const longerBenchtimeProfileDir = "testdata/profile-30630796967"

// bytesAllowanceExemptCell is the one cell whose allowance is not sized from
// within-run spread: its spread is 0 and its 4 B/op covers a reproducible
// between-engine offset licensed by ADR 0008 and three named hosted runs.
const bytesAllowanceExemptCell = "Goldset/guard-nil"

// TestBytesAllowancesAreJustifiedBySpread bounds every stated bytes allowance
// by the within-run B/op spread the gate's own benchtime measures. The bound
// is what makes an allowance structurally unable to admit a regression: a
// regression moves the median while leaving the spread where it was. Only the
// 200ms profile counts -- a spread taken at a different benchtime, or a
// difference taken between two profiles, blends code change, benchtime and
// hardware and presents the result as noise.
func TestBytesAllowancesAreJustifiedBySpread(t *testing.T) {
	t.Parallel()

	spread := benchVMBytesSpread(t, pinnedProfileDir)
	file := readTierConfigFile(t)

	var unjustified []string
	for name := range file.Cells {
		if name == bytesAllowanceExemptCell {
			continue
		}
		measured, ok := spread[name]
		require.True(t, ok, "cell %q has no B/op samples in %s", name, pinnedProfileDir)

		stated, ok := file.BytesAllowanceBOp[name]
		switch {
		case !ok:
			unjustified = append(unjustified, fmt.Sprintf("%s: states no allowance, measured spread %g B/op", name, measured))
		case stated > measured:
			unjustified = append(unjustified, fmt.Sprintf("%s: states %g B/op against a measured spread of %g B/op", name, stated, measured))
		}
	}
	sort.Strings(unjustified)

	assert.Empty(t, unjustified,
		"every bytes allowance must be sized from the within-run spread profile-%s measures on that cell", pinnedProfileRunID)
}

// TestTierConfig_EveryCellStatesAnAllowance requires the allowance to be
// stated rather than inferred from absence. A missing key and a stated 0 reach
// nonIncreasing as the same zero value, so only the file distinguishes a cell
// whose exact non-increasing bound was decided from one nobody looked at.
func TestTierConfig_EveryCellStatesAnAllowance(t *testing.T) {
	t.Parallel()

	file := readTierConfigFile(t)

	var missing []string
	for name := range file.Cells {
		if _, ok := file.BytesAllowanceBOp[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	assert.Empty(t, missing, "every cell in tiers.json must state a bytesAllowanceBOp entry, 0 included")
}

// TestBytesAllowanceSpread_LongerBenchtimeIsTighter is why the pinned 200ms
// profile is the evidence base and the 400ms rerun is only a check: a longer
// benchtime cannot widen a cell's B/op spread, so an allowance sized from the
// rerun would understate what the gate actually sees.
func TestBytesAllowanceSpread_LongerBenchtimeIsTighter(t *testing.T) {
	t.Parallel()

	gate := benchVMBytesSpread(t, pinnedProfileDir)
	longer := benchVMBytesSpread(t, longerBenchtimeProfileDir)

	var wider []string
	for name, gateSpread := range gate {
		longerSpread, ok := longer[name]
		if !ok {
			continue
		}
		if longerSpread > gateSpread {
			wider = append(wider, fmt.Sprintf("%s: %g B/op at 400ms against %g B/op at 200ms", name, longerSpread, gateSpread))
		}
	}
	sort.Strings(wider)

	assert.Empty(t, wider, "the 400ms rerun must not read a wider B/op spread than the 200ms profile the gate is sized from")
}

// benchVMBytesSpread reads one profile's committed VM benchmark output and
// returns each cell's within-run B/op span, keyed the way tiers.json names its
// cells. The samples come from ReadBenchmarkMetrics so the spread and the
// gate's own medians are derived from one reader.
func benchVMBytesSpread(t *testing.T, profileDir string) map[string]float64 {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(profileDir, "bench-vm.txt"))
	require.NoError(t, err)

	metrics, err := ReadBenchmarkMetrics(bytes.NewReader(data))
	require.NoError(t, err)
	require.NotEmpty(t, metrics, "%s carries no benchmark samples", profileDir)

	spread := make(map[string]float64, len(metrics))
	for name, m := range metrics {
		require.NotEmpty(t, m.BytesPerOp, "cell %q in %s carries no B/op samples", name, profileDir)
		spread[TrimProcsSuffix(name)] = slices.Max(m.BytesPerOp) - slices.Min(m.BytesPerOp)
	}
	return spread
}

// readTierConfigFile reads tiers.json through the unexported file shape rather
// than LoadTierConfig: the public loader folds a missing allowance and a
// stated 0 into the same zero value, and telling those apart is the point here.
func readTierConfigFile(t *testing.T) tierConfigFile {
	t.Helper()

	data, err := os.ReadFile("tiers.json")
	require.NoError(t, err)

	var file tierConfigFile
	require.NoError(t, json.Unmarshal(data, &file))
	require.NotEmpty(t, file.Cells)
	return file
}
