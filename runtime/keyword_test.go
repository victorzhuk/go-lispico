package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// Proves keyword-as-function application, (:key m), behaves identically
// through the tree-walker (default Engine) and the bytecode VM
// (WithBytecode), across both the Eval path (literal keyword in head
// position) and the Call path (a bound name resolved to a keyword).
func TestKeywordApplication_EvalAndCall_Parity(t *testing.T) {
	t.Parallel()

	treeEng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = treeEng.Close() })

	vmEng, err := New(nil, WithBytecode())
	require.NoError(t, err)
	t.Cleanup(func() { _ = vmEng.Close() })

	m := core.NewHashMap()
	require.NoError(t, m.Set(core.Keyword{V: "name"}, core.String{V: "Alice"}))

	ctx := context.Background()
	for _, eng := range []Engine{treeEng, vmEng} {
		require.NoError(t, eng.Bind("m", m))
		_, err := eng.Eval(ctx, "bind-k", "(def k :name)")
		require.NoError(t, err)
	}

	t.Run("hit via Eval", func(t *testing.T) {
		treeResult, err := treeEng.Eval(ctx, "hit", "(:name m)")
		require.NoError(t, err)
		vmResult, err := vmEng.Eval(ctx, "hit", "(:name m)")
		require.NoError(t, err)
		assert.True(t, treeResult.Equals(core.String{V: "Alice"}))
		assert.True(t, vmResult.Equals(core.String{V: "Alice"}))
	})

	t.Run("miss via Eval", func(t *testing.T) {
		treeResult, err := treeEng.Eval(ctx, "miss", "(:missing m)")
		require.NoError(t, err)
		vmResult, err := vmEng.Eval(ctx, "miss", "(:missing m)")
		require.NoError(t, err)
		assert.True(t, treeResult.Equals(core.Nil{}))
		assert.True(t, vmResult.Equals(core.Nil{}))
	})

	t.Run("non-map argument via Eval", func(t *testing.T) {
		treeResult, err := treeEng.Eval(ctx, "non-map", "(:name 42)")
		require.NoError(t, err)
		vmResult, err := vmEng.Eval(ctx, "non-map", "(:name 42)")
		require.NoError(t, err)
		assert.True(t, treeResult.Equals(core.Nil{}))
		assert.True(t, vmResult.Equals(core.Nil{}))
	})

	t.Run("hit via Call", func(t *testing.T) {
		treeResult, err := treeEng.Call(ctx, "k", m)
		require.NoError(t, err)
		vmResult, err := vmEng.Call(ctx, "k", m)
		require.NoError(t, err)
		assert.True(t, treeResult.Equals(core.String{V: "Alice"}))
		assert.True(t, vmResult.Equals(core.String{V: "Alice"}))
	})

	t.Run("arity error via Eval matches Code", func(t *testing.T) {
		_, treeErr := treeEng.Eval(ctx, "arity", "(:name)")
		_, vmErr := vmEng.Eval(ctx, "arity", "(:name)")
		require.Error(t, treeErr)
		require.Error(t, vmErr)

		var treeLE, vmLE *core.LispicoError
		require.ErrorAs(t, treeErr, &treeLE)
		require.ErrorAs(t, vmErr, &vmLE)
		assert.Equal(t, treeLE.Code, vmLE.Code)
	})
}
