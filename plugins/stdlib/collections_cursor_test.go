package stdlib

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// pathKey uses a Keyword for the same reason mergeKey does: toHashKey reuses
// the string field directly instead of routing through strconv.FormatInt,
// whose allocation behaviour varies with the value.
func pathKey(i int) core.Keyword { return core.Keyword{V: fmt.Sprintf("k%d", i)} }

func pathKeys(n int) []core.Value {
	keys := make([]core.Value, n)
	for i := range keys {
		keys[i] = pathKey(i)
	}
	return keys
}

// conjVector grows a Vector one element at a time so it promotes to the trie
// at vectorFlatThreshold; NewVector stays flat at any length.
func conjVector(keys []core.Value) core.Vector {
	var v core.Vector
	for _, k := range keys {
		v, _ = v.Conj(k)
	}
	return v
}

func pathLen(tb testing.TB, path core.Value) int {
	tb.Helper()
	switch p := path.(type) {
	case core.List:
		return p.Len()
	case core.Vector:
		return p.Len()
	default:
		tb.Fatalf("unexpected path type %T", path)
		return 0
	}
}

// TestGetIn_PathCursorDoesNotCopy pins get-in at zero allocations per call, at
// every path length and representation: the cursor advances a List by Rest and
// a Vector by index, so nothing path-sized is built before the walk. Zero is
// the assertion that discriminates — a cursor materializing the path with
// ToSlice costs exactly one allocation, flat in path length, so any cap above
// zero would let it through.
//
// The budget bounds allocation count only. An indexed At() walk over a shared
// list is quadratic in time, not in allocations, so it is invisible here —
// BenchmarkGetIn_ListPath is what covers that shape.
func TestGetIn_PathCursorDoesNotCopy(t *testing.T) {
	env := setupEnv(t)
	gf := getInGoFunc(t, env)
	ctx := core.EnsureEvalState(context.Background())

	// The subject misses on the first key, so the walk stops after one step and
	// no path key past the first is ever read: anything the call allocates in
	// proportion to path length can only come from materializing the path.
	subject := core.NewHashMap()
	require.NoError(t, subject.Set(core.Keyword{V: "other"}, core.Int{V: 1}))

	reps := []struct {
		name  string
		build func(int) core.Value
		sizes []int
	}{
		{"list", func(n int) core.Value { return core.NewList(pathKeys(n)) }, []int{8, 32, 33, 1024, 10000}},
		{"vector", func(n int) core.Value { return core.NewVector(pathKeys(n)) }, []int{8, 32, 33, 1024}},
		{"vector-conj", func(n int) core.Value { return conjVector(pathKeys(n)) }, []int{8, 32, 33, 1024}},
	}

	for _, rep := range reps {
		t.Run(rep.name, func(t *testing.T) {
			for _, n := range rep.sizes {
				path := rep.build(n)
				args := []core.Value{subject, path}

				require.Equal(t, n, pathLen(t, path))
				got, err := gf.Fn(ctx, nil, args, env)
				require.NoError(t, err)
				require.True(t, got.Equals(core.Nil{}), "path of %d keys must miss on its first key, got %v", n, got)

				allocs := testing.AllocsPerRun(100, func() {
					_, _ = gf.Fn(ctx, nil, args, env)
				})
				require.Zero(t, allocs, "%s path of %d keys allocates %.0f per call", rep.name, n, allocs)
			}
		})
	}
}
