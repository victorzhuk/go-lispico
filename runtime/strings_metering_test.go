package runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// formatUsage evaluates (format "%s" d) on a fresh engine holding a payload of
// payloadLen bytes under d, and returns the alloc ledger total. The source is
// identical across runs and d is bound rather than read, so nothing on this
// path scales with the payload except format's own charging.
func formatUsage(t *testing.T, bytecode bool, payloadLen int) int64 {
	t.Helper()
	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, 16<<20))
	require.NoError(t, eng.Bind("d", core.String{V: strings.Repeat("x", payloadLen)}))
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 16<<20)
	_, err := eng.Eval(ctx, "format-charge", `(format "%s" d)`)
	require.NoError(t, err)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
}

// TestFormat_ChargedExactlyOnce: format pre-charges its estimate with
// core.ChargeEvalAllocBytes, which does not mark the dispatch as having
// accounted for its own return value, so the apply site bills the finished
// string a second time. Widening the argument moves both charges today; it must
// move one.
//
// The lower bound is the other half of the pair: the pre-charge is what stops
// fmt.Sprintf from running at all under a tight budget, so a total that stopped
// scaling with the argument would mean the guard was removed rather than the
// double charge.
func TestFormat_ChargedExactlyOnce(t *testing.T) {
	skipUntilMeteringFields(t)

	const (
		tinyLen = 1
		wideLen = 4096
	)
	payloadDelta := int64(wideLen - tinyLen)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			tiny := formatUsage(t, bytecode, tinyLen)
			wide := formatUsage(t, bytecode, wideLen)
			delta := wide - tiny

			require.GreaterOrEqualf(t, delta, payloadDelta,
				"(format \"%%s\" <%d bytes>) charged %d against %d for %d byte: the estimate pre-charge must still scale with the argument",
				wideLen, wide, tiny, tinyLen)
			require.Lessf(t, delta, payloadDelta+core.MeterStringHeaderBytes,
				"(format \"%%s\" <%d bytes>) charged %d against %d for %d byte, a delta of %d for a %d-byte argument: the estimate and the apply-site fallback both bill the result, so it is charged twice",
				wideLen, wide, tiny, tinyLen, delta, payloadDelta)
		})
	}
}

// formatEscapedUsage evaluates (format "%s" d) on a fresh engine holding a
// one-element list whose leaf is payloadLen bytes of invalid UTF-8, and returns
// the rendered result alongside the alloc ledger total. The container forces
// toAny's v.String() render, where core.String.String is fmt.Sprintf("%q", V)
// and every invalid byte comes out as \xNN: the render is four times the
// payload that estimateFormatAllocBytes counted.
func formatEscapedUsage(t *testing.T, bytecode bool, payloadLen int) (string, int64) {
	t.Helper()
	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, 16<<20))
	leaf := core.String{V: strings.Repeat("\x80", payloadLen)}
	require.NoError(t, eng.Bind("d", core.NewList([]core.Value{leaf})))
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 16<<20)
	out, err := eng.Eval(ctx, "format-escape-charge", `(format "%s" d)`)
	require.NoError(t, err)
	s, ok := out.(core.String)
	require.Truef(t, ok, "format returned %T, want core.String", out)
	return s.V, core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
}

// TestFormat_EscapedRenderIsChargedOnce is the escaped half of the pair
// TestFormat_ChargedExactlyOnce opens. Its payload renders four times its
// estimate, so the two bounds pull in opposite directions and no single
// arithmetic satisfies both by accident.
//
// The floor is the one that matters: stopping the charge at the estimate is a
// 4x under-bill, and an under-bill on an allocation ledger lets a script
// allocate four bytes for every one it pays for. The ceiling is the same
// double-charge guard the non-escaping test carries, restated where the second
// charge is the larger of the two.
func TestFormat_EscapedRenderIsChargedOnce(t *testing.T) {
	skipUntilMeteringFields(t)

	const (
		tinyLen = 1 << 10
		wideLen = 1 << 16
	)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			tinyOut, tiny := formatEscapedUsage(t, bytecode, tinyLen)
			wideOut, wide := formatEscapedUsage(t, bytecode, wideLen)
			renderDelta := int64(len(wideOut) - len(tinyOut))
			delta := wide - tiny

			require.GreaterOrEqualf(t, delta, renderDelta,
				"(format \"%%s\" <list of %d invalid UTF-8 bytes>) charged %d against %d for %d, a delta of %d for %d rendered bytes: the estimate counts one byte per source byte and the render emits four, so the charge must cover what was materialized",
				wideLen, wide, tiny, tinyLen, delta, renderDelta)
			require.Lessf(t, delta, renderDelta+core.MeterStringHeaderBytes,
				"(format \"%%s\" <list of %d invalid UTF-8 bytes>) charged %d against %d for %d, a delta of %d for %d rendered bytes: the estimate and the apply-site fallback both bill the result, so it is charged twice",
				wideLen, wide, tiny, tinyLen, delta, renderDelta)
		})
	}
}
