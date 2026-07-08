package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// #' reads as (function x) only when the Dialect enables it; the default Dialect
// leaves # non-special, so #'foo fails to read.
func TestDialect_Reader_FunctionRefGatedByFlag(t *testing.T) {
	on, err := New(nil, WithDialect(core.FullDialect().WithFunctionRef()))
	require.NoError(t, err)
	defer on.Close()

	got, err := on.Eval(context.Background(), "reader", "(quote #'foo)")
	require.NoError(t, err)
	want := core.List{Items: []core.Value{core.Symbol{V: "function"}, core.Symbol{V: "foo"}}}
	assert.True(t, want.Equals(got), "want (function foo), got %v", got)

	off, err := New(nil)
	require.NoError(t, err)
	defer off.Close()

	_, err = off.Eval(context.Background(), "reader", "(quote #'foo)")
	require.Error(t, err, "#' must not be special under the default Dialect")
}

// #(...) reads as a vector when the Dialect enables it.
func TestDialect_Reader_ReaderVectorGatedByFlag(t *testing.T) {
	on, err := New(nil, WithDialect(core.FullDialect().WithReaderVector()))
	require.NoError(t, err)
	defer on.Close()

	got, err := on.Eval(context.Background(), "reader", "(quote #(1 2 3))")
	require.NoError(t, err)
	want := core.Vector{Items: []core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}}}
	assert.True(t, want.Equals(got), "want #(1 2 3) vector, got %v", got)

	off, err := New(nil)
	require.NoError(t, err)
	defer off.Close()

	_, err = off.Eval(context.Background(), "reader", "(quote #(1 2 3))")
	require.Error(t, err, "#(...) must not be special under the default Dialect")
}

// [..] literals read as vectors only while enabled; disabling the flag makes
// [1 2] fail to read as a vector literal.
func TestDialect_Reader_BracketLiteralsGatedByFlag(t *testing.T) {
	on, err := New(nil)
	require.NoError(t, err)
	defer on.Close()

	got, err := on.Eval(context.Background(), "reader", "(quote [1 2])")
	require.NoError(t, err)
	want := core.Vector{Items: []core.Value{core.Int{V: 1}, core.Int{V: 2}}}
	assert.True(t, want.Equals(got), "want [1 2] vector, got %v", got)

	got, err = on.Eval(context.Background(), "reader", "(quote {:a 1})")
	require.NoError(t, err)
	_, isMap := got.(*core.HashMap)
	assert.True(t, isMap, "want {..} map literal, got %T", got)

	off, err := New(nil, WithDialect(core.FullDialect().WithoutBracketLiterals()))
	require.NoError(t, err)
	defer off.Close()

	_, err = off.Eval(context.Background(), "reader", "(quote [1 2])")
	require.Error(t, err, "[..] must not read as a vector literal when disabled")

	_, err = off.Eval(context.Background(), "reader", "(quote {:a 1})")
	require.Error(t, err, "{..} must not read as a map literal when disabled")
}
