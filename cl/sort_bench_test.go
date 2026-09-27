package cl_test

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/cl"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

// BenchmarkCLSortComparisonAllocs records the allocation cost of the CL
// surface sort over a 1000-element permutation. Baseline only: the gate
// established that no optimization is possible because ChildVariadic rest
// NewList retains subslices and arbitrary GoFunc callbacks may retain args.
func BenchmarkCLSortComparisonAllocs(b *testing.B) {
	e, err := runtime.New(nil, runtime.WithDialect(cl.Dialect()))
	require.NoError(b, err)
	b.Cleanup(func() { e.Close() })
	require.NoError(b, e.Use(stdlib.New()))

	ctx := context.Background()
	perm := rand.New(rand.NewSource(1)).Perm(1000)
	lits := make([]string, len(perm))
	for i, v := range perm {
		lits[i] = fmt.Sprint(v)
	}
	_, err = e.Eval(ctx, "cl-sort-bench", "(def xs nil)")
	require.NoError(b, err)
	src := "(setq xs (list " + strings.Join(lits, " ") + "))"
	_, err = e.Eval(ctx, "cl-sort-bench", src)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()
	var res core.Value
	for b.Loop() {
		res, err = e.Eval(ctx, "cl-sort-bench", "(sort xs #'<)")
		require.NoError(b, err)
	}
	b.StopTimer()

	want := make([]int64, len(perm))
	for i := range want {
		want[i] = int64(i)
	}
	assert.True(b, intList(want...).Equals(res), "final (sort xs #'<) result must be sorted, got %v", res)
}
