package runtime

import (
	goruntime "runtime"
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
// payload that estimateFormatAllocBytesContext counted.
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

// stringsRejectionBudget is the allocation ceiling the product-shaped
// rejections below run under: wide enough that evaluating the bound operands
// cannot exhaust it on its own, and two orders of magnitude under the output
// either builtin produces, so a builtin that sizes its output before building
// it has to reject at the pre-charge.
const stringsRejectionBudget = 1 << 20

// preallocTolerance is the share of that output a rejection may still allocate:
// one 128th. Building the output is the whole thing a pre-charge exists to
// prevent, so what a charging builtin allocates is its operands plus the reader
// and the compiler, while one that charges afterwards allocates the product.
const preallocTolerance = 128

// stringsRejectionAlloc evaluates src on a fresh engine under
// stringsRejectionBudget, with bind having placed the operands first, and
// returns the process allocation the evaluation itself cost.
// runtime.MemStats.TotalAlloc is cumulative and unaffected by GC, so the delta
// is what this Eval materialized rather than what survived it.
func stringsRejectionAlloc(t *testing.T, bytecode bool, src string, bind func(Engine)) (uint64, error) {
	t.Helper()
	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, stringsRejectionBudget))
	bind(eng)
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, stringsRejectionBudget)

	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	_, err := eng.Eval(ctx, "strings-prealloc", src)
	goruntime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc, err
}

// TestStrings_ReplaceRejectsBeforeAllocating: an empty old makes
// strings.ReplaceAll insert the replacement before every rune of the subject
// and once more at the end, so string/replace's output is the PRODUCT of two
// operands, not a maximum over them - 8192 subject bytes and 16384 replacement
// bytes produce 134242304, from operands the ledger sized at 24 KB.
//
// The ledger fails closed either way, so the returned error proves nothing on
// its own: it is already returned today. What must change is when the charge
// lands. format estimates its output and charges before fmt.Sprintf runs, which
// is why TestStrings_FormatChargesEstimatedOutputBeforeSprintf can measure it
// from the allocation side; string/replace charges after ReplaceAll has already
// built the buffer, so the allocation delta is the discriminator.
func TestStrings_ReplaceRejectsBeforeAllocating(t *testing.T) {
	skipUntilMeteringFields(t)

	const (
		subjectLen = 8192
		newLen     = 16384
	)
	outputBytes := uint64(subjectLen + (subjectLen+1)*newLen)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			delta, err := stringsRejectionAlloc(t, bytecode, `(string/replace s "" r)`, func(e Engine) {
				require.NoError(t, e.Bind("s", core.String{V: strings.Repeat("x", subjectLen)}))
				require.NoError(t, e.Bind("r", core.String{V: strings.Repeat("y", newLen)}))
			})
			require.Truef(t, isResourceLimit(t, err),
				"(string/replace <%d bytes> \"\" <%d bytes>) under a %d-byte budget must fail with %s, got %v",
				subjectLen, newLen, stringsRejectionBudget, core.CodeResourceLimit, err)
			require.Truef(t, core.IsTerminalEvalError(err),
				"(string/replace <%d bytes> \"\" <%d bytes>) must fail terminally, got %v", subjectLen, newLen, err)
			require.Lessf(t, delta, outputBytes/preallocTolerance,
				"(string/replace <%d bytes> \"\" <%d bytes>) allocated %d bytes under a %d-byte budget before returning %v: the output is %d bytes, %dx the budget, so the charge must be computed from the operands and land before strings.ReplaceAll builds it",
				subjectLen, newLen, delta, stringsRejectionBudget, err, outputBytes, outputBytes/stringsRejectionBudget)
		})
	}
}

// TestStrings_JoinRejectsBeforeAllocating is the same defect at the other
// product-shaped phase: strings.Join writes the separator between every part,
// so 2048 empty parts and a 65536-byte separator produce 134152192 bytes from
// operands the ledger sized at 98 KB. The parts loop already Steps once per
// element, so the sum the pre-charge needs is one pass it is making anyway.
func TestStrings_JoinRejectsBeforeAllocating(t *testing.T) {
	skipUntilMeteringFields(t)

	const (
		sepLen   = 65536
		partsLen = 2048
	)
	outputBytes := uint64((partsLen - 1) * sepLen)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			delta, err := stringsRejectionAlloc(t, bytecode, `(string/join sep ps)`, func(e Engine) {
				require.NoError(t, e.Bind("sep", core.String{V: strings.Repeat("s", sepLen)}))
				items := make([]core.Value, partsLen)
				for i := range items {
					items[i] = core.String{V: ""}
				}
				require.NoError(t, e.Bind("ps", core.NewList(items)))
			})
			require.Truef(t, isResourceLimit(t, err),
				"(string/join <%d-byte separator> <%d empty parts>) under a %d-byte budget must fail with %s, got %v",
				sepLen, partsLen, stringsRejectionBudget, core.CodeResourceLimit, err)
			require.Truef(t, core.IsTerminalEvalError(err),
				"(string/join <%d-byte separator> <%d empty parts>) must fail terminally, got %v", sepLen, partsLen, err)
			require.Lessf(t, delta, outputBytes/preallocTolerance,
				"(string/join <%d-byte separator> <%d empty parts>) allocated %d bytes under a %d-byte budget before returning %v: the output is %d bytes, %dx the budget, so the charge must be computed from the parts and land before strings.Join builds it",
				sepLen, partsLen, delta, stringsRejectionBudget, err, outputBytes, outputBytes/stringsRejectionBudget)
		})
	}
}

