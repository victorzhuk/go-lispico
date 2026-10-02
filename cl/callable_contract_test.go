package cl_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// Characterization of the CL adapter contract over the core callable
// predicate. These tests pass before and after the chunk; they are excluded
// from redRun.

func TestCLMapcar_VMClosure(t *testing.T) {
	e := newEngine(t)
	got, err := e.Eval(context.Background(), "mapcar", "(mapcar (fn (x) (* x 2)) '(1 2))")
	require.NoError(t, err)
	want := core.NewList([]core.Value{core.Int{V: 2}, core.Int{V: 4}})
	assert.True(t, want.Equals(got), "(mapcar (fn (x) (* x 2)) '(1 2)) => %v, want (2 4)", got)
}

func TestCLMapcar_NonCallable(t *testing.T) {
	e := newEngine(t)
	_, err := e.Eval(context.Background(), "mapcar", "(mapcar 5 '(1 2))")
	require.Error(t, err)
	var le *core.LispicoError
	require.ErrorAs(t, err, &le)
	assert.Equal(t, "TypeError", le.Code)
	assert.Equal(t, "expected function, got core.Int", le.Message)
}

func TestCLSort_VMClosureCallbacks(t *testing.T) {
	e := newEngine(t)
	got, err := e.Eval(context.Background(), "sort", "(sort '(3 1 2) (fn (a b) (< a b)))")
	require.NoError(t, err)
	asc := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})
	assert.True(t, asc.Equals(got), "sort with closure predicate => %v, want (1 2 3)", got)

	e2 := newEngine(t)
	got, err = e2.Eval(context.Background(), "sort-key",
		"(sort '(3 1 2) (fn (a b) (< a b)) :key (fn (x) (- 0 x)))")
	require.NoError(t, err)
	desc := core.NewList([]core.Value{core.Int{V: 3}, core.Int{V: 2}, core.Int{V: 1}})
	assert.True(t, desc.Equals(got), "sort with closure :key => %v, want (3 2 1)", got)
}

func TestCLSort_NonCallableCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"predicate", "(sort '(3 1 2) 5)"},
		{"key", "(sort '(3 1 2) (fn (a b) (< a b)) :key 5)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEngine(t)
			_, err := e.Eval(context.Background(), "sort", tc.src)
			require.Error(t, err)
			var le *core.LispicoError
			require.ErrorAs(t, err, &le)
			assert.Equal(t, "TypeError", le.Code)
			assert.Equal(t, "expected function, got core.Int", le.Message)
		})
	}
}

func TestCLSort_NilKey(t *testing.T) {
	e := newEngine(t)
	got, err := e.Eval(context.Background(), "sort", "(sort '(3 1 2) (fn (a b) (< a b)) :key nil)")
	require.NoError(t, err)
	asc := core.NewList([]core.Value{core.Int{V: 1}, core.Int{V: 2}, core.Int{V: 3}})
	assert.True(t, asc.Equals(got), ":key nil must disable the key callback, got %v", got)
}
