package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// bulkBuiltMap returns a map built past the small-form limit through Set alone,
// so the first update on it runs the builder-to-trie conversion.
func bulkBuiltMap(t *testing.T, n int) *core.HashMap {
	t.Helper()
	m := core.NewHashMap()
	for i := range int64(n) {
		require.NoError(t, m.Set(core.Int{V: i}, core.Int{V: i}))
	}
	return m
}

// TestMetering_RefusedConversionStillSettlesTheCharge pins where the allocation
// ceiling sits relative to the conversion. The builtin runs assoc first and
// charges afterwards, so a refusal lands after the converted trie is already
// published on the shared value: the refusing evaluation is billed for the
// conversion it ran, and the next update on that same value pays only its own
// path rather than converting a second time.
func TestMetering_RefusedConversionStillSettlesTheCharge(t *testing.T) {
	skipUntilMeteringFields(t)

	const (
		n             = 1000
		maxReductions = 10_000_000
		ampleAlloc    = 64 << 20
		afterRefusal  = 8192
	)

	charge := func(t *testing.T, m *core.HashMap, maxAlloc int, source, src string) (int64, error) {
		t.Helper()
		eng := newMeteringStdlibEngine(t, true, meteringLimits(t, maxReductions, maxAlloc))
		require.NoError(t, eng.Bind("shared-map", m))
		ctx := core.WithEvalResourceLimits(t.Context(), maxReductions, maxAlloc)
		_, err := eng.Eval(ctx, source, src)
		return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes, err
	}

	converted, err := charge(t, bulkBuiltMap(t, n), ampleAlloc, "conversion-calibration", "(assoc shared-map :x 1)")
	require.NoError(t, err)
	require.Greater(t, converted, int64(afterRefusal)*8,
		"the calibration must be dominated by the conversion, else the ceiling below does not single it out")

	// Under half the conversion charge and far above everything else the
	// evaluation allocates, so only the conversion can exhaust it.
	ceiling := int(converted / 2)

	refused := bulkBuiltMap(t, n)
	billed, err := charge(t, refused, ceiling, "conversion-refused", "(assoc shared-map :x 1)")
	assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)
	assert.GreaterOrEqual(t, billed, int64(ceiling),
		"the refusing evaluation must be billed for the conversion it ran before the charge was refused")

	after, err := charge(t, refused, ampleAlloc, "conversion-after-refusal", "(assoc shared-map :y 2)")
	require.NoError(t, err)
	assert.LessOrEqual(t, after, int64(afterRefusal),
		"the refused evaluation published the converted trie; the next update must charge only its own path")
}
