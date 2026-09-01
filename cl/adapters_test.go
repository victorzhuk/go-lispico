package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func listOf(vs ...core.Value) core.List {
	return core.NewList(vs)
}

func intList(vs ...int64) core.List {
	items := make([]core.Value, len(vs))
	for i, v := range vs {
		items[i] = core.Int{V: v}
	}
	return core.NewList(items)
}

func TestCLNth_Goldens(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	got, err := e.Eval(ctx, "nth", "(nth 1 '(10 20 30))")
	require.NoError(t, err)
	assert.True(t, (core.Int{V: 20}).Equals(got), "(nth 1 '(10 20 30)) = %v, want 20", got)

	got, err = e.Eval(ctx, "nth", "(nth 5 '(1 2))")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(nth 5 '(1 2)) = %v, want nil (out of range is nil, not an error)", got)

	got, err = e.Eval(ctx, "nth", "(nth 0 nil)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(nth 0 nil) = %v, want nil", got)
}

func TestCLMapcar_Goldens(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	got, err := e.Eval(ctx, "mapcar", "(mapcar #'+ '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(mapcar #'+ '(1 2 3)) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'+ '(1 2) '(10 20))")
	require.NoError(t, err)
	assert.True(t, intList(11, 22).Equals(got), "(mapcar #'+ '(1 2) '(10 20)) = %v, want (11 22)", got)

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'+ '(1 2 3) '(10 20))")
	require.NoError(t, err)
	assert.True(t, intList(11, 22).Equals(got), "(mapcar #'+ '(1 2 3) '(10 20)) = %v, want (11 22): shortest list terminates", got)
}

func TestCLSort_Goldens(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	got, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'<)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'<) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'>)")
	require.NoError(t, err)
	assert.True(t, intList(3, 2, 1).Equals(got), "(sort '(3 1 2) #'>) = %v, want (3 2 1)", got)

	got, err = e.Eval(ctx, "sort", "(sort '(\"bb\" \"a\" \"ccc\") #'< :key #'length)")
	require.NoError(t, err)
	assert.True(t, listOf(core.String{V: "a"}, core.String{V: "bb"}, core.String{V: "ccc"}).Equals(got),
		"(sort with :key #'length) = %v, want (\"a\" \"bb\" \"ccc\")", got)

	got, err = e.Eval(ctx, "sort", "(sort '(\"bb\" \"a\") #'> :key #'length)")
	require.NoError(t, err)
	assert.True(t, listOf(core.String{V: "bb"}, core.String{V: "a"}).Equals(got),
		"(sort with predicate and :key) = %v, want (\"bb\" \"a\")", got)

	_, err = e.Eval(ctx, "sort", "(sort '(3 1 2))")
	assert.Error(t, err, "sort without a predicate must be rejected by the CL grammar")

	_, err = e.Eval(ctx, "sort", "(sort '(3 1 2) :key #'length)")
	assert.Error(t, err, "sort with :key but no predicate must be rejected by the CL grammar")
}
