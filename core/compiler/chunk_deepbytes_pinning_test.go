package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// TestChunkDeepBytes_FlatVsSharedRepresentationIdentical pins that
// compile-time chunk-constant charging (chunkDeepBytes, via
// core.ValueDeepBytes) doesn't change when a quoted List switches between a
// flat backing and a shared-tail chain: it walks elements through each/At,
// which both representations expose identically, never anything specific to
// the chosen representation. Slice D's incremental-charging change touches
// cons/conj/concat's own byte formulas, not this one — this test pins that
// the representation swap (slice C) left it alone.
func TestChunkDeepBytes_FlatVsSharedRepresentationIdentical(t *testing.T) {
	items := make([]core.Value, 1000)
	for i := range items {
		items[i] = core.Int{V: int64(i)}
	}

	const tail = 20
	flatList := core.NewList(append([]core.Value(nil), items[len(items)-tail:]...))
	sharedList := core.NewList(append([]core.Value(nil), items...))
	for sharedList.Len() > tail {
		sharedList = sharedList.Rest()
	}
	require.Equal(t, tail, flatList.Len())
	require.Equal(t, tail, sharedList.Len())

	deepBytes := func(constant core.Value) int64 {
		form := core.NewList([]core.Value{core.Symbol{V: "quote"}, constant})
		c := NewCompiler("test")
		require.NoError(t, c.Compile(form))
		c.MarkCaptures()
		return c.Chunk().DeepBytes
	}

	flatBytes := deepBytes(flatList)
	sharedBytes := deepBytes(sharedList)
	require.Equal(t, flatBytes, sharedBytes, "chunk.DeepBytes must not depend on List's internal representation")
	require.Greater(t, flatBytes, int64(0))
}

func TestFoldedConstant_RetainedChargeIncludesCollection(t *testing.T) {
	folded := core.NewVector([]core.Value{
		core.Keyword{V: "read"},
		core.Keyword{V: "grep"},
	})
	c := NewCompiler("test")
	require.NoError(t, c.Compile(folded))
	c.MarkCaptures()

	require.GreaterOrEqual(t, c.Chunk().DeepBytes, core.ValueDeepBytes(folded))
}
