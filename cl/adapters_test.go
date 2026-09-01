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
// TestCLAdapters_Lisp2Callbacks: callbacks named in head position resolve
// through the function cell — lambda, defun, and GoFunc targets.
func TestCLAdapters_Lisp2Callbacks(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	_, err := e.Eval(ctx, "defun", "(defun dbl (x) (* x 2))")
	require.NoError(t, err)

	got, err := e.Eval(ctx, "mapcar", "(mapcar #'dbl '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, intList(2, 4, 6).Equals(got), "(mapcar #'dbl '(1 2 3)) = %v, want (2 4 6)", got)

	got, err = e.Eval(ctx, "funcall", "(funcall #'dbl 5)")
	require.NoError(t, err)
	assert.True(t, (core.Int{V: 10}).Equals(got), "(funcall #'dbl 5) = %v, want 10", got)

	_, err = e.Eval(ctx, "defun", "(defun lt (a b) (< a b))")
	require.NoError(t, err)

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'lt)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'lt) = %v, want (1 2 3)", got)

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) (lambda (a b) (< a b)))")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) (lambda (a b) (< a b))) = %v, want (1 2 3)", got)

	require.NoError(t, e.RootEnv().SetBoth("rec", core.GoFunc{
		Name: "rec",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Int{V: args[0].(core.Int).V * 2}, nil
		},
	}))

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'rec '(1 2 3))")
	require.NoError(t, err)
	assert.True(t, intList(2, 4, 6).Equals(got), "(mapcar #'rec '(1 2 3)) = %v, want (2 4 6)", got)

	require.NoError(t, e.RootEnv().SetBoth("ltrec", core.GoFunc{
		Name: "ltrec",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			return core.Bool{V: args[0].(core.Int).V < args[1].(core.Int).V}, nil
		},
	}))

	got, err = e.Eval(ctx, "sort", "(sort '(3 1 2) #'ltrec)")
	require.NoError(t, err)
	assert.True(t, intList(1, 2, 3).Equals(got), "(sort '(3 1 2) #'ltrec) = %v, want (1 2 3)", got)
}

// TestCLAdapters_EmptyAndNil: empty and nil subjects are valid inputs for
// every collection adapter; no callback runs over an empty sequence.
func TestCLAdapters_EmptyAndNil(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	var calls int
	require.NoError(t, e.RootEnv().SetBoth("f", core.GoFunc{
		Name: "f",
		Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
			calls++
			return args[0], nil
		},
	}))

	got, err := e.Eval(ctx, "nth", "(nth 0 nil)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(nth 0 nil) = %v, want nil", got)

	got, err = e.Eval(ctx, "mapcar", "(mapcar #'f nil)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(mapcar #'f nil) = %v, want nil", got)
	assert.Zero(t, calls, "mapcar over nil must not call the callback")

	got, err = e.Eval(ctx, "sort", "(sort nil #'f)")
	require.NoError(t, err)
	assert.True(t, (core.Nil{}).Equals(got), "(sort nil #'f) = %v, want nil", got)
	assert.Zero(t, calls, "sort of nil must not call the predicate")
}

// TestCLAdapters_CallbackErrors: a callback error propagates unchanged and no
// callback runs after the first error.
func TestCLAdapters_CallbackErrors(t *testing.T) {
	e := newEngine(t)
	ctx := context.Background()

	t.Run("typed predicate error", func(t *testing.T) {
		var calls int
		require.NoError(t, e.RootEnv().SetBoth("bad", core.GoFunc{
			Name: "bad",
			Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				calls++
				if calls == 2 {
					return nil, core.NewTypeError("int", core.Int{V: 1})
				}
				return core.Bool{V: true}, nil
			},
		}))

		_, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'bad)")
		require.Error(t, err)
		var typeErr *core.LispicoError
		require.ErrorAs(t, err, &typeErr)
		assert.Equal(t, "TypeError", typeErr.Code, "typed predicate error must propagate unchanged")
		assert.Equal(t, 2, calls, "no predicate call may follow the first error")
	})

	t.Run("typed callback error", func(t *testing.T) {
		var calls int
		require.NoError(t, e.RootEnv().SetBoth("badmap", core.GoFunc{
			Name: "badmap",
			Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				calls++
				if calls == 2 {
					return nil, core.NewTypeError("int", core.Int{V: 1})
				}
				return args[0], nil
			},
		}))

		_, err := e.Eval(ctx, "mapcar", "(mapcar #'badmap '(1 2 3))")
		require.Error(t, err)
		var typeErr *core.LispicoError
		require.ErrorAs(t, err, &typeErr)
		assert.Equal(t, "TypeError", typeErr.Code, "typed callback error must propagate unchanged")
		assert.Equal(t, 2, calls, "no callback may run after the first error")
	})

	t.Run("terminal predicate error", func(t *testing.T) {
		var calls int
		require.NoError(t, e.RootEnv().SetBoth("boom", core.GoFunc{
			Name: "boom",
			Fn: func(ctx context.Context, _ core.Evaluator, args []core.Value, _ *core.Env) (core.Value, error) {
				calls++
				if calls == 2 {
					return nil, core.NewResourceLimitError("boom")
				}
				return core.Bool{V: true}, nil
			},
		}))

		_, err := e.Eval(ctx, "sort", "(sort '(3 1 2) #'boom)")
		require.Error(t, err)
		assert.True(t, core.IsTerminalEvalError(err), "resource-limit callback error must be terminal, got %v", err)
		assert.Equal(t, 2, calls, "no predicate call may follow the first error")
	})
}
