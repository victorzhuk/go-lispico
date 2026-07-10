// Regression tests for bootstrap macros — verified through the default CL
// dialect evaluator so Lisp-2 function-cell binding is exercised.
package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func TestBootstrap_ThreadingMacro_ThreadFirst(t *testing.T) {
	t.Parallel()
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	result, err := eng.Eval(context.Background(), "test", "(-> 1 (+ 2))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 3}.Equals(result))
}

func TestBootstrap_ThreadingMacro_ThreadLast(t *testing.T) {
	t.Parallel()
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	result, err := eng.Eval(context.Background(), "test", "(->> (list 1 2 3) (map (fn (x) (+ x 1))))")
	require.NoError(t, err)
	expected := core.List{Items: []core.Value{core.Int{V: 2}, core.Int{V: 3}, core.Int{V: 4}}}
	assert.True(t, expected.Equals(result), "expected %v, got %v", expected, result)
}

func TestBootstrap_AsArrow(t *testing.T) {
	t.Parallel()
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	result, err := eng.Eval(context.Background(), "test", "(as-> 5 x (+ x 1))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 6}.Equals(result))
}

func TestBootstrap_IfLet(t *testing.T) {
	t.Parallel()
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	result, err := eng.Eval(context.Background(), "test", "(if-let (x 42) x 0)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(result))

	result, err = eng.Eval(context.Background(), "test", "(if-let (x nil) :yes :no)")
	require.NoError(t, err)
	assert.True(t, core.Keyword{V: "no"}.Equals(result))
}

func TestBootstrap_WhenLet(t *testing.T) {
	t.Parallel()
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	result, err := eng.Eval(context.Background(), "test", "(when-let (x 42) x)")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 42}.Equals(result))

	result, err = eng.Eval(context.Background(), "test", "(when-let (x nil) :yes)")
	require.NoError(t, err)
	assert.True(t, core.Nil{}.Equals(result))
}

func TestBootstrap_GetIn(t *testing.T) {
	t.Parallel()
	eng, err := New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eng.Close() })
	require.NoError(t, eng.Use(stdlib.New()))

	result, err := eng.Eval(context.Background(), "test", "(get-in (hash-map :a (hash-map :b 1)) (vector :a :b))")
	require.NoError(t, err)
	assert.True(t, core.Int{V: 1}.Equals(result))
}
