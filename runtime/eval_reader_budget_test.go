package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
)

// readerBudgetBytes is the allocation ceiling every case here reads under:
// tight enough that the wide sources below overrun it long before their parse
// could finish.
const readerBudgetBytes = 1 << 10

const readerBudgetReductions = 1_000_000

func wideQuotedList(items int) string {
	var b strings.Builder
	b.WriteString("'(")
	for range items {
		b.WriteString("1 ")
	}
	b.WriteString(")")
	return b.String()
}

func newReaderBudgetEngine(t *testing.T) (Engine, ResourceLimits) {
	t.Helper()
	limits := meteringLimits(t, readerBudgetReductions, readerBudgetBytes)
	e, err := New(nil, WithDialect(clojure.Dialect()), WithResourceLimits(limits))
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e, limits
}

func readerBudgetCtx(t *testing.T) context.Context {
	t.Helper()
	return core.WithEvalResourceLimits(t.Context(), readerBudgetReductions, readerBudgetBytes)
}

func admittedBytes(ctx context.Context) int64 {
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
}

func legacyReaderBytes(t *testing.T, src string, maxDepth int) int64 {
	t.Helper()
	_, stats, err := clojure.Dialect().ReadWithMaxDepthStats(src, maxDepth)
	require.NoError(t, err)
	return core.ReaderAllocationBytes(stats)
}

func TestEval_ReaderBudget_Characterization(t *testing.T) {
	t.Parallel()

	t.Run("refuses_before_the_full_parse", func(t *testing.T) {
		eng, limits := newReaderBudgetEngine(t)
		src := wideQuotedList(20000)
		ctx := readerBudgetCtx(t)

		_, err := eng.Eval(ctx, "reader-budget", src)
		require.Error(t, err)
		assert.True(t, isResourceLimit(t, err), "expected ResourceLimitError, got %v", err)

		admitted := admittedBytes(ctx)
		full := legacyReaderBytes(t, src, limits.MaxReaderDepth)
		assert.Less(t, admitted, full,
			"admitted %d bytes under a %d-byte budget: the whole %d-byte parse was built before the refusal",
			admitted, readerBudgetBytes, full)
	})

	t.Run("unread_suffix_does_not_widen_admission", func(t *testing.T) {
		eng, _ := newReaderBudgetEngine(t)
		base := wideQuotedList(20000)
		extended := base + " " + strings.Repeat("1 ", 80000)

		baseCtx := readerBudgetCtx(t)
		_, err := eng.Eval(baseCtx, "reader-budget-base", base)
		require.Error(t, err)

		extendedCtx := readerBudgetCtx(t)
		_, err = eng.Eval(extendedCtx, "reader-budget-extended", extended)
		require.Error(t, err)

		assert.Equal(t, admittedBytes(baseCtx), admittedBytes(extendedCtx),
			"admitted storage must not grow with source the reader never reaches")
	})

	t.Run("cancelled_context_rejects_comment_only_source", func(t *testing.T) {
		eng, _ := newReaderBudgetEngine(t)
		ctx, cancel := context.WithCancel(readerBudgetCtx(t))
		cancel()

		_, err := eng.Eval(ctx, "reader-cancelled", "; only a comment\n")
		assert.ErrorIs(t, err, context.Canceled,
			"an already-cancelled read must report cancellation, got %v", err)
	})

	t.Run("cancellation_precedes_syntax_error", func(t *testing.T) {
		eng, _ := newReaderBudgetEngine(t)
		ctx, cancel := context.WithCancel(readerBudgetCtx(t))
		cancel()

		_, err := eng.Eval(ctx, "reader-cancelled-malformed", "(1 2")
		assert.ErrorIs(t, err, context.Canceled,
			"terminal cancellation must outrank the syntax error, got %v", err)
	})
}
