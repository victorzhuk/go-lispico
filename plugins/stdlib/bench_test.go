package stdlib

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func mergeGoFunc(tb testing.TB, env *core.Env) core.GoFunc {
	tb.Helper()
	fnVal, ok := env.Get("merge")
	if !ok {
		tb.Fatal("merge not registered")
	}
	gfn, ok := fnVal.(core.GoFunc)
	if !ok {
		tb.Fatalf("merge is not a GoFunc: %T", fnVal)
	}
	return gfn
}

// mergeKey uses a Keyword rather than an Int so toHashKey reuses the string
// field directly — Int keys route through strconv.FormatInt, which is
// alloc-free for small values (Go's smalls-table cache) but not for larger
// ones, skewing allocation counts across benchmark sizes for reasons
// unrelated to merge's own complexity.
func mergeKey(i int) core.Keyword {
	return core.Keyword{V: fmt.Sprintf("k%d", i)}
}

func buildMergeArgs(tb testing.TB, n int) []core.Value {
	tb.Helper()
	half := n / 2
	m1 := core.NewHashMap()
	for i := range half {
		if err := m1.Set(mergeKey(i), core.Int{V: int64(i)}); err != nil {
			tb.Fatalf("build m1: %v", err)
		}
	}
	m2 := core.NewHashMap()
	for i := half; i < n; i++ {
		if err := m2.Set(mergeKey(i), core.Int{V: int64(i)}); err != nil {
			tb.Fatalf("build m2: %v", err)
		}
	}
	return []core.Value{m1, m2}
}

func BenchmarkMerge(b *testing.B) {
	for _, n := range []int{10, 100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			env := core.NewEnv(nil)
			if err := New().Init(env); err != nil {
				b.Fatalf("init stdlib: %v", err)
			}
			gfn := mergeGoFunc(b, env)
			args := buildMergeArgs(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := gfn.Fn(context.Background(), nil, args, env); err != nil {
					b.Fatalf("merge: %v", err)
				}
			}
		})
	}
}

func TestMerge_LinearGrowth(t *testing.T) {
	env := setupEnv(t)
	gfn := mergeGoFunc(t, env)

	allocsFor := func(n int) float64 {
		args := buildMergeArgs(t, n)
		return testing.AllocsPerRun(5, func() {
			_, err := gfn.Fn(context.Background(), nil, args, env)
			require.NoError(t, err)
		})
	}

	// bytesFor is the primary signal: allocation count is nearly blind to
	// the O(n^2) byte-copying pathology this test guards against (an old
	// entry-by-entry bulk-builder rebuild copies the whole map on every
	// insert, so bytes blow up long before the alloc count does).
	bytesFor := func(n int) float64 {
		args := buildMergeArgs(t, n)
		const iterations = 20
		old := debug.SetGCPercent(-1)
		defer debug.SetGCPercent(old)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range iterations {
			_, err := gfn.Fn(context.Background(), nil, args, env)
			require.NoError(t, err)
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / iterations
	}

	allocsSmall, allocsLarge := allocsFor(100), allocsFor(1000)
	if ratio := allocsLarge / allocsSmall; ratio >= 20 {
		t.Errorf("merge allocation count grows superlinearly: allocs(100)=%.0f allocs(1000)=%.0f ratio=%.1f", allocsSmall, allocsLarge, ratio)
	}

	bytesSmall, bytesLarge := bytesFor(100), bytesFor(1000)
	if ratio := bytesLarge / bytesSmall; ratio >= 40 {
		t.Errorf("merge bytes allocated grow superlinearly: bytes(100)=%.0f bytes(1000)=%.0f ratio=%.1f", bytesSmall, bytesLarge, ratio)
	}
}