// overflowFormatSrc is one expression that saturates format's own estimate. The
// literal width is past what fmt will parse, so fmt abandons the verb and
// renders a few dozen bytes while parseFormatInt pins the estimate at its
// ceiling. Charging that estimate wraps the allocation ledger negative, and the
// ledger admits the charge because it compares after adding.
const overflowFormatSrc = `(format "%9223372036854775807d" 1)`

// overflowBudget is the allocation ceiling the cases below run under. The
// render costs a few dozen bytes, so the whole ceiling is still there for
// whatever the expression does next - which is the point: what must survive the
// saturated estimate is the limit itself.
const overflowBudget = 1 << 20

// overflowLedger evaluates src on a fresh engine under overflowBudget, with
// bind having placed any operands first, and returns the alloc ledger total
// alongside the evaluation's error.
func overflowLedger(t *testing.T, bytecode bool, src string, bind func(Engine)) (int64, error) {
	t.Helper()
	eng := newMeteringStdlibEngine(t, bytecode, meteringLimits(t, 1_000_000, overflowBudget))
	bind(eng)
	ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, overflowBudget)
	_, err := eng.Eval(ctx, "format-overflow", src)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes, err
}

// TestFormat_SaturatedEstimateLeavesLedgerPositive is the single-expression
// half: one format call must not be able to put the ledger somewhere no total
// can reach. A negative used is not a budget.
func TestFormat_SaturatedEstimateLeavesLedgerPositive(t *testing.T) {
	skipUntilMeteringFields(t)

	for _, bytecode := range []bool{false, true} {
		t.Run(evalModeName(bytecode), func(t *testing.T) {
			alloc, err := overflowLedger(t, bytecode, overflowFormatSrc, func(Engine) {})
			require.NoError(t, err)
			require.Greaterf(t, alloc, int64(0),
				"%s left the allocation ledger at %d under a %d-byte budget: the estimate saturated and the counter wrapped, so every later allocation in this evaluation is compared against a negative total",
				overflowFormatSrc, alloc, overflowBudget)
			require.LessOrEqualf(t, alloc, int64(overflowBudget),
				"%s charged %d against a %d-byte budget it did not exceed: the render is a few dozen bytes, so the estimate must track it",
				overflowFormatSrc, alloc, overflowBudget)
		})
	}
}

// TestFormat_SaturatedEstimateDoesNotDisarmLaterCharges is the half that makes
// it a bypass rather than a bookkeeping slip: both product-shaped builtins the
// sealed pre-charge tests pin are refused today only because the ledger is
// honest, and one preceding format call in the same evaluation is enough to
// take that away.
func TestFormat_SaturatedEstimateDoesNotDisarmLaterCharges(t *testing.T) {
	skipUntilMeteringFields(t)

	const (
		subjectLen = 8192
		newLen     = 16384
		sepLen     = 65536
		partsLen   = 2048
	)

	for _, tt := range []struct {
		name string
		src  string
		bind func(Engine)
	}{
		{
			name: "replace",
			src:  `(do ` + overflowFormatSrc + ` (string/replace s "" r))`,
			bind: func(e Engine) {
				require.NoError(t, e.Bind("s", core.String{V: strings.Repeat("x", subjectLen)}))
				require.NoError(t, e.Bind("r", core.String{V: strings.Repeat("y", newLen)}))
			},
		},
		{
			name: "join",
			src:  `(do ` + overflowFormatSrc + ` (string/join sep ps))`,
			bind: func(e Engine) {
				require.NoError(t, e.Bind("sep", core.String{V: strings.Repeat("s", sepLen)}))
				items := make([]core.Value, partsLen)
				for i := range items {
					items[i] = core.String{V: ""}
				}
				require.NoError(t, e.Bind("ps", core.NewList(items)))
			},
		},
	} {
		for _, bytecode := range []bool{false, true} {
			t.Run(tt.name+"/"+evalModeName(bytecode), func(t *testing.T) {
				alloc, err := overflowLedger(t, bytecode, tt.src, tt.bind)
				require.Truef(t, isResourceLimit(t, err),
					"%s under a %d-byte budget returned %v with the ledger at %d: the format call saturated its estimate and wrapped the counter, so the builtin's pre-charge is measured against a negative total and the limit no longer holds",
					tt.src, overflowBudget, err, alloc)
				require.Greaterf(t, alloc, int64(0),
					"%s left the allocation ledger at %d: a refused charge must leave a total the next charge can still be measured against",
					tt.src, alloc)
			})
		}
	}
}
